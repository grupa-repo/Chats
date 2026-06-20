package ws

import (
	"time"

	"github.com/google/uuid"
)

// --- Inbound (client → server) ---

type WSInbound struct {
	Action    string    `json:"action"`
	ChatID    uuid.UUID `json:"chat_id,omitempty"`
	Content   string    `json:"content,omitempty"`
	MessageID uuid.UUID `json:"message_id,omitempty"`
}

type InboundEnvelope struct {
	UserID  uuid.UUID
	Payload WSInbound
}

// --- Outbound (server → client) ---

type WSOutbound struct {
	Event     string           `json:"event"`
	ChatID    uuid.UUID        `json:"chat_id,omitempty"`
	Message   *OutboundMessage `json:"message,omitempty"`
	Error     string           `json:"error,omitempty"`
	Timestamp time.Time        `json:"timestamp"`
}

type OutboundMessage struct {
	ID          uuid.UUID  `json:"id"`
	SenderID    uuid.UUID  `json:"sender_id"`
	Content     string     `json:"content"`
	MessageType string     `json:"message_type"`
	CreatedAt   time.Time  `json:"created_at"`
	DeletedAt   *time.Time `json:"deleted_at,omitempty"`
}
