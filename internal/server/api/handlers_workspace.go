package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"lark-daemon/internal/proto"
)

// --- Workspaces ---

func (r *Router) handleCreateWorkspace(w http.ResponseWriter, req *http.Request) {
	var body struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.Name == "" {
		writeError(w, http.StatusBadRequest, "name required")
		return
	}
	ws := &proto.Workspace{Name: body.Name, Slug: body.Slug}
	if err := r.services.CreateWorkspace(req.Context(), ws); err != nil {
		serverError(w, err, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, ws)
}

func (r *Router) handleListWorkspaces(w http.ResponseWriter, req *http.Request) {
	wss, err := r.services.ListWorkspaces(req.Context())
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	for _, ws := range wss {
		ws.AgentProvisionToken = ""
	}
	writeJSON(w, http.StatusOK, wss)
}

func (r *Router) handleGetWorkspace(w http.ResponseWriter, req *http.Request) {
	ws, err := r.services.GetWorkspace(req.Context(), chi.URLParam(req, "id"))
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	if ws == nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	ws.AgentProvisionToken = ""
	writeJSON(w, http.StatusOK, ws)
}

func (r *Router) handleUpdateWorkspace(w http.ResponseWriter, req *http.Request) {
	if requireWorkspaceAuth(w, req, chi.URLParam(req, "id")) == nil {
		return
	}
	existing, err := r.services.GetWorkspace(req.Context(), chi.URLParam(req, "id"))
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	id := existing.ID
	token := existing.AgentProvisionToken
	if !decodeAndProtect(w, req, existing, func() {
		existing.ID = id
		existing.AgentProvisionToken = token
	}) {
		return
	}
	if err := r.services.UpdateWorkspace(req.Context(), existing); err != nil {
		serverError(w, err, "internal error")
		return
	}
	existing.AgentProvisionToken = ""
	writeJSON(w, http.StatusOK, existing)
}

func (r *Router) handleDeleteWorkspace(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	wsID := chi.URLParam(req, "id")
	existing, err := r.services.GetWorkspace(req.Context(), wsID)
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	if existing == nil || existing.ID != member.WorkspaceID {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	if member.Role != "admin" && member.Role != "owner" {
		writeError(w, http.StatusForbidden, "admin access required")
		return
	}
	if err := r.services.DeleteWorkspace(req.Context(), wsID); err != nil {
		serverError(w, err, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Members ---

