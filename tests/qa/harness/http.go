//go:build qa

package harness

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

var httpClient = &http.Client{Timeout: 10 * time.Second}

// CreateChat POSTs /api/chats and returns the new chat ID.
// Pass "private", "group", or "container" as chatType. For "group" or
// "container" pass a non-nil seed in the request body; this helper sticks
// to "private" because that's all the WS tests need.
func CreateChat(t *testing.T, cfg Config, token, chatType string) uuid.UUID {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"type": chatType})
	req := newReq(t, http.MethodPost, cfg.BaseURL+"/api/chats", token, body)

	resp, err := httpClient.Do(req)
	require.NoError(t, err, "create chat")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create chat: %d: %s", resp.StatusCode, b)
	}
	var out struct {
		ID uuid.UUID `json:"id"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	return out.ID
}

// MarkRead POSTs /api/chats/{chatID}/read with the given last_read_message_id.
// Server rejects uuid.Nil — pass a real message ID.
func MarkRead(t *testing.T, cfg Config, token string, chatID, lastMessageID uuid.UUID) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"last_read_message_id": lastMessageID.String()})
	req := newReq(t, http.MethodPost, cfg.BaseURL+"/api/chats/"+chatID.String()+"/read", token, body)

	resp, err := httpClient.Do(req)
	require.NoError(t, err, "mark read")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("mark read: %d: %s", resp.StatusCode, b)
	}
}

type UnreadEntry struct {
	ChatID uuid.UUID `json:"chat_id"`
	Count  int       `json:"count"`
}

// ListUnread GETs /api/chats/unread for the calling user.
func ListUnread(t *testing.T, cfg Config, token string) map[uuid.UUID]int {
	t.Helper()
	req := newReq(t, http.MethodGet, cfg.BaseURL+"/api/chats/unread", token, nil)

	resp, err := httpClient.Do(req)
	require.NoError(t, err, "list unread")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("list unread: %d: %s", resp.StatusCode, b)
	}
	var out struct {
		Chats []UnreadEntry `json:"chats"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))

	counts := make(map[uuid.UUID]int, len(out.Chats))
	for _, e := range out.Chats {
		counts[e.ChatID] = e.Count
	}
	return counts
}

// Resync POSTs /api/internal/membership/resync as the membership service
// would: shared-secret header, full chat-id list for the affected user.
// Asserts 204 — the endpoint has no body to return.
func Resync(t *testing.T, cfg Config, internalToken string, userID uuid.UUID, chatIDs []uuid.UUID) {
	t.Helper()
	ids := make([]string, len(chatIDs))
	for i, c := range chatIDs {
		ids[i] = c.String()
	}
	body, _ := json.Marshal(map[string]any{
		"user_id":  userID.String(),
		"chat_ids": ids,
	})

	req, err := http.NewRequest(http.MethodPost, cfg.BaseURL+"/api/internal/membership/resync", bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Token", internalToken)

	resp, err := httpClient.Do(req)
	require.NoError(t, err, "resync")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("resync: %d: %s", resp.StatusCode, b)
	}
}

func newReq(t *testing.T, method, url, token string, body []byte) *http.Request {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, url, rdr)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req
}
