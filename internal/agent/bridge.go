package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Bridge connects to a lark-daemon via WebSocket and spawns agent CLI processes.
type Bridge struct {
	serverURL string
	apiKey    string
	name      string
	logger    *slog.Logger

	conn     *websocket.Conn
	connMu   sync.Mutex
	sendChan chan []byte

	detected []DetectedRuntime
	cancel   context.CancelFunc

	// For request-response pattern over WS
	pendingDraftAck chan draftAckData
}

type draftAckData struct {
	DraftID     string `json:"draft_id"`
	RoomVersion int64  `json:"room_version"`
}

// NewBridge creates a new agent bridge.
func NewBridge(serverURL, apiKey, name string, logger *slog.Logger) *Bridge {
	return &Bridge{
		serverURL:       serverURL,
		apiKey:          apiKey,
		name:            name,
		logger:          logger,
		sendChan:        make(chan []byte, 256),
		pendingDraftAck: make(chan draftAckData, 1),
	}
}

// envelope is the wire format for WebSocket messages.
type envelope struct {
	V    int             `json:"v"`
	ID   string          `json:"id,omitempty"`
	Type string          `json:"type"`
	TS   int64           `json:"ts"`
	Data json.RawMessage `json:"data,omitempty"`
}

func (b *Bridge) newEnvelope(eventType string, data any) envelope {
	bts, _ := json.Marshal(data)
	return envelope{
		V:    1,
		Type: eventType,
		TS:   time.Now().UnixMilli(),
		Data: bts,
	}
}

func (b *Bridge) send(env envelope) error {
	bts, err := json.Marshal(env)
	if err != nil {
		return err
	}
	b.connMu.Lock()
	defer b.connMu.Unlock()
	if b.conn == nil {
		return fmt.Errorf("not connected")
	}
	return b.conn.WriteMessage(websocket.TextMessage, bts)
}

// Run starts the bridge: detects runtimes, connects to daemon, and handles events.
func (b *Bridge) Run(ctx context.Context) error {
	ctx, b.cancel = context.WithCancel(ctx)

	// Detect runtimes
	b.detected = DetectRuntimes(ctx)
	b.logger.Info(RuntimeSummary(b.detected))

	// Connect to daemon
	if err := b.connect(ctx); err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer b.close()

	// Authenticate
	if err := b.authenticate(); err != nil {
		return fmt.Errorf("auth: %w", err)
	}

	// Send agent.hello
	if err := b.sendHello(); err != nil {
		return fmt.Errorf("hello: %w", err)
	}

	// Start periodic inbox polling (every 30s)
	go b.inboxPollLoop(ctx)

	// Read loop
	return b.readLoop(ctx)
}

func (b *Bridge) connect(ctx context.Context) error {
	wsURL := strings.Replace(b.serverURL, "http://", "ws://", 1)
	wsURL = strings.Replace(wsURL, "https://", "wss://", 1)
	wsURL = strings.TrimRight(wsURL, "/") + "/ws"

	b.logger.Info("connecting", "url", wsURL)

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}
	conn, _, err := dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return err
	}
	b.conn = conn
	return nil
}

func (b *Bridge) close() {
	b.connMu.Lock()
	defer b.connMu.Unlock()
	if b.conn != nil {
		_ = b.conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		b.conn.Close()
		b.conn = nil
	}
}

func (b *Bridge) authenticate() error {
	env := b.newEnvelope("auth.login", map[string]string{"token": b.apiKey})
	if err := b.send(env); err != nil {
		return err
	}

	// Wait for auth.success
	_ = b.conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	defer b.conn.SetReadDeadline(time.Time{})

	_, msg, err := b.conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("read auth response: %w", err)
	}
	var resp envelope
	if err := json.Unmarshal(msg, &resp); err != nil {
		return fmt.Errorf("parse auth response: %w", err)
	}
	if resp.Type == "auth.fail" {
		var data map[string]string
		_ = json.Unmarshal(resp.Data, &data)
		return fmt.Errorf("auth failed: %s", data["error"])
	}
	if resp.Type != "auth.success" {
		return fmt.Errorf("unexpected auth response: %s", resp.Type)
	}

	b.logger.Info("authenticated")
	return nil
}

func (b *Bridge) sendHello() error {
	// Build capabilities from detected runtimes
	caps := make([]string, 0, len(b.detected))
	for _, d := range b.detected {
		caps = append(caps, string(d.Type))
	}

	// Determine primary runtime
	primaryProvider := "unknown"
	primaryType := "cli"
	primaryModel := ""
	if len(b.detected) > 0 {
		primaryProvider = b.detected[0].Provider
		primaryType = string(b.detected[0].Type)
		if b.detected[0].Version != "" {
			primaryModel = b.detected[0].Version
		}
	}

	env := b.newEnvelope("agent.hello", map[string]any{
		"name": b.name,
		"role_card": map[string]any{
			"system_prompt": fmt.Sprintf("I am %s, a multi-runtime agent bridge. I can spawn and manage %s.", b.name, strings.Join(caps, ", ")),
			"capabilities":  caps,
		},
		"runtime": map[string]any{
			"type":     primaryType,
			"provider": primaryProvider,
			"model":    primaryModel,
		},
	})
	return b.send(env)
}

func (b *Bridge) readLoop(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		_, msg, err := b.conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
				return nil
			}
			return fmt.Errorf("read: %w", err)
		}

		var env envelope
		if err := json.Unmarshal(msg, &env); err != nil {
			b.logger.Warn("invalid envelope", "err", err)
			continue
		}

		b.handleEvent(ctx, env)
	}
}

func (b *Bridge) handleEvent(ctx context.Context, env envelope) {
	switch env.Type {
	case "agent.wake":
		b.handleWake(ctx, env)
	case "agent.welcome":
		b.logger.Info("agent registered")
	case "inbox.items":
		b.handleInboxItems(env)
	case "draft.ack":
		var ack draftAckData
		if err := json.Unmarshal(env.Data, &ack); err == nil {
			select {
			case b.pendingDraftAck <- ack:
			default:
			}
		}
	case "draft.send":
		b.logger.Info("draft sent successfully")
	case "error":
		var data map[string]string
		_ = json.Unmarshal(env.Data, &data)
		b.logger.Warn("server error", "error", data["error"])
	default:
		b.logger.Debug("event", "type", env.Type)
	}
}

func (b *Bridge) handleWake(ctx context.Context, env envelope) {
	var data struct {
		Reason  string `json:"reason"`
		Context struct {
			Channel struct {
				ID string `json:"id"`
			} `json:"channel"`
			RecentMessages []struct {
				ID        string `json:"id"`
				SenderID  string `json:"sender_id"`
				Content   string `json:"content"`
				CreatedAt int64  `json:"created_at"`
			} `json:"recent_messages"`
		} `json:"context"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		b.logger.Warn("invalid wake data", "err", err)
		return
	}

	b.logger.Info("agent woken", "reason", data.Reason, "channel", data.Context.Channel.ID)

	// Signal thinking
	_ = b.send(b.newEnvelope("agent.thinking", map[string]string{
		"channel_id": data.Context.Channel.ID,
	}))

	// Build prompt from recent messages, with inbox context prepended
	prompt := b.buildPrompt(data.Context.RecentMessages)

	// Spawn the best available runtime
	rt := b.pickRuntime()
	if rt == nil {
		b.logger.Error("no runtime available")
		return
	}

	b.logger.Info("spawning", "runtime", rt.Type, "prompt_len", len(prompt))
	go b.spawnAgent(ctx, rt, data.Context.Channel.ID, prompt)
}

func (b *Bridge) buildPrompt(messages []struct {
	ID        string `json:"id"`
	SenderID  string `json:"sender_id"`
	Content   string `json:"content"`
	CreatedAt int64  `json:"created_at"`
}) string {
	var sb strings.Builder
	sb.WriteString("Recent conversation:\n\n")
	for _, m := range messages {
		fmt.Fprintf(&sb, "[%s] %s\n", m.SenderID, m.Content)
	}
	sb.WriteString("\nRespond to the latest message.")
	return sb.String()
}

func (b *Bridge) pickRuntime() *DetectedRuntime {
	// Prefer Claude, then Codex, then Gemini, then Kimi
	for _, rt := range []RuntimeType{RuntimeClaude, RuntimeCodex, RuntimeGemini, RuntimeKimi} {
		if d := RuntimeByType(b.detected, rt); d != nil {
			return d
		}
	}
	return nil
}

func (b *Bridge) spawnAgent(ctx context.Context, rt *DetectedRuntime, channelID, prompt string) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	var cmd *exec.Cmd
	switch rt.Type {
	case RuntimeClaude:
		cmd = exec.CommandContext(ctx, rt.Path, "--print", "--output-format", "text", prompt)
	case RuntimeCodex:
		cmd = exec.CommandContext(ctx, rt.Path, "--quiet", prompt)
	case RuntimeGemini:
		cmd = exec.CommandContext(ctx, rt.Path, "-p", prompt)
	case RuntimeKimi:
		cmd = exec.CommandContext(ctx, rt.Path, "chat", "--message", prompt)
	default:
		b.logger.Error("unsupported runtime", "type", rt.Type)
		return
	}

	cmd.Env = append(os.Environ(), "LARK_CHANNEL_ID="+channelID)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		b.logger.Error("stdout pipe", "err", err)
		return
	}
	stderr, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		b.logger.Error("spawn", "err", err)
		return
	}

	// Read output
	var output strings.Builder
	scanner := bufio.NewScanner(io.MultiReader(stdout, stderr))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		output.WriteString(scanner.Text())
		output.WriteByte('\n')
	}

	if err := cmd.Wait(); err != nil {
		b.logger.Error("agent exit", "err", err)
	}

	response := strings.TrimSpace(output.String())
	if response == "" {
		response = "(no output)"
	}

	// Use held draft pattern: create draft → wait for ack → send draft
	_ = b.send(b.newEnvelope("draft.create", map[string]string{
		"channel_id": channelID,
		"content":    response,
	}))

	// Wait for draft.ack (with timeout)
	select {
	case ack := <-b.pendingDraftAck:
		b.logger.Info("draft created", "draft_id", ack.DraftID, "room_version", ack.RoomVersion)
		// Send the draft (validates room_version server-side)
		_ = b.send(b.newEnvelope("draft.send", map[string]any{
			"draft_id": ack.DraftID,
		}))
	case <-time.After(5 * time.Second):
		b.logger.Warn("draft.ack timeout, falling back to direct send")
		_ = b.send(b.newEnvelope("message.send", map[string]any{
			"channel_id":   channelID,
			"content":      response,
			"content_type": "text",
		}))
	case <-ctx.Done():
		return
	}

	// Go back to sleep
	_ = b.send(b.newEnvelope("agent.sleep", nil))
	b.logger.Info("agent response sent", "len", len(response))
}

// inboxPollLoop periodically polls the agent inbox for missed events.
func (b *Bridge) inboxPollLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.pollInbox()
		}
	}
}

// pollInbox requests inbox items from the server. Items arrive via inbox.items event.
func (b *Bridge) pollInbox() {
	_ = b.send(b.newEnvelope("inbox.poll", map[string]any{
		"unread_only": true,
		"limit":       20,
	}))
}

// handleInboxItems processes inbox items received from the server.
func (b *Bridge) handleInboxItems(env envelope) {
	var items []struct {
		ID         string `json:"id"`
		Type       string `json:"type"`
		Title      string `json:"title"`
		Body       string `json:"body"`
		SourceType string `json:"source_type"`
		Priority   string `json:"priority"`
		Payload    string `json:"payload"`
	}
	if err := json.Unmarshal(env.Data, &items); err != nil {
		b.logger.Warn("invalid inbox items", "err", err)
		return
	}
	if len(items) == 0 {
		return
	}
	b.logger.Info("inbox items received", "count", len(items))
	for _, item := range items {
		b.logger.Info("inbox item", "id", item.ID, "type", item.Type, "title", item.Title)
		// Acknowledge the item after processing
		b.send(b.newEnvelope("inbox.ack", map[string]string{"item_id": item.ID}))
	}
}
