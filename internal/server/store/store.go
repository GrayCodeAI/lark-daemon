package store

import (
	"context"

	"lark-daemon/internal/proto"
)

// Store defines the data access interface.
type Store interface {
	// Workspace operations.
	CreateWorkspace(ctx context.Context, ws *proto.Workspace) error
	GetWorkspace(ctx context.Context, id string) (*proto.Workspace, error)
	GetWorkspaceBySlug(ctx context.Context, slug string) (*proto.Workspace, error)
	ListWorkspaces(ctx context.Context) ([]*proto.Workspace, error)
	UpdateWorkspace(ctx context.Context, ws *proto.Workspace) error
	DeleteWorkspace(ctx context.Context, id string) error

	// Member operations.
	CreateMember(ctx context.Context, m *proto.Member) error
	GetMember(ctx context.Context, id string) (*proto.Member, error)
	GetMemberByAPIKey(ctx context.Context, key string) (*proto.Member, error)
	GetMemberByName(ctx context.Context, workspaceID, name string) (*proto.Member, error)
	ListMembers(ctx context.Context, workspaceID string) ([]*proto.Member, error)
	ListMembersPaginated(ctx context.Context, workspaceID string, limit, offset int) ([]*proto.Member, error)
	UpdateMember(ctx context.Context, m *proto.Member) error
	UpdateMemberRoleCard(ctx context.Context, memberID string, roleCard *proto.RoleCard, runtime *proto.RuntimeInfo) error
	DeleteMember(ctx context.Context, id string) error

	// Channel operations.
	CreateChannel(ctx context.Context, ch *proto.Channel) error
	GetChannel(ctx context.Context, id string) (*proto.Channel, error)
	ListChannels(ctx context.Context, workspaceID string) ([]*proto.Channel, error)
	ListChannelsPaginated(ctx context.Context, workspaceID string, limit, offset int) ([]*proto.Channel, error)
	UpdateChannel(ctx context.Context, ch *proto.Channel) error
	DeleteChannel(ctx context.Context, id string) error

	// Channel membership.
	AddChannelMember(ctx context.Context, channelID, memberID string) error
	RemoveChannelMember(ctx context.Context, channelID, memberID string) error
	ListChannelMembers(ctx context.Context, channelID string) ([]*proto.Member, error)
	IsChannelMember(ctx context.Context, channelID, memberID string) (bool, error)
	ListMemberChannelIDs(ctx context.Context, memberID string) ([]string, error)
	UpdateLastRead(ctx context.Context, channelID, memberID string) error

	// Message operations.
	CreateMessage(ctx context.Context, m *proto.Message) error
	GetMessage(ctx context.Context, id string) (*proto.Message, error)
	ListMessages(ctx context.Context, channelID string, limit, offset int) ([]*proto.Message, error)
	ListThreadMessages(ctx context.Context, threadID string) ([]*proto.Message, error)
	ListMessagesBySender(ctx context.Context, senderID string) ([]*proto.Message, error)
	CountMessagesBySender(ctx context.Context, senderID string) (int, error)
	UpdateMessage(ctx context.Context, m *proto.Message) error
	DeleteMessage(ctx context.Context, id string) error
	GetRecentMessages(ctx context.Context, channelID string, limit int) ([]*proto.Message, error)

	// Reactions.
	AddReaction(ctx context.Context, r *proto.Reaction) error
	RemoveReaction(ctx context.Context, messageID, memberID, emoji string) error
	ListReactions(ctx context.Context, messageID string) ([]*proto.Reaction, error)

	// Search.
	SearchMessages(ctx context.Context, query string, channelID string, limit int) ([]*proto.Message, error)

	// Task operations.
	CreateTask(ctx context.Context, t *proto.Task) error
	GetTask(ctx context.Context, id string) (*proto.Task, error)
	ListTasks(ctx context.Context, workspaceID string, status proto.TaskStatus) ([]*proto.Task, error)
	CountTasksByAssignee(ctx context.Context, assigneeID string) (completed, pending int, err error)
	UpdateTask(ctx context.Context, t *proto.Task) error
	DeleteTask(ctx context.Context, id string) error

	// Agent memory.
	SetMemory(ctx context.Context, m *proto.AgentMemory) error
	GetMemory(ctx context.Context, agentID, namespace, key string) (*proto.AgentMemory, error)
	ListMemory(ctx context.Context, agentID, namespace string) ([]*proto.AgentMemory, error)
	DeleteMemory(ctx context.Context, agentID, namespace, key string) error

	// File operations.
	CreateFile(ctx context.Context, f *proto.File) error
	GetFile(ctx context.Context, id string) (*proto.File, error)
	ListFiles(ctx context.Context, workspaceID string, limit, offset int) ([]*proto.File, error)
	DeleteFile(ctx context.Context, id string) error

	// Pin operations.
	CreatePin(ctx context.Context, p *proto.Pin) error
	DeletePin(ctx context.Context, messageID string) error
	ListPins(ctx context.Context, channelID string) ([]*proto.Pin, error)

	// DM operations.
	GetDMChannel(ctx context.Context, workspaceID string, memberIDs []string) (*proto.Channel, error)
	ListDMChannels(ctx context.Context, memberID string) ([]*proto.Channel, error)
	CreateDMChannel(ctx context.Context, ch *proto.Channel, memberIDs []string) error

	// Unread counts.
	GetUnreadCounts(ctx context.Context, memberID string) (map[string]int, error)

	// Approval requests.
	CreateApproval(ctx context.Context, a *proto.ApprovalRequest) error
	GetApproval(ctx context.Context, id string) (*proto.ApprovalRequest, error)
	ListApprovals(ctx context.Context, workspaceID string, status proto.ApprovalStatus) ([]*proto.ApprovalRequest, error)
	UpdateApproval(ctx context.Context, a *proto.ApprovalRequest) error

	// Webhook operations.
	CreateWebhook(ctx context.Context, w *proto.Webhook) error
	GetWebhook(ctx context.Context, id string) (*proto.Webhook, error)
	ListWebhooks(ctx context.Context, workspaceID string) ([]*proto.Webhook, error)
	DeleteWebhook(ctx context.Context, id string) error

	// Lifecycle.
	Ping(ctx context.Context) error
	Close() error
}
