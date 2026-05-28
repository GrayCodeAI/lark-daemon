package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"lark-daemon/internal/proto"
)

func (r *Router) handleCreateDraft(w http.ResponseWriter, req *http.Request) {
	agentID := chi.URLParam(req, "id")
	var body struct {
		ChannelID string `json:"channel_id"`
		Content   string `json:"content"`
		ThreadID  string `json:"thread_id,omitempty"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeErrorCode(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid request body")
		return
	}
	if body.ChannelID == "" || body.Content == "" {
		writeErrorCode(w, http.StatusBadRequest, ErrCodeBadRequest, "channel_id and content are required")
		return
	}
	roomVersion, err := r.services.GetChannelRoomVersion(req.Context(), body.ChannelID)
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, ErrCodeInternal, "internal error")
		return
	}
	d := &proto.HeldDraft{
		AgentID:     agentID,
		ChannelID:   body.ChannelID,
		Content:     body.Content,
		ThreadID:    body.ThreadID,
		RoomVersion: roomVersion,
		Status:      proto.DraftHeld,
		ExpiresAt:   time.Now().Add(10 * time.Minute).UnixMilli(),
	}
	if err := r.services.CreateDraft(req.Context(), d); err != nil {
		writeErrorCode(w, http.StatusInternalServerError, ErrCodeInternal, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, d)
}

func (r *Router) handleListDrafts(w http.ResponseWriter, req *http.Request) {
	agentID := chi.URLParam(req, "id")
	status := req.URL.Query().Get("status")
	drafts, err := r.services.ListDrafts(req.Context(), agentID, status)
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, ErrCodeInternal, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, drafts)
}

func (r *Router) handleValidateDraft(w http.ResponseWriter, req *http.Request) {
	draftID := chi.URLParam(req, "id")
	d, err := r.services.GetDraft(req.Context(), draftID)
	if err != nil || d == nil {
		writeErrorCode(w, http.StatusNotFound, ErrCodeNotFound, "draft not found")
		return
	}
	currentVersion, err := r.services.GetChannelRoomVersion(req.Context(), d.ChannelID)
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, ErrCodeInternal, "internal error")
		return
	}
	delta := currentVersion - d.RoomVersion
	result := map[string]interface{}{
		"draft_id":        d.ID,
		"valid":           delta <= 5,
		"current_version": currentVersion,
		"draft_version":   d.RoomVersion,
		"version_delta":   delta,
	}
	if delta > 5 {
		msgs, _ := r.services.GetRecentMessages(req.Context(), d.ChannelID, 5)
		result["recent_messages"] = msgs
	}
	writeJSON(w, http.StatusOK, result)
}

func (r *Router) handleSendDraft(w http.ResponseWriter, req *http.Request) {
	draftID := chi.URLParam(req, "id")
	var body struct {
		ForceVersion bool `json:"force_version,omitempty"`
	}
	json.NewDecoder(req.Body).Decode(&body)
	d, err := r.services.GetDraft(req.Context(), draftID)
	if err != nil || d == nil {
		writeErrorCode(w, http.StatusNotFound, ErrCodeNotFound, "draft not found")
		return
	}
	if d.Status != proto.DraftHeld {
		writeErrorCode(w, http.StatusBadRequest, ErrCodeBadRequest, "draft is not in held status")
		return
	}
	currentVersion, _ := r.services.GetChannelRoomVersion(req.Context(), d.ChannelID)
	delta := currentVersion - d.RoomVersion
	if delta > 5 && !body.ForceVersion {
		writeErrorCode(w, http.StatusConflict, ErrCodeBadRequest, "draft is stale, use force_version to override")
		return
	}
	msg := &proto.Message{
		ChannelID:   d.ChannelID,
		SenderID:    d.AgentID,
		ThreadID:    d.ThreadID,
		Content:     d.Content,
		ContentType: "text",
		Type:        "text",
	}
	if err := r.services.CreateMessage(req.Context(), msg); err != nil {
		writeErrorCode(w, http.StatusInternalServerError, ErrCodeInternal, "internal error")
		return
	}
	r.services.UpdateDraftStatus(req.Context(), draftID, proto.DraftSent)
	r.hub.SendNewMessage(d.ChannelID, msg)
	writeJSON(w, http.StatusOK, map[string]string{"message_id": msg.ID, "status": "sent"})
}

func (r *Router) handleCancelDraft(w http.ResponseWriter, req *http.Request) {
	draftID := chi.URLParam(req, "id")
	if err := r.services.UpdateDraftStatus(req.Context(), draftID, proto.DraftCancelled); err != nil {
		writeErrorCode(w, http.StatusInternalServerError, ErrCodeInternal, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}
