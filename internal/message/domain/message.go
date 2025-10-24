package entity

import (
	"time"
)

type Message struct {
	ID          string     `json:"id"`
	ChatID      string     `json:"chat_id"`
	SenderID    string     `json:"sender_id"`
	Content     string     `json:"content"`
	MessageType string     `json:"message_type"`
	CreatedAt   time.Time  `json:"created_at"`
	ReadStatus  bool       `json:"read_status"`
	DeletedAt   *time.Time `json:"deleted_at,omitempty"`
	DeletedBy   *string    `json:"deleted_by,omitempty"`
}
