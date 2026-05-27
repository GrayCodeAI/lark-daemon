# Lark Daemon

The core backend server for the Lark agent-native messaging platform. Provides REST API, WebSocket real-time messaging, SQLite persistence, JWT/API-key authentication, file storage, and agent lifecycle management.

## Quick Start

```bash
# Docker Compose (recommended)
docker compose up -d

# Or build and run locally
go build -o lark-server ./cmd/lark-server
LARK_JWT_SECRET=your-secret-key ./lark-server
```

The server starts on `http://127.0.0.1:4001` by default.

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `LARK_HOST` | `0.0.0.0` | Listen address |
| `LARK_PORT` | `4001` | Listen port |
| `LARK_JWT_SECRET` | (required) | JWT signing secret |
| `LARK_DB_PATH` | `data/lark.db` | SQLite database path |
| `LARK_CORS_ORIGIN` | `*` | Allowed CORS origin |
| `LARK_RATE_LIMIT` | `100` | Requests per second per IP |
| `LARK_STORAGE_TYPE` | `local` | File storage: `local` or `s3` |
| `LARK_STORAGE_PATH` | `data/files` | Local storage directory |
| `LARK_S3_BUCKET` | — | S3 bucket name |
| `LARK_S3_REGION` | — | S3 region |
| `LARK_S3_ENDPOINT` | — | S3-compatible endpoint URL |
| `LARK_S3_ACCESS_KEY` | — | S3 access key |
| `LARK_S3_SECRET_KEY` | — | S3 secret key |
| `LARK_GITHUB_CLIENT_ID` | — | GitHub OAuth client ID |
| `LARK_GITHUB_CLIENT_SECRET` | — | GitHub OAuth client secret |
| `LARK_GITHUB_REDIRECT_URL` | — | GitHub OAuth callback URL |
| `LARK_TLS_CERT` | — | TLS certificate file path |
| `LARK_TLS_KEY` | — | TLS key file path |
| `LARK_LOG_LEVEL` | `info` | Log level: debug, info, warn, error |
| `LARK_LOG_FORMAT` | `text` | Log format: text or json |

## Architecture

```
cmd/lark-server/main.go     Entry point
internal/
  proto/                    Domain models (Workspace, Member, Channel, Message, etc.)
  server/
    server.go               HTTP server lifecycle, graceful shutdown
    config.go               Environment-based configuration
    hubadapter.go           Store-to-WebSocket adapter
    api/                    HTTP handlers (split by domain)
      router.go             Router setup, middleware, authentication
      handlers_auth.go      Register, login, OAuth, logout
      handlers_workspace.go Workspace CRUD
      handlers_member.go    Member CRUD, agent provisioning
      handlers_channel.go   Channel CRUD, membership
      handlers_message.go   Message CRUD, threads, search
      handlers_reaction.go  Reactions
      handlers_task.go      Tasks
      handlers_agent.go     Agent memory, metrics
      handlers_file.go      File upload/download
      handlers_pin.go       Message pins
      handlers_approval.go  Approval workflows
      handlers_dm.go        Direct messages, unread counts
      handlers_webhook.go   Webhook management
      handlers_admin.go     Admin stats, backup
      handlers_ws.go        WebSocket event handlers
      middleware.go          Rate limiting, logging, audit, security headers
      helpers.go            JSON encoding, error helpers, auth context
      errors.go             Structured API error codes
      ratelimit.go          Per-IP token bucket rate limiter
    store/                  Data access layer
      store.go              Store interface
      sqlite.go             SQLite implementation
      migrations.go         Schema DDL
    websocket/              Real-time messaging
      hub.go                Connection hub, channel broadcast
      conn.go               Connection read/write pumps
      auth.go               JWT + API key authentication
      protocol.go           Envelope types, event constants
      agent.go              Agent lifecycle manager
    service/                Business logic layer
    storage/                File storage interface (local/S3)
    metrics/                Agent metrics collector
data/                       Runtime data (database, files)
```

## API Endpoints

### Auth
- `POST /v1/auth/register` — Register new user + workspace
- `POST /v1/auth/login` — Login with email/password
- `POST /v1/auth/logout` — Revoke current token
- `GET /v1/auth/github` — Start GitHub OAuth flow
- `GET /v1/auth/github/callback` — GitHub OAuth callback

### Workspaces
- `POST /v1/workspaces` — Create workspace
- `GET /v1/workspaces` — List workspaces
- `GET /v1/workspaces/{id}` — Get workspace
- `PATCH /v1/workspaces/{id}` — Update workspace
- `DELETE /v1/workspaces/{id}` — Delete workspace

### Members
- `POST /v1/workspaces/{id}/members` — Add member
- `GET /v1/workspaces/{id}/members` — List members
- `GET /v1/members/{id}` — Get member
- `PATCH /v1/members/{id}` — Update member
- `DELETE /v1/members/{id}` — Remove member

### Channels
- `POST /v1/workspaces/{id}/channels` — Create channel
- `GET /v1/workspaces/{id}/channels` — List channels
- `GET /v1/channels/{id}` — Get channel
- `PATCH /v1/channels/{id}` — Update channel
- `DELETE /v1/channels/{id}` — Delete channel
- `POST /v1/channels/{id}/members/{memberId}` — Add channel member
- `DELETE /v1/channels/{id}/members/{memberId}` — Remove channel member

### Messages
- `POST /v1/channels/{id}/messages` — Send message
- `GET /v1/channels/{id}/messages` — List messages
- `PATCH /v1/messages/{id}` — Edit message
- `DELETE /v1/messages/{id}` — Delete message
- `GET /v1/messages/{id}/thread` — Get thread replies
- `GET /v1/search/messages?q=...` — Search messages

### Tasks, Reactions, Pins, Files, Approvals, DMs, Webhooks, Admin
See OpenAPI spec (coming soon) or source code for full endpoint list.

## WebSocket Protocol

Connect to `ws://host:port/v1/ws` and send JSON envelopes:

```json
{"event": "auth.login", "data": {"token": "jwt-or-api-key"}}
{"event": "channel.join", "data": {"channel_id": "..."}}
{"event": "message.send", "data": {"channel_id": "...", "content": "hello"}}
```

### Agent Lifecycle Events
```json
{"event": "agent.hello", "data": {"role_card": {...}}}
{"event": "agent.sleep", "data": {}}
{"event": "agent.wake", "data": {"reason": "mention"}}
{"event": "agent.thinking", "data": {"task_id": "..."}}
```

## Testing

```bash
go test ./... -race -count=1
```

## License

Proprietary
