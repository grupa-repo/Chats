package api

import (
	"database/sql"
	"log"
	"net/http"
	"os"

	"github.com/HappYness-Project/chatApi/common"
	chatRepo "github.com/HappYness-Project/chatApi/internal/chat/repository"
	chatRoute "github.com/HappYness-Project/chatApi/internal/chat/route"
	chatReadRepo "github.com/HappYness-Project/chatApi/internal/chatread/repository"
	chatReadRoute "github.com/HappYness-Project/chatApi/internal/chatread/route"
	messageRepo "github.com/HappYness-Project/chatApi/internal/message/repository"
	messageRoute "github.com/HappYness-Project/chatApi/internal/message/route"
	"github.com/HappYness-Project/chatApi/internal/ws"

	"github.com/HappYness-Project/chatApi/loggers"
	"github.com/HappYness-Project/chatApi/middlewares"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/jwtauth"
)

type ApiServer struct {
	addr      string
	secretKey string
	db        *sql.DB
	tokenAuth *jwtauth.JWTAuth
	logger    *loggers.AppLogger
}

func NewApiServer(addr string, secretKey string, db *sql.DB, logger *loggers.AppLogger) *ApiServer {
	tokenAuth := jwtauth.New("HS512", []byte(secretKey), nil)
	return &ApiServer{
		addr:      addr,
		secretKey: secretKey,
		db:        db,
		tokenAuth: tokenAuth,
		logger:    logger,
	}
}

func (s *ApiServer) Setup() *chi.Mux {
	mux := chi.NewRouter()

	msgRepo := messageRepo.NewRepository(s.db)
	chatRepo := chatRepo.NewRepository(s.db)
	chatReadRepo := chatReadRepo.NewRepository(s.db)

	mux.Use(middleware.Logger)
	mux.Use(middleware.Recoverer)
	mux.Use(middlewares.RequestIdMiddleware)
	mux.Use(middleware.Heartbeat("/ping"))

	mux.Get("/", Home)
	mux.Get("/health", Home)
	mux.Get("/version", Version)
	wsManager := ws.NewManager(s.logger)
	msgHandler := messageRoute.NewHandler(s.logger, *msgRepo, *chatRepo, s.secretKey, wsManager)
	chatHandler := chatRoute.NewHandler(s.logger, *chatRepo, s.secretKey)
	chatReadHandler := chatReadRoute.NewHandler(s.logger, *chatReadRepo)

	mux.Get("/api/chats/{chatID}/ws", msgHandler.HandleConnectionsByChatID)
	mux.Get("/api/ws", msgHandler.HandleUserConnection)
	mux.Group(func(r chi.Router) {
		r.Use(jwtauth.Verifier(s.tokenAuth))
		r.Use(jwtauth.Authenticator)

		chatHandler.RegisterRoutes(r)
		msgHandler.RegisterRoutes(r)
		chatReadHandler.RegisterRoutes(r)
	})
	go msgHandler.HandleMessages()
	go msgHandler.HandleUserMessages()
	return mux
}

func (s *ApiServer) Run(mux *chi.Mux) error {
	log.Println("Listening on ", s.addr)
	return http.ListenAndServe(s.addr, mux)
}
func Home(w http.ResponseWriter, r *http.Request) {
	var payload = struct {
		Status  string `json:"status"`
		Message string `json:"message"`
		Version string `json:"version"`
	}{
		Status:  "active",
		Message: "Message Service server",
		Version: deployedVersion(),
	}
	common.WriteJsonWithEncode(w, http.StatusOK, payload)
}

func Version(w http.ResponseWriter, r *http.Request) {
	var payload = struct {
		Service string `json:"service"`
		Version string `json:"version"`
		Env     string `json:"env"`
	}{
		Service: "chats",
		Version: deployedVersion(),
		Env:     os.Getenv("APP_ENV"),
	}
	common.WriteJsonWithEncode(w, http.StatusOK, payload)
}

// deployedVersion returns the commit SHA of the running build.
// Heroku injects SOURCE_VERSION automatically; falls back to "dev" for local runs.
func deployedVersion() string {
	if v := os.Getenv("SOURCE_VERSION"); v != "" {
		return v
	}
	return "dev"
}
