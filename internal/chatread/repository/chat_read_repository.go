package repository

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/grupa-repo/chats/internal/chatread/domain"
	"github.com/google/uuid"
)

type ChatReadRepository interface {
	Upsert(userID, chatID uuid.UUID, lastReadMessageID uuid.UUID) (*domain.ChatRead, error)
	BulkUpsert(userID uuid.UUID, items []BulkReadItem) ([]domain.ChatRead, error)
	Get(userID, chatID uuid.UUID) (*domain.ChatRead, error)
	UnreadCount(userID, chatID uuid.UUID) (int, error)
	ListUnreadCounts(userID uuid.UUID, chatIDs []uuid.UUID) ([]UnreadEntry, error)
	ListReads(userID uuid.UUID) ([]domain.ChatRead, error)
	ListChatIDsForUser(userID uuid.UUID) ([]uuid.UUID, error)
	AddMember(userID, chatID uuid.UUID) error
}

type BulkReadItem struct {
	ChatID            uuid.UUID
	LastReadMessageID uuid.UUID
}

type UnreadEntry struct {
	ChatID        uuid.UUID  `json:"chat_id"`
	Count         int        `json:"count"`
	LastMessageAt *time.Time `json:"last_message_at,omitempty"`
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

// BulkUpsert applies the monotonic upsert across many (chat_id, last_read_message_id)
// pairs in a single round-trip. It returns the authoritative row for every input chat,
// including ones whose marker was not advanced because the request was stale.
func (r *ChatReadRepo) BulkUpsert(userID uuid.UUID, items []BulkReadItem) ([]domain.ChatRead, error) {
	if len(items) == 0 {
		return []domain.ChatRead{}, nil
	}

	chatIDs := make([]string, len(items))
	msgIDs := make([]string, len(items))
	for i, it := range items {
		chatIDs[i] = it.ChatID.String()
		msgIDs[i] = it.LastReadMessageID.String()
	}

	tx, err := r.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	upsert := `
		INSERT INTO chat_reads (user_id, chat_id, last_read_message_id, last_read_at)
		SELECT $1, c, m, NOW()
		FROM unnest(
		    string_to_array($2::text, ',')::uuid[],
		    string_to_array($3::text, ',')::uuid[]
		) AS t(c, m)
		ON CONFLICT (user_id, chat_id) DO UPDATE
		SET last_read_message_id = EXCLUDED.last_read_message_id,
		    last_read_at         = EXCLUDED.last_read_at
		WHERE EXCLUDED.last_read_message_id > chat_reads.last_read_message_id
		   OR chat_reads.last_read_message_id IS NULL`
	if _, err := tx.Exec(upsert, userID, strings.Join(chatIDs, ","), strings.Join(msgIDs, ",")); err != nil {
		return nil, err
	}

	read := `
		SELECT user_id, chat_id, last_read_message_id, last_read_at
		FROM chat_reads
		WHERE user_id = $1
		  AND chat_id = ANY(string_to_array($2::text, ',')::uuid[])`
	rows, err := tx.Query(read, userID, strings.Join(chatIDs, ","))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.ChatRead
	for rows.Next() {
		var cr domain.ChatRead
		if err := rows.Scan(&cr.UserID, &cr.ChatID, &cr.LastReadMessageID, &cr.LastReadAt); err != nil {
			return nil, err
		}
		out = append(out, cr)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
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

// ListUnreadCounts returns per-chat unread counts and last_message_at for the user.
// If chatIDs is non-empty, scope is restricted to those chats; otherwise scope
// is derived from chats the user has either read or messaged in.
func (r *ChatReadRepo) ListUnreadCounts(userID uuid.UUID, chatIDs []uuid.UUID) ([]UnreadEntry, error) {
	var rows *sql.Rows
	var err error

	const countExpr = `
		COALESCE((
			SELECT COUNT(*) FROM message m
			LEFT JOIN chat_reads cr2 ON cr2.user_id = $1 AND cr2.chat_id = m.chat_id
			WHERE m.chat_id = tc.chat_id
			  AND m.deleted_at IS NULL
			  AND m.sender_id <> $1
			  AND (cr2.last_read_message_id IS NULL OR m.id > cr2.last_read_message_id)
		), 0) AS unread_count,
		(SELECT MAX(created_at) FROM message
		  WHERE chat_id = tc.chat_id AND deleted_at IS NULL) AS last_message_at`

	if len(chatIDs) == 0 {
		query := `
			WITH target_chats AS (
				SELECT chat_id FROM chat_reads WHERE user_id = $1
				UNION
				SELECT DISTINCT chat_id FROM message
				  WHERE sender_id = $1 AND deleted_at IS NULL
			)
			SELECT tc.chat_id,` + countExpr + `
			FROM target_chats tc
			ORDER BY last_message_at DESC NULLS LAST`
		rows, err = r.db.Query(query, userID)
	} else {
		ids := make([]string, len(chatIDs))
		for i, id := range chatIDs {
			ids[i] = id.String()
		}
		query := `
			SELECT tc.chat_id,` + countExpr + `
			FROM unnest(string_to_array($2::text, ',')::uuid[]) AS tc(chat_id)
			ORDER BY last_message_at DESC NULLS LAST`
		rows, err = r.db.Query(query, userID, strings.Join(ids, ","))
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []UnreadEntry
	for rows.Next() {
		var e UnreadEntry
		if err := rows.Scan(&e.ChatID, &e.Count, &e.LastMessageAt); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

func (r *ChatReadRepo) ListReads(userID uuid.UUID) ([]domain.ChatRead, error) {
	query := `
		SELECT user_id, chat_id, last_read_message_id, last_read_at
		FROM chat_reads
		WHERE user_id = $1
		ORDER BY last_read_at DESC`

	rows, err := r.db.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reads []domain.ChatRead
	for rows.Next() {
		var cr domain.ChatRead
		if err := rows.Scan(&cr.UserID, &cr.ChatID, &cr.LastReadMessageID, &cr.LastReadAt); err != nil {
			return nil, err
		}
		reads = append(reads, cr)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return reads, nil
}

// ListChatIDsForUser returns the chat IDs a user is associated with — chats
// they've read or messaged in. Used as a stand-in for the external membership
// API until that integration lands; once it does, swap this for the
// authoritative source.
func (r *ChatReadRepo) ListChatIDsForUser(userID uuid.UUID) ([]uuid.UUID, error) {
	query := `
		SELECT chat_id FROM chat_reads WHERE user_id = $1
		UNION
		SELECT DISTINCT chat_id FROM message
		  WHERE sender_id = $1 AND deleted_at IS NULL`
	rows, err := r.db.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return ids, nil
}

// AddMember seeds a chat_reads row so the (user, chat) pair is treated as
// a member by the WS layer, which derives membership from this table.
// last_read_message_id is NULL — the user has read nothing yet. If a row
// already exists it is left alone so an established read marker is not
// clobbered.
func (r *ChatReadRepo) AddMember(userID, chatID uuid.UUID) error {
	query := `
		INSERT INTO chat_reads (user_id, chat_id, last_read_message_id, last_read_at)
		VALUES ($1, $2, NULL, NOW())
		ON CONFLICT (user_id, chat_id) DO NOTHING`
	_, err := r.db.Exec(query, userID, chatID)
	return err
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
