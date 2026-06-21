package route

import (
	"github.com/HappYness-Project/chatApi/internal/chatread/domain"
	"github.com/HappYness-Project/chatApi/internal/chatread/repository"
	"github.com/google/uuid"
)

type MarkReadRequest struct {
	LastReadMessageID uuid.UUID `json:"last_read_message_id" validate:"required"`
}

type UnreadCountResponse struct {
	ChatID uuid.UUID `json:"chat_id"`
	Count  int       `json:"count"`
}

type UnreadListResponse struct {
	Chats []repository.UnreadEntry `json:"chats"`
}

type ReadsListResponse struct {
	Reads []domain.ChatRead `json:"reads"`
}
