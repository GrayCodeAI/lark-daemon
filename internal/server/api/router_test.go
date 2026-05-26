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

	router := NewRouter(svc, st, hub, auth, logger, ha)
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
	resp := env.doReq(t, "POST", "/v1/messages/"+msg.ID+"/reactions", map[string]string{"emoji": "thumbsup", "member_id": member.ID}, apiKey)
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
	pin := map[string]string{"message_id": msg.ID, "pinned_by": member.ID}
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
	agentID, apiKey, wsID := env.createAgent(t)

	approval := map[string]string{"agent_id": agentID, "action": "deploy", "payload": `{"env":"prod"}`}
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
