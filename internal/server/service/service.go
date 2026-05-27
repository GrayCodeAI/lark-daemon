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

func (s *Services) SearchMessages(ctx context.Context, query, channelID, workspaceID string, limit int) ([]*proto.Message, error) {
	return s.store.SearchMessages(ctx, query, channelID, workspaceID, limit)
}

func (s *Services) SearchChannels(ctx context.Context, workspaceID, query string, limit int) ([]*proto.Channel, error) {
	return s.store.SearchChannels(ctx, workspaceID, query, limit)
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

func (s *Services) ListPinsByMessage(ctx context.Context, messageID string) ([]*proto.Pin, error) {
	return s.store.ListPinsByMessage(ctx, messageID)
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

// --- Webhooks ---

func (s *Services) CreateWebhook(ctx context.Context, w *proto.Webhook) error {
	w.Secret = websocket.GenerateAPIKey("wh_")
	return s.store.CreateWebhook(ctx, w)
}

func (s *Services) GetWebhook(ctx context.Context, id string) (*proto.Webhook, error) {
	return s.store.GetWebhook(ctx, id)
}

func (s *Services) ListWebhooks(ctx context.Context, workspaceID string) ([]*proto.Webhook, error) {
	return s.store.ListWebhooks(ctx, workspaceID)
}

func (s *Services) DeleteWebhook(ctx context.Context, id string) error {
	return s.store.DeleteWebhook(ctx, id)
}

// --- Edit History ---

func (s *Services) CreateEditHistory(ctx context.Context, h *proto.EditHistory) error {
	return s.store.CreateEditHistory(ctx, h)
}

func (s *Services) ListEditHistory(ctx context.Context, messageID string) ([]*proto.EditHistory, error) {
	return s.store.ListEditHistory(ctx, messageID)
}

// --- OAuth Identities ---

func (s *Services) CreateOAuthIdentity(ctx context.Context, o *proto.OAuthIdentity) error {
	return s.store.CreateOAuthIdentity(ctx, o)
}

func (s *Services) GetOAuthIdentity(ctx context.Context, provider, providerUserID string) (*proto.OAuthIdentity, error) {
	return s.store.GetOAuthIdentity(ctx, provider, providerUserID)
}

func (s *Services) GetOAuthIdentityByMember(ctx context.Context, memberID, provider string) (*proto.OAuthIdentity, error) {
	return s.store.GetOAuthIdentityByMember(ctx, memberID, provider)
}

// --- Notifications ---

func (s *Services) CreateNotification(ctx context.Context, n *proto.Notification) error {
	return s.store.CreateNotification(ctx, n)
}

func (s *Services) ListNotifications(ctx context.Context, memberID string, unreadOnly bool, limit int) ([]*proto.Notification, error) {
	return s.store.ListNotifications(ctx, memberID, unreadOnly, limit)
}

func (s *Services) MarkNotificationRead(ctx context.Context, id string) error {
	return s.store.MarkNotificationRead(ctx, id)
}

func (s *Services) MarkAllNotificationsRead(ctx context.Context, memberID string) error {
	return s.store.MarkAllNotificationsRead(ctx, memberID)
}

func (s *Services) CountUnreadNotifications(ctx context.Context, memberID string) (int, error) {
	return s.store.CountUnreadNotifications(ctx, memberID)
}

// --- Integrations ---

func (s *Services) CreateIntegration(ctx context.Context, i *proto.Integration) error {
	return s.store.CreateIntegration(ctx, i)
}

func (s *Services) GetIntegration(ctx context.Context, id string) (*proto.Integration, error) {
	return s.store.GetIntegration(ctx, id)
}

func (s *Services) ListIntegrations(ctx context.Context) ([]*proto.Integration, error) {
	return s.store.ListIntegrations(ctx)
}

func (s *Services) InstallIntegration(ctx context.Context, wi *proto.WorkspaceIntegration) error {
	return s.store.InstallIntegration(ctx, wi)
}

func (s *Services) UninstallIntegration(ctx context.Context, workspaceID, integrationID string) error {
	return s.store.UninstallIntegration(ctx, workspaceID, integrationID)
}

func (s *Services) ListWorkspaceIntegrations(ctx context.Context, workspaceID string) ([]*proto.WorkspaceIntegration, error) {
	return s.store.ListWorkspaceIntegrations(ctx, workspaceID)
}

func (s *Services) GetWorkspaceIntegration(ctx context.Context, workspaceID, integrationID string) (*proto.WorkspaceIntegration, error) {
	return s.store.GetWorkspaceIntegration(ctx, workspaceID, integrationID)
}

// --- SSO Providers ---

func (s *Services) CreateSSOProvider(ctx context.Context, p *proto.SSOProvider) error {
	return s.store.CreateSSOProvider(ctx, p)
}

func (s *Services) GetSSOProvider(ctx context.Context, id string) (*proto.SSOProvider, error) {
	return s.store.GetSSOProvider(ctx, id)
}

func (s *Services) ListSSOProviders(ctx context.Context, workspaceID string) ([]*proto.SSOProvider, error) {
	return s.store.ListSSOProviders(ctx, workspaceID)
}

func (s *Services) DeleteSSOProvider(ctx context.Context, id string) error {
	return s.store.DeleteSSOProvider(ctx, id)
}

func (s *Services) GetSSOProviderByDomain(ctx context.Context, domain string) (*proto.SSOProvider, error) {
	return s.store.GetSSOProviderByDomain(ctx, domain)
}

// --- Calls ---

func (s *Services) CreateCall(ctx context.Context, c *proto.Call) error {
	return s.store.CreateCall(ctx, c)
}

func (s *Services) GetCall(ctx context.Context, id string) (*proto.Call, error) {
	return s.store.GetCall(ctx, id)
}

func (s *Services) UpdateCall(ctx context.Context, c *proto.Call) error {
	return s.store.UpdateCall(ctx, c)
}

func (s *Services) ListCalls(ctx context.Context, memberID string, limit int) ([]*proto.Call, error) {
	return s.store.ListCalls(ctx, memberID, limit)
}

// --- Workflows ---

func (s *Services) CreateWorkflow(ctx context.Context, w *proto.Workflow) error {
	return s.store.CreateWorkflow(ctx, w)
}

func (s *Services) GetWorkflow(ctx context.Context, id string) (*proto.Workflow, error) {
	return s.store.GetWorkflow(ctx, id)
}

func (s *Services) ListWorkflows(ctx context.Context, workspaceID string) ([]*proto.Workflow, error) {
	return s.store.ListWorkflows(ctx, workspaceID)
}

func (s *Services) UpdateWorkflow(ctx context.Context, w *proto.Workflow) error {
	return s.store.UpdateWorkflow(ctx, w)
}

func (s *Services) DeleteWorkflow(ctx context.Context, id string) error {
	return s.store.DeleteWorkflow(ctx, id)
}

func (s *Services) CreateWorkflowRun(ctx context.Context, r *proto.WorkflowRun) error {
	return s.store.CreateWorkflowRun(ctx, r)
}

func (s *Services) GetWorkflowRun(ctx context.Context, id string) (*proto.WorkflowRun, error) {
	return s.store.GetWorkflowRun(ctx, id)
}

func (s *Services) UpdateWorkflowRun(ctx context.Context, r *proto.WorkflowRun) error {
	return s.store.UpdateWorkflowRun(ctx, r)
}

func (s *Services) ListWorkflowRuns(ctx context.Context, workflowID string, limit int) ([]*proto.WorkflowRun, error) {
	return s.store.ListWorkflowRuns(ctx, workflowID, limit)
}

// --- E2EE Key Management ---

func (s *Services) RegisterUserKey(ctx context.Context, k *proto.UserKey) error {
	return s.store.RegisterUserKey(ctx, k)
}

func (s *Services) GetUserKeys(ctx context.Context, memberID string, keyType proto.UserKeyType) ([]*proto.UserKey, error) {
	return s.store.GetUserKeys(ctx, memberID, keyType)
}

func (s *Services) GetUserKey(ctx context.Context, id string) (*proto.UserKey, error) {
	return s.store.GetUserKey(ctx, id)
}

func (s *Services) DeleteUserKey(ctx context.Context, id string) error {
	return s.store.DeleteUserKey(ctx, id)
}

func (s *Services) DeleteUserKeysByMember(ctx context.Context, memberID string) error {
	return s.store.DeleteUserKeysByMember(ctx, memberID)
}

// --- E2EE Encrypted Messages ---

func (s *Services) CreateEncryptedMessage(ctx context.Context, m *proto.EncryptedMessage) error {
	return s.store.CreateEncryptedMessage(ctx, m)
}

func (s *Services) GetEncryptedMessages(ctx context.Context, messageID string, recipientID string) ([]*proto.EncryptedMessage, error) {
	return s.store.GetEncryptedMessages(ctx, messageID, recipientID)
}

func (s *Services) GetEncryptedMessageForRecipient(ctx context.Context, messageID, recipientID string) (*proto.EncryptedMessage, error) {
	return s.store.GetEncryptedMessageForRecipient(ctx, messageID, recipientID)
}

// --- Billing ---

func (s *Services) CreateBillingCustomer(ctx context.Context, c *proto.BillingCustomer) error {
	return s.store.CreateBillingCustomer(ctx, c)
}

func (s *Services) GetBillingCustomer(ctx context.Context, workspaceID string) (*proto.BillingCustomer, error) {
	return s.store.GetBillingCustomer(ctx, workspaceID)
}

func (s *Services) GetBillingCustomerByStripeID(ctx context.Context, stripeCustomerID string) (*proto.BillingCustomer, error) {
	return s.store.GetBillingCustomerByStripeID(ctx, stripeCustomerID)
}

func (s *Services) UpdateBillingCustomer(ctx context.Context, c *proto.BillingCustomer) error {
	return s.store.UpdateBillingCustomer(ctx, c)
}

func (s *Services) DeleteBillingCustomer(ctx context.Context, workspaceID string) error {
	return s.store.DeleteBillingCustomer(ctx, workspaceID)
}

// --- Usage Tracking ---

func (s *Services) CreateUsageRecord(ctx context.Context, r *proto.UsageRecord) error {
	return s.store.CreateUsageRecord(ctx, r)
}

func (s *Services) GetUsageRecord(ctx context.Context, workspaceID, metric string, periodStart int64) (*proto.UsageRecord, error) {
	return s.store.GetUsageRecord(ctx, workspaceID, metric, periodStart)
}

func (s *Services) IncrementUsage(ctx context.Context, workspaceID, metric string, periodStart, periodEnd int64, delta int) error {
	return s.store.IncrementUsage(ctx, workspaceID, metric, periodStart, periodEnd, delta)
}

func (s *Services) ListUsageRecords(ctx context.Context, workspaceID string) ([]*proto.UsageRecord, error) {
	return s.store.ListUsageRecords(ctx, workspaceID)
}
