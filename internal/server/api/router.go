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
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"lark/internal/proto"
	"lark/internal/server/service"
	"lark/internal/server/store"
	"lark/internal/server/websocket"
)

// Router wraps chi.Router with Lark-specific handlers.
type Router struct {
	chi.Router
	services   *service.Services
	store      store.Store
	agentStore websocket.AgentStore
	hub        *websocket.Hub
	auth       *websocket.AuthService
	logger     *slog.Logger
}

// NewRouter creates a new API router with all routes registered.
func NewRouter(services *service.Services, st store.Store, hub *websocket.Hub, auth *websocket.AuthService, logger *slog.Logger, agentStore websocket.AgentStore) *Router {
	r := &Router{
		Router:     chi.NewRouter(),
		services:   services,
		store:      st,
		agentStore: agentStore,
		hub:        hub,
		auth:       auth,
		logger:     logger,
	}
	r.setupMiddleware()
	r.setupRoutes()
	return r
}

func (r *Router) setupMiddleware() {
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: false,
		MaxAge:           300,
	}))
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
				writeError(w, http.StatusInternalServerError, err.Error())
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
			writeError(w, http.StatusInternalServerError, err.Error())
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


func (r *Router) setupRoutes() {
	// Health check
	r.Get("/health", func(w http.ResponseWriter, req *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	r.Route("/v1", func(v1 chi.Router) {
		// Unauthenticated bootstrap routes
		v1.Post("/workspaces", r.handleCreateWorkspace)
		v1.Post("/workspaces/{id}/agents", r.handleAgentProvision)

		// All other routes require authentication
		v1.Group(func(p chi.Router) {
			p.Use(r.authenticate)

			// Workspaces
			p.Get("/workspaces", r.handleListWorkspaces)
			p.Get("/workspaces/{id}", r.handleGetWorkspace)
			p.Patch("/workspaces/{id}", r.handleUpdateWorkspace)

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
		})
	})

	// WebSocket
	r.Get("/ws", r.handleWebSocket)
}

// --- Helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func decodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
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
	ws := &proto.Workspace{Name: body.Name, Slug: body.Slug}
	if err := r.services.CreateWorkspace(req.Context(), ws); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, ws)
}

func (r *Router) handleListWorkspaces(w http.ResponseWriter, req *http.Request) {
	wss, err := r.services.ListWorkspaces(req.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, wss)
}

func (r *Router) handleGetWorkspace(w http.ResponseWriter, req *http.Request) {
	ws, err := r.services.GetWorkspace(req.Context(), chi.URLParam(req, "id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if ws == nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	writeJSON(w, http.StatusOK, ws)
}

func (r *Router) handleUpdateWorkspace(w http.ResponseWriter, req *http.Request) {
	existing, err := r.services.GetWorkspace(req.Context(), chi.URLParam(req, "id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	// Save protected fields before decode.
	id := existing.ID
	token := existing.AgentProvisionToken
	if err := decodeJSON(req, existing); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	existing.ID = id
	existing.AgentProvisionToken = token
	if err := r.services.UpdateWorkspace(req.Context(), existing); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, existing)
}

// --- Members ---

func (r *Router) handleCreateMember(w http.ResponseWriter, req *http.Request) {
	var body struct {
		Name      string `json:"name"`
		Type      string `json:"type"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
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
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, m)
}

func (r *Router) handleListMembers(w http.ResponseWriter, req *http.Request) {
	members, err := r.services.ListMembers(req.Context(), chi.URLParam(req, "id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
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
		writeError(w, http.StatusInternalServerError, err.Error())
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
	existing, _ := r.services.GetMemberByName(req.Context(), workspaceID, body.Name)
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
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{
		"member_id": m.ID,
		"api_key":   m.APIKey,
		"name":      m.Name,
	})
}

func (r *Router) handleGetMember(w http.ResponseWriter, req *http.Request) {
	member, err := r.services.GetMember(req.Context(), chi.URLParam(req, "memberID"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if member == nil {
		writeError(w, http.StatusNotFound, "member not found")
		return
	}
	writeJSON(w, http.StatusOK, member)
}

func (r *Router) handleUpdateMember(w http.ResponseWriter, req *http.Request) {
	existing, err := r.services.GetMember(req.Context(), chi.URLParam(req, "memberID"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "member not found")
		return
	}
	// Save protected fields before decode.
	id := existing.ID
	wsID := existing.WorkspaceID
	mtype := existing.Type
	apiKey := existing.APIKey
	if err := decodeJSON(req, existing); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	existing.ID = id
	existing.WorkspaceID = wsID
	existing.Type = mtype
	existing.APIKey = apiKey
	if err := r.services.UpdateMember(req.Context(), existing); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, existing)
}

func (r *Router) handleDeleteMember(w http.ResponseWriter, req *http.Request) {
	if err := r.services.DeleteMember(req.Context(), chi.URLParam(req, "memberID")); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Channels ---

func (r *Router) handleCreateChannel(w http.ResponseWriter, req *http.Request) {
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
	if err := r.services.CreateChannel(req.Context(), ch); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, ch)
}

func (r *Router) handleListChannels(w http.ResponseWriter, req *http.Request) {
	channels, err := r.services.ListChannels(req.Context(), chi.URLParam(req, "id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, channels)
}

func (r *Router) handleGetChannel(w http.ResponseWriter, req *http.Request) {
	ch, err := r.services.GetChannel(req.Context(), chi.URLParam(req, "channelID"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if ch == nil {
		writeError(w, http.StatusNotFound, "channel not found")
		return
	}
	writeJSON(w, http.StatusOK, ch)
}

func (r *Router) handleUpdateChannel(w http.ResponseWriter, req *http.Request) {
	existing, err := r.services.GetChannel(req.Context(), chi.URLParam(req, "channelID"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "channel not found")
		return
	}
	// Save protected fields before decode.
	id := existing.ID
	wsID := existing.WorkspaceID
	chType := existing.Type
	if err := decodeJSON(req, existing); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	existing.ID = id
	existing.WorkspaceID = wsID
	existing.Type = chType
	if err := r.services.UpdateChannel(req.Context(), existing); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, existing)
}

func (r *Router) handleDeleteChannel(w http.ResponseWriter, req *http.Request) {
	if err := r.services.DeleteChannel(req.Context(), chi.URLParam(req, "channelID")); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (r *Router) handleAddChannelMember(w http.ResponseWriter, req *http.Request) {
	var body struct {
		MemberID string `json:"member_id"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := r.services.AddChannelMember(req.Context(), chi.URLParam(req, "channelID"), body.MemberID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (r *Router) handleRemoveChannelMember(w http.ResponseWriter, req *http.Request) {
	if err := r.services.RemoveChannelMember(req.Context(), chi.URLParam(req, "channelID"), chi.URLParam(req, "memberID")); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Messages ---

func (r *Router) handleCreateMessage(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	var body struct {
		Content string `json:"content"`
		ThreadID string `json:"thread_id"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	msg := &proto.Message{
		ChannelID: chi.URLParam(req, "id"),
		SenderID:  member.ID,
		Content:   body.Content,
		ThreadID:  body.ThreadID,
	}
	if err := r.services.CreateMessage(req.Context(), msg); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	r.hub.SendNewMessage(msg.ChannelID, msg)
	r.handleMentions(msg)
	writeJSON(w, http.StatusCreated, msg)
}

func (r *Router) handleListMessages(w http.ResponseWriter, req *http.Request) {
	limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(req.URL.Query().Get("offset"))
	msgs, err := r.services.ListMessages(req.Context(), chi.URLParam(req, "id"), limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, msgs)
}

func (r *Router) handleUpdateMessage(w http.ResponseWriter, req *http.Request) {
	existing, err := r.services.GetMessage(req.Context(), chi.URLParam(req, "id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "message not found")
		return
	}
	// Save protected fields before decode.
	id := existing.ID
	channelID := existing.ChannelID
	senderID := existing.SenderID
	createdAt := existing.CreatedAt
	if err := decodeJSON(req, existing); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	existing.ID = id
	existing.ChannelID = channelID
	existing.SenderID = senderID
	existing.CreatedAt = createdAt
	if err := r.services.UpdateMessage(req.Context(), existing); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, existing)
}

func (r *Router) handleDeleteMessage(w http.ResponseWriter, req *http.Request) {
	if err := r.services.DeleteMessage(req.Context(), chi.URLParam(req, "id")); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Threads ---

func (r *Router) handleGetThread(w http.ResponseWriter, req *http.Request) {
	msgs, err := r.services.ListThreadMessages(req.Context(), chi.URLParam(req, "id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
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
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, reaction)
}

func (r *Router) handleListReactions(w http.ResponseWriter, req *http.Request) {
	reactions, err := r.services.ListReactions(req.Context(), chi.URLParam(req, "id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
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
		writeError(w, http.StatusInternalServerError, err.Error())
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
	msgs, err := r.services.SearchMessages(req.Context(), q, channelID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, msgs)
}

// --- Tasks ---

func (r *Router) handleCreateTask(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
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
	if err := r.services.CreateTask(req.Context(), task); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, task)
}

func (r *Router) handleListTasks(w http.ResponseWriter, req *http.Request) {
	status := proto.TaskStatus(req.URL.Query().Get("status"))
	tasks, err := r.services.ListTasks(req.Context(), chi.URLParam(req, "id"), status)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, tasks)
}

func (r *Router) handleUpdateTask(w http.ResponseWriter, req *http.Request) {
	existing, err := r.services.GetTask(req.Context(), chi.URLParam(req, "id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	// Save protected fields before decode.
	id := existing.ID
	wsID := existing.WorkspaceID
	createdBy := existing.CreatedBy
	createdAt := existing.CreatedAt
	if err := decodeJSON(req, existing); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	existing.ID = id
	existing.WorkspaceID = wsID
	existing.CreatedBy = createdBy
	existing.CreatedAt = createdAt
	if err := r.services.UpdateTask(req.Context(), existing); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, existing)
}

func (r *Router) handleDeleteTask(w http.ResponseWriter, req *http.Request) {
	if err := r.services.DeleteTask(req.Context(), chi.URLParam(req, "id")); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Agent Memory ---

func (r *Router) handleSetMemory(w http.ResponseWriter, req *http.Request) {
	var body struct {
		Namespace string `json:"namespace"`
		Key       string `json:"key"`
		Value     string `json:"value"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	mem := &proto.AgentMemory{
		AgentID:   chi.URLParam(req, "id"),
		Namespace: body.Namespace,
		Key:       body.Key,
		Value:     body.Value,
	}
	if mem.Namespace == "" {
		mem.Namespace = "default"
	}
	if err := r.services.SetMemory(req.Context(), mem); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, mem)
}

func (r *Router) handleListMemory(w http.ResponseWriter, req *http.Request) {
	namespace := req.URL.Query().Get("namespace")
	if namespace == "" {
		namespace = "default"
	}
	memories, err := r.services.ListMemory(req.Context(), chi.URLParam(req, "id"), namespace)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, memories)
}

func (r *Router) handleDeleteMemory(w http.ResponseWriter, req *http.Request) {
	if err := r.services.DeleteMemory(req.Context(), chi.URLParam(req, "id"), chi.URLParam(req, "namespace"), chi.URLParam(req, "key")); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- WebSocket ---

func (r *Router) handleWebSocket(w http.ResponseWriter, req *http.Request) {
	conn, err := websocket.UpgradeConn.Upgrade(w, req, nil)
	if err != nil {
		r.logger.Error("websocket upgrade failed", "err", err)
		return
	}
	wc := websocket.NewConn(r.hub, conn, "", "", false)
	go wc.WritePump()
	go wc.ReadPump(func(env websocket.Envelope) {
		r.handleWSEvent(wc, env)
	})
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
		am := websocket.NewAgentManager(r.hub, r.agentStore, r.logger)
		am.HandleAgentHello(c, data)

	case websocket.EventAgentSleep:
		am := websocket.NewAgentManager(r.hub, r.agentStore, r.logger)
		am.HandleAgentSleep(c)

	case websocket.EventAgentThinking:
		var td struct {
			ChannelID string `json:"channel_id"`
		}
		json.Unmarshal(env.Data, &td)
		am := websocket.NewAgentManager(r.hub, r.agentStore, r.logger)
		am.HandleAgentThinking(c, td.ChannelID)

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
		member, err := r.store.GetMemberByAPIKey(context.Background(), data.Token)
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
		r.subscribeMemberChannels(c, member.ID, member.WorkspaceID)

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
	member, err := r.store.GetMember(context.Background(), claims.MemberID)
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
	r.subscribeMemberChannels(c, member.ID, member.WorkspaceID)

	c.Send(websocket.NewEnvelope(websocket.EventAuthSuccess, map[string]any{
		"member_id":    member.ID,
		"workspace_id": member.WorkspaceID,
		"name":         member.Name,
		"type":         member.Type,
	}))
	r.hub.SendPresenceUpdate(member.ID, string(proto.PresenceOnline))
}

func (r *Router) subscribeMemberChannels(c *websocket.Conn, memberID, workspaceID string) {
	channels, err := r.store.ListChannels(context.Background(), workspaceID)
	if err != nil {
		return
	}
	for _, ch := range channels {
		isMember, err := r.store.IsChannelMember(context.Background(), ch.ID, memberID)
		if err != nil || !isMember {
			continue
		}
		c.Subscribe(ch.ID)
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
	isMember, err := r.store.IsChannelMember(context.Background(), data.ChannelID, c.ID())
	if err != nil || !isMember {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "not a channel member"}))
		return
	}
	c.Subscribe(data.ChannelID)
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
	if err := r.services.CreateMessage(context.Background(), msg); err != nil {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": err.Error()}))
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
	msg, err := r.services.GetMessage(context.Background(), data.MessageID)
	if err != nil {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "internal error"}))
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
	if err := r.services.UpdateMessage(context.Background(), msg); err != nil {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": err.Error()}))
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
	msg, err := r.services.GetMessage(context.Background(), data.MessageID)
	if err != nil {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "internal error"}))
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
	if err := r.services.DeleteMessage(context.Background(), data.MessageID); err != nil {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": err.Error()}))
		return
	}
	r.hub.BroadcastToChannel(msg.ChannelID, websocket.NewEnvelope(websocket.EventMessageDel, map[string]string{"id": data.MessageID}))
}

// --- Approval handlers ---

func (r *Router) handleCreateApproval(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
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
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Broadcast to workspace that an approval is pending
	r.hub.BroadcastToAll(websocket.NewEnvelope(websocket.EventApprovalRequest, a))
	writeJSON(w, http.StatusCreated, a)
}

func (r *Router) handleGetApproval(w http.ResponseWriter, req *http.Request) {
	a, err := r.services.GetApproval(req.Context(), chi.URLParam(req, "id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if a == nil {
		writeError(w, http.StatusNotFound, "approval not found")
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (r *Router) handleListApprovals(w http.ResponseWriter, req *http.Request) {
	status := proto.ApprovalStatus(req.URL.Query().Get("status"))
	approvals, err := r.services.ListApprovals(req.Context(), chi.URLParam(req, "id"), status)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
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
		writeError(w, http.StatusInternalServerError, err.Error())
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
		writeError(w, http.StatusInternalServerError, err.Error())
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
	member, err := r.services.GetMember(context.Background(), c.ID())
	if err != nil {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "internal error"}))
		return
	}
	if member == nil {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "agent not found"}))
		return
	}
	a.WorkspaceID = member.WorkspaceID
	if err := r.services.CreateApproval(context.Background(), a); err != nil {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": err.Error()}))
		return
	}
	// Notify humans that approval is pending
	r.hub.BroadcastToAll(websocket.NewEnvelope(websocket.EventApprovalRequest, a))
	c.Send(websocket.NewEnvelope(websocket.EventApprovalRequest, map[string]string{"id": a.ID, "status": "pending"}))
}

// --- Agent metrics ---

func (r *Router) handleGetAgentMetrics(w http.ResponseWriter, req *http.Request) {
	agentID := chi.URLParam(req, "id")
	// Get all metrics from agent_memory with namespace "_metrics"
	memories, err := r.services.ListMemory(req.Context(), agentID, "_metrics")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	metrics := make(map[string]interface{})
	for _, m := range memories {
		metrics[m.Key] = m.Value
	}
	// Also get task counts
	member, _ := r.services.GetMember(req.Context(), agentID)
	if member != nil {
		tasks, _ := r.services.ListTasks(req.Context(), member.WorkspaceID, "")
		var assigned, completed int
		for _, t := range tasks {
			if t.AssignedTo == agentID {
				assigned++
				if t.Status == proto.TaskDone {
					completed++
				}
			}
		}
		metrics["tasks_assigned"] = assigned
		metrics["tasks_completed"] = completed
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"agent_id": agentID,
		"metrics":  metrics,
	})
}

func (r *Router) handleWSTypingStart(c *websocket.Conn, env websocket.Envelope) {
	if !c.IsAuthenticated() {
		return
	}
	var data struct {
		ChannelID string `json:"channel_id"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil || data.ChannelID == "" {
		return
	}
	isMember, err := r.store.IsChannelMember(context.Background(), data.ChannelID, c.ID())
	if err != nil || !isMember {
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
		return
	}
	var data struct {
		ChannelID string `json:"channel_id"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil || data.ChannelID == "" {
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
	isMember, err := r.store.IsChannelMember(context.Background(), data.ChannelID, c.ID())
	if err != nil || !isMember {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "not a channel member"}))
		return
	}
	msg := &proto.Message{
		ChannelID: data.ChannelID,
		SenderID:  c.ID(),
		Content:   data.Content,
		ThreadID:  data.ParentID,
	}
	if err := r.services.CreateMessage(context.Background(), msg); err != nil {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": err.Error()}))
		return
	}
	r.hub.BroadcastToChannel(data.ChannelID, websocket.NewEnvelope(websocket.EventMessageNew, msg))
	c.Send(websocket.NewEnvelope(websocket.EventMessageAck, map[string]string{"id": msg.ID}))

	// Check for @mentions in thread replies too
	r.handleMentions(msg)
}

// --- File handlers ---

func (r *Router) handleUploadFile(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	workspaceID := chi.URLParam(req, "id")
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
		writeError(w, http.StatusInternalServerError, "failed to create upload directory")
		return
	}
	// Write file
	fID := proto.NewID()
	ext := filepath.Ext(header.Filename)
	savePath := filepath.Join(uploadDir, fID+ext)
	dst, err := os.Create(savePath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save file")
		return
	}
	defer dst.Close()
	size, err := io.Copy(dst, file)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to write file")
		return
	}
	// Detect MIME type
	buf := make([]byte, 512)
	dst.Seek(0, 0)
	n, _ := dst.Read(buf)
	mimeType := http.DetectContentType(buf[:n])
	// Save to DB
	f := &proto.File{
		ID:          fID,
		WorkspaceID: workspaceID,
		UploaderID:  member.ID,
		Filename:    header.Filename,
		MimeType:    mimeType,
		Size:        size,
		Path:        savePath,
	}
	if err := r.services.CreateFile(req.Context(), f); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, f)
}

func (r *Router) handleGetFile(w http.ResponseWriter, req *http.Request) {
	f, err := r.services.GetFile(req.Context(), chi.URLParam(req, "id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if f == nil {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}
	writeJSON(w, http.StatusOK, f)
}

func (r *Router) handleListFiles(w http.ResponseWriter, req *http.Request) {
	limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(req.URL.Query().Get("offset"))
	files, err := r.services.ListFiles(req.Context(), chi.URLParam(req, "id"), limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, files)
}

func (r *Router) handleDeleteFile(w http.ResponseWriter, req *http.Request) {
	fileID := chi.URLParam(req, "id")
	f, err := r.services.GetFile(req.Context(), fileID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if f == nil {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}
	if removeErr := os.Remove(f.Path); removeErr != nil && !os.IsNotExist(removeErr) {
		writeError(w, http.StatusInternalServerError, "failed to delete file from disk")
		return
	}
	if err := r.services.DeleteFile(req.Context(), fileID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (r *Router) handleDownloadFile(w http.ResponseWriter, req *http.Request) {
	f, err := r.services.GetFile(req.Context(), chi.URLParam(req, "id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if f == nil {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filepath.Base(f.Filename)+"\"")
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
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (r *Router) handleListPins(w http.ResponseWriter, req *http.Request) {
	pins, err := r.services.ListPins(req.Context(), chi.URLParam(req, "id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, pins)
}

func (r *Router) handleUnpinMessage(w http.ResponseWriter, req *http.Request) {
	if err := r.services.DeletePin(req.Context(), chi.URLParam(req, "id")); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
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
	// Check if DM already exists
	existing, _ := r.services.GetDMChannel(req.Context(), workspaceID, body.MemberIDs)
	if existing != nil {
		writeJSON(w, http.StatusOK, existing)
		return
	}
	// Create new DM channel
	chType := proto.ChannelDM
	if len(body.MemberIDs) > 2 {
		chType = proto.ChannelGroupDM
	}
	ch := &proto.Channel{
		WorkspaceID: workspaceID,
		Name:        body.Name,
		Type:        chType,
	}
	if err := r.services.CreateChannel(req.Context(), ch); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Add members
	for _, mid := range body.MemberIDs {
		if err := r.services.AddChannelMember(req.Context(), ch.ID, mid); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to add member: "+err.Error())
			return
		}
	}
	writeJSON(w, http.StatusCreated, ch)
}

func (r *Router) handleListDMs(w http.ResponseWriter, req *http.Request) {
	channels, err := r.services.ListDMChannels(req.Context(), chi.URLParam(req, "id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, channels)
}

// --- Unread handlers ---

func (r *Router) handleGetUnread(w http.ResponseWriter, req *http.Request) {
	counts, err := r.services.GetUnreadCounts(req.Context(), chi.URLParam(req, "id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
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
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
