package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"lark-daemon/internal/proto"
	"lark-daemon/internal/server/store"
	"lark-daemon/internal/server/websocket"
)

func (r *Router) handleWebSocket(w http.ResponseWriter, req *http.Request) {
	conn, err := r.hub.Upgrade(w, req)
	if err != nil {
		r.logger.Error("websocket upgrade failed", "err", err)
		return
	}
	wc := websocket.NewConn(r.hub, conn, "", "", false)
	go wc.WritePump()
	go wc.ReadPump(func(env websocket.Envelope) {
		r.handleWSEvent(wc, env)
	})
	// Close unauthenticated connections after 30 seconds
	go func() {
		time.Sleep(30 * time.Second)
		if !wc.IsAuthenticated() {
			wc.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "auth timeout"}))
			wc.Close()
		}
	}()
}

func (r *Router) handleWSEvent(c *websocket.Conn, env websocket.Envelope) {
	switch env.Type {
	case websocket.EventAuthLogin:
		r.handleWSAuthLogin(c, env)

	case websocket.EventAgentHello:
		data, err := websocket.ParseAgentHello(env.Data)
		if err != nil {
			c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "invalid agent hello data"}))
			return
		}
		r.agentManager.HandleAgentHello(c, data)

	case websocket.EventDaemonRegister:
		r.handleWSDaemonRegister(c, env)

	case websocket.EventAgentSleep:
		r.agentManager.HandleAgentSleep(c)

	case websocket.EventAgentThinking:
		if !c.IsAuthenticated() || !c.IsAgent() {
			c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "not authorized"}))
			return
		}
		var td struct {
			ChannelID string `json:"channel_id"`
		}
		if err := json.Unmarshal(env.Data, &td); err != nil || td.ChannelID == "" {
			c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "invalid thinking data"}))
			return
		}
		r.agentManager.HandleAgentThinking(c, td.ChannelID)

	case websocket.EventChannelJoin:
		r.handleWSChannelJoin(c, env)

	case websocket.EventChannelLeave:
		r.handleWSChannelLeave(c, env)

	case websocket.EventMessageSend:
		r.handleWSMessageSend(c, env)

	case websocket.EventTypingStart:
		r.handleWSTypingStart(c, env)

	case websocket.EventTypingStop:
		r.handleWSTypingStop(c, env)

	case websocket.EventThreadReply:
		r.handleWSThreadReply(c, env)

	case websocket.EventMessageEdit:
		r.handleWSMessageEdit(c, env)

	case websocket.EventMessageDel:
		r.handleWSMessageDelete(c, env)

	case websocket.EventApprovalRequest:
		r.handleWSApprovalRequest(c, env)

	case websocket.EventCallOffer:
		r.handleWSCallOffer(c, env)

	case websocket.EventCallAnswer:
		r.handleWSCallAnswer(c, env)

	case websocket.EventCallICE:
		r.handleWSCallICE(c, env)

	case websocket.EventCallEnd:
		r.handleWSCallEnd(c, env)

	// Agent inbox
	case websocket.EventInboxPoll:
		r.handleWSInboxPoll(c, env)
	case websocket.EventInboxAck:
		r.handleWSInboxAck(c, env)

	// Held drafts
	case websocket.EventDraftCreate:
		r.handleWSDraftCreate(c, env)
	case websocket.EventDraftValidate:
		r.handleWSDraftValidate(c, env)
	case websocket.EventDraftSend:
		r.handleWSDraftSend(c, env)

	// Reviews
	case websocket.EventReviewRequest:
		r.handleWSReviewRequest(c, env)
	case websocket.EventReviewResult:
		r.handleWSReviewResult(c, env)

	// Agent workspace
	case websocket.EventWorkspaceUpdate:
		r.handleWSWorkspaceUpdate(c, env)

	default:
		r.logger.Warn("unknown ws event", "type", env.Type)
	}
}

func (r *Router) handleWSAuthLogin(c *websocket.Conn, env websocket.Envelope) {
	data, err := websocket.ParseAuthLogin(env.Data)
	if err != nil {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "invalid auth data"}))
		return
	}

	// Validate JWT
	claims, err := r.auth.ValidateToken(data.Token, &tokenBlacklistAdapter{store: r.store})
	if err != nil {
		// Try as API key
		ctx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
		defer cancel()
		member, err := r.store.GetMemberByAPIKey(ctx, data.Token)
		if err != nil {
			c.Send(websocket.NewEnvelope(websocket.EventAuthFail, map[string]string{"error": "auth error"}))
			return
		}
		if member == nil {
			c.Send(websocket.NewEnvelope(websocket.EventAuthFail, map[string]string{"error": "invalid credentials"}))
			return
		}
		c.SetIdentity(member.ID, member.Name, member.Type == proto.MemberAgent)
		c.SetAuthenticated(true)
		r.hub.Add(c)

		// Auto-subscribe to all channels the member belongs to
		r.subscribeMemberChannels(c, member.ID)

		c.Send(websocket.NewEnvelope(websocket.EventAuthSuccess, map[string]any{
			"member_id":    member.ID,
			"workspace_id": member.WorkspaceID,
			"name":         member.Name,
			"type":         member.Type,
		}))
		r.hub.SendPresenceUpdate(member.ID, string(proto.PresenceOnline))
		return
	}

	// JWT auth
	ctx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
	defer cancel()
	member, err := r.store.GetMember(ctx, claims.MemberID)
	if err != nil {
		c.Send(websocket.NewEnvelope(websocket.EventAuthFail, map[string]string{"error": "auth error"}))
		return
	}
	if member == nil {
		c.Send(websocket.NewEnvelope(websocket.EventAuthFail, map[string]string{"error": "member not found"}))
		return
	}
	c.SetIdentity(member.ID, member.Name, member.Type == proto.MemberAgent)
	c.SetAuthenticated(true)
	r.hub.Add(c)

	// Auto-subscribe to all channels the member belongs to
	r.subscribeMemberChannels(c, member.ID)

	c.Send(websocket.NewEnvelope(websocket.EventAuthSuccess, map[string]any{
		"member_id":    member.ID,
		"workspace_id": member.WorkspaceID,
		"name":         member.Name,
		"type":         member.Type,
	}))
	r.hub.SendPresenceUpdate(member.ID, string(proto.PresenceOnline))
}

func (r *Router) subscribeMemberChannels(c *websocket.Conn, memberID string) {
	channelIDs, err := r.store.ListMemberChannelIDs(c.Context(), memberID)
	if err != nil {
		return
	}
	for _, id := range channelIDs {
		if c.Subscribe(id) {
			r.hub.SubscribeChannel(id, c)
		}
	}
}

func (r *Router) handleWSChannelJoin(c *websocket.Conn, env websocket.Envelope) {
	if !c.IsAuthenticated() {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "not authenticated"}))
		return
	}
	var data struct {
		ChannelID string `json:"channel_id"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "invalid data"}))
		return
	}
	// Verify membership
	isMember, err := r.store.IsChannelMember(c.Context(), data.ChannelID, c.ID())
	if err != nil || !isMember {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "not a channel member"}))
		return
	}
	if !c.Subscribe(data.ChannelID) {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "subscription limit reached"}))
		return
	}
	r.hub.SubscribeChannel(data.ChannelID, c)
	c.Send(websocket.NewEnvelope(websocket.EventChannelJoin, map[string]string{"channel_id": data.ChannelID}))
}

func (r *Router) handleWSChannelLeave(c *websocket.Conn, env websocket.Envelope) {
	if !c.IsAuthenticated() {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "not authenticated"}))
		return
	}
	var data struct {
		ChannelID string `json:"channel_id"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "invalid data"}))
		return
	}
	c.Unsubscribe(data.ChannelID)
	r.hub.UnsubscribeChannel(data.ChannelID, c.ID())
	c.Send(websocket.NewEnvelope(websocket.EventChannelLeave, map[string]string{"channel_id": data.ChannelID}))
}

func (r *Router) handleWSMessageSend(c *websocket.Conn, env websocket.Envelope) {
	if !c.IsAuthenticated() {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "not authenticated"}))
		return
	}
	data, err := websocket.ParseMessageSend(env.Data)
	if err != nil {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "invalid message data"}))
		return
	}
	if strings.TrimSpace(data.Content) == "" {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "content is required"}))
		return
	}
	if len(data.Content) > 10000 {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "content too long (max 10000 characters)"}))
		return
	}
	// Verify sender is subscribed to the channel
	if !c.IsSubscribed(data.ChannelID) {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "not subscribed to channel"}))
		return
	}
	msg := &proto.Message{
		ChannelID:   data.ChannelID,
		SenderID:    c.ID(),
		Content:     data.Content,
		ThreadID:    data.ThreadID,
		ContentType: data.ContentType,
		Type:        data.Type,
	}
	if msg.ContentType == "" {
		msg.ContentType = "text"
	}
	if err := r.services.CreateMessage(c.Context(), msg); err != nil {
		wsError(c, err, "ws create message failed")
		return
	}
	r.hub.SendNewMessage(msg.ChannelID, msg)
	c.Send(websocket.NewEnvelope(websocket.EventMessageAck, map[string]string{"message_id": msg.ID}))
	r.handleMentions(msg)
}

// handleMentions checks for @mentions and wakes agents by name with context.
// Also creates notification records for mentioned users.
func (r *Router) handleMentions(msg *proto.Message) {
	mentions := websocket.ParseMentions(msg.Content)
	if len(mentions) == 0 {
		return
	}
	// Look up channel to get workspace_id for member lookup
	ch, _ := r.services.GetChannel(context.Background(), msg.ChannelID)
	for _, name := range mentions {
		r.hub.WakeAgentByName(name, msg.ChannelID, "mention")
		if ch == nil {
			continue
		}
		member, err := r.store.GetMemberByName(context.Background(), ch.WorkspaceID, name)
		if err != nil || member == nil {
			continue
		}
		if member.ID == msg.SenderID {
			continue
		}
		n := &proto.Notification{
			MemberID:    member.ID,
			Type:        "mention",
			Title:       "You were mentioned",
			Body:        msg.Content,
			ChannelID:   msg.ChannelID,
			MessageID:   msg.ID,
			SourceType:  "mention",
			Priority:    "normal",
			AckRequired: false,
		}
		_ = r.services.CreateNotification(context.Background(), n)
		if c := r.hub.GetConn(member.ID); c != nil {
			c.Send(websocket.NewEnvelope(websocket.EventNotificationNew, n))
		}
	}
}

// handleWSMessageEdit processes a message.edit event.
func (r *Router) handleWSMessageEdit(c *websocket.Conn, env websocket.Envelope) {
	if !c.IsAuthenticated() {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "not authenticated"}))
		return
	}
	var data struct {
		MessageID string `json:"message_id"`
		Content   string `json:"content"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "invalid data"}))
		return
	}
	if strings.TrimSpace(data.Content) == "" {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "content is required"}))
		return
	}
	if len(data.Content) > 10000 {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "content too long (max 10000 characters)"}))
		return
	}
	msg, err := r.services.GetMessage(c.Context(), data.MessageID)
	if err != nil {
		wsError(c, err, "ws message edit: get message failed")
		return
	}
	if msg == nil {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "message not found"}))
		return
	}
	if msg.SenderID != c.ID() {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "not message author"}))
		return
	}
	msg.Content = data.Content
	r.trackMessageEdit(msg)
	if err := r.services.UpdateMessage(c.Context(), msg); err != nil {
		wsError(c, err, "ws update message failed")
		return
	}
	r.hub.BroadcastToChannel(msg.ChannelID, websocket.NewEnvelope(websocket.EventMessageEdit, msg))
}

// handleWSMessageDelete processes a message.delete event.
func (r *Router) handleWSMessageDelete(c *websocket.Conn, env websocket.Envelope) {
	if !c.IsAuthenticated() {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "not authenticated"}))
		return
	}
	var data struct {
		MessageID string `json:"message_id"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "invalid data"}))
		return
	}
	msg, err := r.services.GetMessage(c.Context(), data.MessageID)
	if err != nil {
		wsError(c, err, "ws message delete: get message failed")
		return
	}
	if msg == nil {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "message not found"}))
		return
	}
	if msg.SenderID != c.ID() {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "not message author"}))
		return
	}
	if err := r.services.DeleteMessage(c.Context(), data.MessageID); err != nil {
		wsError(c, err, "ws delete message failed")
		return
	}
	r.hub.BroadcastToChannel(msg.ChannelID, websocket.NewEnvelope(websocket.EventMessageDel, map[string]string{"id": data.MessageID}))
}

func (r *Router) handleWSApprovalRequest(c *websocket.Conn, env websocket.Envelope) {
	if !c.IsAuthenticated() || !c.IsAgent() {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "not authenticated agent"}))
		return
	}
	var data struct {
		ChannelID string `json:"channel_id,omitempty"`
		Action    string `json:"action"`
		Payload   string `json:"payload,omitempty"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "invalid data"}))
		return
	}
	a := &proto.ApprovalRequest{
		AgentID:   c.ID(),
		ChannelID: data.ChannelID,
		Action:    data.Action,
		Payload:   data.Payload,
	}
	// We need workspace_id — get from agent's member record
	member, err := r.services.GetMember(c.Context(), c.ID())
	if err != nil {
		wsError(c, err, "ws approval: get member failed")
		return
	}
	if member == nil {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "agent not found"}))
		return
	}
	a.WorkspaceID = member.WorkspaceID
	if err := r.services.CreateApproval(c.Context(), a); err != nil {
		wsError(c, err, "ws create approval failed")
		return
	}
	// Notify humans that approval is pending
	r.hub.BroadcastToAll(websocket.NewEnvelope(websocket.EventApprovalRequest, a))
	c.Send(websocket.NewEnvelope(websocket.EventApprovalRequest, map[string]string{"id": a.ID, "status": "pending"}))
}

// --- Agent metrics ---


func (r *Router) handleWSTypingStart(c *websocket.Conn, env websocket.Envelope) {
	if !c.IsAuthenticated() {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "not authenticated"}))
		return
	}
	var data struct {
		ChannelID string `json:"channel_id"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil || data.ChannelID == "" {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "invalid data"}))
		return
	}
	isMember, err := r.store.IsChannelMember(c.Context(), data.ChannelID, c.ID())
	if err != nil {
		r.logger.Error("typing start: check membership", "err", err)
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "internal error"}))
		return
	}
	if !isMember {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "not a channel member"}))
		return
	}
	r.hub.BroadcastToChannel(data.ChannelID, websocket.NewEnvelope(websocket.EventTypingStart, map[string]string{
		"channel_id": data.ChannelID,
		"member_id":  c.ID(),
		"name":       c.Name(),
	}))
}

func (r *Router) handleWSTypingStop(c *websocket.Conn, env websocket.Envelope) {
	if !c.IsAuthenticated() {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "not authenticated"}))
		return
	}
	var data struct {
		ChannelID string `json:"channel_id"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil || data.ChannelID == "" {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "invalid data"}))
		return
	}
	isMember, err := r.store.IsChannelMember(c.Context(), data.ChannelID, c.ID())
	if err != nil {
		r.logger.Error("typing stop: check membership", "err", err)
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "internal error"}))
		return
	}
	if !isMember {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "not a channel member"}))
		return
	}
	r.hub.BroadcastToChannel(data.ChannelID, websocket.NewEnvelope(websocket.EventTypingStop, map[string]string{
		"channel_id": data.ChannelID,
		"member_id":  c.ID(),
	}))
}

func (r *Router) handleWSThreadReply(c *websocket.Conn, env websocket.Envelope) {
	if !c.IsAuthenticated() {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "not authenticated"}))
		return
	}
	var data struct {
		ChannelID string `json:"channel_id"`
		ParentID  string `json:"parent_id"`
		Content   string `json:"content"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "invalid data"}))
		return
	}
	if data.ParentID == "" || data.Content == "" {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "parent_id and content required"}))
		return
	}
	if len(data.Content) > 10000 {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "content too long (max 10000 characters)"}))
		return
	}
	isMember, err := r.store.IsChannelMember(c.Context(), data.ChannelID, c.ID())
	if err != nil {
		r.logger.Error("thread reply: check membership", "err", err)
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "internal error"}))
		return
	}
	if !isMember {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "not a channel member"}))
		return
	}
	msg := &proto.Message{
		ChannelID: data.ChannelID,
		SenderID:  c.ID(),
		Content:   data.Content,
		ThreadID:  data.ParentID,
	}
	if err := r.services.CreateMessage(c.Context(), msg); err != nil {
		wsError(c, err, "ws create thread reply failed")
		return
	}
	r.hub.BroadcastToChannel(data.ChannelID, websocket.NewEnvelope(websocket.EventMessageNew, msg))
	c.Send(websocket.NewEnvelope(websocket.EventMessageAck, map[string]string{"id": msg.ID}))

	// Check for @mentions in thread replies too
	r.handleMentions(msg)
}

// --- File handlers ---


// --- Real-time WS event helpers ---

// --- Real-time WS event helpers ---

func (r *Router) broadcastReactionEvent(msgID string, reaction *proto.Reaction) {
	// Get the message to find its channel
	msg, err := r.services.GetMessage(context.Background(), msgID)
	if err != nil || msg == nil {
		return
	}
	r.hub.BroadcastToChannel(msg.ChannelID, websocket.NewEnvelope("reaction.add", reaction))
}

func (r *Router) broadcastPinEvent(channelID string, pin *proto.Pin) {
	r.hub.BroadcastToChannel(channelID, websocket.NewEnvelope("pin.add", pin))
}

func (r *Router) broadcastTaskEvent(task *proto.Task) {
	r.hub.BroadcastToAll(websocket.NewEnvelope("task.update", task))
}

func (r *Router) notifyAssignee(task *proto.Task) {
	if task.AssignedTo == "" {
		return
	}
	if c := r.hub.GetConn(task.AssignedTo); c != nil {
		c.Send(websocket.NewEnvelope("task.assigned", map[string]any{
			"task":    task,
			"message": "You have been assigned a task: " + task.Title,
		}))
	}
	// If assignee is an agent, wake them
	r.hub.WakeAgent(task.AssignedTo, websocket.AgentWakeData{
		Reason: "task_assigned",
		Context: websocket.WakeContext{
			Channel: map[string]string{"id": task.ChannelID},
		},
	})
}

// --- WebRTC Call Signaling ---

func (r *Router) handleWSCallOffer(c *websocket.Conn, env websocket.Envelope) {
	if !c.IsAuthenticated() {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "not authenticated"}))
		return
	}
	var data struct {
		CalleeID string `json:"callee_id"`
		CallType string `json:"type"`
		SDP      string `json:"sdp"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil || data.CalleeID == "" || data.SDP == "" {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "callee_id and sdp required"}))
		return
	}
	if data.CallType == "" {
		data.CallType = "audio"
	}
	member, _ := r.services.GetMember(c.Context(), c.ID())
	if member == nil {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "member not found"}))
		return
	}
	call := &proto.Call{
		WorkspaceID: member.WorkspaceID,
		CallerID:    c.ID(),
		CalleeID:    data.CalleeID,
		Type:        proto.CallType(data.CallType),
		Status:      proto.CallRinging,
	}
	if err := r.services.CreateCall(c.Context(), call); err != nil {
		wsError(c, err, "create call failed")
		return
	}
	// Ring the callee
	if calleeConn := r.hub.GetConn(data.CalleeID); calleeConn != nil {
		calleeConn.Send(websocket.NewEnvelope(websocket.EventCallRing, map[string]any{
			"call_id":   call.ID,
			"caller_id": c.ID(),
			"caller_name": member.Name,
			"type":      data.CallType,
		}))
		// Forward SDP offer
		calleeConn.Send(websocket.NewEnvelope(websocket.EventCallOffer, map[string]any{
			"call_id":   call.ID,
			"caller_id": c.ID(),
			"type":      data.CallType,
			"sdp":       data.SDP,
		}))
	}
	c.Send(websocket.NewEnvelope(websocket.EventCallOffer, map[string]any{
		"call_id":  call.ID,
		"status":   "ringing",
	}))
}

func (r *Router) handleWSCallAnswer(c *websocket.Conn, env websocket.Envelope) {
	if !c.IsAuthenticated() {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "not authenticated"}))
		return
	}
	var data struct {
		CallID string `json:"call_id"`
		SDP    string `json:"sdp"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil || data.CallID == "" || data.SDP == "" {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "call_id and sdp required"}))
		return
	}
	call, err := r.services.GetCall(c.Context(), data.CallID)
	if err != nil || call == nil {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "call not found"}))
		return
	}
	if call.CalleeID != c.ID() {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "not the callee"}))
		return
	}
	now := time.Now().UnixMilli()
	call.Status = proto.CallAnswered
	call.StartedAt = now
	_ = r.services.UpdateCall(c.Context(), call)
	// Forward answer to caller
	if callerConn := r.hub.GetConn(call.CallerID); callerConn != nil {
		callerConn.Send(websocket.NewEnvelope(websocket.EventCallAnswer, map[string]any{
			"call_id": call.ID,
			"sdp":     data.SDP,
		}))
	}
}

func (r *Router) handleWSCallICE(c *websocket.Conn, env websocket.Envelope) {
	if !c.IsAuthenticated() {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "not authenticated"}))
		return
	}
	var data struct {
		CallID    string `json:"call_id"`
		TargetID  string `json:"target_id"`
		Candidate string `json:"candidate"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil || data.CallID == "" || data.TargetID == "" {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "invalid data"}))
		return
	}
	// Forward ICE candidate to the other peer
	if targetConn := r.hub.GetConn(data.TargetID); targetConn != nil {
		targetConn.Send(websocket.NewEnvelope(websocket.EventCallICE, map[string]any{
			"call_id":   data.CallID,
			"from_id":   c.ID(),
			"candidate": data.Candidate,
		}))
	}
}

func (r *Router) handleWSCallEnd(c *websocket.Conn, env websocket.Envelope) {
	if !c.IsAuthenticated() {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "not authenticated"}))
		return
	}
	var data struct {
		CallID string `json:"call_id"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil || data.CallID == "" {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "call_id required"}))
		return
	}
	call, err := r.services.GetCall(c.Context(), data.CallID)
	if err != nil || call == nil {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "call not found"}))
		return
	}
	if call.CallerID != c.ID() && call.CalleeID != c.ID() {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "not a participant"}))
		return
	}
	call.Status = proto.CallEnded
	call.EndedAt = time.Now().UnixMilli()
	_ = r.services.UpdateCall(c.Context(), call)
	// Notify the other peer
	otherID := call.CallerID
	if c.ID() == call.CallerID {
		otherID = call.CalleeID
	}
	if otherConn := r.hub.GetConn(otherID); otherConn != nil {
		otherConn.Send(websocket.NewEnvelope(websocket.EventCallEnd, map[string]any{
			"call_id": call.ID,
			"reason":  "ended",
		}))
	}
}

// --- Agent Inbox WebSocket Handlers ---

func (r *Router) handleWSInboxPoll(c *websocket.Conn, env websocket.Envelope) {
	if !c.IsAuthenticated() || !c.IsAgent() {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "not authorized"}))
		return
	}
	var data websocket.InboxPollData
	if err := json.Unmarshal(env.Data, &data); err != nil {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "invalid inbox poll data"}))
		return
	}
	opts := store.InboxOptions{
		SourceType: data.SourceType,
		UnreadOnly: data.UnreadOnly,
		Since:      data.Since,
		Limit:      data.Limit,
	}
	if opts.Limit == 0 {
		opts.Limit = 50
	}
	items, err := r.store.ListAgentInbox(c.Context(), c.ID(), opts)
	if err != nil {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "failed to list inbox"}))
		return
	}
	c.Send(websocket.NewEnvelope(websocket.EventInboxItems, items))
}

func (r *Router) handleWSInboxAck(c *websocket.Conn, env websocket.Envelope) {
	if !c.IsAuthenticated() || !c.IsAgent() {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "not authorized"}))
		return
	}
	var data websocket.InboxAckData
	if err := json.Unmarshal(env.Data, &data); err != nil || data.ItemID == "" {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "invalid inbox ack data"}))
		return
	}
	if err := r.store.AckInboxItem(c.Context(), data.ItemID); err != nil {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "failed to ack inbox item"}))
		return
	}
	c.Send(websocket.NewEnvelope(websocket.EventInboxAck, map[string]string{"item_id": data.ItemID, "status": "acked"}))
}

// --- Held Draft WebSocket Handlers ---

func (r *Router) handleWSDraftCreate(c *websocket.Conn, env websocket.Envelope) {
	if !c.IsAuthenticated() || !c.IsAgent() {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "not authorized"}))
		return
	}
	var data websocket.DraftCreateData
	if err := json.Unmarshal(env.Data, &data); err != nil || data.ChannelID == "" || data.Content == "" {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "channel_id and content required"}))
		return
	}
	roomVersion, err := r.store.GetChannelRoomVersion(c.Context(), data.ChannelID)
	if err != nil {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "failed to get room version"}))
		return
	}
	d := &proto.HeldDraft{
		AgentID:     c.ID(),
		ChannelID:   data.ChannelID,
		Content:     data.Content,
		ThreadID:    data.ThreadID,
		RoomVersion: roomVersion,
		Status:      proto.DraftHeld,
		ExpiresAt:   time.Now().Add(10 * time.Minute).UnixMilli(),
	}
	if err := r.store.CreateDraft(c.Context(), d); err != nil {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "failed to create draft"}))
		return
	}
	c.Send(websocket.NewEnvelope(websocket.EventDraftAck, websocket.DraftAckData{
		DraftID:     d.ID,
		RoomVersion: d.RoomVersion,
	}))
}

func (r *Router) handleWSDraftValidate(c *websocket.Conn, env websocket.Envelope) {
	if !c.IsAuthenticated() || !c.IsAgent() {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "not authorized"}))
		return
	}
	var data websocket.DraftValidateData
	if err := json.Unmarshal(env.Data, &data); err != nil || data.DraftID == "" {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "invalid draft validate data"}))
		return
	}
	d, err := r.store.GetDraft(c.Context(), data.DraftID)
	if err != nil || d == nil {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "draft not found"}))
		return
	}
	if d.AgentID != c.ID() {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "not your draft"}))
		return
	}
	currentVersion, err := r.store.GetChannelRoomVersion(c.Context(), d.ChannelID)
	if err != nil {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "failed to get room version"}))
		return
	}
	delta := currentVersion - d.RoomVersion
	result := websocket.DraftResultData{
		DraftID:        d.ID,
		Valid:          delta <= 5,
		CurrentVersion: currentVersion,
		DraftVersion:   d.RoomVersion,
		VersionDelta:   delta,
	}
	if delta > 5 {
		msgs, _ := r.store.GetRecentMessages(c.Context(), d.ChannelID, 5)
		for _, m := range msgs {
			result.RecentMessages = append(result.RecentMessages, m)
		}
	}
	c.Send(websocket.NewEnvelope(websocket.EventDraftResult, result))
}

func (r *Router) handleWSDraftSend(c *websocket.Conn, env websocket.Envelope) {
	if !c.IsAuthenticated() || !c.IsAgent() {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "not authorized"}))
		return
	}
	var data websocket.DraftSendData
	if err := json.Unmarshal(env.Data, &data); err != nil || data.DraftID == "" {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "invalid draft send data"}))
		return
	}
	d, err := r.store.GetDraft(c.Context(), data.DraftID)
	if err != nil || d == nil {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "draft not found"}))
		return
	}
	if d.AgentID != c.ID() {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "not your draft"}))
		return
	}
	if d.Status != proto.DraftHeld {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "draft is not in held status"}))
		return
	}
	currentVersion, _ := r.store.GetChannelRoomVersion(c.Context(), d.ChannelID)
	delta := currentVersion - d.RoomVersion
	if delta > 5 && !data.ForceVersion {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "draft is stale, use force_version to override"}))
		return
	}
	msg := &proto.Message{
		ChannelID:   d.ChannelID,
		SenderID:    d.AgentID,
		ThreadID:    d.ThreadID,
		Content:     d.Content,
		ContentType: "text",
		Type:        "text",
	}
	if err := r.store.CreateMessage(c.Context(), msg); err != nil {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "failed to create message"}))
		return
	}
	r.store.UpdateDraftStatus(c.Context(), data.DraftID, proto.DraftSent)
	r.hub.SendNewMessage(d.ChannelID, msg)
	c.Send(websocket.NewEnvelope(websocket.EventDraftSend, map[string]string{"message_id": msg.ID, "status": "sent"}))
}

// --- Review WebSocket Handlers ---

func (r *Router) handleWSReviewRequest(c *websocket.Conn, env websocket.Envelope) {
	if !c.IsAuthenticated() || !c.IsAgent() {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "not authorized"}))
		return
	}
	var data websocket.ReviewRequestData
	if err := json.Unmarshal(env.Data, &data); err != nil {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "invalid review request data"}))
		return
	}
	if data.ReviewerID == "" || data.Subject == "" || data.Content == "" || data.ChannelID == "" {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "reviewer_id, subject, content, and channel_id required"}))
		return
	}
	member, err := r.store.GetMember(c.Context(), c.ID())
	if err != nil || member == nil {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "agent not found"}))
		return
	}
	rr := &proto.ReviewRequest{
		WorkspaceID: member.WorkspaceID,
		ChannelID:   data.ChannelID,
		RequesterID: c.ID(),
		ReviewerID:  data.ReviewerID,
		Subject:     data.Subject,
		Content:     data.Content,
		Status:      proto.ReviewPending,
	}
	if err := r.store.CreateReviewRequest(c.Context(), rr); err != nil {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "failed to create review"}))
		return
	}
	// Post message to channel
	msg := &proto.Message{
		ChannelID: data.ChannelID,
		SenderID:  c.ID(),
		Content:   fmt.Sprintf("Review requested: %s", data.Subject),
		Type:      "text",
		Metadata:  json.RawMessage(fmt.Sprintf(`{"type":"review_request","review_id":"%s"}`, rr.ID)),
	}
	r.store.CreateMessage(c.Context(), msg)
	r.hub.SendNewMessage(data.ChannelID, msg)
	// Create inbox notification for reviewer
	n := &proto.Notification{
		MemberID:    data.ReviewerID,
		Type:        "review_request",
		Title:       "Review requested",
		Body:        fmt.Sprintf("%s requested your review: %s", member.Name, data.Subject),
		ChannelID:   data.ChannelID,
		MessageID:   msg.ID,
		SourceType:  "review_request",
		Priority:    "high",
		AckRequired: true,
		Payload:     fmt.Sprintf(`{"review_id":"%s","channel_id":"%s"}`, rr.ID, data.ChannelID),
	}
	r.store.CreateNotification(c.Context(), n)
	if reviewerConn := r.hub.GetConn(data.ReviewerID); reviewerConn != nil {
		reviewerConn.Send(websocket.NewEnvelope(websocket.EventNotificationNew, n))
	}
	c.Send(websocket.NewEnvelope(websocket.EventReviewRequest, map[string]string{"review_id": rr.ID, "status": "pending"}))
}

func (r *Router) handleWSReviewResult(c *websocket.Conn, env websocket.Envelope) {
	if !c.IsAuthenticated() || !c.IsAgent() {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "not authorized"}))
		return
	}
	var data websocket.ReviewResultData
	if err := json.Unmarshal(env.Data, &data); err != nil {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "invalid review result data"}))
		return
	}
	if data.ReviewID == "" || data.Status == "" {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "review_id and status required"}))
		return
	}
	rr, err := r.store.GetReviewRequest(c.Context(), data.ReviewID)
	if err != nil || rr == nil {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "review not found"}))
		return
	}
	if rr.ReviewerID != c.ID() {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "not the reviewer"}))
		return
	}
	rr.Status = proto.ReviewStatus(data.Status)
	rr.ReviewComment = data.Comment
	if data.Status == string(proto.ReviewApproved) || data.Status == string(proto.ReviewChangesRequired) {
		rr.ReviewedAt = time.Now().UnixMilli()
	}
	if err := r.store.UpdateReviewRequest(c.Context(), rr); err != nil {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "failed to update review"}))
		return
	}
	// Post result to channel
	msg := &proto.Message{
		ChannelID: rr.ChannelID,
		SenderID:  c.ID(),
		Content:   fmt.Sprintf("Review %s: %s", data.Status, data.Comment),
		Type:      "text",
	}
	r.store.CreateMessage(c.Context(), msg)
	r.hub.SendNewMessage(rr.ChannelID, msg)
	// Notify requester
	n := &proto.Notification{
		MemberID:   rr.RequesterID,
		Type:       "review_result",
		Title:      "Review completed",
		Body:       fmt.Sprintf("Your review was %s", data.Status),
		ChannelID:  rr.ChannelID,
		SourceType: "review_result",
		Priority:   "high",
	}
	r.store.CreateNotification(c.Context(), n)
	if requesterConn := r.hub.GetConn(rr.RequesterID); requesterConn != nil {
		requesterConn.Send(websocket.NewEnvelope(websocket.EventNotificationNew, n))
	}
	c.Send(websocket.NewEnvelope(websocket.EventReviewResult, map[string]string{"review_id": rr.ID, "status": string(rr.Status)}))
}

// --- Agent Workspace WebSocket Handler ---

func (r *Router) handleWSWorkspaceUpdate(c *websocket.Conn, env websocket.Envelope) {
	if !c.IsAuthenticated() || !c.IsAgent() {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "not authorized"}))
		return
	}
	var data websocket.WorkspaceUpdateData
	if err := json.Unmarshal(env.Data, &data); err != nil {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "invalid workspace update data"}))
		return
	}
	if data.Name == "" {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "name is required"}))
		return
	}
	if data.ItemID != "" {
		// Update existing item
		existing, err := r.store.GetWorkspaceItem(c.Context(), data.ItemID)
		if err != nil || existing == nil {
			c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "item not found"}))
			return
		}
		if existing.AgentID != c.ID() {
			c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "not your workspace item"}))
			return
		}
		existing.Name = data.Name
		if data.Content != "" {
			existing.Content = data.Content
		}
		if data.Namespace != "" {
			existing.Namespace = data.Namespace
		}
		if data.Description != "" {
			existing.Description = data.Description
		}
		if len(data.Tags) > 0 {
			existing.Tags = data.Tags
		}
		if err := r.store.UpdateWorkspaceItem(c.Context(), existing); err != nil {
			c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "failed to update item"}))
			return
		}
		c.Send(websocket.NewEnvelope(websocket.EventWorkspaceUpdate, existing))
	} else {
		// Create new item
		member, err := r.store.GetMember(c.Context(), c.ID())
		if err != nil || member == nil {
			c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "agent not found"}))
			return
		}
		ns := data.Namespace
		if ns == "" {
			ns = "default"
		}
		item := &proto.AgentWorkspaceItem{
			AgentID:     c.ID(),
			WorkspaceID: member.WorkspaceID,
			Name:        data.Name,
			Content:     data.Content,
			Namespace:   ns,
			Description: data.Description,
			Tags:        data.Tags,
			MimeType:    "text/plain",
		}
		if err := r.store.CreateWorkspaceItem(c.Context(), item); err != nil {
			c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "failed to create item"}))
			return
		}
		c.Send(websocket.NewEnvelope(websocket.EventWorkspaceUpdate, item))
	}
}

// handleWSDaemonRegister handles daemon.register events from local daemons.
func (r *Router) handleWSDaemonRegister(c *websocket.Conn, env websocket.Envelope) {
	if !c.IsAuthenticated() {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "not authenticated"}))
		return
	}

	var data websocket.DaemonRegisterData
	if err := json.Unmarshal(env.Data, &data); err != nil {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "invalid daemon register data"}))
		return
	}

	if len(data.Agents) == 0 {
		c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "no agents provided"}))
		return
	}

	// Generate daemon ID
	daemonID := fmt.Sprintf("daemon_%s_%d", c.ID(), time.Now().UnixMilli())

	// Build agents map
	agents := make(map[string]string)
	for _, a := range data.Agents {
		agents[a.Name] = a.AgentID
	}

	// Register with hub
	r.hub.RegisterDaemon(daemonID, c, agents)

	// Send confirmation
	c.Send(websocket.NewEnvelope(websocket.EventDaemonRegistered, websocket.DaemonRegisteredData{
		DaemonID: daemonID,
		Agents:   data.Agents,
	}))

	r.logger.Info("daemon registered",
		"daemon_id", daemonID,
		"agents", len(data.Agents),
		"member_id", c.ID(),
	)
}
