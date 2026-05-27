package api

import (
	"context"
	"testing"

	"lark-daemon/internal/proto"
)

func TestSSOProviderCRUD(t *testing.T) {
	env := newTestEnv(t)
	_, _, wsID := env.createAgent(t)

	// Create admin member (SSO management requires admin)
	admin := &proto.Member{WorkspaceID: wsID, Name: "admin", Type: proto.MemberHuman, Role: proto.RoleAdmin}
	env.store.CreateMember(context.Background(), admin)
	token, _, _ := env.auth.GenerateToken(admin.ID, wsID)

	// Create
	body := map[string]string{
		"name":          "Okta",
		"type":          "oidc",
		"issuer":        "https://okta.com/issuer",
		"client_id":     "abc123",
		"client_secret": "secret456",
		"domain":        "example.com",
	}
	resp := env.doReq(t, "POST", "/v1/workspaces/"+wsID+"/sso", body, token)
	checkOK(t, resp)
	var provider map[string]any
	readJSON(t, resp, &provider)
	providerID := provider["id"].(string)

	// List
	resp = env.doReq(t, "GET", "/v1/workspaces/"+wsID+"/sso", nil, token)
	checkOK(t, resp)
	var providers []map[string]any
	readJSON(t, resp, &providers)
	if len(providers) != 1 {
		t.Fatalf("expected 1, got %d", len(providers))
	}

	// Delete
	resp = env.doReq(t, "DELETE", "/v1/sso/"+providerID, nil, token)
	if resp.StatusCode != 204 {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Verify empty
	resp = env.doReq(t, "GET", "/v1/workspaces/"+wsID+"/sso", nil, token)
	checkOK(t, resp)
	readJSON(t, resp, &providers)
	if len(providers) != 0 {
		t.Fatalf("expected 0 after delete, got %d", len(providers))
	}
}

func TestSSODiscover(t *testing.T) {
	env := newTestEnv(t)
	_, apiKey, wsID := env.createAgent(t)

	// Create provider with domain
	env.store.CreateSSOProvider(context.Background(), &proto.SSOProvider{
		WorkspaceID: wsID,
		Name:        "Okta",
		Type:        "oidc",
		Issuer:      "https://okta.com/issuer",
		ClientID:    "abc123",
		Domain:      "example.com",
		Enabled:     true,
	})

	// Discover by domain (needs auth token)
	resp := env.doReq(t, "GET", "/v1/sso/discover?domain=example.com", nil, apiKey)
	checkOK(t, resp)
	var out map[string]any
	readJSON(t, resp, &out)
	if out["name"] != "Okta" {
		t.Fatalf("expected Okta, got %v", out["name"])
	}

	// Unknown domain
	resp = env.doReq(t, "GET", "/v1/sso/discover?domain=unknown.com", nil, apiKey)
	if resp.StatusCode != 404 {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}
