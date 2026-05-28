package api

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"golang.org/x/oauth2"

	"lark-daemon/internal/proto"
	"lark-daemon/internal/server/metrics"
	"lark-daemon/internal/server/service"
	"lark-daemon/internal/server/storage"
	"lark-daemon/internal/server/store"
	"lark-daemon/internal/server/websocket"
)

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
	storage      storage.Store
	stripe       *service.StripeService
	githubOAuth    *oauth2.Config
	googleOAuth    *oauth2.Config
	microsoftOAuth *oauth2.Config
}

// NewRouter creates a new API router with all routes registered.
func NewRouter(services *service.Services, st store.Store, hub *websocket.Hub, auth *websocket.AuthService, logger *slog.Logger, agentStore websocket.AgentStore, corsOrigin string, collector *metrics.Collector, rl *RateLimiter, str storage.Store, githubOAuth, googleOAuth, microsoftOAuth *oauth2.Config) *Router {
	r := &Router{
		Router:         chi.NewRouter(),
		services:       services,
		store:          st,
		agentStore:     agentStore,
		hub:            hub,
		auth:           auth,
		logger:         logger,
		corsOrigin:     corsOrigin,
		agentManager:   websocket.NewAgentManager(hub, agentStore, logger),
		collector:      collector,
		rateLimiter:    rl,
		storage:        str,
		stripe:         service.NewStripeService(),
		githubOAuth:    githubOAuth,
		googleOAuth:    googleOAuth,
		microsoftOAuth: microsoftOAuth,
	}
	// Forward the hub's wake callback to the agent manager so HandleAgentHello also records metrics
	r.agentManager.SetWakeCallback(hub.WakeCallback())
	r.setupMiddleware()
	r.setupRoutes()
	r.startCronWorkflows()
	return r
}

func (r *Router) setupMiddleware() {
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(securityHeadersMiddleware)
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

// authenticate middleware validates JWT or API key from the Authorization header.
// If valid, it sets the member in the request context.
func (r *Router) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		authHeader := req.Header.Get("Authorization")
		if authHeader == "" {
			writeErrorCode(w, http.StatusUnauthorized, ErrCodeAuthRequired, "missing Authorization header")
			return
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token == authHeader {
			// Try as API key directly
			token = strings.TrimPrefix(authHeader, "ApiKey ")
			if token == authHeader {
				writeErrorCode(w, http.StatusUnauthorized, ErrCodeAuthInvalidToken, "invalid Authorization format")
				return
			}
		}

		// Try JWT first
		claims, err := r.auth.ValidateToken(token, &tokenBlacklistAdapter{store: r.store})
		if err == nil {
			member, err := r.store.GetMember(req.Context(), claims.MemberID)
			if err != nil {
				r.logger.Error("auth: get member by JWT", "err", err)
				writeErrorCode(w, http.StatusInternalServerError, ErrCodeInternal, "internal error")
				return
			}
			if member == nil {
				writeErrorCode(w, http.StatusUnauthorized, ErrCodeAuthInvalidToken, "member not found")
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
			writeErrorCode(w, http.StatusInternalServerError, ErrCodeInternal, "internal error")
			return
		}
		if member == nil {
			writeErrorCode(w, http.StatusUnauthorized, ErrCodeAuthInvalidCreds, "invalid credentials")
			return
		}
		ctx := context.WithValue(req.Context(), contextKeyMember, member)
		next.ServeHTTP(w, req.WithContext(ctx))
	})
}

// tokenBlacklistAdapter wraps store.Store to implement websocket.TokenBlacklist.
type tokenBlacklistAdapter struct {
	store store.Store
}

func (a *tokenBlacklistAdapter) IsTokenBlacklisted(ctx context.Context, jti string) (bool, error) {
	return a.store.IsTokenBlacklisted(ctx, jti)
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
		writeErrorCode(w, http.StatusUnauthorized, ErrCodeAuthRequired, "not authenticated")
		return nil
	}
	if workspaceID != "" && member.WorkspaceID != workspaceID {
		writeErrorCode(w, http.StatusForbidden, ErrCodeForbidden, "access denied")
		return nil
	}
	return member
}

// requireAdmin returns the authenticated member only if they have admin or owner role.
func requireAdmin(w http.ResponseWriter, req *http.Request) *proto.Member {
	member := memberFromContext(req)
	if member == nil {
		writeErrorCode(w, http.StatusUnauthorized, ErrCodeAuthRequired, "not authenticated")
		return nil
	}
	if member.Role != proto.RoleAdmin && member.Role != proto.RoleOwner {
		writeErrorCode(w, http.StatusForbidden, ErrCodeForbidden, "admin access required")
		return nil
	}
	return member
}


func (r *Router) setupRoutes() {
	// llms.txt — machine-readable platform description for LLMs (no auth required)
	r.Get("/llms.txt", r.handleLLMsTxt)

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
		v1.Post("/auth/register", r.handleRegister)
		v1.Post("/auth/login", r.handleLogin)
		v1.Get("/auth/github", r.handleGithubLogin)
		v1.Get("/auth/github/callback", r.handleGithubCallback)
		v1.Get("/auth/google", r.handleGoogleLogin)
		v1.Get("/auth/google/callback", r.handleGoogleCallback)
		v1.Get("/auth/microsoft", r.handleMicrosoftLogin)
		v1.Get("/auth/microsoft/callback", r.handleMicrosoftCallback)
		v1.Get("/auth/providers", r.handleListProviders)
		v1.Get("/sso/callback", r.handleSSOCallback)
		v1.Post("/workspaces", r.handleCreateWorkspace)
		v1.Post("/workspaces/{id}/agents", r.handleAgentProvision)

		// All other routes require authentication
		v1.Group(func(p chi.Router) {
			p.Use(r.authenticate)

			p.Post("/auth/logout", r.handleLogout)

			// Admin
			p.Get("/admin/stats", r.handleAdminStats)
			p.Get("/admin/workspaces", r.handleAdminListWorkspaces)
			p.Get("/admin/agents", r.handleAdminListAgents)
			p.Post("/admin/backup", r.handleAdminBackup)

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
			p.Get("/members/me", r.handleMyProfile)
			p.Post("/members/me/avatar", r.handleUploadAvatar)
			p.Post("/members/me/password", r.handleChangePassword)

			// Channels
			p.Post("/workspaces/{id}/channels", r.handleCreateChannel)
			p.Get("/workspaces/{id}/channels", r.handleListChannels)
			p.Get("/workspaces/{id}/channels/search", r.handleSearchChannels)
			p.Get("/workspaces/{id}/channels/{channelID}", r.handleGetChannel)
			p.Patch("/workspaces/{id}/channels/{channelID}", r.handleUpdateChannel)
			p.Delete("/workspaces/{id}/channels/{channelID}", r.handleDeleteChannel)
			p.Post("/workspaces/{id}/channels/{channelID}/archive", r.handleArchiveChannel)
			p.Post("/workspaces/{id}/channels/{channelID}/unarchive", r.handleUnarchiveChannel)
			p.Post("/workspaces/{id}/channels/{channelID}/members", r.handleAddChannelMember)
			p.Delete("/workspaces/{id}/channels/{channelID}/members/{memberID}", r.handleRemoveChannelMember)

			// Messages
			p.Post("/channels/{id}/messages", r.handleCreateMessage)
			p.Get("/channels/{id}/messages", r.handleListMessages)
			p.Patch("/messages/{id}", r.handleUpdateMessage)
			p.Delete("/messages/{id}", r.handleDeleteMessage)

			// Threads
			p.Get("/messages/{id}/thread", r.handleGetThread)

			// Edit history
			p.Get("/messages/{id}/edits", r.handleGetEditHistory)

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

			// Agents
			p.Get("/workspaces/{id}/agents", r.handleListAgents)

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
			p.Patch("/channels/{id}/notification-preference", r.handleNotificationPreference)

			// Notifications
			p.Get("/notifications", r.handleListNotifications)
			p.Get("/notifications/unread-count", r.handleCountUnreadNotifications)
			p.Patch("/notifications/{id}/read", r.handleMarkNotificationRead)
			p.Post("/notifications/mark-all-read", r.handleMarkAllNotificationsRead)

			// Integrations
			p.Get("/integrations", r.handleListIntegrations)
			p.Get("/integrations/{id}", r.handleGetIntegration)
			p.Post("/integrations", r.handleCreateIntegration)
			p.Post("/workspaces/{id}/integrations", r.handleInstallIntegration)
			p.Get("/workspaces/{id}/integrations", r.handleListWorkspaceIntegrations)
			p.Delete("/workspaces/{id}/integrations/{integrationId}", r.handleUninstallIntegration)

			// SSO Providers
			p.Get("/workspaces/{id}/sso", r.handleListSSOProviders)
			p.Post("/workspaces/{id}/sso", r.handleCreateSSOProvider)
			p.Delete("/sso/{id}", r.handleDeleteSSOProvider)
			p.Get("/sso/discover", r.handleSSODiscover)

			// Calls
			p.Get("/calls", r.handleListCalls)
			p.Get("/calls/{id}", r.handleGetCall)

			// Workflows
			p.Post("/workspaces/{id}/workflows", r.handleCreateWorkflow)
			p.Get("/workspaces/{id}/workflows", r.handleListWorkflows)
			p.Get("/workflows/{id}", r.handleGetWorkflow)
			p.Patch("/workflows/{id}", r.handleUpdateWorkflow)
			p.Delete("/workflows/{id}", r.handleDeleteWorkflow)
			p.Post("/workflows/{id}/trigger", r.handleTriggerWorkflow)
			p.Get("/workflows/{id}/runs", r.handleListWorkflowRuns)
			p.Get("/workflow-runs/{id}", r.handleGetWorkflowRun)

			// E2EE
			p.Post("/keys", r.handleRegisterKey)
			p.Get("/members/{id}/keys", r.handleGetKeys)
			p.Delete("/keys/{id}", r.handleDeleteKey)
			p.Post("/messages/{id}/encrypted", r.handleSendEncrypted)
			p.Get("/messages/{id}/encrypted", r.handleGetEncrypted)

			// Billing
			p.Get("/workspaces/{id}/billing", r.handleGetBilling)
			p.Post("/workspaces/{id}/billing/checkout", r.handleCreateCheckout)
			p.Post("/workspaces/{id}/billing/portal", r.handleBillingPortal)
			p.Get("/workspaces/{id}/usage", r.handleGetUsage)

			// Webhooks management
			p.Post("/workspaces/{id}/webhooks", r.handleCreateWebhook)
			p.Get("/workspaces/{id}/webhooks", r.handleListWebhooks)
			p.Delete("/webhooks/{id}", r.handleDeleteWebhook)

			// Agent inbox (pull-based)
			p.Get("/agents/{id}/inbox", r.handleListAgentInbox)
			p.Get("/agents/{id}/inbox/count", r.handleCountAgentInbox)
			p.Post("/agents/{id}/inbox/{itemID}/ack", r.handleAckInboxItem)
			p.Post("/agents/{id}/inbox/ack-all", r.handleAckAllInbox)

			// Held drafts
			p.Post("/agents/{id}/drafts", r.handleCreateDraft)
			p.Get("/agents/{id}/drafts", r.handleListDrafts)
			p.Post("/drafts/{id}/validate", r.handleValidateDraft)
			p.Post("/drafts/{id}/send", r.handleSendDraft)
			p.Delete("/drafts/{id}", r.handleCancelDraft)

			// Agent workspace
			p.Post("/agents/{id}/workspace", r.handleCreateWorkspaceItem)
			p.Get("/agents/{id}/workspace", r.handleListWorkspaceItems)
			p.Get("/agents/{id}/workspace/search", r.handleSearchWorkspaceItems)
			p.Get("/agents/{id}/workspace/{itemID}", r.handleGetWorkspaceItem)
			p.Put("/agents/{id}/workspace/{itemID}", r.handleUpdateWorkspaceItem)
			p.Delete("/agents/{id}/workspace/{itemID}", r.handleDeleteWorkspaceItem)

			// Reviews
			p.Post("/workspaces/{id}/reviews", r.handleCreateReview)
			p.Get("/workspaces/{id}/reviews", r.handleListReviews)
			p.Get("/reviews/{id}", r.handleGetReview)
			p.Patch("/reviews/{id}", r.handleUpdateReview)

			// Team templates
			p.Get("/workspaces/{id}/templates", r.handleListTemplates)
			p.Post("/workspaces/{id}/templates", r.handleCreateTemplate)
			p.Get("/templates/{id}", r.handleGetTemplate)
			p.Delete("/templates/{id}", r.handleDeleteTemplate)
			p.Post("/templates/{id}/instantiate", r.handleInstantiateTemplate)

		})
	})

	// WebSocket (rate limited)
	router.Get("/ws", r.handleWebSocket)

	// Stripe webhook (outside auth, verified by signature)
	router.Post("/webhooks/stripe", r.handleStripeWebhook)
	})
}