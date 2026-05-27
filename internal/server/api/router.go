package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"lark-daemon/internal/proto"
	"lark-daemon/internal/server/metrics"
	"lark-daemon/internal/server/service"
	"lark-daemon/internal/server/store"
	"lark-daemon/internal/server/websocket"
)

// RateLimiter provides simple per-IP rate limiting.
type RateLimiter struct {
	mu      sync.Mutex
	clients map[string]*clientBucket
	limit   int
}

type clientBucket struct {
	tokens    int
	lastCheck time.Time
}

func NewRateLimiter(limit int) *RateLimiter {
	return &RateLimiter{
		clients: make(map[string]*clientBucket),
		limit:   limit,
	}
}

func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	b, ok := rl.clients[ip]
	if !ok {
		b = &clientBucket{tokens: rl.limit, lastCheck: time.Now()}
		rl.clients[ip] = b
	}
	elapsed := time.Since(b.lastCheck).Seconds()
	b.lastCheck = time.Now()
	b.tokens += int(elapsed)
	if b.tokens > rl.limit {
		b.tokens = rl.limit
	}
	if b.tokens <= 0 {
		return false
	}
	b.tokens--
	return true
}

// rateLimitMiddleware returns an HTTP handler that rate-limits per IP.
func rateLimitMiddleware(rl *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := r.RemoteAddr
			if idx := strings.LastIndex(ip, ":"); idx != -1 {
				ip = ip[:idx]
			}
			if !rl.Allow(ip) {
				writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Router wraps chi.Router with Lark-specific handlers.
type Router struct {
	chi.Router
	services     *service.Services
	store        store.Store
	agentStore   websocket.AgentStore
	hub          *websocket.Hub
	auth         *websocket.AuthService
	logger       *slog.Logger
	corsOrigin   string
	agentManager *websocket.AgentManager
	collector    *metrics.Collector
	rateLimiter  *RateLimiter
}

// NewRouter creates a new API router with all routes registered.
func NewRouter(services *service.Services, st store.Store, hub *websocket.Hub, auth *websocket.AuthService, logger *slog.Logger, agentStore websocket.AgentStore, corsOrigin string, collector *metrics.Collector, rl *RateLimiter) *Router {
	r := &Router{
		Router:       chi.NewRouter(),
		services:     services,
		store:        st,
		agentStore:   agentStore,
		hub:          hub,
		auth:         auth,
		logger:       logger,
		corsOrigin:   corsOrigin,
		agentManager: websocket.NewAgentManager(hub, agentStore, logger),
		collector:    collector,
		rateLimiter:  rl,
	}
	// Forward the hub's wake callback to the agent manager so HandleAgentHello also records metrics
	r.agentManager.SetWakeCallback(hub.WakeCallback())
	r.setupMiddleware()
	r.setupRoutes()
	return r
}

func (r *Router) setupMiddleware() {
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(requestLoggerMiddleware(r.logger))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{r.corsOrigin},
		AllowedMethods:   []string{"GET", "POST", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: false,
		MaxAge:           300,
	}))
	r.Use(rateLimitMiddleware(r.rateLimiter))
}

// requestLoggerMiddleware logs every request with method, path, status, and duration.
func requestLoggerMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			logger.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"duration", time.Since(start).String(),
			)
		})
	}
}

// authenticate middleware validates JWT or API key from the Authorization header.
// If valid, it sets the member in the request context.
func (r *Router) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		authHeader := req.Header.Get("Authorization")
		if authHeader == "" {
			writeError(w, http.StatusUnauthorized, "missing Authorization header")
			return
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token == authHeader {
			// Try as API key directly
			token = strings.TrimPrefix(authHeader, "ApiKey ")
			if token == authHeader {
				writeError(w, http.StatusUnauthorized, "invalid Authorization format")
				return
			}
		}

		// Try JWT first
		claims, err := r.auth.ValidateToken(token)
		if err == nil {
			member, err := r.store.GetMember(req.Context(), claims.MemberID)
			if err != nil {
				r.logger.Error("auth: get member by JWT", "err", err)
				writeError(w, http.StatusInternalServerError, "internal error")
				return
			}
			if member == nil {
				writeError(w, http.StatusUnauthorized, "member not found")
				return
			}
			ctx := context.WithValue(req.Context(), contextKeyMember, member)
			next.ServeHTTP(w, req.WithContext(ctx))
			return
		}

		// Try API key
		member, err := r.store.GetMemberByAPIKey(req.Context(), token)
		if err != nil {
			r.logger.Error("auth: get member by API key", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if member == nil {
			writeError(w, http.StatusUnauthorized, "invalid credentials")
			return
		}
		ctx := context.WithValue(req.Context(), contextKeyMember, member)
		next.ServeHTTP(w, req.WithContext(ctx))
	})
}

// contextKey is an unexported type for context keys in this package.
type contextKey string

const contextKeyMember contextKey = "member"

func memberFromContext(req *http.Request) *proto.Member {
	m, _ := req.Context().Value(contextKeyMember).(*proto.Member)
	return m
}

// requireWorkspaceAuth returns the authenticated member or sends an error response.
// If workspaceID is non-empty, it also verifies the member belongs to that workspace.
func requireWorkspaceAuth(w http.ResponseWriter, req *http.Request, workspaceID string) *proto.Member {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return nil
	}
	if workspaceID != "" && member.WorkspaceID != workspaceID {
		writeError(w, http.StatusForbidden, "access denied")
		return nil
	}
	return member
}


func (r *Router) setupRoutes() {
	// Health check — no rate limiting
	r.Get("/health", func(w http.ResponseWriter, req *http.Request) {
		ctx, cancel := context.WithTimeout(req.Context(), 3*time.Second)
		defer cancel()
		if err := r.store.Ping(ctx); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "error", "error": "database unreachable"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

		r.Group(func(router chi.Router) {
		router.Use(rateLimitMiddleware(r.rateLimiter))
		router.Use(cacheControlMiddleware)

		// Prometheus-style metrics (no auth required)
		router.Get("/metrics", r.handleMetrics)

		// Incoming webhook execution (authenticated by secret in URL)
		router.Post("/webhooks/{id}/{secret}", r.handleWebhookExecute)

		router.Route("/v1", func(v1 chi.Router) {
		// Unauthenticated bootstrap routes
		v1.Post("/workspaces", r.handleCreateWorkspace)
		v1.Post("/workspaces/{id}/agents", r.handleAgentProvision)

		// All other routes require authentication
		v1.Group(func(p chi.Router) {
			p.Use(r.authenticate)

			// Admin
			p.Get("/admin/stats", r.handleAdminStats)

			// Workspaces
			p.Get("/workspaces", r.handleListWorkspaces)
			p.Get("/workspaces/{id}", r.handleGetWorkspace)
			p.Patch("/workspaces/{id}", r.handleUpdateWorkspace)
			p.Delete("/workspaces/{id}", r.handleDeleteWorkspace)

			// Members
			p.Post("/workspaces/{id}/members", r.handleCreateMember)
			p.Get("/workspaces/{id}/members", r.handleListMembers)
			p.Get("/workspaces/{id}/members/{memberID}", r.handleGetMember)
			p.Patch("/workspaces/{id}/members/{memberID}", r.handleUpdateMember)
			p.Delete("/workspaces/{id}/members/{memberID}", r.handleDeleteMember)

			// Channels
			p.Post("/workspaces/{id}/channels", r.handleCreateChannel)
			p.Get("/workspaces/{id}/channels", r.handleListChannels)
			p.Get("/workspaces/{id}/channels/{channelID}", r.handleGetChannel)
			p.Patch("/workspaces/{id}/channels/{channelID}", r.handleUpdateChannel)
			p.Delete("/workspaces/{id}/channels/{channelID}", r.handleDeleteChannel)
			p.Post("/workspaces/{id}/channels/{channelID}/members", r.handleAddChannelMember)
			p.Delete("/workspaces/{id}/channels/{channelID}/members/{memberID}", r.handleRemoveChannelMember)

			// Messages
			p.Post("/channels/{id}/messages", r.handleCreateMessage)
			p.Get("/channels/{id}/messages", r.handleListMessages)
			p.Patch("/messages/{id}", r.handleUpdateMessage)
			p.Delete("/messages/{id}", r.handleDeleteMessage)

			// Threads
			p.Get("/messages/{id}/thread", r.handleGetThread)

			// Reactions
			p.Post("/messages/{id}/reactions", r.handleAddReaction)
			p.Get("/messages/{id}/reactions", r.handleListReactions)
			p.Delete("/messages/{id}/reactions/{emoji}", r.handleRemoveReaction)

			// Search
			p.Get("/search", r.handleSearch)

			// Tasks
			p.Post("/workspaces/{id}/tasks", r.handleCreateTask)
			p.Get("/workspaces/{id}/tasks", r.handleListTasks)
			p.Patch("/tasks/{id}", r.handleUpdateTask)
			p.Delete("/tasks/{id}", r.handleDeleteTask)

			// Agent memory
			p.Post("/agents/{id}/memory", r.handleSetMemory)
			p.Get("/agents/{id}/memory", r.handleListMemory)
			p.Delete("/agents/{id}/memory/{namespace}/{key}", r.handleDeleteMemory)

			// Files
			p.Post("/workspaces/{id}/files", r.handleUploadFile)
			p.Get("/workspaces/{id}/files", r.handleListFiles)
			p.Get("/files/{id}", r.handleGetFile)
			p.Delete("/files/{id}", r.handleDeleteFile)
			p.Get("/files/{id}/download", r.handleDownloadFile)

			// Pins
			p.Post("/channels/{id}/pins", r.handlePinMessage)
			p.Get("/channels/{id}/pins", r.handleListPins)
			p.Delete("/pins/{id}", r.handleUnpinMessage)

			// Approvals
			p.Post("/workspaces/{id}/approvals", r.handleCreateApproval)
			p.Get("/workspaces/{id}/approvals", r.handleListApprovals)
			p.Get("/approvals/{id}", r.handleGetApproval)
			p.Patch("/approvals/{id}", r.handleReviewApproval)

			// Agent metrics
			p.Get("/agents/{id}/metrics", r.handleGetAgentMetrics)

			// DMs
			p.Post("/workspaces/{id}/dm", r.handleCreateDM)
			p.Get("/members/{id}/dm", r.handleListDMs)

			// Unread
			p.Get("/members/{id}/unread", r.handleGetUnread)
			p.Post("/channels/{id}/read", r.handleMarkRead)

			// Webhooks management
			p.Post("/workspaces/{id}/webhooks", r.handleCreateWebhook)
			p.Get("/workspaces/{id}/webhooks", r.handleListWebhooks)
			p.Delete("/webhooks/{id}", r.handleDeleteWebhook)
		})
	})

	// WebSocket (rate limited)
	router.Get("/ws", r.handleWebSocket)
	})
}

// --- Helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("writeJSON encode failed", "err", err, "status", status)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// decodeAndProtect decodes JSON into obj, then calls restore to revert protected fields.
// Returns false and writes an error response if JSON decoding fails.
func decodeAndProtect(w http.ResponseWriter, req *http.Request, obj interface{}, restore func()) bool {
	if err := decodeJSON(req, obj); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return false
	}
	restore()
	return true
}

// serverError logs the real error and sends a generic message to the client.
func serverError(w http.ResponseWriter, err error, logMsg string, logArgs ...any) {
	slog.Error(logMsg, append([]any{"err", err}, logArgs...)...)
	writeError(w, http.StatusInternalServerError, "internal error")
}

// wsError logs the real error and sends a generic error envelope over WS.
func wsError(c *websocket.Conn, err error, logMsg string, logArgs ...any) {
	slog.Error(logMsg, append([]any{"err", err}, logArgs...)...)
	c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "internal error"}))
}

func decodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(nil, r.Body, 1<<20) // 1MB limit
	return json.NewDecoder(r.Body).Decode(v)
}

// isUUID checks that s is a valid UUID (36 chars, hex+dashes only).
func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// sanitizeFilename strips path separators, null bytes, and quotes from a filename.
func sanitizeFilename(name string) string {
	name = filepath.Base(name)
	name = strings.ReplaceAll(name, "\"", "")
	name = strings.ReplaceAll(name, "\n", "")
	name = strings.ReplaceAll(name, "\r", "")
	if name == "." || name == "/" {
		return "unnamed"
	}
	return name
}

// cacheControlMiddleware sets Cache-Control: no-cache on all API responses
// to prevent stale data caching in real-time collaboration.
func cacheControlMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		next.ServeHTTP(w, r)
	})
}


// --- Workspaces ---

func (r *Router) handleCreateWorkspace(w http.ResponseWriter, req *http.Request) {
	var body struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.Name == "" {
		writeError(w, http.StatusBadRequest, "name required")
		return
	}
	ws := &proto.Workspace{Name: body.Name, Slug: body.Slug}
	if err := r.services.CreateWorkspace(req.Context(), ws); err != nil {
		serverError(w, err, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, ws)
}

func (r *Router) handleListWorkspaces(w http.ResponseWriter, req *http.Request) {
	wss, err := r.services.ListWorkspaces(req.Context())
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	for _, ws := range wss {
		ws.AgentProvisionToken = ""
	}
	writeJSON(w, http.StatusOK, wss)
}

func (r *Router) handleGetWorkspace(w http.ResponseWriter, req *http.Request) {
	ws, err := r.services.GetWorkspace(req.Context(), chi.URLParam(req, "id"))
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	if ws == nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	ws.AgentProvisionToken = ""
	writeJSON(w, http.StatusOK, ws)
}

func (r *Router) handleUpdateWorkspace(w http.ResponseWriter, req *http.Request) {
	if requireWorkspaceAuth(w, req, chi.URLParam(req, "id")) == nil {
		return
	}
	existing, err := r.services.GetWorkspace(req.Context(), chi.URLParam(req, "id"))
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	id := existing.ID
	token := existing.AgentProvisionToken
	if !decodeAndProtect(w, req, existing, func() {
		existing.ID = id
		existing.AgentProvisionToken = token
	}) {
		return
	}
	if err := r.services.UpdateWorkspace(req.Context(), existing); err != nil {
		serverError(w, err, "internal error")
		return
	}
	existing.AgentProvisionToken = ""
	writeJSON(w, http.StatusOK, existing)
}

func (r *Router) handleDeleteWorkspace(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	wsID := chi.URLParam(req, "id")
	existing, err := r.services.GetWorkspace(req.Context(), wsID)
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	if existing == nil || existing.ID != member.WorkspaceID {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	if err := r.services.DeleteWorkspace(req.Context(), wsID); err != nil {
		serverError(w, err, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Members ---

func (r *Router) handleCreateMember(w http.ResponseWriter, req *http.Request) {
	if requireWorkspaceAuth(w, req, chi.URLParam(req, "id")) == nil {
		return
	}
	var body struct {
		Name      string `json:"name"`
		Type      string `json:"type"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.Name == "" {
		writeError(w, http.StatusBadRequest, "name required")
		return
	}
	if proto.MemberType(body.Type) != proto.MemberHuman && proto.MemberType(body.Type) != proto.MemberAgent {
		writeError(w, http.StatusBadRequest, "type must be 'human' or 'agent'")
		return
	}
	m := &proto.Member{
		WorkspaceID: chi.URLParam(req, "id"),
		Name:        body.Name,
		Type:        proto.MemberType(body.Type),
		AvatarURL:   body.AvatarURL,
		Status:      proto.PresenceOffline,
	}
	if err := r.services.CreateMember(req.Context(), m); err != nil {
		serverError(w, err, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, m)
}

func (r *Router) handleListMembers(w http.ResponseWriter, req *http.Request) {
	if requireWorkspaceAuth(w, req, chi.URLParam(req, "id")) == nil {
		return
	}
	workspaceID := chi.URLParam(req, "id")
	limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(req.URL.Query().Get("offset"))
	var members []*proto.Member
	var err error
	if limit > 0 {
		members, err = r.services.ListMembersPaginated(req.Context(), workspaceID, limit, offset)
	} else {
		members, err = r.services.ListMembers(req.Context(), workspaceID)
	}
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	for _, m := range members {
		m.APIKey = ""
	}
	writeJSON(w, http.StatusOK, members)
}

func (r *Router) handleAgentProvision(w http.ResponseWriter, req *http.Request) {
	workspaceID := chi.URLParam(req, "id")
	var body struct {
		Name            string   `json:"name"`
		ProvisionToken  string   `json:"provision_token"`
		SystemPrompt    string   `json:"system_prompt"`
		Capabilities    []string `json:"capabilities"`
		RuntimeType     string   `json:"runtime_type"`
		RuntimeProvider string   `json:"runtime_provider"`
		RuntimeModel    string   `json:"runtime_model"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.Name == "" || body.ProvisionToken == "" {
		writeError(w, http.StatusBadRequest, "name and provision_token required")
		return
	}
	// Validate provision token
	ws, err := r.services.GetWorkspace(req.Context(), workspaceID)
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	if ws == nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	if subtle.ConstantTimeCompare([]byte(ws.AgentProvisionToken), []byte(body.ProvisionToken)) != 1 {
		writeError(w, http.StatusUnauthorized, "invalid provision token")
		return
	}
	// Check if agent already exists
	existing, err2 := r.services.GetMemberByName(req.Context(), workspaceID, body.Name)
	if err2 != nil {
		serverError(w, err2, "agent provision: get member failed")
		return
	}
	if existing != nil {
		// Return existing agent's API key
		writeJSON(w, http.StatusOK, map[string]string{
			"member_id": existing.ID,
			"api_key":   existing.APIKey,
			"name":      existing.Name,
		})
		return
	}
	// Create new agent member
	m := &proto.Member{
		WorkspaceID: workspaceID,
		Name:        body.Name,
		Type:        proto.MemberAgent,
		Status:      proto.PresenceOffline,
		RoleCard: &proto.RoleCard{
			SystemPrompt: body.SystemPrompt,
			Capabilities: body.Capabilities,
		},
		RuntimeInfo: &proto.RuntimeInfo{
			Type:     body.RuntimeType,
			Provider: body.RuntimeProvider,
			Model:    body.RuntimeModel,
		},
	}
	if err := r.services.CreateMember(req.Context(), m); err != nil {
		serverError(w, err, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{
		"member_id": m.ID,
		"api_key":   m.APIKey,
		"name":      m.Name,
	})
}

func (r *Router) handleGetMember(w http.ResponseWriter, req *http.Request) {
	caller := memberFromContext(req)
	if caller == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	member, err := r.services.GetMember(req.Context(), chi.URLParam(req, "memberID"))
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	if member == nil || member.WorkspaceID != caller.WorkspaceID {
		writeError(w, http.StatusNotFound, "member not found")
		return
	}
	member.APIKey = "" // Don't expose API key in GET responses
	writeJSON(w, http.StatusOK, member)
}

func (r *Router) handleUpdateMember(w http.ResponseWriter, req *http.Request) {
	existing, err := r.services.GetMember(req.Context(), chi.URLParam(req, "memberID"))
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "member not found")
		return
	}
	id := existing.ID
	wsID := existing.WorkspaceID
	mtype := existing.Type
	apiKey := existing.APIKey
	if !decodeAndProtect(w, req, existing, func() {
		existing.ID = id
		existing.WorkspaceID = wsID
		existing.Type = mtype
		existing.APIKey = apiKey
	}) {
		return
	}
	if err := r.services.UpdateMember(req.Context(), existing); err != nil {
		serverError(w, err, "internal error")
		return
	}
	existing.APIKey = ""
	writeJSON(w, http.StatusOK, existing)
}

func (r *Router) handleDeleteMember(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	targetID := chi.URLParam(req, "memberID")
	target, err := r.services.GetMember(req.Context(), targetID)
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	if target == nil || target.WorkspaceID != member.WorkspaceID {
		writeError(w, http.StatusNotFound, "member not found")
		return
	}
	if err := r.services.DeleteMember(req.Context(), targetID); err != nil {
		serverError(w, err, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Channels ---

func (r *Router) handleCreateChannel(w http.ResponseWriter, req *http.Request) {
	if requireWorkspaceAuth(w, req, chi.URLParam(req, "id")) == nil {
		return
	}
	var body struct {
		Name      string `json:"name"`
		Type      string `json:"type"`
		Topic     string `json:"topic"`
		IsPrivate bool   `json:"is_private"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.Name == "" {
		writeError(w, http.StatusBadRequest, "name required")
		return
	}
	ch := &proto.Channel{
		WorkspaceID: chi.URLParam(req, "id"),
		Name:        body.Name,
		Type:        proto.ChannelType(body.Type),
		Topic:       body.Topic,
		IsPrivate:   body.IsPrivate,
	}
	if ch.Type == "" {
		ch.Type = proto.ChannelPublic
	}
	if ch.Type != proto.ChannelPublic && ch.Type != proto.ChannelDM && ch.Type != proto.ChannelGroupDM {
		writeError(w, http.StatusBadRequest, "invalid channel type (must be channel, dm, or group_dm)")
		return
	}
	if err := r.services.CreateChannel(req.Context(), ch); err != nil {
		serverError(w, err, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, ch)
}

func (r *Router) handleListChannels(w http.ResponseWriter, req *http.Request) {
	if requireWorkspaceAuth(w, req, chi.URLParam(req, "id")) == nil {
		return
	}
	workspaceID := chi.URLParam(req, "id")
	limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(req.URL.Query().Get("offset"))
	var channels []*proto.Channel
	var err error
	if limit > 0 {
		channels, err = r.services.ListChannelsPaginated(req.Context(), workspaceID, limit, offset)
	} else {
		channels, err = r.services.ListChannels(req.Context(), workspaceID)
	}
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, channels)
}

func (r *Router) handleGetChannel(w http.ResponseWriter, req *http.Request) {
	caller := memberFromContext(req)
	if caller == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	ch, err := r.services.GetChannel(req.Context(), chi.URLParam(req, "channelID"))
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	if ch == nil || ch.WorkspaceID != caller.WorkspaceID {
		writeError(w, http.StatusNotFound, "channel not found")
		return
	}
	writeJSON(w, http.StatusOK, ch)
}

func (r *Router) handleUpdateChannel(w http.ResponseWriter, req *http.Request) {
	existing, err := r.services.GetChannel(req.Context(), chi.URLParam(req, "channelID"))
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "channel not found")
		return
	}
	id := existing.ID
	wsID := existing.WorkspaceID
	chType := existing.Type
	if !decodeAndProtect(w, req, existing, func() {
		existing.ID = id
		existing.WorkspaceID = wsID
		existing.Type = chType
	}) {
		return
	}
	if err := r.services.UpdateChannel(req.Context(), existing); err != nil {
		serverError(w, err, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, existing)
}

func (r *Router) handleDeleteChannel(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	channelID := chi.URLParam(req, "channelID")
	ch, err := r.services.GetChannel(req.Context(), channelID)
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	if ch == nil || ch.WorkspaceID != member.WorkspaceID {
		writeError(w, http.StatusNotFound, "channel not found")
		return
	}
	if err := r.services.DeleteChannel(req.Context(), channelID); err != nil {
		serverError(w, err, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (r *Router) handleAddChannelMember(w http.ResponseWriter, req *http.Request) {
	if requireWorkspaceAuth(w, req, chi.URLParam(req, "id")) == nil {
		return
	}
	var body struct {
		MemberID string `json:"member_id"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := r.services.AddChannelMember(req.Context(), chi.URLParam(req, "channelID"), body.MemberID); err != nil {
		serverError(w, err, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (r *Router) handleRemoveChannelMember(w http.ResponseWriter, req *http.Request) {
	if requireWorkspaceAuth(w, req, chi.URLParam(req, "id")) == nil {
		return
	}
	channelID := chi.URLParam(req, "channelID")
	memberID := chi.URLParam(req, "memberID")
	if err := r.services.RemoveChannelMember(req.Context(), channelID, memberID); err != nil {
		serverError(w, err, "internal error")
		return
	}
	// Unsubscribe from WebSocket channel if connected
	if c := r.hub.GetConn(memberID); c != nil {
		c.Unsubscribe(channelID)
	}
	r.hub.UnsubscribeChannel(channelID, memberID)
	w.WriteHeader(http.StatusNoContent)
}

// --- Messages ---

func (r *Router) handleCreateMessage(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	channelID := chi.URLParam(req, "id")
	// Verify sender is a member of the channel.
	isMember, err := r.store.IsChannelMember(req.Context(), channelID, member.ID)
	if err != nil {
		serverError(w, err, "create message: membership check failed")
		return
	}
	if !isMember {
		writeError(w, http.StatusForbidden, "not a member of this channel")
		return
	}
	var body struct {
		Content  string `json:"content"`
		ThreadID string `json:"thread_id"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	body.Content = strings.TrimSpace(body.Content)
	if body.Content == "" {
		writeError(w, http.StatusBadRequest, "content required")
		return
	}
	if len(body.Content) > 10000 {
		writeError(w, http.StatusBadRequest, "content too long (max 10000 characters)")
		return
	}
	msg := &proto.Message{
		ChannelID: channelID,
		SenderID:  member.ID,
		Content:   body.Content,
		ThreadID:  body.ThreadID,
	}
	if err := r.services.CreateMessage(req.Context(), msg); err != nil {
		serverError(w, err, "create message failed")
		return
	}
	r.hub.SendNewMessage(msg.ChannelID, msg)
	r.handleMentions(msg)
	writeJSON(w, http.StatusCreated, msg)
}

func (r *Router) handleListMessages(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	channelID := chi.URLParam(req, "id")
	isMember, err := r.store.IsChannelMember(req.Context(), channelID, member.ID)
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	if !isMember {
		writeError(w, http.StatusForbidden, "not a member of this channel")
		return
	}
	limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(req.URL.Query().Get("offset"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	msgs, err := r.services.ListMessages(req.Context(), channelID, limit, offset)
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, msgs)
}

func (r *Router) handleUpdateMessage(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	existing, err := r.services.GetMessage(req.Context(), chi.URLParam(req, "id"))
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "message not found")
		return
	}
	if existing.SenderID != member.ID {
		writeError(w, http.StatusForbidden, "not message author")
		return
	}
	id := existing.ID
	channelID := existing.ChannelID
	senderID := existing.SenderID
	createdAt := existing.CreatedAt
	if !decodeAndProtect(w, req, existing, func() {
		existing.ID = id
		existing.ChannelID = channelID
		existing.SenderID = senderID
		existing.CreatedAt = createdAt
	}) {
		return
	}
	if err := r.services.UpdateMessage(req.Context(), existing); err != nil {
		serverError(w, err, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, existing)
}

func (r *Router) handleDeleteMessage(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	existing, err := r.services.GetMessage(req.Context(), chi.URLParam(req, "id"))
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "message not found")
		return
	}
	if existing.SenderID != member.ID {
		writeError(w, http.StatusForbidden, "not message author")
		return
	}
	if err := r.services.DeleteMessage(req.Context(), chi.URLParam(req, "id")); err != nil {
		serverError(w, err, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Threads ---

func (r *Router) handleGetThread(w http.ResponseWriter, req *http.Request) {
	caller := memberFromContext(req)
	if caller == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	threadID := chi.URLParam(req, "id")
	// Verify the parent message exists and caller is a member of its channel
	parent, err := r.services.GetMessage(req.Context(), threadID)
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	if parent == nil {
		writeError(w, http.StatusNotFound, "thread not found")
		return
	}
	isMember, err := r.store.IsChannelMember(req.Context(), parent.ChannelID, caller.ID)
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	if !isMember {
		writeError(w, http.StatusForbidden, "not a member of this channel")
		return
	}
	msgs, err := r.services.ListThreadMessages(req.Context(), threadID)
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, msgs)
}

// --- Reactions ---

func (r *Router) handleAddReaction(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	var body struct {
		Emoji string `json:"emoji"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	reaction := &proto.Reaction{
		MessageID: chi.URLParam(req, "id"),
		MemberID:  member.ID,
		Emoji:     body.Emoji,
	}
	if err := r.services.AddReaction(req.Context(), reaction); err != nil {
		serverError(w, err, "internal error")
		return
	}
	r.broadcastReactionEvent(reaction.MessageID, reaction)
	writeJSON(w, http.StatusCreated, reaction)
}

func (r *Router) handleListReactions(w http.ResponseWriter, req *http.Request) {
	if memberFromContext(req) == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	reactions, err := r.services.ListReactions(req.Context(), chi.URLParam(req, "id"))
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, reactions)
}

func (r *Router) handleRemoveReaction(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	if err := r.services.RemoveReaction(req.Context(), chi.URLParam(req, "id"), member.ID, chi.URLParam(req, "emoji")); err != nil {
		serverError(w, err, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Search ---

func (r *Router) handleSearch(w http.ResponseWriter, req *http.Request) {
	q := req.URL.Query().Get("q")
	if q == "" {
		writeError(w, http.StatusBadRequest, "q is required")
		return
	}
	channelID := req.URL.Query().Get("channel")
	limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	msgs, err := r.services.SearchMessages(req.Context(), q, channelID, limit)
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, msgs)
}

// --- Tasks ---

func (r *Router) handleCreateTask(w http.ResponseWriter, req *http.Request) {
	member := requireWorkspaceAuth(w, req, chi.URLParam(req, "id"))
	if member == nil {
		return
	}
	var body struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		AssignedTo  string `json:"assigned_to"`
		ChannelID   string `json:"channel_id"`
		Priority    string `json:"priority"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.Title == "" {
		writeError(w, http.StatusBadRequest, "title required")
		return
	}
	task := &proto.Task{
		WorkspaceID: chi.URLParam(req, "id"),
		Title:       body.Title,
		Description: body.Description,
		AssignedTo:  body.AssignedTo,
		CreatedBy:   member.ID,
		ChannelID:   body.ChannelID,
		Priority:    body.Priority,
	}
	if task.Priority == "" {
		task.Priority = proto.TaskPriorityMedium
	}
	if task.Priority != proto.TaskPriorityLow && task.Priority != proto.TaskPriorityMedium &&
		task.Priority != proto.TaskPriorityHigh && task.Priority != proto.TaskPriorityUrgent {
		writeError(w, http.StatusBadRequest, "invalid priority (must be low, medium, high, or urgent)")
		return
	}
	if err := r.services.CreateTask(req.Context(), task); err != nil {
		serverError(w, err, "internal error")
		return
	}
	r.broadcastTaskEvent(task)
	writeJSON(w, http.StatusCreated, task)
}

func (r *Router) handleListTasks(w http.ResponseWriter, req *http.Request) {
	if requireWorkspaceAuth(w, req, chi.URLParam(req, "id")) == nil {
		return
	}
	status := proto.TaskStatus(req.URL.Query().Get("status"))
	tasks, err := r.services.ListTasks(req.Context(), chi.URLParam(req, "id"), status)
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, tasks)
}

func (r *Router) handleUpdateTask(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	existing, err := r.services.GetTask(req.Context(), chi.URLParam(req, "id"))
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	if existing == nil || existing.WorkspaceID != member.WorkspaceID {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	id := existing.ID
	wsID := existing.WorkspaceID
	createdBy := existing.CreatedBy
	createdAt := existing.CreatedAt
	if !decodeAndProtect(w, req, existing, func() {
		existing.ID = id
		existing.WorkspaceID = wsID
		existing.CreatedBy = createdBy
		existing.CreatedAt = createdAt
	}) {
		return
	}
	if err := r.services.UpdateTask(req.Context(), existing); err != nil {
		serverError(w, err, "internal error")
		return
	}
	r.broadcastTaskEvent(existing)
	writeJSON(w, http.StatusOK, existing)
}

func (r *Router) handleDeleteTask(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	task, err := r.services.GetTask(req.Context(), chi.URLParam(req, "id"))
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	if task == nil || task.WorkspaceID != member.WorkspaceID {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	if err := r.services.DeleteTask(req.Context(), task.ID); err != nil {
		serverError(w, err, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Agent Memory ---

func (r *Router) handleSetMemory(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	agentID := chi.URLParam(req, "id")
	if member.ID != agentID {
		writeError(w, http.StatusForbidden, "cannot modify another agent's memory")
		return
	}
	var body struct {
		Namespace string `json:"namespace"`
		Key       string `json:"key"`
		Value     string `json:"value"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.Key == "" {
		writeError(w, http.StatusBadRequest, "key required")
		return
	}
	mem := &proto.AgentMemory{
		AgentID:   agentID,
		Namespace: body.Namespace,
		Key:       body.Key,
		Value:     body.Value,
	}
	if mem.Namespace == "" {
		mem.Namespace = "default"
	}
	if err := r.services.SetMemory(req.Context(), mem); err != nil {
		serverError(w, err, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, mem)
}

func (r *Router) handleListMemory(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	agentID := chi.URLParam(req, "id")
	if member.ID != agentID {
		writeError(w, http.StatusForbidden, "cannot read another agent's memory")
		return
	}
	namespace := req.URL.Query().Get("namespace")
	if namespace == "" {
		namespace = "default"
	}
	memories, err := r.services.ListMemory(req.Context(), chi.URLParam(req, "id"), namespace)
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, memories)
}

func (r *Router) handleDeleteMemory(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	agentID := chi.URLParam(req, "id")
	if member.ID != agentID {
		writeError(w, http.StatusForbidden, "cannot delete another agent's memory")
		return
	}
	if err := r.services.DeleteMemory(req.Context(), agentID, chi.URLParam(req, "namespace"), chi.URLParam(req, "key")); err != nil {
		serverError(w, err, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- WebSocket ---

func (r *Router) handleWebSocket(w http.ResponseWriter, req *http.Request) {
	conn, err := r.hub.Upgrade(w, req)
	if err != nil {
		r.logger.Error("websocket upgrade failed", "err", err)
		return
	}
	wc := websocket.NewConn(r.hub, conn, "", "", false)
	go wc.WritePump()
	go wc.ReadPump(func(env websocket.Envelope) {
		r.handleWSEvent(wc, env)
	})
	// Close unauthenticated connections after 30 seconds
	go func() {
		time.Sleep(30 * time.Second)
		if !wc.IsAuthenticated() {
			wc.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "auth timeout"}))
			wc.Close()
		}
	}()
}

func (r *Router) handleWSEvent(c *websocket.Conn, env websocket.Envelope) {
	switch env.Type {
	case websocket.EventAuthLogin:
		r.handleWSAuthLogin(c, env)

	case websocket.EventAgentHello:
		data, err := websocket.ParseAgentHello(env.Data)
		if err != nil {
			c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "invalid agent hello data"}))
			return
		}
		r.agentManager.HandleAgentHello(c, data)

	case websocket.EventAgentSleep:
		r.agentManager.HandleAgentSleep(c)

	case websocket.EventAgentThinking:
		if !c.IsAuthenticated() || !c.IsAgent() {
			c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "not authorized"}))
			return
		}
		var td struct {
			ChannelID string `json:"channel_id"`
		}
		if err := json.Unmarshal(env.Data, &td); err != nil || td.ChannelID == "" {
			c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "invalid thinking data"}))
			return
		}
		r.agentManager.HandleAgentThinking(c, td.ChannelID)

	case websocket.EventChannelJoin:
		r.handleWSChannelJoin(c, env)

	case websocket.EventChannelLeave:
		r.handleWSChannelLeave(c, env)

	case websocket.EventMessageSend:
		r.handleWSMessageSend(c, env)

	case websocket.EventTypingStart:
		r.handleWSTypingStart(c, env)

	case websocket.EventTypingStop:
		r.handleWSTypingStop(c, env)

	case websocket.EventThreadReply:
		r.handleWSThreadReply(c, env)

	case websocket.EventMessageEdit:
		r.handleWSMessageEdit(c, env)

	case websocket.EventMessageDel:
		r.handleWSMessageDelete(c, env)

	case websocket.EventApprovalRequest:
		r.handleWSApprovalRequest(c, env)

	default:
		r.logger.Warn("unknown ws event", "type", env.Type)
	}
}

func (r *Router) handleWSAuthLogin(c *websocket.Conn, env websocket.Envelope) {
	data, err := websocket.ParseAuthLogin(env.Data)
	if err != nil {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "invalid auth data"}))
		return
	}

	// Validate JWT
	claims, err := r.auth.ValidateToken(data.Token)
	if err != nil {
		// Try as API key
		ctx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
		defer cancel()
		member, err := r.store.GetMemberByAPIKey(ctx, data.Token)
		if err != nil {
			c.Send(websocket.NewEnvelope(websocket.EventAuthFail, map[string]string{"error": "auth error"}))
			return
		}
		if member == nil {
			c.Send(websocket.NewEnvelope(websocket.EventAuthFail, map[string]string{"error": "invalid credentials"}))
			return
		}
		c.SetIdentity(member.ID, member.Name, member.Type == proto.MemberAgent)
		c.SetAuthenticated(true)
		r.hub.Add(c)

		// Auto-subscribe to all channels the member belongs to
		r.subscribeMemberChannels(c, member.ID)

		c.Send(websocket.NewEnvelope(websocket.EventAuthSuccess, map[string]any{
			"member_id":    member.ID,
			"workspace_id": member.WorkspaceID,
			"name":         member.Name,
			"type":         member.Type,
		}))
		r.hub.SendPresenceUpdate(member.ID, string(proto.PresenceOnline))
		return
	}

	// JWT auth
	ctx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
	defer cancel()
	member, err := r.store.GetMember(ctx, claims.MemberID)
	if err != nil {
		c.Send(websocket.NewEnvelope(websocket.EventAuthFail, map[string]string{"error": "auth error"}))
		return
	}
	if member == nil {
		c.Send(websocket.NewEnvelope(websocket.EventAuthFail, map[string]string{"error": "member not found"}))
		return
	}
	c.SetIdentity(member.ID, member.Name, member.Type == proto.MemberAgent)
	c.SetAuthenticated(true)
	r.hub.Add(c)

	// Auto-subscribe to all channels the member belongs to
	r.subscribeMemberChannels(c, member.ID)

	c.Send(websocket.NewEnvelope(websocket.EventAuthSuccess, map[string]any{
		"member_id":    member.ID,
		"workspace_id": member.WorkspaceID,
		"name":         member.Name,
		"type":         member.Type,
	}))
	r.hub.SendPresenceUpdate(member.ID, string(proto.PresenceOnline))
}

func (r *Router) subscribeMemberChannels(c *websocket.Conn, memberID string) {
	channelIDs, err := r.store.ListMemberChannelIDs(c.Context(), memberID)
	if err != nil {
		return
	}
	for _, id := range channelIDs {
		if c.Subscribe(id) {
			r.hub.SubscribeChannel(id, c)
		}
	}
}

func (r *Router) handleWSChannelJoin(c *websocket.Conn, env websocket.Envelope) {
	if !c.IsAuthenticated() {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "not authenticated"}))
		return
	}
	var data struct {
		ChannelID string `json:"channel_id"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "invalid data"}))
		return
	}
	// Verify membership
	isMember, err := r.store.IsChannelMember(c.Context(), data.ChannelID, c.ID())
	if err != nil || !isMember {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "not a channel member"}))
		return
	}
	if !c.Subscribe(data.ChannelID) {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "subscription limit reached"}))
		return
	}
	r.hub.SubscribeChannel(data.ChannelID, c)
	c.Send(websocket.NewEnvelope(websocket.EventChannelJoin, map[string]string{"channel_id": data.ChannelID}))
}

func (r *Router) handleWSChannelLeave(c *websocket.Conn, env websocket.Envelope) {
	if !c.IsAuthenticated() {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "not authenticated"}))
		return
	}
	var data struct {
		ChannelID string `json:"channel_id"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "invalid data"}))
		return
	}
	c.Unsubscribe(data.ChannelID)
	r.hub.UnsubscribeChannel(data.ChannelID, c.ID())
	c.Send(websocket.NewEnvelope(websocket.EventChannelLeave, map[string]string{"channel_id": data.ChannelID}))
}

func (r *Router) handleWSMessageSend(c *websocket.Conn, env websocket.Envelope) {
	if !c.IsAuthenticated() {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "not authenticated"}))
		return
	}
	data, err := websocket.ParseMessageSend(env.Data)
	if err != nil {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "invalid message data"}))
		return
	}
	if strings.TrimSpace(data.Content) == "" {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "content is required"}))
		return
	}
	if len(data.Content) > 10000 {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "content too long (max 10000 characters)"}))
		return
	}
	// Verify sender is subscribed to the channel
	if !c.IsSubscribed(data.ChannelID) {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "not subscribed to channel"}))
		return
	}
	msg := &proto.Message{
		ChannelID: data.ChannelID,
		SenderID:  c.ID(),
		Content:   data.Content,
		ThreadID:  data.ThreadID,
		Type:      data.Type,
	}
	if err := r.services.CreateMessage(c.Context(), msg); err != nil {
		wsError(c, err, "ws create message failed")
		return
	}
	r.hub.SendNewMessage(msg.ChannelID, msg)
	c.Send(websocket.NewEnvelope(websocket.EventMessageAck, map[string]string{"message_id": msg.ID}))
	r.handleMentions(msg)
}

// handleMentions checks for @mentions and wakes agents by name with context.
func (r *Router) handleMentions(msg *proto.Message) {
	mentions := websocket.ParseMentions(msg.Content)
	if len(mentions) == 0 {
		return
	}
	for _, name := range mentions {
		r.hub.WakeAgentByName(name, msg.ChannelID, "mention")
	}
}

// handleWSMessageEdit processes a message.edit event.
func (r *Router) handleWSMessageEdit(c *websocket.Conn, env websocket.Envelope) {
	if !c.IsAuthenticated() {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "not authenticated"}))
		return
	}
	var data struct {
		MessageID string `json:"message_id"`
		Content   string `json:"content"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "invalid data"}))
		return
	}
	if strings.TrimSpace(data.Content) == "" {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "content is required"}))
		return
	}
	if len(data.Content) > 10000 {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "content too long (max 10000 characters)"}))
		return
	}
	msg, err := r.services.GetMessage(c.Context(), data.MessageID)
	if err != nil {
		wsError(c, err, "ws message edit: get message failed")
		return
	}
	if msg == nil {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "message not found"}))
		return
	}
	if msg.SenderID != c.ID() {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "not message author"}))
		return
	}
	msg.Content = data.Content
	if err := r.services.UpdateMessage(c.Context(), msg); err != nil {
		wsError(c, err, "ws update message failed")
		return
	}
	r.hub.BroadcastToChannel(msg.ChannelID, websocket.NewEnvelope(websocket.EventMessageEdit, msg))
}

// handleWSMessageDelete processes a message.delete event.
func (r *Router) handleWSMessageDelete(c *websocket.Conn, env websocket.Envelope) {
	if !c.IsAuthenticated() {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "not authenticated"}))
		return
	}
	var data struct {
		MessageID string `json:"message_id"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "invalid data"}))
		return
	}
	msg, err := r.services.GetMessage(c.Context(), data.MessageID)
	if err != nil {
		wsError(c, err, "ws message delete: get message failed")
		return
	}
	if msg == nil {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "message not found"}))
		return
	}
	if msg.SenderID != c.ID() {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "not message author"}))
		return
	}
	if err := r.services.DeleteMessage(c.Context(), data.MessageID); err != nil {
		wsError(c, err, "ws delete message failed")
		return
	}
	r.hub.BroadcastToChannel(msg.ChannelID, websocket.NewEnvelope(websocket.EventMessageDel, map[string]string{"id": data.MessageID}))
}

// --- Approval handlers ---

func (r *Router) handleCreateApproval(w http.ResponseWriter, req *http.Request) {
	member := requireWorkspaceAuth(w, req, chi.URLParam(req, "id"))
	if member == nil {
		return
	}
	workspaceID := chi.URLParam(req, "id")
	var body struct {
		ChannelID string `json:"channel_id,omitempty"`
		Action    string `json:"action"`
		Payload   string `json:"payload,omitempty"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.Action == "" {
		writeError(w, http.StatusBadRequest, "action required")
		return
	}
	a := &proto.ApprovalRequest{
		WorkspaceID: workspaceID,
		AgentID:     member.ID,
		ChannelID:   body.ChannelID,
		Action:      body.Action,
		Payload:     body.Payload,
	}
	if err := r.services.CreateApproval(req.Context(), a); err != nil {
		serverError(w, err, "internal error")
		return
	}
	// Broadcast to workspace that an approval is pending
	r.hub.BroadcastToAll(websocket.NewEnvelope(websocket.EventApprovalRequest, a))
	writeJSON(w, http.StatusCreated, a)
}

func (r *Router) handleGetApproval(w http.ResponseWriter, req *http.Request) {
	caller := memberFromContext(req)
	if caller == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	a, err := r.services.GetApproval(req.Context(), chi.URLParam(req, "id"))
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	if a == nil || a.WorkspaceID != caller.WorkspaceID {
		writeError(w, http.StatusNotFound, "approval not found")
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (r *Router) handleListApprovals(w http.ResponseWriter, req *http.Request) {
	if requireWorkspaceAuth(w, req, chi.URLParam(req, "id")) == nil {
		return
	}
	status := proto.ApprovalStatus(req.URL.Query().Get("status"))
	approvals, err := r.services.ListApprovals(req.Context(), chi.URLParam(req, "id"), status)
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, approvals)
}

func (r *Router) handleReviewApproval(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	approvalID := chi.URLParam(req, "id")
	existing, err := r.services.GetApproval(req.Context(), approvalID)
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "approval not found")
		return
	}
	if existing.Status != proto.ApprovalPending {
		writeError(w, http.StatusBadRequest, "already reviewed")
		return
	}
	var body struct {
		Approved bool   `json:"approved"`
		Note     string `json:"note,omitempty"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.Approved {
		existing.Status = proto.ApprovalApproved
	} else {
		existing.Status = proto.ApprovalDenied
	}
	existing.ReviewerID = member.ID
	existing.ReviewNote = body.Note
	existing.ReviewedAt = time.Now().UnixMilli()
	if err := r.services.UpdateApproval(req.Context(), existing); err != nil {
		serverError(w, err, "internal error")
		return
	}
	// Notify the agent of the result
	r.hub.BroadcastToAll(websocket.NewEnvelope(websocket.EventApprovalResult, existing))
	writeJSON(w, http.StatusOK, existing)
}

// handleWSApprovalRequest processes an agent's approval.request via WebSocket.
func (r *Router) handleWSApprovalRequest(c *websocket.Conn, env websocket.Envelope) {
	if !c.IsAuthenticated() || !c.IsAgent() {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "not authenticated agent"}))
		return
	}
	var data struct {
		ChannelID string `json:"channel_id,omitempty"`
		Action    string `json:"action"`
		Payload   string `json:"payload,omitempty"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "invalid data"}))
		return
	}
	a := &proto.ApprovalRequest{
		AgentID:   c.ID(),
		ChannelID: data.ChannelID,
		Action:    data.Action,
		Payload:   data.Payload,
	}
	// We need workspace_id — get from agent's member record
	member, err := r.services.GetMember(c.Context(), c.ID())
	if err != nil {
		wsError(c, err, "ws approval: get member failed")
		return
	}
	if member == nil {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "agent not found"}))
		return
	}
	a.WorkspaceID = member.WorkspaceID
	if err := r.services.CreateApproval(c.Context(), a); err != nil {
		wsError(c, err, "ws create approval failed")
		return
	}
	// Notify humans that approval is pending
	r.hub.BroadcastToAll(websocket.NewEnvelope(websocket.EventApprovalRequest, a))
	c.Send(websocket.NewEnvelope(websocket.EventApprovalRequest, map[string]string{"id": a.ID, "status": "pending"}))
}

// --- Agent metrics ---

func (r *Router) handleGetAgentMetrics(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	agentID := chi.URLParam(req, "id")
	// Verify the agent exists in the same workspace
	agent, err := r.services.GetMember(req.Context(), agentID)
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	if agent == nil || agent.WorkspaceID != member.WorkspaceID {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	// Get metrics from agent_memory with namespace "_metrics"
	memories, err := r.services.ListMemory(req.Context(), agentID, "_metrics")
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	metrics := make(map[string]interface{})
	for _, m := range memories {
		metrics[m.Key] = m.Value
	}
	// Get task counts via efficient store method
	completed, pending, terr := r.store.CountTasksByAssignee(req.Context(), agentID)
	if terr == nil {
		metrics["tasks_assigned"] = completed + pending
		metrics["tasks_completed"] = completed
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"agent_id": agentID,
		"metrics":  metrics,
	})
}

func (r *Router) handleWSTypingStart(c *websocket.Conn, env websocket.Envelope) {
	if !c.IsAuthenticated() {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "not authenticated"}))
		return
	}
	var data struct {
		ChannelID string `json:"channel_id"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil || data.ChannelID == "" {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "invalid data"}))
		return
	}
	isMember, err := r.store.IsChannelMember(c.Context(), data.ChannelID, c.ID())
	if err != nil {
		r.logger.Error("typing start: check membership", "err", err)
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "internal error"}))
		return
	}
	if !isMember {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "not a channel member"}))
		return
	}
	r.hub.BroadcastToChannel(data.ChannelID, websocket.NewEnvelope(websocket.EventTypingStart, map[string]string{
		"channel_id": data.ChannelID,
		"member_id":  c.ID(),
		"name":       c.Name(),
	}))
}

func (r *Router) handleWSTypingStop(c *websocket.Conn, env websocket.Envelope) {
	if !c.IsAuthenticated() {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "not authenticated"}))
		return
	}
	var data struct {
		ChannelID string `json:"channel_id"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil || data.ChannelID == "" {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "invalid data"}))
		return
	}
	isMember, err := r.store.IsChannelMember(c.Context(), data.ChannelID, c.ID())
	if err != nil {
		r.logger.Error("typing stop: check membership", "err", err)
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "internal error"}))
		return
	}
	if !isMember {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "not a channel member"}))
		return
	}
	r.hub.BroadcastToChannel(data.ChannelID, websocket.NewEnvelope(websocket.EventTypingStop, map[string]string{
		"channel_id": data.ChannelID,
		"member_id":  c.ID(),
	}))
}

func (r *Router) handleWSThreadReply(c *websocket.Conn, env websocket.Envelope) {
	if !c.IsAuthenticated() {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "not authenticated"}))
		return
	}
	var data struct {
		ChannelID string `json:"channel_id"`
		ParentID  string `json:"parent_id"`
		Content   string `json:"content"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "invalid data"}))
		return
	}
	if data.ParentID == "" || data.Content == "" {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "parent_id and content required"}))
		return
	}
	if len(data.Content) > 10000 {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "content too long (max 10000 characters)"}))
		return
	}
	isMember, err := r.store.IsChannelMember(c.Context(), data.ChannelID, c.ID())
	if err != nil {
		r.logger.Error("thread reply: check membership", "err", err)
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "internal error"}))
		return
	}
	if !isMember {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "not a channel member"}))
		return
	}
	msg := &proto.Message{
		ChannelID: data.ChannelID,
		SenderID:  c.ID(),
		Content:   data.Content,
		ThreadID:  data.ParentID,
	}
	if err := r.services.CreateMessage(c.Context(), msg); err != nil {
		wsError(c, err, "ws create thread reply failed")
		return
	}
	r.hub.BroadcastToChannel(data.ChannelID, websocket.NewEnvelope(websocket.EventMessageNew, msg))
	c.Send(websocket.NewEnvelope(websocket.EventMessageAck, map[string]string{"id": msg.ID}))

	// Check for @mentions in thread replies too
	r.handleMentions(msg)
}

// --- File handlers ---

func (r *Router) handleUploadFile(w http.ResponseWriter, req *http.Request) {
	member := requireWorkspaceAuth(w, req, chi.URLParam(req, "id"))
	if member == nil {
		return
	}
	workspaceID := chi.URLParam(req, "id")
	if !isUUID(workspaceID) {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return
	}
	// Parse multipart form (max 32MB)
	if err := req.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart form")
		return
	}
	file, header, err := req.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file field required")
		return
	}
	defer file.Close()
	// Create upload directory
	uploadDir := filepath.Join("data", "files", workspaceID)
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		serverError(w, err, "failed to create upload directory")
		return
	}
	// Write file
	fID := proto.NewID()
	ext := filepath.Ext(header.Filename)
	savePath := filepath.Join(uploadDir, fID+ext)
	dst, err := os.Create(savePath)
	if err != nil {
		serverError(w, err, "failed to create file on disk")
		return
	}
	defer dst.Close()
	size, err := io.Copy(dst, file)
	if err != nil {
		dst.Close()
		os.Remove(savePath) // clean up partial file on disk
		serverError(w, err, "failed to write file")
		return
	}
	// Detect MIME type: prefer Content-Type header, fallback to sniff
	mimeType := header.Header.Get("Content-Type")
	if mimeType == "" || mimeType == "application/octet-stream" {
		buf := make([]byte, 512)
		dst.Seek(0, 0)
		n, _ := dst.Read(buf)
		if n > 0 {
			mimeType = http.DetectContentType(buf[:n])
		}
	}
	// Save to DB
	f := &proto.File{
		ID:          fID,
		WorkspaceID: workspaceID,
		UploaderID:  member.ID,
		Filename:    sanitizeFilename(header.Filename),
		MimeType:    mimeType,
		Size:        size,
		Path:        savePath,
	}
	if err := r.services.CreateFile(req.Context(), f); err != nil {
		os.Remove(savePath) // clean up orphaned file on disk
		serverError(w, err, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, f)
}

func (r *Router) handleGetFile(w http.ResponseWriter, req *http.Request) {
	caller := memberFromContext(req)
	if caller == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	f, err := r.services.GetFile(req.Context(), chi.URLParam(req, "id"))
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	if f == nil || f.WorkspaceID != caller.WorkspaceID {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}
	writeJSON(w, http.StatusOK, f)
}

func (r *Router) handleListFiles(w http.ResponseWriter, req *http.Request) {
	if requireWorkspaceAuth(w, req, chi.URLParam(req, "id")) == nil {
		return
	}
	limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(req.URL.Query().Get("offset"))
	files, err := r.services.ListFiles(req.Context(), chi.URLParam(req, "id"), limit, offset)
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, files)
}

func (r *Router) handleDeleteFile(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	fileID := chi.URLParam(req, "id")
	f, err := r.services.GetFile(req.Context(), fileID)
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	if f == nil || f.WorkspaceID != member.WorkspaceID {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}
	if removeErr := os.Remove(f.Path); removeErr != nil && !os.IsNotExist(removeErr) {
		serverError(w, removeErr, "failed to delete file from disk")
		return
	}
	if err := r.services.DeleteFile(req.Context(), fileID); err != nil {
		serverError(w, err, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (r *Router) handleDownloadFile(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	f, err := r.services.GetFile(req.Context(), chi.URLParam(req, "id"))
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	if f == nil || f.WorkspaceID != member.WorkspaceID {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}
	w.Header().Set("Content-Disposition", "attachment; filename=\""+sanitizeFilename(f.Filename)+"\"")
	w.Header().Set("Content-Type", f.MimeType)
	http.ServeFile(w, req, f.Path)
}

// --- Pin handlers ---

func (r *Router) handlePinMessage(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	channelID := chi.URLParam(req, "id")
	isMember, err := r.store.IsChannelMember(req.Context(), channelID, member.ID)
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	if !isMember {
		writeError(w, http.StatusForbidden, "not a member of this channel")
		return
	}
	var body struct {
		MessageID string `json:"message_id"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.MessageID == "" {
		writeError(w, http.StatusBadRequest, "message_id required")
		return
	}
	p := &proto.Pin{
		MessageID: body.MessageID,
		ChannelID: channelID,
		PinnedBy:  member.ID,
	}
	if err := r.services.CreatePin(req.Context(), p); err != nil {
		serverError(w, err, "internal error")
		return
	}
	r.broadcastPinEvent(channelID, p)
	writeJSON(w, http.StatusCreated, p)
}

func (r *Router) handleListPins(w http.ResponseWriter, req *http.Request) {
	caller := memberFromContext(req)
	if caller == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	isMember, err := r.store.IsChannelMember(req.Context(), chi.URLParam(req, "id"), caller.ID)
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	if !isMember {
		writeError(w, http.StatusForbidden, "not a member of this channel")
		return
	}
	pins, err := r.services.ListPins(req.Context(), chi.URLParam(req, "id"))
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, pins)
}

func (r *Router) handleUnpinMessage(w http.ResponseWriter, req *http.Request) {
	if memberFromContext(req) == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	if err := r.services.DeletePin(req.Context(), chi.URLParam(req, "id")); err != nil {
		serverError(w, err, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- DM handlers ---

func (r *Router) handleCreateDM(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	workspaceID := chi.URLParam(req, "id")
	var body struct {
		MemberIDs []string `json:"member_ids"`
		Name      string   `json:"name,omitempty"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	// Ensure authenticated member is a participant.
	isParticipant := false
	for _, mid := range body.MemberIDs {
		if mid == member.ID {
			isParticipant = true
			break
		}
	}
	if !isParticipant {
		writeError(w, http.StatusForbidden, "authenticated member must be included in member_ids")
		return
	}
	if len(body.MemberIDs) < 2 {
		writeError(w, http.StatusBadRequest, "at least 2 member_ids required")
		return
	}
	// Fast-path: check if DM already exists.
	existing, _ := r.services.GetDMChannel(req.Context(), workspaceID, body.MemberIDs)
	if existing != nil {
		writeJSON(w, http.StatusOK, existing)
		return
	}
	chType := proto.ChannelDM
	if len(body.MemberIDs) > 2 {
		chType = proto.ChannelGroupDM
	}
	ch := &proto.Channel{
		WorkspaceID: workspaceID,
		Name:        body.Name,
		Type:        chType,
	}
	if err := r.services.CreateDMChannel(req.Context(), ch, body.MemberIDs); err != nil {
		serverError(w, err, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, ch)
}

func (r *Router) handleListDMs(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	memberID := chi.URLParam(req, "id")
	if member.ID != memberID {
		writeError(w, http.StatusForbidden, "cannot list another member's DMs")
		return
	}
	channels, err := r.services.ListDMChannels(req.Context(), memberID)
	if err != nil {
		serverError(w, err, "list DMs failed")
		return
	}
	writeJSON(w, http.StatusOK, channels)
}

// --- Unread handlers ---

func (r *Router) handleGetUnread(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	memberID := chi.URLParam(req, "id")
	if member.ID != memberID {
		writeError(w, http.StatusForbidden, "cannot view another member's unread counts")
		return
	}
	counts, err := r.services.GetUnreadCounts(req.Context(), memberID)
	if err != nil {
		serverError(w, err, "get unread counts failed")
		return
	}
	writeJSON(w, http.StatusOK, counts)
}

func (r *Router) handleMarkRead(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	channelID := chi.URLParam(req, "id")
	if err := r.services.MarkChannelRead(req.Context(), channelID, member.ID); err != nil {
		serverError(w, err, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Webhook handlers ---

func (r *Router) handleCreateWebhook(w http.ResponseWriter, req *http.Request) {
	member := requireWorkspaceAuth(w, req, chi.URLParam(req, "id"))
	if member == nil {
		return
	}
	var body struct {
		ChannelID string `json:"channel_id"`
		Name      string `json:"name"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.ChannelID == "" || body.Name == "" {
		writeError(w, http.StatusBadRequest, "channel_id and name required")
		return
	}
	wb := &proto.Webhook{
		WorkspaceID: chi.URLParam(req, "id"),
		ChannelID:   body.ChannelID,
		Name:        body.Name,
		CreatedBy:   member.ID,
	}
	if err := r.services.CreateWebhook(req.Context(), wb); err != nil {
		serverError(w, err, "create webhook failed")
		return
	}
	writeJSON(w, http.StatusCreated, wb)
}

func (r *Router) handleListWebhooks(w http.ResponseWriter, req *http.Request) {
	if requireWorkspaceAuth(w, req, chi.URLParam(req, "id")) == nil {
		return
	}
	webhooks, err := r.services.ListWebhooks(req.Context(), chi.URLParam(req, "id"))
	if err != nil {
		serverError(w, err, "list webhooks failed")
		return
	}
	// Mask secrets in list responses
	for _, wb := range webhooks {
		if len(wb.Secret) > 8 {
			wb.Secret = wb.Secret[:8] + "..."
		}
	}
	writeJSON(w, http.StatusOK, webhooks)
}

func (r *Router) handleDeleteWebhook(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	if err := r.services.DeleteWebhook(req.Context(), chi.URLParam(req, "id")); err != nil {
		serverError(w, err, "delete webhook failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleWebhookExecute handles unauthenticated webhook POST requests.
func (r *Router) handleWebhookExecute(w http.ResponseWriter, req *http.Request) {
	webhookID := chi.URLParam(req, "id")
	secret := chi.URLParam(req, "secret")
	wb, err := r.services.GetWebhook(req.Context(), webhookID)
	if err != nil {
		serverError(w, err, "get webhook failed")
		return
	}
	if wb == nil || wb.Secret != secret {
		writeError(w, http.StatusUnauthorized, "invalid webhook")
		return
	}
	var body struct {
		Content string `json:"content"`
		Text    string `json:"text"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	content := body.Content
	if content == "" {
		content = body.Text
	}
	if content == "" {
		writeError(w, http.StatusBadRequest, "content or text required")
		return
	}
	msg := &proto.Message{
		ChannelID: wb.ChannelID,
		SenderID:  wb.CreatedBy,
		Content:   content,
	}
	if err := r.services.CreateMessage(req.Context(), msg); err != nil {
		serverError(w, err, "webhook message failed")
		return
	}
	r.hub.SendNewMessage(msg.ChannelID, msg)
	writeJSON(w, http.StatusCreated, map[string]string{"status": "ok", "message_id": msg.ID})
}

// --- Admin handlers ---

func (r *Router) handleAdminStats(w http.ResponseWriter, req *http.Request) {
	if memberFromContext(req) == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	stats := map[string]any{
		"ws_connections": r.hub.Total(),
		"ws_agents":      r.hub.AgentCount(),
	}
	writeJSON(w, http.StatusOK, stats)
}

// --- Metrics ---

func (r *Router) handleMetrics(w http.ResponseWriter, req *http.Request) {
	stats := map[string]any{
		"connections": r.hub.Total(),
		"agents":      r.hub.AgentCount(),
	}
	writeJSON(w, http.StatusOK, stats)
}

// --- Real-time WS event helpers ---

func (r *Router) broadcastReactionEvent(msgID string, reaction *proto.Reaction) {
	// Get the message to find its channel
	msg, err := r.services.GetMessage(context.Background(), msgID)
	if err != nil || msg == nil {
		return
	}
	r.hub.BroadcastToChannel(msg.ChannelID, websocket.NewEnvelope("reaction.add", reaction))
}

func (r *Router) broadcastPinEvent(channelID string, pin *proto.Pin) {
	r.hub.BroadcastToChannel(channelID, websocket.NewEnvelope("pin.add", pin))
}

func (r *Router) broadcastTaskEvent(task *proto.Task) {
	r.hub.BroadcastToAll(websocket.NewEnvelope("task.update", task))
}
