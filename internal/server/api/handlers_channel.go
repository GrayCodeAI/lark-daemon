package api

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"lark-daemon/internal/proto"
)

func (r *Router) handleCreateChannel(w http.ResponseWriter, req *http.Request) {
	if requireWorkspaceAuth(w, req, chi.URLParam(req, "id")) == nil {
		return
	}
	var body struct {
		Name      string `json:"name"`
		Type      string `json:"type"`
		Topic     string `json:"topic"`
		Category  string `json:"category"`
		IsPrivate bool   `json:"is_private"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.Name == "" {
		writeError(w, http.StatusBadRequest, "name required")
		return
	}
	ch := &proto.Channel{
		WorkspaceID: chi.URLParam(req, "id"),
		Name:        body.Name,
		Type:        proto.ChannelType(body.Type),
		Topic:       body.Topic,
		Category:    body.Category,
		IsPrivate:   body.IsPrivate,
	}
	if ch.Type == "" {
		ch.Type = proto.ChannelPublic
	}
	if ch.Type != proto.ChannelPublic && ch.Type != proto.ChannelDM && ch.Type != proto.ChannelGroupDM {
		writeError(w, http.StatusBadRequest, "invalid channel type (must be channel, dm, or group_dm)")
		return
	}
	if err := r.services.CreateChannel(req.Context(), ch); err != nil {
		serverError(w, err, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, ch)
}

func (r *Router) handleListChannels(w http.ResponseWriter, req *http.Request) {
	if requireWorkspaceAuth(w, req, chi.URLParam(req, "id")) == nil {
		return
	}
	workspaceID := chi.URLParam(req, "id")
	limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(req.URL.Query().Get("offset"))
	var channels []*proto.Channel
	var err error
	if limit > 0 {
		channels, err = r.services.ListChannelsPaginated(req.Context(), workspaceID, limit, offset)
	} else {
		channels, err = r.services.ListChannels(req.Context(), workspaceID)
	}
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, channels)
}

func (r *Router) handleGetChannel(w http.ResponseWriter, req *http.Request) {
	caller := memberFromContext(req)
	if caller == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	ch, err := r.services.GetChannel(req.Context(), chi.URLParam(req, "channelID"))
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	if ch == nil || ch.WorkspaceID != caller.WorkspaceID {
		writeError(w, http.StatusNotFound, "channel not found")
		return
	}
	writeJSON(w, http.StatusOK, ch)
}

func (r *Router) handleUpdateChannel(w http.ResponseWriter, req *http.Request) {
	caller := memberFromContext(req)
	if caller == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	existing, err := r.services.GetChannel(req.Context(), chi.URLParam(req, "channelID"))
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	if existing == nil || existing.WorkspaceID != caller.WorkspaceID {
		writeError(w, http.StatusNotFound, "channel not found")
		return
	}
	id := existing.ID
	wsID := existing.WorkspaceID
	chType := existing.Type
	if !decodeAndProtect(w, req, existing, func() {
		existing.ID = id
		existing.WorkspaceID = wsID
		existing.Type = chType
	}) {
		return
	}
	if err := r.services.UpdateChannel(req.Context(), existing); err != nil {
		serverError(w, err, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, existing)
}

func (r *Router) handleDeleteChannel(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	channelID := chi.URLParam(req, "channelID")
	ch, err := r.services.GetChannel(req.Context(), channelID)
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	if ch == nil || ch.WorkspaceID != member.WorkspaceID {
		writeError(w, http.StatusNotFound, "channel not found")
		return
	}
	if err := r.services.DeleteChannel(req.Context(), channelID); err != nil {
		serverError(w, err, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (r *Router) handleAddChannelMember(w http.ResponseWriter, req *http.Request) {
	if requireWorkspaceAuth(w, req, chi.URLParam(req, "id")) == nil {
		return
	}
	var body struct {
		MemberID string `json:"member_id"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := r.services.AddChannelMember(req.Context(), chi.URLParam(req, "channelID"), body.MemberID); err != nil {
		serverError(w, err, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (r *Router) handleRemoveChannelMember(w http.ResponseWriter, req *http.Request) {
	if requireWorkspaceAuth(w, req, chi.URLParam(req, "id")) == nil {
		return
	}
	channelID := chi.URLParam(req, "channelID")
	memberID := chi.URLParam(req, "memberID")
	if err := r.services.RemoveChannelMember(req.Context(), channelID, memberID); err != nil {
		serverError(w, err, "internal error")
		return
	}
	// Unsubscribe from WebSocket channel if connected
	if c := r.hub.GetConn(memberID); c != nil {
		c.Unsubscribe(channelID)
	}
	r.hub.UnsubscribeChannel(channelID, memberID)
	w.WriteHeader(http.StatusNoContent)
}

func (r *Router) handleSearchChannels(w http.ResponseWriter, req *http.Request) {
	if requireWorkspaceAuth(w, req, chi.URLParam(req, "id")) == nil {
		return
	}
	q := req.URL.Query().Get("q")
	if q == "" {
		writeError(w, http.StatusBadRequest, "q is required")
		return
	}
	limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))
	channels, err := r.services.SearchChannels(req.Context(), chi.URLParam(req, "id"), q, limit)
	if err != nil {
		serverError(w, err, "search channels failed")
		return
	}
	writeJSON(w, http.StatusOK, channels)
}

// --- Channel archive ---

func (r *Router) handleArchiveChannel(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	ch, err := r.services.GetChannel(req.Context(), chi.URLParam(req, "channelID"))
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	if ch == nil || ch.WorkspaceID != member.WorkspaceID {
		writeError(w, http.StatusNotFound, "channel not found")
		return
	}
	ch.IsArchived = true
	if err := r.services.UpdateChannel(req.Context(), ch); err != nil {
		serverError(w, err, "archive failed")
		return
	}
	writeJSON(w, http.StatusOK, ch)
}

func (r *Router) handleUnarchiveChannel(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	ch, err := r.services.GetChannel(req.Context(), chi.URLParam(req, "channelID"))
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	if ch == nil || ch.WorkspaceID != member.WorkspaceID {
		writeError(w, http.StatusNotFound, "channel not found")
		return
	}
	ch.IsArchived = false
	if err := r.services.UpdateChannel(req.Context(), ch); err != nil {
		serverError(w, err, "unarchive failed")
		return
	}
	writeJSON(w, http.StatusOK, ch)
}
