# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**lazytunnel** is a production-grade SSH Tunnel Manager that provides a secure, auditable, and highly-available way to create, maintain, and tear down SSH port forwards (local, remote, or dynamic) for a fleet of hosts.

## Technology Stack

### Backend
- **Language**: Go 1.24.0 (per `go.mod`)
- **SSH**: `golang.org/x/crypto/ssh`
- **API**: `gorilla/mux` for REST
- **CLI**: `cobra` + `viper`
- **Database**: SQLite via `modernc.org/sqlite` (pure Go, no cgo); a single database file at the path configured by `database.path`
- **Logging**: `zerolog` (structured JSON)

### Frontend
- React 19 with TypeScript
- Radix UI primitives + Tailwind CSS (shadcn-style components)
- zustand for client state, TanStack Query for server state
- Vite for builds

### Infrastructure
- Docker with multi-stage builds
- Kubernetes + Helm charts (not yet implemented / aspirational — `deployments/helm/` and `deployments/k8s/` are empty, untracked directories; no chart or manifest exists in the repo)
- AWS KMS or HashiCorp Vault for key management (not yet implemented / aspirational — see Security Requirements → Key Management)
- Optional JWT bearer-token authentication (disabled by default unless a JWT secret is configured)

## Project Structure

```
lazytunnel/
├── cmd/
│   ├── server/           # API server entrypoint
│   ├── agent/            # Tunnel agent entrypoint
│   └── tunnelctl/        # CLI tool
├── internal/
│   ├── agent/            # Remote-agent coordinator, registry, worker
│   ├── api/              # REST handlers, auth middleware, websocket, validation
│   ├── auth/             # SSH authentication methods & secure key storage
│   ├── cli/              # tunnelctl command implementations
│   ├── config/           # Configuration management
│   ├── storage/          # SQLite persistence (sqlite.go)
│   └── tunnel/           # Core tunnel management
│       ├── manager.go        # Tunnel lifecycle manager
│       ├── session.go        # SSH session handling
│       ├── forward.go        # Port forwarding implementations
│       └── circuitbreaker.go # Reconnect circuit breaker
├── pkg/
│   ├── agentclient/      # Client for the remote-agent control plane
│   └── types/            # Shared types
├── web/                  # React + TypeScript frontend (built)
├── deployments/
│   ├── docker/           # Dockerfiles
│   └── systemd/          # systemd unit files
├── migrations/           # empty; schema is applied at boot (see Development Commands)
└── tests/
    ├── integration/
    └── e2e/
```

## Core Architecture

### Tunnel Data Model

The system revolves around `TunnelSpec` which defines a tunnel configuration:

```go
// pkg/types/tunnel.go
type TunnelSpec struct {
    ID               string        // Unique identifier
    Name             string        // Human-readable name
    Owner            string        // User/service that owns this tunnel
    AgentID          string        // Empty = run on API server (embedded); else routed to a remote agent
    DesiredStatus    DesiredStatus // Control-plane target state: "stopped" or "active"
    Type             TunnelType    // local, remote, dynamic (SOCKS5)
    Hops             []Hop         // Multi-hop chain (bastion -> target)
    LocalPort        int           // Local bind port
    LocalBindAddress string        // Local bind address
    RemoteHost       string        // Final destination host
    RemotePort       int           // Final destination port
    Auth             AuthConfig    // Authentication configuration
    AutoReconnect    bool          // Enable automatic reconnection
    KeepAlive        time.Duration // SSH keep-alive interval
    MaxRetries       int           // Maximum reconnection attempts
    Policy           PolicySpec    // Authorization policy
    CreatedAt        time.Time     // Creation timestamp
    UpdatedAt        time.Time     // Last-update timestamp
}

type Hop struct {
    Host                string              // Hostname/IP
    Port                int                 // SSH port (typically 22)
    User                string              // SSH username
    AuthMethod          AuthMethod          // key, password, agent, cert
    KeyID               string              // Path to SSH private key file (KMS is not yet integrated; see Security Requirements → Key Management)
    HostKeyVerification HostKeyVerification // strict, prompt, or insecure
    KnownHostsPath      string              // Path to known_hosts file
}
```

### Tunnel Lifecycle

1. **Create**: Validate spec → Store in DB → Queue for agent
2. **Connect**: SSH handshake → Establish forwards → Mark active
3. **Monitor**: Health checks → Metrics collection → Auto-reconnect
4. **Terminate**: Graceful shutdown → Close connections → Update DB

### Multi-Hop Logic

For tunnels requiring bastion hosts, the system chains SSH connections:
- Connect to Hop 1 (bastion)
- Create `net.Conn` over Hop 1 to Hop 2
- Continue until final destination
- Establish port forward through the chain

## Development Commands

### Running Tests

```bash
# Run all tests
go test ./...

# Run tests with coverage
go test -cover ./...

# Run integration tests (requires test SSH servers)
go test -tags=integration ./tests/integration/...

# Run specific package tests
go test ./internal/tunnel/...
```

### Building

```bash
# Build all binaries
go build -o bin/server cmd/server/main.go
go build -o bin/agent cmd/agent/main.go
go build -o bin/tunnelctl cmd/tunnelctl/main.go

# Build with version info
go build -ldflags "-X main.version=1.0.0" -o bin/tunnelctl cmd/tunnelctl/main.go

# Cross-compile for Linux
GOOS=linux GOARCH=amd64 go build -o bin/tunnelctl-linux cmd/tunnelctl/main.go
```

### Local Development

```bash
# There is no separate migration step. The schema is applied automatically at boot
# by initSchema() in internal/storage/sqlite.go (migrations/ is empty; no migration
# tool is used). New columns are added with a guarded ALTER TABLE ... ADD COLUMN,
# following the existing agent_id / desired_status pattern.

# Or run directly
go run cmd/server/main.go --config config.yaml

# Run agent
go run cmd/agent/main.go

# Run CLI tool
go run cmd/tunnelctl/main.go list
```

### Docker

```bash
# Build images (only Dockerfile.server and Dockerfile.web are tracked; there is no
# Dockerfile.agent)
docker build -f deployments/docker/Dockerfile.server -t lazytunnel-server .
docker build -f deployments/docker/Dockerfile.web -t lazytunnel-web .

# Run locally
docker-compose up
```

## Configuration

The system uses YAML configuration files, loaded by `internal/config/config.go` via viper. Example `config.yaml` (fields and defaults verified against that file):

```yaml
server:
  addr: ":8080"          # default ":8080"
  tls_cert: ""            # optional; set both to enable TLS
  tls_key: ""
  cors:
    allowed_origins: []  # default []; empty denies all cross-origin browser access

database:
  path: "tunnels.db"       # SQLite database file; default "tunnels.db"

auth:
  jwt_secret: ""            # if empty, falls back to jwt_secret_env; if both empty, auth is disabled
  jwt_secret_env: "LAZYTUNNEL_JWT_SECRET"  # default env var name
  token_expiration: "24h"   # default "24h"
  auto_start_tunnels: false

logging:
  level: "info"    # default "info"
  format: "console"  # default "console"
```

## Security Requirements

### Key Management (Not yet implemented / aspirational)
There is no KMS integration today. SSH keys are read from disk paths or from `ssh-agent`
(see `internal/auth/auth.go`); `internal/kms/` contains no code. The requirements below
describe the intended future state, not the current system:
- **Never** store private SSH keys in plaintext
- All keys must be stored in KMS (AWS KMS or Vault)
- Implement key rotation policies
- Audit all key access operations

### API Security
- TLS is optional: the server only serves HTTPS when both `server.tls_cert` and
  `server.tls_key` are configured (`internal/api/server.go`, `StartTLS`). No
  `tls.Config.MinVersion` is set anywhere in the repo, so there is no enforced
  minimum TLS version — Go's default is used when TLS is enabled
- JWT bearer-token authentication; there is no OAuth2/OIDC integration. Auth is
  disabled entirely unless a JWT secret is configured (`cmd/server/main.go:72`,
  via `auth.jwt_secret` or the `LAZYTUNNEL_JWT_SECRET` env var)
- Default token expiration is 24h (`auth.token_expiration`), not short-lived
- The `/api/v1/auth/login` endpoint currently accepts only hardcoded development
  credentials (`admin` / `lazytunnel`) — see `internal/api/handlers.go`
- Rate limiting is per client (authenticated user ID if present, otherwise
  source IP) via a token-bucket limiter (`internal/api/ratelimit.go`,
  `RateLimiter.extractClientID`); there is no role concept in it
- Proper CORS configuration

### Audit Logging
- Log all tunnel create/delete/modify operations
- Include user context (who, when, what)
- HMAC-sign log entries for tamper detection
- Immutable storage (append-only)

## Testing Strategy

### Unit Tests
- Focus on `internal/tunnel/` logic (session management, forwarding)
- Mock SSH connections using interfaces
- Test authentication method selection
- Test configuration parsing and validation

### Integration Tests (not yet implemented / aspirational)
`tests/integration/` and `tests/e2e/` are both empty directories today, and no
`//go:build integration` tag (or any build tag) exists anywhere in the repo. The
items below describe intended future coverage, not current tests:
- Requires actual SSH server(s) for testing
- Test end-to-end tunnel creation
- Test multi-hop tunneling
- Test auto-reconnect behavior

### Performance Requirements
- Support 1000+ concurrent tunnels per instance
- API response time < 100ms (p95)
- Tunnel establishment < 2s
- Auto-reconnect < 5s after failure

## CLI Usage Examples

```bash
# Create a simple local tunnel (--remote-host takes a combined host:port for
# local tunnels; --remote-port is only used for --type remote)
tunnelctl create \
  --name prod-db \
  --type local \
  --local-port 5432 \
  --remote-host db.internal.example.com:5432 \
  --hop bastion.example.com:22 \
  --user deploy \
  --key ~/.ssh/id_rsa

# Create SOCKS5 proxy
tunnelctl create --name socks --type dynamic --local-port 1080 --hop jumphost:22

# List active tunnels
tunnelctl list

# Get tunnel status
tunnelctl status prod-db

# Stop tunnel
tunnelctl stop prod-db
```

There is no `apply` subcommand — `internal/cli/root.go` registers only `create`,
`list`, `status`, `stop`, and `version`.

## Common Patterns

### Error Handling
- Use `errors.Is()` and `errors.As()` for error inspection
- Wrap errors with context: `fmt.Errorf("failed to connect to %s: %w", host, err)`
- Return errors, don't panic (except in init functions)

### Logging
- Use structured logging with fields: `log.Info().Str("tunnel_id", id).Msg("tunnel connected")`
- Include correlation IDs for request tracing
- Log at appropriate levels (debug/info/warn/error)

### Context
- Pass `context.Context` as first parameter
- Respect context cancellation in long-running operations
- Use `context.WithTimeout()` for external calls (e.g. SSH)

## Implementation Status

lazytunnel is built and running, not greenfield. What exists today:
- An API server (`cmd/server`) serving the REST API and the web UI
- A remote tunnel agent (`cmd/agent`) that lets tunnels run on a separate host from the API server
- A CLI (`cmd/tunnelctl`)
- A React + TypeScript web UI (`web/`)
- SQLite persistence (`internal/storage/sqlite.go`) with schema applied at boot
- A remote-agent control plane (`internal/agent`) for coordinating tunnels across agents

## Resources

- [Go SSH Package Documentation](https://pkg.go.dev/golang.org/x/crypto/ssh)
- [Cobra CLI Framework](https://github.com/spf13/cobra)
- [Viper Configuration](https://github.com/spf13/viper)
- [zerolog Logging](https://github.com/rs/zerolog)
- [AWS KMS SDK](https://docs.aws.amazon.com/kms/)
- [HashiCorp Vault API](https://www.vaultproject.io/api-docs)
