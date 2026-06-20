package ws

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	domain "github.com/HappYness-Project/chatApi/internal/message/domain"
	"github.com/HappYness-Project/chatApi/loggers"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// UserConn wraps a raw WebSocket connection with a buffered send channel
// to serialize writes (gorilla/websocket does not support concurrent writers).
type UserConn struct {
	conn   *websocket.Conn
	send   chan WSOutbound
	userID uuid.UUID
}

func newUserConn(userID uuid.UUID, conn *websocket.Conn) *UserConn {
	uc := &UserConn{
		conn:   conn,
		send:   make(chan WSOutbound, 64),
		userID: userID,
	}
	go uc.writePump()
	return uc
}

func (uc *UserConn) writePump() {
	defer uc.conn.Close()
	for msg := range uc.send {
		if err := uc.conn.WriteJSON(msg); err != nil {
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

	// Per-user connection tracking
	userConns         map[uuid.UUID][]*UserConn
	chatSubscribers   map[uuid.UUID]map[uuid.UUID]struct{} // chatID → set of userIDs
	userSubscriptions map[uuid.UUID]map[uuid.UUID]struct{} // userID → set of chatIDs
	Inbound           chan InboundEnvelope

	Upgrader websocket.Upgrader
	mutex    sync.RWMutex
	logger   *loggers.AppLogger
}

func NewManager(logger *loggers.AppLogger) *Manager {
	return &Manager{
		Clients:           make(map[uuid.UUID][]*websocket.Conn),
		Broadcast:         make(chan domain.Message, 256),
		userConns:         make(map[uuid.UUID][]*UserConn),
		chatSubscribers:   make(map[uuid.UUID]map[uuid.UUID]struct{}),
		userSubscriptions: make(map[uuid.UUID]map[uuid.UUID]struct{}),
		Inbound:           make(chan InboundEnvelope, 256),
		Upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
		logger: logger,
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
	defer m.mutex.Unlock()

	conns := m.userConns[userID]
	for i, c := range conns {
		if c == uc {
			m.userConns[userID] = append(conns[:i], conns[i+1:]...)
			break
		}
	}
	if len(m.userConns[userID]) == 0 {
		delete(m.userConns, userID)
		for chatID := range m.userSubscriptions[userID] {
			delete(m.chatSubscribers[chatID], userID)
			if len(m.chatSubscribers[chatID]) == 0 {
				delete(m.chatSubscribers, chatID)
			}
		}
		delete(m.userSubscriptions, userID)
	}

	uc.Close()
	m.logger.Info().Str("userID", userID.String()).Msg("User connection removed")
}

func (m *Manager) Subscribe(userID, chatID uuid.UUID) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if m.chatSubscribers[chatID] == nil {
		m.chatSubscribers[chatID] = make(map[uuid.UUID]struct{})
	}
	m.chatSubscribers[chatID][userID] = struct{}{}

	if m.userSubscriptions[userID] == nil {
		m.userSubscriptions[userID] = make(map[uuid.UUID]struct{})
	}
	m.userSubscriptions[userID][chatID] = struct{}{}

	m.logger.Info().
		Str("userID", userID.String()).
		Str("chatID", chatID.String()).
		Msg("User subscribed to chat")
}

func (m *Manager) Unsubscribe(userID, chatID uuid.UUID) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	delete(m.chatSubscribers[chatID], userID)
	if len(m.chatSubscribers[chatID]) == 0 {
		delete(m.chatSubscribers, chatID)
	}

	delete(m.userSubscriptions[userID], chatID)
	if len(m.userSubscriptions[userID]) == 0 {
		delete(m.userSubscriptions, userID)
	}

	m.logger.Info().
		Str("userID", userID.String()).
		Str("chatID", chatID.String()).
		Msg("User unsubscribed from chat")
}

func (m *Manager) IsSubscribed(userID, chatID uuid.UUID) bool {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	if subs, ok := m.userSubscriptions[userID]; ok {
		_, subscribed := subs[chatID]
		return subscribed
	}
	return false
}

func (m *Manager) SendToChat(chatID uuid.UUID, msg WSOutbound) {
	m.mutex.RLock()
	subscribers := make([]uuid.UUID, 0, len(m.chatSubscribers[chatID]))
	for uid := range m.chatSubscribers[chatID] {
		subscribers = append(subscribers, uid)
	}
	m.mutex.RUnlock()

	for _, uid := range subscribers {
		m.sendToUserConns(uid, msg)
	}
}

func (m *Manager) SendToUser(userID uuid.UUID, msg WSOutbound) {
	m.sendToUserConns(userID, msg)
}

func (m *Manager) sendToUserConns(userID uuid.UUID, msg WSOutbound) {
	m.mutex.RLock()
	conns := m.userConns[userID]
	connsCopy := make([]*UserConn, len(conns))
	copy(connsCopy, conns)
	m.mutex.RUnlock()

	for _, uc := range connsCopy {
		select {
		case uc.send <- msg:
		default:
			m.logger.Error().Str("userID", userID.String()).Msg("User send buffer full, dropping message")
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
	m.SendToUser(userID, WSOutbound{
		Event:     "error",
		Error:     errMsg,
		Timestamp: time.Now().UTC(),
	})
}
