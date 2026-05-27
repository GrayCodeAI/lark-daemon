package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"lark-daemon/internal/proto"
)

// --- Pin handlers ---

func (r *Router) handlePinMessage(w http.ResponseWriter, req *http.Request) {
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
	var body struct {
		MessageID string `json:"message_id"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.MessageID == "" {
		writeError(w, http.StatusBadRequest, "message_id required")
		return
	}
	p := &proto.Pin{
		MessageID: body.MessageID,
		ChannelID: channelID,
		PinnedBy:  member.ID,
	}
	if err := r.services.CreatePin(req.Context(), p); err != nil {
		serverError(w, err, "internal error")
		return
	}
	r.broadcastPinEvent(channelID, p)
	writeJSON(w, http.StatusCreated, p)
}

func (r *Router) handleListPins(w http.ResponseWriter, req *http.Request) {
	caller := memberFromContext(req)
	if caller == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	isMember, err := r.store.IsChannelMember(req.Context(), chi.URLParam(req, "id"), caller.ID)
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	if !isMember {
		writeError(w, http.StatusForbidden, "not a member of this channel")
		return
	}
	pins, err := r.services.ListPins(req.Context(), chi.URLParam(req, "id"))
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, pins)
}

func (r *Router) handleUnpinMessage(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	messageID := chi.URLParam(req, "id")
	pins, err := r.services.ListPinsByMessage(req.Context(), messageID)
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	if len(pins) == 0 {
		writeError(w, http.StatusNotFound, "pin not found")
		return
	}
	isMember, err := r.store.IsChannelMember(req.Context(), pins[0].ChannelID, member.ID)
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	if !isMember {
		writeError(w, http.StatusForbidden, "not a member of this channel")
		return
	}
	if err := r.services.DeletePin(req.Context(), messageID); err != nil {
		serverError(w, err, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
