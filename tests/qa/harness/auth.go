//go:build qa

package harness

import (
	"crypto/hmac"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

// User is a fresh synthetic identity for one test run.
type User struct {
	ID    uuid.UUID
	Token string
}

// NewUser mints a UUID and signs an HS512 JWT for it with the shared QA secret.
// 1h expiry — well past any reasonable test runtime.
func NewUser(t *testing.T, cfg Config) User {
	t.Helper()
	id := uuid.New()
	return User{ID: id, Token: signJWT(t, cfg.JWTSecret, id)}
}

func signJWT(t *testing.T, secret string, userID uuid.UUID) string {
	t.Helper()
	header, _ := json.Marshal(map[string]string{"alg": "HS512", "typ": "JWT"})
	payload, _ := json.Marshal(map[string]any{
		"nameid": userID.String(),
		"exp":    time.Now().Add(time.Hour).Unix(),
	})
	signingInput := b64url(header) + "." + b64url(payload)
	mac := hmac.New(sha512.New, []byte(secret))
	mac.Write([]byte(signingInput))
	return signingInput + "." + b64url(mac.Sum(nil))
}

func b64url(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}
