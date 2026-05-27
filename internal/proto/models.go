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

// MemberRole defines access levels within a workspace.
type MemberRole string

const (
	RoleUser  MemberRole = "user"
	RoleAdmin MemberRole = "admin"
	RoleOwner MemberRole = "owner"
)

// Member represents a human or agent.
type Member struct {
	ID           string       `json:"id"`
	WorkspaceID  string       `json:"workspace_id"`
	Name         string       `json:"name"`
	Email        string       `json:"email,omitempty"`
	PasswordHash string       `json:"-"`
	Type         MemberType   `json:"type"`
	Role         MemberRole   `json:"role"`
	AvatarURL    string       `json:"avatar_url,omitempty"`
	Status       Presence     `json:"status"`
	StatusText   string       `json:"status_text,omitempty"`
	StatusEmoji  string       `json:"status_emoji,omitempty"`
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
	Category    string      `json:"category,omitempty"`
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
	ID          string          `json:"id"`
	ChannelID   string          `json:"channel_id"`
	SenderID    string          `json:"sender_id"`
	ThreadID    string          `json:"thread_id,omitempty"`
	Content     string          `json:"content"`
	ContentType string          `json:"content_type,omitempty"`
	FileID      string          `json:"file_id,omitempty"`
	Type        string          `json:"type"`
	Metadata    json.RawMessage `json:"metadata,omitempty"`
	EditedAt    int64           `json:"edited_at,omitempty"`
	EditCount   int             `json:"edit_count,omitempty"`
	ReplyCount  int             `json:"reply_count,omitempty"`
	CreatedAt   int64           `json:"created_at"`
	UpdatedAt   int64           `json:"updated_at"`
}

// Bookmark represents a saved/bookmarked message for a user.
type Bookmark struct {
	ID        string `json:"id"`
	UserID    string `json:"user_id"`
	MessageID string `json:"message_id"`
	ChannelID string `json:"channel_id"`
	Note      string `json:"note,omitempty"`
	CreatedAt int64  `json:"created_at"`
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

// EditHistory stores previous versions of edited messages.
type EditHistory struct {
	ID        string `json:"id"`
	MessageID string `json:"message_id"`
	Content   string `json:"content"`
	EditedAt  int64  `json:"edited_at"`
	EditedBy  string `json:"edited_by"`
}

// OAuthIdentity links an external OAuth provider account to a member.
type OAuthIdentity struct {
	ID             string `json:"id"`
	MemberID       string `json:"member_id"`
	Provider       string `json:"provider"`
	ProviderUserID string `json:"provider_user_id"`
	Email          string `json:"email,omitempty"`
	CreatedAt      int64  `json:"created_at"`
}

// Notification represents an in-app notification for a member.
type Notification struct {
	ID        string `json:"id"`
	MemberID  string `json:"member_id"`
	Type      string `json:"type"`
	Title     string `json:"title"`
	Body      string `json:"body,omitempty"`
	ChannelID string `json:"channel_id,omitempty"`
	MessageID string `json:"message_id,omitempty"`
	IsRead    bool   `json:"is_read"`
	CreatedAt int64  `json:"created_at"`
}

// IntegrationType defines integration types.
type IntegrationType string

const (
	IntegrationWebhook IntegrationType = "webhook"
	IntegrationBot     IntegrationType = "bot"
	IntegrationOAuth   IntegrationType = "oauth"
	IntegrationCustom  IntegrationType = "custom"
)

// Integration represents an available integration.
type Integration struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Description   string          `json:"description,omitempty"`
	IconURL       string          `json:"icon_url,omitempty"`
	Type          IntegrationType `json:"type"`
	ConfigSchema  json.RawMessage `json:"config_schema,omitempty"`
	CreatedAt     int64           `json:"created_at"`
}

// WorkspaceIntegration represents an installed integration in a workspace.
type WorkspaceIntegration struct {
	ID            string          `json:"id"`
	WorkspaceID   string          `json:"workspace_id"`
	IntegrationID string          `json:"integration_id"`
	InstalledBy   string          `json:"installed_by"`
	Config        json.RawMessage `json:"config,omitempty"`
	Enabled       bool            `json:"enabled"`
	CreatedAt     int64           `json:"created_at"`
}

// SSOProviderType defines SSO protocol types.
type SSOProviderType string

const (
	SSOOIDC SSOProviderType = "oidc"
	SSOSAML SSOProviderType = "saml"
)

// SSOProvider represents a configured SSO provider for a workspace.
type SSOProvider struct {
	ID           string          `json:"id"`
	WorkspaceID  string          `json:"workspace_id"`
	Name         string          `json:"name"`
	Type         SSOProviderType `json:"type"`
	Issuer       string          `json:"issuer,omitempty"`
	ClientID     string          `json:"client_id,omitempty"`
	ClientSecret string          `json:"-"`
	DiscoveryURL string          `json:"discovery_url,omitempty"`
	Domain       string          `json:"domain,omitempty"`
	Enabled      bool            `json:"enabled"`
	CreatedAt    int64           `json:"created_at"`
}

// CallStatus defines call states.
type CallStatus string

const (
	CallRinging CallStatus = "ringing"
	CallAnswered CallStatus = "answered"
	CallEnded   CallStatus = "ended"
	CallMissed  CallStatus = "missed"
)

// CallType defines call media types.
type CallType string

const (
	CallAudio CallType = "audio"
	CallVideo CallType = "video"
)

// Call represents a 1:1 call between two members.
type Call struct {
	ID          string     `json:"id"`
	WorkspaceID string     `json:"workspace_id"`
	ChannelID   string     `json:"channel_id,omitempty"`
	CallerID    string     `json:"caller_id"`
	CalleeID    string     `json:"callee_id"`
	Type        CallType   `json:"type"`
	Status      CallStatus `json:"status"`
	StartedAt   int64      `json:"started_at,omitempty"`
	EndedAt     int64      `json:"ended_at,omitempty"`
	CreatedAt   int64      `json:"created_at"`
}

// Workflow represents an automation workflow.
type Workflow struct {
	ID           string          `json:"id"`
	WorkspaceID  string          `json:"workspace_id"`
	Name         string          `json:"name"`
	Description  string          `json:"description,omitempty"`
	TriggerType  string          `json:"trigger_type"`
	TriggerConfig json.RawMessage `json:"trigger_config,omitempty"`
	Steps        json.RawMessage `json:"steps"`
	Enabled      bool            `json:"enabled"`
	CreatedBy    string          `json:"created_by"`
	CreatedAt    int64           `json:"created_at"`
	UpdatedAt    int64           `json:"updated_at"`
}

// WorkflowRunStatus defines workflow run states.
type WorkflowRunStatus string

const (
	WfRunRunning   WorkflowRunStatus = "running"
	WfRunCompleted WorkflowRunStatus = "completed"
	WfRunFailed    WorkflowRunStatus = "failed"
)

// WorkflowRun represents an execution of a workflow.
type WorkflowRun struct {
	ID          string            `json:"id"`
	WorkflowID  string            `json:"workflow_id"`
	Status      WorkflowRunStatus `json:"status"`
	TriggerData json.RawMessage   `json:"trigger_data,omitempty"`
	Result      json.RawMessage   `json:"result,omitempty"`
	Error       string            `json:"error,omitempty"`
	StartedAt   int64             `json:"started_at"`
	FinishedAt  int64             `json:"finished_at,omitempty"`
}

// UserKeyType defines the type of cryptographic key.
type UserKeyType string

const (
	KeyIdentity    UserKeyType = "identity"
	KeySignedPre   UserKeyType = "signed_pre"
	KeyOneTime     UserKeyType = "one_time"
)

// UserKey represents a registered public key for E2EE.
type UserKey struct {
	ID         string      `json:"id"`
	MemberID   string      `json:"member_id"`
	KeyType    UserKeyType `json:"key_type"`
	PublicKey  string      `json:"public_key"`
	PrivateKey string      `json:"-"`
	CreatedAt  int64       `json:"created_at"`
}

// EncryptedMessage represents an encrypted message payload for a recipient.
type EncryptedMessage struct {
	ID                 string `json:"id"`
	MessageID          string `json:"message_id"`
	RecipientID        string `json:"recipient_id"`
	EncryptedContent   string `json:"encrypted_content"`
	SenderIdentityKey  string `json:"sender_identity_key"`
	EphemeralKey       string `json:"ephemeral_key,omitempty"`
	CreatedAt          int64  `json:"created_at"`
}

// BillingPlan defines subscription plan types.
type BillingPlan string

const (
	PlanFree       BillingPlan = "free"
	PlanPro        BillingPlan = "pro"
	PlanEnterprise BillingPlan = "enterprise"
)

// BillingStatus defines subscription status.
type BillingStatus string

const (
	BillingActive   BillingStatus = "active"
	BillingPastDue  BillingStatus = "past_due"
	BillingCanceled BillingStatus = "canceled"
	BillingTrialing BillingStatus = "trialing"
)

// BillingCustomer represents a workspace's billing relationship.
type BillingCustomer struct {
	ID                   string        `json:"id"`
	WorkspaceID          string        `json:"workspace_id"`
	StripeCustomerID     string        `json:"stripe_customer_id,omitempty"`
	StripeSubscriptionID string        `json:"stripe_subscription_id,omitempty"`
	Plan                 BillingPlan   `json:"plan"`
	Status               BillingStatus `json:"status"`
	CurrentPeriodStart   int64         `json:"current_period_start,omitempty"`
	CurrentPeriodEnd     int64         `json:"current_period_end,omitempty"`
	CreatedAt            int64         `json:"created_at"`
	UpdatedAt            int64         `json:"updated_at"`
}

// UsageRecord tracks a metric for a workspace in a billing period.
type UsageRecord struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Metric      string `json:"metric"`
	Quantity    int    `json:"quantity"`
	PeriodStart int64  `json:"period_start"`
	PeriodEnd   int64  `json:"period_end"`
	CreatedAt   int64  `json:"created_at"`
}

// PlanLimits defines what each plan allows.
type PlanLimits struct {
	MaxMembers    int `json:"max_members"`
	MaxChannels   int `json:"max_channels"`
	MaxMessages   int `json:"max_messages_per_month"`
	MaxFileUpload int `json:"max_file_upload_mb"`
	MaxWorkflows  int `json:"max_workflows"`
	MaxAgents     int `json:"max_agents"`
}

// GetPlanLimits returns limits for a given plan.
func GetPlanLimits(plan BillingPlan) PlanLimits {
	switch plan {
	case PlanPro:
		return PlanLimits{
			MaxMembers:    100,
			MaxChannels:   500,
			MaxMessages:   100000,
			MaxFileUpload: 100,
			MaxWorkflows:  50,
			MaxAgents:     20,
		}
	case PlanEnterprise:
		return PlanLimits{
			MaxMembers:    10000,
			MaxChannels:   50000,
			MaxMessages:   10000000,
			MaxFileUpload: 1000,
			MaxWorkflows:  1000,
			MaxAgents:     500,
		}
	default: // free
		return PlanLimits{
			MaxMembers:    10,
			MaxChannels:   20,
			MaxMessages:   5000,
			MaxFileUpload: 10,
			MaxWorkflows:  3,
			MaxAgents:     2,
		}
	}
}
