# API Reference

lazytunnel provides a RESTful HTTP API for managing SSH tunnels programmatically.

## Base URL

```
http://localhost:8080/api/v1
```

## Endpoints

### Health Check

**GET** `/health`

Returns server health status.

**Response** `200 OK`:
```json
{
  "status": "healthy",
  "time": "2026-01-09T12:34:56Z"
}
```

### List Tunnels

**GET** `/tunnels`

Returns all active tunnels.

**Response** `200 OK`:
```json
{
  "tunnels": [
    {
      "id": "550e8400-e29b-41d4-a716-446655440000",
      "name": "prod-db",
      "type": "local",
      "status": {
        "tunnel_id": "550e8400-e29b-41d4-a716-446655440000",
        "state": "active",
        "connected_at": "2026-01-09T12:00:00Z",
        "bytes_sent": 1024000,
        "bytes_received": 2048000,
        "retry_count": 0
      },
      "created_at": "2026-01-09T12:00:00Z"
    }
  ],
  "count": 1
}
```

### Create Tunnel

**POST** `/tunnels`

Creates a new SSH tunnel.

**Request Body**:
```json
{
  "name": "prod-db",
  "type": "local",
  "local_port": 5432,
  "remote_host": "db.internal.example.com",
  "remote_port": 5432,
  "hops": [
    {
      "host": "bastion.example.com",
      "port": 22,
      "user": "deploy",
      "auth_method": "key",
      "key_id": "/path/to/key"
    }
  ],
  "auto_reconnect": true,
  "keep_alive": "30s",
  "max_retries": 3
}
```

**Response** `201 Created`:
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "message": "Tunnel created successfully",
  "spec": { /* full tunnel spec */ }
}
```

**Error** `400 Bad Request`:
```json
{
  "error": "Invalid request body: missing required field"
}
```

**Error** `500 Internal Server Error`:
```json
{
  "error": "Failed to create tunnel: connection refused"
}
```

### Get Tunnel

**GET** `/tunnels/{id}`

Returns detailed information about a specific tunnel.

**Response** `200 OK`:
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "name": "prod-db",
  "type": "local",
  "spec": {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "name": "prod-db",
    "type": "local",
    "local_port": 5432,
    "remote_host": "db.internal.example.com",
    "remote_port": 5432,
    "hops": [
      {
        "host": "bastion.example.com",
        "port": 22,
        "user": "deploy",
        "auth_method": "key",
        "key_id": "/path/to/key"
      }
    ],
    "auto_reconnect": true,
    "keep_alive": "30s",
    "max_retries": 3
  },
  "status": {
    "tunnel_id": "550e8400-e29b-41d4-a716-446655440000",
    "state": "active",
    "connected_at": "2026-01-09T12:00:00Z",
    "bytes_sent": 1024000,
    "bytes_received": 2048000,
    "retry_count": 0
  },
  "created_at": "2026-01-09T12:00:00Z"
}
```

**Error** `404 Not Found`:
```json
{
  "error": "Tunnel not found"
}
```

### Get Tunnel Status

**GET** `/tunnels/{id}/status`

Returns status information for a specific tunnel.

**Response** `200 OK`:
```json
{
  "tunnel_id": "550e8400-e29b-41d4-a716-446655440000",
  "state": "active",
  "connected_at": "2026-01-09T12:00:00Z",
  "last_error": "",
  "bytes_sent": 1024000,
  "bytes_received": 2048000,
  "latency": 0,
  "retry_count": 0
}
```

**Error** `404 Not Found`:
```json
{
  "error": "Tunnel not found"
}
```

### Delete Tunnel

**DELETE** `/tunnels/{id}`

Stops and removes a tunnel.

**Response** `200 OK`:
```json
{
  "message": "Tunnel stopped successfully"
}
```

**Error** `404 Not Found`:
```json
{
  "error": "Tunnel not found"
}
```

**Error** `500 Internal Server Error`:
```json
{
  "error": "Failed to stop tunnel: internal error"
}
```

## Tunnel Types

### Local Port Forwarding

```json
{
  "type": "local",
  "local_port": 5432,
  "remote_host": "db.internal.example.com",
  "remote_port": 5432
}
```

Binds to `localhost:5432` and forwards to `db.internal.example.com:5432` through the SSH tunnel.

### Remote Port Forwarding

```json
{
  "type": "remote",
  "local_port": 8080,
  "remote_port": 9090
}
```

Binds to `remote:9090` on the SSH server and forwards back to `localhost:8080`.

### Dynamic Port Forwarding (SOCKS5)

```json
{
  "type": "dynamic",
  "local_port": 1080
}
```

Creates a SOCKS5 proxy on `localhost:1080` with dynamic destination routing.

## Configuration Backup and Restore

These two endpoints back up and restore tunnel definitions as a portable JSON
archive. Like every other endpoint under `/api/v1` except `/health`,
`/openapi.yaml`, `/metrics` and `/auth/login`, they require authentication
when a JWT secret is configured, and are open when it is not — auth is the
only access control in front of them. There is no rate limiting on these (or
any) routes: `internal/api/ratelimit.go` defines a limiter, but it is never
registered on the router.

### Export Configuration

**GET** `/config/export`

Returns every stored tunnel definition as a versioned JSON archive. Sets
`Content-Disposition: attachment; filename="lazytunnel-backup-<timestamp>.json"`,
so a browser downloads it directly instead of navigating to it, and a
`Content-Length` header so the download shows a size and progress bar.

The archive contains hostnames, SSH usernames and key **paths**. It contains
no key material — `AuthConfig` is never persisted by the storage layer, so a
restore does not return credentials.

Tunnel names are trimmed of surrounding whitespace on export. A tunnel's
`local_bind_address` is normalized to the explicit string `"0.0.0.0"` when it
was empty at runtime — an empty value already means "bind all interfaces"
(see `internal/tunnel/forward.go`), so the archive makes that exposure visible
in the file rather than leaving it implicit. To narrow an exported tunnel to
loopback only, edit its `local_bind_address` to `127.0.0.1` in the archive
before importing it back.

**Response** `200 OK`:
```json
{
  "version": 1,
  "exported_at": "2026-07-29T12:00:00Z",
  "source": "lazytunnel/1.0.0",
  "tunnels": [
    {
      "id": "550e8400-e29b-41d4-a716-446655440000",
      "name": "prod-db",
      "owner": "admin",
      "desired_status": "active",
      "type": "local",
      "hops": [
        {
          "host": "bastion.example.com",
          "port": 22,
          "user": "deploy",
          "auth_method": "key",
          "key_id": "/home/user/.ssh/id_rsa",
          "host_key_verification": "strict",
          "known_hosts_path": "/home/user/.ssh/known_hosts"
        }
      ],
      "local_port": 5432,
      "local_bind_address": "0.0.0.0",
      "remote_host": "db.internal.example.com",
      "remote_port": 5432,
      "auto_reconnect": true,
      "keep_alive_seconds": 30,
      "max_retries": 3
    }
  ]
}
```

**Error** `503 Service Unavailable`: persistent storage is not configured.
```json
{
  "code": "SERVICE_UNAVAILABLE",
  "message": "Persistent storage is not configured",
  "timestamp": "2026-07-29T12:00:00Z"
}
```

**Error** `500 Internal Server Error`: reading the stored tunnels failed, or
encoding the archive as JSON failed. The message names which step failed:
```json
{
  "code": "INTERNAL_ERROR",
  "message": "Failed to export configuration",
  "timestamp": "2026-07-29T12:00:00Z"
}
```

### Import Configuration

**POST** `/config/import`

Restores tunnel definitions from an archive produced by `GET /config/export`.

Query parameters:
- `mode` — `merge` (default) updates and creates but never deletes; `replace`
  additionally deletes stored tunnels absent from the archive.
- `dry_run` — `true` returns the intended plan without writing anything. Any
  other value must parse as a boolean (`strconv.ParseBool`); an unparseable
  value is a `400`, not a silent no-op, so a typo can never turn a dry run
  into a real (and, combined with `mode=replace`, destructive) import.

A `dry_run=true` preview request and the later `dry_run=false` apply request
are two **independent** plans: the server recomputes the plan from live
storage on each request rather than reusing what the preview computed. A
tunnel created (or deleted) on the server in between is not reflected in the
plan that was previewed and confirmed, so with `mode=replace` a tunnel that
appears after the preview can still be deleted by the apply without ever
having shown up as a `DELETE` in the confirmed plan.

Tunnels are matched by **name**, trimmed of surrounding whitespace on both
sides of the comparison. A matched tunnel keeps its existing ID, owner and
creation time — only its configuration changes. An entry identical to what is
already stored is reported as `skip` and is not rewritten, so re-importing an
unmodified archive changes nothing and does not interrupt running tunnels.

A `200` response means **the archive was written to storage** — it does not
mean every tunnel reached its desired state. After a successful write the
server reconciles the running fleet against the restored `desired_status` in
the background, but `Manager.Reconcile` only returns an error when storage is
nil or the tunnel listing fails; a per-tunnel start/stop failure during
reconciliation is logged, not surfaced, and does not change the response. An
operator who reads `200` as "the fleet converged" would be wrong — check
tunnel status separately to confirm.

Import is **validate-then-write, not transactional**: `tunnel.Storage`
exposes no transaction API, so a write failure partway through does not roll
back the items already saved. Never describe an import as atomic. Merge mode
is idempotent, so re-running a failed or partial import converges.

Responses:

- `200 OK` — the report (see below). Present whether or not `dry_run` was set.
- `400 Bad Request` — malformed JSON body or an unknown `mode`/`dry_run`
  value. Body: `{"code": "BAD_REQUEST", "message": "...", "timestamp": "..."}`.
- `400 Bad Request` — the archive itself failed validation (bad `version`,
  an invalid entry field, and so on). Per-entry problems (an entry with a
  missing `remote_host`, an unknown `type`, and so on) are all collected and
  returned together, so a file with several bad entries can be fixed in one
  pass. That guarantee has one exception: a nil/missing archive or an
  unsupported `version` short-circuits validation with a single archive-level
  error (`index: -1`) before any entry is examined at all — in that case you
  get exactly one problem back per request, fix it, and re-submit to see the
  next layer of errors, if any:
  ```json
  {
    "code": "VALIDATION_ERROR",
    "message": "archive validation failed: 2 problem(s)",
    "details": [
      { "index": 0, "name": "prod-db", "field": "remote_host", "message": "must not be empty" },
      { "index": 1, "field": "name", "message": "must not be empty" }
    ]
  }
  ```
  `name` is tagged `json:"name,omitempty"` on the server's `EntryError`
  type, so an entry with no name (as in the second item above) omits the
  `name` key entirely rather than serializing it as `""`.
- `409 Conflict` — two **stored** tunnels have names that differ only by
  surrounding whitespace, making the archive entry ambiguous to match. This is
  a problem with server state, not with the uploaded file; the message names
  both tunnels, e.g. `stored tunnels "prod-db" and "prod-db " have names that
  differ only by surrounding whitespace; rename one before importing`.
- `413 Payload Too Large` — the request body exceeded the 10 MiB import
  limit. Body: `{"code": "PAYLOAD_TOO_LARGE", "message": "archive exceeds the 10 MiB import limit"}`.
- `500 Internal Server Error` — reading the currently stored tunnels failed
  **before planning began**: nothing was checked and nothing was written.
  This is the plain error envelope, with no `report` field at all:
  ```json
  {
    "code": "INTERNAL_ERROR",
    "message": "Failed to read existing tunnels",
    "timestamp": "2026-07-29T12:00:00Z"
  }
  ```
- `500 Internal Server Error` — planning succeeded but a write failed
  partway through apply, so some items already landed. The body carries the
  full report so the caller can see exactly which tunnels made it — this is
  the shape to check for a `report` field before assuming nothing happened:
  ```json
  {
    "code": "IMPORT_PARTIAL_FAILURE",
    "message": "import partially failed: 1 of 3 item(s)",
    "report": { "mode": "merge", "dry_run": false, "items": [ /* ... */ ], "created": 1, "updated": 1, "skipped": 0, "deleted": 0, "failed": 1 }
  }
  ```
- `503 Service Unavailable` — persistent storage is not configured.

Report body (also the shape of the `dry_run=true` response, and of the
`report` field in the `500` above):

```json
{
  "mode": "merge",
  "dry_run": false,
  "items": [
    { "action": "update", "name": "prod-db", "id": "9f1c…" },
    { "action": "create", "name": "staging-api", "id": "3a7e…" },
    { "action": "skip", "name": "socks-jump", "id": "b204…", "reason": "identical to stored tunnel" }
  ],
  "created": 1,
  "updated": 1,
  "skipped": 1,
  "deleted": 0,
  "failed": 0
}
```

`items[].error` is present only on a failed item within a `500` report.

## Multi-Hop Tunnels

Specify multiple hops to chain through bastion hosts:

```json
{
  "hops": [
    {
      "host": "jumphost.example.com",
      "port": 22,
      "user": "jump",
      "auth_method": "key",
      "key_id": "/path/to/jump/key"
    },
    {
      "host": "bastion.internal.example.com",
      "port": 22,
      "user": "bastion",
      "auth_method": "key",
      "key_id": "/path/to/bastion/key"
    }
  ]
}
```

## Authentication Methods

Currently supported:
- `key`: SSH private key authentication
- `agent`: SSH agent authentication

Coming soon:
- `password`: Password authentication
- `cert`: Certificate-based authentication

## Tunnel States

- `pending`: Tunnel created, connecting
- `active`: Tunnel connected and active
- `failed`: Connection failed
- `stopped`: Tunnel stopped

## Error Codes

- `400`: Bad Request - Invalid input
- `404`: Not Found - Tunnel doesn't exist
- `500`: Internal Server Error - Server-side error

## Rate Limiting

Currently no rate limiting is applied. Will be added in future versions.

## Examples

### cURL Examples

```bash
# Health check
curl http://localhost:8080/api/v1/health

# List tunnels
curl http://localhost:8080/api/v1/tunnels

# Create local tunnel
curl -X POST http://localhost:8080/api/v1/tunnels \
  -H "Content-Type: application/json" \
  -d '{
    "name": "prod-db",
    "type": "local",
    "local_port": 5432,
    "remote_host": "db.internal",
    "remote_port": 5432,
    "hops": [{
      "host": "bastion.example.com",
      "port": 22,
      "user": "deploy",
      "auth_method": "key",
      "key_id": "/home/user/.ssh/id_rsa"
    }]
  }'

# Get tunnel status
curl http://localhost:8080/api/v1/tunnels/YOUR-TUNNEL-ID/status

# Stop tunnel
curl -X DELETE http://localhost:8080/api/v1/tunnels/YOUR-TUNNEL-ID
```

### Go Client Example

```go
package main

import (
    "bytes"
    "encoding/json"
    "fmt"
    "net/http"
    "time"

    "github.com/craigderington/lazytunnel/pkg/types"
)

func main() {
    spec := types.TunnelSpec{
        Name:      "my-tunnel",
        Type:      types.TunnelTypeLocal,
        LocalPort: 5432,
        RemoteHost: "db.internal",
        RemotePort: 5432,
        Hops: []types.Hop{{
            Host: "bastion.example.com",
            Port: 22,
            User: "deploy",
            AuthMethod: types.AuthMethodKey,
            KeyID: "/path/to/key",
        }},
        AutoReconnect: true,
        KeepAlive: 30 * time.Second,
        MaxRetries: 3,
    }

    data, _ := json.Marshal(spec)
    resp, _ := http.Post(
        "http://localhost:8080/api/v1/tunnels",
        "application/json",
        bytes.NewBuffer(data),
    )
    defer resp.Body.Close()

    var result map[string]interface{}
    json.NewDecoder(resp.Body).Decode(&result)
    fmt.Printf("Tunnel ID: %s\n", result["id"])
}
```

## Server Configuration

Start the server with:

```bash
./server -addr :8080 -debug
```

Flags:
- `-addr`: HTTP server address (default: `:8080`)
- `-debug`: Enable debug logging

## Logging

All requests are logged with:
- Method
- Path
- Status code
- Duration

Example log output:
```
{"level":"info","method":"POST","path":"/api/v1/tunnels","status":201,"duration":45,"message":"HTTP request"}
```
