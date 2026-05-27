package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"lark-daemon/internal/proto"
)

func (r *Router) handleCreateTask(w http.ResponseWriter, req *http.Request) {
	member := requireWorkspaceAuth(w, req, chi.URLParam(req, "id"))
	if member == nil {
		return
	}
	var body struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		AssignedTo  string `json:"assigned_to"`
		ChannelID   string `json:"channel_id"`
		Priority    string `json:"priority"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.Title == "" {
		writeError(w, http.StatusBadRequest, "title required")
		return
	}
	task := &proto.Task{
		WorkspaceID: chi.URLParam(req, "id"),
		Title:       body.Title,
		Description: body.Description,
		AssignedTo:  body.AssignedTo,
		CreatedBy:   member.ID,
		ChannelID:   body.ChannelID,
		Priority:    body.Priority,
	}
	if task.Priority == "" {
		task.Priority = proto.TaskPriorityMedium
	}
	if task.Priority != proto.TaskPriorityLow && task.Priority != proto.TaskPriorityMedium &&
		task.Priority != proto.TaskPriorityHigh && task.Priority != proto.TaskPriorityUrgent {
		writeError(w, http.StatusBadRequest, "invalid priority (must be low, medium, high, or urgent)")
		return
	}
	if err := r.services.CreateTask(req.Context(), task); err != nil {
		serverError(w, err, "internal error")
		return
	}
	r.broadcastTaskEvent(task)
	r.notifyAssignee(task)
	writeJSON(w, http.StatusCreated, task)
}

func (r *Router) handleListTasks(w http.ResponseWriter, req *http.Request) {
	if requireWorkspaceAuth(w, req, chi.URLParam(req, "id")) == nil {
		return
	}
	status := proto.TaskStatus(req.URL.Query().Get("status"))
	tasks, err := r.services.ListTasks(req.Context(), chi.URLParam(req, "id"), status)
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, tasks)
}

func (r *Router) handleUpdateTask(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	existing, err := r.services.GetTask(req.Context(), chi.URLParam(req, "id"))
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	if existing == nil || existing.WorkspaceID != member.WorkspaceID {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	id := existing.ID
	wsID := existing.WorkspaceID
	createdBy := existing.CreatedBy
	createdAt := existing.CreatedAt
	if !decodeAndProtect(w, req, existing, func() {
		existing.ID = id
		existing.WorkspaceID = wsID
		existing.CreatedBy = createdBy
		existing.CreatedAt = createdAt
	}) {
		return
	}
	if err := r.services.UpdateTask(req.Context(), existing); err != nil {
		serverError(w, err, "internal error")
		return
	}
	r.broadcastTaskEvent(existing)
	r.notifyAssignee(existing)
	writeJSON(w, http.StatusOK, existing)
}

func (r *Router) handleDeleteTask(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	task, err := r.services.GetTask(req.Context(), chi.URLParam(req, "id"))
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	if task == nil || task.WorkspaceID != member.WorkspaceID {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	if err := r.services.DeleteTask(req.Context(), task.ID); err != nil {
		serverError(w, err, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
