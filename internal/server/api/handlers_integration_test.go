package api

import (
	"context"
	"testing"

	"lark-daemon/internal/proto"
)

func TestIntegrationListCatalog(t *testing.T) {
	env := newTestEnv(t)
	_, apiKey, _ := env.createAgent(t)

	// Seed an integration (type must be webhook/bot/oauth/custom)
	env.store.CreateIntegration(context.Background(), &proto.Integration{Name: "GitHub", Type: "bot", Description: "GitHub integration"})

	resp := env.doReq(t, "GET", "/v1/integrations", nil, apiKey)
	checkOK(t, resp)
	var integrations []map[string]any
	readJSON(t, resp, &integrations)
	if len(integrations) != 1 {
		t.Fatalf("expected 1, got %d", len(integrations))
	}
}

func TestIntegrationGet(t *testing.T) {
	env := newTestEnv(t)
	_, apiKey, _ := env.createAgent(t)

	integ := &proto.Integration{Name: "Slack", Type: "webhook"}
	env.store.CreateIntegration(context.Background(), integ)

	resp := env.doReq(t, "GET", "/v1/integrations/"+integ.ID, nil, apiKey)
	checkOK(t, resp)
	var out map[string]any
	readJSON(t, resp, &out)
	if out["name"] != "Slack" {
		t.Fatalf("expected Slack, got %v", out["name"])
	}
}

func TestIntegrationCreate(t *testing.T) {
	env := newTestEnv(t)
	_, _, wsID := env.createAgent(t)

	// Create admin member (integration creation requires admin)
	admin := &proto.Member{WorkspaceID: wsID, Name: "admin", Type: proto.MemberHuman, Role: proto.RoleAdmin}
	env.store.CreateMember(context.Background(), admin)
	token, _, _ := env.auth.GenerateToken(admin.ID, wsID)

	body := map[string]string{"name": "Jira", "type": "custom", "description": "Jira integration"}
	resp := env.doReq(t, "POST", "/v1/integrations", body, token)
	checkOK(t, resp)
	var out map[string]any
	readJSON(t, resp, &out)
	if out["name"] != "Jira" {
		t.Fatalf("expected Jira, got %v", out["name"])
	}
}

func TestWorkspaceIntegrationInstallUninstall(t *testing.T) {
	env := newTestEnv(t)
	_, apiKey, wsID := env.createAgent(t)

	integ := &proto.Integration{Name: "GitHub", Type: "bot"}
	env.store.CreateIntegration(context.Background(), integ)

	// Install
	body := map[string]string{"integration_id": integ.ID}
	resp := env.doReq(t, "POST", "/v1/workspaces/"+wsID+"/integrations", body, apiKey)
	checkOK(t, resp)
	resp.Body.Close()

	// List installed
	resp = env.doReq(t, "GET", "/v1/workspaces/"+wsID+"/integrations", nil, apiKey)
	checkOK(t, resp)
	var installed []map[string]any
	readJSON(t, resp, &installed)
	if len(installed) != 1 {
		t.Fatalf("expected 1 installed, got %d", len(installed))
	}

	// Uninstall
	resp = env.doReq(t, "DELETE", "/v1/workspaces/"+wsID+"/integrations/"+integ.ID, nil, apiKey)
	if resp.StatusCode != 204 {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Verify empty
	resp = env.doReq(t, "GET", "/v1/workspaces/"+wsID+"/integrations", nil, apiKey)
	checkOK(t, resp)
	readJSON(t, resp, &installed)
	if len(installed) != 0 {
		t.Fatalf("expected 0 after uninstall, got %d", len(installed))
	}
}

func TestWorkspaceIntegrationDuplicateInstall(t *testing.T) {
	env := newTestEnv(t)
	_, apiKey, wsID := env.createAgent(t)

	integ := &proto.Integration{Name: "GitHub", Type: "bot"}
	env.store.CreateIntegration(context.Background(), integ)

	body := map[string]string{"integration_id": integ.ID}
	env.doReq(t, "POST", "/v1/workspaces/"+wsID+"/integrations", body, apiKey).Body.Close()

	// Second install should fail
	resp := env.doReq(t, "POST", "/v1/workspaces/"+wsID+"/integrations", body, apiKey)
	if resp.StatusCode != 409 {
		t.Fatalf("expected 409 for duplicate, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}
