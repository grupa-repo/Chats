package middlewares

import (
	"context"
	"net/http"

	"github.com/grupa-repo/chats/common"
	"github.com/google/uuid"
)

func RequestIdMiddleware(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		rid := r.Header.Get("X-Request-ID")
		if rid == "" {
			rid = uuid.New().String()
		}
		ctx := context.WithValue(r.Context(), common.ContextKey(common.RequestIdentifier), rid)
		w.Header().Add("X-Request-ID", rid)

		h.ServeHTTP(w, r.WithContext(ctx))
	})
}
