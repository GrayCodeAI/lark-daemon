# CLAUDE.md — lark-daemon

## Build & Test Commands

```bash
go build ./cmd/lark-server          # Build server binary
go build ./...                       # Build all packages
go test ./... -race -count=1         # Run all tests with race detector
go test ./internal/server/api -v -run TestName  # Run single test
go vet ./...                         # Static analysis
```

## Code Structure

- **`cmd/lark-server/main.go`** — Entry point, wires dependencies, starts server
- **`internal/proto/`** — Domain models (no logic, pure data structs)
- **`internal/server/api/`** — HTTP + WebSocket handlers, split by domain
- **`internal/server/store/`** — SQLite persistence layer
- **`internal/server/websocket/`** — Real-time hub, connection management, auth
- **`internal/server/service/`** — Thin business logic layer
- **`internal/server/storage/`** — File storage abstraction (local/S3)
- **`internal/server/metrics/`** — Agent metrics collector

## Key Conventions

- **IDs**: UUIDs generated with `github.com/google/uuid`
- **Timestamps**: Unix milliseconds (`time.Now().UnixMilli()`)
- **JSON**: All API responses use `application/json`
- **Auth**: JWT tokens (24h expiry) or API keys in `Authorization: Bearer <token>` header
- **WebSocket**: JSON envelope protocol with `event` and `data` fields
- **Errors**: Structured `{"code": "...", "message": "..."}` responses
- **Database**: SQLite with WAL mode, single-writer (`MaxOpenConns(1)`)
- **Migrations**: Versioned in `store/migrations.go`, tracked in `schema_version` table

## Testing Patterns

- In-memory SQLite: `file::memory:?cache=shared`
- HTTP tests: `httptest.NewServer` with full request/response testing
- WebSocket tests: Mock connections with `websocket.NewConn`
- Test helpers: `newTestStore()`, `seedWorkspace()`, `seedMember()`, `seedChannel()`

## Security Invariants (tested)

- Sender identity cannot be spoofed (tests verify)
- Protected fields (ID, workspace_id, type, api_key) cannot be overwritten via PATCH
- Channel membership enforced for message access
- Admin endpoints require admin/owner role
- JWT tokens can be revoked via blacklist
- Webhook secrets compared with constant-time comparison
- Rate limiter automatically evicts stale IP entries

## Router Decomposition

The API handlers are split into focused files by domain:
- `handlers_auth.go` — Registration, login, OAuth, logout
- `handlers_workspace.go` — Workspace CRUD
- `handlers_member.go` — Member CRUD, agent provisioning, profile
- `handlers_channel.go` — Channel CRUD, membership, archive
- `handlers_message.go` — Message CRUD, threads, search
- `handlers_reaction.go` — Reactions
- `handlers_task.go` — Tasks
- `handlers_agent.go` — Agent memory, metrics
- `handlers_file.go` — File upload/download
- `handlers_pin.go` — Message pins
- `handlers_approval.go` — Approval workflows
- `handlers_dm.go` — Direct messages, unread counts
- `handlers_webhook.go` — Webhook management
- `handlers_admin.go` — Admin stats, backup
- `handlers_ws.go` — WebSocket event handlers
- `middleware.go` — Rate limiting, logging, audit, security headers
- `helpers.go` — JSON encoding, error helpers, auth context
- `errors.go` — Structured API error codes
- `ratelimit.go` — Per-IP token bucket with auto-eviction
