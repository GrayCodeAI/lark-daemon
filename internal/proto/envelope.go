package proto

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Envelope is the wire format for all messages.
type Envelope struct {
	V    int             `json:"v"`
	ID   string          `json:"id,omitempty"`
	Type string          `json:"type"`
	TS   int64           `json:"ts"`
	Data json.RawMessage `json:"data,omitempty"`
}

func NewEnvelope(eventType string, data any) Envelope {
	return Envelope{
		V:    1,
		ID:   uuid.New().String(),
		Type: eventType,
		TS:   time.Now().UnixMilli(),
		Data: mustMarshal(data),
	}
}

func mustMarshal(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("{}")
	}
	return b
}

// NewID generates a random ID.
func NewID() string {
	return uuid.New().String()
}
