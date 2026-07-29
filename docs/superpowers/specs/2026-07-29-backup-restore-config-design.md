# Backup / Restore Config — Design

**Date:** 2026-07-29
**Status:** Approved, ready for planning

## Summary

Export every tunnel definition to a portable, versioned JSON file, and restore it —
either merging into an existing fleet or replacing it wholesale — from the CLI, the
REST API, or the web UI.

## Scope

**In scope:** tunnel definitions (the SQLite `tunnels` table).

**Out of scope:**

- UI preferences (API base URL, theme, default auto-reconnect, drag order). These live
  in browser `localStorage` via the zustand stores in `web/src/store/`, not on the
  server.
- `config.yaml`. Managed by config management / systemd, not by the application.
- Raw `.db` snapshots. A portable document beats an opaque byte copy for diffing,
  hand-editing, and cross-version restores.

## Constraints discovered in the existing code

These three facts shaped the design and are load-bearing. All were verified by reading
the source, not assumed.

1. **`Manager.Update` refuses to modify a running tunnel.** `internal/tunnel/manager.go:190`
   returns `"tunnel %s must be stopped before editing"` when state is `Active` or
   `Pending`. A merge that changes a live tunnel must stop → update → restart.

2. **`Manager.Create` always connects.** `internal/tunnel/manager.go:168` fires
   `connectTunnel` unconditionally when the tunnel runs on this node, and
   `handleCreateTunnel` never sets `DesiredStatus`. Importing via `Create` would start
   every restored tunnel regardless of its recorded desired status.

3. **`LoadFromStorage` is unsafe on a live manager.** `internal/tunnel/manager.go:111`
   blindly overwrites `m.tunnels[id]` with a fresh stopped `Tunnel`, orphaning any
   running SSH goroutine. It is a boot-time-only function and cannot be reused for
   restore.

Consequence: the substantive work is not JSON serialization. It is reconciling desired
state against a running fleet.

## Architecture

New package `internal/backup`. No knowledge of HTTP or SSH.

| File | Contents |
|---|---|
| `archive.go` | Envelope + entry DTOs, `SchemaVersion = 1` |
| `export.go` | `Export(ctx, Store, source) (*Archive, error)` |
| `plan.go` | `Plan(current []*types.TunnelSpec, *Archive, Mode) (*ImportPlan, error)` — pure, zero I/O |
| `apply.go` | `Apply(ctx, Store, *ImportPlan) (*Report, error)` |

It depends on a narrow local interface rather than `tunnel.Storage`:

```go
type Store interface {
    List(ctx context.Context) ([]*types.TunnelSpec, error)
    Save(ctx context.Context, spec *types.TunnelSpec) error // INSERT OR REPLACE: create+update
    Delete(ctx context.Context, id string) error
}
```

Three methods, so a test fake is trivial and every diff rule is testable without a
database. `*storage.SQLiteStore` already satisfies it.

One new method on `Manager` — `Reconcile(ctx)` — applies stored state to the running
fleet. See below.

## File format

Versioned JSON envelope:

```json
{
  "version": 1,
  "exported_at": "2026-07-29T08:40:12Z",
  "source": "lazytunnel/1.0.0",
  "tunnels": [
    {
      "id": "9f1c2b4e-7a30-4d51-9c8f-2e6b1a0d3f55",
      "name": "prod-db",
      "owner": "admin",
      "type": "local",
      "desired_status": "active",
      "local_port": 5432,
      "local_bind_address": "127.0.0.1",
      "remote_host": "db.internal",
      "remote_port": 5432,
      "auto_reconnect": true,
      "keep_alive_seconds": 30,
      "max_retries": 5,
      "hops": [{ "host": "bastion", "port": 22, "user": "deploy", "auth_method": "key" }]
    }
  ]
}
```

The archive uses **its own DTO, not `types.TunnelSpec`**, for two reasons:

- `TunnelSpec.KeepAlive` is a `time.Duration`, marshalling to raw nanoseconds
  (`30000000000`) — unreadable in a file meant to be hand-edited. The DTO uses
  `keep_alive_seconds`, matching how SQLite already stores the value.
- `TunnelSpec.Auth` and `.Policy` **are never persisted** — `Save`/`Get` in
  `internal/storage/sqlite.go` do not reference them. Exporting `TunnelSpec` directly
  would emit an empty `auth: {}`, implying the backup carries credentials it does not
  have. The DTO omits them, so the file is honest about what a restore returns.

The decoupling also means internal `TunnelSpec` refactors cannot silently break the
on-disk format. The `version` field is what lets a future schema change import an old
file instead of erroring.

**Field semantics:**

- `source` is the exporting server's version string (the same `version` the binary is
  built with). Informational only — never parsed on import.
- `exported_at` is informational only. Import never compares it against anything.
- `owner` is exported, and on import is used only when creating a tunnel that does not
  already exist. An existing tunnel keeps its current `owner` — a restore must not
  silently reassign ownership. If an entry has no `owner`, the importing user's
  username is used (`api-user` when auth is disabled), matching `handleCreateTunnel`.
- `desired_status` defaults to `stopped` when absent, matching the DB column default.
  Any value other than `active` or `stopped` is a validation error.
- `id` is optional. An archive hand-written without IDs imports fine; every entry gets
  a fresh `uuid`.

## Restore semantics

### Modes

- **`merge` (default)** — update matching tunnels in place, create missing ones, leave
  everything else untouched. Never deletes.
- **`replace`** — as merge, plus delete stored tunnels absent from the archive, so the
  fleet exactly mirrors the file. Opt-in via `--replace` / a UI checkbox, and prompts
  before deleting.

### Matching and identity

Match on **`name`** (the `UNIQUE` column in the `tunnels` table), not ID.

- **Name matches an existing tunnel** → keep its existing ID and `CreatedAt`, overwrite
  the rest, bump `UpdatedAt`. Keeping the ID matters because `orderStore` keys the drag
  order on tunnel ID; a re-import must not scramble the list.
- **No name match** → reuse the archive's ID if free, otherwise mint a fresh `uuid`.
  Reuse makes a bare-metal restore faithful; the fallback stops an ID collision from
  clobbering an unrelated tunnel.
- **Spec identical to what is stored** → `ActionSkip`. No write, no restart. This makes
  re-import idempotent and stops a routine restore from bouncing healthy connections.

"Identical" is defined precisely, since it gates whether a live tunnel gets bounced:
convert the stored `TunnelSpec` to an archive DTO and compare that against the incoming
DTO with `reflect.DeepEqual`. Comparing DTOs rather than specs means the fields the
archive does not carry (`Auth`, `Policy`) cannot produce a spurious difference, and
`CreatedAt` / `UpdatedAt` are excluded by construction — they are not DTO fields.
`ID` and `Owner` are zeroed on both sides before comparing, because they are resolved by
the matching rules above rather than taken from the file. `desired_status` **is** part of
the comparison: a tunnel whose only change is stopped → active must not be skipped.

`agent_id` is preserved verbatim. A tunnel assigned to an agent that is not currently
registered simply stays unassigned until that agent appears — the existing coordinator
path already handles this.

### Plan types

```go
type Action string // create | update | skip | delete

type PlanItem struct {
    Action Action
    Name   string
    ID     string
    Spec   *types.TunnelSpec // nil for delete
    Reason string            // what changed, or why skipped
}

type ImportPlan struct {
    Mode  Mode
    Items []PlanItem
}
```

`Apply` returns a `Report` of the same shape recording what actually happened, so the
CLI's printed output and the UI's preview render from one structure.

## Reconcile

```go
func (m *Manager) Reconcile(ctx context.Context) error
```

Computes the delta under `RLock`, **releases the lock**, then acts through the existing
public `Start` / `Stop` / `Delete` — which acquire the lock themselves, so holding it
across the delta would deadlock.

- in storage, not in memory → add; start if `desired_status=active` and it runs on this node
- in memory, not in storage → stop, remove
- in both, spec changed → stop → swap spec → start if desired active (the ordering
  `Manager.Update` currently rejects outright, per constraint 1)
- unchanged → left completely alone

This also retires the `LoadFromStorage` foot-gun from constraint 3: boot can call
`Reconcile` instead of a function that orphans live goroutines.

Restoring `desired_status` from the archive is what makes disaster recovery a single
step — tunnels recorded as active come back up, tunnels recorded as stopped stay down.

## Surface

### API

Both routes go on the existing `protected` subrouter in `internal/api/server.go`, so
auth and rate limiting apply unchanged.

- `GET /api/v1/config/export` → archive JSON, plus
  `Content-Disposition: attachment; filename="lazytunnel-backup-<ts>.json"` so the UI
  download is a plain link with no blob handling.
- `POST /api/v1/config/import?mode=merge|replace&dry_run=true` → report. Body is the
  archive. `dry_run=true` computes and returns the plan without writing.

### CLI

- `tunnelctl export [-o FILE]` — stdout by default, so it pipes and cron-redirects.
- `tunnelctl import FILE [--replace] [--dry-run] [--yes]` — runs a dry run first when
  `--replace` is set, prints the plan, and prompts before deleting unless `--yes`.

```
$ tunnelctl import tunnels.json
  update  prod-db      (local  5432)
  create  staging-api  (local  8081)
  skip    socks-jump   (unchanged)
  3 tunnels, 1 created, 1 updated
```

### Web UI

A Backup section in `web/src/components/Settings.tsx`: a download button, and restore
via file picker that runs `dry_run` first and shows the diff before committing. Replace
mode is a checkbox with a warning about deletions.

## Error handling

Every entry is validated **before anything is written**: name non-empty, type in
`{local, remote, dynamic}`, ports in 1–65535, at least one hop, hop host and user
non-empty, and no duplicate names within the archive. All errors are collected and
returned together so the file can be fixed in one pass.

**On the atomicity guarantee:** `tunnel.Storage` exposes no transaction API (verified —
the interface at `internal/tunnel/manager.go:14` has no `Begin`/`Tx`). This is
therefore validate-then-write, *not* rollback. If a write fails mid-apply, the report
names exactly which items landed and the call returns 500; because merge is idempotent,
re-running converges. Stating this beats claiming atomicity the storage layer cannot
deliver.

Other cases:

- Unknown or newer `version` → 400 with a clear message. Version 1 only, for now.
- A tunnel that fails to *connect* after import is not an import failure. The spec is
  stored, status shows failed, and normal retry applies.

## Security

The archive contains hostnames, SSH usernames, and key *paths* — no key material, since
`AuthConfig` is not persisted. Not secret, but not public either: it reveals filesystem
layout and account names. Export stays behind the same auth and rate limiting as every
other protected route, and the file is written with `0600` when `-o` is used.

## Testing

`plan.go` being pure is the point — the diff rules are table-testable with no database,
no HTTP, and no SSH.

- **`plan_test.go`** — merge creates/updates/skips; replace deletes; name collision keeps
  existing ID; free archive ID reused; colliding archive ID regenerated; duplicate names
  rejected; empty archive; re-import of an unchanged archive produces all-skip; an
  entry differing only in `desired_status` is *not* skipped; an existing tunnel keeps
  its `owner` on update; entries with no `id` and no `desired_status` import cleanly.
- **`archive_test.go`** — export → import → export round-trips stably; version 0 and
  version 2 both rejected.
- **`manager_reconcile_test.go`** — fake `Store`; tunnels carry an `agent_id` so nothing
  attempts real SSH. Asserts add, remove, spec-change restart, and that unchanged
  tunnels are not bounced.
- **API handler tests** — following the existing style in `internal/api/auth_test.go`.
- **CLI** — `import --dry-run` writes nothing.

## Build order

1. `internal/backup`: archive DTOs + `Export` + `Plan` (+ tests). No wiring yet.
2. `Manager.Reconcile` (+ tests).
3. `Apply`, wired to `Reconcile`.
4. API handlers and routes.
5. CLI `export` / `import`.
6. Web UI Backup section.
7. Docs: `docs/api-reference.md` and `docs/cli-reference.md`.
