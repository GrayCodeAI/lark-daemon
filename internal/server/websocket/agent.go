package websocket

import (
	"log/slog"
	"strings"

	"lark/internal/proto"
)

// AgentStore is the interface the AgentManager needs to update agent data.
type AgentStore interface {
	UpdateMemberRoleCard(memberID string, roleCard *proto.RoleCard, runtime *proto.RuntimeInfo) error
}

// AgentManager handles agent lifecycle (sleep/wake protocol).
type AgentManager struct {
	hub    *Hub
	store  AgentStore
	logger *slog.Logger
}

// NewAgentManager creates a new AgentManager.
func NewAgentManager(hub *Hub, store AgentStore, logger *slog.Logger) *AgentManager {
	return &AgentManager{
		hub:    hub,
		store:  store,
		logger: logger,
	}
}

// HandleAgentHello processes an agent.hello event.
// If the connection is already authenticated (via auth.login with API key),
// it reuses the existing identity and updates role card/runtime info.
// Otherwise it creates a new ephemeral identity.
func (am *AgentManager) HandleAgentHello(c *Conn, data *AgentHelloData) {
	if c.IsAuthenticated() {
		// Agent already authenticated via auth.login — update role card
		if am.store != nil {
			roleCard := &proto.RoleCard{
				SystemPrompt: data.RoleCard.SystemPrompt,
				Capabilities: data.RoleCard.Capabilities,
			}
			runtime := &proto.RuntimeInfo{
				Type:     data.Runtime.Type,
				Provider: data.Runtime.Provider,
				Model:    data.Runtime.Model,
			}
			if err := am.store.UpdateMemberRoleCard(c.ID(), roleCard, runtime); err != nil {
				slog.Error("failed to update agent role card", "err", err, "agent_id", c.ID())
			}
		}

		// Send welcome with existing ID
		welcome := NewEnvelope(EventAgentWelcome, AgentWelcomeData{
			AgentID: c.ID(),
			Status:  string(proto.AgentAwake),
		})
		c.Send(welcome)

		// Ensure presence is online
		am.hub.SetPresence(c.ID(), string(proto.PresenceOnline))

		slog.Info("agent reconnected", "name", c.Name(), "id", c.ID())
		return
	}

	// Not authenticated — create new ephemeral identity
	memberID := proto.NewID()
	c.SetIdentity(memberID, data.Name, true)
	c.SetAuthenticated(true)
	am.hub.Add(c)

	// Send welcome
	welcome := NewEnvelope(EventAgentWelcome, AgentWelcomeData{
		AgentID: memberID,
		Status:  string(proto.AgentAwake),
	})
	c.Send(welcome)

	// Broadcast presence
	am.hub.SendPresenceUpdate(memberID, string(proto.PresenceOnline))

	slog.Info("agent connected (new)", "name", data.Name, "id", memberID)
}

// HandleAgentSleep processes an agent.sleep event.
func (am *AgentManager) HandleAgentSleep(c *Conn) {
	if !c.IsAgent() {
		return
	}

	am.hub.SetPresence(c.ID(), string(proto.AgentSleeping))

	slog.Info("agent sleeping", "id", c.ID(), "name", c.Name())
}

// HandleAgentThinking processes an agent.thinking event.
func (am *AgentManager) HandleAgentThinking(c *Conn, channelID string) {
	if !c.IsAgent() {
		return
	}

	// Broadcast typing indicator
	am.hub.SendTypingIndicator(channelID, c.ID())
}

// ParseMentions extracts @mentions from message content.
func ParseMentions(content string) []string {
	var mentions []string
	words := strings.Fields(content)
	for _, word := range words {
		if strings.HasPrefix(word, "@") {
			name := strings.TrimRight(word[1:], ",.!?;:")
			if name != "" {
				mentions = append(mentions, name)
			}
		}
	}
	return mentions
}
