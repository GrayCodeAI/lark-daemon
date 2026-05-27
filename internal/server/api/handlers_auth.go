package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/oauth2"

	"lark-daemon/internal/proto"
)

// oauthStateStore holds short-lived OAuth state values to prevent CSRF.
var oauthStateStore = struct {
	sync.Mutex
	states map[string]time.Time
}{states: make(map[string]time.Time)}

func init() {
	go func() {
		for {
			time.Sleep(5 * time.Minute)
			oauthStateStore.Lock()
			now := time.Now()
			for k, t := range oauthStateStore.states {
				if now.Sub(t) > 10*time.Minute {
					delete(oauthStateStore.states, k)
				}
			}
			oauthStateStore.Unlock()
		}
	}()
}

func generateOAuthState() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// --- Auth handlers ---

func (r *Router) handleRegister(w http.ResponseWriter, req *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Name     string `json:"name"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.Email == "" || body.Password == "" || body.Name == "" {
		writeError(w, http.StatusBadRequest, "email, password, and name required")
		return
	}
	if len(body.Password) < 6 {
		writeError(w, http.StatusBadRequest, "password must be at least 6 characters")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(body.Password), bcrypt.DefaultCost)
	if err != nil {
		serverError(w, err, "password hash failed")
		return
	}
	// Create a personal workspace for the user
	ws := &proto.Workspace{
		Name: body.Name + "'s Workspace",
		Slug: strings.ToLower(strings.ReplaceAll(body.Name, " ", "-")) + "-" + uuid.New().String()[:8],
	}
	if err := r.services.CreateWorkspace(req.Context(), ws); err != nil {
		serverError(w, err, "create workspace failed")
		return
	}
	m := &proto.Member{
		WorkspaceID:  ws.ID,
		Name:         body.Name,
		Email:        body.Email,
		PasswordHash: string(hash),
		Type:         proto.MemberHuman,
		Role:         proto.RoleOwner,
		Status:       proto.PresenceOffline,
	}
	if err := r.services.CreateMember(req.Context(), m); err != nil {
		serverError(w, err, "create member failed")
		return
	}
	token, _, err := r.auth.GenerateToken(m.ID, ws.ID)
	if err != nil {
		serverError(w, err, "generate token failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"token":      token,
		"member_id":  m.ID,
		"workspace":  ws,
	})
}

func (r *Router) handleLogin(w http.ResponseWriter, req *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.Email == "" || body.Password == "" {
		writeError(w, http.StatusBadRequest, "email and password required")
		return
	}
	member, err := r.store.GetMemberByEmail(req.Context(), body.Email)
	if err != nil {
		serverError(w, err, "login failed")
		return
	}
	if member == nil {
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(member.PasswordHash), []byte(body.Password)); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}
	token, _, err := r.auth.GenerateToken(member.ID, member.WorkspaceID)
	if err != nil {
		serverError(w, err, "generate token failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"token":        token,
		"member_id":    member.ID,
		"workspace_id": member.WorkspaceID,
		"name":         member.Name,
	})
}

func (r *Router) handleLogout(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	// Extract JTI from the token in the Authorization header
	authHeader := req.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
		claims, _ := r.auth.ValidateToken(tokenStr, nil) // skip blacklist check for logout
		if claims != nil && claims.ID != "" {
			// Blacklist the token until its natural expiry
			r.store.BlacklistToken(req.Context(), claims.ID, claims.ExpiresAt.UnixMilli())
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "logged out"})
}

// --- OAuth handlers ---

func (r *Router) handleGithubLogin(w http.ResponseWriter, req *http.Request) {
	if r.githubOAuth == nil {
		writeError(w, http.StatusBadRequest, "GitHub OAuth not configured")
		return
	}
	state := generateOAuthState()
	oauthStateStore.Lock()
	oauthStateStore.states[state] = time.Now()
	oauthStateStore.Unlock()
	url := r.githubOAuth.AuthCodeURL(state, oauth2.AccessTypeOffline)
	http.Redirect(w, req, url, http.StatusFound)
}

func (r *Router) handleGithubCallback(w http.ResponseWriter, req *http.Request) {
	if r.githubOAuth == nil {
		writeError(w, http.StatusBadRequest, "GitHub OAuth not configured")
		return
	}
	// Validate state parameter to prevent CSRF
	state := req.URL.Query().Get("state")
	oauthStateStore.Lock()
	createdAt, exists := oauthStateStore.states[state]
	if exists {
		delete(oauthStateStore.states, state)
	}
	oauthStateStore.Unlock()
	if !exists || time.Since(createdAt) > 10*time.Minute {
		writeError(w, http.StatusBadRequest, "invalid or expired OAuth state")
		return
	}
	code := req.URL.Query().Get("code")
	if code == "" {
		writeError(w, http.StatusBadRequest, "missing code")
		return
	}
	tok, err := r.githubOAuth.Exchange(req.Context(), code)
	if err != nil {
		serverError(w, err, "oauth exchange failed")
		return
	}
	// Fetch user info from GitHub
	ghReq, _ := http.NewRequest("GET", "https://api.github.com/user", nil)
	ghReq.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	ghResp, err := (&http.Client{Timeout: 10 * time.Second}).Do(ghReq)
	if err != nil {
		serverError(w, err, "github user fetch failed")
		return
	}
	defer ghResp.Body.Close()
	var ghUser struct {
		ID    int    `json:"id"`
		Login string `json:"login"`
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if err := json.NewDecoder(ghResp.Body).Decode(&ghUser); err != nil {
		serverError(w, err, "github user decode failed")
		return
	}
	email := ghUser.Email
	if email == "" {
		email = fmt.Sprintf("%s@github-user", ghUser.Login)
	}
	name := ghUser.Name
	if name == "" {
		name = ghUser.Login
	}
	// Check if user exists
	member, err := r.store.GetMemberByEmail(req.Context(), email)
	if err != nil {
		serverError(w, err, "lookup failed")
		return
	}
	if member == nil {
		// Create workspace + member
		ws := &proto.Workspace{
			Name: name + "'s Workspace",
			Slug: ghUser.Login + "-" + uuid.New().String()[:8],
		}
		if err := r.services.CreateWorkspace(req.Context(), ws); err != nil {
			serverError(w, err, "create workspace failed")
			return
		}
		member = &proto.Member{
			WorkspaceID: ws.ID,
			Name:        name,
			Email:       email,
			Type:        proto.MemberHuman,
			Status:      proto.PresenceOffline,
		}
		if err := r.services.CreateMember(req.Context(), member); err != nil {
			serverError(w, err, "create member failed")
			return
		}
	}
	token, _, err := r.auth.GenerateToken(member.ID, member.WorkspaceID)
	if err != nil {
		serverError(w, err, "generate token failed")
		return
	}
	// Redirect to frontend with token in fragment (not query param) to avoid leaking in logs/Referer
	redirectURL := req.URL.Query().Get("redirect")
	if redirectURL == "" || !strings.HasPrefix(redirectURL, "/") || strings.HasPrefix(redirectURL, "//") {
		redirectURL = "/"
	}
	http.Redirect(w, req, redirectURL+"#token="+token, http.StatusFound)
}

// --- Google OAuth ---

func (r *Router) handleGoogleLogin(w http.ResponseWriter, req *http.Request) {
	if r.googleOAuth == nil {
		writeError(w, http.StatusBadRequest, "Google OAuth not configured")
		return
	}
	state := generateOAuthState()
	oauthStateStore.Lock()
	oauthStateStore.states[state] = time.Now()
	oauthStateStore.Unlock()
	url := r.googleOAuth.AuthCodeURL(state, oauth2.AccessTypeOffline)
	http.Redirect(w, req, url, http.StatusFound)
}

func (r *Router) handleGoogleCallback(w http.ResponseWriter, req *http.Request) {
	if r.googleOAuth == nil {
		writeError(w, http.StatusBadRequest, "Google OAuth not configured")
		return
	}
	state := req.URL.Query().Get("state")
	oauthStateStore.Lock()
	createdAt, exists := oauthStateStore.states[state]
	if exists {
		delete(oauthStateStore.states, state)
	}
	oauthStateStore.Unlock()
	if !exists || time.Since(createdAt) > 10*time.Minute {
		writeError(w, http.StatusBadRequest, "invalid or expired OAuth state")
		return
	}
	code := req.URL.Query().Get("code")
	if code == "" {
		writeError(w, http.StatusBadRequest, "missing code")
		return
	}
	tok, err := r.googleOAuth.Exchange(req.Context(), code)
	if err != nil {
		serverError(w, err, "google oauth exchange failed")
		return
	}
	gReq, _ := http.NewRequest("GET", "https://www.googleapis.com/oauth2/v2/userinfo", nil)
	gReq.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	gResp, err := (&http.Client{Timeout: 10 * time.Second}).Do(gReq)
	if err != nil {
		serverError(w, err, "google user fetch failed")
		return
	}
	defer gResp.Body.Close()
	var gUser struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if err := json.NewDecoder(gResp.Body).Decode(&gUser); err != nil {
		serverError(w, err, "google user decode failed")
		return
	}
	if gUser.Email == "" {
		writeError(w, http.StatusBadRequest, "no email from Google")
		return
	}
	r.handleOAuthCallback(w, req, "google", gUser.ID, gUser.Email, gUser.Name)
}

// --- Microsoft OAuth ---

func (r *Router) handleMicrosoftLogin(w http.ResponseWriter, req *http.Request) {
	if r.microsoftOAuth == nil {
		writeError(w, http.StatusBadRequest, "Microsoft OAuth not configured")
		return
	}
	state := generateOAuthState()
	oauthStateStore.Lock()
	oauthStateStore.states[state] = time.Now()
	oauthStateStore.Unlock()
	url := r.microsoftOAuth.AuthCodeURL(state, oauth2.AccessTypeOffline)
	http.Redirect(w, req, url, http.StatusFound)
}

func (r *Router) handleMicrosoftCallback(w http.ResponseWriter, req *http.Request) {
	if r.microsoftOAuth == nil {
		writeError(w, http.StatusBadRequest, "Microsoft OAuth not configured")
		return
	}
	state := req.URL.Query().Get("state")
	oauthStateStore.Lock()
	createdAt, exists := oauthStateStore.states[state]
	if exists {
		delete(oauthStateStore.states, state)
	}
	oauthStateStore.Unlock()
	if !exists || time.Since(createdAt) > 10*time.Minute {
		writeError(w, http.StatusBadRequest, "invalid or expired OAuth state")
		return
	}
	code := req.URL.Query().Get("code")
	if code == "" {
		writeError(w, http.StatusBadRequest, "missing code")
		return
	}
	tok, err := r.microsoftOAuth.Exchange(req.Context(), code)
	if err != nil {
		serverError(w, err, "microsoft oauth exchange failed")
		return
	}
	msReq, _ := http.NewRequest("GET", "https://graph.microsoft.com/v1.0/me", nil)
	msReq.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	msResp, err := (&http.Client{Timeout: 10 * time.Second}).Do(msReq)
	if err != nil {
		serverError(w, err, "microsoft user fetch failed")
		return
	}
	defer msResp.Body.Close()
	var msUser struct {
		ID    string `json:"id"`
		Name  string `json:"displayName"`
		Email string `json:"mail"`
	}
	if err := json.NewDecoder(msResp.Body).Decode(&msUser); err != nil {
		serverError(w, err, "microsoft user decode failed")
		return
	}
	if msUser.Email == "" {
		msUser.Email = msUser.ID + "@microsoft-user"
	}
	r.handleOAuthCallback(w, req, "microsoft", msUser.ID, msUser.Email, msUser.Name)
}

// --- Shared OAuth callback logic ---

func (r *Router) handleOAuthCallback(w http.ResponseWriter, req *http.Request, provider, providerUserID, email, name string) {
	// 1. Look up by provider identity
	identity, err := r.services.GetOAuthIdentity(req.Context(), provider, providerUserID)
	if err != nil {
		serverError(w, err, "oauth lookup failed")
		return
	}
	var member *proto.Member
	if identity != nil {
		member, err = r.services.GetMember(req.Context(), identity.MemberID)
		if err != nil {
			serverError(w, err, "member lookup failed")
			return
		}
	}
	// 2. Look up by email
	if member == nil {
		member, err = r.store.GetMemberByEmail(req.Context(), email)
		if err != nil {
			serverError(w, err, "email lookup failed")
			return
		}
		if member != nil {
			// Link provider to existing member
			_ = r.services.CreateOAuthIdentity(req.Context(), &proto.OAuthIdentity{
				MemberID:       member.ID,
				Provider:       provider,
				ProviderUserID: providerUserID,
				Email:          email,
			})
		}
	}
	// 3. Create new workspace + member
	if member == nil {
		if name == "" {
			name = email
		}
		ws := &proto.Workspace{
			Name: name + "'s Workspace",
			Slug: provider + "-" + uuid.New().String()[:8],
		}
		if err := r.services.CreateWorkspace(req.Context(), ws); err != nil {
			serverError(w, err, "create workspace failed")
			return
		}
		member = &proto.Member{
			WorkspaceID: ws.ID,
			Name:        name,
			Email:       email,
			Type:        proto.MemberHuman,
			Status:      proto.PresenceOffline,
		}
		if err := r.services.CreateMember(req.Context(), member); err != nil {
			serverError(w, err, "create member failed")
			return
		}
		_ = r.services.CreateOAuthIdentity(req.Context(), &proto.OAuthIdentity{
			MemberID:       member.ID,
			Provider:       provider,
			ProviderUserID: providerUserID,
			Email:          email,
		})
	}
	token, _, err := r.auth.GenerateToken(member.ID, member.WorkspaceID)
	if err != nil {
		serverError(w, err, "generate token failed")
		return
	}
	redirectURL := req.URL.Query().Get("redirect")
	if redirectURL == "" || !strings.HasPrefix(redirectURL, "/") || strings.HasPrefix(redirectURL, "//") {
		redirectURL = "/"
	}
	http.Redirect(w, req, redirectURL+"#token="+token, http.StatusFound)
}

// --- Providers list ---

func (r *Router) handleListProviders(w http.ResponseWriter, req *http.Request) {
	var providers []string
	if r.githubOAuth != nil {
		providers = append(providers, "github")
	}
	if r.googleOAuth != nil {
		providers = append(providers, "google")
	}
	if r.microsoftOAuth != nil {
		providers = append(providers, "microsoft")
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": providers})
}
