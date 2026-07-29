# CLI Reference

`tunnelctl` is the command-line interface for managing SSH tunnels through the lazytunnel server.

## Installation

```bash
go install github.com/craigderington/lazytunnel/cmd/tunnelctl@latest
```

Or build from source:

```bash
git clone https://github.com/craigderington/lazytunnel.git
cd lazytunnel
go build -o tunnelctl ./cmd/tunnelctl
```

## Global Flags

```bash
--server string    lazytunnel server address (default: http://localhost:8080)
--config string    config file (default: $HOME/.tunnelctl.yaml)
```

## Commands

### version

Print version information.

```bash
tunnelctl version
```

Output:
```
tunnelctl version dev
lazytunnel SSH Tunnel Manager CLI
```

### create

Create a new SSH tunnel.

```bash
tunnelctl create [flags]
```

**Flags:**
```
--name string          tunnel name (required)
--type string          tunnel type: local, remote, or dynamic (default: local)
--local-port int       local port to bind
--remote-host string   remote host:port (for local tunnels)
--remote-port int      remote port (for remote tunnels)
--hop stringArray      SSH hop in format host:port (can specify multiple)
--user string          SSH username (default: $USER)
--key string           path to SSH private key
--auto-reconnect       automatically reconnect on failure (default: true)
--keep-alive int       SSH keep-alive interval in seconds (default: 30)
--max-retries int      maximum reconnection attempts (default: 3)
```

**Examples:**

Local port forwarding:
```bash
tunnelctl create \
  --name prod-db \
  --type local \
  --local-port 5432 \
  --remote-host db.internal.example.com:5432 \
  --hop bastion.example.com:22 \
  --user deploy \
  --key ~/.ssh/id_rsa
```

SOCKS5 proxy:
```bash
tunnelctl create \
  --name socks \
  --type dynamic \
  --local-port 1080 \
  --hop jumphost.example.com:22 \
  --user admin \
  --key ~/.ssh/id_rsa
```

Remote port forwarding:
```bash
tunnelctl create \
  --name expose-local \
  --type remote \
  --local-port 8080 \
  --remote-port 9090 \
  --hop server.example.com:22 \
  --user deploy \
  --key ~/.ssh/id_rsa
```

Multi-hop tunnel:
```bash
tunnelctl create \
  --name multi-hop \
  --type local \
  --local-port 5432 \
  --remote-host db.private:5432 \
  --hop jumphost.example.com:22 \
  --hop bastion.internal:22 \
  --user deploy \
  --key ~/.ssh/id_rsa
```

### list

List all active tunnels.

```bash
tunnelctl list
```

Output:
```
ID        NAME      TYPE     STATE    CREATED
──        ────      ────     ─────    ───────
550e8400  prod-db   local    active   2026-01-09 12:00
7c9e6679  socks     dynamic  active   2026-01-09 12:05

Total: 2 tunnel(s)
```

### status

Get detailed status for a specific tunnel.

```bash
tunnelctl status [tunnel-id-or-name]
```

Example:
```bash
tunnelctl status 550e8400
```

Output:
```
Tunnel Status: 550e8400
─────────────────────────────
  State: active
  Connected: 2026-01-09T12:00:00Z
  Bytes Sent: 1.0 MB
  Bytes Received: 2.0 MB
  Retry Count: 0
```

### stop

Stop an active tunnel.

```bash
tunnelctl stop [tunnel-id-or-name]
```

Example:
```bash
tunnelctl stop prod-db
```

Output:
```
✓ Tunnel stopped: prod-db
```

### export

Export every tunnel definition on the server as a versioned JSON archive.

```bash
tunnelctl export
tunnelctl export -o tunnels.json
```

**Flags:**
```
-o, --output string   write the archive to FILE instead of stdout (created with 0600)
```

Output goes to stdout by default, so it pipes and redirects cleanly — useful
in a cron job or when committing the archive to a config repository. When
`-o` is used, the file is created with `0600` permissions, and an existing
file at that path is `chmod`'d to `0600` too, so re-running the export into
the same path never leaves it world- or group-readable. When `-o` is used,
the confirmation line `Wrote <file> (<n> bytes)` is printed to **stderr**, not
stdout — that matters if you're piping stdout elsewhere, since the
confirmation won't be mixed into the archive data.

The archive contains hostnames, SSH usernames and key paths, but no key
material.

`tunnelctl export` and `tunnelctl import` do not send an `Authorization`
header, and there is no flag to supply a bearer token — `export.go` and
`import.go` issue a bare `http.Get`/`http.Post`. These commands therefore only
work against a server with authentication disabled (no JWT secret
configured), even though `docs/api-reference.md` documents both routes as
requiring auth when a secret is set.

### import

Restore tunnel definitions from an archive produced by `tunnelctl export`.

```bash
tunnelctl import tunnels.json
tunnelctl import --dry-run tunnels.json
tunnelctl import --replace tunnels.json
tunnelctl import --replace --yes tunnels.json
```

**Flags:**
```
--replace     delete tunnels not present in the archive, so the server mirrors the file exactly
--dry-run     print what would change and write nothing
-y, --yes     skip the confirmation prompt for deletions
```

By default the import merges: tunnels matching by name are updated in place,
missing ones are created, and anything not mentioned in the archive is left
alone. Merging never deletes.

Every run previews first, then applies — the command always issues a
`dry_run=true` request to compute and print the plan before deciding whether
to write anything. Output is labelled `Plan:` for the preview and `Applied:`
for the real request:

```
$ tunnelctl import tunnels.json
Plan:
  update  prod-db
  create  staging-api
  skip    socks-jump   identical to stored tunnel

1 created, 1 updated, 1 skipped, 0 deleted
Applied:
  update  prod-db
  create  staging-api
  skip    socks-jump   identical to stored tunnel

1 created, 1 updated, 1 skipped, 0 deleted
```

`--dry-run` stops after printing `Plan:` and writes nothing, printing a final
`Dry run: nothing was written.` line so that's unambiguous from the output
alone:

```
$ tunnelctl import --dry-run tunnels.json
Plan:
  update  prod-db
  create  staging-api
  skip    socks-jump   identical to stored tunnel

1 created, 1 updated, 1 skipped, 0 deleted
Dry run: nothing was written.
```

With `--replace`, deletions show as `DELETE` in the plan, and unless `-y` /
`--yes` is given, they are confirmed before anything is applied:

```
$ tunnelctl import --replace tunnels.json
Plan:
  update  prod-db
  DELETE  old-bastion  not present in archive

0 created, 1 updated, 0 skipped, 1 deleted

--replace deletes 1 tunnel(s). Continue? [y/N]
```

Answering `y` or `Y` confirms the deletions; anything else (including a bare
Enter) prints `Aborted.` and exits `0` without writing — the confirmation
check lowercases the answer before comparing it. If stdin is not interactive
(for example, a cron job) and deletions are pending, `import` does not guess
at an answer: it exits non-zero with a message pointing at `--yes`, rather
than silently treating end-of-input as a decline.

Import is validate-then-write, not transactional — there is no rollback if a
write fails partway through. On a partial failure the command still prints an
`Applied:` report naming exactly which tunnels landed, then exits non-zero.
Re-running an unmodified or previously-failed archive is safe: unchanged
entries are reported as `skip`, so a merge-mode import converges.

The previewed `Plan:` and the applied request are two **independent** plans —
the server recomputes the plan from live storage on each request, it does not
reuse the preview's computed plan. A tunnel created (or deleted) on the
server between the preview and the confirmed apply is not reflected in what
you confirmed, so with `--replace` a tunnel that appeared after the preview
was printed can still be deleted by the apply without ever showing up in the
plan you approved.

## Configuration File

Create `~/.tunnelctl.yaml` to set defaults:

```yaml
server: http://localhost:8080

# Default tunnel settings
defaults:
  user: deploy
  key: ~/.ssh/id_rsa
  auto_reconnect: true
  keep_alive: 30
  max_retries: 3

# Named tunnel configurations
tunnels:
  prod-db:
    type: local
    local_port: 5432
    remote_host: db.internal.example.com:5432
    hops:
      - bastion.example.com:22

  socks:
    type: dynamic
    local_port: 1080
    hops:
      - jumphost.example.com:22
```

With config file, create tunnels easily:
```bash
# Uses configuration from ~/.tunnelctl.yaml
tunnelctl create --name prod-db
```

## Environment Variables

All settings can be configured via environment variables:

```bash
export TUNNELCTL_SERVER=http://localhost:8080
export TUNNELCTL_USER=deploy
export TUNNELCTL_KEY=~/.ssh/id_rsa

tunnelctl create --name my-tunnel ...
```

## Exit Codes

- `0`: Success
- `1`: Error

## Common Workflows

### Development Database Access

```bash
# Start tunnel
tunnelctl create \
  --name dev-db \
  --type local \
  --local-port 5432 \
  --remote-host db.dev.internal:5432 \
  --hop bastion.dev:22

# Connect to database
psql -h localhost -p 5432 -U postgres

# Stop when done
tunnelctl stop dev-db
```

### Web Browsing through Proxy

```bash
# Start SOCKS5 proxy
tunnelctl create \
  --name browse \
  --type dynamic \
  --local-port 1080 \
  --hop proxy.example.com:22

# Configure browser to use localhost:1080 as SOCKS5 proxy
# Or use with curl
curl --socks5 localhost:1080 https://example.com

# Stop when done
tunnelctl stop browse
```

### Expose Local Service

```bash
# Start remote tunnel
tunnelctl create \
  --name webhook \
  --type remote \
  --local-port 3000 \
  --remote-port 8080 \
  --hop server.example.com:22

# Now server.example.com:8080 forwards to your localhost:3000

# Stop when done
tunnelctl stop webhook
```

### Monitor Tunnels

```bash
# List all tunnels
tunnelctl list

# Check specific tunnel
tunnelctl status prod-db

# Monitor in loop
watch -n 5 tunnelctl list
```

## Troubleshooting

### Connection refused

```bash
# Check server is running
curl http://localhost:8080/api/v1/health

# Check server address
tunnelctl --server http://localhost:8080 list
```

### SSH key permission denied

```bash
# Check key permissions
chmod 600 ~/.ssh/id_rsa

# Verify key path
ls -la ~/.ssh/id_rsa

# Use absolute path
tunnelctl create --key /home/user/.ssh/id_rsa ...
```

### Tunnel not found

```bash
# List all tunnels to get exact ID
tunnelctl list

# Use full tunnel ID, not partial
tunnelctl status 550e8400-e29b-41d4-a716-446655440000
```

## Tips

1. **Use short IDs**: First 8 characters of tunnel ID usually sufficient
2. **Config file**: Set up `~/.tunnelctl.yaml` for frequently used tunnels
3. **Shell aliases**: Create aliases for common commands
   ```bash
   alias tctl='tunnelctl'
   alias tls='tunnelctl list'
   ```
4. **Tab completion**: (Coming soon) Shell completion for commands and tunnel IDs

## See Also

- [API Reference](api-reference.md)
- [Local Forwarding Guide](local-forwarding.md)
- [Remote Forwarding Guide](remote-forwarding.md)
- [Dynamic Forwarding Guide](dynamic-forwarding.md)
