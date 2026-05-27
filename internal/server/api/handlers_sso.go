package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"lark-daemon/internal/proto"
)

// --- SSO Provider management ---

func (r *Router) handleListSSOProviders(w http.ResponseWriter, req *http.Request) {
	workspaceID := chi.URLParam(req, "id")
	providers, err := r.services.ListSSOProviders(req.Context(), workspaceID)
	if err != nil {
		serverError(w, err, "list sso providers")
		return
	}
	writeJSON(w, http.StatusOK, providers)
}

func (r *Router) handleCreateSSOProvider(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	if member.Role != proto.RoleAdmin && member.Role != proto.RoleOwner {
		writeError(w, http.StatusForbidden, "admin required")
		return
	}
	workspaceID := chi.URLParam(req, "id")
	var body struct {
		Name         string `json:"name"`
		Type         string `json:"type"`
		Issuer       string `json:"issuer"`
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
		DiscoveryURL string `json:"discovery_url"`
		Domain       string `json:"domain"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.Name == "" || body.Type == "" {
		writeError(w, http.StatusBadRequest, "name and type required")
		return
	}
	if body.Type == "oidc" && (body.ClientID == "" || body.ClientSecret == "") {
		writeError(w, http.StatusBadRequest, "client_id and client_secret required for OIDC")
		return
	}
	p := &proto.SSOProvider{
		WorkspaceID:  workspaceID,
		Name:         body.Name,
		Type:         proto.SSOProviderType(body.Type),
		Issuer:       body.Issuer,
		ClientID:     body.ClientID,
		ClientSecret: body.ClientSecret,
		DiscoveryURL: body.DiscoveryURL,
		Domain:       body.Domain,
		Enabled:      true,
	}
	if err := r.services.CreateSSOProvider(req.Context(), p); err != nil {
		serverError(w, err, "create sso provider")
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (r *Router) handleDeleteSSOProvider(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	if member.Role != proto.RoleAdmin && member.Role != proto.RoleOwner {
		writeError(w, http.StatusForbidden, "admin required")
		return
	}
	id := chi.URLParam(req, "id")
	if err := r.services.DeleteSSOProvider(req.Context(), id); err != nil {
		serverError(w, err, "delete sso provider")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleSSODiscover returns the SSO provider config for a given domain.
// Used by the frontend to detect if SSO is available before showing the SSO login button.
func (r *Router) handleSSODiscover(w http.ResponseWriter, req *http.Request) {
	domain := req.URL.Query().Get("domain")
	if domain == "" {
		writeError(w, http.StatusBadRequest, "domain required")
		return
	}
	provider, err := r.services.GetSSOProviderByDomain(req.Context(), domain)
	if err != nil {
		serverError(w, err, "sso discover")
		return
	}
	if provider == nil {
		writeError(w, http.StatusNotFound, "no sso provider for domain")
		return
	}
	// Don't expose client_secret
	provider.ClientSecret = ""
	writeJSON(w, http.StatusOK, provider)
}

// handleSSOCallback handles the OIDC callback after user authenticates with the identity provider.
func (r *Router) handleSSOCallback(w http.ResponseWriter, req *http.Request) {
	code := req.URL.Query().Get("code")
	state := req.URL.Query().Get("state")
	if code == "" || state == "" {
		writeError(w, http.StatusBadRequest, "code and state required")
		return
	}

	// Parse state to get provider ID and workspace ID
	stateData, err := url.QueryUnescape(state)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid state")
		return
	}
	var stateInfo struct {
		ProviderID  string `json:"provider_id"`
		WorkspaceID string `json:"workspace_id"`
		RedirectURI string `json:"redirect_uri"`
	}
	if err := json.Unmarshal([]byte(stateData), &stateInfo); err != nil {
		writeError(w, http.StatusBadRequest, "invalid state format")
		return
	}

	// Get the SSO provider
	provider, err := r.services.GetSSOProvider(req.Context(), stateInfo.ProviderID)
	if err != nil || provider == nil {
		writeError(w, http.StatusBadRequest, "provider not found")
		return
	}

	// Exchange authorization code for tokens
	tokenEndpoint := provider.Issuer + "/protocol/openid-connect/token"
	if provider.DiscoveryURL != "" {
		// Use discovery URL to get token endpoint
		discovered, err := r.discoverOIDCEndpoints(provider.DiscoveryURL)
		if err == nil && discovered.TokenEndpoint != "" {
			tokenEndpoint = discovered.TokenEndpoint
		}
	}

	tokenResp, err := r.exchangeCodeForTokens(req.Context(), tokenEndpoint, code, provider.ClientID, provider.ClientSecret, stateInfo.RedirectURI)
	if err != nil {
		writeError(w, http.StatusBadGateway, "token exchange failed: "+err.Error())
		return
	}

	// Get user info from the ID token or userinfo endpoint
	userInfo, err := r.getUserInfo(req.Context(), provider, tokenResp.AccessToken)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to get user info: "+err.Error())
		return
	}

	// Find or create member
	member, err := r.findOrCreateSSOMember(req.Context(), stateInfo.WorkspaceID, provider, userInfo)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create member: "+err.Error())
		return
	}

	// Generate JWT token
	token, _, err := r.auth.GenerateToken(member.ID, stateInfo.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	// Redirect to frontend with token
	redirectURL := stateInfo.RedirectURI
	if redirectURL == "" {
		redirectURL = "/"
	}
separator := "?"
if strings.Contains(redirectURL, "?") {
	separator = "&"
}
http.Redirect(w, req, redirectURL+separator+"token="+token, http.StatusTemporaryRedirect)
}

type oidcEndpoints struct {
	TokenEndpoint  string `json:"token_endpoint"`
	UserInfoEndpoint string `json:"userinfo_endpoint"`
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	IDToken     string `json:"id_token"`
	TokenType   string `json:"token_type"`
}

type oidcUserInfo struct {
	Sub     string `json:"sub"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	Picture string `json:"picture"`
}

// discoverOIDCEndpoints discovers OIDC endpoints from the discovery URL.
func (r *Router) discoverOIDCEndpoints(discoveryURL string) (*oidcEndpoints, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", discoveryURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var endpoints oidcEndpoints
	if err := json.NewDecoder(resp.Body).Decode(&endpoints); err != nil {
		return nil, err
	}
	return &endpoints, nil
}

// exchangeCodeForTokens exchanges an authorization code for tokens.
func (r *Router) exchangeCodeForTokens(ctx context.Context, tokenEndpoint, code, clientID, clientSecret, redirectURI string) (*tokenResponse, error) {
	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("code", code)
	data.Set("client_id", clientID)
	data.Set("client_secret", clientSecret)
	if redirectURI != "" {
		data.Set("redirect_uri", redirectURI)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", tokenEndpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("token exchange failed (%d): %s", resp.StatusCode, string(body))
	}
	var tokenResp tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, err
	}
	return &tokenResp, nil
}

// getUserInfo gets user info from the OIDC provider.
func (r *Router) getUserInfo(ctx context.Context, provider *proto.SSOProvider, accessToken string) (*oidcUserInfo, error) {
	userInfoEndpoint := provider.Issuer + "/protocol/openid-connect/userinfo"
	if provider.DiscoveryURL != "" {
		discovered, err := r.discoverOIDCEndpoints(provider.DiscoveryURL)
		if err == nil && discovered.UserInfoEndpoint != "" {
			userInfoEndpoint = discovered.UserInfoEndpoint
		}
	}

	req, err := http.NewRequestWithContext(ctx, "GET", userInfoEndpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("userinfo request failed (%d): %s", resp.StatusCode, string(body))
	}
	var userInfo oidcUserInfo
	if err := json.NewDecoder(resp.Body).Decode(&userInfo); err != nil {
		return nil, err
	}
	return &userInfo, nil
}

// findOrCreateSSOMember finds an existing member by SSO identity or creates a new one.
func (r *Router) findOrCreateSSOMember(ctx context.Context, workspaceID string, provider *proto.SSOProvider, userInfo *oidcUserInfo) (*proto.Member, error) {
	// Check if OAuth identity exists
	identity, err := r.services.GetOAuthIdentity(ctx, "sso:"+provider.ID, userInfo.Sub)
	if err != nil {
		return nil, err
	}
	if identity != nil {
		// Existing member
		member, err := r.services.GetMember(ctx, identity.MemberID)
		if err != nil || member == nil {
			return nil, fmt.Errorf("member not found")
		}
		return member, nil
	}

	// Create new member
	name := userInfo.Name
	if name == "" {
		name = userInfo.Email
		if name == "" {
			name = "SSO User"
		}
	}
	member := &proto.Member{
		WorkspaceID: workspaceID,
		Name:        name,
		Type:        proto.MemberHuman,
		Role:        proto.RoleUser,
	}
	if err := r.services.CreateMember(ctx, member); err != nil {
		return nil, err
	}

	// Create OAuth identity
	oauth := &proto.OAuthIdentity{
		MemberID:       member.ID,
		Provider:       "sso:" + provider.ID,
		ProviderUserID: userInfo.Sub,
	}
	if err := r.services.CreateOAuthIdentity(ctx, oauth); err != nil {
		// Non-fatal: member already created
		r.logger.Error("failed to create oauth identity", "error", err)
	}

	return member, nil
}
