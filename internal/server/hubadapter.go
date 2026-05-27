package server

import (
	"context"

	"lark-daemon/internal/proto"
	"lark-daemon/internal/server/store"
	"lark-daemon/internal/server/websocket"
)

// HubStoreAdapter adapts the store.Store to websocket.StoreQuerier and websocket.AgentStore.
type HubStoreAdapter struct {
	store store.Store
}

// NewHubStoreAdapter creates a new HubStoreAdapter.
func NewHubStoreAdapter(s store.Store) *HubStoreAdapter {
	return &HubStoreAdapter{store: s}
}

// GetMemberByName implements websocket.StoreQuerier.
func (a *HubStoreAdapter) GetMemberByName(workspaceID, name string) (*websocket.MemberBrief, error) {
	m, err := a.store.GetMemberByName(context.Background(), workspaceID, name)
	if err != nil {
		return nil, err
	}
	if m == nil {
		return nil, nil
	}
	return &websocket.MemberBrief{
		ID:          m.ID,
		Name:        m.Name,
		IsAgent:     m.Type == "agent",
		WorkspaceID: m.WorkspaceID,
	}, nil
}

// GetRecentMessages implements websocket.StoreQuerier.
func (a *HubStoreAdapter) GetRecentMessages(channelID string, limit int) ([]websocket.MessageBrief, error) {
	msgs, err := a.store.GetRecentMessages(context.Background(), channelID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]websocket.MessageBrief, len(msgs))
	for i, m := range msgs {
		out[i] = websocket.MessageBrief{
			ID:        m.ID,
			SenderID:  m.SenderID,
			Content:   m.Content,
			CreatedAt: m.CreatedAt,
		}
	}
	return out, nil
}

// UpdateMemberRoleCard implements websocket.AgentStore.
func (a *HubStoreAdapter) UpdateMemberRoleCard(memberID string, roleCard *proto.RoleCard, runtime *proto.RuntimeInfo) error {
	return a.store.UpdateMemberRoleCard(context.Background(), memberID, roleCard, runtime)
}
