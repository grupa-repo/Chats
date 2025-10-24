package route

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/HappYness-Project/chatApi/common"
	domain "github.com/HappYness-Project/chatApi/internal/message/domain"
	"github.com/HappYness-Project/chatApi/loggers"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	chatRepo "github.com/HappYness-Project/chatApi/internal/chat/repository"
	msgRepo "github.com/HappYness-Project/chatApi/internal/message/repository"
	"github.com/go-chi/chi/v5"
)

type Handler struct {
	logger      *loggers.AppLogger
	messageRepo msgRepo.MessageRepo
	chatRepo    chatRepo.ChatRepo
	wsManager   *WebSocketManager
	jwtSecret   []byte
}

func NewHandler(logger *loggers.AppLogger, repo msgRepo.MessageRepo, chatRepo chatRepo.ChatRepo, secretKey string) *Handler {
	wsManager := NewWebSocketManager(logger)
	handler := &Handler{
		logger:      logger,
		messageRepo: repo,
		chatRepo:    chatRepo,
		wsManager:   wsManager,
		jwtSecret:   []byte(secretKey),
	}
	go handler.HandleMessages()
	return handler
}

func (h *Handler) RegisterRoutes(router chi.Router) {
	router.Get("/api/chats/{chatID}/messages", h.GetMessagesByChatID)
}

func (h *Handler) HandleConnectionsByChatID(w http.ResponseWriter, r *http.Request) {
	if !h.authenticateRequest(w, r) {
		common.ErrorResponse(w, http.StatusUnauthorized, common.ProblemDetails{
			Title:     "Unauthorized",
			ErrorCode: "AuthenticationFailure",
			Detail:    "Invalid authentication token",
		})
		return
	}

	chatIDStr := chi.URLParam(r, "chatID")
	if chatIDStr == "" {
		h.logger.Error().Msg("chatID is required")
		http.Error(w, "chatID is required", http.StatusBadRequest)
		return
	}

	chatID := uuid.MustParse(chatIDStr)

	conn, err := h.wsManager.upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Error().Err(err).Msg(err.Error())
		return
	}
	defer h.wsManager.RemoveClient(conn)

	// Set up Pong handler to respond to client Pings automatically
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	})

	h.wsManager.AddClient(conn)
	chat, err := h.chatRepo.GetChatById(chatID)
	if err != nil {
		h.logger.Error().Err(err).Msg("Error occurred during getting chat by user group. " + err.Error())
		return
	}

	for {
		var msg domain.Message
		err := conn.ReadJSON(&msg)
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				h.logger.Info().Msg("Client disconnected normally")
				return
			}
			if websocket.IsUnexpectedCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				h.logger.Error().Err(err).Msg("Unexpected websocket close")
				return
			} else {
				h.logger.Error().Err(err).Msg("Error reading websocket message")
				continue // Skip this iteration instead of returning
			}
		}

		// Skip empty messages or messages without content (likely ping/control messages)
		if msg.Content == "" {
			continue
		}

		msg.ChatID = chat.Id
		msg.CreatedAt = time.Now().UTC()
		msg.MessageType = "text"

		h.wsManager.BroadcastMessage(msg)
	}
}

func (h *Handler) HandleMessages() {
	for {
		msg := <-h.wsManager.broadcast
		id, _ := uuid.NewV7()
		msg.ID = id
		if err := h.messageRepo.Create(msg); err != nil {
			h.logger.Error().Err(err).Msg("Unable to create a message")
			continue
		}
		fmt.Printf("Broadcasting message to %d clients\n", len(h.wsManager.clients))
		fmt.Printf("[ChatID:%s]|[SenderID:%s]|Message: %s\n", msg.ChatID, msg.SenderID, msg.Content)
		fmt.Println("-------------------------------------------------------------")
		h.wsManager.SendToClients(msg, h.logger)
	}
}

func (h *Handler) GetMessagesByChatID(w http.ResponseWriter, r *http.Request) {
	chatIDStr := chi.URLParam(r, "chatID")
	if chatIDStr == "" {
		http.Error(w, "chatID is required", http.StatusBadRequest)
		return
	}
	chatID := uuid.MustParse(chatIDStr)

	limitStr := r.URL.Query().Get("limit")
	limit := 120
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	offsetStr := r.URL.Query().Get("offset")
	offset := 0
	if offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	messages, err := h.messageRepo.GetByChatID(chatID, limit, offset)
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to retrieve messages by chatID")
		common.ErrorResponse(w, http.StatusInternalServerError, common.ProblemDetails{
			Title:  "Internal Server Error",
			Detail: "Error occurred during getting chat ID",
		})
		return
	}

	common.WriteJsonWithEncode(w, http.StatusOK, map[string]interface{}{
		"messages": messages,
		"count":    len(messages),
	})
}

func (h *Handler) authenticateRequest(_ http.ResponseWriter, r *http.Request) bool {
	token := r.URL.Query().Get("token")
	if token == "" {
		h.logger.Error().Msg("Missing jwt token")
		return false
	}

	if !h.validateJWTToken(token) {
		h.logger.Error().Msg("Invalid jwt token")
		return false
	}
	return true
}

func (h *Handler) validateJWTToken(tokenString string) bool {
	// TODO: Implement real JWT validation
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if token.Method != jwt.SigningMethodHS512 {
			return nil, fmt.Errorf("unexpected signing method: %v, expected HS512", token.Header["alg"])
		}
		return h.jwtSecret, nil
	})

	if err != nil {
		h.logger.Error().Err(err).Msg("JWT parsing error")
		return false
	}

	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		h.logger.Info().Interface("claims", claims).Msg("JWT token validated successfully")
		return true
	}

	h.logger.Error().Msg("Invalid JWT token claims")
	return false
}
