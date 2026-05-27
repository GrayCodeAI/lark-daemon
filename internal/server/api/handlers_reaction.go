package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"lark-daemon/internal/proto"
	"lark-daemon/internal/server/websocket"
)

// --- Reactions ---

func (r *Router) handleAddReaction(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	var body struct {
		Emoji string `json:"emoji"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	reaction := &proto.Reaction{
		MessageID: chi.URLParam(req, "id"),
		MemberID:  member.ID,
		Emoji:     body.Emoji,
	}
	if err := r.services.AddReaction(req.Context(), reaction); err != nil {
		serverError(w, err, "internal error")
		return
	}
	r.broadcastReactionEvent(reaction.MessageID, reaction)
	writeJSON(w, http.StatusCreated, reaction)
}

func (r *Router) handleListReactions(w http.ResponseWriter, req *http.Request) {
	if memberFromContext(req) == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	reactions, err := r.services.ListReactions(req.Context(), chi.URLParam(req, "id"))
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, reactions)
}

func (r *Router) handleRemoveReaction(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	msgID := chi.URLParam(req, "id")
	if err := r.services.RemoveReaction(req.Context(), msgID, member.ID, chi.URLParam(req, "emoji")); err != nil {
		serverError(w, err, "internal error")
		return
	}
	// Broadcast reaction removal
	msg, err := r.services.GetMessage(req.Context(), msgID)
	if err == nil && msg != nil {
		r.hub.BroadcastToChannel(msg.ChannelID, websocket.NewEnvelope("reaction.remove", map[string]string{
			"message_id": msgID,
			"member_id":  member.ID,
			"emoji":      chi.URLParam(req, "emoji"),
		}))
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Search ---

