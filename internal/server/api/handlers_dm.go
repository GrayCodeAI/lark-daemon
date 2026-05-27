package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"lark-daemon/internal/proto"
)

// --- DM handlers ---

func (r *Router) handleCreateDM(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	workspaceID := chi.URLParam(req, "id")
	var body struct {
		MemberIDs []string `json:"member_ids"`
		Name      string   `json:"name,omitempty"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	// Ensure authenticated member is a participant.
	isParticipant := false
	for _, mid := range body.MemberIDs {
		if mid == member.ID {
			isParticipant = true
			break
		}
	}
	if !isParticipant {
		writeError(w, http.StatusForbidden, "authenticated member must be included in member_ids")
		return
	}
	if len(body.MemberIDs) < 2 {
		writeError(w, http.StatusBadRequest, "at least 2 member_ids required")
		return
	}
	// Fast-path: check if DM already exists.
	existing, _ := r.services.GetDMChannel(req.Context(), workspaceID, body.MemberIDs)
	if existing != nil {
		writeJSON(w, http.StatusOK, existing)
		return
	}
	chType := proto.ChannelDM
	if len(body.MemberIDs) > 2 {
		chType = proto.ChannelGroupDM
	}
	ch := &proto.Channel{
		WorkspaceID: workspaceID,
		Name:        body.Name,
		Type:        chType,
	}
	if err := r.services.CreateDMChannel(req.Context(), ch, body.MemberIDs); err != nil {
		serverError(w, err, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, ch)
}

func (r *Router) handleListDMs(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	memberID := chi.URLParam(req, "id")
	if member.ID != memberID {
		writeError(w, http.StatusForbidden, "cannot list another member's DMs")
		return
	}
	channels, err := r.services.ListDMChannels(req.Context(), memberID)
	if err != nil {
		serverError(w, err, "list DMs failed")
		return
	}
	writeJSON(w, http.StatusOK, channels)
}

// --- Unread handlers ---

func (r *Router) handleGetUnread(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	memberID := chi.URLParam(req, "id")
	if member.ID != memberID {
		writeError(w, http.StatusForbidden, "cannot view another member's unread counts")
		return
	}
	counts, err := r.services.GetUnreadCounts(req.Context(), memberID)
	if err != nil {
		serverError(w, err, "get unread counts failed")
		return
	}
	writeJSON(w, http.StatusOK, counts)
}

func (r *Router) handleMarkRead(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	channelID := chi.URLParam(req, "id")
	if err := r.services.MarkChannelRead(req.Context(), channelID, member.ID); err != nil {
		serverError(w, err, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Notification preferences ---

func (r *Router) handleNotificationPreference(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	var body struct {
		Preference string `json:"preference"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.Preference != "all" && body.Preference != "mentions" && body.Preference != "none" {
		writeError(w, http.StatusBadRequest, "preference must be all, mentions, or none")
		return
	}
	if err := r.store.UpdateNotificationPreference(req.Context(), chi.URLParam(req, "id"), member.ID, body.Preference); err != nil {
		serverError(w, err, "update preference failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Agent discovery ---

