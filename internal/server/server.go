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
	"lark/internal/server/metrics"
	"lark/internal/server/service"
	"lark/internal/server/store"
	"lark/internal/server/websocket"
)

// Server is the main Lark server.
type Server struct {
	config    *Config
	store     store.Store
	hub       *websocket.Hub
	router    *api.Router
	services  *service.Services
	logger    *slog.Logger
	collector *metrics.Collector
}



// New creates a new Server.
func New(cfg Config) (*Server, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	level := parseLogLevel(cfg.LogLevel)
	var handler slog.Handler
	switch cfg.LogFormat {
	case "json":
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	default:
		handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	}
	logger := slog.New(handler)

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
	collector := metrics.NewCollector(db)
	rl := api.NewRateLimiter(cfg.RateLimit)
	hub.SetWakeCallback(func(agentID string) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		collector.RecordWake(ctx, agentID)
	})
	router := api.NewRouter(services, db, hub, auth, logger, hubAdapter, cfg.CORSOrigin, collector, rl)

	return &Server{
		config:    &cfg,
		store:     db,
		hub:       hub,
		router:    router,
		services:  services,
		logger:    logger,
		collector: collector,
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
		var err error
		if s.config.TLSCert != "" && s.config.TLSKey != "" {
			s.logger.Info("starting with TLS", "cert", s.config.TLSCert)
			err = srv.ListenAndServeTLS(s.config.TLSCert, s.config.TLSKey)
		} else {
			err = srv.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
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
	var shutdownErr error
	if err := srv.Shutdown(ctx); err != nil {
		s.logger.Error("shutdown error", "err", err)
		shutdownErr = err
	}
	if err := s.Close(); err != nil && shutdownErr == nil {
		return err
	}
	return shutdownErr
}

// Close closes the server and its resources.
func (s *Server) Close() error {
	s.hub.Close()
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
