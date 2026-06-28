//go:build qa

package harness

import (
	"encoding/json"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

// Envelope mirrors broadcaster.Event on the wire; payload stays raw so the
// caller decodes into the per-event-type struct it expects.
type Envelope struct {
	Type    string          `json:"type"`
	ChatID  string          `json:"chat_id,omitempty"`
	Payload json.RawMessage `json:"payload"`
}

// WSClient is one /api/ws connection for one user. Frames land in a buffered
// channel; Expect/ExpectNone drain it. Not safe for concurrent Send from
// multiple goroutines (gorilla/websocket forbids concurrent writes).
type WSClient struct {
	UserID uuid.UUID
	conn   *websocket.Conn
	in     chan Envelope
	stop   chan struct{}
	t      *testing.T
}

// Dial opens the per-user socket and starts the read pump. Token is appended
// as ?token=... per the documented contract.
func Dial(t *testing.T, cfg Config, user User) *WSClient {
	t.Helper()

	parsed, err := url.Parse(cfg.WSURL)
	require.NoError(t, err, "parse WSURL")
	q := parsed.Query()
	q.Set("token", user.Token)
	parsed.RawQuery = q.Encode()

	conn, _, err := websocket.DefaultDialer.Dial(parsed.String(), nil)
	require.NoError(t, err, "dial ws")

	c := &WSClient{
		UserID: user.ID,
		conn:   conn,
		in:     make(chan Envelope, 64),
		stop:   make(chan struct{}),
		t:      t,
	}
	go c.readPump()
	return c
}

func (c *WSClient) readPump() {
	defer close(c.in)
	for {
		var env Envelope
		if err := c.conn.ReadJSON(&env); err != nil {
			return
		}
		select {
		case c.in <- env:
		case <-c.stop:
			return
		}
	}
}

// Close shuts the socket; safe to call multiple times.
func (c *WSClient) Close() {
	select {
	case <-c.stop:
		return
	default:
		close(c.stop)
	}
	_ = c.conn.WriteMessage(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	_ = c.conn.Close()
}

// SendMessage sends a send_message action frame.
func (c *WSClient) SendMessage(chatID uuid.UUID, content string) {
	c.t.Helper()
	frame := map[string]any{
		"action":  "send_message",
		"chat_id": chatID.String(),
		"content": content,
	}
	require.NoError(c.t, c.conn.WriteJSON(frame), "send_message")
}

// DeleteMessage sends a delete_message action frame.
func (c *WSClient) DeleteMessage(chatID, messageID uuid.UUID) {
	c.t.Helper()
	frame := map[string]any{
		"action":     "delete_message",
		"chat_id":    chatID.String(),
		"message_id": messageID.String(),
	}
	require.NoError(c.t, c.conn.WriteJSON(frame), "delete_message")
}

// ExpectReady drains until it sees the "ready" frame and returns its chat IDs.
// Fails the test on timeout or wrong event type — ready is always first.
func (c *WSClient) ExpectReady(timeout time.Duration) []string {
	c.t.Helper()
	env := c.next(timeout)
	require.Equal(c.t, "ready", env.Type, "first frame should be ready, got %q", env.Type)
	var p struct {
		ChatIDs []string `json:"chat_ids"`
	}
	require.NoError(c.t, json.Unmarshal(env.Payload, &p))
	return p.ChatIDs
}

// Expect drains frames until one matches (eventType, chatID), returns it.
// Pass chatID = "" to match any chat. Fails on timeout.
//
// Non-matching frames are dropped — if you care about ordering or absence,
// use ExpectNone or assert on the full sequence yourself.
func (c *WSClient) Expect(eventType, chatID string, timeout time.Duration) Envelope {
	c.t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			c.t.Fatalf("timed out waiting for %s on chat %q", eventType, chatID)
		}
		env := c.next(remaining)
		if env.Type == eventType && (chatID == "" || env.ChatID == chatID) {
			return env
		}
	}
}

// ExpectNone fails if any frame arrives within the window.
// Use to assert isolation (e.g. a user not in a chat sees nothing about it).
func (c *WSClient) ExpectNone(within time.Duration) {
	c.t.Helper()
	select {
	case env, ok := <-c.in:
		if !ok {
			c.t.Fatal("socket closed during ExpectNone")
		}
		c.t.Fatalf("expected no frames, got: type=%s chat_id=%s", env.Type, env.ChatID)
	case <-time.After(within):
	}
}

func (c *WSClient) next(timeout time.Duration) Envelope {
	c.t.Helper()
	select {
	case env, ok := <-c.in:
		if !ok {
			c.t.Fatal("socket closed; no more frames")
		}
		return env
	case <-time.After(timeout):
		c.t.Fatal(fmt.Sprintf("timeout after %s waiting for frame", timeout))
	}
	return Envelope{} // unreachable
}
