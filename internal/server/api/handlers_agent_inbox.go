package api

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"lark-daemon/internal/server/store"
)

func (r *Router) handleListAgentInbox(w http.ResponseWriter, req *http.Request) {
	agentID := chi.URLParam(req, "id")
	opts := store.InboxOptions{
		UnreadOnly: req.URL.Query().Get("unread_only") == "true",
		SourceType: req.URL.Query().Get("source_type"),
		Priority:   req.URL.Query().Get("priority"),
	}
	if v := req.URL.Query().Get("since"); v != "" {
		opts.Since, _ = strconv.ParseInt(v, 10, 64)
	}
	if v := req.URL.Query().Get("limit"); v != "" {
		opts.Limit, _ = strconv.Atoi(v)
	}
	items, err := r.services.ListAgentInbox(req.Context(), agentID, opts)
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, ErrCodeInternal, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (r *Router) handleCountAgentInbox(w http.ResponseWriter, req *http.Request) {
	agentID := chi.URLParam(req, "id")
	count, err := r.services.CountAgentInbox(req.Context(), agentID)
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, ErrCodeInternal, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"count": count})
}

func (r *Router) handleAckInboxItem(w http.ResponseWriter, req *http.Request) {
	itemID := chi.URLParam(req, "itemID")
	if err := r.services.AckInboxItem(req.Context(), itemID); err != nil {
		writeErrorCode(w, http.StatusInternalServerError, ErrCodeInternal, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (r *Router) handleAckAllInbox(w http.ResponseWriter, req *http.Request) {
	agentID := chi.URLParam(req, "id")
	if err := r.services.AckAllInbox(req.Context(), agentID); err != nil {
		writeErrorCode(w, http.StatusInternalServerError, ErrCodeInternal, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
