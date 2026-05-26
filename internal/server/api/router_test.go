package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"lark/internal/proto"
	"lark/internal/server/service"
	"lark/internal/server/store"
	"lark/internal/server/websocket"
)

// testEnv holds the test server and helpers.
type testEnv struct {
	server *httptest.Server
	auth   *websocket.AuthService
	store  store.Store
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	st, err := store.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	svc := service.NewServices(st)
	hub := websocket.NewHub()
	auth := websocket.NewAuthService("test-secret")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// HubAdapter for websocket.AgentStore
	ha := &hubAdapter{store: st}

	router := NewRouter(svc, st, hub, auth, logger, ha, "*")
	ts := httptest.NewServer(router)
	t.Cleanup(ts.Close)

	return &testEnv{server: ts, auth: auth, store: st}
}

// hubAdapter is a minimal adapter for tests.
type hubAdapter struct {
	store store.Store
}

func (h *hubAdapter) GetMemberByName(workspaceID, name string) (*websocket.MemberBrief, error) {
	m, err := h.store.GetMemberByName(context.Background(), workspaceID, name)
	if err != nil || m == nil {
		return nil, err
	}
	return &websocket.MemberBrief{
		ID:          m.ID,
		Name:        m.Name,
		IsAgent:     m.Type == proto.MemberAgent,
		WorkspaceID: m.WorkspaceID,
	}, nil
}

func (h *hubAdapter) GetRecentMessages(channelID string, limit int) ([]websocket.MessageBrief, error) {
	msgs, err := h.store.GetRecentMessages(context.Background(), channelID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]websocket.MessageBrief, len(msgs))
	for i, m := range msgs {
		out[i] = websocket.MessageBrief{
			ID:        m.ID,
			SenderID:  m.SenderID,
			Content:   m.Content,
			CreatedAt: m.CreatedAt,
		}
	}
	return out, nil
}

func (h *hubAdapter) UpdateMemberRoleCard(memberID string, roleCard *proto.RoleCard, runtime *proto.RuntimeInfo) error {
	return h.store.UpdateMemberRoleCard(context.Background(), memberID, roleCard, runtime)
}

// createWorkspace creates a workspace via the API and returns its ID.
func (e *testEnv) createWorkspace(t *testing.T, slug string) string {
	t.Helper()
	body := fmt.Sprintf(`{"name":"W-%s","slug":"%s"}`, slug, slug)
	req, _ := http.NewRequest("POST", e.server.URL+"/v1/workspaces", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create workspace: %d %s", resp.StatusCode, b)
	}
	var out map[string]string
	json.NewDecoder(resp.Body).Decode(&out)
	return out["id"]
}

// createMember creates a member and returns its ID.
func (e *testEnv) createMember(t *testing.T, wsID, name, mtype, apiKey string) string {
	t.Helper()
	body := fmt.Sprintf(`{"name":"%s","type":"%s"}`, name, mtype)
	req, _ := http.NewRequest("POST", e.server.URL+"/v1/workspaces/"+wsID+"/members", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create member: %d %s", resp.StatusCode, b)
	}
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	return out["id"].(string)
}

// createAgent creates a workspace + agent member, returns (agentID, apiKey, wsID).
func (e *testEnv) createAgent(t *testing.T) (string, string, string) {
	t.Helper()
	// Create a human member first to get auth
	wsID := e.createWorkspace(t, "ws-"+t.Name())
	human := &proto.Member{WorkspaceID: wsID, Name: "admin", Type: proto.MemberHuman}
	e.store.CreateMember(context.Background(), human)
	token, _ := e.auth.GenerateToken(human.ID, wsID)

	// Create agent via API
	body := `{"name":"agent1","type":"agent"}`
	req, _ := http.NewRequest("POST", e.server.URL+"/v1/workspaces/"+wsID+"/members", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	agentID := out["id"].(string)
	apiKey := out["api_key"].(string)
	return agentID, apiKey, wsID
}

// doReq performs a request with auth.
func (e *testEnv) doReq(t *testing.T, method, path string, body any, apiKey string) *http.Response {
	t.Helper()
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		reqBody = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, e.server.URL+path, reqBody)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	return resp
}

// readJSON reads and decodes JSON response body.
func readJSON(t *testing.T, resp *http.Response, out any) {
	t.Helper()
	defer resp.Body.Close()
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("decode: %v", err)
		}
	}
}

// checkOK fails the test if the response is not a 2xx success.
func checkOK(t *testing.T, resp *http.Response) {
	t.Helper()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("expected 2xx, got %d: %s", resp.StatusCode, b)
	}
}

// --- Health ---

func TestHealthEndpoint(t *testing.T) {
	env := newTestEnv(t)
	resp, err := http.Get(env.server.URL + "/health")
	if err != nil {
		t.Fatalf("health: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// --- Auth middleware ---

func TestAuthRequired(t *testing.T) {
	env := newTestEnv(t)
	resp := env.doReq(t, "GET", "/v1/workspaces", nil, "")
	if resp.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

// --- Workspace ---

func TestWorkspaceCreateUnauthenticated(t *testing.T) {
	env := newTestEnv(t)
	wsID := env.createWorkspace(t, "test")
	if wsID == "" {
		t.Fatal("expected workspace ID")
	}
}

func TestWorkspaceCRUD(t *testing.T) {
	env := newTestEnv(t)
	_, apiKey, wsID := env.createAgent(t)

	// Get
	resp := env.doReq(t, "GET", "/v1/workspaces/"+wsID, nil, apiKey)
	checkOK(t, resp)
	var ws map[string]any
	readJSON(t, resp, &ws)

	// Update
	resp = env.doReq(t, "PATCH", "/v1/workspaces/"+wsID, map[string]string{"name": "Updated"}, apiKey)
	checkOK(t, resp)
	resp.Body.Close()
}

// --- Member ---

func TestMemberCRUD(t *testing.T) {
	env := newTestEnv(t)
	_, apiKey, wsID := env.createAgent(t)

	// Create human member
	memberID := env.createMember(t, wsID, "alice", "human", apiKey)

	// Get
	resp := env.doReq(t, "GET", fmt.Sprintf("/v1/workspaces/%s/members/%s", wsID, memberID), nil, apiKey)
	checkOK(t, resp)
	resp.Body.Close()

	// List
	resp = env.doReq(t, "GET", "/v1/workspaces/"+wsID+"/members", nil, apiKey)
	checkOK(t, resp)
	var members []map[string]any
	readJSON(t, resp, &members)
	if len(members) < 2 { // at least agent + alice
		t.Fatalf("expected >=2 members, got %d", len(members))
	}

	// Delete
	resp = env.doReq(t, "DELETE", fmt.Sprintf("/v1/workspaces/%s/members/%s", wsID, memberID), nil, apiKey)
	checkOK(t, resp)
	resp.Body.Close()
}

// --- Channel ---

func TestChannelCRUD(t *testing.T) {
	env := newTestEnv(t)
	_, apiKey, wsID := env.createAgent(t)

	// Create
	ch := map[string]string{"name": "general", "type": "channel"}
	resp := env.doReq(t, "POST", "/v1/workspaces/"+wsID+"/channels", ch, apiKey)
	checkOK(t, resp)
	var chResp map[string]any
	readJSON(t, resp, &chResp)
	chID := chResp["id"].(string)

	// Get
	resp = env.doReq(t, "GET", fmt.Sprintf("/v1/workspaces/%s/channels/%s", wsID, chID), nil, apiKey)
	checkOK(t, resp)
	resp.Body.Close()

	// List
	resp = env.doReq(t, "GET", "/v1/workspaces/"+wsID+"/channels", nil, apiKey)
	checkOK(t, resp)
	resp.Body.Close()

	// Update
	resp = env.doReq(t, "PATCH", fmt.Sprintf("/v1/workspaces/%s/channels/%s", wsID, chID), map[string]string{"name": "general-chat"}, apiKey)
	checkOK(t, resp)
	resp.Body.Close()

	// Delete
	resp = env.doReq(t, "DELETE", fmt.Sprintf("/v1/workspaces/%s/channels/%s", wsID, chID), nil, apiKey)
	checkOK(t, resp)
	resp.Body.Close()
}

// --- Message ---

func TestMessageCRUD(t *testing.T) {
	env := newTestEnv(t)
	_, apiKey, wsID := env.createAgent(t)

	// Create channel
	ch := &proto.Channel{WorkspaceID: wsID, Name: "general", Type: proto.ChannelPublic}
	env.store.CreateChannel(context.Background(), ch)

	// Create message
	msg := map[string]string{"content": "hello world"}
	resp := env.doReq(t, "POST", "/v1/channels/"+ch.ID+"/messages", msg, apiKey)
	checkOK(t, resp)
	var msgResp map[string]any
	readJSON(t, resp, &msgResp)
	msgID := msgResp["id"].(string)

	// List
	resp = env.doReq(t, "GET", "/v1/channels/"+ch.ID+"/messages", nil, apiKey)
	checkOK(t, resp)
	var msgs []map[string]any
	readJSON(t, resp, &msgs)
	if len(msgs) != 1 {
		t.Fatalf("expected 1, got %d", len(msgs))
	}

	// Update
	resp = env.doReq(t, "PATCH", "/v1/messages/"+msgID, map[string]string{"content": "edited"}, apiKey)
	checkOK(t, resp)
	resp.Body.Close()

	// Delete
	resp = env.doReq(t, "DELETE", "/v1/messages/"+msgID, nil, apiKey)
	checkOK(t, resp)
	resp.Body.Close()
}

// --- Search ---

func TestSearch(t *testing.T) {
	env := newTestEnv(t)
	_, apiKey, wsID := env.createAgent(t)

	// Create channel and messages via store
	ch := &proto.Channel{WorkspaceID: wsID, Name: "general", Type: proto.ChannelPublic}
	env.store.CreateChannel(context.Background(), ch)
	member := &proto.Member{WorkspaceID: wsID, Name: "agent1", Type: proto.MemberAgent}
	env.store.CreateMember(context.Background(), member)
	env.store.CreateMessage(context.Background(), &proto.Message{ChannelID: ch.ID, SenderID: member.ID, Content: "hello world"})
	env.store.CreateMessage(context.Background(), &proto.Message{ChannelID: ch.ID, SenderID: member.ID, Content: "goodbye world"})

	// Search
	resp := env.doReq(t, "GET", "/v1/search?q=world", nil, apiKey)
	checkOK(t, resp)
	var results []map[string]any
	readJSON(t, resp, &results)
	if len(results) != 2 {
		t.Fatalf("expected 2, got %d", len(results))
	}
}

// --- Reaction ---

func TestReactionCRUD(t *testing.T) {
	env := newTestEnv(t)
	_, apiKey, wsID := env.createAgent(t)

	ch := &proto.Channel{WorkspaceID: wsID, Name: "general", Type: proto.ChannelPublic}
	env.store.CreateChannel(context.Background(), ch)
	member := &proto.Member{WorkspaceID: wsID, Name: "agent1", Type: proto.MemberAgent}
	env.store.CreateMember(context.Background(), member)
	msg := &proto.Message{ChannelID: ch.ID, SenderID: member.ID, Content: "react me"}
	env.store.CreateMessage(context.Background(), msg)

	// Add reaction
	resp := env.doReq(t, "POST", "/v1/messages/"+msg.ID+"/reactions", map[string]string{"emoji": "thumbsup"}, apiKey)
	checkOK(t, resp)
	resp.Body.Close()

	// List reactions
	resp = env.doReq(t, "GET", "/v1/messages/"+msg.ID+"/reactions", nil, apiKey)
	checkOK(t, resp)
	var reactions []map[string]any
	readJSON(t, resp, &reactions)
	if len(reactions) != 1 {
		t.Fatalf("expected 1, got %d", len(reactions))
	}

	// Remove reaction
	resp = env.doReq(t, "DELETE", "/v1/messages/"+msg.ID+"/reactions/thumbsup", nil, apiKey)
	checkOK(t, resp)
	resp.Body.Close()
}

// --- Task ---

func TestTaskCRUD(t *testing.T) {
	env := newTestEnv(t)
	_, apiKey, wsID := env.createAgent(t)

	// Create
	task := map[string]string{"title": "Fix bug", "priority": "high"}
	resp := env.doReq(t, "POST", "/v1/workspaces/"+wsID+"/tasks", task, apiKey)
	checkOK(t, resp)
	var taskResp map[string]any
	readJSON(t, resp, &taskResp)
	taskID := taskResp["id"].(string)

	// List
	resp = env.doReq(t, "GET", "/v1/workspaces/"+wsID+"/tasks", nil, apiKey)
	checkOK(t, resp)
	resp.Body.Close()

	// Update
	resp = env.doReq(t, "PATCH", "/v1/tasks/"+taskID, map[string]string{"status": "in_progress"}, apiKey)
	checkOK(t, resp)
	resp.Body.Close()

	// Delete
	resp = env.doReq(t, "DELETE", "/v1/tasks/"+taskID, nil, apiKey)
	checkOK(t, resp)
	resp.Body.Close()
}

// --- Agent memory ---

func TestAgentMemoryCRUD(t *testing.T) {
	env := newTestEnv(t)
	agentID, apiKey, _ := env.createAgent(t)

	// Set
	mem := map[string]string{"namespace": "prefs", "key": "theme", "value": "dark"}
	resp := env.doReq(t, "POST", "/v1/agents/"+agentID+"/memory", mem, apiKey)
	checkOK(t, resp)
	resp.Body.Close()

	// List
	resp = env.doReq(t, "GET", "/v1/agents/"+agentID+"/memory?namespace=prefs", nil, apiKey)
	checkOK(t, resp)
	var memories []map[string]any
	readJSON(t, resp, &memories)
	if len(memories) != 1 {
		t.Fatalf("expected 1, got %d", len(memories))
	}

	// Delete
	resp = env.doReq(t, "DELETE", "/v1/agents/"+agentID+"/memory/prefs/theme", nil, apiKey)
	checkOK(t, resp)
	resp.Body.Close()
}

// --- Pin ---

func TestPinCRUD(t *testing.T) {
	env := newTestEnv(t)
	_, apiKey, wsID := env.createAgent(t)

	ch := &proto.Channel{WorkspaceID: wsID, Name: "general", Type: proto.ChannelPublic}
	env.store.CreateChannel(context.Background(), ch)
	member := &proto.Member{WorkspaceID: wsID, Name: "agent1", Type: proto.MemberAgent}
	env.store.CreateMember(context.Background(), member)
	msg := &proto.Message{ChannelID: ch.ID, SenderID: member.ID, Content: "pin me"}
	env.store.CreateMessage(context.Background(), msg)

	// Pin
	pin := map[string]string{"message_id": msg.ID}
	resp := env.doReq(t, "POST", "/v1/channels/"+ch.ID+"/pins", pin, apiKey)
	checkOK(t, resp)
	resp.Body.Close()

	// List
	resp = env.doReq(t, "GET", "/v1/channels/"+ch.ID+"/pins", nil, apiKey)
	checkOK(t, resp)
	var pins []map[string]any
	readJSON(t, resp, &pins)
	if len(pins) != 1 {
		t.Fatalf("expected 1, got %d", len(pins))
	}

	// Unpin
	resp = env.doReq(t, "DELETE", "/v1/pins/"+msg.ID, nil, apiKey)
	checkOK(t, resp)
	resp.Body.Close()
}

// --- Approval ---

func TestApprovalCRUD(t *testing.T) {
	env := newTestEnv(t)
	_, apiKey, wsID := env.createAgent(t)

	approval := map[string]string{"action": "deploy", "payload": `{"env":"prod"}`}
	resp := env.doReq(t, "POST", "/v1/workspaces/"+wsID+"/approvals", approval, apiKey)
	checkOK(t, resp)
	var aResp map[string]any
	readJSON(t, resp, &aResp)
	if aResp["status"] != "pending" {
		t.Fatalf("expected pending, got %v", aResp["status"])
	}

	// List pending
	resp = env.doReq(t, "GET", "/v1/workspaces/"+wsID+"/approvals?status=pending", nil, apiKey)
	checkOK(t, resp)
	var approvals []map[string]any
	readJSON(t, resp, &approvals)
	if len(approvals) != 1 {
		t.Fatalf("expected 1, got %d", len(approvals))
	}
}

// --- Agent metrics ---

func TestAgentMetrics(t *testing.T) {
	env := newTestEnv(t)
	agentID, apiKey, _ := env.createAgent(t)

	resp := env.doReq(t, "GET", "/v1/agents/"+agentID+"/metrics", nil, apiKey)
	checkOK(t, resp)
	var metrics map[string]any
	readJSON(t, resp, &metrics)
	if metrics["agent_id"] != agentID {
		t.Fatalf("expected %s, got %v", agentID, metrics["agent_id"])
	}
}

// --- Edge cases ---

func TestGetNonExistent(t *testing.T) {
	env := newTestEnv(t)
	_, apiKey, _ := env.createAgent(t)
	resp := env.doReq(t, "GET", "/v1/workspaces/nonexistent", nil, apiKey)
	if resp.StatusCode != 404 {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestDuplicateWorkspaceSlug(t *testing.T) {
	env := newTestEnv(t)
	env.createWorkspace(t, "dup")
	resp := env.doReq(t, "POST", "/v1/workspaces", map[string]string{"name": "B", "slug": "dup"}, "")
	if resp.StatusCode != 500 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 500, got %d %s", resp.StatusCode, b)
	}
	resp.Body.Close()
}

func TestFileCRUD(t *testing.T) {
	env := newTestEnv(t)
	agentID, apiKey, wsID := env.createAgent(t)

	f := &proto.File{
		WorkspaceID: wsID,
		UploaderID:  agentID,
		Filename:    "test.txt",
		MimeType:    "text/plain",
		Size:        5,
		Path:        "/tmp/test.txt",
	}
	env.store.CreateFile(context.Background(), f)

	resp := env.doReq(t, "GET", "/v1/files/"+f.ID, nil, apiKey)
	checkOK(t, resp)
	var fileResp map[string]any
	readJSON(t, resp, &fileResp)
	if fileResp["filename"] != "test.txt" {
		t.Fatalf("expected test.txt, got %v", fileResp["filename"])
	}
}

// --- Sender spoofing fix ---

func TestCreateMessageSenderFromAuth(t *testing.T) {
	env := newTestEnv(t)
	agentID, apiKey, wsID := env.createAgent(t)

	ch := &proto.Channel{WorkspaceID: wsID, Name: "general", Type: proto.ChannelPublic}
	env.store.CreateChannel(context.Background(), ch)

	// Attempt to spoof sender_id in request body
	msg := map[string]string{"content": "hello", "sender_id": "spoofed-id"}
	resp := env.doReq(t, "POST", "/v1/channels/"+ch.ID+"/messages", msg, apiKey)
	checkOK(t, resp)
	var msgResp map[string]any
	readJSON(t, resp, &msgResp)

	// sender_id must come from auth context, not request body
	if msgResp["sender_id"] != agentID {
		t.Fatalf("expected sender_id=%s from auth, got %v", agentID, msgResp["sender_id"])
	}
}

// --- Protected fields on PATCH ---

func TestUpdateWorkspaceProtectedFields(t *testing.T) {
	env := newTestEnv(t)
	_, apiKey, wsID := env.createAgent(t)

	ws, _ := env.store.GetWorkspace(context.Background(), wsID)
	originalToken := ws.AgentProvisionToken

	// Try to overwrite ID and AgentProvisionToken via PATCH
	patch := map[string]string{
		"name":                      "Updated",
		"id":                        "hacked-id",
		"agent_provision_token":     "hacked-token",
	}
	resp := env.doReq(t, "PATCH", "/v1/workspaces/"+wsID, patch, apiKey)
	checkOK(t, resp)
	var wsResp map[string]any
	readJSON(t, resp, &wsResp)

	if wsResp["id"] != wsID {
		t.Fatalf("ID was overwritten: got %v", wsResp["id"])
	}
	if wsResp["agent_provision_token"] != originalToken {
		t.Fatalf("AgentProvisionToken was overwritten: got %v", wsResp["agent_provision_token"])
	}
	if wsResp["name"] != "Updated" {
		t.Fatalf("name was not updated: got %v", wsResp["name"])
	}
}

func TestUpdateMemberProtectedFields(t *testing.T) {
	env := newTestEnv(t)
	agentID, apiKey, wsID := env.createAgent(t)

	member, _ := env.store.GetMember(context.Background(), agentID)
	originalType := string(member.Type)

	// Try to overwrite ID, WorkspaceID, Type, APIKey via PATCH on the agent itself
	patch := map[string]string{
		"name":         "agent-updated",
		"id":           "hacked-id",
		"workspace_id": "hacked-ws",
		"type":         "human",
		"api_key":      "hacked-key",
	}
	resp := env.doReq(t, "PATCH", "/v1/workspaces/"+wsID+"/members/"+agentID, patch, apiKey)
	checkOK(t, resp)
	var mResp map[string]any
	readJSON(t, resp, &mResp)

	if mResp["id"] != agentID {
		t.Fatalf("ID was overwritten: got %v", mResp["id"])
	}
	if mResp["workspace_id"] != wsID {
		t.Fatalf("WorkspaceID was overwritten: got %v", mResp["workspace_id"])
	}
	if mResp["type"] != originalType {
		t.Fatalf("Type was overwritten: got %v", mResp["type"])
	}
	// API key should be masked in responses
	if mResp["api_key"] != nil && mResp["api_key"] != "" {
		t.Fatalf("APIKey should be masked, got %v", mResp["api_key"])
	}
	if mResp["name"] != "agent-updated" {
		t.Fatalf("name was not updated: got %v", mResp["name"])
	}
}

func TestUpdateChannelProtectedFields(t *testing.T) {
	env := newTestEnv(t)
	_, apiKey, wsID := env.createAgent(t)

	ch := &proto.Channel{WorkspaceID: wsID, Name: "general", Type: proto.ChannelPublic}
	env.store.CreateChannel(context.Background(), ch)

	// Try to overwrite ID, WorkspaceID, Type
	patch := map[string]string{
		"name":         "general-updated",
		"id":           "hacked-id",
		"workspace_id": "hacked-ws",
		"type":         "dm",
	}
	resp := env.doReq(t, "PATCH", "/v1/workspaces/"+wsID+"/channels/"+ch.ID, patch, apiKey)
	checkOK(t, resp)
	var chResp map[string]any
	readJSON(t, resp, &chResp)

	if chResp["id"] != ch.ID {
		t.Fatalf("ID was overwritten: got %v", chResp["id"])
	}
	if chResp["workspace_id"] != wsID {
		t.Fatalf("WorkspaceID was overwritten: got %v", chResp["workspace_id"])
	}
	if chResp["type"] != "channel" {
		t.Fatalf("Type was overwritten: got %v", chResp["type"])
	}
	if chResp["name"] != "general-updated" {
		t.Fatalf("name was not updated: got %v", chResp["name"])
	}
}

func TestUpdateMessageProtectedFields(t *testing.T) {
	env := newTestEnv(t)
	agentID, apiKey, wsID := env.createAgent(t)

	ch := &proto.Channel{WorkspaceID: wsID, Name: "general", Type: proto.ChannelPublic}
	env.store.CreateChannel(context.Background(), ch)
	msg := &proto.Message{ChannelID: ch.ID, SenderID: agentID, Content: "original"}
	env.store.CreateMessage(context.Background(), msg)

	// Try to overwrite ID, ChannelID, SenderID
	patch := map[string]string{
		"content":    "edited",
		"id":         "hacked-id",
		"channel_id": "hacked-channel",
		"sender_id":  "hacked-sender",
	}
	resp := env.doReq(t, "PATCH", "/v1/messages/"+msg.ID, patch, apiKey)
	checkOK(t, resp)
	var mResp map[string]any
	readJSON(t, resp, &mResp)

	if mResp["id"] != msg.ID {
		t.Fatalf("ID was overwritten: got %v", mResp["id"])
	}
	if mResp["channel_id"] != ch.ID {
		t.Fatalf("ChannelID was overwritten: got %v", mResp["channel_id"])
	}
	if mResp["sender_id"] != agentID {
		t.Fatalf("SenderID was overwritten: got %v", mResp["sender_id"])
	}
	if mResp["content"] != "edited" {
		t.Fatalf("content was not updated: got %v", mResp["content"])
	}
}

func TestUpdateTaskProtectedFields(t *testing.T) {
	env := newTestEnv(t)
	_, apiKey, wsID := env.createAgent(t)

	task := map[string]string{"title": "Fix bug", "priority": "high"}
	resp := env.doReq(t, "POST", "/v1/workspaces/"+wsID+"/tasks", task, apiKey)
	checkOK(t, resp)
	var taskResp map[string]any
	readJSON(t, resp, &taskResp)
	taskID := taskResp["id"].(string)

	// Try to overwrite ID, WorkspaceID, CreatedBy
	patch := map[string]string{
		"title":       "Updated bug",
		"id":          "hacked-id",
		"workspace_id": "hacked-ws",
		"created_by":  "hacked-creator",
	}
	resp = env.doReq(t, "PATCH", "/v1/tasks/"+taskID, patch, apiKey)
	checkOK(t, resp)
	var tResp map[string]any
	readJSON(t, resp, &tResp)

	if tResp["id"] != taskID {
		t.Fatalf("ID was overwritten: got %v", tResp["id"])
	}
	if tResp["workspace_id"] != wsID {
		t.Fatalf("WorkspaceID was overwritten: got %v", tResp["workspace_id"])
	}
	if tResp["title"] != "Updated bug" {
		t.Fatalf("title was not updated: got %v", tResp["title"])
	}
}

// --- Additional identity spoofing tests ---

func TestReactionMemberFromAuth(t *testing.T) {
	env := newTestEnv(t)
	agentID, apiKey, wsID := env.createAgent(t)

	ch := &proto.Channel{WorkspaceID: wsID, Name: "general", Type: proto.ChannelPublic}
	env.store.CreateChannel(context.Background(), ch)
	msg := &proto.Message{ChannelID: ch.ID, SenderID: agentID, Content: "react me"}
	env.store.CreateMessage(context.Background(), msg)

	// Attempt to spoof member_id
	resp := env.doReq(t, "POST", "/v1/messages/"+msg.ID+"/reactions", map[string]string{"emoji": "thumbsup", "member_id": "spoofed-id"}, apiKey)
	checkOK(t, resp)
	var rResp map[string]any
	readJSON(t, resp, &rResp)
	if rResp["member_id"] != agentID {
		t.Fatalf("expected member_id=%s from auth, got %v", agentID, rResp["member_id"])
	}
}

func TestRemoveReactionMemberFromAuth(t *testing.T) {
	env := newTestEnv(t)
	agentID, apiKey, wsID := env.createAgent(t)

	ch := &proto.Channel{WorkspaceID: wsID, Name: "general", Type: proto.ChannelPublic}
	env.store.CreateChannel(context.Background(), ch)
	msg := &proto.Message{ChannelID: ch.ID, SenderID: agentID, Content: "react me"}
	env.store.CreateMessage(context.Background(), msg)

	// Add reaction
	env.doReq(t, "POST", "/v1/messages/"+msg.ID+"/reactions", map[string]string{"emoji": "thumbsup"}, apiKey).Body.Close()

	// Remove — should use auth member, not query param
	resp := env.doReq(t, "DELETE", "/v1/messages/"+msg.ID+"/reactions/thumbsup?member_id=spoofed-id", nil, apiKey)
	if resp.StatusCode != 204 {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Verify removed
	resp = env.doReq(t, "GET", "/v1/messages/"+msg.ID+"/reactions", nil, apiKey)
	checkOK(t, resp)
	var reactions []map[string]any
	readJSON(t, resp, &reactions)
	if len(reactions) != 0 {
		t.Fatalf("expected 0 reactions after remove, got %d", len(reactions))
	}
}

func TestCreateTaskCreatedByFromAuth(t *testing.T) {
	env := newTestEnv(t)
	agentID, apiKey, wsID := env.createAgent(t)

	task := map[string]string{"title": "Fix bug", "created_by": "spoofed-id"}
	resp := env.doReq(t, "POST", "/v1/workspaces/"+wsID+"/tasks", task, apiKey)
	checkOK(t, resp)
	var taskResp map[string]any
	readJSON(t, resp, &taskResp)
	if taskResp["created_by"] != agentID {
		t.Fatalf("expected created_by=%s from auth, got %v", agentID, taskResp["created_by"])
	}
}

func TestPinMessagePinnedByFromAuth(t *testing.T) {
	env := newTestEnv(t)
	agentID, apiKey, wsID := env.createAgent(t)

	ch := &proto.Channel{WorkspaceID: wsID, Name: "general", Type: proto.ChannelPublic}
	env.store.CreateChannel(context.Background(), ch)
	msg := &proto.Message{ChannelID: ch.ID, SenderID: agentID, Content: "pin me"}
	env.store.CreateMessage(context.Background(), msg)

	// Attempt to spoof pinned_by
	pin := map[string]string{"message_id": msg.ID, "pinned_by": "spoofed-id"}
	resp := env.doReq(t, "POST", "/v1/channels/"+ch.ID+"/pins", pin, apiKey)
	checkOK(t, resp)
	var pResp map[string]any
	readJSON(t, resp, &pResp)
	if pResp["pinned_by"] != agentID {
		t.Fatalf("expected pinned_by=%s from auth, got %v", agentID, pResp["pinned_by"])
	}
}

func TestApprovalAgentFromAuth(t *testing.T) {
	env := newTestEnv(t)
	agentID, apiKey, wsID := env.createAgent(t)

	approval := map[string]string{"agent_id": "spoofed-id", "action": "deploy"}
	resp := env.doReq(t, "POST", "/v1/workspaces/"+wsID+"/approvals", approval, apiKey)
	checkOK(t, resp)
	var aResp map[string]any
	readJSON(t, resp, &aResp)
	if aResp["agent_id"] != agentID {
		t.Fatalf("expected agent_id=%s from auth, got %v", agentID, aResp["agent_id"])
	}
}

func TestReviewApprovalReviewerFromAuth(t *testing.T) {
	env := newTestEnv(t)
	agentID, apiKey, wsID := env.createAgent(t)

	// Create approval
	approval := map[string]string{"action": "deploy", "payload": `{"env":"prod"}`}
	resp := env.doReq(t, "POST", "/v1/workspaces/"+wsID+"/approvals", approval, apiKey)
	checkOK(t, resp)
	var aResp map[string]any
	readJSON(t, resp, &aResp)
	approvalID := aResp["id"].(string)

	// Review with spoofed reviewer_id
	review := map[string]any{"approved": true, "reviewer_id": "spoofed-id", "note": "looks good"}
	resp = env.doReq(t, "PATCH", "/v1/approvals/"+approvalID, review, apiKey)
	checkOK(t, resp)
	var rResp map[string]any
	readJSON(t, resp, &rResp)
	if rResp["reviewer_id"] != agentID {
		t.Fatalf("expected reviewer_id=%s from auth, got %v", agentID, rResp["reviewer_id"])
	}
}

func TestCreateDMMustIncludeAuthMember(t *testing.T) {
	env := newTestEnv(t)
	_, apiKey, wsID := env.createAgent(t)

	// Create two other members
	m1 := &proto.Member{WorkspaceID: wsID, Name: "alice", Type: proto.MemberHuman}
	env.store.CreateMember(context.Background(), m1)
	m2 := &proto.Member{WorkspaceID: wsID, Name: "bob", Type: proto.MemberHuman}
	env.store.CreateMember(context.Background(), m2)

	// Try to create DM without including auth'd member — should be forbidden
	dm := map[string]any{"member_ids": []string{m1.ID, m2.ID}}
	resp := env.doReq(t, "POST", "/v1/workspaces/"+wsID+"/dm", dm, apiKey)
	if resp.StatusCode != 403 {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestUpdateMessageRequiresAuthor(t *testing.T) {
	env := newTestEnv(t)
	_, apiKey, wsID := env.createAgent(t)

	other := &proto.Member{WorkspaceID: wsID, Name: "other", Type: proto.MemberAgent}
	env.store.CreateMember(context.Background(), other)
	ch := &proto.Channel{WorkspaceID: wsID, Name: "general", Type: proto.ChannelPublic}
	env.store.CreateChannel(context.Background(), ch)
	msg := &proto.Message{ChannelID: ch.ID, SenderID: other.ID, Content: "not yours"}
	env.store.CreateMessage(context.Background(), msg)

	resp := env.doReq(t, "PATCH", "/v1/messages/"+msg.ID, map[string]string{"content": "hacked"}, apiKey)
	if resp.StatusCode != 403 {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestDeleteMessageRequiresAuthor(t *testing.T) {
	env := newTestEnv(t)
	_, apiKey, wsID := env.createAgent(t)

	other := &proto.Member{WorkspaceID: wsID, Name: "other", Type: proto.MemberAgent}
	env.store.CreateMember(context.Background(), other)
	ch := &proto.Channel{WorkspaceID: wsID, Name: "general", Type: proto.ChannelPublic}
	env.store.CreateChannel(context.Background(), ch)
	msg := &proto.Message{ChannelID: ch.ID, SenderID: other.ID, Content: "not yours"}
	env.store.CreateMessage(context.Background(), msg)

	resp := env.doReq(t, "DELETE", "/v1/messages/"+msg.ID, nil, apiKey)
	if resp.StatusCode != 403 {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}
