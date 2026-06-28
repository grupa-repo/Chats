// Package route exposes the internal HTTP endpoints the external membership
// service calls to keep live WebSocket subscriptions in sync with chat
// membership. Without this, /api/ws subscribes on connect and stays frozen —
// any chat the user is added to mid-connection is silently dead until the
// next reconnect.
package route

import (
	"encoding/json"
	"net/http"

	"github.com/grupa-repo/chats/common"
	"github.com/grupa-repo/chats/internal/ws"
	"github.com/grupa-repo/chats/loggers"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const internalTokenHeader = "X-Internal-Token"

type Handler struct {
	logger        *loggers.AppLogger
	wsManager     *ws.Manager
	internalToken string
}

func NewHandler(logger *loggers.AppLogger, wsManager *ws.Manager, internalToken string) *Handler {
	return &Handler{
		logger:        logger,
		wsManager:     wsManager,
		internalToken: internalToken,
	}
}

func (h *Handler) RegisterRoutes(router chi.Router) {
	router.Post("/api/internal/membership/resync", h.HandleResync)
}

type ResyncRequest struct {
	UserID  uuid.UUID   `json:"user_id"`
	ChatIDs []uuid.UUID `json:"chat_ids"`
}

// HandleResync is called by the membership service whenever a user's chat
// membership changes. The request body carries the user's full chat_ids set,
// which the WS manager uses to add/remove subscriptions and emit a fresh
// ready frame on each open connection. Fails closed when INTERNAL_API_TOKEN
// is unset (503) or the supplied token does not match (401).
func (h *Handler) HandleResync(w http.ResponseWriter, r *http.Request) {
	if h.internalToken == "" {
		common.ErrorResponse(w, http.StatusServiceUnavailable, common.ProblemDetails{
			Title:     "Service Unavailable",
			ErrorCode: "InternalAPIDisabled",
			Detail:    "Internal API is not configured",
		})
		return
	}
	if r.Header.Get(internalTokenHeader) != h.internalToken {
		common.ErrorResponse(w, http.StatusUnauthorized, common.ProblemDetails{
			Title:     "Unauthorized",
			ErrorCode: "InvalidInternalToken",
			Detail:    "Invalid internal token",
		})
		return
	}

	var req ResyncRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.ErrorResponse(w, http.StatusBadRequest, common.ProblemDetails{
			Title:     "Invalid Request Body",
			ErrorCode: "InvalidJSON",
			Detail:    "Unable to decode request body as JSON",
		})
		return
	}
	if req.UserID == uuid.Nil {
		common.ErrorResponse(w, http.StatusBadRequest, common.ProblemDetails{
			Title:     "Invalid Parameter",
			ErrorCode: "MissingUserID",
			Detail:    "user_id is required",
		})
		return
	}

	h.wsManager.ResyncUserSubscriptions(req.UserID, req.ChatIDs)
	w.WriteHeader(http.StatusNoContent)
}
