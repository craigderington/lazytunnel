# Three Pre-existing Bug Fixes — Design

**Date:** 2026-07-29
**Status:** Approved, ready for planning

## Summary

Fix three defects found during the backup/restore review that predate that work
and are unrelated to it: `tunnelctl create` cannot create a tunnel, CORS is
wide open with its configuration silently ignored, and `config.example.yaml`
documents a schema the code does not read.

Each fix is paired with a test that prevents the same class of drift recurring.

## Scope

Three independent fixes, one spec. They share a theme — the CLI, the CORS
middleware, and the example config each disagree with the code they claim to
describe — and each is small enough that separate specs would be overhead.

**Out of scope:** rate limiting (`RateLimiter.Middleware` is never registered on
any router). It is a genuine gap but a whole-API change, and `docs/api-reference.md`
already documents its absence rather than claiming otherwise.

---

## Fix 1 — `tunnelctl create` sends a body the API cannot read

### The defect

`internal/cli/create.go:146` builds a `types.TunnelSpec` and marshals it at
`:162`, POSTing it to a handler that decodes `api.CreateTunnelRequest`. The two
disagree in **two independent ways**, and both must be fixed.

**Key names.** `TunnelSpec` uses snake_case, `CreateTunnelRequest` camelCase:

| Field | Sent | Expected |
|---|---|---|
| local port | `local_port` | `localPort` |
| remote host | `remote_host` | `remoteHost` |
| remote port | `remote_port` | `remotePort` |
| bind address | `local_bind_address` | `localBindAddress` |
| auto-reconnect | `auto_reconnect` | `autoReconnect` |
| keepalive | `keep_alive` | `keepAlive` |
| max retries | `max_retries` | `maxRetries` |
| agent | `agent_id` | `agentId` |

The fields decode to zero values and the `required` validator on `remoteHost`
and `remotePort` rejects the request.

Hop fields are **not** affected — `types.Hop` and `api.HopReq` both use
snake_case (`host`, `port`, `user`, `auth_method`, `key_id`). That is why the
failure presents as a validation error rather than an obviously malformed body,
and why it was initially misdiagnosed as a flag-parsing problem in `create.go`.
The parsing at `create.go:131-140` is correct.

**Value encoding.** `TunnelSpec.KeepAlive` is a `time.Duration`, which marshals
to nanoseconds; `CreateTunnelRequest.KeepAlive` is an `int` in seconds carrying
`validate:"min=0,max=300"`. So `--keep-alive 30` is transmitted as
`30000000000` and fails validation *even if the key name matched*. Fixing only
the names would leave the command broken.

### The fix

`create.go` builds a local request struct with the API's exact JSON tags and
second-based keepalive, then marshals that instead of the spec. This matches the
established pattern in the package: `list.go` declares `tunnelListItem` and
`import.go` declares `importReport`/`importItem`, both mirroring server shapes
rather than importing them.

The struct carries exactly the fields the CLI has flags for — `name`, `type`,
`hops`, `localPort`, `localBindAddress`, `remoteHost`, `remotePort`,
`autoReconnect`, `keepAlive`, `maxRetries`. Hops keep their existing snake_case
tags, since those already agree.

`agentId` is deliberately omitted: `create.go` has no flag for it, so there is
no value to send, and an always-empty field would be noise.

---

## Fix 1b — `tunnelctl create --local-bind-address`

### The defect

`create.go` has no flag for the bind address (see `create.go:61-71`), so a
CLI-created tunnel is stored with an empty value, which
`internal/tunnel/forward.go:105` and `:618` both treat as `0.0.0.0`. Every
CLI-created tunnel therefore listens on all interfaces with no way to ask for
loopback.

The web UI has the same gap from the other direction — it never sends
`localBindAddress` at all — so in practice every tunnel created by any path
today binds all interfaces, even though the schema at
`internal/storage/sqlite.go:52` declares `local_bind_address TEXT DEFAULT '127.0.0.1'`.
That column default never applies, because `Save` always supplies the column
explicitly.

### The fix

Add `--local-bind-address`, defaulting to **`127.0.0.1`**.

The default is a deliberate behaviour choice, and it is safe to make because
`tunnelctl create` has never worked: the JSON contract defect in Fix 1 means no
working script can exist today, so there is no established behaviour to
preserve. Given a free choice, loopback is right — it matches the schema's own
declared intent, it matches the deny-by-default posture taken for CORS, and
widening exposure should be something an operator asks for rather than
something they get by not knowing about a flag.

Operators who want the old effective behaviour pass
`--local-bind-address 0.0.0.0` explicitly.

### Known inconsistency this introduces

After this change, a tunnel created by the CLI binds loopback while one created
through the web UI still binds all interfaces, because the UI sends no value.
That divergence is real and should be closed by having the UI send an explicit
bind address, but doing so changes behaviour for an existing, working UI flow
and belongs in its own change with its own decision about defaults. It is
recorded here so the difference is known rather than discovered.

### Testing

The contract test covers the field like any other. Add a CLI test asserting the
flag's default is `127.0.0.1` and that an explicit `0.0.0.0` is transmitted
unchanged — the second half matters because it is the escape hatch.

### The guard

`internal/cli/create_contract_test.go` marshals the CLI's request struct,
decodes the bytes into a real `api.CreateTunnelRequest`, and asserts every field
survives the round trip. It then runs the API's own `ValidateRequest` on the
decoded value.

That second step is what makes the test catch both defect classes: a renamed key
fails the field-survival assertions, and a wrong encoding (such as nanoseconds
in a seconds field) fails validation exactly as the server would.

`internal/api` does not import `internal/cli`, so the test-only dependency
introduces no import cycle. Verified before adopting this approach.

---

## Fix 2 — CORS is hardcoded open and its configuration is dead

### The defect

`internal/api/server.go:257` hardcodes `Access-Control-Allow-Origin: *` on every
response. `config.Server.CORS.AllowedOrigins` exists in `internal/config/config.go:28`,
defaults to `["*"]`, and is never read by anything.

The middleware does not set `Access-Control-Allow-Credentials`, so browsers will
not send cookies cross-origin. The real exposure is therefore the specific
combination of wildcard origins **and** authentication disabled — which is the
default, since `cmd/server/main.go` only builds the auth middleware when a JWT
secret is configured. In that state any page the operator visits can drive the
full API from their browser.

### The fix

Wire the existing configuration through, and change its default to deny.

- `api.Config` gains `AllowedOrigins []string`.
- `cmd/server/main.go` passes `cfg.Server.CORS.AllowedOrigins`.
- The default in `config.go` changes from `["*"]` to an empty list.

`corsMiddleware` behaviour, in order:

1. **No `Origin` request header** — not a cross-origin request. Emit no CORS
   headers and proceed. This also corrects today's behaviour of stamping `*`
   onto same-origin responses.
2. **`Origin` present and the allowlist contains `*`** — echo `*`.
3. **`Origin` present and exactly matches an allowlist entry** — echo that
   origin, and set `Vary: Origin` so a shared cache cannot serve one origin's
   response to another.
4. **No match** — emit no `Access-Control-Allow-Origin`. A preflight still
   returns 200; without the header the browser blocks the request.

Matching is exact, case-sensitive string comparison against the configured list.
No wildcard subdomain patterns — YAGNI, and pattern matching is where CORS
implementations usually acquire their bypasses. If the list contains both `*`
and specific origins, rule 2 wins and `*` is echoed; the wildcard is not
silently narrowed.

`Access-Control-Allow-Methods` and `Access-Control-Allow-Headers` keep their
current values and are emitted only alongside an allow-origin header, since
they are meaningless without one.

The server logs the effective policy once at startup, at info level, so the
active posture is never a mystery.

### Compatibility

Nothing in the repository breaks:

- The bundled web UI is served by the same server (`server.go` serves
  `web/dist` from the catch-all route), so it is same-origin.
- `npm run dev` proxies `/api` to `:8080` via `web/vite.config.ts`, so the
  browser also sees same-origin and CORS never engages.
- `tunnelctl` is not a browser; CORS does not apply.

The only affected configuration is a frontend hosted on a different origin
calling the API directly. Such an operator adds their origin to
`server.cors.allowed_origins`, which now actually works.

---

## Fix 3 — `config.example.yaml` documents a schema that does not exist

### The defect

The file documents `server.host`, `server.port`, `database.host`,
`database.port`, `database.database`, `database.user`, a `kms:` block, and
`auth.provider`. `internal/config/config.go` reads none of them. An operator
copying the example gets a server that ignores most of what they wrote.

### The fix

Rewrite the file to exactly the schema `Load()` reads: `server.addr`,
`server.tls_cert`, `server.tls_key`, `server.cors.allowed_origins`,
`database.path`, `auth.jwt_secret`, `auth.jwt_secret_env`,
`auth.token_expiration`, `auth.auto_start_tunnels`, `logging.level`,
`logging.format`.

Document the newly-live `cors` block, including that an empty list means no
cross-origin access and that `["*"]` restores the old behaviour.

### The guard

A test in `internal/config` calls `Load()` against the real
`config.example.yaml` and asserts the parsed values. If the file drifts from the
schema again, or the example stops parsing, the test fails.

---

## Testing

- **`internal/cli/create_contract_test.go`** — CLI request struct round-trips
  into `api.CreateTunnelRequest` with every field intact, and the decoded value
  passes `ValidateRequest`. Covers both the key-name and value-encoding classes.
- **`internal/api`** — a table test over (`Origin` header, configured allowlist)
  asserting the exact response headers, covering: no Origin header; wildcard
  allowlist; exact match with `Vary: Origin` present; non-matching origin with
  no allow-origin header; empty allowlist denying everything; and an `OPTIONS`
  preflight in both the allowed and denied cases.
- **`internal/config`** — `Load()` parses the real `config.example.yaml` and
  yields the expected values, including the CORS block.

## Build order

1. Fix 1 — CLI request struct and contract test. Self-contained.
2. Fix 1b — `--local-bind-address` flag. Immediately after Fix 1, because it
   adds a field to the struct that fix introduces.
3. Fix 2 — CORS config plumbing, middleware rewrite, table test.
4. Fix 3 — rewrite `config.example.yaml` and add its load test. Ordered last
   because it documents the CORS default that Fix 2 establishes.

## Documentation

`docs/cli-reference.md` gains the `--local-bind-address` flag in the `create`
section, stating the `127.0.0.1` default and that `0.0.0.0` opts into all
interfaces. Folded into Fix 1b rather than given its own task.
