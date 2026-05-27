package api

import (
	"context"
	"testing"

	"lark-daemon/internal/proto"
)

func TestNotificationListEmpty(t *testing.T) {
	env := newTestEnv(t)
	_, apiKey, _ := env.createAgent(t)

	resp := env.doReq(t, "GET", "/v1/notifications", nil, apiKey)
	checkOK(t, resp)
	var notifs []map[string]any
	readJSON(t, resp, &notifs)
	if len(notifs) != 0 {
		t.Fatalf("expected 0, got %d", len(notifs))
	}
}

func TestNotificationListWithData(t *testing.T) {
	env := newTestEnv(t)
	agentID, apiKey, _ := env.createAgent(t)

	n1 := &proto.Notification{MemberID: agentID, Type: "mention", Title: "Mention", Body: "You were mentioned"}
	env.store.CreateNotification(context.Background(), n1)
	n2 := &proto.Notification{MemberID: agentID, Type: "dm", Title: "DM", Body: "New message"}
	env.store.CreateNotification(context.Background(), n2)

	resp := env.doReq(t, "GET", "/v1/notifications", nil, apiKey)
	checkOK(t, resp)
	var notifs []map[string]any
	readJSON(t, resp, &notifs)
	if len(notifs) != 2 {
		t.Fatalf("expected 2, got %d", len(notifs))
	}
}

func TestNotificationMarkRead(t *testing.T) {
	env := newTestEnv(t)
	agentID, apiKey, _ := env.createAgent(t)

	n := &proto.Notification{MemberID: agentID, Type: "mention", Title: "Mention", Body: "hi"}
	env.store.CreateNotification(context.Background(), n)

	resp := env.doReq(t, "PATCH", "/v1/notifications/"+n.ID+"/read", nil, apiKey)
	if resp.StatusCode != 204 {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	notifs, _ := env.store.ListNotifications(context.Background(), agentID, false, 10)
	if !notifs[0].IsRead {
		t.Fatal("expected notification to be read")
	}
}

func TestNotificationMarkAllRead(t *testing.T) {
	env := newTestEnv(t)
	agentID, apiKey, _ := env.createAgent(t)

	env.store.CreateNotification(context.Background(), &proto.Notification{MemberID: agentID, Type: "mention", Title: "A", Body: "a"})
	env.store.CreateNotification(context.Background(), &proto.Notification{MemberID: agentID, Type: "dm", Title: "B", Body: "b"})

	resp := env.doReq(t, "POST", "/v1/notifications/mark-all-read", nil, apiKey)
	if resp.StatusCode != 204 {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	count, _ := env.store.CountUnreadNotifications(context.Background(), agentID)
	if count != 0 {
		t.Fatalf("expected 0 unread, got %d", count)
	}
}

func TestNotificationUnreadCount(t *testing.T) {
	env := newTestEnv(t)
	agentID, apiKey, _ := env.createAgent(t)

	env.store.CreateNotification(context.Background(), &proto.Notification{MemberID: agentID, Type: "mention", Title: "A", Body: "a"})
	env.store.CreateNotification(context.Background(), &proto.Notification{MemberID: agentID, Type: "dm", Title: "B", Body: "b"})

	resp := env.doReq(t, "GET", "/v1/notifications/unread-count", nil, apiKey)
	checkOK(t, resp)
	var out map[string]any
	readJSON(t, resp, &out)
	if int(out["count"].(float64)) != 2 {
		t.Fatalf("expected 2, got %v", out["count"])
	}
}

func TestNotificationUnreadOnly(t *testing.T) {
	env := newTestEnv(t)
	agentID, apiKey, _ := env.createAgent(t)

	n1 := &proto.Notification{MemberID: agentID, Type: "mention", Title: "A", Body: "a"}
	env.store.CreateNotification(context.Background(), n1)
	env.store.CreateNotification(context.Background(), &proto.Notification{MemberID: agentID, Type: "dm", Title: "B", Body: "b"})

	env.store.MarkNotificationRead(context.Background(), n1.ID)

	resp := env.doReq(t, "GET", "/v1/notifications?unread=true", nil, apiKey)
	checkOK(t, resp)
	var notifs []map[string]any
	readJSON(t, resp, &notifs)
	if len(notifs) != 1 {
		t.Fatalf("expected 1 unread, got %d", len(notifs))
	}
}
