package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"lark-daemon/internal/proto"
)

func (r *Router) handleCreateWorkspaceItem(w http.ResponseWriter, req *http.Request) {
	agentID := chi.URLParam(req, "id")
	var body proto.AgentWorkspaceItem
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeErrorCode(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid request body")
		return
	}
	if body.Name == "" {
		writeErrorCode(w, http.StatusBadRequest, ErrCodeBadRequest, "name is required")
		return
	}
	body.AgentID = agentID
	if body.Namespace == "" {
		body.Namespace = "default"
	}
	if body.MimeType == "" {
		body.MimeType = "text/plain"
	}
	// Get workspace_id from agent's membership
	member, err := r.services.GetMember(req.Context(), agentID)
	if err != nil || member == nil {
		writeErrorCode(w, http.StatusNotFound, ErrCodeNotFound, "agent not found")
		return
	}
	body.WorkspaceID = member.WorkspaceID
	if err := r.services.CreateWorkspaceItem(req.Context(), &body); err != nil {
		writeErrorCode(w, http.StatusInternalServerError, ErrCodeInternal, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, body)
}

func (r *Router) handleListWorkspaceItems(w http.ResponseWriter, req *http.Request) {
	agentID := chi.URLParam(req, "id")
	namespace := req.URL.Query().Get("namespace")
	items, err := r.services.ListWorkspaceItems(req.Context(), agentID, namespace)
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, ErrCodeInternal, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (r *Router) handleGetWorkspaceItem(w http.ResponseWriter, req *http.Request) {
	itemID := chi.URLParam(req, "itemID")
	item, err := r.services.GetWorkspaceItem(req.Context(), itemID)
	if err != nil || item == nil {
		writeErrorCode(w, http.StatusNotFound, ErrCodeNotFound, "item not found")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (r *Router) handleUpdateWorkspaceItem(w http.ResponseWriter, req *http.Request) {
	itemID := chi.URLParam(req, "itemID")
	existing, err := r.services.GetWorkspaceItem(req.Context(), itemID)
	if err != nil || existing == nil {
		writeErrorCode(w, http.StatusNotFound, ErrCodeNotFound, "item not found")
		return
	}
	var body proto.AgentWorkspaceItem
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeErrorCode(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid request body")
		return
	}
	body.ID = itemID
	body.AgentID = existing.AgentID
	body.WorkspaceID = existing.WorkspaceID
	if body.Namespace == "" {
		body.Namespace = existing.Namespace
	}
	if err := r.services.UpdateWorkspaceItem(req.Context(), &body); err != nil {
		writeErrorCode(w, http.StatusInternalServerError, ErrCodeInternal, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, body)
}

func (r *Router) handleDeleteWorkspaceItem(w http.ResponseWriter, req *http.Request) {
	itemID := chi.URLParam(req, "itemID")
	if err := r.services.DeleteWorkspaceItem(req.Context(), itemID); err != nil {
		writeErrorCode(w, http.StatusInternalServerError, ErrCodeInternal, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (r *Router) handleSearchWorkspaceItems(w http.ResponseWriter, req *http.Request) {
	agentID := chi.URLParam(req, "id")
	query := req.URL.Query().Get("q")
	if query == "" {
		writeErrorCode(w, http.StatusBadRequest, ErrCodeBadRequest, "q parameter is required")
		return
	}
	items, err := r.services.SearchWorkspaceItems(req.Context(), agentID, query)
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, ErrCodeInternal, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, items)
}
