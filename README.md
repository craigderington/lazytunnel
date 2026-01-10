# <img src="web/public/tunnel.svg" alt="lazytunnel" width="32" height="32" style="vertical-align: middle;"/> lazytunnel

A production-grade SSH Tunnel Manager for creating, maintaining, and managing SSH port forwards across a fleet of hosts.

## Features

### Core Functionality
- **Multiple Tunnel Types**: Local, remote, and dynamic (SOCKS5) port forwarding
- **Multi-Hop Support**: Chain tunnels through multiple bastion hosts
- **Auto-Reconnect**: Automatic reconnection with exponential backoff on failure
- **SSH Authentication**: Support for SSH keys, passwords, and SSH agent
- **Persistent Storage**: SQLite database for tunnel configurations and state
- **Graceful Lifecycle Management**: Clean startup, shutdown, and reconnection handling

### Web Interface
- **Modern React UI**: Beautiful, responsive web interface built with React 18 + TypeScript
- **Real-time Monitoring**: Live tunnel status updates and system health dashboard
- **Interactive Metrics**: Visualize tunnel statistics, uptime, and traffic patterns
- **Tunnel Management**: Create, view, stop, and delete tunnels through the UI
- **Demo Mode**: Test the interface with simulated data
- **Dark/Light Theme**: Customizable theme with persistent preferences
- **shadcn/ui Components**: Professional, accessible UI components

### API & CLI
- **RESTful API**: Full-featured API for programmatic tunnel management
- **CLI Tool**: `tunnelctl` command-line interface for scripting and automation
- **Health Endpoints**: Built-in health checks for monitoring and orchestration

### Deployment & Operations
- **Docker Support**: Multi-stage Docker builds for optimized container images
- **Docker Compose**: Complete orchestration for server and web frontend
- **Production Ready**: Health checks, restart policies, and volume management
- **Nginx Frontend**: Optimized static file serving with proper proxy configuration

## Quick Start

### Running with Docker Compose (Recommended)

The easiest way to get started is using Docker Compose:

```bash
# Clone the repository
git clone https://github.com/craigderington/lazytunnel.git
cd lazytunnel

# Start the server and web UI
docker-compose up -d

# Access the web interface
open http://localhost:3000
```

The server will be available at `http://localhost:8080` and the web UI at `http://localhost:3000`.

### Development Setup

#### Prerequisites

- Go 1.21 or later
- Node.js 18+ and npm (for web development)
- Docker & Docker Compose (recommended)
- SSH access to at least one remote server (for testing)

#### Building from Source

1. Clone the repository:
   ```bash
   git clone https://github.com/craigderington/lazytunnel.git
   cd lazytunnel
   ```

2. Build the Go binaries:
   ```bash
   # Build all binaries
   go build -o bin/server cmd/server/main.go
   go build -o bin/agent cmd/agent/main.go
   go build -o bin/tunnelctl cmd/tunnelctl/main.go
   ```

3. Run the server:
   ```bash
   # Run with default settings (listens on :8080)
   ./bin/server

   # Or specify a custom port
   ADDR=:9090 ./bin/server
   ```

4. (Optional) Build and run the web frontend:
   ```bash
   cd web
   npm install
   npm run dev
   ```

## Project Structure

```
lazytunnel/
├── cmd/                          # Application entrypoints
│   ├── server/                  # API server
│   ├── agent/                   # Tunnel agent
│   └── tunnelctl/               # CLI tool
├── internal/                    # Private application code
│   ├── api/                     # REST API handlers
│   │   ├── handlers.go         # HTTP handlers for tunnels, metrics, health
│   │   └── server.go           # HTTP server setup and middleware
│   ├── auth/                    # Authentication & authorization
│   │   └── auth.go             # SSH authentication methods
│   ├── cli/                     # CLI commands implementation
│   │   ├── create.go           # Create tunnel command
│   │   ├── list.go             # List tunnels command
│   │   ├── status.go           # Status command
│   │   └── stop.go             # Stop tunnel command
│   ├── storage/                 # Data persistence
│   │   └── sqlite.go           # SQLite database implementation
│   └── tunnel/                  # Core tunnel management
│       ├── manager.go          # Tunnel lifecycle manager
│       ├── session.go          # SSH session handling
│       └── forward.go          # Port forwarding implementations
├── pkg/                         # Public libraries
│   └── types/                   # Shared types
│       └── tunnel.go           # Tunnel data structures
├── web/                         # React web interface
│   ├── src/
│   │   ├── components/         # React components
│   │   │   ├── TunnelList.tsx  # Main tunnel list view
│   │   │   ├── CreateTunnelDialog.tsx
│   │   │   ├── Monitoring.tsx  # Real-time monitoring dashboard
│   │   │   ├── Metrics.tsx     # Metrics visualization
│   │   │   └── Settings.tsx    # Settings and preferences
│   │   ├── lib/                # Utilities and API client
│   │   ├── store/              # State management (Zustand)
│   │   └── types/              # TypeScript types
│   ├── public/                 # Static assets
│   │   └── tunnel.svg         # Application icon
│   └── package.json
├── deployments/                 # Deployment configurations
│   └── docker/
│       ├── Dockerfile.server   # Server container
│       ├── Dockerfile.web      # Web UI container
│       └── nginx.conf          # Nginx configuration
├── docker-compose.yml           # Docker Compose orchestration
├── examples/                    # Example usage code
└── tests/                       # Test suites
```

## Usage

### Web Interface

Access the web interface at `http://localhost:3000` (when running via Docker Compose) or `http://localhost:5173` (when running with `npm run dev`).

#### Features:
- **Dashboard**: View all active tunnels with real-time status
- **Create Tunnel**: Interactive form to create new tunnels with validation
- **Monitoring**: Live system health metrics and tunnel statistics
- **Metrics**: Visualize tunnel performance, uptime, and traffic
- **Settings**: Configure demo mode, theme preferences, and API endpoints
- **Demo Mode**: Test the interface with simulated tunnel data

#### Creating a Tunnel via Web UI:
1. Click "Create Tunnel" button
2. Fill in the tunnel details:
   - Name and description
   - Tunnel type (local, remote, or SOCKS5)
   - Local port, remote host, and remote port
   - SSH connection details (host, port, username)
   - Authentication (SSH key path or password)
3. Click "Create" to establish the tunnel

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

### API Endpoints

The server exposes a RESTful API on port 8080 (configurable via `ADDR` environment variable):

#### Core Endpoints:
- `GET /api/v1/health` - Health check endpoint
- `GET /api/v1/tunnels` - List all tunnels
- `POST /api/v1/tunnels` - Create a new tunnel
- `GET /api/v1/tunnels/:id` - Get tunnel details
- `DELETE /api/v1/tunnels/:id` - Stop and delete a tunnel
- `GET /api/v1/metrics` - Get system metrics

#### Example: Create a tunnel via API
```bash
curl -X POST http://localhost:8080/api/v1/tunnels \
  -H "Content-Type: application/json" \
  -d '{
    "name": "prod-db",
    "type": "local",
    "local_port": 5432,
    "remote_host": "db.internal.example.com",
    "remote_port": 5432,
    "ssh_host": "bastion.example.com",
    "ssh_port": 22,
    "ssh_user": "deploy",
    "ssh_key_path": "/root/.ssh/id_rsa"
  }'
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

### Docker Deployment

The project includes a complete Docker Compose setup for easy deployment:

```bash
# Build and start all services
docker-compose up -d

# View logs
docker-compose logs -f

# Stop services
docker-compose down

# Rebuild after code changes
docker-compose up -d --build
```

#### Docker Compose Services:
- **server**: Go API server running on host network mode (for tunnel access)
  - Exposes port 8080
  - Mounts `~/.ssh` for SSH key access
  - Uses persistent volume for SQLite database
  - Includes health checks
- **web**: React frontend served by Nginx
  - Exposes port 3000
  - Proxies API requests to server
  - Optimized multi-stage build

#### Building Images Manually:
```bash
# Build server image
docker build -f deployments/docker/Dockerfile.server -t lazytunnel-server .

# Build web image
docker build -f deployments/docker/Dockerfile.web -t lazytunnel-web .
```

### Frontend Development

```bash
cd web

# Install dependencies
npm install

# Run development server with hot reload
npm run dev

# Build for production
npm run build

# Preview production build
npm run preview

# Lint code
npm run lint
```

## Security

lazytunnel takes security seriously:

- **SSH Key Management**: Secure handling of SSH keys with support for key-based authentication
- **Read-only SSH Mounts**: SSH keys mounted read-only in Docker containers
- **No Plaintext Passwords**: Authentication via SSH keys and SSH agent
- **CORS Configuration**: Proper CORS headers for API security
- **Input Validation**: Comprehensive validation of tunnel configurations
- **Isolated Tunnels**: Each tunnel runs in its own goroutine with proper error handling
- **Future**: TLS 1.3 for API, OAuth2/OIDC, and KMS integration planned

## Documentation

- [Architecture Documentation](docs/architecture.md) _(coming soon)_
- [API Reference](docs/api.md) _(coming soon)_
- [CLI Reference](docs/cli.md) _(coming soon)_
- See [CLAUDE.md](CLAUDE.md) for development guidelines

## Current Status

**Active Development** - Core functionality is implemented and working!

### ✅ Completed
- **Phase 1: Core SSH Engine**
  - SSH client wrapper with multiple authentication methods
  - Single and multi-hop tunnel support
  - Local, remote, and SOCKS5 (dynamic) port forwarding
  - Auto-reconnect with exponential backoff
  - Comprehensive error handling

- **Storage & Persistence**
  - SQLite database for tunnel configurations
  - State management and persistence
  - Migration support

- **API & Server**
  - RESTful API with CORS support
  - Health check endpoints
  - Metrics collection and reporting
  - Structured error responses

- **Web Interface**
  - Modern React + TypeScript UI
  - Real-time tunnel management
  - Interactive monitoring dashboard
  - Metrics visualization
  - Dark/light theme support
  - Demo mode for testing

- **CLI Tool**
  - Full-featured `tunnelctl` command-line interface
  - Create, list, status, stop commands
  - JSON output support

- **Docker Deployment**
  - Multi-stage Docker builds
  - Docker Compose orchestration
  - Health checks and monitoring
  - Volume management

### 🚧 In Progress / Planned
- Enhanced metrics and monitoring
- WebSocket support for real-time updates
- Integration with KMS (AWS KMS, HashiCorp Vault)
- OAuth2/OIDC authentication
- Role-based access control (RBAC)
- Comprehensive test suite
- Kubernetes deployment (Helm charts)
- Multi-cluster support

## Technology Stack

### Backend
- **Go 1.21+**: Core language for server and CLI
- **golang.org/x/crypto/ssh**: SSH protocol implementation
- **gorilla/mux**: HTTP routing
- **SQLite**: Embedded database for persistence

### Frontend
- **React 18**: UI framework
- **TypeScript**: Type-safe JavaScript
- **Vite**: Build tool and dev server
- **TanStack Query**: Data fetching and caching
- **Zustand**: Lightweight state management
- **shadcn/ui**: UI component library
- **Tailwind CSS**: Utility-first styling
- **Lucide React**: Icon library

### Infrastructure
- **Docker**: Containerization
- **Docker Compose**: Multi-container orchestration
- **Nginx**: Web server and reverse proxy

## Screenshots

_Screenshots coming soon!_

The web interface includes:
- Dashboard with tunnel list and status indicators
- Create tunnel dialog with validation
- Real-time monitoring with system metrics
- Metrics visualization with charts
- Settings page with theme toggle and demo mode

## Contributing

Contributions are welcome! Please read the contributing guidelines before submitting PRs.

### How to Contribute
1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

MIT License - see [LICENSE](LICENSE) file for details

## Author

**Craig Derington**
- Email: craig@craigderington.dev
- GitHub: [@craigderington](https://github.com/craigderington)

---

**Note**: This project is under active development. The API and functionality may change as new features are added.
