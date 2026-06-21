package domain

import (
	"time"

	"github.com/google/uuid"
)

type ChatRead struct {
	UserID            uuid.UUID  `json:"user_id"`
	ChatID            uuid.UUID  `json:"chat_id"`
	LastReadMessageID *uuid.UUID `json:"last_read_message_id,omitempty"`
	LastReadAt        time.Time  `json:"last_read_at"`
}
