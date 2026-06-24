// Package broadcaster defines the event fan-out abstraction used by the chat
// service. The in-process implementation lives here today; tomorrow the same
// interface will sit in front of an external bus (NATS/Redis) without changing
// the bytes on the wire to WebSocket clients.
package broadcaster

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
)

// Event is the canonical wire-format event. It must be fully serializable:
// no in-memory pointers, no interface{} fields backed by local types. The
// JSON form of Event is the contract with WebSocket clients.
type Event struct {
	Type    string          `json:"type"`
	ChatID  string          `json:"chat_id,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Handler receives events published to a topic. Implementations may invoke
// handlers concurrently across topics, so handlers must be safe to call from
// multiple goroutines. Handlers must not block; do any slow work asynchronously.
type Handler func(Event)

// Broadcaster fans out events to subscribers of a topic.
type Broadcaster interface {
	// Publish sends event to every current subscriber of topic. It must not
	// block on slow subscribers; backpressure is the caller's concern (e.g.
	// a bounded outbound channel per WebSocket connection).
	Publish(ctx context.Context, topic string, event Event) error

	// Subscribe registers handler for topic and returns a function that
	// removes the subscription. The unsubscribe func is idempotent.
	Subscribe(topic string, handler Handler) (unsubscribe func())
}

// ChatTopic returns the topic name for chat-scoped events.
func ChatTopic(chatID uuid.UUID) string {
	return "chat:" + chatID.String()
}

// UserTopic returns the topic name for user-scoped events (presence, etc.).
func UserTopic(userID uuid.UUID) string {
	return "user:" + userID.String()
}
