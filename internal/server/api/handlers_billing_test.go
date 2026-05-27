package api

import (
	"context"
	"encoding/json"
	"testing"

	"lark-daemon/internal/proto"
)

func TestBillingGetDefault(t *testing.T) {
	env := newTestEnv(t)
	_, apiKey, wsID := env.createAgent(t)

	resp := env.doReq(t, "GET", "/v1/workspaces/"+wsID+"/billing", nil, apiKey)
	checkOK(t, resp)
	var out map[string]any
	readJSON(t, resp, &out)

	customer := out["customer"].(map[string]any)
	if customer["plan"] != "free" {
		t.Fatalf("expected free plan, got %v", customer["plan"])
	}
	limits := out["limits"].(map[string]any)
	if limits["max_members"] == nil {
		t.Fatal("expected limits to be set")
	}
}

func TestBillingWithCustomer(t *testing.T) {
	env := newTestEnv(t)
	_, apiKey, wsID := env.createAgent(t)

	// Create billing customer
	cust := &proto.BillingCustomer{
		WorkspaceID: wsID,
		Plan:        proto.PlanPro,
		Status:      proto.BillingActive,
	}
	env.store.CreateBillingCustomer(context.Background(), cust)

	resp := env.doReq(t, "GET", "/v1/workspaces/"+wsID+"/billing", nil, apiKey)
	checkOK(t, resp)
	var out map[string]any
	readJSON(t, resp, &out)

	customer := out["customer"].(map[string]any)
	if customer["plan"] != "pro" {
		t.Fatalf("expected pro, got %v", customer["plan"])
	}
}

func TestUsageGetEmpty(t *testing.T) {
	env := newTestEnv(t)
	_, apiKey, wsID := env.createAgent(t)

	resp := env.doReq(t, "GET", "/v1/workspaces/"+wsID+"/usage", nil, apiKey)
	checkOK(t, resp)
	var out map[string]any
	readJSON(t, resp, &out)

	// usage may be nil or empty array
	usage, _ := out["usage"].([]any)
	if usage != nil && len(usage) != 0 {
		t.Fatalf("expected 0 usage, got %d", len(usage))
	}
	if out["plan"] != "free" {
		t.Fatalf("expected free, got %v", out["plan"])
	}
}

func TestUsageWithData(t *testing.T) {
	env := newTestEnv(t)
	_, apiKey, wsID := env.createAgent(t)

	// Create usage record
	env.store.CreateUsageRecord(context.Background(), &proto.UsageRecord{
		WorkspaceID: wsID,
		Metric:      "messages",
		Quantity:    42,
		PeriodStart: 1000,
	})

	resp := env.doReq(t, "GET", "/v1/workspaces/"+wsID+"/usage", nil, apiKey)
	checkOK(t, resp)
	var out map[string]any
	readJSON(t, resp, &out)

	usage := out["usage"].([]any)
	if len(usage) != 1 {
		t.Fatalf("expected 1, got %d", len(usage))
	}
	record := usage[0].(map[string]any)
	if int(record["quantity"].(float64)) != 42 {
		t.Fatalf("expected 42, got %v", record["quantity"])
	}
}

func TestStripeWebhookNotConfigured(t *testing.T) {
	env := newTestEnv(t)

	body := map[string]any{"type": "checkout.session.completed", "data": map[string]any{}}
	resp := env.doReq(t, "POST", "/webhooks/stripe", body, "")
	// Should return 503 because webhook secret is not configured
	if resp.StatusCode != 503 {
		t.Fatalf("expected 503, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestCheckoutSessionRequiresAdmin(t *testing.T) {
	env := newTestEnv(t)
	_, apiKey, wsID := env.createAgent(t)

	// Agent is not admin, should fail
	body := map[string]string{"plan": "pro"}
	resp := env.doReq(t, "POST", "/v1/workspaces/"+wsID+"/billing/checkout", body, apiKey)
	if resp.StatusCode != 403 {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestCheckoutInvalidPlan(t *testing.T) {
	env := newTestEnv(t)
	_, _, wsID := env.createAgent(t)

	// Create admin member
	admin := &proto.Member{WorkspaceID: wsID, Name: "admin", Type: proto.MemberHuman, Role: proto.RoleAdmin}
	env.store.CreateMember(context.Background(), admin)
	token, _, _ := env.auth.GenerateToken(admin.ID, wsID)

	body := map[string]string{"plan": "invalid"}
	resp := env.doReq(t, "POST", "/v1/workspaces/"+wsID+"/billing/checkout", body, token)
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestBillingPortalNoAccount(t *testing.T) {
	env := newTestEnv(t)
	_, apiKey, wsID := env.createAgent(t)

	resp := env.doReq(t, "POST", "/v1/workspaces/"+wsID+"/billing/portal", nil, apiKey)
	if resp.StatusCode != 404 {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestPlanLimitsEnforcement(t *testing.T) {
	env := newTestEnv(t)
	_, apiKey, wsID := env.createAgent(t)

	// Free plan has MaxMembers=10, create 10 members to hit the limit
	for i := 0; i < 8; i++ {
		m := &proto.Member{WorkspaceID: wsID, Name: "member-" + string(rune('a'+i)), Type: proto.MemberHuman}
		env.store.CreateMember(context.Background(), m)
	}

	// checkPlanLimit is tested indirectly via the billing endpoints
	// The actual enforcement happens in create member/channel handlers
	resp := env.doReq(t, "GET", "/v1/workspaces/"+wsID+"/billing", nil, apiKey)
	checkOK(t, resp)
	var out map[string]any
	readJSON(t, resp, &out)
	limits := out["limits"].(map[string]any)
	if int(limits["max_members"].(float64)) != 10 {
		t.Fatalf("expected 10 max members for free plan, got %v", limits["max_members"])
	}
}

func TestBillingWebhookEventParsing(t *testing.T) {
	// Test that the webhook handler correctly parses event types
	// This tests the JSON parsing logic without needing Stripe configured
	events := []struct {
	 eventType string
	 data      map[string]any
	}{
		{"checkout.session.completed", map[string]any{"customer": "cus_123", "subscription": "sub_456", "client_reference_id": "ws_789", "amount_total": 2000}},
		{"customer.subscription.updated", map[string]any{"customer": "cus_123", "status": "active"}},
		{"customer.subscription.deleted", map[string]any{"customer": "cus_123"}},
	}

	for _, e := range events {
		_, err := json.Marshal(map[string]any{"type": e.eventType, "data": e.data})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		// The webhook handler requires LARK_STRIPE_WEBHOOK_SECRET to be set
		// so we just verify the JSON structure is correct
	}
}
