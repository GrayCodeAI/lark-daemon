package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"lark-daemon/internal/proto"
)

func (r *Router) handleListTemplates(w http.ResponseWriter, req *http.Request) {
	wsID := chi.URLParam(req, "id")
	templates, err := r.services.ListTeamTemplates(req.Context(), wsID)
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, ErrCodeInternal, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, templates)
}

func (r *Router) handleCreateTemplate(w http.ResponseWriter, req *http.Request) {
	wsID := chi.URLParam(req, "id")
	member := requireWorkspaceAuth(w, req, wsID)
	if member == nil {
		return
	}
	var body proto.TeamTemplate
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeErrorCode(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid request body")
		return
	}
	if body.Name == "" {
		writeErrorCode(w, http.StatusBadRequest, ErrCodeBadRequest, "name is required")
		return
	}
	body.WorkspaceID = wsID
	body.CreatedBy = member.ID
	if err := r.services.CreateTeamTemplate(req.Context(), &body); err != nil {
		writeErrorCode(w, http.StatusInternalServerError, ErrCodeInternal, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, body)
}

func (r *Router) handleGetTemplate(w http.ResponseWriter, req *http.Request) {
	templateID := chi.URLParam(req, "id")
	t, err := r.services.GetTeamTemplate(req.Context(), templateID)
	if err != nil || t == nil {
		writeErrorCode(w, http.StatusNotFound, ErrCodeNotFound, "template not found")
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (r *Router) handleDeleteTemplate(w http.ResponseWriter, req *http.Request) {
	templateID := chi.URLParam(req, "id")
	if err := r.services.DeleteTeamTemplate(req.Context(), templateID); err != nil {
		writeErrorCode(w, http.StatusInternalServerError, ErrCodeInternal, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (r *Router) handleInstantiateTemplate(w http.ResponseWriter, req *http.Request) {
	templateID := chi.URLParam(req, "id")
	t, err := r.services.GetTeamTemplate(req.Context(), templateID)
	if err != nil || t == nil {
		writeErrorCode(w, http.StatusNotFound, ErrCodeNotFound, "template not found")
		return
	}
	var body struct {
		ChannelPrefix string `json:"channel_prefix,omitempty"`
	}
	json.NewDecoder(req.Body).Decode(&body)

	var roles []proto.TemplateRole
	if err := json.Unmarshal(t.Roles, &roles); err != nil {
		writeErrorCode(w, http.StatusInternalServerError, ErrCodeInternal, "invalid template roles")
		return
	}
	var channels []proto.TemplateChannel
	if t.Channels != nil {
		json.Unmarshal(t.Channels, &channels)
	}

	// Determine workspace from template or auth
	wsID := t.WorkspaceID
	if wsID == "" {
		member := memberFromContext(req)
		if member == nil {
			writeErrorCode(w, http.StatusUnauthorized, ErrCodeAuthRequired, "not authenticated")
			return
		}
		wsID = member.WorkspaceID
	}

	// Create agents for each role
	agentMap := make(map[string]string) // role name -> agent ID
	createdAgents := make([]string, 0, len(roles))
	for _, role := range roles {
		agent := &proto.Member{
			WorkspaceID: wsID,
			Name:        role.Name,
			Type:        proto.MemberAgent,
			Role:        proto.RoleUser,
			RoleCard: &proto.RoleCard{
				SystemPrompt: role.SystemPrompt,
				Capabilities: role.Capabilities,
			},
		}
		if err := r.services.CreateMember(req.Context(), agent); err != nil {
			writeErrorCode(w, http.StatusInternalServerError, ErrCodeInternal, "failed to create agent: "+role.Name)
			return
		}
		agentMap[role.Name] = agent.ID
		createdAgents = append(createdAgents, agent.ID)
	}

	// Create channels and add agents
	createdChannels := make([]string, 0, len(channels))
	for _, chDef := range channels {
		chName := chDef.Name
		if body.ChannelPrefix != "" {
			chName = body.ChannelPrefix + "-" + chName
		}
		ch := &proto.Channel{
			WorkspaceID: wsID,
			Name:        chName,
			Type:        proto.ChannelPublic,
			Topic:       chDef.Topic,
			IsPrivate:   chDef.IsPrivate,
		}
		if err := r.services.CreateChannel(req.Context(), ch); err != nil {
			writeErrorCode(w, http.StatusInternalServerError, ErrCodeInternal, "failed to create channel: "+chName)
			return
		}
		createdChannels = append(createdChannels, ch.ID)
		// Add agent members to channel
		for _, roleName := range chDef.Members {
			if agentID, ok := agentMap[roleName]; ok {
				r.services.AddChannelMember(req.Context(), ch.ID, agentID)
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"template_id": templateID,
		"agents":      createdAgents,
		"channels":    createdChannels,
	})
}
