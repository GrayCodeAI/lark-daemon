package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"lark-daemon/internal/proto"
	"lark-daemon/internal/server/websocket"
)

// --- Messages ---

func (r *Router) handleCreateMessage(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	channelID := chi.URLParam(req, "id")
	// Verify sender is a member of the channel.
	isMember, err := r.store.IsChannelMember(req.Context(), channelID, member.ID)
	if err != nil {
		serverError(w, err, "create message: membership check failed")
		return
	}
	if !isMember {
		writeError(w, http.StatusForbidden, "not a member of this channel")
		return
	}
	var body struct {
		Content     string `json:"content"`
		ThreadID    string `json:"thread_id"`
		ContentType string `json:"content_type"`
		FileID      string `json:"file_id"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	body.Content = strings.TrimSpace(body.Content)
	if body.Content == "" {
		writeError(w, http.StatusBadRequest, "content required")
		return
	}
	if len(body.Content) > 10000 {
		writeError(w, http.StatusBadRequest, "content too long (max 10000 characters)")
		return
	}
	msg := &proto.Message{
		ChannelID:   channelID,
		SenderID:    member.ID,
		Content:     body.Content,
		ThreadID:    body.ThreadID,
		ContentType: body.ContentType,
		FileID:      body.FileID,
	}
	if msg.ContentType == "" {
		msg.ContentType = "text"
	}
	if err := r.services.CreateMessage(req.Context(), msg); err != nil {
		serverError(w, err, "create message failed")
		return
	}
	r.hub.SendNewMessage(msg.ChannelID, msg)
	r.handleMentions(msg)
	r.notifyDMRecipients(msg)
	// Record usage for billing
	if ch, _ := r.store.GetChannel(req.Context(), channelID); ch != nil {
		r.recordUsage(ch.WorkspaceID, "messages", 1)
	}
	writeJSON(w, http.StatusCreated, msg)
}

func (r *Router) handleListMessages(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	channelID := chi.URLParam(req, "id")
	isMember, err := r.store.IsChannelMember(req.Context(), channelID, member.ID)
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	if !isMember {
		writeError(w, http.StatusForbidden, "not a member of this channel")
		return
	}
	limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(req.URL.Query().Get("offset"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	msgs, err := r.services.ListMessages(req.Context(), channelID, limit, offset)
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, msgs)
}

func (r *Router) handleUpdateMessage(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	existing, err := r.services.GetMessage(req.Context(), chi.URLParam(req, "id"))
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "message not found")
		return
	}
	if existing.SenderID != member.ID {
		writeError(w, http.StatusForbidden, "not message author")
		return
	}
	id := existing.ID
	channelID := existing.ChannelID
	senderID := existing.SenderID
	createdAt := existing.CreatedAt
	if !decodeAndProtect(w, req, existing, func() {
		existing.ID = id
		existing.ChannelID = channelID
		existing.SenderID = senderID
		existing.CreatedAt = createdAt
	}) {
		return
	}
	// Save edit history before updating
	if existing.Content != "" {
		editHistory := &proto.EditHistory{
			MessageID: existing.ID,
			Content:   existing.Content,
			EditedAt:  time.Now().UnixMilli(),
			EditedBy:  member.ID,
		}
		_ = r.services.CreateEditHistory(req.Context(), editHistory)
	}
	r.trackMessageEdit(existing)
	if err := r.services.UpdateMessage(req.Context(), existing); err != nil {
		serverError(w, err, "internal error")
		return
	}
	r.hub.BroadcastToChannel(existing.ChannelID, websocket.NewEnvelope("message.edit", existing))
	writeJSON(w, http.StatusOK, existing)
}

func (r *Router) handleDeleteMessage(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	existing, err := r.services.GetMessage(req.Context(), chi.URLParam(req, "id"))
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "message not found")
		return
	}
	ch, err := r.services.GetChannel(req.Context(), existing.ChannelID)
	if err != nil || ch == nil || ch.WorkspaceID != member.WorkspaceID {
		writeError(w, http.StatusNotFound, "message not found")
		return
	}
	if existing.SenderID != member.ID {
		writeError(w, http.StatusForbidden, "not message author")
		return
	}
	if err := r.services.DeleteMessage(req.Context(), chi.URLParam(req, "id")); err != nil {
		serverError(w, err, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Threads ---

func (r *Router) handleGetThread(w http.ResponseWriter, req *http.Request) {
	caller := memberFromContext(req)
	if caller == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	threadID := chi.URLParam(req, "id")
	// Verify the parent message exists and caller is a member of its channel
	parent, err := r.services.GetMessage(req.Context(), threadID)
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	if parent == nil {
		writeError(w, http.StatusNotFound, "thread not found")
		return
	}
	isMember, err := r.store.IsChannelMember(req.Context(), parent.ChannelID, caller.ID)
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	if !isMember {
		writeError(w, http.StatusForbidden, "not a member of this channel")
		return
	}
	msgs, err := r.services.ListThreadMessages(req.Context(), threadID)
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, msgs)
}

func (r *Router) handleSearch(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	q := req.URL.Query().Get("q")
	if q == "" {
		writeError(w, http.StatusBadRequest, "q is required")
		return
	}
	channelID := req.URL.Query().Get("channel")
	limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	msgs, err := r.services.SearchMessages(req.Context(), q, channelID, member.WorkspaceID, limit)
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, msgs)
}

// --- Message edit tracking ---

func (r *Router) trackMessageEdit(msg *proto.Message) {
	msg.EditedAt = time.Now().UnixMilli()
	msg.EditCount++
}

func (r *Router) handleGetEditHistory(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	messageID := chi.URLParam(req, "id")
	existing, err := r.services.GetMessage(req.Context(), messageID)
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "message not found")
		return
	}
	isMember, err := r.store.IsChannelMember(req.Context(), existing.ChannelID, member.ID)
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	if !isMember {
		writeError(w, http.StatusForbidden, "not a member of this channel")
		return
	}
	history, err := r.services.ListEditHistory(req.Context(), messageID)
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, history)
}

// notifyDMRecipients creates notifications for DM channel members (excluding sender).
func (r *Router) notifyDMRecipients(msg *proto.Message) {
	ch, err := r.services.GetChannel(context.Background(), msg.ChannelID)
	if err != nil || ch == nil {
		return
	}
	if ch.Type != proto.ChannelDM && ch.Type != proto.ChannelGroupDM {
		return
	}
	members, err := r.services.ListChannelMembers(context.Background(), msg.ChannelID)
	if err != nil {
		return
	}
	sender, _ := r.services.GetMember(context.Background(), msg.SenderID)
	senderName := "Someone"
	if sender != nil {
		senderName = sender.Name
	}
	for _, m := range members {
		if m.ID == msg.SenderID {
			continue
		}
		n := &proto.Notification{
			MemberID:  m.ID,
			Type:      "dm",
			Title:     "New message from " + senderName,
			Body:      msg.Content,
			ChannelID: msg.ChannelID,
			MessageID: msg.ID,
		}
		_ = r.services.CreateNotification(context.Background(), n)
		if c := r.hub.GetConn(m.ID); c != nil {
			c.Send(websocket.NewEnvelope(websocket.EventNotificationNew, n))
		}
	}
}
