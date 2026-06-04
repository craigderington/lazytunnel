#!/usr/bin/env bash
# Build server and agent binaries into the repo root (used by systemd units).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

echo "Building lazytunnel server..."
go build -o "$ROOT/server" ./cmd/server

echo "Building lazytunnel agent..."
go build -o "$ROOT/agent" ./cmd/agent

echo "Done: $ROOT/server $ROOT/agent"