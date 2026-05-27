package api

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

// --- Notifications ---

func (r *Router) handleListNotifications(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	unreadOnly := req.URL.Query().Get("unread") == "true"
	limit := 50
	if v := req.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	notifications, err := r.services.ListNotifications(req.Context(), member.ID, unreadOnly, limit)
	if err != nil {
		serverError(w, err, "list notifications")
		return
	}
	writeJSON(w, http.StatusOK, notifications)
}

func (r *Router) handleMarkNotificationRead(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	id := chi.URLParam(req, "id")
	if err := r.services.MarkNotificationRead(req.Context(), id); err != nil {
		serverError(w, err, "mark notification read")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (r *Router) handleMarkAllNotificationsRead(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	if err := r.services.MarkAllNotificationsRead(req.Context(), member.ID); err != nil {
		serverError(w, err, "mark all notifications read")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (r *Router) handleCountUnreadNotifications(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	count, err := r.services.CountUnreadNotifications(req.Context(), member.ID)
	if err != nil {
		serverError(w, err, "count unread notifications")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"count": count})
}
