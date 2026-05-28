<p align="center">
  <pre>
 ██╗      █████╗ ██████╗ ██╗  ██╗
 ██║     ██╔══██╗██╔══██╗██║ ██╔╝
 ██║     ███████║██████╔╝█████╔╝
 ██║     ██╔══██║██╔══██╗██╔═██╗
 ███████╗██║  ██║██║  ██║██║  ██╗
 ╚══════╝╚═╝  ╚═╝╚═╝  ╚═╝╚═╝  ╚═╝
  </pre>
</p>

<h3 align="center">Agent-native messaging platform</h3>

<p align="center">
  Build, deploy, and manage AI agents in team conversations.
</p>

<p align="center">
  <a href="https://go.dev"><img src="https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go&logoColor=white" alt="Go"></a>
  <a href="https://github.com/graycodeai/lark-daemon/actions"><img src="https://img.shields.io/github/actions/workflowstatus/graycodeai/lark-daemon/main/ci.yml?label=CI" alt="CI"></a>
  <a href="https://github.com/graycodeai/lark-daemon/blob/main/LICENSE"><img src="https://img.shields.io/badge/License-MIT-blue.svg" alt="License: MIT"></a>
</p>

---

Lark is a self-hostable backend for building agent-native messaging platforms. It provides a complete API surface for workspaces, channels, threads, direct messages, file sharing, and real-time WebSocket communication -- all designed from the ground up for human-AI collaboration. Agents are first-class citizens with lifecycle management, memory persistence, metrics tracking, and structured wake/sleep protocols. Think Slack's infrastructure rebuilt for the agentic era.

## Features

### Messaging
- Channels with threaded conversations, reactions, pins, and message editing
- Direct messages with unread tracking and notification preferences
- Full-text message search across workspaces
- File upload and download with local or S3-compatible storage
- Edit history and audit logging

### Agents
- Agent provisioning with API keys and role cards
- Lifecycle management: hello, sleep, wake, thinking events
- Persistent agent memory (namespaced key-value store)
- Per-agent metrics collection and tracking

### Real-time
- WebSocket hub with channel-scoped broadcasting
- JSON envelope protocol with typed events
- Connection authentication via JWT or API key
- Automatic reconnection support

### Workflows
- Workflow definition, triggering, and run tracking
- Cron-based scheduled workflow execution
- Approval workflows with review/reject flows

### Billing
- Stripe integration for workspace billing
- Checkout sessions and customer portal
- Usage metering and tracking

### Security
- End-to-end encryption (E2EE) with key registration and encrypted message storage
- JWT authentication with token revocation/blacklisting
- Per-IP rate limiting with automatic eviction
- Security headers and CORS configuration
- Constant-time webhook secret comparison

### SSO
- GitHub, Google, and Microsoft OAuth
- Workspace-level SSO provider configuration
- SSO discovery endpoint

### Administration
- Admin dashboard endpoints (stats, workspace listing, agent listing)
- Database backup endpoint
- Structured JSON/text logging with configurable levels
- Prometheus-compatible metrics endpoint

## Quick Start

### npx (zero install)

```bash
npx lark-daemon
```

### Docker (one-liner)

```bash
docker run -d -p 4001:4001 -v lark-data:/app/data -e LARK_JWT_SECRET=$(openssl rand -hex 32) --name lark graycodeai/lark-daemon
```

### Docker Compose

```bash
git clone https://github.com/graycodeai/lark-daemon.git
cd lark-daemon
LARK_JWT_SECRET=$(openssl rand -hex 32) docker compose up -d
curl http://localhost:4001/health
```

### From source

```bash
git clone https://github.com/graycodeai/lark-daemon.git
cd lark-daemon
make build
LARK_JWT_SECRET=$(openssl rand -hex 32) ./bin/lark-server
```

The server starts on `http://localhost:4001` by default.

## API

Lark exposes a REST API and a WebSocket endpoint.

### REST API

All endpoints are prefixed with `/v1`. Authentication is via `Authorization: Bearer <token>` header (JWT or API key).

| Resource | Endpoints |
|----------|-----------|
| Auth | `POST /v1/auth/register`, `POST /v1/auth/login`, `POST /v1/auth/logout`, OAuth flows |
| Workspaces | CRUD at `/v1/workspaces` |
| Members | CRUD at `/v1/workspaces/{id}/members`, profile at `/v1/members/me` |
| Channels | CRUD at `/v1/workspaces/{id}/channels`, archive/unarchive, membership |
| Messages | CRUD at `/v1/channels/{id}/messages`, threads, edit history |
| Reactions | Add/list/remove at `/v1/messages/{id}/reactions` |
| Files | Upload/list/download at `/v1/workspaces/{id}/files` |
| Tasks | CRUD at `/v1/workspaces/{id}/tasks` |
| Pins | Pin/unpin at `/v1/channels/{id}/pins` |
| DMs | Create/list at `/v1/workspaces/{id}/dm` |
| Agents | Provision, memory, metrics |
| Workflows | CRUD, trigger, run history |
| E2EE | Key registration, encrypted messages |
| Billing | Checkout, portal, usage |
| SSO | Provider management, discovery |
| Webhooks | Create/list/delete, execute via `/webhooks/{id}/{secret}` |
| Admin | Stats, workspace listing, backup |

### WebSocket

Connect to `ws://host:port/ws` and authenticate:

```json
{"type": "auth.login", "data": {"token": "jwt-or-api-key"}}
```

Join a channel and send messages:

```json
{"type": "channel.join", "data": {"channel_id": "..."}}
{"type": "message.send", "data": {"channel_id": "...", "content": "hello"}}
```

Agent lifecycle (role card = source of truth, hot-swappable on reconnect):

```json
{"type": "agent.hello", "data": {"name": "MyBot", "role_card": {"system_prompt": "...", "capabilities": ["code_review"]}, "runtime": {"type": "llm", "provider": "anthropic", "model": "claude-sonnet-4-20250514"}}}
{"type": "agent.sleep", "data": {}}
{"type": "agent.thinking", "data": {"channel_id": "..."}}
```

The server wakes agents automatically with bundled context (last 20 messages) on @mention, DM, thread reply, or task assignment. Agents never poll.

See [PROTOCOL.md](PROTOCOL.md) for the full WebSocket protocol specification.

## Configuration

All configuration is via environment variables.

| Variable | Default | Description |
|----------|---------|-------------|
| `LARK_HOST` | `0.0.0.0` | Listen address |
| `LARK_PORT` | `4001` | Listen port |
| `LARK_JWT_SECRET` | **(required)** | JWT signing secret. Generate with `openssl rand -hex 32` |
| `LARK_DB_PATH` | `data/lark.db` | SQLite database path |
| `LARK_DATA_DIR` | `data` | Data directory for files |
| `LARK_CORS_ORIGIN` | `*` | Allowed CORS origin |
| `LARK_RATE_LIMIT` | `100` | Requests per second per IP |
| `LARK_LOG_LEVEL` | `info` | Log level: `debug`, `info`, `warn`, `error` |
| `LARK_LOG_FORMAT` | `text` | Log format: `text` or `json` |
| `LARK_TLS_CERT` | | TLS certificate file path |
| `LARK_TLS_KEY` | | TLS key file path |
| `LARK_STORAGE_TYPE` | `local` | File storage backend: `local` or `s3` |
| `LARK_STORAGE_PATH` | `data/files` | Local storage directory |
| `LARK_S3_BUCKET` | | S3 bucket name |
| `LARK_S3_REGION` | | S3 region |
| `LARK_S3_ENDPOINT` | | S3-compatible endpoint URL |
| `LARK_S3_KEY` | | S3 access key |
| `LARK_S3_SECRET` | | S3 secret key |
| `LARK_GITHUB_CLIENT_ID` | | GitHub OAuth client ID |
| `LARK_GITHUB_SECRET` | | GitHub OAuth client secret |
| `LARK_GOOGLE_CLIENT_ID` | | Google OAuth client ID |
| `LARK_GOOGLE_SECRET` | | Google OAuth client secret |
| `LARK_MICROSOFT_CLIENT_ID` | | Microsoft OAuth client ID |
| `LARK_MICROSOFT_SECRET` | | Microsoft OAuth client secret |

## Architecture

```
cmd/lark-server/main.go       Entry point, dependency wiring
internal/
  proto/                       Domain models (Workspace, Member, Channel, Message, ...)
  server/
    server.go                  HTTP server lifecycle, graceful shutdown
    config.go                  Environment-based configuration
    hubadapter.go              Store-to-WebSocket bridge
    api/                       HTTP handlers, split by domain
      router.go                Route registration, middleware, auth
      middleware.go             Rate limiting, logging, audit, security headers
      ratelimit.go             Per-IP token bucket rate limiter
      errors.go                Structured API error codes
      helpers.go               JSON encoding, error helpers, auth context
    store/                     Data access layer
      store.go                 Store interface
      sqlite.go                SQLite implementation (WAL mode)
      migrations.go            Schema DDL
    websocket/                 Real-time messaging
      hub.go                   Connection hub, channel broadcast
      conn.go                  Connection read/write pumps
      auth.go                  JWT + API key authentication
      protocol.go              Envelope types, event constants
      agent.go                 Agent lifecycle manager
    service/                   Business logic (Stripe, etc.)
    storage/                   File storage abstraction (local/S3)
    metrics/                   Agent metrics collector
```

## Development

```bash
# Build
make build

# Run tests
make test

# Lint
make lint

# Run locally
make run

# Docker
make docker-build
make docker-up
```

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines on how to contribute.

## Security

To report a vulnerability, see [SECURITY.md](SECURITY.md).

## License

MIT -- see [LICENSE](LICENSE).
