package websocket

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	ws "github.com/gorilla/websocket"
)

const (
	pingInterval = 30 * time.Second
	pongWait     = 60 * time.Second
)

// Conn wraps a WebSocket connection with identity and channel subscriptions.
type Conn struct {
	conn          *ws.Conn
	mu            sync.Mutex    // protects id, name, isAgent, authenticated, channels, closed
	writeMu       sync.Mutex    // protects writes to the underlying conn
	id            string
	name          string
	isAgent       bool
	authenticated bool
	channels      map[string]bool
	send          chan []byte
	hub           *Hub
	closed        bool
	ctx           context.Context
	cancel        context.CancelFunc
}

func NewConn(hub *Hub, conn *ws.Conn, id, name string, isAgent bool) *Conn {
	ctx, cancel := context.WithCancel(context.Background())
	return &Conn{
		conn:     conn,
		id:       id,
		name:     name,
		isAgent:  isAgent,
		channels: make(map[string]bool),
		send:     make(chan []byte, 256),
		hub:      hub,
		ctx:      ctx,
		cancel:   cancel,
	}
}

// Context returns a context that is cancelled when the connection closes.
func (c *Conn) Context() context.Context {
	return c.ctx
}

// ID returns the member ID.
func (c *Conn) ID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.id
}

// Name returns the member name.
func (c *Conn) Name() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.name
}

// IsAgent returns whether this is an agent connection.
func (c *Conn) IsAgent() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
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

// MaxSubscriptions is the maximum number of channels a single connection can subscribe to.
const MaxSubscriptions = 200

// Subscribe adds this connection to a channel. Returns false if the limit is reached.
func (c *Conn) Subscribe(channelID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.channels) >= MaxSubscriptions {
		return false
	}
	c.channels[channelID] = true
	return true
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

// Send enqueues an envelope for the WritePump to send.
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

// Close signals the WritePump to stop and closes the underlying connection.
func (c *Conn) Close() {
	c.CloseWithMessage(0, nil)
}

// CloseWithMessage sends a close frame (if messageType > 0) then closes.
func (c *Conn) CloseWithMessage(messageType int, data []byte) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	close(c.send)
	c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
	}
	if c.conn != nil {
		c.writeMu.Lock()
		if messageType > 0 {
			c.conn.WriteMessage(messageType, data)
		}
		c.conn.Close()
		c.writeMu.Unlock()
	}
}

// ReadPump reads messages from the WebSocket.
func (c *Conn) ReadPump(handler func(env Envelope)) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("ws read pump panic", "recover", r, "conn_id", c.ID())
		}
		c.hub.Remove(c)
		c.Close()
	}()

	c.conn.SetReadLimit(65536)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if ws.IsUnexpectedCloseError(err, ws.CloseGoingAway, ws.CloseNormalClosure) {
				slog.Error("ws read error", "err", err, "conn_id", c.ID())
			}
			break
		}

		var env Envelope
		if err := json.Unmarshal(message, &env); err != nil {
			slog.Error("unmarshal envelope", "err", err, "conn_id", c.ID())
			continue
		}
		handler(env)
	}
}

// WritePump writes messages from the send channel to the WebSocket.
func (c *Conn) WritePump() {
	ticker := time.NewTicker(pingInterval)
	defer func() {
		ticker.Stop()
		c.writeMu.Lock()
		c.conn.Close()
		c.writeMu.Unlock()
	}()

	for {
		select {
		case msg, ok := <-c.send:
			if !ok {
				// send channel closed
				return
			}
			c.writeMu.Lock()
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			err := c.conn.WriteMessage(ws.TextMessage, msg)
			c.writeMu.Unlock()
			if err != nil {
				slog.Error("ws write error", "err", err, "conn_id", c.ID())
				return
			}
		case <-ticker.C:
			c.writeMu.Lock()
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(ws.PingMessage, nil); err != nil {
				c.writeMu.Unlock()
				slog.Error("ws ping error", "err", err, "conn_id", c.ID())
				return
			}
			c.writeMu.Unlock()
		}
	}
}
