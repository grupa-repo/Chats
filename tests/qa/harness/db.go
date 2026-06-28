//go:build qa

package harness

import (
	"database/sql"
	"sync"
	"testing"

	"github.com/grupa-repo/chats/dbs"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

var (
	dbOnce sync.Once
	dbConn *sql.DB
	dbErr  error
)

// JoinChat seeds membership by inserting a chat.chat_reads row with NULL
// last_read_message_id. The per-user socket's auto-subscribe reads from
// (chat_reads ∪ sent-messages), so this is enough to make the next Dial
// pick the chat up.
//
// Why direct DB: the public POST /api/chats/{chatID}/read endpoint requires
// a real last_read_message_id, but a brand-new chat has none. Once the
// authoritative membership API lands, replace this with a call to it.
func JoinChat(t *testing.T, cfg Config, userID, chatID uuid.UUID) {
	t.Helper()
	db := openDB(t, cfg)
	_, err := db.Exec(`
		INSERT INTO chat.chat_reads (user_id, chat_id, last_read_message_id)
		VALUES ($1, $2, NULL)
		ON CONFLICT (user_id, chat_id) DO NOTHING`,
		userID, chatID,
	)
	require.NoError(t, err, "seed chat_reads")
}

// CleanupUser removes all chat_reads rows for a synthetic test user. Safe
// because NewUser mints a fresh UUID per test — nothing else owns these rows.
func CleanupUser(t *testing.T, cfg Config, userID uuid.UUID) {
	t.Helper()
	db := openDB(t, cfg)
	_, err := db.Exec(`DELETE FROM chat.chat_reads WHERE user_id = $1`, userID)
	if err != nil {
		t.Logf("cleanup chat_reads for %s: %v", userID, err)
	}
}

func openDB(t *testing.T, cfg Config) *sql.DB {
	t.Helper()
	dbOnce.Do(func() { dbConn, dbErr = dbs.ConnectToDb(cfg.DSN) })
	require.NoError(t, dbErr, "open QA db")
	require.NotNil(t, dbConn, "QA db conn")
	return dbConn
}
