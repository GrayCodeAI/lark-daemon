package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"lark-daemon/internal/proto"
)

// --- Workflow CRUD ---

func (r *Router) handleCreateWorkflow(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	workspaceID := chi.URLParam(req, "id")
	var body struct {
		Name         string          `json:"name"`
		Description  string          `json:"description"`
		TriggerType  string          `json:"trigger_type"`
		TriggerConfig json.RawMessage `json:"trigger_config"`
		Steps        json.RawMessage `json:"steps"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.Name == "" || body.TriggerType == "" || body.Steps == nil {
		writeError(w, http.StatusBadRequest, "name, trigger_type, and steps required")
		return
	}
	// Validate steps is valid JSON array
	var steps []any
	if err := json.Unmarshal(body.Steps, &steps); err != nil {
		writeError(w, http.StatusBadRequest, "steps must be a JSON array")
		return
	}
	wf := &proto.Workflow{
		WorkspaceID:   workspaceID,
		Name:          body.Name,
		Description:   body.Description,
		TriggerType:   body.TriggerType,
		TriggerConfig: body.TriggerConfig,
		Steps:         body.Steps,
		Enabled:       true,
		CreatedBy:     member.ID,
	}
	if err := r.services.CreateWorkflow(req.Context(), wf); err != nil {
		serverError(w, err, "create workflow")
		return
	}
	writeJSON(w, http.StatusCreated, wf)
}

func (r *Router) handleListWorkflows(w http.ResponseWriter, req *http.Request) {
	workspaceID := chi.URLParam(req, "id")
	workflows, err := r.services.ListWorkflows(req.Context(), workspaceID)
	if err != nil {
		serverError(w, err, "list workflows")
		return
	}
	writeJSON(w, http.StatusOK, workflows)
}

func (r *Router) handleGetWorkflow(w http.ResponseWriter, req *http.Request) {
	id := chi.URLParam(req, "id")
	wf, err := r.services.GetWorkflow(req.Context(), id)
	if err != nil {
		serverError(w, err, "get workflow")
		return
	}
	if wf == nil {
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	}
	writeJSON(w, http.StatusOK, wf)
}

func (r *Router) handleUpdateWorkflow(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	id := chi.URLParam(req, "id")
	existing, err := r.services.GetWorkflow(req.Context(), id)
	if err != nil {
		serverError(w, err, "get workflow")
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	}
	var body struct {
		Name         string          `json:"name"`
		Description  string          `json:"description"`
		TriggerType  string          `json:"trigger_type"`
		TriggerConfig json.RawMessage `json:"trigger_config"`
		Steps        json.RawMessage `json:"steps"`
		Enabled      *bool           `json:"enabled"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.Name != "" {
		existing.Name = body.Name
	}
	if body.Description != "" {
		existing.Description = body.Description
	}
	if body.TriggerType != "" {
		existing.TriggerType = body.TriggerType
	}
	if body.TriggerConfig != nil {
		existing.TriggerConfig = body.TriggerConfig
	}
	if body.Steps != nil {
		existing.Steps = body.Steps
	}
	if body.Enabled != nil {
		existing.Enabled = *body.Enabled
	}
	if err := r.services.UpdateWorkflow(req.Context(), existing); err != nil {
		serverError(w, err, "update workflow")
		return
	}
	writeJSON(w, http.StatusOK, existing)
}

func (r *Router) handleDeleteWorkflow(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	id := chi.URLParam(req, "id")
	if err := r.services.DeleteWorkflow(req.Context(), id); err != nil {
		serverError(w, err, "delete workflow")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Workflow runs ---

func (r *Router) handleListWorkflowRuns(w http.ResponseWriter, req *http.Request) {
	id := chi.URLParam(req, "id")
	limit := 20
	if v := req.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	runs, err := r.services.ListWorkflowRuns(req.Context(), id, limit)
	if err != nil {
		serverError(w, err, "list workflow runs")
		return
	}
	writeJSON(w, http.StatusOK, runs)
}

func (r *Router) handleGetWorkflowRun(w http.ResponseWriter, req *http.Request) {
	id := chi.URLParam(req, "id")
	run, err := r.services.GetWorkflowRun(req.Context(), id)
	if err != nil {
		serverError(w, err, "get workflow run")
		return
	}
	if run == nil {
		writeError(w, http.StatusNotFound, "workflow run not found")
		return
	}
	writeJSON(w, http.StatusOK, run)
}

// handleTriggerWorkflow manually triggers a workflow execution.
func (r *Router) handleTriggerWorkflow(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	id := chi.URLParam(req, "id")
	wf, err := r.services.GetWorkflow(req.Context(), id)
	if err != nil {
		serverError(w, err, "get workflow")
		return
	}
	if wf == nil {
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	}
	if !wf.Enabled {
		writeError(w, http.StatusBadRequest, "workflow is disabled")
		return
	}
	var triggerData json.RawMessage
	if req.ContentLength > 0 {
		var body map[string]any
		if err := decodeJSON(req, &body); err == nil {
			triggerData, _ = json.Marshal(body)
		}
	}
	run := &proto.WorkflowRun{
		WorkflowID:  wf.ID,
		Status:      proto.WfRunRunning,
		TriggerData: triggerData,
	}
	if err := r.services.CreateWorkflowRun(req.Context(), run); err != nil {
		serverError(w, err, "create workflow run")
		return
	}
	// Execute workflow steps asynchronously
	go r.executeWorkflow(run, wf)
	r.recordUsage(wf.WorkspaceID, "workflows", 1)
	writeJSON(w, http.StatusAccepted, run)
}

// workflowStep defines a step in a workflow.
type workflowStep struct {
	Type           string          `json:"type"`
	Config         json.RawMessage `json:"config"`
	ContinueOnError bool           `json:"continue_on_error,omitempty"`
}

// executeWorkflow runs the workflow steps with panic recovery and context timeout.
func (r *Router) executeWorkflow(run *proto.WorkflowRun, wf *proto.Workflow) {
	// Panic recovery
	defer func() {
		if rec := recover(); rec != nil {
			run.Status = proto.WfRunFailed
			run.Error = fmt.Sprintf("panic: %v", rec)
			run.FinishedAt = time.Now().UnixMilli()
			_ = r.services.UpdateWorkflowRun(context.Background(), run)
		}
	}()

	// Context with 5-minute timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	var steps []workflowStep
	if err := json.Unmarshal(wf.Steps, &steps); err != nil {
		run.Status = proto.WfRunFailed
		run.Error = "invalid steps: " + err.Error()
		run.FinishedAt = time.Now().UnixMilli()
		_ = r.services.UpdateWorkflowRun(ctx, run)
		return
	}

	results := make([]map[string]any, 0, len(steps))
	prevOutput := map[string]any{} // output from previous step

	for i, step := range steps {
		// Check context cancellation
		if ctx.Err() != nil {
			results = append(results, map[string]any{"step": i, "type": step.Type, "status": "skipped", "error": "timeout"})
			break
		}

		result := map[string]any{"step": i, "type": step.Type, "status": "completed"}
		// Pass previous step output for chaining
		if len(prevOutput) > 0 {
			result["prev_output"] = prevOutput
		}

		output := r.executeStep(ctx, wf, run, step, result, prevOutput)
		prevOutput = output
		results = append(results, result)

		if result["status"] == "failed" && !step.ContinueOnError {
			break
		}
	}

	resultJSON, _ := json.Marshal(results)
	run.Result = resultJSON
	run.FinishedAt = time.Now().UnixMilli()
	failed := false
	for _, res := range results {
		if res["status"] == "failed" {
			failed = true
			break
		}
	}
	if failed {
		run.Status = proto.WfRunFailed
		run.Error = "one or more steps failed"
	} else {
		run.Status = proto.WfRunCompleted
	}
	_ = r.services.UpdateWorkflowRun(ctx, run)
}

// executeStep executes a single workflow step and returns its output.
func (r *Router) executeStep(ctx context.Context, wf *proto.Workflow, run *proto.WorkflowRun, step workflowStep, result map[string]any, prevOutput map[string]any) map[string]any {
	output := map[string]any{}

	switch step.Type {
	case "send_message":
		var cfg struct {
			ChannelID string `json:"channel_id"`
			Content   string `json:"content"`
		}
		if err := json.Unmarshal(step.Config, &cfg); err != nil {
			result["status"] = "failed"
			result["error"] = err.Error()
		} else {
			msg := &proto.Message{
				ChannelID:   cfg.ChannelID,
				SenderID:    wf.CreatedBy,
				Content:     cfg.Content,
				ContentType: "text",
			}
			if err := r.services.CreateMessage(ctx, msg); err != nil {
				result["status"] = "failed"
				result["error"] = err.Error()
			} else {
				r.hub.SendNewMessage(cfg.ChannelID, msg)
				result["message_id"] = msg.ID
				output["message_id"] = msg.ID
			}
		}

	case "send_dm":
		var cfg struct {
			MemberID string `json:"member_id"`
			Content  string `json:"content"`
		}
		if err := json.Unmarshal(step.Config, &cfg); err != nil {
			result["status"] = "failed"
			result["error"] = err.Error()
		} else {
			// Get or create DM channel
			memberIDs := []string{wf.CreatedBy, cfg.MemberID}
			dm, err := r.services.GetDMChannel(ctx, wf.WorkspaceID, memberIDs)
			if err != nil {
				result["status"] = "failed"
				result["error"] = err.Error()
			} else if dm == nil {
				dm = &proto.Channel{
					WorkspaceID: wf.WorkspaceID,
					Name:        "dm",
					Type:        proto.ChannelDM,
				}
				if err := r.services.CreateDMChannel(ctx, dm, memberIDs); err != nil {
					result["status"] = "failed"
					result["error"] = err.Error()
					break
				}
			}
			if dm != nil {
				msg := &proto.Message{
					ChannelID:   dm.ID,
					SenderID:    wf.CreatedBy,
					Content:     cfg.Content,
					ContentType: "text",
				}
				if err := r.services.CreateMessage(ctx, msg); err != nil {
					result["status"] = "failed"
					result["error"] = err.Error()
				} else {
					r.hub.SendNewMessage(dm.ID, msg)
					result["message_id"] = msg.ID
					output["message_id"] = msg.ID
				}
			}
		}

	case "create_channel":
		var cfg struct {
			Name string `json:"name"`
			Type string `json:"type"`
		}
		if err := json.Unmarshal(step.Config, &cfg); err != nil {
			result["status"] = "failed"
			result["error"] = err.Error()
		} else {
			if cfg.Type == "" {
				cfg.Type = "public"
			}
			ch := &proto.Channel{
				WorkspaceID: wf.WorkspaceID,
				Name:        cfg.Name,
				Type:        proto.ChannelType(cfg.Type),
			}
			if err := r.services.CreateChannel(ctx, ch); err != nil {
				result["status"] = "failed"
				result["error"] = err.Error()
			} else {
				result["channel_id"] = ch.ID
				output["channel_id"] = ch.ID
			}
		}

	case "create_task":
		var cfg struct {
			Title       string `json:"title"`
			Description string `json:"description"`
			ChannelID   string `json:"channel_id"`
			AssignedTo  string `json:"assigned_to"`
		}
		if err := json.Unmarshal(step.Config, &cfg); err != nil {
			result["status"] = "failed"
			result["error"] = err.Error()
		} else {
			task := &proto.Task{
				WorkspaceID: wf.WorkspaceID,
				ChannelID:   cfg.ChannelID,
				AssignedTo:  cfg.AssignedTo,
				CreatedBy:   wf.CreatedBy,
				Title:       cfg.Title,
				Description: cfg.Description,
				Status:      proto.TaskTodo,
				Priority:    "medium",
			}
			if err := r.services.CreateTask(ctx, task); err != nil {
				result["status"] = "failed"
				result["error"] = err.Error()
			} else {
				result["task_id"] = task.ID
				output["task_id"] = task.ID
			}
		}

	case "update_task":
		var cfg struct {
			TaskID string `json:"task_id"`
			Status string `json:"status"`
			Title  string `json:"title"`
		}
		if err := json.Unmarshal(step.Config, &cfg); err != nil {
			result["status"] = "failed"
			result["error"] = err.Error()
		} else {
			task, err := r.services.GetTask(ctx, cfg.TaskID)
			if err != nil || task == nil {
				result["status"] = "failed"
				result["error"] = "task not found"
			} else {
				if cfg.Status != "" {
					task.Status = proto.TaskStatus(cfg.Status)
				}
				if cfg.Title != "" {
					task.Title = cfg.Title
				}
				if err := r.services.UpdateTask(ctx, task); err != nil {
					result["status"] = "failed"
					result["error"] = err.Error()
				} else {
					result["task_id"] = task.ID
				}
			}
		}

	case "send_notification":
		var cfg struct {
			MemberID string `json:"member_id"`
			Title    string `json:"title"`
			Body     string `json:"body"`
			Type     string `json:"type"`
		}
		if err := json.Unmarshal(step.Config, &cfg); err != nil {
			result["status"] = "failed"
			result["error"] = err.Error()
		} else {
			if cfg.Type == "" {
				cfg.Type = "workflow"
			}
			notif := &proto.Notification{
				MemberID: cfg.MemberID,
				Type:     cfg.Type,
				Title:    cfg.Title,
				Body:     cfg.Body,
			}
			if err := r.services.CreateNotification(ctx, notif); err != nil {
				result["status"] = "failed"
				result["error"] = err.Error()
			} else {
				result["notification_id"] = notif.ID
				output["notification_id"] = notif.ID
			}
		}

	case "http_request":
		var cfg struct {
			URL     string            `json:"url"`
			Method  string            `json:"method"`
			Headers map[string]string `json:"headers"`
			Body    string            `json:"body"`
		}
		if err := json.Unmarshal(step.Config, &cfg); err != nil {
			result["status"] = "failed"
			result["error"] = err.Error()
		} else {
			if cfg.Method == "" {
				cfg.Method = "GET"
			}
			var bodyReader io.Reader
			if cfg.Body != "" {
				bodyReader = strings.NewReader(cfg.Body)
			}
			httpReq, err := http.NewRequestWithContext(ctx, cfg.Method, cfg.URL, bodyReader)
			if err != nil {
				result["status"] = "failed"
				result["error"] = err.Error()
			} else {
				for k, v := range cfg.Headers {
					httpReq.Header.Set(k, v)
				}
				httpResp, err := http.DefaultClient.Do(httpReq)
				if err != nil {
					result["status"] = "failed"
					result["error"] = err.Error()
				} else {
					defer httpResp.Body.Close()
					respBody, _ := io.ReadAll(io.LimitReader(httpResp.Body, 1<<20)) // 1MB limit
					result["status_code"] = httpResp.StatusCode
					result["response"] = string(respBody)
					output["status_code"] = httpResp.StatusCode
					output["response"] = string(respBody)
				}
			}
		}

	case "conditional":
		var cfg struct {
			Field    string `json:"field"`
			Operator string `json:"operator"`
			Value    any    `json:"value"`
		}
		if err := json.Unmarshal(step.Config, &cfg); err != nil {
			result["status"] = "failed"
			result["error"] = err.Error()
		} else {
			// Evaluate condition against previous step output
			var fieldValue any
			if cfg.Field != "" {
				// Try to get from previous output using field as key
				fieldValue = getNestedValue(map[string]any{"prev": prevOutput}, cfg.Field)
			}
			conditionMet := evaluateCondition(fieldValue, cfg.Operator, cfg.Value)
			result["condition_met"] = conditionMet
			output["condition_met"] = conditionMet
			if !conditionMet {
				result["status"] = "skipped"
			}
		}

	case "delay":
		var cfg struct {
			Seconds int `json:"seconds"`
		}
		if err := json.Unmarshal(step.Config, &cfg); err != nil {
			result["status"] = "failed"
			result["error"] = err.Error()
		} else if cfg.Seconds > 0 && cfg.Seconds <= 300 {
			time.Sleep(time.Duration(cfg.Seconds) * time.Second)
		}

	default:
		result["status"] = "skipped"
		result["error"] = "unknown step type: " + step.Type
	}

	return output
}

// evaluateCondition evaluates a simple condition.
func evaluateCondition(fieldValue any, operator string, expected any) bool {
	switch operator {
	case "eq", "equals":
		return fmt.Sprintf("%v", fieldValue) == fmt.Sprintf("%v", expected)
	case "neq", "not_equals":
		return fmt.Sprintf("%v", fieldValue) != fmt.Sprintf("%v", expected)
	case "contains":
		return strings.Contains(fmt.Sprintf("%v", fieldValue), fmt.Sprintf("%v", expected))
	case "exists":
		return fieldValue != nil
	default:
		return false
	}
}

// getNestedValue retrieves a value from a nested map using dot notation (e.g., "prev.status_code").
func getNestedValue(data map[string]any, path string) any {
	parts := strings.SplitN(path, ".", 2)
	if len(parts) == 0 {
		return nil
	}
	val, ok := data[parts[0]]
	if !ok {
		return nil
	}
	if len(parts) == 1 {
		return val
	}
	if nested, ok := val.(map[string]any); ok {
		return getNestedValue(nested, parts[1])
	}
	return nil
}

// startCronWorkflows starts all cron-triggered workflows.
func (r *Router) startCronWorkflows() {
	go func() {
		ctx := context.Background()
		// Get all workspaces and their cron workflows
		workspaces, err := r.services.ListWorkspaces(ctx)
		if err != nil {
			return
		}
		for _, ws := range workspaces {
			workflows, err := r.services.ListWorkflows(ctx, ws.ID)
			if err != nil {
				continue
			}
			for _, wf := range workflows {
				if wf.Enabled && wf.TriggerType == "cron" {
					go r.runCronWorkflow(wf)
				}
			}
		}
	}()
}

// runCronWorkflow runs a cron-triggered workflow on a schedule.
func (r *Router) runCronWorkflow(wf *proto.Workflow) {
	// Parse cron expression from trigger_config
	var cfg struct {
		IntervalSeconds int `json:"interval_seconds"`
	}
	if err := json.Unmarshal(wf.TriggerConfig, &cfg); err != nil || cfg.IntervalSeconds <= 0 {
		return
	}
	ticker := time.NewTicker(time.Duration(cfg.IntervalSeconds) * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		if !wf.Enabled {
			break
		}
		// Re-check if workflow is still enabled
		current, err := r.services.GetWorkflow(context.Background(), wf.ID)
		if err != nil || current == nil || !current.Enabled {
			break
		}
		wf = current

		run := &proto.WorkflowRun{
			WorkflowID: wf.ID,
			Status:     proto.WfRunRunning,
		}
		if err := r.services.CreateWorkflowRun(context.Background(), run); err != nil {
			continue
		}
		r.executeWorkflow(run, wf)
	}
}

// TriggerWorkflowByEvent triggers all workflows with an event trigger matching the given event type.
func (r *Router) TriggerWorkflowByEvent(workspaceID, eventType string, eventData map[string]any) {
	go func() {
		ctx := context.Background()
		workflows, err := r.services.ListWorkflows(ctx, workspaceID)
		if err != nil {
			return
		}
		for _, wf := range workflows {
			if !wf.Enabled || wf.TriggerType != "event" {
				continue
			}
			// Check if event matches trigger config
			var cfg struct {
				EventType string `json:"event_type"`
			}
			if err := json.Unmarshal(wf.TriggerConfig, &cfg); err != nil {
				continue
			}
			if cfg.EventType != eventType {
				continue
			}
			triggerData, _ := json.Marshal(eventData)
			run := &proto.WorkflowRun{
				WorkflowID:  wf.ID,
				Status:      proto.WfRunRunning,
				TriggerData: triggerData,
			}
			if err := r.services.CreateWorkflowRun(ctx, run); err != nil {
				continue
			}
			r.executeWorkflow(run, wf)
		}
	}()
}
