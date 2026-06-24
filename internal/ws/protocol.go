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

// --- Outbound payload shapes ---
//
// The wire envelope on the per-user /ws socket is broadcaster.Event
// ({type, chat_id, payload}). These structs document the shape of the
// type-specific payload field for each event type.

// MessagePayload is the payload for "message.created" events.
type MessagePayload struct {
	ID          uuid.UUID `json:"id"`
	SenderID    uuid.UUID `json:"sender_id"`
	Content     string    `json:"content"`
	MessageType string    `json:"message_type"`
	CreatedAt   time.Time `json:"created_at"`
}

// MessageDeletedPayload is the payload for "message.deleted" events.
type MessageDeletedPayload struct {
	ID        uuid.UUID `json:"id"`
	DeletedAt time.Time `json:"deleted_at"`
}

// ErrorPayload is the payload for "error" events.
type ErrorPayload struct {
	Error string `json:"error"`
}

// ReadyPayload is the payload for the "ready" event sent after the server has
// subscribed the connection to every chat the user is a member of. The client
// uses this signal to re-sync via GET /chats/unread.
type ReadyPayload struct {
	ChatIDs []string `json:"chat_ids"`
}

// ChatReadPayload is the payload for "chat.read" events. Lets other members
// see live read receipts and lets the same user's other devices clear the
// unread badge.
type ChatReadPayload struct {
	UserID            uuid.UUID `json:"user_id"`
	LastReadMessageID uuid.UUID `json:"last_read_message_id"`
}

// Event type constants.
const (
	EventMessageCreated = "message.created"
	EventMessageDeleted = "message.deleted"
	EventChatRead       = "chat.read"
	EventReady          = "ready"
	EventError          = "error"
)
