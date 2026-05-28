package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"lark-daemon/internal/proto"
	"lark-daemon/internal/server/websocket"
)

func (r *Router) handleCreateReview(w http.ResponseWriter, req *http.Request) {
	wsID := chi.URLParam(req, "id")
	member := requireWorkspaceAuth(w, req, wsID)
	if member == nil {
		return
	}
	var body struct {
		ReviewerID string `json:"reviewer_id"`
		Subject    string `json:"subject"`
		Content    string `json:"content"`
		ChannelID  string `json:"channel_id"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeErrorCode(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid request body")
		return
	}
	if body.ReviewerID == "" || body.Subject == "" || body.Content == "" || body.ChannelID == "" {
		writeErrorCode(w, http.StatusBadRequest, ErrCodeBadRequest, "reviewer_id, subject, content, and channel_id are required")
		return
	}
	rr := &proto.ReviewRequest{
		WorkspaceID: wsID,
		ChannelID:   body.ChannelID,
		RequesterID: member.ID,
		ReviewerID:  body.ReviewerID,
		Subject:     body.Subject,
		Content:     body.Content,
		Status:      proto.ReviewPending,
	}
	if err := r.services.CreateReviewRequest(req.Context(), rr); err != nil {
		writeErrorCode(w, http.StatusInternalServerError, ErrCodeInternal, "internal error")
		return
	}
	// Create a message in the channel about the review request
	msg := &proto.Message{
		ChannelID: body.ChannelID,
		SenderID:  member.ID,
		Content:   fmt.Sprintf("Review requested: %s", body.Subject),
		Type:      "text",
		Metadata:  json.RawMessage(fmt.Sprintf(`{"type":"review_request","review_id":"%s"}`, rr.ID)),
	}
	r.services.CreateMessage(context.Background(), msg)
	r.hub.SendNewMessage(body.ChannelID, msg)
	// Create inbox notification for reviewer
	n := &proto.Notification{
		MemberID:    body.ReviewerID,
		Type:        "review_request",
		Title:       "Review requested",
		Body:        fmt.Sprintf("%s requested your review: %s", member.Name, body.Subject),
		ChannelID:   body.ChannelID,
		MessageID:   msg.ID,
		SourceType:  "review_request",
		Priority:    "high",
		AckRequired: true,
		Payload:     fmt.Sprintf(`{"review_id":"%s","channel_id":"%s"}`, rr.ID, body.ChannelID),
	}
	r.services.CreateNotification(context.Background(), n)
	if c := r.hub.GetConn(body.ReviewerID); c != nil {
		c.Send(websocket.NewEnvelope(websocket.EventNotificationNew, n))
	}
	writeJSON(w, http.StatusCreated, rr)
}

func (r *Router) handleListReviews(w http.ResponseWriter, req *http.Request) {
	reviewerID := req.URL.Query().Get("reviewer_id")
	status := req.URL.Query().Get("status")
	if reviewerID == "" {
		member := memberFromContext(req)
		if member != nil {
			reviewerID = member.ID
		}
	}
	reviews, err := r.services.ListReviewRequests(req.Context(), reviewerID, status)
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, ErrCodeInternal, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, reviews)
}

func (r *Router) handleGetReview(w http.ResponseWriter, req *http.Request) {
	reviewID := chi.URLParam(req, "id")
	rr, err := r.services.GetReviewRequest(req.Context(), reviewID)
	if err != nil || rr == nil {
		writeErrorCode(w, http.StatusNotFound, ErrCodeNotFound, "review not found")
		return
	}
	writeJSON(w, http.StatusOK, rr)
}

func (r *Router) handleUpdateReview(w http.ResponseWriter, req *http.Request) {
	reviewID := chi.URLParam(req, "id")
	rr, err := r.services.GetReviewRequest(req.Context(), reviewID)
	if err != nil || rr == nil {
		writeErrorCode(w, http.StatusNotFound, ErrCodeNotFound, "review not found")
		return
	}
	var body struct {
		Status  string `json:"status"`
		Comment string `json:"comment,omitempty"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeErrorCode(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid request body")
		return
	}
	rr.Status = proto.ReviewStatus(body.Status)
	rr.ReviewComment = body.Comment
	if body.Status == string(proto.ReviewApproved) || body.Status == string(proto.ReviewChangesRequired) {
		rr.ReviewedAt = time.Now().UnixMilli()
	}
	if err := r.services.UpdateReviewRequest(req.Context(), rr); err != nil {
		writeErrorCode(w, http.StatusInternalServerError, ErrCodeInternal, "internal error")
		return
	}
	// Post result to the channel
	msg := &proto.Message{
		ChannelID: rr.ChannelID,
		SenderID:  rr.ReviewerID,
		Content:   fmt.Sprintf("Review %s: %s", body.Status, body.Comment),
		Type:      "text",
	}
	r.services.CreateMessage(context.Background(), msg)
	r.hub.SendNewMessage(rr.ChannelID, msg)
	// Notify requester
	n := &proto.Notification{
		MemberID:   rr.RequesterID,
		Type:       "review_result",
		Title:      "Review completed",
		Body:       fmt.Sprintf("Your review was %s", body.Status),
		ChannelID:  rr.ChannelID,
		SourceType: "review_result",
		Priority:   "high",
	}
	r.services.CreateNotification(context.Background(), n)
	if c := r.hub.GetConn(rr.RequesterID); c != nil {
		c.Send(websocket.NewEnvelope(websocket.EventNotificationNew, n))
	}
	writeJSON(w, http.StatusOK, rr)
}
