package api

import (
	"crypto/subtle"
	"net/http"

	"github.com/go-chi/chi/v5"

	"lark-daemon/internal/proto"
)

// --- Webhook handlers ---

func (r *Router) handleCreateWebhook(w http.ResponseWriter, req *http.Request) {
	member := requireWorkspaceAuth(w, req, chi.URLParam(req, "id"))
	if member == nil {
		return
	}
	var body struct {
		ChannelID string `json:"channel_id"`
		Name      string `json:"name"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.ChannelID == "" || body.Name == "" {
		writeError(w, http.StatusBadRequest, "channel_id and name required")
		return
	}
	wb := &proto.Webhook{
		WorkspaceID: chi.URLParam(req, "id"),
		ChannelID:   body.ChannelID,
		Name:        body.Name,
		CreatedBy:   member.ID,
	}
	if err := r.services.CreateWebhook(req.Context(), wb); err != nil {
		serverError(w, err, "create webhook failed")
		return
	}
	writeJSON(w, http.StatusCreated, wb)
}

func (r *Router) handleListWebhooks(w http.ResponseWriter, req *http.Request) {
	if requireWorkspaceAuth(w, req, chi.URLParam(req, "id")) == nil {
		return
	}
	webhooks, err := r.services.ListWebhooks(req.Context(), chi.URLParam(req, "id"))
	if err != nil {
		serverError(w, err, "list webhooks failed")
		return
	}
	// Mask secrets in list responses
	for _, wb := range webhooks {
		if len(wb.Secret) > 8 {
			wb.Secret = wb.Secret[:8] + "..."
		}
	}
	writeJSON(w, http.StatusOK, webhooks)
}

func (r *Router) handleDeleteWebhook(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	webhook, err := r.services.GetWebhook(req.Context(), chi.URLParam(req, "id"))
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	if webhook == nil || webhook.WorkspaceID != member.WorkspaceID {
		writeError(w, http.StatusNotFound, "webhook not found")
		return
	}
	if err := r.services.DeleteWebhook(req.Context(), chi.URLParam(req, "id")); err != nil {
		serverError(w, err, "delete webhook failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleWebhookExecute handles unauthenticated webhook POST requests.
func (r *Router) handleWebhookExecute(w http.ResponseWriter, req *http.Request) {
	webhookID := chi.URLParam(req, "id")
	secret := chi.URLParam(req, "secret")
	wb, err := r.services.GetWebhook(req.Context(), webhookID)
	if err != nil {
		serverError(w, err, "get webhook failed")
		return
	}
	if wb == nil || subtle.ConstantTimeCompare([]byte(wb.Secret), []byte(secret)) != 1 {
		writeError(w, http.StatusUnauthorized, "invalid webhook")
		return
	}
	var body struct {
		Content string `json:"content"`
		Text    string `json:"text"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	content := body.Content
	if content == "" {
		content = body.Text
	}
	if content == "" {
		writeError(w, http.StatusBadRequest, "content or text required")
		return
	}
	msg := &proto.Message{
		ChannelID: wb.ChannelID,
		SenderID:  wb.CreatedBy,
		Content:   content,
	}
	if err := r.services.CreateMessage(req.Context(), msg); err != nil {
		serverError(w, err, "webhook message failed")
		return
	}
	r.hub.SendNewMessage(msg.ChannelID, msg)
	writeJSON(w, http.StatusCreated, map[string]string{"status": "ok", "message_id": msg.ID})
}

// --- Admin handlers ---

