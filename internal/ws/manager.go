package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/HappYness-Project/chatApi/internal/broadcaster"
	domain "github.com/HappYness-Project/chatApi/internal/message/domain"
	"github.com/HappYness-Project/chatApi/loggers"
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
	// Legacy per-chat connection tracking (kept for backward compat with old endpoint)
	Clients   map[uuid.UUID][]*websocket.Conn
	Broadcast chan domain.Message

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
		Clients:        make(map[uuid.UUID][]*websocket.Conn),
		Broadcast:      make(chan domain.Message, 256),
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

// --- Legacy per-chat methods (old endpoint) ---

func (m *Manager) AddClient(chatID uuid.UUID, conn *websocket.Conn) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.Clients[chatID] = append(m.Clients[chatID], conn)
	m.logger.Info().Str("chatID", chatID.String()).Msg("New client connected")
}

func (m *Manager) RemoveClient(chatID uuid.UUID, conn *websocket.Conn) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	conns := m.Clients[chatID]
	for i, c := range conns {
		if c == conn {
			m.Clients[chatID] = append(conns[:i], conns[i+1:]...)
			break
		}
	}
	if len(m.Clients[chatID]) == 0 {
		delete(m.Clients, chatID)
	}
	conn.Close()
	m.logger.Info().Str("chatID", chatID.String()).Msg("Client disconnected")
}

func (m *Manager) BroadcastMessage(msg domain.Message) {
	select {
	case m.Broadcast <- msg:
	default:
		fmt.Println("Broadcast channel full, dropping message")
	}
}

func (m *Manager) SendToClients(msg domain.Message, logger *loggers.AppLogger) {
	m.mutex.RLock()
	conns := m.Clients[msg.ChatID]
	clientsCopy := make([]*websocket.Conn, len(conns))
	copy(clientsCopy, conns)
	m.mutex.RUnlock()

	for _, client := range clientsCopy {
		err := client.WriteJSON(msg)
		if err != nil {
			logger.Error().Err(err).Msg("Unable to write a message")
			m.RemoveClient(msg.ChatID, client)
		}
	}
}

// --- Per-user connection methods (new endpoint) ---

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
			m.logger.Error().Str("userID", userID.String()).Msg("User send buffer full, dropping event")
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
