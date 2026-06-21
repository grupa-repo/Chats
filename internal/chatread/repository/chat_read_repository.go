package repository

import (
	"database/sql"
	"errors"

	"github.com/HappYness-Project/chatApi/internal/chatread/domain"
	"github.com/google/uuid"
)

type ChatReadRepository interface {
	Upsert(userID, chatID uuid.UUID, lastReadMessageID uuid.UUID) (*domain.ChatRead, error)
	Get(userID, chatID uuid.UUID) (*domain.ChatRead, error)
	UnreadCount(userID, chatID uuid.UUID) (int, error)
}

type ChatReadRepo struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *ChatReadRepo {
	return &ChatReadRepo{db: db}
}

// Upsert advances the read marker for (user, chat) to lastReadMessageID.
// The WHERE clause relies on UUID v7 lexicographic ordering matching creation order,
// so an older message id cannot regress a newer marker.
func (r *ChatReadRepo) Upsert(userID, chatID uuid.UUID, lastReadMessageID uuid.UUID) (*domain.ChatRead, error) {
	query := `
		INSERT INTO chat_reads (user_id, chat_id, last_read_message_id, last_read_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (user_id, chat_id) DO UPDATE
		SET last_read_message_id = EXCLUDED.last_read_message_id,
		    last_read_at         = EXCLUDED.last_read_at
		WHERE EXCLUDED.last_read_message_id > chat_reads.last_read_message_id
		   OR chat_reads.last_read_message_id IS NULL
		RETURNING user_id, chat_id, last_read_message_id, last_read_at`

	row := r.db.QueryRow(query, userID, chatID, lastReadMessageID)

	cr := new(domain.ChatRead)
	err := row.Scan(&cr.UserID, &cr.ChatID, &cr.LastReadMessageID, &cr.LastReadAt)
	if errors.Is(err, sql.ErrNoRows) {
		// The conflict update was skipped because the incoming marker is not newer.
		// Return the existing row so callers see the authoritative state.
		return r.Get(userID, chatID)
	}
	if err != nil {
		return nil, err
	}
	return cr, nil
}

func (r *ChatReadRepo) Get(userID, chatID uuid.UUID) (*domain.ChatRead, error) {
	query := `
		SELECT user_id, chat_id, last_read_message_id, last_read_at
		FROM chat_reads
		WHERE user_id = $1 AND chat_id = $2`

	row := r.db.QueryRow(query, userID, chatID)

	cr := new(domain.ChatRead)
	err := row.Scan(&cr.UserID, &cr.ChatID, &cr.LastReadMessageID, &cr.LastReadAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return cr, nil
}

func (r *ChatReadRepo) UnreadCount(userID, chatID uuid.UUID) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM message m
		LEFT JOIN chat_reads cr
		  ON cr.user_id = $1 AND cr.chat_id = m.chat_id
		WHERE m.chat_id = $2
		  AND m.deleted_at IS NULL
		  AND m.sender_id <> $1
		  AND (cr.last_read_message_id IS NULL OR m.id > cr.last_read_message_id)`

	var count int
	if err := r.db.QueryRow(query, userID, chatID).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}
