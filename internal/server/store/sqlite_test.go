package store

import (
	"context"
	"testing"

	"lark-daemon/internal/proto"
)

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func seedWorkspace(t *testing.T, s *SQLiteStore, slug string) *proto.Workspace {
	t.Helper()
	ws := &proto.Workspace{Name: "Test Workspace", Slug: slug}
	if err := s.CreateWorkspace(context.Background(), ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	return ws
}

func seedMember(t *testing.T, s *SQLiteStore, wsID, name string, mt proto.MemberType) *proto.Member {
	t.Helper()
	m := &proto.Member{WorkspaceID: wsID, Name: name, Type: mt}
	if err := s.CreateMember(context.Background(), m); err != nil {
		t.Fatalf("create member: %v", err)
	}
	return m
}

func seedChannel(t *testing.T, s *SQLiteStore, wsID, name string) *proto.Channel {
	t.Helper()
	ch := &proto.Channel{WorkspaceID: wsID, Name: name, Type: proto.ChannelPublic}
	if err := s.CreateChannel(context.Background(), ch); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	return ch
}

// --- Workspace tests ---

func TestWorkspaceCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	ws := &proto.Workspace{Name: "Acme", Slug: "acme"}
	if err := s.CreateWorkspace(ctx, ws); err != nil {
		t.Fatalf("create: %v", err)
	}
	if ws.ID == "" {
		t.Fatal("ID not set")
	}

	got, err := s.GetWorkspace(ctx, ws.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "Acme" || got.Slug != "acme" {
		t.Fatalf("mismatch: %+v", got)
	}

	bySlug, err := s.GetWorkspaceBySlug(ctx, "acme")
	if err != nil {
		t.Fatalf("get by slug: %v", err)
	}
	if bySlug.ID != ws.ID {
		t.Fatalf("slug lookup mismatch")
	}

	ws.Name = "Acme Corp"
	if err := s.UpdateWorkspace(ctx, ws); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ = s.GetWorkspace(ctx, ws.ID)
	if got.Name != "Acme Corp" {
		t.Fatalf("update failed: %s", got.Name)
	}

	all, err := s.ListWorkspaces(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1, got %d", len(all))
	}
}

func TestWorkspaceDuplicateSlug(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	s.CreateWorkspace(ctx, &proto.Workspace{Name: "A", Slug: "dup"})
	err := s.CreateWorkspace(ctx, &proto.Workspace{Name: "B", Slug: "dup"})
	if err == nil {
		t.Fatal("expected duplicate slug error")
	}
}

// --- Member tests ---

func TestMemberCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	ws := seedWorkspace(t, s, "ws1")

	m := &proto.Member{WorkspaceID: ws.ID, Name: "alice", Type: proto.MemberHuman}
	if err := s.CreateMember(ctx, m); err != nil {
		t.Fatalf("create: %v", err)
	}
	if m.ID == "" {
		t.Fatal("ID not set")
	}

	got, err := s.GetMember(ctx, m.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "alice" || got.Type != proto.MemberHuman {
		t.Fatalf("mismatch: %+v", got)
	}

	byName, err := s.GetMemberByName(ctx, ws.ID, "alice")
	if err != nil {
		t.Fatalf("get by name: %v", err)
	}
	if byName.ID != m.ID {
		t.Fatal("name lookup mismatch")
	}

	m.AvatarURL = "https://example.com/avatar.png"
	if err := s.UpdateMember(ctx, m); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ = s.GetMember(ctx, m.ID)
	if got.AvatarURL != "https://example.com/avatar.png" {
		t.Fatalf("update failed")
	}

	all, err := s.ListMembers(ctx, ws.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1, got %d", len(all))
	}

	if err := s.DeleteMember(ctx, m.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	got, _ = s.GetMember(ctx, m.ID)
	if got != nil {
		t.Fatal("expected nil after delete")
	}
}

func TestMemberAgentAPIKey(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	ws := seedWorkspace(t, s, "ws1")

	agent := &proto.Member{WorkspaceID: ws.ID, Name: "bot", Type: proto.MemberAgent, APIKey: "lr_test123"}
	s.CreateMember(ctx, agent)

	byKey, err := s.GetMemberByAPIKey(ctx, "lr_test123")
	if err != nil {
		t.Fatalf("get by api key: %v", err)
	}
	if byKey.ID != agent.ID {
		t.Fatal("api key lookup mismatch")
	}

	nilKey, _ := s.GetMemberByAPIKey(ctx, "nonexistent")
	if nilKey != nil {
		t.Fatal("expected nil for bad key")
	}
}

func TestMemberRoleCard(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	ws := seedWorkspace(t, s, "ws1")
	agent := seedMember(t, s, ws.ID, "agent1", proto.MemberAgent)

	rc := &proto.RoleCard{SystemPrompt: "You are helpful", Capabilities: []string{"code", "chat"}}
	rt := &proto.RuntimeInfo{Type: "llm", Provider: "anthropic", Model: "claude-3"}
	if err := s.UpdateMemberRoleCard(ctx, agent.ID, rc, rt); err != nil {
		t.Fatalf("update role card: %v", err)
	}

	got, _ := s.GetMember(ctx, agent.ID)
	if got.RoleCard == nil || got.RoleCard.SystemPrompt != "You are helpful" {
		t.Fatalf("role card not persisted: %+v", got.RoleCard)
	}
	if got.RuntimeInfo == nil || got.RuntimeInfo.Model != "claude-3" {
		t.Fatalf("runtime info not persisted: %+v", got.RuntimeInfo)
	}
}

// --- Channel tests ---

func TestChannelCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	ws := seedWorkspace(t, s, "ws1")

	ch := &proto.Channel{WorkspaceID: ws.ID, Name: "general", Type: proto.ChannelPublic, Topic: "Chat"}
	if err := s.CreateChannel(ctx, ch); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := s.GetChannel(ctx, ch.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "general" || got.Topic != "Chat" || got.IsPrivate {
		t.Fatalf("mismatch: %+v", got)
	}

	ch.Name = "general-chat"
	if err := s.UpdateChannel(ctx, ch); err != nil {
		t.Fatalf("update: %v", err)
	}

	all, _ := s.ListChannels(ctx, ws.ID)
	if len(all) != 1 {
		t.Fatalf("expected 1, got %d", len(all))
	}

	if err := s.DeleteChannel(ctx, ch.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	got, _ = s.GetChannel(ctx, ch.ID)
	if got != nil {
		t.Fatal("expected nil after delete")
	}
}

func TestChannelPrivate(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	ws := seedWorkspace(t, s, "ws1")

	ch := &proto.Channel{WorkspaceID: ws.ID, Name: "secret", Type: proto.ChannelPublic, IsPrivate: true}
	s.CreateChannel(ctx, ch)

	got, _ := s.GetChannel(ctx, ch.ID)
	if !got.IsPrivate {
		t.Fatal("expected private")
	}
}

// --- Channel membership tests ---

func TestChannelMembership(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	ws := seedWorkspace(t, s, "ws1")
	ch := seedChannel(t, s, ws.ID, "general")
	m1 := seedMember(t, s, ws.ID, "alice", proto.MemberHuman)
	m2 := seedMember(t, s, ws.ID, "bob", proto.MemberHuman)

	s.AddChannelMember(ctx, ch.ID, m1.ID)
	s.AddChannelMember(ctx, ch.ID, m2.ID)

	// Duplicate add should not error (INSERT OR IGNORE)
	if err := s.AddChannelMember(ctx, ch.ID, m1.ID); err != nil {
		t.Fatalf("duplicate add: %v", err)
	}

	members, err := s.ListChannelMembers(ctx, ch.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("expected 2, got %d", len(members))
	}

	isMem, _ := s.IsChannelMember(ctx, ch.ID, m1.ID)
	if !isMem {
		t.Fatal("expected true")
	}

	s.RemoveChannelMember(ctx, ch.ID, m1.ID)
	isMem, _ = s.IsChannelMember(ctx, ch.ID, m1.ID)
	if isMem {
		t.Fatal("expected false after remove")
	}
}

func TestUpdateLastRead(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	ws := seedWorkspace(t, s, "ws1")
	ch := seedChannel(t, s, ws.ID, "general")
	m := seedMember(t, s, ws.ID, "alice", proto.MemberHuman)
	s.AddChannelMember(ctx, ch.ID, m.ID)

	if err := s.UpdateLastRead(ctx, ch.ID, m.ID); err != nil {
		t.Fatalf("update last read: %v", err)
	}
}

// --- Message tests ---

func TestMessageCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	ws := seedWorkspace(t, s, "ws1")
	ch := seedChannel(t, s, ws.ID, "general")
	member := seedMember(t, s, ws.ID, "alice", proto.MemberHuman)

	msg := &proto.Message{ChannelID: ch.ID, SenderID: member.ID, Content: "Hello world"}
	if err := s.CreateMessage(ctx, msg); err != nil {
		t.Fatalf("create: %v", err)
	}
	if msg.ID == "" || msg.Type != "text" {
		t.Fatalf("defaults not set: %+v", msg)
	}

	got, err := s.GetMessage(ctx, msg.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Content != "Hello world" {
		t.Fatalf("mismatch: %+v", got)
	}

	msg.Content = "Edited"
	s.UpdateMessage(ctx, msg)
	got, _ = s.GetMessage(ctx, msg.ID)
	if got.Content != "Edited" {
		t.Fatalf("update failed")
	}

	all, _ := s.ListMessages(ctx, ch.ID, 10, 0)
	if len(all) != 1 {
		t.Fatalf("expected 1, got %d", len(all))
	}

	recent, _ := s.GetRecentMessages(ctx, ch.ID, 5)
	if len(recent) != 1 {
		t.Fatalf("expected 1 recent, got %d", len(recent))
	}

	s.DeleteMessage(ctx, msg.ID)
	got, _ = s.GetMessage(ctx, msg.ID)
	if got != nil {
		t.Fatal("expected nil after delete")
	}
}

func TestMessageThread(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	ws := seedWorkspace(t, s, "ws1")
	ch := seedChannel(t, s, ws.ID, "general")
	member := seedMember(t, s, ws.ID, "alice", proto.MemberHuman)

	parent := &proto.Message{ChannelID: ch.ID, SenderID: member.ID, Content: "parent"}
	s.CreateMessage(ctx, parent)

	reply := &proto.Message{ChannelID: ch.ID, SenderID: member.ID, Content: "reply", ThreadID: parent.ID}
	s.CreateMessage(ctx, reply)

	thread, _ := s.ListThreadMessages(ctx, parent.ID)
	if len(thread) != 2 {
		t.Fatalf("expected 2 (parent + reply), got %d", len(thread))
	}

	// ListMessages should exclude threaded replies
	topLevel, _ := s.ListMessages(ctx, ch.ID, 10, 0)
	if len(topLevel) != 1 {
		t.Fatalf("expected 1 top-level, got %d", len(topLevel))
	}
}

func TestListMessagesBySender(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	ws := seedWorkspace(t, s, "ws1")
	ch := seedChannel(t, s, ws.ID, "general")
	alice := seedMember(t, s, ws.ID, "alice", proto.MemberHuman)
	bob := seedMember(t, s, ws.ID, "bob", proto.MemberHuman)

	s.CreateMessage(ctx, &proto.Message{ChannelID: ch.ID, SenderID: alice.ID, Content: "a1"})
	s.CreateMessage(ctx, &proto.Message{ChannelID: ch.ID, SenderID: alice.ID, Content: "a2"})
	s.CreateMessage(ctx, &proto.Message{ChannelID: ch.ID, SenderID: bob.ID, Content: "b1"})

	msgs, _ := s.ListMessagesBySender(ctx, alice.ID)
	if len(msgs) != 2 {
		t.Fatalf("expected 2, got %d", len(msgs))
	}
}

func TestMessageMetadata(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	ws := seedWorkspace(t, s, "ws1")
	ch := seedChannel(t, s, ws.ID, "general")
	member := seedMember(t, s, ws.ID, "alice", proto.MemberHuman)

	msg := &proto.Message{
		ChannelID: ch.ID,
		SenderID:  member.ID,
		Content:   "with meta",
		Metadata:  []byte(`{"key":"value"}`),
	}
	s.CreateMessage(ctx, msg)

	got, _ := s.GetMessage(ctx, msg.ID)
	if string(got.Metadata) != `{"key":"value"}` {
		t.Fatalf("metadata not preserved: %s", got.Metadata)
	}
}

// --- Reaction tests ---

func TestReactions(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	ws := seedWorkspace(t, s, "ws1")
	ch := seedChannel(t, s, ws.ID, "general")
	member := seedMember(t, s, ws.ID, "alice", proto.MemberHuman)
	msg := &proto.Message{ChannelID: ch.ID, SenderID: member.ID, Content: "hi"}
	s.CreateMessage(ctx, msg)

	r := &proto.Reaction{MessageID: msg.ID, MemberID: member.ID, Emoji: "thumbsup"}
	if err := s.AddReaction(ctx, r); err != nil {
		t.Fatalf("add: %v", err)
	}

	reactions, _ := s.ListReactions(ctx, msg.ID)
	if len(reactions) != 1 {
		t.Fatalf("expected 1, got %d", len(reactions))
	}

	// Duplicate should be ignored
	s.AddReaction(ctx, r)
	reactions, _ = s.ListReactions(ctx, msg.ID)
	if len(reactions) != 1 {
		t.Fatalf("expected 1 after dup, got %d", len(reactions))
	}

	s.RemoveReaction(ctx, msg.ID, member.ID, "thumbsup")
	reactions, _ = s.ListReactions(ctx, msg.ID)
	if len(reactions) != 0 {
		t.Fatalf("expected 0 after remove, got %d", len(reactions))
	}
}

// --- Search tests ---

func TestSearchMessages(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	ws := seedWorkspace(t, s, "ws1")
	ch := seedChannel(t, s, ws.ID, "general")
	member := seedMember(t, s, ws.ID, "alice", proto.MemberHuman)

	s.CreateMessage(ctx, &proto.Message{ChannelID: ch.ID, SenderID: member.ID, Content: "hello world"})
	s.CreateMessage(ctx, &proto.Message{ChannelID: ch.ID, SenderID: member.ID, Content: "goodbye world"})
	s.CreateMessage(ctx, &proto.Message{ChannelID: ch.ID, SenderID: member.ID, Content: "foo bar"})

	results, err := s.SearchMessages(ctx, "world", "", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2, got %d", len(results))
	}

	// Search with channel filter
	results, _ = s.SearchMessages(ctx, "world", ch.ID, 10)
	if len(results) != 2 {
		t.Fatalf("expected 2 with channel, got %d", len(results))
	}

	// No match
	results, _ = s.SearchMessages(ctx, "nonexistent", "", 10)
	if len(results) != 0 {
		t.Fatalf("expected 0, got %d", len(results))
	}
}

// --- Task tests ---

func TestTaskCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	ws := seedWorkspace(t, s, "ws1")
	agent := seedMember(t, s, ws.ID, "agent", proto.MemberAgent)
	creator := seedMember(t, s, ws.ID, "human", proto.MemberHuman)

	task := &proto.Task{
		WorkspaceID: ws.ID,
		AssignedTo:  agent.ID,
		CreatedBy:   creator.ID,
		Title:       "Fix the bug",
		Description: "It's broken",
	}
	if err := s.CreateTask(ctx, task); err != nil {
		t.Fatalf("create: %v", err)
	}
	if task.ID == "" || task.Status != proto.TaskTodo || task.Priority != "medium" {
		t.Fatalf("defaults not set: %+v", task)
	}

	got, _ := s.GetTask(ctx, task.ID)
	if got.Title != "Fix the bug" || got.AssignedTo != agent.ID {
		t.Fatalf("mismatch: %+v", got)
	}

	task.Status = proto.TaskInProgress
	task.Priority = "high"
	s.UpdateTask(ctx, task)
	got, _ = s.GetTask(ctx, task.ID)
	if got.Status != proto.TaskInProgress || got.Priority != "high" {
		t.Fatalf("update failed: %+v", got)
	}

	all, _ := s.ListTasks(ctx, ws.ID, "")
	if len(all) != 1 {
		t.Fatalf("expected 1, got %d", len(all))
	}

	done, _ := s.ListTasks(ctx, ws.ID, proto.TaskDone)
	if len(done) != 0 {
		t.Fatalf("expected 0 done, got %d", len(done))
	}

	inProgress, _ := s.ListTasks(ctx, ws.ID, proto.TaskInProgress)
	if len(inProgress) != 1 {
		t.Fatalf("expected 1 in_progress, got %d", len(inProgress))
	}

	s.DeleteTask(ctx, task.ID)
	got, _ = s.GetTask(ctx, task.ID)
	if got != nil {
		t.Fatal("expected nil after delete")
	}
}

// --- Agent memory tests ---

func TestAgentMemoryCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	ws := seedWorkspace(t, s, "ws1")
	agent := seedMember(t, s, ws.ID, "agent", proto.MemberAgent)

	mem := &proto.AgentMemory{AgentID: agent.ID, Namespace: "prefs", Key: "theme", Value: "dark"}
	if err := s.SetMemory(ctx, mem); err != nil {
		t.Fatalf("set: %v", err)
	}

	got, _ := s.GetMemory(ctx, agent.ID, "prefs", "theme")
	if got.Value != "dark" {
		t.Fatalf("mismatch: %+v", got)
	}

	// Upsert
	mem.Value = "light"
	s.SetMemory(ctx, mem)
	got, _ = s.GetMemory(ctx, agent.ID, "prefs", "theme")
	if got.Value != "light" {
		t.Fatalf("upsert failed: %s", got.Value)
	}

	s.SetMemory(ctx, &proto.AgentMemory{AgentID: agent.ID, Namespace: "prefs", Key: "lang", Value: "en"})
	all, _ := s.ListMemory(ctx, agent.ID, "prefs")
	if len(all) != 2 {
		t.Fatalf("expected 2, got %d", len(all))
	}

	s.DeleteMemory(ctx, agent.ID, "prefs", "theme")
	got, _ = s.GetMemory(ctx, agent.ID, "prefs", "theme")
	if got != nil {
		t.Fatal("expected nil after delete")
	}
}

func TestAgentMemoryNamespaces(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	ws := seedWorkspace(t, s, "ws1")
	agent := seedMember(t, s, ws.ID, "agent", proto.MemberAgent)

	s.SetMemory(ctx, &proto.AgentMemory{AgentID: agent.ID, Namespace: "ns1", Key: "k", Value: "v1"})
	s.SetMemory(ctx, &proto.AgentMemory{AgentID: agent.ID, Namespace: "ns2", Key: "k", Value: "v2"})

	ns1, _ := s.ListMemory(ctx, agent.ID, "ns1")
	ns2, _ := s.ListMemory(ctx, agent.ID, "ns2")
	if len(ns1) != 1 || len(ns2) != 1 {
		t.Fatalf("namespace isolation failed: ns1=%d ns2=%d", len(ns1), len(ns2))
	}
	if ns1[0].Value != "v1" || ns2[0].Value != "v2" {
		t.Fatal("namespace values wrong")
	}
}

// --- File tests ---

func TestFileCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	ws := seedWorkspace(t, s, "ws1")
	uploader := seedMember(t, s, ws.ID, "alice", proto.MemberHuman)

	f := &proto.File{
		WorkspaceID: ws.ID,
		UploaderID:  uploader.ID,
		Filename:    "test.png",
		MimeType:    "image/png",
		Size:        1024,
		Path:        "/uploads/test.png",
	}
	if err := s.CreateFile(ctx, f); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, _ := s.GetFile(ctx, f.ID)
	if got.Filename != "test.png" || got.Size != 1024 {
		t.Fatalf("mismatch: %+v", got)
	}

	all, _ := s.ListFiles(ctx, ws.ID, 10, 0)
	if len(all) != 1 {
		t.Fatalf("expected 1, got %d", len(all))
	}

	s.DeleteFile(ctx, f.ID)
	got, _ = s.GetFile(ctx, f.ID)
	if got != nil {
		t.Fatal("expected nil after delete")
	}
}

// --- Pin tests ---

func TestPinCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	ws := seedWorkspace(t, s, "ws1")
	ch := seedChannel(t, s, ws.ID, "general")
	member := seedMember(t, s, ws.ID, "alice", proto.MemberHuman)
	msg := &proto.Message{ChannelID: ch.ID, SenderID: member.ID, Content: "pin me"}
	s.CreateMessage(ctx, msg)

	pin := &proto.Pin{MessageID: msg.ID, ChannelID: ch.ID, PinnedBy: member.ID}
	if err := s.CreatePin(ctx, pin); err != nil {
		t.Fatalf("create: %v", err)
	}

	pins, _ := s.ListPins(ctx, ch.ID)
	if len(pins) != 1 {
		t.Fatalf("expected 1, got %d", len(pins))
	}

	s.DeletePin(ctx, msg.ID)
	pins, _ = s.ListPins(ctx, ch.ID)
	if len(pins) != 0 {
		t.Fatalf("expected 0 after delete, got %d", len(pins))
	}
}

func TestPinDuplicate(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	ws := seedWorkspace(t, s, "ws1")
	ch := seedChannel(t, s, ws.ID, "general")
	member := seedMember(t, s, ws.ID, "alice", proto.MemberHuman)
	msg := &proto.Message{ChannelID: ch.ID, SenderID: member.ID, Content: "pin me"}
	s.CreateMessage(ctx, msg)

	s.CreatePin(ctx, &proto.Pin{MessageID: msg.ID, ChannelID: ch.ID, PinnedBy: member.ID})
	err := s.CreatePin(ctx, &proto.Pin{MessageID: msg.ID, ChannelID: ch.ID, PinnedBy: member.ID})
	if err == nil {
		t.Fatal("expected duplicate pin error")
	}
}

// --- DM tests ---

func TestDMChannel(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	ws := seedWorkspace(t, s, "ws1")
	alice := seedMember(t, s, ws.ID, "alice", proto.MemberHuman)
	bob := seedMember(t, s, ws.ID, "bob", proto.MemberHuman)

	dm := &proto.Channel{WorkspaceID: ws.ID, Name: "", Type: proto.ChannelDM}
	s.CreateChannel(ctx, dm)
	s.AddChannelMember(ctx, dm.ID, alice.ID)
	s.AddChannelMember(ctx, dm.ID, bob.ID)

	found, _ := s.GetDMChannel(ctx, ws.ID, []string{alice.ID, bob.ID})
	if found == nil || found.ID != dm.ID {
		t.Fatalf("DM channel not found")
	}

	// Non-existent DM
	other := seedMember(t, s, ws.ID, "charlie", proto.MemberHuman)
	found, _ = s.GetDMChannel(ctx, ws.ID, []string{alice.ID, other.ID})
	if found != nil {
		t.Fatal("expected nil for non-existent DM")
	}

	// List DM channels for alice
	dms, _ := s.ListDMChannels(ctx, alice.ID)
	if len(dms) != 1 {
		t.Fatalf("expected 1 DM, got %d", len(dms))
	}
}

// --- Unread tests ---

func TestUnreadCounts(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	ws := seedWorkspace(t, s, "ws1")
	ch := seedChannel(t, s, ws.ID, "general")
	alice := seedMember(t, s, ws.ID, "alice", proto.MemberHuman)
	bob := seedMember(t, s, ws.ID, "bob", proto.MemberHuman)

	s.AddChannelMember(ctx, ch.ID, alice.ID)
	s.AddChannelMember(ctx, ch.ID, bob.ID)

	// Bob sends messages
	s.CreateMessage(ctx, &proto.Message{ChannelID: ch.ID, SenderID: bob.ID, Content: "msg1"})
	s.CreateMessage(ctx, &proto.Message{ChannelID: ch.ID, SenderID: bob.ID, Content: "msg2"})

	counts, _ := s.GetUnreadCounts(ctx, alice.ID)
	if counts[ch.ID] != 2 {
		t.Fatalf("expected 2 unread, got %d", counts[ch.ID])
	}

	// Mark read
	s.UpdateLastRead(ctx, ch.ID, alice.ID)
	counts, _ = s.GetUnreadCounts(ctx, alice.ID)
	if counts[ch.ID] != 0 {
		t.Fatalf("expected 0 after mark read, got %d", counts[ch.ID])
	}
}

// --- Approval tests ---

func TestApprovalCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	ws := seedWorkspace(t, s, "ws1")
	agent := seedMember(t, s, ws.ID, "agent", proto.MemberAgent)

	a := &proto.ApprovalRequest{
		WorkspaceID: ws.ID,
		AgentID:     agent.ID,
		Action:      "deploy",
		Payload:     `{"env":"prod"}`,
	}
	if err := s.CreateApproval(ctx, a); err != nil {
		t.Fatalf("create: %v", err)
	}
	if a.Status != proto.ApprovalPending {
		t.Fatalf("expected pending, got %s", a.Status)
	}

	got, _ := s.GetApproval(ctx, a.ID)
	if got.Action != "deploy" || got.Status != proto.ApprovalPending {
		t.Fatalf("mismatch: %+v", got)
	}

	all, _ := s.ListApprovals(ctx, ws.ID, proto.ApprovalPending)
	if len(all) != 1 {
		t.Fatalf("expected 1 pending, got %d", len(all))
	}

	// Approve
	a.Status = proto.ApprovalApproved
	a.ReviewerID = agent.ID
	a.ReviewNote = "LGTM"
	a.ReviewedAt = 12345
	s.UpdateApproval(ctx, a)

	got, _ = s.GetApproval(ctx, a.ID)
	if got.Status != proto.ApprovalApproved || got.ReviewNote != "LGTM" {
		t.Fatalf("approval update failed: %+v", got)
	}

	done, _ := s.ListApprovals(ctx, ws.ID, proto.ApprovalApproved)
	if len(done) != 1 {
		t.Fatalf("expected 1 approved, got %d", len(done))
	}
}

// --- Edge cases ---

func TestGetNonExistent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	ws, _ := s.GetWorkspace(ctx, "nonexistent")
	if ws != nil {
		t.Fatal("expected nil")
	}

	m, _ := s.GetMember(ctx, "nonexistent")
	if m != nil {
		t.Fatal("expected nil")
	}

	ch, _ := s.GetChannel(ctx, "nonexistent")
	if ch != nil {
		t.Fatal("expected nil")
	}

	msg, _ := s.GetMessage(ctx, "nonexistent")
	if msg != nil {
		t.Fatal("expected nil")
	}

	task, _ := s.GetTask(ctx, "nonexistent")
	if task != nil {
		t.Fatal("expected nil")
	}
}

func TestListMessagesDefaultLimit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	ws := seedWorkspace(t, s, "ws1")
	ch := seedChannel(t, s, ws.ID, "general")
	member := seedMember(t, s, ws.ID, "alice", proto.MemberHuman)

	for i := 0; i < 60; i++ {
		s.CreateMessage(ctx, &proto.Message{ChannelID: ch.ID, SenderID: member.ID, Content: "msg"})
	}

	// Default limit should be 50
	msgs, _ := s.ListMessages(ctx, ch.ID, 0, 0)
	if len(msgs) != 50 {
		t.Fatalf("expected 50 default limit, got %d", len(msgs))
	}

	// With offset
	msgs, _ = s.ListMessages(ctx, ch.ID, 10, 50)
	if len(msgs) != 10 {
		t.Fatalf("expected 10 with offset, got %d", len(msgs))
	}
}

// --- Cascade / delete tests ---

func TestDeleteMemberCascade(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	ws := seedWorkspace(t, s, "ws1")
	ch := seedChannel(t, s, ws.ID, "general")
	alice := seedMember(t, s, ws.ID, "alice", proto.MemberHuman)
	bob := seedMember(t, s, ws.ID, "bob", proto.MemberHuman)
	s.AddChannelMember(ctx, ch.ID, alice.ID)
	s.AddChannelMember(ctx, ch.ID, bob.ID)

	// Alice sends messages, creates tasks, uploads files, pins
	msg1 := &proto.Message{ChannelID: ch.ID, SenderID: alice.ID, Content: "msg1"}
	s.CreateMessage(ctx, msg1)
	s.CreateMessage(ctx, &proto.Message{ChannelID: ch.ID, SenderID: alice.ID, Content: "msg2"})
	s.CreateTask(ctx, &proto.Task{WorkspaceID: ws.ID, ChannelID: ch.ID, CreatedBy: alice.ID, Title: "task1"})
	s.CreateFile(ctx, &proto.File{WorkspaceID: ws.ID, UploaderID: alice.ID, Filename: "file1.txt", Path: "/tmp/f1", Size: 100})
	s.CreatePin(ctx, &proto.Pin{ChannelID: ch.ID, MessageID: msg1.ID, PinnedBy: alice.ID})

	// Delete alice — must not fail with FK constraint error
	if err := s.DeleteMember(ctx, alice.ID); err != nil {
		t.Fatalf("DeleteMember should succeed with cascade, got: %v", err)
	}

	// Verify alice is gone
	m, _ := s.GetMember(ctx, alice.ID)
	if m != nil {
		t.Fatal("deleted member should be nil")
	}

	// Verify alice's messages are gone
	msgs, _ := s.ListMessagesBySender(ctx, alice.ID)
	if len(msgs) != 0 {
		t.Fatalf("expected 0 messages after cascade delete, got %d", len(msgs))
	}
}

func TestDeleteChannelCascade(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	ws := seedWorkspace(t, s, "ws1")
	ch := seedChannel(t, s, ws.ID, "general")
	alice := seedMember(t, s, ws.ID, "alice", proto.MemberHuman)
	s.AddChannelMember(ctx, ch.ID, alice.ID)

	s.CreateMessage(ctx, &proto.Message{ChannelID: ch.ID, SenderID: alice.ID, Content: "msg1"})
	s.CreateTask(ctx, &proto.Task{WorkspaceID: ws.ID, ChannelID: ch.ID, CreatedBy: alice.ID, Title: "task1"})

	// Delete channel — must not fail with FK constraint error
	if err := s.DeleteChannel(ctx, ch.ID); err != nil {
		t.Fatalf("DeleteChannel should succeed with cascade, got: %v", err)
	}
}

// --- DM exact match test ---

func TestDMChannelExactMatch(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	ws := seedWorkspace(t, s, "ws1")
	alice := seedMember(t, s, ws.ID, "alice", proto.MemberHuman)
	bob := seedMember(t, s, ws.ID, "bob", proto.MemberHuman)
	charlie := seedMember(t, s, ws.ID, "charlie", proto.MemberHuman)

	// Create a group DM with alice, bob, charlie
	group := &proto.Channel{WorkspaceID: ws.ID, Name: "group", Type: proto.ChannelDM, IsPrivate: true}
	s.CreateChannel(ctx, group)
	s.AddChannelMember(ctx, group.ID, alice.ID)
	s.AddChannelMember(ctx, group.ID, bob.ID)
	s.AddChannelMember(ctx, group.ID, charlie.ID)

	// Create a 2-member DM: alice + bob
	dm := &proto.Channel{WorkspaceID: ws.ID, Name: "dm", Type: proto.ChannelDM, IsPrivate: true}
	s.CreateChannel(ctx, dm)
	s.AddChannelMember(ctx, dm.ID, alice.ID)
	s.AddChannelMember(ctx, dm.ID, bob.ID)

	// GetDMChannel(alice, bob) should find the 2-member DM, NOT the group
	found, err := s.GetDMChannel(ctx, ws.ID, []string{alice.ID, bob.ID})
	if err != nil {
		t.Fatalf("GetDMChannel error: %v", err)
	}
	if found == nil {
		t.Fatal("expected DM channel, got nil")
	}
	if found.ID != dm.ID {
		t.Fatalf("expected DM channel %s, got group %s", dm.ID, found.ID)
	}
}

// --- Unread excludes own messages ---

func TestUnreadExcludesOwnMessages(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	ws := seedWorkspace(t, s, "ws1")
	ch := seedChannel(t, s, ws.ID, "general")
	alice := seedMember(t, s, ws.ID, "alice", proto.MemberHuman)
	bob := seedMember(t, s, ws.ID, "bob", proto.MemberHuman)
	s.AddChannelMember(ctx, ch.ID, alice.ID)
	s.AddChannelMember(ctx, ch.ID, bob.ID)

	// Alice sends messages — should NOT count as unread for alice
	s.CreateMessage(ctx, &proto.Message{ChannelID: ch.ID, SenderID: alice.ID, Content: "self1"})
	s.CreateMessage(ctx, &proto.Message{ChannelID: ch.ID, SenderID: alice.ID, Content: "self2"})

	counts, _ := s.GetUnreadCounts(ctx, alice.ID)
	if counts[ch.ID] != 0 {
		t.Fatalf("own messages should not be unread, got %d", counts[ch.ID])
	}

	// Bob sends — should count for alice
	s.CreateMessage(ctx, &proto.Message{ChannelID: ch.ID, SenderID: bob.ID, Content: "from bob"})
	counts, _ = s.GetUnreadCounts(ctx, alice.ID)
	if counts[ch.ID] != 1 {
		t.Fatalf("expected 1 unread from bob, got %d", counts[ch.ID])
	}
}

// --- GetRecentMessages return type ---

func TestGetRecentMessagesPointerType(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	ws := seedWorkspace(t, s, "ws1")
	ch := seedChannel(t, s, ws.ID, "general")
	alice := seedMember(t, s, ws.ID, "alice", proto.MemberHuman)
	s.CreateMessage(ctx, &proto.Message{ChannelID: ch.ID, SenderID: alice.ID, Content: "hello"})

	msgs, err := s.GetRecentMessages(ctx, ch.ID, 10)
	if err != nil {
		t.Fatalf("GetRecentMessages error: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Content != "hello" {
		t.Fatalf("expected 'hello', got '%s'", msgs[0].Content)
	}
}

// --- ListMessagesBySender limit ---

func TestListMessagesBySenderLimit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	ws := seedWorkspace(t, s, "ws1")
	ch := seedChannel(t, s, ws.ID, "general")
	alice := seedMember(t, s, ws.ID, "alice", proto.MemberHuman)

	for i := 0; i < 600; i++ {
		s.CreateMessage(ctx, &proto.Message{ChannelID: ch.ID, SenderID: alice.ID, Content: "msg"})
	}

	msgs, _ := s.ListMessagesBySender(ctx, alice.ID)
	if len(msgs) > 500 {
		t.Fatalf("expected max 500, got %d", len(msgs))
	}
}
