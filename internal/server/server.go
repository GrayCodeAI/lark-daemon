package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"lark/internal/server/api"
	"lark/internal/server/service"
	"lark/internal/server/store"
	"lark/internal/server/websocket"
)

// Server is the main Lark server.
type Server struct {
	config   *Config
	store    store.Store
	hub      *websocket.Hub
	router   *api.Router
	services *service.Services
	logger   *slog.Logger
}

// New creates a new Server.
func New(cfg Config) (*Server, error) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	db, err := store.NewSQLiteStore(cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	services := service.NewServices(db)
	hub := websocket.NewHub()
	hubAdapter := NewHubStoreAdapter(db)
	hub.SetStore(hubAdapter)
	auth := websocket.NewAuthService(cfg.JWTSecret)
	router := api.NewRouter(services, db, hub, auth, logger, hubAdapter)

	return &Server{
		config:   &cfg,
		store:    db,
		hub:      hub,
		router:   router,
		services: services,
		logger:   logger,
	}, nil
}

// Run starts the server and blocks until it exits.
func (s *Server) Run() error {
	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)
	s.logger.Info("starting lark server", "addr", addr)
	return http.ListenAndServe(addr, s.router)
}

// Close closes the server and its resources.
func (s *Server) Close() error {
	return s.store.Close()
}
