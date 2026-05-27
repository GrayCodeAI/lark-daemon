package api

import (
	"crypto/subtle"
	"net/http"
	"path/filepath"
	"strconv"

	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/bcrypt"

	"lark-daemon/internal/proto"
)

func (r *Router) handleCreateMember(w http.ResponseWriter, req *http.Request) {
	if requireWorkspaceAuth(w, req, chi.URLParam(req, "id")) == nil {
		return
	}
	var body struct {
		Name      string `json:"name"`
		Type      string `json:"type"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.Name == "" {
		writeError(w, http.StatusBadRequest, "name required")
		return
	}
	if proto.MemberType(body.Type) != proto.MemberHuman && proto.MemberType(body.Type) != proto.MemberAgent {
		writeError(w, http.StatusBadRequest, "type must be 'human' or 'agent'")
		return
	}
	m := &proto.Member{
		WorkspaceID: chi.URLParam(req, "id"),
		Name:        body.Name,
		Type:        proto.MemberType(body.Type),
		AvatarURL:   body.AvatarURL,
		Status:      proto.PresenceOffline,
	}
	if err := r.services.CreateMember(req.Context(), m); err != nil {
		serverError(w, err, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, m)
}

func (r *Router) handleListMembers(w http.ResponseWriter, req *http.Request) {
	if requireWorkspaceAuth(w, req, chi.URLParam(req, "id")) == nil {
		return
	}
	workspaceID := chi.URLParam(req, "id")
	limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(req.URL.Query().Get("offset"))
	var members []*proto.Member
	var err error
	if limit > 0 {
		members, err = r.services.ListMembersPaginated(req.Context(), workspaceID, limit, offset)
	} else {
		members, err = r.services.ListMembers(req.Context(), workspaceID)
	}
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	for _, m := range members {
		m.APIKey = ""
	}
	writeJSON(w, http.StatusOK, members)
}

func (r *Router) handleAgentProvision(w http.ResponseWriter, req *http.Request) {
	workspaceID := chi.URLParam(req, "id")
	var body struct {
		Name            string   `json:"name"`
		ProvisionToken  string   `json:"provision_token"`
		SystemPrompt    string   `json:"system_prompt"`
		Capabilities    []string `json:"capabilities"`
		RuntimeType     string   `json:"runtime_type"`
		RuntimeProvider string   `json:"runtime_provider"`
		RuntimeModel    string   `json:"runtime_model"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.Name == "" || body.ProvisionToken == "" {
		writeError(w, http.StatusBadRequest, "name and provision_token required")
		return
	}
	// Validate provision token
	ws, err := r.services.GetWorkspace(req.Context(), workspaceID)
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	if ws == nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	if subtle.ConstantTimeCompare([]byte(ws.AgentProvisionToken), []byte(body.ProvisionToken)) != 1 {
		writeError(w, http.StatusUnauthorized, "invalid provision token")
		return
	}
	// Check if agent already exists
	existing, err2 := r.services.GetMemberByName(req.Context(), workspaceID, body.Name)
	if err2 != nil {
		serverError(w, err2, "agent provision: get member failed")
		return
	}
	if existing != nil {
		// Return existing agent's API key
		writeJSON(w, http.StatusOK, map[string]string{
			"member_id": existing.ID,
			"api_key":   existing.APIKey,
			"name":      existing.Name,
		})
		return
	}
	// Create new agent member
	m := &proto.Member{
		WorkspaceID: workspaceID,
		Name:        body.Name,
		Type:        proto.MemberAgent,
		Status:      proto.PresenceOffline,
		RoleCard: &proto.RoleCard{
			SystemPrompt: body.SystemPrompt,
			Capabilities: body.Capabilities,
		},
		RuntimeInfo: &proto.RuntimeInfo{
			Type:     body.RuntimeType,
			Provider: body.RuntimeProvider,
			Model:    body.RuntimeModel,
		},
	}
	if err := r.services.CreateMember(req.Context(), m); err != nil {
		serverError(w, err, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{
		"member_id": m.ID,
		"api_key":   m.APIKey,
		"name":      m.Name,
	})
}

func (r *Router) handleGetMember(w http.ResponseWriter, req *http.Request) {
	caller := memberFromContext(req)
	if caller == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	member, err := r.services.GetMember(req.Context(), chi.URLParam(req, "memberID"))
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	if member == nil || member.WorkspaceID != caller.WorkspaceID {
		writeError(w, http.StatusNotFound, "member not found")
		return
	}
	member.APIKey = "" // Don't expose API key in GET responses
	writeJSON(w, http.StatusOK, member)
}

func (r *Router) handleUpdateMember(w http.ResponseWriter, req *http.Request) {
	caller := memberFromContext(req)
	if caller == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	existing, err := r.services.GetMember(req.Context(), chi.URLParam(req, "memberID"))
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	if existing == nil || existing.WorkspaceID != caller.WorkspaceID {
		writeError(w, http.StatusNotFound, "member not found")
		return
	}
	id := existing.ID
	wsID := existing.WorkspaceID
	mtype := existing.Type
	apiKey := existing.APIKey
	if !decodeAndProtect(w, req, existing, func() {
		existing.ID = id
		existing.WorkspaceID = wsID
		existing.Type = mtype
		existing.APIKey = apiKey
	}) {
		return
	}
	if err := r.services.UpdateMember(req.Context(), existing); err != nil {
		serverError(w, err, "internal error")
		return
	}
	existing.APIKey = ""
	writeJSON(w, http.StatusOK, existing)
}

func (r *Router) handleDeleteMember(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	targetID := chi.URLParam(req, "memberID")
	target, err := r.services.GetMember(req.Context(), targetID)
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	if target == nil || target.WorkspaceID != member.WorkspaceID {
		writeError(w, http.StatusNotFound, "member not found")
		return
	}
	if err := r.services.DeleteMember(req.Context(), targetID); err != nil {
		serverError(w, err, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Channels ---


func (r *Router) handleMyProfile(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	member.APIKey = ""
	member.PasswordHash = ""
	writeJSON(w, http.StatusOK, member)
}

func (r *Router) handleUploadAvatar(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	if err := req.ParseMultipartForm(5 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	file, header, err := req.FormFile("avatar")
	if err != nil {
		writeError(w, http.StatusBadRequest, "avatar field required")
		return
	}
	defer file.Close()
	ext := filepath.Ext(header.Filename)
	path := "avatars/" + member.ID + ext
	if err := r.storage.Save(req.Context(), path, file); err != nil {
		serverError(w, err, "save avatar failed")
		return
	}
	member.AvatarURL = path
	if err := r.services.UpdateMember(req.Context(), member); err != nil {
		serverError(w, err, "update member failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"avatar_url": path})
}

func (r *Router) handleChangePassword(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	var body struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if member.PasswordHash == "" {
		writeError(w, http.StatusBadRequest, "no password set (use OAuth)")
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(member.PasswordHash), []byte(body.OldPassword)); err != nil {
		writeError(w, http.StatusUnauthorized, "wrong password")
		return
	}
	if len(body.NewPassword) < 6 {
		writeError(w, http.StatusBadRequest, "password must be at least 6 characters")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(body.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		serverError(w, err, "hash failed")
		return
	}
	member.PasswordHash = string(hash)
	if err := r.services.UpdateMember(req.Context(), member); err != nil {
		serverError(w, err, "update failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Channel search ---

