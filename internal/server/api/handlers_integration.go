package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"lark-daemon/internal/proto"
)

// --- Integration marketplace ---

func (r *Router) handleListIntegrations(w http.ResponseWriter, req *http.Request) {
	integrations, err := r.services.ListIntegrations(req.Context())
	if err != nil {
		serverError(w, err, "list integrations")
		return
	}
	writeJSON(w, http.StatusOK, integrations)
}

func (r *Router) handleGetIntegration(w http.ResponseWriter, req *http.Request) {
	id := chi.URLParam(req, "id")
	integration, err := r.services.GetIntegration(req.Context(), id)
	if err != nil {
		serverError(w, err, "get integration")
		return
	}
	if integration == nil {
		writeError(w, http.StatusNotFound, "integration not found")
		return
	}
	writeJSON(w, http.StatusOK, integration)
}

func (r *Router) handleCreateIntegration(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	if member.Role != proto.RoleAdmin && member.Role != proto.RoleOwner {
		writeError(w, http.StatusForbidden, "admin required")
		return
	}
	var body struct {
		Name         string `json:"name"`
		Description  string `json:"description"`
		IconURL      string `json:"icon_url"`
		Type         string `json:"type"`
		ConfigSchema string `json:"config_schema"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.Name == "" || body.Type == "" {
		writeError(w, http.StatusBadRequest, "name and type required")
		return
	}
	i := &proto.Integration{
		Name:         body.Name,
		Description:  body.Description,
		IconURL:      body.IconURL,
		Type:         proto.IntegrationType(body.Type),
		ConfigSchema: []byte(body.ConfigSchema),
	}
	if err := r.services.CreateIntegration(req.Context(), i); err != nil {
		serverError(w, err, "create integration")
		return
	}
	writeJSON(w, http.StatusCreated, i)
}

func (r *Router) handleInstallIntegration(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	workspaceID := chi.URLParam(req, "id")
	var body struct {
		IntegrationID string `json:"integration_id"`
		Config        string `json:"config"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.IntegrationID == "" {
		writeError(w, http.StatusBadRequest, "integration_id required")
		return
	}
	// Verify integration exists
	integration, err := r.services.GetIntegration(req.Context(), body.IntegrationID)
	if err != nil || integration == nil {
		writeError(w, http.StatusNotFound, "integration not found")
		return
	}
	// Check if already installed
	existing, _ := r.services.GetWorkspaceIntegration(req.Context(), workspaceID, body.IntegrationID)
	if existing != nil {
		writeError(w, http.StatusConflict, "integration already installed")
		return
	}
	wi := &proto.WorkspaceIntegration{
		WorkspaceID:   workspaceID,
		IntegrationID: body.IntegrationID,
		InstalledBy:   member.ID,
		Config:        []byte(body.Config),
		Enabled:       true,
	}
	if err := r.services.InstallIntegration(req.Context(), wi); err != nil {
		serverError(w, err, "install integration")
		return
	}
	writeJSON(w, http.StatusCreated, wi)
}

func (r *Router) handleListWorkspaceIntegrations(w http.ResponseWriter, req *http.Request) {
	workspaceID := chi.URLParam(req, "id")
	integrations, err := r.services.ListWorkspaceIntegrations(req.Context(), workspaceID)
	if err != nil {
		serverError(w, err, "list workspace integrations")
		return
	}
	writeJSON(w, http.StatusOK, integrations)
}

func (r *Router) handleUninstallIntegration(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	workspaceID := chi.URLParam(req, "id")
	integrationID := chi.URLParam(req, "integrationId")
	if err := r.services.UninstallIntegration(req.Context(), workspaceID, integrationID); err != nil {
		serverError(w, err, "uninstall integration")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
