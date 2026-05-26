package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

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
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	level := parseLogLevel(cfg.LogLevel)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
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
	router := api.NewRouter(services, db, hub, auth, logger, hubAdapter, cfg.CORSOrigin)

	return &Server{
		config:   &cfg,
		store:    db,
		hub:      hub,
		router:   router,
		services: services,
		logger:   logger,
	}, nil
}

// Run starts the server and blocks until it receives a shutdown signal.
func (s *Server) Run() error {
	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)
	s.logger.Info("starting lark server", "addr", addr)

	srv := &http.Server{
		Addr:           addr,
		Handler:        s.router,
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   60 * time.Second,
		IdleTimeout:    120 * time.Second,
		MaxHeaderBytes: 1 << 20, // 1MB
	}

	// Graceful shutdown on SIGINT/SIGTERM.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case sig := <-stop:
		s.logger.Info("shutting down", "signal", sig)
	case err := <-errCh:
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		s.logger.Error("shutdown error", "err", err)
	}
	return s.Close()
}

// Close closes the server and its resources.
func (s *Server) Close() error {
	return s.store.Close()
}

func parseLogLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
