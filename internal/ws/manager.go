package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"

	"github.com/grupa-repo/chats/internal/broadcaster"
	"github.com/grupa-repo/chats/loggers"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// UserConn wraps a raw WebSocket connection with a buffered send channel
// to serialize writes (gorilla/websocket does not support concurrent writers).
type UserConn struct {
	conn   *websocket.Conn
	send   chan broadcaster.Event
	userID uuid.UUID
}

func newUserConn(userID uuid.UUID, conn *websocket.Conn) *UserConn {
	uc := &UserConn{
		conn:   conn,
		send:   make(chan broadcaster.Event, 64),
		userID: userID,
	}
	go uc.writePump()
	return uc
}

func (uc *UserConn) writePump() {
	defer uc.conn.Close()
	for evt := range uc.send {
		if err := uc.conn.WriteJSON(evt); err != nil {
			return
		}
	}
}

func (uc *UserConn) Close() {
	close(uc.send)
}

type Manager struct {
	// Per-user connection tracking. The broadcaster owns topic→handler
	// routing; Manager keeps only the per-(user,chat) unsubscribe funcs
	// so it can release them on Unsubscribe / RemoveUserConn.
	userConns      map[uuid.UUID][]*UserConn
	userChatUnsubs map[uuid.UUID]map[uuid.UUID]func()
	Inbound        chan InboundEnvelope

	Upgrader    websocket.Upgrader
	mutex       sync.RWMutex
	logger      *loggers.AppLogger
	broadcaster broadcaster.Broadcaster
}

func NewManager(logger *loggers.AppLogger, b broadcaster.Broadcaster) *Manager {
	return &Manager{
		userConns:      make(map[uuid.UUID][]*UserConn),
		userChatUnsubs: make(map[uuid.UUID]map[uuid.UUID]func()),
		Inbound:        make(chan InboundEnvelope, 256),
		Upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
		logger:      logger,
		broadcaster: b,
	}
}

func (m *Manager) AddUserConn(userID uuid.UUID, conn *websocket.Conn) *UserConn {
	uc := newUserConn(userID, conn)
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.userConns[userID] = append(m.userConns[userID], uc)
	m.logger.Info().Str("userID", userID.String()).Msg("User connection added")
	return uc
}

func (m *Manager) RemoveUserConn(userID uuid.UUID, uc *UserConn) {
	m.mutex.Lock()
	var unsubs []func()
	conns := m.userConns[userID]
	for i, c := range conns {
		if c == uc {
			m.userConns[userID] = append(conns[:i], conns[i+1:]...)
			break
		}
	}
	if len(m.userConns[userID]) == 0 {
		delete(m.userConns, userID)
		for _, unsub := range m.userChatUnsubs[userID] {
			unsubs = append(unsubs, unsub)
		}
		delete(m.userChatUnsubs, userID)
	}
	m.mutex.Unlock()

	for _, unsub := range unsubs {
		unsub()
	}

	uc.Close()
	m.logger.Info().Str("userID", userID.String()).Msg("User connection removed")
}

// Subscribe registers user for chat events via the broadcaster. Idempotent:
// re-subscribing the same (user, chat) is a no-op.
func (m *Manager) Subscribe(userID, chatID uuid.UUID) {
	m.mutex.Lock()
	if m.userChatUnsubs[userID] == nil {
		m.userChatUnsubs[userID] = make(map[uuid.UUID]func())
	}
	if _, exists := m.userChatUnsubs[userID][chatID]; exists {
		m.mutex.Unlock()
		return
	}
	// Reserve the slot so a concurrent Subscribe doesn't double-register.
	m.userChatUnsubs[userID][chatID] = func() {}
	m.mutex.Unlock()

	unsub := m.broadcaster.Subscribe(broadcaster.ChatTopic(chatID), func(evt broadcaster.Event) {
		m.sendToUserConns(userID, evt)
	})

	m.mutex.Lock()
	m.userChatUnsubs[userID][chatID] = unsub
	m.mutex.Unlock()

	m.logger.Info().
		Str("userID", userID.String()).
		Str("chatID", chatID.String()).
		Msg("User subscribed to chat")
}

func (m *Manager) Unsubscribe(userID, chatID uuid.UUID) {
	m.mutex.Lock()
	unsub, ok := m.userChatUnsubs[userID][chatID]
	if ok {
		delete(m.userChatUnsubs[userID], chatID)
		if len(m.userChatUnsubs[userID]) == 0 {
			delete(m.userChatUnsubs, userID)
		}
	}
	m.mutex.Unlock()

	if unsub != nil {
		unsub()
	}

	m.logger.Info().
		Str("userID", userID.String()).
		Str("chatID", chatID.String()).
		Msg("User unsubscribed from chat")
}

// ResyncUserSubscriptions reconciles the live subscription set for userID to
// match chatIDs and emits a fresh ready frame to each of the user's open
// connections. Intended for use by the membership service after it adds or
// removes a user from a chat — without this the connection's subscription set
// is frozen at connect time and events for newly-joined chats never arrive.
// No-op when the user has no open connections.
func (m *Manager) ResyncUserSubscriptions(userID uuid.UUID, chatIDs []uuid.UUID) {
	m.mutex.RLock()
	hasConn := len(m.userConns[userID]) > 0
	current := make(map[uuid.UUID]struct{}, len(m.userChatUnsubs[userID]))
	for c := range m.userChatUnsubs[userID] {
		current[c] = struct{}{}
	}
	m.mutex.RUnlock()

	if !hasConn {
		return
	}

	desired := make(map[uuid.UUID]struct{}, len(chatIDs))
	for _, c := range chatIDs {
		desired[c] = struct{}{}
	}

	for c := range desired {
		if _, exists := current[c]; !exists {
			m.Subscribe(userID, c)
		}
	}
	for c := range current {
		if _, keep := desired[c]; !keep {
			m.Unsubscribe(userID, c)
		}
	}

	chatIDStrs := make([]string, 0, len(chatIDs))
	for _, c := range chatIDs {
		chatIDStrs = append(chatIDStrs, c.String())
	}
	payload, err := json.Marshal(ReadyPayload{ChatIDs: chatIDStrs})
	if err != nil {
		m.logger.Error().Err(err).Msg("Failed to marshal ready payload on resync")
		return
	}
	m.SendToUser(userID, broadcaster.Event{
		Type:    EventReady,
		Payload: payload,
	})

	m.logger.Info().
		Str("userID", userID.String()).
		Int("chats", len(chatIDs)).
		Msg("Resynced user subscriptions")
}

func (m *Manager) IsSubscribed(userID, chatID uuid.UUID) bool {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	if subs, ok := m.userChatUnsubs[userID]; ok {
		_, subscribed := subs[chatID]
		return subscribed
	}
	return false
}

// Publish fans out an event to all subscribers of a chat via the broadcaster.
// Use this from HTTP/WS write paths after a successful persistence.
func (m *Manager) Publish(ctx context.Context, chatID uuid.UUID, evt broadcaster.Event) error {
	if evt.ChatID == "" {
		evt.ChatID = chatID.String()
	}
	return m.broadcaster.Publish(ctx, broadcaster.ChatTopic(chatID), evt)
}

// SendToUser enqueues an event directly to one user's connections (not via
// the broadcaster). Use for connection-scoped events like acks and errors.
func (m *Manager) SendToUser(userID uuid.UUID, evt broadcaster.Event) {
	m.sendToUserConns(userID, evt)
}

func (m *Manager) sendToUserConns(userID uuid.UUID, evt broadcaster.Event) {
	m.mutex.RLock()
	conns := m.userConns[userID]
	connsCopy := make([]*UserConn, len(conns))
	copy(connsCopy, conns)
	m.mutex.RUnlock()

	for _, uc := range connsCopy {
		select {
		case uc.send <- evt:
		default:
			// Outbound buffer full: close the socket so the client
			// reconnects and re-syncs via GET /chats/unread. Dropping
			// events would leave the client silently out of date.
			m.logger.Error().
				Str("userID", userID.String()).
				Msg("User send buffer full, closing connection to force re-sync")
			_ = uc.conn.Close()
		}
	}
}

func (m *Manager) EnqueueInbound(env InboundEnvelope) {
	select {
	case m.Inbound <- env:
	default:
		m.logger.Error().Msg("Inbound channel full, dropping message")
	}
}

func (m *Manager) SendErrorToUser(userID uuid.UUID, errMsg string) {
	payload, _ := json.Marshal(ErrorPayload{Error: errMsg})
	m.SendToUser(userID, broadcaster.Event{
		Type:    EventError,
		Payload: payload,
	})
}
