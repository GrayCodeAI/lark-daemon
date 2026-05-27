package api

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

// --- Call history ---

func (r *Router) handleListCalls(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	limit := 20
	if v := req.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	calls, err := r.services.ListCalls(req.Context(), member.ID, limit)
	if err != nil {
		serverError(w, err, "list calls")
		return
	}
	writeJSON(w, http.StatusOK, calls)
}

func (r *Router) handleGetCall(w http.ResponseWriter, req *http.Request) {
	id := chi.URLParam(req, "id")
	call, err := r.services.GetCall(req.Context(), id)
	if err != nil {
		serverError(w, err, "get call")
		return
	}
	if call == nil {
		writeError(w, http.StatusNotFound, "call not found")
		return
	}
	writeJSON(w, http.StatusOK, call)
}
