package route

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/grupa-repo/chats/common"
	domain "github.com/grupa-repo/chats/internal/chat/domain"
	"github.com/grupa-repo/chats/internal/chat/repository"
	"github.com/grupa-repo/chats/loggers"
	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type Handler struct {
	logger    *loggers.AppLogger
	chatRepo  repository.ChatRepo
	jwtSecret []byte
}

func NewHandler(logger *loggers.AppLogger, chatRepo repository.ChatRepo, secretKey string) *Handler {
	return &Handler{
		logger:    logger,
		chatRepo:  chatRepo,
		jwtSecret: []byte(secretKey),
	}
}

func (h *Handler) RegisterRoutes(router chi.Router) {
	router.Get("/api/chats/{chatID}", h.GetChatById)
	router.Get("/api/user-groups/{groupID}/chat", h.GetChatByGroupID)
	router.Get("/api/containers/{containerID}/chat", h.GetChatByContainerID)
	router.Post("/api/chats", h.CreateChat)
	router.Delete("/api/chats/{chatID}", h.RemoveChat)
	router.Delete("/api/user-groups/{groupID}/chat", h.RemoveChatByUserGroupId)
}
func (h *Handler) GetChatById(w http.ResponseWriter, r *http.Request) {
	chatIDStr := chi.URLParam(r, "chatID")
	if chatIDStr == "" {
		common.ErrorResponse(w, http.StatusBadRequest, common.ProblemDetails{
			Title:     "Invalid Parameter",
			ErrorCode: "MissingChatID",
			Detail:    "chatID is required",
		})
		return
	}

	chatID := uuid.MustParse(chatIDStr)

	chat, err := h.chatRepo.GetChatById(chatID)
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to retrieve chat by ID")
		common.ErrorResponse(w, http.StatusInternalServerError, common.ProblemDetails{
			Title:  "Internal Server Error",
			Detail: "Error occurred while retrieving chat",
		})
		return
	}

	if chat.Id == uuid.Nil {
		common.ErrorResponse(w, http.StatusNotFound, common.ProblemDetails{
			Title:     "Not Found",
			ErrorCode: "ChatNotFound",
			Detail:    "Chat not found with the provided ID",
		})
		return
	}

	common.WriteJsonWithEncode(w, http.StatusOK, chat)
}

func (h *Handler) GetChatByGroupID(w http.ResponseWriter, r *http.Request) {
	groupIDStr := chi.URLParam(r, "groupID")
	if groupIDStr == "" {
		http.Error(w, "groupID is required", http.StatusBadRequest)
		return
	}

	groupID, err := strconv.Atoi(groupIDStr)
	if err != nil {
		common.ErrorResponse(w, http.StatusBadRequest, common.ProblemDetails{
			Title:     "Invalid Parameter",
			ErrorCode: "Invalid Group ID",
			Detail:    "The provided groupID is not a valid integer.",
		})
		return
	}

	chat, err := h.chatRepo.GetChatByGroupID(groupID)
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to retrieve chat by groupID")
		common.ErrorResponse(w, http.StatusInternalServerError, common.ProblemDetails{
			Title:  "Internal Server Error",
			Detail: "Error occurred during getting chat by group ID",
		})
		return
	}

	if chat.Id == uuid.Nil {
		common.ErrorResponse(w, http.StatusNotFound, common.ProblemDetails{
			Title:     "Not Found",
			ErrorCode: "ChatNotFound",
			Detail:    "Chat not found for the provided group ID",
		})
		return
	}

	common.WriteJsonWithEncode(w, http.StatusOK, chat)
}

func (h *Handler) GetChatByContainerID(w http.ResponseWriter, r *http.Request) {
	containerIDStr := chi.URLParam(r, "containerID")
	if containerIDStr == "" {
		common.ErrorResponse(w, http.StatusBadRequest, common.ProblemDetails{
			Title:     "Invalid Parameter",
			ErrorCode: "MissingContainerID",
			Detail:    "containerID is required",
		})
		return
	}

	containerID, err := uuid.Parse(containerIDStr)
	if err != nil {
		common.ErrorResponse(w, http.StatusBadRequest, common.ProblemDetails{
			Title:     "Invalid Parameter",
			ErrorCode: "InvalidContainerID",
			Detail:    "The provided containerID is not a valid UUID",
		})
		return
	}

	chat, err := h.chatRepo.GetChatByContainerID(containerID)
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to retrieve chat by containerID")
		common.ErrorResponse(w, http.StatusInternalServerError, common.ProblemDetails{
			Title:  "Internal Server Error",
			Detail: "Error occurred during getting chat by container ID",
		})
		return
	}

	if chat.Id == uuid.Nil {
		common.ErrorResponse(w, http.StatusNotFound, common.ProblemDetails{
			Title:     "Not Found",
			ErrorCode: "ChatNotFound",
			Detail:    "Chat not found for the provided container ID",
		})
		return
	}

	common.WriteJsonWithEncode(w, http.StatusOK, chat)
}

func (h *Handler) CreateChat(w http.ResponseWriter, r *http.Request) {
	var request CreateChatRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		common.ErrorResponse(w, http.StatusBadRequest, common.ProblemDetails{
			Title:     "Invalid Request Body",
			ErrorCode: "InvalidJSON",
			Detail:    "Unable to decode request body as JSON",
		})
		return
	}

	chatType := domain.ChatTypeGroup
	if request.Type != "" {
		chatType = domain.ChatType(request.Type)
	}
	chat, err := domain.NewChat(chatType, request.UserGroupId, request.ContainerId)
	if err != nil {
		common.ErrorResponse(w, http.StatusBadRequest, common.ProblemDetails{
			Title:     "Invalid Request",
			ErrorCode: "InvalidChatConfiguration",
			Detail:    err.Error(),
		})
		return
	}

	var createdChat *domain.Chat

	if request.UserId != "" {
		if err != nil {
			h.logger.Error().Err(err).Msg("Failed to create chat with participant")
			common.ErrorResponse(w, http.StatusInternalServerError, common.ProblemDetails{
				Title:  "Internal Server Error",
				Detail: "Error occurred while creating chat with participant",
			})
			return
		}
	} else {
		// Create chat only
		createdChat, err = h.chatRepo.CreateChat(chat)
		if err != nil {
			h.logger.Error().Err(err).Msg("Failed to create chat")
			common.ErrorResponse(w, http.StatusInternalServerError, common.ProblemDetails{
				Title:  "Internal Server Error",
				Detail: "Error occurred while creating chat",
			})
			return
		}
	}
	h.logger.Info().Msgf("Successfully created chat with ID: %s", createdChat.Id)
	common.WriteJsonWithEncode(w, http.StatusCreated, createdChat)
}

func (h *Handler) RemoveChat(w http.ResponseWriter, r *http.Request) {
	chatIDStr := chi.URLParam(r, "chatID")
	if chatIDStr == "" {
		common.ErrorResponse(w, http.StatusBadRequest, common.ProblemDetails{
			Title:     "Invalid Parameter",
			ErrorCode: "MissingChatID",
			Detail:    "chatID is required",
		})
		return
	}

	chatID := uuid.MustParse(chatIDStr)

	chat, err := h.chatRepo.GetChatById(chatID)
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to retrieve chat by ID")
		common.ErrorResponse(w, http.StatusInternalServerError, common.ProblemDetails{
			Title:  "Internal Server Error",
			Detail: "Error occurred while retrieving chat",
		})
		return
	}

	if chat.Id == uuid.Nil {
		common.ErrorResponse(w, http.StatusNotFound, common.ProblemDetails{
			Title:     "Not Found",
			ErrorCode: "ChatNotFound",
			Detail:    "Chat not found with the provided ID",
		})
		return
	}

	err = h.chatRepo.DeleteChat(chatID)
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to delete chat")
		common.ErrorResponse(w, http.StatusInternalServerError, common.ProblemDetails{
			Title:  "Internal Server Error",
			Detail: "Error occurred while deleting chat",
		})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) RemoveChatByUserGroupId(w http.ResponseWriter, r *http.Request) {
	groupIDStr := chi.URLParam(r, "groupID")
	if groupIDStr == "" {
		common.ErrorResponse(w, http.StatusBadRequest, common.ProblemDetails{
			Title:     "Invalid Parameter",
			ErrorCode: "MissingGroupID",
			Detail:    "groupID is required",
		})
		return
	}

	groupID, err := strconv.Atoi(groupIDStr)
	if err != nil {
		common.ErrorResponse(w, http.StatusBadRequest, common.ProblemDetails{
			Title:     "Invalid Parameter",
			ErrorCode: "InvalidGroupID",
			Detail:    "The provided groupID is not a valid integer",
		})
		return
	}

	chat, err := h.chatRepo.GetChatByGroupID(groupID)
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to retrieve chat by groupID")
		common.ErrorResponse(w, http.StatusInternalServerError, common.ProblemDetails{
			Title:  "Internal Server Error",
			Detail: "Error occurred while retrieving chat",
		})
		return
	}

	if chat.Id == uuid.Nil {
		common.ErrorResponse(w, http.StatusNotFound, common.ProblemDetails{
			Title:     "Not Found",
			ErrorCode: "ChatNotFound",
			Detail:    "Chat not found for the provided group ID",
		})
		return
	}

	err = h.chatRepo.DeleteChat(chat.Id)
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to delete chat")
		common.ErrorResponse(w, http.StatusInternalServerError, common.ProblemDetails{
			Title:  "Internal Server Error",
			Detail: "Error occurred while deleting chat",
		})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) validateJWTToken(tokenString string) bool {
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
