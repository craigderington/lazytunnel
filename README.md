# lazytunnel

A production-grade SSH Tunnel Manager for creating, maintaining, and managing SSH port forwards across a fleet of hosts.

## Features

- **Multiple Tunnel Types**: Local, remote, and dynamic (SOCKS5) port forwarding
- **Multi-Hop Support**: Chain tunnels through multiple bastion hosts
- **Auto-Reconnect**: Automatic reconnection with exponential backoff
- **Secure Key Management**: Integration with AWS KMS and HashiCorp Vault
- **RESTful API**: Full API for tunnel management
- **CLI Tool**: `tunnelctl` command-line interface
- **Observability**: Prometheus metrics, structured logging, and audit trails

## Quick Start

### Prerequisites

- Go 1.21 or later
- PostgreSQL 15+
- Docker & Docker Compose (for local development)

### Development Setup

1. Clone the repository:
   ```bash
   git clone https://github.com/craigderington/lazytunnel.git
   cd lazytunnel
   ```

2. Start PostgreSQL:
   ```bash
   docker-compose up -d postgres
   ```

3. Copy and configure the example config:
   ```bash
   cp config.example.yaml config.yaml
   # Edit config.yaml with your settings
   ```

4. Run database migrations:
   ```bash
   # Install golang-migrate if not already installed
   # https://github.com/golang-migrate/migrate
   migrate -path migrations -database "postgresql://tunnelmanager:development_password_change_in_production@localhost/tunnelmanager?sslmode=disable" up
   ```

5. Build and run:
   ```bash
   # Build all binaries
   go build -o bin/server cmd/server/main.go
   go build -o bin/agent cmd/agent/main.go
   go build -o bin/tunnelctl cmd/tunnelctl/main.go

   # Run the server
   ./bin/server --config config.yaml
   ```

## Project Structure

```
lazytunnel/
├── cmd/                  # Application entrypoints
│   ├── server/          # API server
│   ├── agent/           # Tunnel agent
│   └── tunnelctl/       # CLI tool
├── internal/            # Private application code
│   ├── api/             # REST/gRPC handlers
│   ├── auth/            # Authentication & authorization
│   ├── tunnel/          # Core tunnel management
│   ├── config/          # Configuration management
│   ├── kms/             # Key management integration
│   ├── metrics/         # Prometheus metrics
│   ├── audit/           # Audit logging
│   └── models/          # Data models
├── pkg/                 # Public libraries
│   ├── client/          # Go client library
│   └── types/           # Shared types
└── tests/               # Test suites
    ├── integration/
    └── e2e/
```

## Usage

### CLI Examples

Create a local port forward:
```bash
tunnelctl create \
  --name prod-db \
  --type local \
  --local-port 5432 \
  --remote-host db.internal.example.com \
  --remote-port 5432 \
  --hop bastion.example.com:22 \
  --user deploy \
  --key ~/.ssh/id_rsa
```

Create a SOCKS5 proxy:
```bash
tunnelctl create \
  --name socks \
  --type dynamic \
  --local-port 1080 \
  --hop jumphost.example.com:22
```

List active tunnels:
```bash
tunnelctl list
```

Get tunnel status:
```bash
tunnelctl status prod-db
```

Stop a tunnel:
```bash
tunnelctl stop prod-db
```

## Development

### Running Tests

```bash
# Run all tests
go test ./...

# Run with coverage
go test -cover ./...

# Run integration tests
go test -tags=integration ./tests/integration/...
```

### Building Docker Images

```bash
docker build -f deployments/docker/Dockerfile.server -t lazytunnel-server .
docker build -f deployments/docker/Dockerfile.agent -t lazytunnel-agent .
```

## Security

lazytunnel takes security seriously:

- **Never stores private keys in plaintext** - all keys managed via KMS
- **TLS 1.3 minimum** for all API communications
- **Comprehensive audit logging** for compliance
- **OAuth2/OIDC support** for authentication
- **Role-based access control** (RBAC)

## Documentation

- [Architecture Documentation](docs/architecture.md) _(coming soon)_
- [API Reference](docs/api.md) _(coming soon)_
- [CLI Reference](docs/cli.md) _(coming soon)_
- See [CLAUDE.md](CLAUDE.md) for development guidelines

## Current Status

This project is in **early development** (Phase 1: Core SSH Engine).

## Contributing

Contributions are welcome! Please read the contributing guidelines before submitting PRs.

## License

MIT (or GPL - TBD)

## Author

Craig Derington

---

**Note**: This is a work in progress. The API and functionality are subject to change.
