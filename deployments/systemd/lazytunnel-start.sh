#!/usr/bin/env bash
# Start lazytunnel backend and Vite frontend under one systemd unit.
set -euo pipefail

ROOT="${LAZYTUNNEL_ROOT:-/home/cd/Work/lazytunnel}"
NODE_BIN="${LAZYTUNNEL_NODE_BIN:-/home/cd/.local/share/mise/installs/node/25.2.1/bin}"
SERVER_BIN="${LAZYTUNNEL_SERVER_BIN:-${ROOT}/server}"
WEB_DIR="${ROOT}/web"
SERVER_ADDR="${LAZYTUNNEL_ADDR:-:8080}"
WEB_PORT="${LAZYTUNNEL_WEB_PORT:-5173}"

server_pid=""
web_pid=""

log() {
  echo "[lazytunnel] $*"
}

cleanup() {
  local pid
  for pid in "$web_pid" "$server_pid"; do
    if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
      kill -TERM "$pid" 2>/dev/null || true
    fi
  done
  wait 2>/dev/null || true
}

trap 'cleanup; exit 0' EXIT INT TERM

if [[ ! -x "$SERVER_BIN" ]]; then
  log "error: server binary not found at ${SERVER_BIN}"
  exit 1
fi

if [[ ! -d "$WEB_DIR" ]]; then
  log "error: web directory not found at ${WEB_DIR}"
  exit 1
fi

export PATH="${NODE_BIN}:${PATH}"
export NODE_ENV="${NODE_ENV:-development}"
export VITE_API_URL="${VITE_API_URL:-http://localhost:8080/api/v1}"

log "starting backend (${SERVER_BIN} --addr=${SERVER_ADDR})"
cd "$ROOT"
"$SERVER_BIN" --addr="${SERVER_ADDR}" --debug &
server_pid=$!

log "starting frontend (npm run dev -- --host --port ${WEB_PORT})"
cd "$WEB_DIR"
npm run dev -- --host --port "${WEB_PORT}" &
web_pid=$!

log "backend pid=${server_pid}, frontend pid=${web_pid}"

# Restart the whole unit if either process exits.
wait -n "$server_pid" "$web_pid"
status=$?
log "a child process exited (status=${status}), shutting down"
cleanup
exit "${status}"