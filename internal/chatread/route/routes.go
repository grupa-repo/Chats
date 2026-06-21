package route

import (
	"encoding/json"
	"net/http"

	"github.com/HappYness-Project/chatApi/common"
	"github.com/HappYness-Project/chatApi/internal/chatread/repository"
	"github.com/HappYness-Project/chatApi/loggers"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/jwtauth"
	"github.com/google/uuid"
)

type Handler struct {
	logger        *loggers.AppLogger
	chatReadRepo  repository.ChatReadRepo
}

func NewHandler(logger *loggers.AppLogger, chatReadRepo repository.ChatReadRepo) *Handler {
	return &Handler{
		logger:       logger,
		chatReadRepo: chatReadRepo,
	}
}

func (h *Handler) RegisterRoutes(router chi.Router) {
	router.Post("/api/chats/{chatID}/read", h.MarkRead)
	router.Get("/api/chats/{chatID}/read", h.GetRead)
	router.Get("/api/chats/{chatID}/unread-count", h.GetUnreadCount)
}

func (h *Handler) MarkRead(w http.ResponseWriter, r *http.Request) {
	chatID, ok := parseChatID(w, r)
	if !ok {
		return
	}
	userID, ok := h.userIDFromContext(w, r)
	if !ok {
		return
	}

	var req MarkReadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.ErrorResponse(w, http.StatusBadRequest, common.ProblemDetails{
			Title:     "Invalid Request Body",
			ErrorCode: "InvalidJSON",
			Detail:    "Unable to decode request body as JSON",
		})
		return
	}
	if req.LastReadMessageID == uuid.Nil {
		common.ErrorResponse(w, http.StatusBadRequest, common.ProblemDetails{
			Title:     "Invalid Parameter",
			ErrorCode: "MissingLastReadMessageID",
			Detail:    "last_read_message_id is required",
		})
		return
	}

	cr, err := h.chatReadRepo.Upsert(userID, chatID, req.LastReadMessageID)
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to upsert chat read marker")
		common.ErrorResponse(w, http.StatusInternalServerError, common.ProblemDetails{
			Title:  "Internal Server Error",
			Detail: "Error occurred while updating read marker",
		})
		return
	}

	common.WriteJsonWithEncode(w, http.StatusOK, cr)
}

func (h *Handler) GetRead(w http.ResponseWriter, r *http.Request) {
	chatID, ok := parseChatID(w, r)
	if !ok {
		return
	}
	userID, ok := h.userIDFromContext(w, r)
	if !ok {
		return
	}

	cr, err := h.chatReadRepo.Get(userID, chatID)
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to retrieve chat read marker")
		common.ErrorResponse(w, http.StatusInternalServerError, common.ProblemDetails{
			Title:  "Internal Server Error",
			Detail: "Error occurred while retrieving read marker",
		})
		return
	}
	if cr == nil {
		common.ErrorResponse(w, http.StatusNotFound, common.ProblemDetails{
			Title:     "Not Found",
			ErrorCode: "ChatReadNotFound",
			Detail:    "No read marker exists for this user and chat",
		})
		return
	}

	common.WriteJsonWithEncode(w, http.StatusOK, cr)
}

func (h *Handler) GetUnreadCount(w http.ResponseWriter, r *http.Request) {
	chatID, ok := parseChatID(w, r)
	if !ok {
		return
	}
	userID, ok := h.userIDFromContext(w, r)
	if !ok {
		return
	}

	count, err := h.chatReadRepo.UnreadCount(userID, chatID)
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to compute unread count")
		common.ErrorResponse(w, http.StatusInternalServerError, common.ProblemDetails{
			Title:  "Internal Server Error",
			Detail: "Error occurred while computing unread count",
		})
		return
	}

	common.WriteJsonWithEncode(w, http.StatusOK, UnreadCountResponse{
		ChatID: chatID,
		Count:  count,
	})
}

func parseChatID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	chatIDStr := chi.URLParam(r, "chatID")
	if chatIDStr == "" {
		common.ErrorResponse(w, http.StatusBadRequest, common.ProblemDetails{
			Title:     "Invalid Parameter",
			ErrorCode: "MissingChatID",
			Detail:    "chatID is required",
		})
		return uuid.Nil, false
	}
	chatID, err := uuid.Parse(chatIDStr)
	if err != nil {
		common.ErrorResponse(w, http.StatusBadRequest, common.ProblemDetails{
			Title:     "Invalid Parameter",
			ErrorCode: "InvalidChatID",
			Detail:    "The provided chatID is not a valid UUID",
		})
		return uuid.Nil, false
	}
	return chatID, true
}

func (h *Handler) userIDFromContext(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	_, claims, err := jwtauth.FromContext(r.Context())
	if err != nil || claims == nil {
		common.ErrorResponse(w, http.StatusUnauthorized, common.ProblemDetails{
			Title:     "Unauthorized",
			ErrorCode: "MissingAuthClaims",
			Detail:    "Authentication claims not found",
		})
		return uuid.Nil, false
	}

	for _, key := range []string{"nameid", "sub"} {
		if v, ok := claims[key].(string); ok {
			if id, err := uuid.Parse(v); err == nil {
				return id, true
			}
		}
	}

	h.logger.Error().Msg("Could not extract user ID from JWT claims")
	common.ErrorResponse(w, http.StatusUnauthorized, common.ProblemDetails{
		Title:     "Unauthorized",
		ErrorCode: "InvalidUserClaim",
		Detail:    "Could not extract user ID from token",
	})
	return uuid.Nil, false
}
