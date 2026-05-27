package service

import (
	"context"
	"fmt"

	"lark-daemon/internal/proto"
	"lark-daemon/internal/server/store"
	"lark-daemon/internal/server/websocket"
)

// Services wraps the store and provides business logic.
type Services struct {
	store store.Store
}

// NewServices creates a new Services.
func NewServices(s store.Store) *Services {
	return &Services{store: s}
}

// --- Workspaces ---

func (s *Services) CreateWorkspace(ctx context.Context, ws *proto.Workspace) error {
	if ws.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	ws.AgentProvisionToken = websocket.GenerateProvisionToken()
	return s.store.CreateWorkspace(ctx, ws)
}

func (s *Services) GetWorkspace(ctx context.Context, id string) (*proto.Workspace, error) {
	return s.store.GetWorkspace(ctx, id)
}

func (s *Services) ListWorkspaces(ctx context.Context) ([]*proto.Workspace, error) {
	return s.store.ListWorkspaces(ctx)
}

func (s *Services) UpdateWorkspace(ctx context.Context, ws *proto.Workspace) error {
	return s.store.UpdateWorkspace(ctx, ws)
}

func (s *Services) DeleteWorkspace(ctx context.Context, id string) error {
	return s.store.DeleteWorkspace(ctx, id)
}

// --- Members ---

func (s *Services) CreateMember(ctx context.Context, m *proto.Member) error {
	if m.Type == proto.MemberAgent && m.APIKey == "" {
		m.APIKey = websocket.GenerateAPIKey(proto.AgentAPIKeyPrefix)
	}
	return s.store.CreateMember(ctx, m)
}

func (s *Services) GetMember(ctx context.Context, id string) (*proto.Member, error) {
	return s.store.GetMember(ctx, id)
}

func (s *Services) GetMemberByName(ctx context.Context, workspaceID, name string) (*proto.Member, error) {
	return s.store.GetMemberByName(ctx, workspaceID, name)
}

func (s *Services) ListMembers(ctx context.Context, workspaceID string) ([]*proto.Member, error) {
	return s.store.ListMembers(ctx, workspaceID)
}

func (s *Services) ListMembersPaginated(ctx context.Context, workspaceID string, limit, offset int) ([]*proto.Member, error) {
	return s.store.ListMembersPaginated(ctx, workspaceID, limit, offset)
}

func (s *Services) UpdateMember(ctx context.Context, m *proto.Member) error {
	return s.store.UpdateMember(ctx, m)
}

func (s *Services) DeleteMember(ctx context.Context, id string) error {
	return s.store.DeleteMember(ctx, id)
}

// --- Channels ---

func (s *Services) CreateChannel(ctx context.Context, ch *proto.Channel) error {
	return s.store.CreateChannel(ctx, ch)
}

func (s *Services) GetChannel(ctx context.Context, id string) (*proto.Channel, error) {
	return s.store.GetChannel(ctx, id)
}

func (s *Services) ListChannels(ctx context.Context, workspaceID string) ([]*proto.Channel, error) {
	return s.store.ListChannels(ctx, workspaceID)
}

func (s *Services) ListChannelsPaginated(ctx context.Context, workspaceID string, limit, offset int) ([]*proto.Channel, error) {
	return s.store.ListChannelsPaginated(ctx, workspaceID, limit, offset)
}

func (s *Services) UpdateChannel(ctx context.Context, ch *proto.Channel) error {
	return s.store.UpdateChannel(ctx, ch)
}

func (s *Services) DeleteChannel(ctx context.Context, id string) error {
	return s.store.DeleteChannel(ctx, id)
}

func (s *Services) AddChannelMember(ctx context.Context, channelID, memberID string) error {
	return s.store.AddChannelMember(ctx, channelID, memberID)
}

func (s *Services) RemoveChannelMember(ctx context.Context, channelID, memberID string) error {
	return s.store.RemoveChannelMember(ctx, channelID, memberID)
}

func (s *Services) ListChannelMembers(ctx context.Context, channelID string) ([]*proto.Member, error) {
	return s.store.ListChannelMembers(ctx, channelID)
}

// --- Messages ---

func (s *Services) CreateMessage(ctx context.Context, m *proto.Message) error {
	return s.store.CreateMessage(ctx, m)
}

func (s *Services) GetMessage(ctx context.Context, id string) (*proto.Message, error) {
	return s.store.GetMessage(ctx, id)
}

func (s *Services) ListMessages(ctx context.Context, channelID string, limit, offset int) ([]*proto.Message, error) {
	return s.store.ListMessages(ctx, channelID, limit, offset)
}

func (s *Services) ListThreadMessages(ctx context.Context, threadID string) ([]*proto.Message, error) {
	return s.store.ListThreadMessages(ctx, threadID)
}

func (s *Services) UpdateMessage(ctx context.Context, m *proto.Message) error {
	return s.store.UpdateMessage(ctx, m)
}

func (s *Services) DeleteMessage(ctx context.Context, id string) error {
	return s.store.DeleteMessage(ctx, id)
}

func (s *Services) GetRecentMessages(ctx context.Context, channelID string, limit int) ([]*proto.Message, error) {
	return s.store.GetRecentMessages(ctx, channelID, limit)
}

// --- Reactions ---

func (s *Services) AddReaction(ctx context.Context, r *proto.Reaction) error {
	return s.store.AddReaction(ctx, r)
}

func (s *Services) RemoveReaction(ctx context.Context, messageID, memberID, emoji string) error {
	return s.store.RemoveReaction(ctx, messageID, memberID, emoji)
}

func (s *Services) ListReactions(ctx context.Context, messageID string) ([]*proto.Reaction, error) {
	return s.store.ListReactions(ctx, messageID)
}

// --- Search ---

func (s *Services) SearchMessages(ctx context.Context, query, channelID string, limit int) ([]*proto.Message, error) {
	return s.store.SearchMessages(ctx, query, channelID, limit)
}

// --- Tasks ---

func (s *Services) CreateTask(ctx context.Context, t *proto.Task) error {
	return s.store.CreateTask(ctx, t)
}

func (s *Services) GetTask(ctx context.Context, id string) (*proto.Task, error) {
	return s.store.GetTask(ctx, id)
}

func (s *Services) ListTasks(ctx context.Context, workspaceID string, status proto.TaskStatus) ([]*proto.Task, error) {
	return s.store.ListTasks(ctx, workspaceID, status)
}

func (s *Services) UpdateTask(ctx context.Context, t *proto.Task) error {
	return s.store.UpdateTask(ctx, t)
}

func (s *Services) DeleteTask(ctx context.Context, id string) error {
	return s.store.DeleteTask(ctx, id)
}

// --- Agent Memory ---

func (s *Services) SetMemory(ctx context.Context, m *proto.AgentMemory) error {
	return s.store.SetMemory(ctx, m)
}

func (s *Services) GetMemory(ctx context.Context, agentID, namespace, key string) (*proto.AgentMemory, error) {
	return s.store.GetMemory(ctx, agentID, namespace, key)
}

func (s *Services) ListMemory(ctx context.Context, agentID, namespace string) ([]*proto.AgentMemory, error) {
	return s.store.ListMemory(ctx, agentID, namespace)
}

func (s *Services) DeleteMemory(ctx context.Context, agentID, namespace, key string) error {
	return s.store.DeleteMemory(ctx, agentID, namespace, key)
}

// --- Files ---

func (s *Services) CreateFile(ctx context.Context, f *proto.File) error {
	return s.store.CreateFile(ctx, f)
}

func (s *Services) GetFile(ctx context.Context, id string) (*proto.File, error) {
	return s.store.GetFile(ctx, id)
}

func (s *Services) ListFiles(ctx context.Context, workspaceID string, limit, offset int) ([]*proto.File, error) {
	return s.store.ListFiles(ctx, workspaceID, limit, offset)
}

func (s *Services) DeleteFile(ctx context.Context, id string) error {
	return s.store.DeleteFile(ctx, id)
}

// --- Pins ---

func (s *Services) CreatePin(ctx context.Context, p *proto.Pin) error {
	return s.store.CreatePin(ctx, p)
}

func (s *Services) DeletePin(ctx context.Context, messageID string) error {
	return s.store.DeletePin(ctx, messageID)
}

func (s *Services) ListPins(ctx context.Context, channelID string) ([]*proto.Pin, error) {
	return s.store.ListPins(ctx, channelID)
}

// --- DMs ---

func (s *Services) GetDMChannel(ctx context.Context, workspaceID string, memberIDs []string) (*proto.Channel, error) {
	return s.store.GetDMChannel(ctx, workspaceID, memberIDs)
}

func (s *Services) ListDMChannels(ctx context.Context, memberID string) ([]*proto.Channel, error) {
	return s.store.ListDMChannels(ctx, memberID)
}

func (s *Services) CreateDMChannel(ctx context.Context, ch *proto.Channel, memberIDs []string) error {
	return s.store.CreateDMChannel(ctx, ch, memberIDs)
}

// --- Unread ---

func (s *Services) GetUnreadCounts(ctx context.Context, memberID string) (map[string]int, error) {
	return s.store.GetUnreadCounts(ctx, memberID)
}

func (s *Services) MarkChannelRead(ctx context.Context, channelID, memberID string) error {
	return s.store.UpdateLastRead(ctx, channelID, memberID)
}

// --- Approvals ---

func (s *Services) CreateApproval(ctx context.Context, a *proto.ApprovalRequest) error {
	return s.store.CreateApproval(ctx, a)
}

func (s *Services) GetApproval(ctx context.Context, id string) (*proto.ApprovalRequest, error) {
	return s.store.GetApproval(ctx, id)
}

func (s *Services) ListApprovals(ctx context.Context, workspaceID string, status proto.ApprovalStatus) ([]*proto.ApprovalRequest, error) {
	return s.store.ListApprovals(ctx, workspaceID, status)
}

func (s *Services) UpdateApproval(ctx context.Context, a *proto.ApprovalRequest) error {
	return s.store.UpdateApproval(ctx, a)
}
