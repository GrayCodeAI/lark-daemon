package api

import (
	"testing"
)

func TestE2EEKeyCRUD(t *testing.T) {
	env := newTestEnv(t)
	agentID, apiKey, _ := env.createAgent(t)

	// Register key
	key := map[string]string{
		"key_type":   "identity",
		"public_key": "base64encodedkey",
		"algorithm":  "curve25519",
	}
	resp := env.doReq(t, "POST", "/v1/keys", key, apiKey)
	checkOK(t, resp)
	var keyResp map[string]any
	readJSON(t, resp, &keyResp)
	keyID := keyResp["id"].(string)

	// List keys
	resp = env.doReq(t, "GET", "/v1/members/"+agentID+"/keys?type=identity", nil, apiKey)
	checkOK(t, resp)
	var keys []map[string]any
	readJSON(t, resp, &keys)
	if len(keys) != 1 {
		t.Fatalf("expected 1, got %d", len(keys))
	}

	// Delete key
	resp = env.doReq(t, "DELETE", "/v1/keys/"+keyID, nil, apiKey)
	if resp.StatusCode != 204 {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Verify gone
	resp = env.doReq(t, "GET", "/v1/members/"+agentID+"/keys?type=identity", nil, apiKey)
	checkOK(t, resp)
	readJSON(t, resp, &keys)
	if len(keys) != 0 {
		t.Fatalf("expected 0 after delete, got %d", len(keys))
	}
}

func TestE2EEMultipleKeyTypes(t *testing.T) {
	env := newTestEnv(t)
	agentID, apiKey, _ := env.createAgent(t)

	env.doReq(t, "POST", "/v1/keys", map[string]string{"key_type": "identity", "public_key": "k1"}, apiKey).Body.Close()
	env.doReq(t, "POST", "/v1/keys", map[string]string{"key_type": "signed_pre", "public_key": "k2"}, apiKey).Body.Close()
	env.doReq(t, "POST", "/v1/keys", map[string]string{"key_type": "one_time", "public_key": "k3"}, apiKey).Body.Close()

	resp := env.doReq(t, "GET", "/v1/members/"+agentID+"/keys?type=identity", nil, apiKey)
	checkOK(t, resp)
	var keys []map[string]any
	readJSON(t, resp, &keys)
	if len(keys) != 1 {
		t.Fatalf("expected 1 identity key, got %d", len(keys))
	}

	resp = env.doReq(t, "GET", "/v1/members/"+agentID+"/keys?type=signed_pre", nil, apiKey)
	checkOK(t, resp)
	readJSON(t, resp, &keys)
	if len(keys) != 1 {
		t.Fatalf("expected 1 signed_pre key, got %d", len(keys))
	}
}

func TestE2EEGetPublicKeys(t *testing.T) {
	env := newTestEnv(t)
	agentID, apiKey, _ := env.createAgent(t)

	// Register a key first
	env.doReq(t, "POST", "/v1/keys", map[string]string{"key_type": "identity", "public_key": "mykey"}, apiKey).Body.Close()

	// Get public keys for the member (different member querying)
	resp := env.doReq(t, "GET", "/v1/members/"+agentID+"/keys?type=identity", nil, apiKey)
	checkOK(t, resp)
	var keys []map[string]any
	readJSON(t, resp, &keys)
	if len(keys) != 1 {
		t.Fatalf("expected 1, got %d", len(keys))
	}
	if keys[0]["public_key"] != "mykey" {
		t.Fatalf("expected mykey, got %v", keys[0]["public_key"])
	}
}
