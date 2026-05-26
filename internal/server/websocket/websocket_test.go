package websocket

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func TestNewEnvelope(t *testing.T) {
	env := NewEnvelope(EventMessageNew, map[string]string{"key": "value"})
	if env.V != 1 {
		t.Fatalf("expected V=1, got %d", env.V)
	}
	if env.Type != EventMessageNew {
		t.Fatalf("expected type %s, got %s", EventMessageNew, env.Type)
	}
	if env.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if env.TS == 0 {
		t.Fatal("expected non-zero timestamp")
	}
	var data map[string]string
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if data["key"] != "value" {
		t.Fatalf("expected key=value, got %s", data["key"])
	}
}

func TestNewEnvelopeMarshalError(t *testing.T) {
	env := NewEnvelope(EventError, make(chan int))
	if env.Data == nil {
		t.Fatal("expected non-nil data even on marshal error")
	}
}

func TestEnvelopeRoundTrip(t *testing.T) {
	env := NewEnvelope(EventTypingStart, TypingData{
		ChannelID: "ch1",
		MemberID:  "u1",
	})
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	var decoded Envelope
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if decoded.Type != EventTypingStart {
		t.Fatalf("expected %s, got %s", EventTypingStart, decoded.Type)
	}
	var td TypingData
	if err := json.Unmarshal(decoded.Data, &td); err != nil {
		t.Fatalf("unmarshal typing data: %v", err)
	}
	if td.ChannelID != "ch1" || td.MemberID != "u1" {
		t.Fatalf("mismatch: %+v", td)
	}
}

func TestConnIdentity(t *testing.T) {
	c := newTestConn("id1", "", false)
	c.SetIdentity("user1", "Alice", false)
	if c.ID() != "user1" {
		t.Fatalf("expected user1, got %s", c.ID())
	}
	if c.Name() != "Alice" {
		t.Fatalf("expected Alice, got %s", c.Name())
	}
	if c.IsAgent() {
		t.Fatal("expected not agent")
	}

	c.SetIdentity("agent1", "Bot", true)
	if !c.IsAgent() {
		t.Fatal("expected agent")
	}
}

func TestConnAuth(t *testing.T) {
	c := newTestConn("id1", "", false)
	if c.IsAuthenticated() {
		t.Fatal("expected not authenticated initially")
	}
	c.SetAuthenticated(true)
	if !c.IsAuthenticated() {
		t.Fatal("expected authenticated")
	}
}

func TestConnSubscribe(t *testing.T) {
	c := newTestConn("id1", "", false)
	if ok := c.Subscribe("ch1"); !ok {
		t.Fatal("expected subscribe ch1 to succeed")
	}
	if ok := c.Subscribe("ch2"); !ok {
		t.Fatal("expected subscribe ch2 to succeed")
	}
	if !c.IsSubscribed("ch1") {
		t.Fatal("expected subscribed to ch1")
	}
	if !c.IsSubscribed("ch2") {
		t.Fatal("expected subscribed to ch2")
	}
	if c.IsSubscribed("ch3") {
		t.Fatal("expected not subscribed to ch3")
	}
	c.Unsubscribe("ch1")
	if c.IsSubscribed("ch1") {
		t.Fatal("expected unsubscribed from ch1")
	}
}

func TestConnSubscribeLimit(t *testing.T) {
	c := newTestConn("id1", "", false)
	for i := 0; i < MaxSubscriptions; i++ {
		if ok := c.Subscribe(fmt.Sprintf("ch%d", i)); !ok {
			t.Fatalf("expected subscribe ch%d to succeed", i)
		}
	}
	if ok := c.Subscribe("overflow"); ok {
		t.Fatal("expected subscribe to fail at limit")
	}
	// Unsubscribing one should allow another
	c.Unsubscribe("ch0")
	if ok := c.Subscribe("new_ch"); !ok {
		t.Fatal("expected subscribe to succeed after unsubscribe")
	}
}

func newTestConn(id, name string, isAgent bool) *Conn {
	c := &Conn{id: id, send: make(chan []byte, 10), channels: make(map[string]bool)}
	c.SetIdentity(id, name, isAgent)
	return c
}

func newAuthConn(id, name string, isAgent bool) *Conn {
	c := newTestConn(id, name, isAgent)
	c.authenticated = true
	return c
}

func TestHubAddRemove(t *testing.T) {
	hub := NewHub()
	c := newAuthConn("u1", "Alice", false)

	hub.Add(c)
	if hub.Total() != 1 {
		t.Fatalf("expected 1 total, got %d", hub.Total())
	}
	if hub.GetConn("u1") != c {
		t.Fatal("expected to find conn u1")
	}

	hub.Remove(c)
	if hub.Total() != 0 {
		t.Fatalf("expected 0 total after remove, got %d", hub.Total())
	}
	if hub.GetConn("u1") != nil {
		t.Fatal("expected nil after remove")
	}
}

func TestHubDuplicateAdd(t *testing.T) {
	hub := NewHub()
	old := newAuthConn("u1", "Alice", false)
	newConn := newAuthConn("u1", "Alice-v2", false)

	hub.Add(old)
	if hub.Total() != 1 {
		t.Fatalf("expected 1, got %d", hub.Total())
	}

	hub.Add(newConn)
	if hub.Total() != 1 {
		t.Fatalf("expected 1 after dup add, got %d", hub.Total())
	}
	if hub.GetConn("u1") != newConn {
		t.Fatal("expected new conn to replace old")
	}
}

func TestHubAgentTracking(t *testing.T) {
	hub := NewHub()
	agent := newAuthConn("a1", "Bot", true)
	human := newAuthConn("u1", "Alice", false)

	hub.Add(agent)
	hub.Add(human)

	if hub.AgentCount() != 1 {
		t.Fatalf("expected 1 agent, got %d", hub.AgentCount())
	}

	found := hub.GetAgentByName("Bot")
	if found != agent {
		t.Fatal("expected to find agent by name")
	}
	if hub.GetAgentByName("NonExistent") != nil {
		t.Fatal("expected nil for non-existent agent")
	}

	hub.Remove(agent)
	if hub.AgentCount() != 0 {
		t.Fatalf("expected 0 agents after remove, got %d", hub.AgentCount())
	}
}

func TestHubPresence(t *testing.T) {
	hub := NewHub()
	c := newAuthConn("u1", "Alice", false)
	hub.Add(c)

	presence := hub.GetPresence("u1")
	if presence != "online" {
		t.Fatalf("expected online, got %s", presence)
	}

	hub.SetPresence("u1", "away")
	presence = hub.GetPresence("u1")
	if presence != "away" {
		t.Fatalf("expected away, got %s", presence)
	}
}

func TestHubClose(t *testing.T) {
	hub := NewHub()
	c1 := newAuthConn("u1", "Alice", false)
	c2 := newAuthConn("u2", "Bot", true)

	hub.Add(c1)
	hub.Add(c2)
	if hub.Total() != 2 {
		t.Fatalf("expected 2, got %d", hub.Total())
	}

	hub.Close()
	if hub.Total() != 0 {
		t.Fatalf("expected 0 after close, got %d", hub.Total())
	}
}

func TestHubBroadcastToChannel(t *testing.T) {
	hub := NewHub()
	c1 := newAuthConn("u1", "Alice", false)
	if ok := c1.Subscribe("ch1"); !ok {
		t.Fatal("expected subscribe ch1 to succeed")
	}
	c2 := newAuthConn("u2", "Bob", false)
	if ok := c2.Subscribe("ch2"); !ok {
		t.Fatal("expected subscribe ch2 to succeed")
	}

	hub.Add(c1)
	hub.Add(c2)

	env := NewEnvelope(EventMessageNew, map[string]string{"test": "data"})
	hub.BroadcastToChannel("ch1", env)

	select {
	case msg := <-c1.send:
		var decoded Envelope
		if err := json.Unmarshal(msg, &decoded); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if decoded.Type != EventMessageNew {
			t.Fatalf("expected message.new, got %s", decoded.Type)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for c1 message")
	}

	select {
	case <-c2.send:
		t.Fatal("c2 should not receive ch1 broadcast")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestHubBroadcastToAll(t *testing.T) {
	hub := NewHub()
	c1 := newAuthConn("u1", "Alice", false)
	c2 := newAuthConn("u2", "Bob", false)
	hub.Add(c1)
	hub.Add(c2)

	env := NewEnvelope(EventPresenceUpdate, map[string]string{"member_id": "u1", "status": "online"})
	hub.BroadcastToAll(env)

	for _, c := range []*Conn{c1, c2} {
		select {
		case <-c.send:
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("timeout waiting for %s", c.ID())
		}
	}
}

func TestAgentHelloData(t *testing.T) {
	d := AgentHelloData{
		Name: "bot",
		RoleCard: RoleCard{
			SystemPrompt: "You are helpful",
			Capabilities: []string{"chat"},
		},
		Runtime: RuntimeInfo{
			Type:     "llm",
			Provider: "openai",
			Model:    "gpt-4",
		},
	}
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded AgentHelloData
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Name != "bot" || decoded.Runtime.Provider != "openai" {
		t.Fatalf("mismatch: %+v", decoded)
	}
}

func TestMessageSendData(t *testing.T) {
	d := MessageSendData{
		ChannelID: "ch1",
		Content:   "hello",
		Type:      "text",
	}
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded MessageSendData
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.ChannelID != "ch1" || decoded.Content != "hello" {
		t.Fatalf("mismatch: %+v", decoded)
	}
}

func TestThreadReplyData(t *testing.T) {
	d := ThreadReplyData{
		ChannelID: "ch1",
		ThreadID:  "msg1",
		Content:   "reply",
	}
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded ThreadReplyData
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.ThreadID != "msg1" || decoded.Content != "reply" {
		t.Fatalf("mismatch: %+v", decoded)
	}
}

func TestAuthLoginData(t *testing.T) {
	d := AuthLoginData{Token: "tok123"}
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded AuthLoginData
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Token != "tok123" {
		t.Fatalf("expected tok123, got %s", decoded.Token)
	}
}
