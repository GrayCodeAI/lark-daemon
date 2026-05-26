package websocket

import (
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

// Envelope is the wire format for all WebSocket messages.
type Envelope struct {
	V    int             `json:"v"`
	ID   string          `json:"id,omitempty"`
	Type string          `json:"type"`
	TS   int64           `json:"ts"`
	Data json.RawMessage `json:"data,omitempty"`
}

// NewEnvelope creates a new envelope with the given type and data.
func NewEnvelope(eventType string, data any) Envelope {
	b, err := json.Marshal(data)
	if err != nil {
		slog.Error("marshal envelope data", "err", err, "type", eventType)
		b = []byte("null")
	}
	return Envelope{
		V:    1,
		ID:   uuid.New().String(),
		Type: eventType,
		TS:   time.Now().UnixMilli(),
		Data: b,
	}
}

// Event type constants.
const (
	EventAuthLogin   = "auth.login"
	EventAuthSuccess = "auth.success"
	EventAuthFail    = "auth.fail"

	EventAgentHello    = "agent.hello"
	EventAgentWelcome  = "agent.welcome"
	EventAgentSleep    = "agent.sleep"
	EventAgentWake     = "agent.wake"
	EventAgentThinking = "agent.thinking"

	EventMessageSend = "message.send"
	EventMessageNew  = "message.new"
	EventMessageAck  = "message.ack"
	EventMessageEdit = "message.edit"
	EventMessageDel  = "message.delete"

	EventPresenceUpdate = "presence.update"
	EventTypingStart    = "typing.start"
	EventTypingStop     = "typing.stop"

	EventChannelCreate = "channel.create"
	EventChannelJoin   = "channel.join"
	EventChannelLeave  = "channel.leave"

	EventThreadReply = "thread.reply"

	EventApprovalRequest = "approval.request"
	EventApprovalResult  = "approval.result"

	EventError = "error"
)

// Data types for events.

type AuthLoginData struct {
	Token string `json:"token"`
}

type AuthSuccessData struct {
	User interface{} `json:"user"`
}

type AgentHelloData struct {
	Name     string      `json:"name"`
	RoleCard RoleCard    `json:"role_card"`
	Runtime  RuntimeInfo `json:"runtime"`
}

type RoleCard struct {
	SystemPrompt string   `json:"system_prompt"`
	Capabilities []string `json:"capabilities"`
}

type RuntimeInfo struct {
	Type     string `json:"type"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

type AgentWelcomeData struct {
	AgentID string `json:"agent_id"`
	Status  string `json:"status"`
}

type AgentWakeData struct {
	Reason  string      `json:"reason"`
	Context WakeContext `json:"context"`
}

type WakeContext struct {
	Channel        interface{} `json:"channel"`
	RecentMessages []interface{} `json:"recent_messages"`
	Thread         interface{} `json:"thread,omitempty"`
}

type MessageSendData struct {
	ChannelID string          `json:"channel_id"`
	Content   string          `json:"content"`
	ThreadID  string          `json:"thread_id,omitempty"`
	Type      string          `json:"type,omitempty"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
}

type TypingData struct {
	ChannelID string `json:"channel_id"`
	MemberID  string `json:"member_id,omitempty"`
}

type ThreadReplyData struct {
	ChannelID string          `json:"channel_id"`
	ThreadID  string          `json:"thread_id"`
	Content   string          `json:"content"`
	Type      string          `json:"type,omitempty"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
}
