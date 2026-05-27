package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"

	"lark-daemon/internal/server/websocket"
)

// --- Helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("writeJSON encode failed", "err", err, "status", status)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// writeErrorCode writes a structured error response with a machine-readable code.
func writeErrorCode(w http.ResponseWriter, status int, code string, msg string) {
	writeJSON(w, status, APIError{Code: code, Message: msg})
}

// decodeAndProtect decodes JSON into obj, then calls restore to revert protected fields.
// Returns false and writes an error response if JSON decoding fails.
func decodeAndProtect(w http.ResponseWriter, req *http.Request, obj interface{}, restore func()) bool {
	if err := decodeJSON(req, obj); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return false
	}
	restore()
	return true
}

// serverError logs the real error and sends a generic message to the client.
func serverError(w http.ResponseWriter, err error, logMsg string, logArgs ...any) {
	slog.Error(logMsg, append([]any{"err", err}, logArgs...)...)
	writeErrorCode(w, http.StatusInternalServerError, ErrCodeInternal, "internal error")
}

// wsError logs the real error and sends a generic error envelope over WS.
func wsError(c *websocket.Conn, err error, logMsg string, logArgs ...any) {
	slog.Error(logMsg, append([]any{"err", err}, logArgs...)...)
	c.Send(websocket.NewEnvelope(websocket.EventError, map[string]string{"error": "internal error"}))
}

func decodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(nil, r.Body, 1<<20) // 1MB limit
	return json.NewDecoder(r.Body).Decode(v)
}

// isUUID checks that s is a valid UUID (36 chars, hex+dashes only).
func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// sanitizeFilename strips path separators, null bytes, and quotes from a filename.
func sanitizeFilename(name string) string {
	name = filepath.Base(name)
	name = strings.ReplaceAll(name, "\"", "")
	name = strings.ReplaceAll(name, "\n", "")
	name = strings.ReplaceAll(name, "\r", "")
	if name == "." || name == "/" {
		return "unnamed"
	}
	return name
}
