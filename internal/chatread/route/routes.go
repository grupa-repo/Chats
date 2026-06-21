package route

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/HappYness-Project/chatApi/common"
	"github.com/HappYness-Project/chatApi/internal/chatread/domain"
	"github.com/HappYness-Project/chatApi/internal/chatread/repository"
	"github.com/HappYness-Project/chatApi/loggers"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/jwtauth"
	"github.com/google/uuid"
)

type Handler struct {
	logger       *loggers.AppLogger
	chatReadRepo repository.ChatReadRepo
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
	router.Get("/api/chats/unread", h.ListUnread)
	router.Get("/api/chats/reads", h.ListReads)
	router.Post("/api/chats/reads", h.BulkMarkRead)
}

const bulkMarkReadMax = 200

func (h *Handler) BulkMarkRead(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.userIDFromContext(w, r)
	if !ok {
		return
	}

	var req BulkMarkReadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.ErrorResponse(w, http.StatusBadRequest, common.ProblemDetails{
			Title:     "Invalid Request Body",
			ErrorCode: "InvalidJSON",
			Detail:    "Unable to decode request body as JSON",
		})
		return
	}
	if len(req.Reads) == 0 {
		common.ErrorResponse(w, http.StatusBadRequest, common.ProblemDetails{
			Title:     "Invalid Parameter",
			ErrorCode: "EmptyReads",
			Detail:    "reads must contain at least one entry",
		})
		return
	}
	if len(req.Reads) > bulkMarkReadMax {
		common.ErrorResponse(w, http.StatusBadRequest, common.ProblemDetails{
			Title:     "Invalid Parameter",
			ErrorCode: "TooManyReads",
			Detail:    "reads exceeds the per-request limit",
		})
		return
	}

	seen := make(map[uuid.UUID]struct{}, len(req.Reads))
	items := make([]repository.BulkReadItem, 0, len(req.Reads))
	for _, it := range req.Reads {
		if it.ChatID == uuid.Nil || it.LastReadMessageID == uuid.Nil {
			common.ErrorResponse(w, http.StatusBadRequest, common.ProblemDetails{
				Title:     "Invalid Parameter",
				ErrorCode: "MissingIDs",
				Detail:    "each entry requires chat_id and last_read_message_id",
			})
			return
		}
		if _, dup := seen[it.ChatID]; dup {
			common.ErrorResponse(w, http.StatusBadRequest, common.ProblemDetails{
				Title:     "Invalid Parameter",
				ErrorCode: "DuplicateChatID",
				Detail:    "chat_id appears more than once: " + it.ChatID.String(),
			})
			return
		}
		seen[it.ChatID] = struct{}{}
		items = append(items, repository.BulkReadItem{
			ChatID:            it.ChatID,
			LastReadMessageID: it.LastReadMessageID,
		})
	}

	reads, err := h.chatReadRepo.BulkUpsert(userID, items)
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to bulk upsert chat read markers")
		common.ErrorResponse(w, http.StatusInternalServerError, common.ProblemDetails{
			Title:  "Internal Server Error",
			Detail: "Error occurred while updating read markers",
		})
		return
	}
	if reads == nil {
		reads = []domain.ChatRead{}
	}

	common.WriteJsonWithEncode(w, http.StatusOK, ReadsListResponse{Reads: reads})
}

func (h *Handler) ListUnread(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.userIDFromContext(w, r)
	if !ok {
		return
	}

	chatIDs, err := parseChatIDsQuery(r)
	if err != nil {
		common.ErrorResponse(w, http.StatusBadRequest, common.ProblemDetails{
			Title:     "Invalid Parameter",
			ErrorCode: "InvalidChatIDs",
			Detail:    err.Error(),
		})
		return
	}

	entries, err := h.chatReadRepo.ListUnreadCounts(userID, chatIDs)
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to list unread counts")
		common.ErrorResponse(w, http.StatusInternalServerError, common.ProblemDetails{
			Title:  "Internal Server Error",
			Detail: "Error occurred while listing unread counts",
		})
		return
	}
	if entries == nil {
		entries = []repository.UnreadEntry{}
	}

	common.WriteJsonWithEncode(w, http.StatusOK, UnreadListResponse{Chats: entries})
}

func (h *Handler) ListReads(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.userIDFromContext(w, r)
	if !ok {
		return
	}

	reads, err := h.chatReadRepo.ListReads(userID)
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to list chat read markers")
		common.ErrorResponse(w, http.StatusInternalServerError, common.ProblemDetails{
			Title:  "Internal Server Error",
			Detail: "Error occurred while listing read markers",
		})
		return
	}
	if reads == nil {
		reads = []domain.ChatRead{}
	}

	common.WriteJsonWithEncode(w, http.StatusOK, ReadsListResponse{Reads: reads})
}

// parseChatIDsQuery accepts repeated ?chat_id=uuid OR ?chat_ids=uuid1,uuid2.
// Returns nil when neither is provided (caller falls back to user-scoped derivation).
func parseChatIDsQuery(r *http.Request) ([]uuid.UUID, error) {
	raw := append([]string(nil), r.URL.Query()["chat_id"]...)
	if csv := r.URL.Query().Get("chat_ids"); csv != "" {
		for p := range strings.SplitSeq(csv, ",") {
			if p = strings.TrimSpace(p); p != "" {
				raw = append(raw, p)
			}
		}
	}
	if len(raw) == 0 {
		return nil, nil
	}
	ids := make([]uuid.UUID, 0, len(raw))
	for _, s := range raw {
		id, err := uuid.Parse(s)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
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
