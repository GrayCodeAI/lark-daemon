package metrics

import (
	"context"
	"strconv"
	"time"

	"lark/internal/proto"
	"lark/internal/server/store"
)

// AgentMetrics holds aggregated metrics for an agent.
type AgentMetrics struct {
	AgentID        string `json:"agent_id"`
	MessagesSent   int    `json:"messages_sent"`
	TasksCompleted int    `json:"tasks_completed"`
	TasksPending   int    `json:"tasks_pending"`
	WakeCount      int    `json:"wake_count"`
	LastActiveAt   int64  `json:"last_active_at"`
}

// Collector gathers agent metrics from the store.
type Collector struct {
	store store.Store
}

// NewCollector creates a new metrics Collector.
func NewCollector(s store.Store) *Collector {
	return &Collector{store: s}
}

// GetAgentMetrics returns aggregated metrics for an agent.
func (c *Collector) GetAgentMetrics(ctx context.Context, agentID string) (*AgentMetrics, error) {
	m := &AgentMetrics{AgentID: agentID}

	// Count messages sent by this agent
	msgCount, err := c.store.CountMessagesBySender(ctx, agentID)
	if err == nil {
		m.MessagesSent = msgCount
	}

	// Count tasks by assignee
	completed, pending, err := c.store.CountTasksByAssignee(ctx, agentID)
	if err == nil {
		m.TasksCompleted = completed
		m.TasksPending = pending
	}

	// Get wake count from agent memory
	wakeMem, _ := c.store.GetMemory(ctx, agentID, "_metrics", "wake_count")
	if wakeMem != nil {
		if n, err := strconv.Atoi(wakeMem.Value); err == nil {
			m.WakeCount = n
		}
	}

	// Get last active from agent memory
	lastMem, _ := c.store.GetMemory(ctx, agentID, "_metrics", "last_active")
	if lastMem != nil {
		if t, err := time.Parse(time.RFC3339, lastMem.Value); err == nil {
			m.LastActiveAt = t.UnixMilli()
		}
	}

	return m, nil
}

// RecordWake increments the wake counter and updates last active time for an agent.
func (c *Collector) RecordWake(ctx context.Context, agentID string) {
	// Increment wake count
	wakeMem, _ := c.store.GetMemory(ctx, agentID, "_metrics", "wake_count")
	count := 0
	if wakeMem != nil {
		count, _ = strconv.Atoi(wakeMem.Value)
	}
	count++
	c.store.SetMemory(ctx, &proto.AgentMemory{
		AgentID:   agentID,
		Namespace: "_metrics",
		Key:       "wake_count",
		Value:     strconv.Itoa(count),
	})

	// Update last active
	c.store.SetMemory(ctx, &proto.AgentMemory{
		AgentID:   agentID,
		Namespace: "_metrics",
		Key:       "last_active",
		Value:     time.Now().Format(time.RFC3339),
	})
}
