package proto

import (
	"github.com/google/uuid"
)

// Envelope is defined in the websocket package.
// This file only provides shared helpers.

// NewID generates a random UUID.
func NewID() string {
	return uuid.New().String()
}
