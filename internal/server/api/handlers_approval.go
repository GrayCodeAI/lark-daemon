package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"lark-daemon/internal/proto"
	"lark-daemon/internal/server/websocket"
)

// --- Approval handlers ---

func (r *Router) handleCreateApproval(w http.ResponseWriter, req *http.Request) {
	member := requireWorkspaceAuth(w, req, chi.URLParam(req, "id"))
	if member == nil {
		return
	}
	workspaceID := chi.URLParam(req, "id")
	var body struct {
		ChannelID string `json:"channel_id,omitempty"`
		Action    string `json:"action"`
		Payload   string `json:"payload,omitempty"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.Action == "" {
		writeError(w, http.StatusBadRequest, "action required")
		return
	}
	a := &proto.ApprovalRequest{
		WorkspaceID: workspaceID,
		AgentID:     member.ID,
		ChannelID:   body.ChannelID,
		Action:      body.Action,
		Payload:     body.Payload,
	}
	if err := r.services.CreateApproval(req.Context(), a); err != nil {
		serverError(w, err, "internal error")
		return
	}
	// Broadcast to workspace that an approval is pending
	r.hub.BroadcastToAll(websocket.NewEnvelope(websocket.EventApprovalRequest, a))
	writeJSON(w, http.StatusCreated, a)
}

func (r *Router) handleGetApproval(w http.ResponseWriter, req *http.Request) {
	caller := memberFromContext(req)
	if caller == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	a, err := r.services.GetApproval(req.Context(), chi.URLParam(req, "id"))
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	if a == nil || a.WorkspaceID != caller.WorkspaceID {
		writeError(w, http.StatusNotFound, "approval not found")
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (r *Router) handleListApprovals(w http.ResponseWriter, req *http.Request) {
	if requireWorkspaceAuth(w, req, chi.URLParam(req, "id")) == nil {
		return
	}
	status := proto.ApprovalStatus(req.URL.Query().Get("status"))
	approvals, err := r.services.ListApprovals(req.Context(), chi.URLParam(req, "id"), status)
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, approvals)
}

func (r *Router) handleReviewApproval(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	approvalID := chi.URLParam(req, "id")
	existing, err := r.services.GetApproval(req.Context(), approvalID)
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "approval not found")
		return
	}
	if existing.Status != proto.ApprovalPending {
		writeError(w, http.StatusBadRequest, "already reviewed")
		return
	}
	var body struct {
		Approved bool   `json:"approved"`
		Note     string `json:"note,omitempty"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.Approved {
		existing.Status = proto.ApprovalApproved
	} else {
		existing.Status = proto.ApprovalDenied
	}
	existing.ReviewerID = member.ID
	existing.ReviewNote = body.Note
	existing.ReviewedAt = time.Now().UnixMilli()
	if err := r.services.UpdateApproval(req.Context(), existing); err != nil {
		serverError(w, err, "internal error")
		return
	}
	// Notify the agent of the result
	r.hub.BroadcastToAll(websocket.NewEnvelope(websocket.EventApprovalResult, existing))
	writeJSON(w, http.StatusOK, existing)
}

// handleWSApprovalRequest processes an agent's approval.request via WebSocket.
