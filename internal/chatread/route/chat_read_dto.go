package route

import "github.com/google/uuid"

type MarkReadRequest struct {
	LastReadMessageID uuid.UUID `json:"last_read_message_id" validate:"required"`
}

type UnreadCountResponse struct {
	ChatID uuid.UUID `json:"chat_id"`
	Count  int       `json:"count"`
}
