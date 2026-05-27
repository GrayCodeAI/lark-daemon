package api

import (
	"context"
	"testing"

	"lark-daemon/internal/proto"
)

func TestCallListEmpty(t *testing.T) {
	env := newTestEnv(t)
	_, apiKey, _ := env.createAgent(t)

	resp := env.doReq(t, "GET", "/v1/calls", nil, apiKey)
	checkOK(t, resp)
	var calls []map[string]any
	readJSON(t, resp, &calls)
	if len(calls) != 0 {
		t.Fatalf("expected 0, got %d", len(calls))
	}
}

func TestCallListWithData(t *testing.T) {
	env := newTestEnv(t)
	agentID, apiKey, wsID := env.createAgent(t)

	// Seed a member to call
	member := &proto.Member{WorkspaceID: wsID, Name: "alice", Type: proto.MemberHuman}
	env.store.CreateMember(context.Background(), member)

	// Create a call
	call := &proto.Call{
		WorkspaceID: wsID,
		CallerID:    agentID,
		CalleeID:    member.ID,
		Type:        "audio",
		Status:      "ended",
	}
	env.store.CreateCall(context.Background(), call)

	resp := env.doReq(t, "GET", "/v1/calls", nil, apiKey)
	checkOK(t, resp)
	var calls []map[string]any
	readJSON(t, resp, &calls)
	if len(calls) != 1 {
		t.Fatalf("expected 1, got %d", len(calls))
	}
}

func TestCallGetByID(t *testing.T) {
	env := newTestEnv(t)
	agentID, apiKey, wsID := env.createAgent(t)

	member := &proto.Member{WorkspaceID: wsID, Name: "alice", Type: proto.MemberHuman}
	env.store.CreateMember(context.Background(), member)

	call := &proto.Call{
		WorkspaceID: wsID,
		CallerID:    agentID,
		CalleeID:    member.ID,
		Type:        "video",
		Status:      "ended",
	}
	env.store.CreateCall(context.Background(), call)

	resp := env.doReq(t, "GET", "/v1/calls/"+call.ID, nil, apiKey)
	checkOK(t, resp)
	var out map[string]any
	readJSON(t, resp, &out)
	if out["type"] != "video" {
		t.Fatalf("expected video, got %v", out["type"])
	}
}

func TestCallGetNonExistent(t *testing.T) {
	env := newTestEnv(t)
	_, apiKey, _ := env.createAgent(t)

	resp := env.doReq(t, "GET", "/v1/calls/nonexistent", nil, apiKey)
	if resp.StatusCode != 404 {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}
