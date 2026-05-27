package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"lark-daemon/internal/proto"
)

// --- Agent Memory ---

func (r *Router) handleSetMemory(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	agentID := chi.URLParam(req, "id")
	if member.ID != agentID {
		writeError(w, http.StatusForbidden, "cannot modify another agent's memory")
		return
	}
	var body struct {
		Namespace string `json:"namespace"`
		Key       string `json:"key"`
		Value     string `json:"value"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.Key == "" {
		writeError(w, http.StatusBadRequest, "key required")
		return
	}
	mem := &proto.AgentMemory{
		AgentID:   agentID,
		Namespace: body.Namespace,
		Key:       body.Key,
		Value:     body.Value,
	}
	if mem.Namespace == "" {
		mem.Namespace = "default"
	}
	if err := r.services.SetMemory(req.Context(), mem); err != nil {
		serverError(w, err, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, mem)
}

func (r *Router) handleListMemory(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	agentID := chi.URLParam(req, "id")
	if member.ID != agentID {
		writeError(w, http.StatusForbidden, "cannot read another agent's memory")
		return
	}
	namespace := req.URL.Query().Get("namespace")
	if namespace == "" {
		namespace = "default"
	}
	memories, err := r.services.ListMemory(req.Context(), chi.URLParam(req, "id"), namespace)
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, memories)
}

func (r *Router) handleDeleteMemory(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	agentID := chi.URLParam(req, "id")
	if member.ID != agentID {
		writeError(w, http.StatusForbidden, "cannot delete another agent's memory")
		return
	}
	if err := r.services.DeleteMemory(req.Context(), agentID, chi.URLParam(req, "namespace"), chi.URLParam(req, "key")); err != nil {
		serverError(w, err, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (r *Router) handleGetAgentMetrics(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	agentID := chi.URLParam(req, "id")
	// Verify the agent exists in the same workspace
	agent, err := r.services.GetMember(req.Context(), agentID)
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	if agent == nil || agent.WorkspaceID != member.WorkspaceID {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	// Get metrics from agent_memory with namespace "_metrics"
	memories, err := r.services.ListMemory(req.Context(), agentID, "_metrics")
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	metrics := make(map[string]interface{})
	for _, m := range memories {
		metrics[m.Key] = m.Value
	}
	// Get task counts via efficient store method
	completed, pending, terr := r.store.CountTasksByAssignee(req.Context(), agentID)
	if terr == nil {
		metrics["tasks_assigned"] = completed + pending
		metrics["tasks_completed"] = completed
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"agent_id": agentID,
		"metrics":  metrics,
	})
}


func (r *Router) handleListAgents(w http.ResponseWriter, req *http.Request) {
	if requireWorkspaceAuth(w, req, chi.URLParam(req, "id")) == nil {
		return
	}
	members, err := r.services.ListMembers(req.Context(), chi.URLParam(req, "id"))
	if err != nil {
		serverError(w, err, "list agents failed")
		return
	}
	agents := make([]map[string]any, 0)
	for _, m := range members {
		if m.Type == proto.MemberAgent {
			agents = append(agents, map[string]any{
				"id":       m.ID,
				"name":     m.Name,
				"status":   m.Status,
				"online":   r.hub.GetAgent(m.ID) != nil,
			})
		}
	}
	writeJSON(w, http.StatusOK, agents)
}

// --- Profile handlers ---

