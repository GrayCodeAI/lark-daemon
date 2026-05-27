package api

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"lark-daemon/internal/proto"
)

func TestWorkflowCRUD(t *testing.T) {
	env := newTestEnv(t)
	_, apiKey, wsID := env.createAgent(t)

	// Create
	steps, _ := json.Marshal([]map[string]any{
		{"type": "send_message", "config": map[string]any{"channel_id": "ch1", "content": "hello"}},
	})
	wf := map[string]any{
		"name":         "Test Workflow",
		"description":  "A test workflow",
		"trigger_type": "manual",
		"steps":        json.RawMessage(steps),
	}
	resp := env.doReq(t, "POST", "/v1/workspaces/"+wsID+"/workflows", wf, apiKey)
	checkOK(t, resp)
	var created map[string]any
	readJSON(t, resp, &created)
	wfID := created["id"].(string)

	// Get
	resp = env.doReq(t, "GET", "/v1/workflows/"+wfID, nil, apiKey)
	checkOK(t, resp)
	var got map[string]any
	readJSON(t, resp, &got)
	if got["name"] != "Test Workflow" {
		t.Fatalf("expected Test Workflow, got %v", got["name"])
	}

	// List
	resp = env.doReq(t, "GET", "/v1/workspaces/"+wsID+"/workflows", nil, apiKey)
	checkOK(t, resp)
	var list []map[string]any
	readJSON(t, resp, &list)
	if len(list) != 1 {
		t.Fatalf("expected 1, got %d", len(list))
	}

	// Update
	patch := map[string]any{"name": "Updated Workflow", "enabled": true}
	resp = env.doReq(t, "PATCH", "/v1/workflows/"+wfID, patch, apiKey)
	checkOK(t, resp)
	resp.Body.Close()

	// Delete
	resp = env.doReq(t, "DELETE", "/v1/workflows/"+wfID, nil, apiKey)
	if resp.StatusCode != 204 {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Verify gone
	resp = env.doReq(t, "GET", "/v1/workflows/"+wfID, nil, apiKey)
	if resp.StatusCode != 404 {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestWorkflowTrigger(t *testing.T) {
	env := newTestEnv(t)
	agentID, apiKey, wsID := env.createAgent(t)

	// Create a channel for send_message step
	ch := &proto.Channel{WorkspaceID: wsID, Name: "general", Type: proto.ChannelPublic}
	env.store.CreateChannel(context.Background(), ch)
	env.store.AddChannelMember(context.Background(), ch.ID, agentID)

	steps, _ := json.Marshal([]map[string]any{
		{"type": "send_message", "config": map[string]any{"channel_id": ch.ID, "content": "automated"}},
	})

	// Create workflow
	wf := &proto.Workflow{
		WorkspaceID: wsID,
		Name:        "Auto Msg",
		TriggerType: "manual",
		Steps:       steps,
		Enabled:     true,
		CreatedBy:   agentID,
	}
	env.store.CreateWorkflow(context.Background(), wf)

	// Trigger
	resp := env.doReq(t, "POST", "/v1/workflows/"+wf.ID+"/trigger", nil, apiKey)
	if resp.StatusCode != 202 {
		t.Fatalf("expected 202, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Check that a run was created and wait for async execution to finish
	var runs []*proto.WorkflowRun
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		runs, _ = env.store.ListWorkflowRuns(context.Background(), wf.ID, 10)
		if len(runs) > 0 && runs[0].Status != proto.WfRunRunning {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(runs) == 0 {
		t.Fatal("expected at least 1 run")
	}
}

func TestWorkflowListRuns(t *testing.T) {
	env := newTestEnv(t)
	agentID, apiKey, wsID := env.createAgent(t)

	wf := &proto.Workflow{
		WorkspaceID: wsID,
		Name:        "Test",
		TriggerType: "manual",
		Steps:       json.RawMessage("[]"),
		Enabled:     true,
		CreatedBy:   agentID,
	}
	if err := env.store.CreateWorkflow(context.Background(), wf); err != nil {
		t.Fatalf("create workflow: %v", err)
	}

	run := &proto.WorkflowRun{WorkflowID: wf.ID, Status: proto.WfRunCompleted}
	if err := env.store.CreateWorkflowRun(context.Background(), run); err != nil {
		t.Fatalf("create run: %v", err)
	}

	resp := env.doReq(t, "GET", "/v1/workflows/"+wf.ID+"/runs", nil, apiKey)
	checkOK(t, resp)
	var runs []map[string]any
	readJSON(t, resp, &runs)
	if len(runs) != 1 {
		t.Fatalf("expected 1, got %d", len(runs))
	}
}

func TestWorkflowGetRun(t *testing.T) {
	env := newTestEnv(t)
	agentID, apiKey, wsID := env.createAgent(t)

	wf := &proto.Workflow{WorkspaceID: wsID, Name: "Test", TriggerType: "manual", Steps: json.RawMessage("[]"), CreatedBy: agentID}
	if err := env.store.CreateWorkflow(context.Background(), wf); err != nil {
		t.Fatalf("create workflow: %v", err)
	}

	run := &proto.WorkflowRun{WorkflowID: wf.ID, Status: proto.WfRunCompleted}
	if err := env.store.CreateWorkflowRun(context.Background(), run); err != nil {
		t.Fatalf("create run: %v", err)
	}

	resp := env.doReq(t, "GET", "/v1/workflow-runs/"+run.ID, nil, apiKey)
	checkOK(t, resp)
	var out map[string]any
	readJSON(t, resp, &out)
	if out["status"] != "completed" {
		t.Fatalf("expected completed, got %v", out["status"])
	}
}
