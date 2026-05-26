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
	mu          sync.RWMutex
	connections map[string]*Conn   // member ID -> Conn
	agents      map[string]*Conn   // agent ID -> Conn (subset of connections)
	presence    map[string]string  // member ID -> presence status
	store       StoreQuerier
}

func NewHub() *Hub {
	return &Hub{
		connections: make(map[string]*Conn),
		agents:      make(map[string]*Conn),
		presence:    make(map[string]string),
	}
}

// SetStore sets the store querier for name resolution and wake context.
func (h *Hub) SetStore(s StoreQuerier) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.store = s
}

// Add registers a new connection and sets presence to online.
func (h *Hub) Add(c *Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.connections[c.id] = c
	if c.isAgent {
		h.agents[c.id] = c
	}
	h.presence[c.id] = "online"
	slog.Info("connection added", "id", c.id, "name", c.name, "is_agent", c.isAgent)
}

// Remove unregisters a connection and sets presence to offline.
func (h *Hub) Remove(c *Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.connections, c.id)
	if c.isAgent {
		delete(h.agents, c.id)
	}
	h.presence[c.id] = "offline"
	slog.Info("connection removed", "id", c.id, "name", c.name)
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

// GetAgentByName looks up an agent connection by name.
func (h *Hub) GetAgentByName(name string) *Conn {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, c := range h.agents {
		if c.name == name {
			return c
		}
	}
	return nil
}

// BroadcastToChannel sends a message to all connections subscribed to a channel.
func (h *Hub) BroadcastToChannel(channelID string, env Envelope) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, c := range h.connections {
		if c.IsSubscribed(channelID) {
			c.Send(env)
		}
	}
}

// BroadcastToAll sends a message to all connections.
func (h *Hub) BroadcastToAll(env Envelope) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, c := range h.connections {
		c.Send(env)
	}
}

// BroadcastToAgents sends a message to all agent connections.
func (h *Hub) BroadcastToAgents(env Envelope) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, c := range h.agents {
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
	env := NewEnvelope(EventAgentWake, data)
	c.Send(env)
	slog.Info("agent woken", "agent_id", agentID, "reason", data.Reason)
}

// WakeAgentByName resolves an agent name to a connection and sends a wake signal
// with bundled context (recent messages from the channel).
func (h *Hub) WakeAgentByName(name, channelID, reason string) {
	c := h.GetAgentByName(name)
	if c == nil {
		// Agent not connected — try store lookup to log a useful warning
		h.mu.RLock()
		store := h.store
		h.mu.RUnlock()
		if store != nil {
			// We don't know workspace ID from just the channel, so skip store lookup
			slog.Warn("agent not connected by name", "name", name)
		}
		return
	}

	// Build wake context with recent messages
	ctx := WakeContext{
		Channel: map[string]string{"id": channelID},
	}
	h.mu.RLock()
	store := h.store
	h.mu.RUnlock()
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
		Reason:  reason,
		Context: ctx,
	}
	env := NewEnvelope(EventAgentWake, data)
	c.Send(env)
	slog.Info("agent woken by name", "name", name, "agent_id", c.id, "reason", reason)
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

// AgentCount returns the number of agent connections.
func (h *Hub) AgentCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.agents)
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

// UpgradeConn upgrades an HTTP request to a WebSocket connection.
var UpgradeConn = ws.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}
