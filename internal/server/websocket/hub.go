package websocket

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"

	ws "github.com/gorilla/websocket"
)

// StoreQuerier is a minimal interface for the hub to query member/message data.
type StoreQuerier interface {
	GetMemberByName(workspaceID, name string) (*MemberBrief, error)
	GetRecentMessages(channelID string, limit int) ([]MessageBrief, error)
}

// MemberBrief is a minimal member representation for the hub.
type MemberBrief struct {
	ID          string
	Name        string
	IsAgent     bool
	WorkspaceID string
}

// MessageBrief is a minimal message for wake context.
type MessageBrief struct {
	ID        string
	SenderID  string
	Content   string
	CreatedAt int64
}

// Hub manages all WebSocket connections.
type Hub struct {
	mu            sync.RWMutex
	connections   map[string]*Conn            // member ID -> Conn
	agents        map[string]*Conn            // agent ID -> Conn (subset of connections)
	agentNames    map[string]*Conn            // agent name -> Conn (O(1) name lookup)
	channels      map[string]map[string]*Conn // channel ID -> member ID -> Conn
	presence      map[string]string           // member ID -> presence status
	store         StoreQuerier
	onWake        func(agentID string) // callback for metrics recording
	allowedOrigin string

	// Daemon proxy support
	daemons       map[string]*DaemonProxy // daemon ID -> proxy
	daemonAgents  map[string]*Conn        // agent name -> daemon Conn (for daemon-hosted agents)
}

// DaemonProxy represents a connected daemon that proxies for multiple agents.
type DaemonProxy struct {
	ID      string            `json:"id"`
	Conn    *Conn             `json:"-"`
	Agents  map[string]string `json:"agents"` // agent_name -> agent_id
}

func NewHub() *Hub {
	return &Hub{
		connections:  make(map[string]*Conn),
		agents:       make(map[string]*Conn),
		agentNames:   make(map[string]*Conn),
		channels:     make(map[string]map[string]*Conn),
		presence:     make(map[string]string),
		daemons:      make(map[string]*DaemonProxy),
		daemonAgents: make(map[string]*Conn),
	}
}

// SetStore sets the store querier for name resolution and wake context.
func (h *Hub) SetStore(s StoreQuerier) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.store = s
}

// SetWakeCallback sets a callback invoked when an agent is woken.
func (h *Hub) SetWakeCallback(fn func(agentID string)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onWake = fn
}

// WakeCallback returns the current wake callback.
func (h *Hub) WakeCallback() func(agentID string) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.onWake
}

// Add registers a new connection and sets presence to online.
// If a connection with the same ID already exists, the old connection is closed asynchronously.
func (h *Hub) Add(c *Conn) {
	h.mu.Lock()
	id := c.ID()
	isAgent := c.IsAgent()
	name := c.Name()
	var old *Conn
	if prev, ok := h.connections[id]; ok {
		old = prev
		slog.Warn("replacing stale connection", "id", id, "name", name)
	}
	h.connections[id] = c
	if isAgent {
		h.agents[id] = c
		h.agentNames[name] = c
	}
	h.presence[id] = "online"
	h.mu.Unlock()
	if old != nil {
		old.Close()
	}
	slog.Info("connection added", "id", id, "name", name, "is_agent", isAgent)
}

// Remove unregisters a connection and sets presence to offline.
func (h *Hub) Remove(c *Conn) {
	h.mu.Lock()
	id := c.ID()
	name := c.Name()
	isAgent := c.IsAgent()
	delete(h.connections, id)
	if isAgent {
		delete(h.agents, id)
		delete(h.agentNames, name)
	}
	// Remove from all channel subscriptions
	for chID, cm := range h.channels {
		delete(cm, id)
		if len(cm) == 0 {
			delete(h.channels, chID)
		}
	}
	h.presence[id] = "offline"
	h.mu.Unlock()
	slog.Info("connection removed", "id", id, "name", name)
}

// GetPresence returns the presence status of a member.
func (h *Hub) GetPresence(memberID string) string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if p, ok := h.presence[memberID]; ok {
		return p
	}
	return "offline"
}

// SetPresence sets a member's presence status and broadcasts the change.
func (h *Hub) SetPresence(memberID, status string) {
	h.mu.Lock()
	h.presence[memberID] = status
	h.mu.Unlock()
	h.SendPresenceUpdate(memberID, status)
}

// GetConn returns the connection for a member ID.
func (h *Hub) GetConn(memberID string) *Conn {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.connections[memberID]
}

// GetAgent returns the agent connection for an agent ID.
func (h *Hub) GetAgent(agentID string) *Conn {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.agents[agentID]
}

// GetAgentByName looks up an agent connection by name (O(1)).
func (h *Hub) GetAgentByName(name string) *Conn {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.agentNames[name]
}

// SubscribeChannel adds a connection to a channel's subscriber list.
func (h *Hub) SubscribeChannel(channelID string, c *Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	cm := h.channels[channelID]
	if cm == nil {
		cm = make(map[string]*Conn)
		h.channels[channelID] = cm
	}
	cm[c.ID()] = c
}

// UnsubscribeChannel removes a connection from a channel's subscriber list.
func (h *Hub) UnsubscribeChannel(channelID, memberID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if cm, ok := h.channels[channelID]; ok {
		delete(cm, memberID)
		if len(cm) == 0 {
			delete(h.channels, channelID)
		}
	}
}

// BroadcastToChannel sends a message to all connections subscribed to a channel (O(1) lookup).
func (h *Hub) BroadcastToChannel(channelID string, env Envelope) {
	h.mu.RLock()
	cm := h.channels[channelID]
	conns := make([]*Conn, 0, len(cm))
	for _, c := range cm {
		conns = append(conns, c)
	}
	h.mu.RUnlock()
	for _, c := range conns {
		c.Send(env)
	}
}

// BroadcastToAll sends a message to all connections.
func (h *Hub) BroadcastToAll(env Envelope) {
	h.mu.RLock()
	conns := make([]*Conn, 0, len(h.connections))
	for _, c := range h.connections {
		conns = append(conns, c)
	}
	h.mu.RUnlock()
	for _, c := range conns {
		c.Send(env)
	}
}

// BroadcastToAgents sends a message to all agent connections.
func (h *Hub) BroadcastToAgents(env Envelope) {
	h.mu.RLock()
	conns := make([]*Conn, 0, len(h.agents))
	for _, c := range h.agents {
		conns = append(conns, c)
	}
	h.mu.RUnlock()
	for _, c := range conns {
		c.Send(env)
	}
}

// WakeAgent sends a wake signal to a specific agent by ID.
func (h *Hub) WakeAgent(agentID string, data AgentWakeData) {
	c := h.GetAgent(agentID)
	if c == nil {
		slog.Warn("agent not connected, cannot wake", "agent_id", agentID)
		return
	}
	c.Send(NewEnvelope(EventAgentWake, data))
}

// AgentCount returns the number of agent connections.
func (h *Hub) AgentCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.agents)
}

// RegisterDaemon registers a daemon proxy connection.
func (h *Hub) RegisterDaemon(daemonID string, c *Conn, agents map[string]string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	proxy := &DaemonProxy{
		ID:     daemonID,
		Conn:   c,
		Agents: agents,
	}
	h.daemons[daemonID] = proxy

	// Register each agent as routed through this daemon
	for agentName := range agents {
		h.daemonAgents[agentName] = c
		h.agentNames[agentName] = c
	}

	slog.Info("daemon registered", "daemon_id", daemonID, "agents", len(agents))
}

// UnregisterDaemon unregisters a daemon proxy connection.
func (h *Hub) UnregisterDaemon(daemonID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	proxy, exists := h.daemons[daemonID]
	if !exists {
		return
	}

	// Remove all agent registrations
	for agentName := range proxy.Agents {
		delete(h.daemonAgents, agentName)
		delete(h.agentNames, agentName)
	}

	delete(h.daemons, daemonID)
	slog.Info("daemon unregistered", "daemon_id", daemonID)
}

// WakeAgentByName resolves an agent name to a connection and sends a wake signal
// with bundled context (recent messages from the channel).
func (h *Hub) WakeAgentByName(name, channelID, reason string) {
	// Check daemon agents first, then direct agents
	h.mu.RLock()
	c := h.daemonAgents[name]
	if c == nil {
		c = h.agentNames[name]
	}
	store := h.store
	h.mu.RUnlock()

	if c == nil {
		slog.Warn("agent not connected by name", "name", name)
		return
	}

	// Build wake context with recent messages
	ctx := WakeContext{
		Channel: map[string]string{"id": channelID},
	}
	if store != nil {
		msgs, err := store.GetRecentMessages(channelID, 20)
		if err == nil {
			recent := make([]interface{}, len(msgs))
			for i, m := range msgs {
				recent[i] = map[string]any{
					"id":         m.ID,
					"sender_id":  m.SenderID,
					"content":    m.Content,
					"created_at": m.CreatedAt,
				}
			}
			ctx.RecentMessages = recent
		}
	}

	data := AgentWakeData{
		Reason:     reason,
		Context:    ctx,
		AgentName:  name, // Include agent name for daemon routing
	}
	env := NewEnvelope(EventAgentWake, data)
	c.Send(env)
	if h.onWake != nil {
		h.onWake(c.ID())
	}
	slog.Info("agent woken by name", "name", name, "agent_id", c.ID(), "reason", reason)
}

// DaemonCount returns the number of connected daemons.
func (h *Hub) DaemonCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.daemons)
}

// SendTypingIndicator broadcasts a typing indicator to a channel.
func (h *Hub) SendTypingIndicator(channelID, memberID string) {
	env := NewEnvelope(EventTypingStart, TypingData{
		ChannelID: channelID,
		MemberID:  memberID,
	})
	h.BroadcastToChannel(channelID, env)
}

// SendPresenceUpdate broadcasts a presence change.
func (h *Hub) SendPresenceUpdate(memberID, status string) {
	env := NewEnvelope(EventPresenceUpdate, map[string]string{
		"member_id": memberID,
		"status":    status,
	})
	h.BroadcastToAll(env)
}

// SendNewMessage broadcasts a new message to a channel.
func (h *Hub) SendNewMessage(channelID string, msg any) {
	env := NewEnvelope(EventMessageNew, map[string]any{
		"message": msg,
	})
	h.BroadcastToChannel(channelID, env)
}

// Total returns the number of active connections.
func (h *Hub) Total() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.connections)
}

// Close gracefully closes all WebSocket connections.
func (h *Hub) Close() {
	h.mu.Lock()
	conns := make([]*Conn, 0, len(h.connections))
	for _, c := range h.connections {
		conns = append(conns, c)
	}
	h.connections = make(map[string]*Conn)
	h.agents = make(map[string]*Conn)
	h.agentNames = make(map[string]*Conn)
	h.channels = make(map[string]map[string]*Conn)
	h.presence = make(map[string]string)
	h.mu.Unlock()
	for _, c := range conns {
		c.CloseWithMessage(ws.CloseMessage, []byte(`{"type":"shutdown"}`))
	}
	slog.Info("hub closed, all connections dropped", "count", len(conns))
}

// ParseAgentHello parses an agent.hello event from raw JSON.
func ParseAgentHello(data json.RawMessage) (*AgentHelloData, error) {
	var d AgentHelloData
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// ParseAuthLogin parses an auth.login event from raw JSON.
func ParseAuthLogin(data json.RawMessage) (*AuthLoginData, error) {
	var d AuthLoginData
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// ParseMessageSend parses a message.send event from raw JSON.
func ParseMessageSend(data json.RawMessage) (*MessageSendData, error) {
	var d MessageSendData
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// SetAllowedOrigin sets the allowed CORS origin for WebSocket upgrades.
func (h *Hub) SetAllowedOrigin(origin string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.allowedOrigin = origin
}

// Upgrade upgrades an HTTP request to a WebSocket connection with CORS origin validation.
func (h *Hub) Upgrade(w http.ResponseWriter, r *http.Request) (*ws.Conn, error) {
	h.mu.RLock()
	allowedOrigin := h.allowedOrigin
	h.mu.RUnlock()
	upgrader := ws.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(req *http.Request) bool {
			origin := req.Header.Get("Origin")
			if origin == "" {
				return true
			}
			if allowedOrigin != "" && allowedOrigin != "*" {
				return origin == allowedOrigin
			}
			host := req.Header.Get("Host")
			if host == "" {
				return false
			}
			return origin == "http://"+host || origin == "https://"+host
		},
	}
	return upgrader.Upgrade(w, r, nil)
}
