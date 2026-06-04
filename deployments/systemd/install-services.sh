#!/bin/bash
# Install LazyTunnel systemd services
# Usage: sudo ./install-services.sh <username>

set -e

if [ "$#" -ne 1 ]; then
    echo "Usage: sudo $0 <username>"
    echo "Example: sudo $0 cd"
    exit 1
fi

USERNAME=$1
LAZYTUNNEL_DIR="/home/$USERNAME/Work/lazytunnel"
SERVICE_DIR="/etc/systemd/system"

echo "=== Installing LazyTunnel Services ==="
echo "User: $USERNAME"
echo "Directory: $LAZYTUNNEL_DIR"
echo ""

# Check if running as root
if [ "$EUID" -ne 0 ]; then
    echo "Error: Please run as root (use sudo)"
    exit 1
fi

# Check if user exists
if ! id "$USERNAME" &>/dev/null; then
    echo "Error: User '$USERNAME' does not exist"
    exit 1
fi

# Check if lazytunnel directory exists
if [ ! -d "$LAZYTUNNEL_DIR" ]; then
    echo "Error: LazyTunnel directory not found at $LAZYTUNNEL_DIR"
    exit 1
fi

# Check if binaries exist
if [ ! -f "$LAZYTUNNEL_DIR/server" ]; then
    echo "Error: server binary not found at $LAZYTUNNEL_DIR/server"
    echo "Please build the project first: go build -o server cmd/server/main.go"
    exit 1
fi

# Copy service files
echo "Installing service files..."
cp "$LAZYTUNNEL_DIR/deployments/systemd/lazytunnel-backend@.service" "$SERVICE_DIR/"
cp "$LAZYTUNNEL_DIR/deployments/systemd/lazytunnel-frontend@.service" "$SERVICE_DIR/"

# Reload systemd
echo "Reloading systemd..."
systemctl daemon-reload

# Enable services for the specific user
echo "Enabling services for user $USERNAME..."
systemctl enable "lazytunnel-backend@$USERNAME"
systemctl enable "lazytunnel-frontend@$USERNAME"

echo ""
echo "=== Installation Complete ==="
echo ""
echo "Services installed:"
echo "  - lazytunnel-backend@$USERNAME"
echo "  - lazytunnel-frontend@$USERNAME"
echo ""
echo "To start the services:"
echo "  sudo systemctl start lazytunnel-backend@$USERNAME"
echo "  sudo systemctl start lazytunnel-frontend@$USERNAME"
echo ""
echo "To check status:"
echo "  sudo systemctl status lazytunnel-backend@$USERNAME"
echo "  sudo systemctl status lazytunnel-frontend@$USERNAME"
echo ""
echo "To view logs:"
echo "  sudo journalctl -u lazytunnel-backend@$USERNAME -f"
echo "  sudo journalctl -u lazytunnel-frontend@$USERNAME -f"
echo ""
echo "API will be available at: http://localhost:8080"
echo "Frontend will be available at: http://localhost:5173"
