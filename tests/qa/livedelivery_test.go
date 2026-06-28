//go:build qa

// Package qa drives the per-user WebSocket against a deployed QA service.
// Build-tagged so it stays out of the default `go test ./...` runs.
//
// Run with:
//
//	QA_BASE_URL=https://chats.qa.example.com \
//	QA_WS_URL=wss://chats.qa.example.com/api/ws \
//	QA_JWT_SECRET=... \
//	QA_DSN="postgres://...?sslmode=require" \
//	go test -tags=qa -v ./tests/qa/...
package qa

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/HappYness-Project/chatApi/tests/qa/harness"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type messagePayload struct {
	ID       uuid.UUID `json:"id"`
	SenderID uuid.UUID `json:"sender_id"`
	Content  string    `json:"content"`
}

// TestLiveDelivery_TwoUsers exercises the whole contract end-to-end:
// connect → ready → cross-user send/receive → sender echo → negative
// isolation against an unrelated user → delete propagation.
func TestLiveDelivery_TwoUsers(t *testing.T) {
	cfg := harness.LoadCfg(t)

	alice := harness.NewUser(t, cfg)
	bob := harness.NewUser(t, cfg)
	eve := harness.NewUser(t, cfg)
	t.Cleanup(func() {
		harness.CleanupUser(t, cfg, alice.ID)
		harness.CleanupUser(t, cfg, bob.ID)
		harness.CleanupUser(t, cfg, eve.ID)
	})

	chatID := harness.CreateChat(t, cfg, alice.Token, "private")
	harness.JoinChat(t, cfg, alice.ID, chatID)
	harness.JoinChat(t, cfg, bob.ID, chatID)
	// eve is intentionally NOT joined — she's the isolation control.

	a := harness.Dial(t, cfg, alice)
	defer a.Close()
	b := harness.Dial(t, cfg, bob)
	defer b.Close()
	e := harness.Dial(t, cfg, eve)
	defer e.Close()

	require.Contains(t, a.ExpectReady(2*time.Second), chatID.String())
	require.Contains(t, b.ExpectReady(2*time.Second), chatID.String())
	require.NotContains(t, e.ExpectReady(2*time.Second), chatID.String())

	a.SendMessage(chatID, "hello bob")

	got := b.Expect("message.created", chatID.String(), 2*time.Second)
	var bp messagePayload
	require.NoError(t, json.Unmarshal(got.Payload, &bp))
	require.Equal(t, "hello bob", bp.Content)
	require.Equal(t, alice.ID, bp.SenderID)

	// Sender's own socket should see the echo too.
	a.Expect("message.created", chatID.String(), 2*time.Second)

	// Eve, not a member, must see nothing.
	e.ExpectNone(500 * time.Millisecond)

	// Now delete from alice, confirm bob sees message.deleted with the same id.
	a.DeleteMessage(chatID, bp.ID)
	del := b.Expect("message.deleted", chatID.String(), 2*time.Second)
	var dp struct {
		ID uuid.UUID `json:"id"`
	}
	require.NoError(t, json.Unmarshal(del.Payload, &dp))
	require.Equal(t, bp.ID, dp.ID)
}

// TestChatRead_AcrossDevices: same user, two sockets. Marking read on one
// device must fire chat.read on the other (badge-clear-across-devices).
func TestChatRead_AcrossDevices(t *testing.T) {
	cfg := harness.LoadCfg(t)

	alice := harness.NewUser(t, cfg)
	bob := harness.NewUser(t, cfg)
	t.Cleanup(func() {
		harness.CleanupUser(t, cfg, alice.ID)
		harness.CleanupUser(t, cfg, bob.ID)
	})

	chatID := harness.CreateChat(t, cfg, alice.Token, "private")
	harness.JoinChat(t, cfg, alice.ID, chatID)
	harness.JoinChat(t, cfg, bob.ID, chatID)

	// Alice has two devices.
	a1 := harness.Dial(t, cfg, alice)
	defer a1.Close()
	a2 := harness.Dial(t, cfg, alice)
	defer a2.Close()
	b := harness.Dial(t, cfg, bob)
	defer b.Close()
	a1.ExpectReady(2 * time.Second)
	a2.ExpectReady(2 * time.Second)
	b.ExpectReady(2 * time.Second)

	// Bob sends something for alice to read.
	b.SendMessage(chatID, "ping")
	got := a1.Expect("message.created", chatID.String(), 2*time.Second)
	var mp messagePayload
	require.NoError(t, json.Unmarshal(got.Payload, &mp))
	a2.Expect("message.created", chatID.String(), 2*time.Second)

	// Alice marks read on device 1 (via HTTP). Device 2 should hear about it.
	harness.MarkRead(t, cfg, alice.Token, chatID, mp.ID)
	a2.Expect("chat.read", chatID.String(), 2*time.Second)
}
