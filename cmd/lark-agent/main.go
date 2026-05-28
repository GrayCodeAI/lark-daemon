package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"lark-daemon/internal/agent"
)

func main() {
	var (
		serverURL = flag.String("server", envOr("LARK_SERVER", "http://localhost:4001"), "Lark daemon URL")
		apiKey    = flag.String("api-key", envOr("LARK_API_KEY", ""), "Agent API key (lr_...)")
		name      = flag.String("name", envOr("LARK_AGENT_NAME", "lark-agent"), "Agent display name")
		logLevel  = flag.String("log-level", envOr("LARK_LOG_LEVEL", "info"), "Log level: debug, info, warn, error")
		detect    = flag.Bool("detect", false, "Detect installed runtimes and exit")
	)
	flag.Parse()

	// Setup logger
	level := slog.LevelInfo
	switch *logLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	// Detect mode
	if *detect {
		ctx := context.Background()
		detected := agent.DetectRuntimes(ctx)
		fmt.Print(agent.RuntimeSummary(detected))
		return
	}

	// Validate
	if *apiKey == "" {
		fmt.Fprintln(os.Stderr, "Error: --api-key or LARK_API_KEY is required")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Usage:")
		fmt.Fprintln(os.Stderr, "  lark-agent --server http://localhost:4001 --api-key lr_...")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Or set environment variables:")
		fmt.Fprintln(os.Stderr, "  LARK_SERVER, LARK_API_KEY, LARK_AGENT_NAME")
		os.Exit(1)
	}

	// Detect runtimes first
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	detected := agent.DetectRuntimes(ctx)
	if len(detected) == 0 {
		logger.Error("no agent runtimes detected — install one of: claude, codex, gemini, kimi")
		os.Exit(1)
	}

	// Create and run bridge
	bridge := agent.NewBridge(*serverURL, *apiKey, *name, logger)
	logger.Info("starting agent bridge",
		"server", *serverURL,
		"name", *name,
		"runtimes", len(detected),
	)

	if err := bridge.Run(ctx); err != nil {
		logger.Error("bridge exited", "err", err)
		os.Exit(1)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
