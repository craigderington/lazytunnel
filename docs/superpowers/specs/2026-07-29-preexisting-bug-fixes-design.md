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

### The destination is type-conditional

Fixing the wire format alone would repair only `--type local`, because
`CreateTunnelRequest` marked both `remoteHost` and `remotePort` `required` for
**every** type — so a dynamic SOCKS5 tunnel could never be created through the
API at all, even though `create.go`'s help text documents one and
`internal/backup/validate.go` already treats the destination as optional for
dynamic (such a tunnel could be *restored* but never *created*). The validator
therefore has to become type-aware:

- `remoteHost` is `required_if=Type local`. Only a local tunnel has a fixed
  destination host. Remote forwarding binds a port on the far side and forwards
  to localhost — `NewRemoteForwarder` in `internal/tunnel/forward.go` reads only
  `RemotePort` and `LocalPort` and never touches `RemoteHost` — and dynamic is a
  SOCKS5 proxy with no fixed destination. Requiring it for `remote` demanded a
  value nothing then read, and rejected every documented `--type remote`
  invocation.
- `remotePort` is `required_unless=Type dynamic`. A remote tunnel genuinely
  needs the port it binds.
- Format checks stay `omitempty`, so a value that *is* supplied is still
  validated.

`formatValidationError` gains cases for both `required_if` and
`required_unless`; without them the most common create-time mistake falls
through to the developer-facing `default:` branch and reports
`RemoteHost failed validation: required_if` instead of `RemoteHost is required`.

`create.go` keeps its own per-type guards so an obviously wrong invocation fails
client-side without a round trip, and transmits `remoteHost` for `local` and
`remote` while leaving it empty for `dynamic`.

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

### The WebSocket origin check

Wiring the allowlist through `corsMiddleware` alone leaves a hole: **WebSocket
upgrades are not subject to CORS**. A browser performs the handshake without a
preflight and without consulting `Access-Control-Allow-Origin`, so `/api/v1/ws`
stayed readable from any origin no matter what `allowed_origins` said. With
authentication disabled by default, any page the operator visited could open the
socket and read the live tunnel stream. Closing it is therefore part of this fix,
not a follow-up.

gorilla/websocket's `Upgrader.CheckOrigin` is the only hook where that allowlist
can be enforced. `(*Server).originAllowed` becomes a package-level
`originAllowed`, shared verbatim by the HTTP middleware and `CheckOrigin`, and
`NewWebSocketManager` takes the allowlist. `NewServer` normalizes the configured
list once and hands the same slice to both, so the two paths cannot drift.

`CheckOrigin` order matters, and getting it wrong is what made this the riskiest
part of the change:

1. **No `Origin` header** — a non-browser client (a Go websocket client, a CLI).
   Accepted. Browsers always send one, so this does not weaken the
   browser-facing protection.
2. **Same origin** — `url.Parse(Origin).Host == r.Host`. Accepted *before* the
   allowlist is consulted. This is the load-bearing case: unlike `fetch`, which
   omits `Origin` on a same-origin GET, a browser **always** sends `Origin` on a
   WebSocket handshake, including a same-origin one. That asymmetry is why
   `corsMiddleware` can treat "no Origin" as "not cross-origin" and this
   function cannot — without an explicit same-origin branch, the UI this very
   server hands out from `web/dist` is 403'd on its own WebSocket under the
   shipped deny-all default.
3. **Malformed `Origin`** — denied, before the allowlist, so neither a wildcard
   nor an entry matching the raw bytes can let it through.
4. **Otherwise** — the shared `originAllowed` decides.

**Accepted risk.** The same-origin branch trusts `r.Host`, which is
client-supplied, so an attacker who controls DNS for a name pointing at the
server can satisfy it (a DNS-rebinding shape, over plaintext `ws://` only). This
is not a new hole: the identical trick already defeats the HTTP API, because a
page at `http://evil:8080` fetching `http://evil:8080/api/...` is same-origin
from the browser's point of view and CORS never engages at all. It is also
verbatim gorilla/websocket's documented default behaviour. The real mitigation is
a `Host`-header allowlist, which this codebase has nowhere — the strongest
remaining follow-up from this work.

### Compatibility

`tunnelctl` is not a browser; CORS does not apply, and it sends no `Origin` on a
WebSocket handshake either. For browsers, HTTP and the WebSocket diverge:

- **Bundled UI at the server's own address** — `server.go` serves `web/dist`
  from the catch-all route, so both HTTP and the WebSocket are same-origin and
  need no configuration.
- **HTTP through a proxy** — a proxy that forwards `/api` to the backend makes
  the calls same-origin from the browser's point of view, so CORS never engages
  and nothing is needed. This is true of both `web/vite.config.ts`'s dev proxy
  and the bundled `deployments/docker/nginx.conf`.
- **The WebSocket through a proxy** — needs the browser-facing origin listed in
  `server.cors.allowed_origins` whenever the proxy rewrites `Host`, because the
  same-origin branch then cannot fire. Both shipped proxies do rewrite it:
  `vite.config.ts` sets `changeOrigin: true` (rewriting `Host` to
  `localhost:8080` while forwarding `Origin: http://<host>:5173` verbatim), and
  `nginx.conf` sets `proxy_set_header Host $host`, which additionally strips the
  port (browser at `http://localhost:3000`, server sees `Host: localhost`).
  Without the entry the handshake is 403'd; HTTP polling still works, so the
  symptom is the UI showing disconnected and reconnecting every few seconds
  rather than an outage.
- **A frontend on a genuinely different origin calling the API directly** —
  adds its origin to `server.cors.allowed_origins`, which now actually works.

Because neither shipped topology loads a config file, the allowlist is set for
them via the `LAZYTUNNEL_SERVER_CORS_ALLOWED_ORIGINS` environment variable:
enabled in `docker-compose.yml`, and present but commented out in the systemd
units that run the server, since the correct origin is deployment-specific.

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
