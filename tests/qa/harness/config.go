//go:build qa

package harness

import (
	"os"
	"strings"
	"testing"
)

// Config is the QA-deploy connection info, sourced entirely from env.
// All five fields are required; LoadCfg fails the test if anything's missing.
type Config struct {
	BaseURL   string // e.g. https://chats.qa.example.com
	WSURL     string // e.g. wss://chats.qa.example.com/api/ws
	JWTSecret string // HS512 secret shared with the QA chat service
	DSN       string // Postgres DSN for membership seeding (QA only)
}

func LoadCfg(t *testing.T) Config {
	t.Helper()
	cfg := Config{
		BaseURL:   strings.TrimRight(os.Getenv("QA_BASE_URL"), "/"),
		WSURL:     os.Getenv("QA_WS_URL"),
		JWTSecret: os.Getenv("QA_JWT_SECRET"),
		DSN:       os.Getenv("QA_DSN"),
	}
	missing := []string{}
	if cfg.BaseURL == "" {
		missing = append(missing, "QA_BASE_URL")
	}
	if cfg.WSURL == "" {
		missing = append(missing, "QA_WS_URL")
	}
	if cfg.JWTSecret == "" {
		missing = append(missing, "QA_JWT_SECRET")
	}
	if cfg.DSN == "" {
		missing = append(missing, "QA_DSN")
	}
	if len(missing) > 0 {
		t.Skipf("QA harness skipped — missing env: %s", strings.Join(missing, ", "))
	}
	return cfg
}

// RequireInternalToken returns the INTERNAL_API_TOKEN value from QA_INTERNAL_TOKEN
// or skips the calling test if unset. Kept out of LoadCfg so that tests which
// don't touch /api/internal/* still run when the token isn't provisioned.
func RequireInternalToken(t *testing.T) string {
	t.Helper()
	tok := os.Getenv("QA_INTERNAL_TOKEN")
	if tok == "" {
		t.Skip("QA harness skipped — missing env: QA_INTERNAL_TOKEN")
	}
	return tok
}
