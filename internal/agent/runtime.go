package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// RuntimeType identifies a supported agent CLI.
type RuntimeType string

const (
	RuntimeClaude RuntimeType = "claude"
	RuntimeCodex  RuntimeType = "codex"
	RuntimeGemini RuntimeType = "gemini"
	RuntimeKimi   RuntimeType = "kimi"
)

// RuntimeDef describes a supported agent CLI runtime.
type RuntimeDef struct {
	Type        RuntimeType
	Binary      string
	Description string
	Protocol    string // stream-json, process-per-turn, jsonrpc
	Provider    string
}

// SupportedRuntimes is the list of all supported agent CLIs, in detection order.
var SupportedRuntimes = []RuntimeDef{
	{
		Type:        RuntimeClaude,
		Binary:      "claude",
		Description: "Claude Code (Anthropic)",
		Protocol:    "stream-json",
		Provider:    "anthropic",
	},
	{
		Type:        RuntimeCodex,
		Binary:      "codex",
		Description: "Codex CLI (OpenAI)",
		Protocol:    "process-per-turn",
		Provider:    "openai",
	},
	{
		Type:        RuntimeGemini,
		Binary:      "gemini",
		Description: "Gemini CLI (Google)",
		Protocol:    "process-per-turn",
		Provider:    "google",
	},
	{
		Type:        RuntimeKimi,
		Binary:      "kimi",
		Description: "Kimi CLI (Moonshot)",
		Protocol:    "jsonrpc",
		Provider:    "moonshot",
	},
}

// DetectedRuntime holds info about a detected CLI runtime.
type DetectedRuntime struct {
	RuntimeDef
	Path    string // absolute path to binary
	Version string // version string if available
}

// DetectRuntimes scans the system for installed agent CLIs.
func DetectRuntimes(ctx context.Context) []DetectedRuntime {
	var detected []DetectedRuntime
	for _, def := range SupportedRuntimes {
		binary := def.Binary
		if runtime.GOOS == "windows" {
			binary += ".exe"
		}
		path, err := exec.LookPath(binary)
		if err != nil {
			continue
		}
		dr := DetectedRuntime{
			RuntimeDef: def,
			Path:       path,
		}
		// Try to get version
		dr.Version = detectVersion(ctx, path)
		detected = append(detected, dr)
		slog.Info("detected runtime", "type", def.Type, "path", path, "version", dr.Version)
	}
	return detected
}

// detectVersion tries to get a version string from the CLI.
func detectVersion(ctx context.Context, binary string) string {
	for _, args := range [][]string{{"--version"}, {"version"}, {"-v"}} {
		ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, binary, args...).CombinedOutput()
		if err == nil {
			v := strings.TrimSpace(string(out))
			if v != "" {
				// Take first line only
				if idx := strings.IndexByte(v, '\n'); idx > 0 {
					v = v[:idx]
				}
				if len(v) > 80 {
					v = v[:80]
				}
				return v
			}
		}
	}
	return ""
}

// RuntimeByType returns the detected runtime for a given type, or nil.
func RuntimeByType(detected []DetectedRuntime, rt RuntimeType) *DetectedRuntime {
	for i := range detected {
		if detected[i].Type == rt {
			return &detected[i]
		}
	}
	return nil
}

// RuntimeSummary returns a human-readable summary of detected runtimes.
func RuntimeSummary(detected []DetectedRuntime) string {
	if len(detected) == 0 {
		return "No agent runtimes detected."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Detected %d runtime(s):\n", len(detected))
	for _, d := range detected {
		fmt.Fprintf(&b, "  %-8s %-30s %s\n", d.Type, d.Path, d.Version)
	}
	return b.String()
}
