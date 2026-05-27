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

	"golang.org/x/oauth2"

	"lark-daemon/internal/server/api"
	"lark-daemon/internal/server/metrics"
	"lark-daemon/internal/server/service"
	"lark-daemon/internal/server/storage"
	"lark-daemon/internal/server/store"
	"lark-daemon/internal/server/websocket"
)

// Server is the main Lark server.
type Server struct {
	config      *Config
	store       store.Store
	hub         *websocket.Hub
	router      *api.Router
	services    *service.Services
	logger      *slog.Logger
	collector   *metrics.Collector
	rateLimiter *api.RateLimiter
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
	hub.SetAllowedOrigin(cfg.CORSOrigin)
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
	storeBackend, err := storage.NewStore(storage.Config{
		Type:       cfg.StorageType,
		LocalDir:   filepath.Join(cfg.DataDir, "files"),
		S3Bucket:   cfg.S3Bucket,
		S3Region:   cfg.S3Region,
		S3Endpoint: cfg.S3Endpoint,
		S3Key:      cfg.S3Key,
		S3Secret:   cfg.S3Secret,
	})
	if err != nil {
		return nil, fmt.Errorf("storage init: %w", err)
	}
	scheme := "http"
	if cfg.TLSCert != "" {
		scheme = "https"
	}

	var githubOAuthCfg *oauth2.Config
	if cfg.GithubClientID != "" && cfg.GithubSecret != "" {
		githubOAuthCfg = &oauth2.Config{
			ClientID:     cfg.GithubClientID,
			ClientSecret: cfg.GithubSecret,
			Endpoint: oauth2.Endpoint{
				AuthURL:  "https://github.com/login/oauth/authorize",
				TokenURL: "https://github.com/login/oauth/access_token",
			},
			RedirectURL: fmt.Sprintf("%s://%s:%d/v1/auth/github/callback", scheme, cfg.Host, cfg.Port),
			Scopes:      []string{"read:user", "user:email"},
		}
	}

	var googleOAuthCfg *oauth2.Config
	if cfg.GoogleClientID != "" && cfg.GoogleSecret != "" {
		googleOAuthCfg = &oauth2.Config{
			ClientID:     cfg.GoogleClientID,
			ClientSecret: cfg.GoogleSecret,
			Endpoint: oauth2.Endpoint{
				AuthURL:  "https://accounts.google.com/o/oauth2/v2/auth",
				TokenURL: "https://oauth2.googleapis.com/token",
			},
			RedirectURL: fmt.Sprintf("%s://%s:%d/v1/auth/google/callback", scheme, cfg.Host, cfg.Port),
			Scopes:      []string{"openid", "email", "profile"},
		}
	}

	var microsoftOAuthCfg *oauth2.Config
	if cfg.MicrosoftClientID != "" && cfg.MicrosoftSecret != "" {
		microsoftOAuthCfg = &oauth2.Config{
			ClientID:     cfg.MicrosoftClientID,
			ClientSecret: cfg.MicrosoftSecret,
			Endpoint: oauth2.Endpoint{
				AuthURL:  "https://login.microsoftonline.com/common/oauth2/v2.0/authorize",
				TokenURL: "https://login.microsoftonline.com/common/oauth2/v2.0/token",
			},
			RedirectURL: fmt.Sprintf("%s://%s:%d/v1/auth/microsoft/callback", scheme, cfg.Host, cfg.Port),
			Scopes:      []string{"openid", "email", "profile"},
		}
	}

	router := api.NewRouter(services, db, hub, auth, logger, hubAdapter, cfg.CORSOrigin, collector, rl, storeBackend, githubOAuthCfg, googleOAuthCfg, microsoftOAuthCfg)

	return &Server{
		config:      &cfg,
		store:       db,
		hub:         hub,
		router:      router,
		services:    services,
		logger:      logger,
		collector:   collector,
		rateLimiter: rl,
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
	s.rateLimiter.Stop()
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
