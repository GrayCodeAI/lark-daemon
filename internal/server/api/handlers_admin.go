package api

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"lark-daemon/internal/proto"
)

// --- Admin handlers ---

func (r *Router) handleAdminStats(w http.ResponseWriter, req *http.Request) {
	if requireAdmin(w, req) == nil {
		return
	}
	ws, _ := r.services.ListWorkspaces(req.Context())
	stats := map[string]any{
		"ws_connections": r.hub.Total(),
		"ws_agents":      r.hub.AgentCount(),
		"workspaces":     len(ws),
	}
	writeJSON(w, http.StatusOK, stats)
}

func (r *Router) handleAdminListWorkspaces(w http.ResponseWriter, req *http.Request) {
	if requireAdmin(w, req) == nil {
		return
	}
	ws, err := r.services.ListWorkspaces(req.Context())
	if err != nil {
		serverError(w, err, "list workspaces failed")
		return
	}
	for _, w := range ws {
		w.AgentProvisionToken = ""
	}
	writeJSON(w, http.StatusOK, ws)
}

func (r *Router) handleAdminListAgents(w http.ResponseWriter, req *http.Request) {
	if requireAdmin(w, req) == nil {
		return
	}
	ws, _ := r.services.ListWorkspaces(req.Context())
	allAgents := make([]map[string]any, 0)
	for _, w := range ws {
		members, err := r.services.ListMembers(req.Context(), w.ID)
		if err != nil {
			continue
		}
		for _, m := range members {
			if m.Type == proto.MemberAgent {
				allAgents = append(allAgents, map[string]any{
					"id":           m.ID,
					"name":         m.Name,
					"workspace_id": w.ID,
					"workspace":    w.Name,
					"online":       r.hub.GetAgent(m.ID) != nil,
					"status":       m.Status,
				})
			}
		}
	}
	writeJSON(w, http.StatusOK, allAgents)
}

// --- Metrics ---

func (r *Router) handleMetrics(w http.ResponseWriter, req *http.Request) {
	stats := map[string]any{
		"connections": r.hub.Total(),
		"agents":      r.hub.AgentCount(),
	}
	writeJSON(w, http.StatusOK, stats)
}

// --- Backup ---

func (r *Router) handleAdminBackup(w http.ResponseWriter, req *http.Request) {
	if requireAdmin(w, req) == nil {
		return
	}
	backupDir := filepath.Join("data", "backups")
	if err := os.MkdirAll(backupDir, 0750); err != nil {
		serverError(w, err, "create backup dir failed")
		return
	}
	filename := fmt.Sprintf("lark-backup-%s.db", time.Now().UTC().Format("20060102-150405"))
	destPath := filepath.Join(backupDir, filename)
	if err := r.store.Backup(req.Context(), destPath); err != nil {
		serverError(w, err, "backup failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"message":  "backup created",
		"path":     destPath,
		"filename": filename,
	})
}
