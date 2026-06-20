package repository

import (
	"database/sql"

	"github.com/HappYness-Project/chatApi/internal/chat/domain"
	"github.com/google/uuid"
)

type ChatRepository interface {
	GetChatById(chatId uuid.UUID) (*domain.Chat, error)
	GetChatByGroupID(groupID int) (*domain.Chat, error)
	GetChatByContainerID(containerID uuid.UUID) (*domain.Chat, error)
	CreateChat(chat *domain.Chat) (*domain.Chat, error)
	DeleteChat(chatId uuid.UUID) error
}

type ChatRepo struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *ChatRepo {
	return &ChatRepo{db: db}
}

func (r *ChatRepo) GetChatById(chatId uuid.UUID) (*domain.Chat, error) {
	rows, err := r.db.Query(`SELECT id, type, usergroup_id, container_id, created_at
							FROM chat
							WHERE id = $1`, chatId)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	chat := new(domain.Chat)
	for rows.Next() {
		chat, err = scanRowsIntoChat(rows)
		if err != nil {
			return nil, err
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return chat, nil
}

func (r *ChatRepo) GetChatByGroupID(groupID int) (*domain.Chat, error) {
	rows, err := r.db.Query(`SELECT id, type, usergroup_id, container_id, created_at
							FROM chat
							WHERE usergroup_id = $1`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	chat := new(domain.Chat)
	for rows.Next() {
		chat, err = scanRowsIntoChat(rows)
		if err != nil {
			return nil, err
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return chat, nil
}

func (r *ChatRepo) GetChatByContainerID(containerID uuid.UUID) (*domain.Chat, error) {
	rows, err := r.db.Query(`SELECT id, type, usergroup_id, container_id, created_at
							FROM chat
							WHERE container_id = $1`, containerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	chat := new(domain.Chat)
	for rows.Next() {
		chat, err = scanRowsIntoChat(rows)
		if err != nil {
			return nil, err
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return chat, nil
}

func (r *ChatRepo) CreateChat(chat *domain.Chat) (*domain.Chat, error) {
	_, err := r.db.Exec(`INSERT INTO chat (id, type, usergroup_id, container_id, created_at)
						VALUES ($1, $2, $3, $4, $5)`,
		chat.Id, chat.Type.String(), chat.UserGroupId, chat.ContainerId, chat.CreatedAt)
	if err != nil {
		return nil, err
	}

	return chat, nil
}

func (r *ChatRepo) DeleteChat(chatId uuid.UUID) error {
	_, err := r.db.Exec(`DELETE FROM chat WHERE id = $1`, chatId)
	return err
}

func scanRowsIntoChat(rows *sql.Rows) (*domain.Chat, error) {
	chat := new(domain.Chat)
	var typeStr string
	err := rows.Scan(
		&chat.Id,
		&typeStr,
		&chat.UserGroupId,
		&chat.ContainerId,
		&chat.CreatedAt,
	)
	if err != nil {
		return nil, err
	}

	chat.Type = domain.ChatType(typeStr)
	return chat, nil
}
