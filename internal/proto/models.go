package proto

import "encoding/json"

// MemberType distinguishes humans from agents.
type MemberType string

const (
	MemberHuman MemberType = "human"
	MemberAgent MemberType = "agent"
)

// ChannelType distinguishes channel types.
type ChannelType string

const (
	ChannelPublic  ChannelType = "channel"
	ChannelDM      ChannelType = "dm"
	ChannelGroupDM ChannelType = "group_dm"
)

// Presence status.
type Presence string

const (
	PresenceOnline  Presence = "online"
	PresenceIdle    Presence = "idle"
	PresenceOffline Presence = "offline"
	PresenceDND     Presence = "dnd"
)

// Agent status.
type AgentStatus string

const (
	AgentAwake    AgentStatus = "awake"
	AgentSleeping AgentStatus = "sleeping"
)

// Task status.
type TaskStatus string

const (
	TaskTodo       TaskStatus = "todo"
	TaskInProgress TaskStatus = "in_progress"
	TaskReview     TaskStatus = "review"
	TaskDone       TaskStatus = "done"
)

// Task priority.
const (
	TaskPriorityLow    = "low"
	TaskPriorityMedium = "medium"
	TaskPriorityHigh   = "high"
	TaskPriorityUrgent = "urgent"

	AgentProvisionTokenPrefix = "lpt_"
	AgentAPIKeyPrefix         = "lr_"
)

// Workspace represents a workspace.
type Workspace struct {
	ID                  string `json:"id"`
	Name                string `json:"name"`
	Slug                string `json:"slug"`
	IconURL             string `json:"icon_url,omitempty"`
	AgentProvisionToken string `json:"agent_provision_token,omitempty"`
	CreatedAt           int64  `json:"created_at"`
	UpdatedAt           int64  `json:"updated_at"`
}

// Member represents a human or agent.
type Member struct {
	ID           string       `json:"id"`
	WorkspaceID  string       `json:"workspace_id"`
	Name         string       `json:"name"`
	Email        string       `json:"email,omitempty"`
	PasswordHash string       `json:"-"`
	Type         MemberType   `json:"type"`
	AvatarURL    string       `json:"avatar_url,omitempty"`
	Status       Presence     `json:"status"`
	APIKey       string       `json:"api_key,omitempty"`
	RoleCard     *RoleCard    `json:"role_card,omitempty"`
	Capabilities []string     `json:"capabilities,omitempty"`
	RuntimeInfo  *RuntimeInfo `json:"runtime_info,omitempty"`
	CreatedAt    int64        `json:"created_at"`
	UpdatedAt    int64        `json:"updated_at"`
}

// Channel represents a channel, DM, or group DM.
type Channel struct {
	ID          string      `json:"id"`
	WorkspaceID string      `json:"workspace_id"`
	Name        string      `json:"name"`
	Type        ChannelType `json:"type"`
	Topic       string      `json:"topic,omitempty"`
	IsPrivate   bool        `json:"is_private"`
	IsArchived  bool        `json:"is_archived,omitempty"`
	CreatedAt   int64       `json:"created_at"`
	UpdatedAt   int64       `json:"updated_at"`
}

// ChannelMember represents membership in a channel.
type ChannelMember struct {
	ChannelID              string `json:"channel_id"`
	MemberID               string `json:"member_id"`
	LastReadAt             int64  `json:"last_read_at,omitempty"`
	NotificationPreference string `json:"notification_preference"`
}

// Message represents a chat message.
type Message struct {
	ID        string          `json:"id"`
	ChannelID string          `json:"channel_id"`
	SenderID  string          `json:"sender_id"`
	ThreadID  string          `json:"thread_id,omitempty"`
	Content   string          `json:"content"`
	FileID    string          `json:"file_id,omitempty"`
	Type      string          `json:"type"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
	CreatedAt int64           `json:"created_at"`
	UpdatedAt int64           `json:"updated_at"`
}

// Pin represents a pinned message.
type Pin struct {
	ID        string `json:"id"`
	MessageID string `json:"message_id"`
	ChannelID string `json:"channel_id"`
	PinnedBy  string `json:"pinned_by"`
	CreatedAt int64  `json:"created_at"`
}

// UnreadCount represents unread message count for a channel.
type UnreadCount struct {
	ChannelID string `json:"channel_id"`
	Count     int    `json:"count"`
}

// ApprovalStatus is the state of an approval request.
type ApprovalStatus string

const (
	ApprovalPending  ApprovalStatus = "pending"
	ApprovalApproved ApprovalStatus = "approved"
	ApprovalDenied   ApprovalStatus = "denied"
)

// ApprovalRequest represents an agent's request for human approval.
type ApprovalRequest struct {
	ID          string         `json:"id"`
	WorkspaceID string         `json:"workspace_id"`
	AgentID     string         `json:"agent_id"`
	ChannelID   string         `json:"channel_id,omitempty"`
	Action      string         `json:"action"`
	Payload     string         `json:"payload,omitempty"`
	Status      ApprovalStatus `json:"status"`
	ReviewerID  string         `json:"reviewer_id,omitempty"`
	ReviewNote  string         `json:"review_note,omitempty"`
	CreatedAt   int64          `json:"created_at"`
	ReviewedAt  int64          `json:"reviewed_at,omitempty"`
}

// Task represents a task assignable to agents.
type Task struct {
	ID          string     `json:"id"`
	WorkspaceID string     `json:"workspace_id"`
	ChannelID   string     `json:"channel_id,omitempty"`
	AssignedTo  string     `json:"assigned_to,omitempty"`
	CreatedBy   string     `json:"created_by"`
	Title       string     `json:"title"`
	Description string     `json:"description,omitempty"`
	Status      TaskStatus `json:"status"`
	Priority    string     `json:"priority"`
	DueAt       int64      `json:"due_at,omitempty"`
	CreatedAt   int64      `json:"created_at"`
	UpdatedAt   int64      `json:"updated_at"`
}

// Reaction represents an emoji reaction on a message.
type Reaction struct {
	MessageID string `json:"message_id"`
	MemberID  string `json:"member_id"`
	Emoji     string `json:"emoji"`
	CreatedAt int64  `json:"created_at"`
}

// AgentMemory represents a stored memory for an agent.
type AgentMemory struct {
	ID        string `json:"id"`
	AgentID   string `json:"agent_id"`
	Namespace string `json:"namespace"`
	Key       string `json:"key"`
	Value     string `json:"value"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

// File represents an uploaded file.
type File struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	UploaderID  string `json:"uploader_id"`
	Filename    string `json:"filename"`
	MimeType    string `json:"mime_type"`
	Size        int64  `json:"size"`
	Path        string `json:"path"`
	CreatedAt   int64  `json:"created_at"`
}

// Webhook represents an incoming webhook that posts to a channel.
type Webhook struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	ChannelID   string `json:"channel_id"`
	Name        string `json:"name"`
	Secret      string `json:"secret,omitempty"`
	CreatedBy   string `json:"created_by"`
	CreatedAt   int64  `json:"created_at"`
}

// RoleCard defines an agent's identity.
type RoleCard struct {
	SystemPrompt string   `json:"system_prompt"`
	Capabilities []string `json:"capabilities"`
}

// RuntimeInfo describes an agent's LLM runtime.
type RuntimeInfo struct {
	Type     string `json:"type"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

