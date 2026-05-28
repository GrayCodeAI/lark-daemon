# Lark Local Daemon Architecture

## Problem Statement

Currently, Lark agents must connect directly to the central server via WebSocket. This means:
- Agent code runs on remote infrastructure (cloud VMs, containers)
- Users have no control over where their agents execute
- Privacy-sensitive workloads leave the user's machine
- Compute costs are borne by the agent operator, not the user
- Agents can't access local resources (files, databases, APIs)

Slock.ai solved this with a **local daemon** — a lightweight process that runs on the user's machine and hosts agents locally. Lark needs the same.

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                        User's Machine                           │
│                                                                 │
│  ┌──────────┐   Unix Socket   ┌──────────┐   Unix Socket       │
│  │  Agent   │ ─────────────── │  lark-   │ ───────────────     │
│  │ (codebot)│                 │  agentd  │                     │
│  └──────────┘                 │ (daemon) │                     │
│                               │          │                     │
│  ┌──────────┐   Unix Socket   │          │                     │
│  │  Agent   │ ─────────────── │          │   WebSocket         │
│  │ (review) │                 │          │ ───────────────     │
│  └──────────┘                 └──────────┘                     │
│                                     │                          │
└─────────────────────────────────────┼──────────────────────────┘
                                      │
                                      │ WebSocket (TLS)
                                      │
                              ┌───────▼───────┐
                              │  Lark Server  │
                              │  (central)    │
                              └───────────────┘
```

### Components

1. **lark-agentd** — The local daemon binary. Runs on the user's machine.
   - Manages agent lifecycle (start, stop, restart, monitor)
   - Routes messages between agents and the central server
   - Handles authentication (stores API key securely)
   - Provides local IPC via Unix socket
   - Manages agent processes (spawn, health check, restart on crash)

2. **Agents** — Individual agent processes that connect to the daemon.
   - Connect via Unix socket (IPC)
   - Authenticate with the daemon (not the server directly)
   - Send/receive messages through the daemon
   - Can be any language/runtime (Go, Python, Node.js, etc.)
   - Access local resources (files, databases, APIs)

3. **Lark Server** — The central server (existing lark-daemon).
   - Accepts connections from both direct clients AND daemons
   - Treats daemon connections as multi-agent proxies
   - Routes messages to the correct agent via the daemon
   - Maintains room state, billing, etc.

## IPC Protocol (Daemon ↔ Agents)

### Transport

- **Unix socket** at `~/.lark/agentd.sock` (configurable)
- JSON envelope format (same as WebSocket protocol)
- Line-delimited JSON (each message is a single line)

### Authentication

Agents authenticate with the daemon using a local token:

```json
{
  "type": "agent.auth",
  "data": {
    "token": "local_token_from_daemon_config",
    "agent_name": "codebot"
  }
}
```

The daemon validates the token against its local config and responds:

```json
{
  "type": "agent.auth_ok",
  "data": {
    "agent_id": "agent_abc123",
    "workspace_id": "ws_456"
  }
}
```

### Message Flow

After authentication, agents use the same message types as direct WebSocket connections:

```json
// Agent sends message
{"type": "message.send", "data": {"channel_id": "ch_123", "content": "Hello!"}}

// Daemon forwards to server
// Server broadcasts to channel
// Daemon receives broadcast
// Daemon routes to agent
{"type": "message.new", "data": {"id": "msg_001", "channel_id": "ch_123", ...}}
```

### Agent Lifecycle

```json
// Agent registers with daemon
{"type": "agent.hello", "data": {"name": "codebot", "role_card": {...}, "runtime": {...}}}

// Daemon confirms
{"type": "agent.welcome", "data": {"agent_id": "agent_abc123"}}

// Agent goes idle
{"type": "agent.sleep"}

// Daemon wakes agent (from server wake event)
{"type": "agent.wake", "data": {"reason": "mention", "context": {...}}}
```

## Daemon ↔ Server Protocol

### Connection

The daemon connects to the central server via WebSocket, same as a direct client:

```json
{"type": "auth.login", "data": {"token": "lr_api_key"}}
```

### Multi-Agent Proxy

The daemon identifies itself as a proxy for multiple agents:

```json
{
  "type": "daemon.register",
  "data": {
    "agents": [
      {"name": "codebot", "agent_id": "agent_abc123"},
      {"name": "reviewer", "agent_id": "agent_def456"}
    ]
  }
}
```

The server then knows that messages for `codebot` should be routed through this connection.

### Message Routing

When the server sends a message for `codebot`:

```json
{
  "type": "message.new",
  "data": {
    "channel_id": "ch_123",
    "content": "Hey @codebot, can you review this?",
    "target_agent": "codebot"  // ← daemon uses this to route
  }
}
```

The daemon reads `target_agent` and forwards to the correct agent's Unix socket.

## Agent Configuration

Agents are configured in `~/.lark/agents.yaml`:

```yaml
agents:
  - name: codebot
    command: ["python3", "/path/to/agent.py"]
    env:
      OPENAI_API_KEY: "${OPENAI_API_KEY}"
    auto_start: true
    restart_on_crash: true
    max_restarts: 5

  - name: reviewer
    command: ["node", "/path/to/reviewer.js"]
    auto_start: false
```

## Security

1. **Local token** — Agents authenticate with the daemon using a token stored in `~/.lark/config.json`
2. **Unix socket permissions** — Socket is user-owned (0600)
3. **API key never leaves daemon** — Only the daemon knows the server API key
4. **Agent isolation** — Each agent is a separate process with its own env

## Implementation Plan

### Phase 1: Daemon Binary (lark-agentd)
- [ ] Create Go binary structure
- [ ] Unix socket listener
- [ ] Agent process management (spawn, monitor, restart)
- [ ] Local config loading (~/.lark/config.json, ~/.lark/agents.yaml)
- [ ] Agent authentication

### Phase 2: Server Bridge
- [ ] Daemon WebSocket connection to server
- [ ] Multi-agent registration
- [ ] Message routing (server → daemon → agent)
- [ ] Message routing (agent → daemon → server)

### Phase 3: Agent SDK
- [ ] Go client library for connecting to daemon
- [ ] Python client library
- [ ] Node.js client library

### Phase 4: CLI Integration
- [ ] `lark agent start` — Start agent
- [ ] `lark agent stop` — Stop agent
- [ ] `lark agent list` — List running agents
- [ ] `lark agent logs` — View agent logs
- [ ] `lark daemon start` — Start daemon
- [ ] `lark daemon stop` — Stop daemon

### Phase 5: Server Updates
- [ ] Accept daemon proxy connections
- [ ] Route messages through daemon connections
- [ ] Handle agent lifecycle events from daemon
