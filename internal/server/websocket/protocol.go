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

	// Daemon proxy
	EventDaemonRegister   = "daemon.register"
	EventDaemonRegistered = "daemon.registered"

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

	EventNotificationNew  = "notification.new"
	EventNotificationRead = "notification.read"

	EventCallOffer  = "call.offer"
	EventCallAnswer = "call.answer"
	EventCallICE    = "call.ice"
	EventCallEnd    = "call.end"
	EventCallRing   = "call.ring"

	EventThreadReply = "thread.reply"

	EventApprovalRequest = "approval.request"
	EventApprovalResult  = "approval.result"

	// Agent inbox
	EventInboxPoll  = "inbox.poll"
	EventInboxItems = "inbox.items"
	EventInboxAck   = "inbox.ack"

	// Held drafts
	EventDraftCreate   = "draft.create"
	EventDraftAck      = "draft.ack"
	EventDraftValidate = "draft.validate"
	EventDraftResult   = "draft.result"
	EventDraftSend     = "draft.send"

	// Reviews
	EventReviewRequest = "review.request"
	EventReviewResult  = "review.result"

	// Agent workspace
	EventWorkspaceUpdate = "workspace.update"

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
	Reason    string      `json:"reason"`
	Context   WakeContext `json:"context"`
	AgentName string      `json:"agent_name,omitempty"` // For daemon routing
}

type WakeContext struct {
	Channel        interface{} `json:"channel"`
	RecentMessages []interface{} `json:"recent_messages"`
	Thread         interface{} `json:"thread,omitempty"`
}

type MessageSendData struct {
	ChannelID   string          `json:"channel_id"`
	Content     string          `json:"content"`
	ThreadID    string          `json:"thread_id,omitempty"`
	ContentType string          `json:"content_type,omitempty"`
	Type        string          `json:"type,omitempty"`
	Metadata    json.RawMessage `json:"metadata,omitempty"`
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

// Inbox event data types.

type InboxPollData struct {
	SourceType string `json:"source_type,omitempty"`
	UnreadOnly bool   `json:"unread_only,omitempty"`
	Since      int64  `json:"since,omitempty"`
	Limit      int    `json:"limit,omitempty"`
}

type InboxAckData struct {
	ItemID string `json:"item_id"`
}

// Draft event data types.

type DraftCreateData struct {
	ChannelID string `json:"channel_id"`
	Content   string `json:"content"`
	ThreadID  string `json:"thread_id,omitempty"`
}

type DraftAckData struct {
	DraftID      string `json:"draft_id"`
	RoomVersion  int64  `json:"room_version"`
}

type DraftValidateData struct {
	DraftID string `json:"draft_id"`
}

type DraftResultData struct {
	DraftID        string        `json:"draft_id"`
	Valid          bool          `json:"valid"`
	CurrentVersion int64         `json:"current_version"`
	DraftVersion   int64         `json:"draft_version"`
	VersionDelta   int64         `json:"version_delta"`
	RecentMessages []interface{} `json:"recent_messages,omitempty"`
}

type DraftSendData struct {
	DraftID      string `json:"draft_id"`
	ForceVersion bool   `json:"force_version,omitempty"`
}

// Review event data types.

type ReviewRequestData struct {
	ReviewerID string `json:"reviewer_id"`
	Subject    string `json:"subject"`
	Content    string `json:"content"`
	ChannelID  string `json:"channel_id"`
}

type ReviewResultData struct {
	ReviewID  string `json:"review_id"`
	Status    string `json:"status"`
	Comment   string `json:"comment,omitempty"`
}

// Workspace event data types.

type WorkspaceUpdateData struct {
	ItemID      string   `json:"item_id,omitempty"`
	Name        string   `json:"name"`
	Content     string   `json:"content,omitempty"`
	Namespace   string   `json:"namespace,omitempty"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

// Daemon event data types.

type DaemonRegisterData struct {
	Agents []DaemonAgent `json:"agents"`
}

type DaemonAgent struct {
	Name    string `json:"name"`
	AgentID string `json:"agent_id"`
}

type DaemonRegisteredData struct {
	DaemonID string `json:"daemon_id"`
	Agents   []DaemonAgent `json:"agents"`
}
