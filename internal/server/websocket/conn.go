package websocket

import (
	"encoding/json"
	"log/slog"
	"sync"

	ws "github.com/gorilla/websocket"
)

// Conn wraps a WebSocket connection with identity and channel subscriptions.
type Conn struct {
	conn          *ws.Conn
	mu            sync.Mutex
	id            string // member ID
	name          string
	isAgent       bool
	authenticated bool
	channels      map[string]bool // subscribed channel IDs
	send          chan []byte
	hub           *Hub
	closed        bool
}

func NewConn(hub *Hub, conn *ws.Conn, id, name string, isAgent bool) *Conn {
	return &Conn{
		conn:     conn,
		id:       id,
		name:     name,
		isAgent:  isAgent,
		channels: make(map[string]bool),
		send:     make(chan []byte, 256),
		hub:      hub,
	}
}

// ID returns the member ID.
func (c *Conn) ID() string {
	return c.id
}

// Name returns the member name.
func (c *Conn) Name() string {
	return c.name
}

// IsAgent returns whether this is an agent connection.
func (c *Conn) IsAgent() bool {
	return c.isAgent
}

// IsAuthenticated returns whether this connection is authenticated.
func (c *Conn) IsAuthenticated() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.authenticated
}

// SetAuthenticated sets the authentication state.
func (c *Conn) SetAuthenticated(auth bool) {
	c.mu.Lock()
	c.authenticated = auth
	c.mu.Unlock()
}

// SetIdentity sets the connection identity.
func (c *Conn) SetIdentity(id, name string, isAgent bool) {
	c.mu.Lock()
	c.id = id
	c.name = name
	c.isAgent = isAgent
	c.mu.Unlock()
}

// Subscribe adds this connection to a channel.
func (c *Conn) Subscribe(channelID string) {
	c.mu.Lock()
	c.channels[channelID] = true
	c.mu.Unlock()
}

// Unsubscribe removes this connection from a channel.
func (c *Conn) Unsubscribe(channelID string) {
	c.mu.Lock()
	delete(c.channels, channelID)
	c.mu.Unlock()
}

// IsSubscribed checks if this connection is in a channel.
func (c *Conn) IsSubscribed(channelID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.channels[channelID]
}

// Send sends an envelope to this connection.
func (c *Conn) Send(env Envelope) {
	b, err := json.Marshal(env)
	if err != nil {
		slog.Error("marshal envelope", "err", err)
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	select {
	case c.send <- b:
	default:
		slog.Warn("send buffer full, dropping message", "conn_id", c.id)
	}
}

// Close closes the connection.
func (c *Conn) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	close(c.send)
	c.conn.Close()
}

// ReadPump reads messages from the WebSocket.
func (c *Conn) ReadPump(handler func(env Envelope)) {
	defer func() {
		c.hub.Remove(c)
		c.Close()
	}()

	c.conn.SetReadLimit(65536)
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if ws.IsUnexpectedCloseError(err, ws.CloseGoingAway, ws.CloseNormalClosure) {
				slog.Error("ws read error", "err", err, "conn_id", c.id)
			}
			break
		}

		var env Envelope
		if err := json.Unmarshal(message, &env); err != nil {
			slog.Error("unmarshal envelope", "err", err, "conn_id", c.id)
			continue
		}
		handler(env)
	}
}

// WritePump writes messages to the WebSocket.
func (c *Conn) WritePump() {
	defer c.conn.Close()

	for msg := range c.send {
		if err := c.conn.WriteMessage(ws.TextMessage, msg); err != nil {
			slog.Error("ws write error", "err", err, "conn_id", c.id)
			return
		}
	}
}
