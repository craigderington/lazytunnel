# Three Pre-existing Bug Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Repair `tunnelctl create`, give it a bind-address flag, wire up the dead CORS allowlist with a deny-by-default policy, and make `config.example.yaml` describe the schema the code actually reads.

**Architecture:** Four independent fixes, each paired with a test that prevents the same class of drift recurring. The CLI gets a request DTO mirroring the API's wire format plus a contract test that decodes it into the real server type and runs the server's own validator. The CORS middleware reads a configured allowlist instead of a hardcoded wildcard. The example config gets a test that loads the real file.

**Tech Stack:** Go 1.24, `spf13/cobra` + `viper`, `gorilla/mux`, `go-playground/validator`.

**Spec:** `docs/superpowers/specs/2026-07-29-preexisting-bug-fixes-design.md`

## Global Constraints

- No new Go dependencies. Everything needed is already in `go.mod`.
- Tests run with `go test ./...`. The `internal/tunnel` package takes ~8s; that is normal.
- `internal/api` does NOT import `internal/cli`, so `internal/cli` may import `internal/api` in a `_test.go` file. Verified. Do not create an import in non-test code.
- CORS matching is exact, case-sensitive string comparison. No wildcard subdomain patterns.
- If the CORS allowlist contains both `*` and specific origins, `*` wins and is echoed. The wildcard is never silently narrowed.
- The CORS default becomes an **empty list** (deny all cross-origin). The previous default was `["*"]`.
- `--local-bind-address` defaults to `127.0.0.1`.

### Two facts that shaped this plan

1. **`tunnelctl create` has never worked.** `create.go:162` marshals a `types.TunnelSpec` into a handler decoding `api.CreateTunnelRequest`. There are TWO mismatches: snake_case vs camelCase top-level keys, AND `KeepAlive` being a `time.Duration` (nanoseconds) where the API expects `int` seconds with `validate:"max=300"`. Fixing only the key names leaves the command broken. Because it has never worked, no existing script depends on its behaviour, which is why Task 2 can freely choose a safer default.

2. **Hop fields already agree.** `types.Hop` and `api.HopReq` both use snake_case (`host`, `port`, `user`, `auth_method`, `key_id`). Only top-level fields are wrong. Do not "fix" the hop tags.

---

### Task 1: Repair the create request wire format

**Files:**
- Modify: `internal/cli/create.go` (add a request type, extract a builder, change the marshal)
- Modify: `internal/api/validation.go:54-55` (make the destination conditional on tunnel type)
- Test: `internal/cli/create_contract_test.go`

**Interfaces:**
- Consumes: `api.CreateTunnelRequest`, `api.HopReq`, `api.ValidateRequest` from `internal/api/validation.go`; `types.Hop` from `pkg/types`.
- Produces: type `createTunnelRequest`; function `buildCreateRequest() (createTunnelRequest, error)`. Task 2 adds a field to both.

**Two rulings from the project owner that extend this task beyond the spec:**

1. **`remoteHost`/`remotePort` must become conditional on tunnel type.** They are
   currently `validate:"required"` for every type, so a `dynamic` tunnel — a
   SOCKS5 proxy with no destination — can never be created, even though
   `create.go`'s own help text documents doing exactly that. Without this,
   repairing the wire format would fix `--type local` and leave `--type dynamic`
   broken. `internal/backup/validate.go` already treats the destination as
   optional for dynamic tunnels, so this also stops the two validators
   disagreeing about which tunnels are legal.

2. **`--remote-host` must be transmitted for `remote` tunnels.** The original
   code leaves `remHost` empty for every non-local type
   (`create.go:141-143`), silently discarding the flag. A `remote` tunnel
   forwards to a real destination so the value belongs on the wire; a `dynamic`
   tunnel has none, so it stays empty.

- [ ] **Step 1: Write the failing test**

Create `internal/cli/create_contract_test.go`:

```go
package cli

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/craigderington/lazytunnel/internal/api"
	"github.com/craigderington/lazytunnel/pkg/types"
)

// sampleCreateRequest is a realistic request with every field populated.
func sampleCreateRequest() createTunnelRequest {
	return createTunnelRequest{
		Name: "prod-db",
		Type: "local",
		Hops: []types.Hop{{
			Host:       "bastion.example.com",
			Port:       22,
			User:       "deploy",
			AuthMethod: types.AuthMethodKey,
			KeyID:      "/home/deploy/.ssh/id_ed25519",
		}},
		LocalPort:     5432,
		RemoteHost:    "db.internal.example.com",
		RemotePort:    5432,
		AutoReconnect: true,
		KeepAlive:     30,
		MaxRetries:    3,
	}
}

// withCreateFlags sets the package-level create flags to a valid local-tunnel
// configuration and restores them afterwards, so tests in this package cannot
// leak state into each other. Capture happens before assignment — see
// callExport in export_test.go for the same pattern.
//
// Tests that need a different shape call this and then override the specific
// vars they care about; the cleanup still restores everything.
func withCreateFlags(t *testing.T) {
	t.Helper()

	prevName := tunnelName
	prevType := tunnelType
	prevLocalPort := localPort
	prevRemoteHost := remoteHost
	prevRemotePort := remotePort
	prevHops := hops
	prevUser := sshUser
	prevKey := sshKey
	prevAutoReconnect := autoReconnect
	prevKeepAlive := keepAlive
	prevMaxRetries := maxRetries

	t.Cleanup(func() {
		tunnelName = prevName
		tunnelType = prevType
		localPort = prevLocalPort
		remoteHost = prevRemoteHost
		remotePort = prevRemotePort
		hops = prevHops
		sshUser = prevUser
		sshKey = prevKey
		autoReconnect = prevAutoReconnect
		keepAlive = prevKeepAlive
		maxRetries = prevMaxRetries
	})

	tunnelName = "prod-db"
	tunnelType = "local"
	localPort = 5432
	remoteHost = "db.internal.example.com:5432"
	remotePort = 0
	hops = []string{"bastion.example.com:22"}
	sshUser = "deploy"
	sshKey = "/home/deploy/.ssh/id_ed25519"
	autoReconnect = true
	keepAlive = 30
	maxRetries = 3
}

// decodeAsServer marshals the CLI's request and decodes it exactly as the
// server does, returning what the handler would actually see.
func decodeAsServer(t *testing.T, req createTunnelRequest) api.CreateTunnelRequest {
	t.Helper()

	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshalling the CLI request failed: %v", err)
	}

	var server api.CreateTunnelRequest
	if err := json.Unmarshal(body, &server); err != nil {
		t.Fatalf("the server could not decode the CLI's body: %v\nbody: %s", err, body)
	}
	return server
}

func TestCreateRequestSurvivesTheWire(t *testing.T) {
	req := sampleCreateRequest()
	got := decodeAsServer(t, req)

	if got.Name != req.Name {
		t.Errorf("Name: got %q, want %q", got.Name, req.Name)
	}
	if got.Type != req.Type {
		t.Errorf("Type: got %q, want %q", got.Type, req.Type)
	}
	if got.LocalPort != req.LocalPort {
		t.Errorf("LocalPort: got %d, want %d", got.LocalPort, req.LocalPort)
	}
	if got.RemoteHost != req.RemoteHost {
		t.Errorf("RemoteHost: got %q, want %q — this is the field the original bug dropped", got.RemoteHost, req.RemoteHost)
	}
	if got.RemotePort != req.RemotePort {
		t.Errorf("RemotePort: got %d, want %d", got.RemotePort, req.RemotePort)
	}
	if got.AutoReconnect != req.AutoReconnect {
		t.Errorf("AutoReconnect: got %v, want %v", got.AutoReconnect, req.AutoReconnect)
	}
	if got.KeepAlive != req.KeepAlive {
		t.Errorf("KeepAlive: got %d, want %d", got.KeepAlive, req.KeepAlive)
	}
	if got.MaxRetries != req.MaxRetries {
		t.Errorf("MaxRetries: got %d, want %d", got.MaxRetries, req.MaxRetries)
	}

	if len(got.Hops) != 1 {
		t.Fatalf("Hops: got %d, want 1", len(got.Hops))
	}
	if got.Hops[0].Host != "bastion.example.com" {
		t.Errorf("hop Host: got %q, want bastion.example.com", got.Hops[0].Host)
	}
	if got.Hops[0].Port != 22 {
		t.Errorf("hop Port: got %d, want 22", got.Hops[0].Port)
	}
	if got.Hops[0].User != "deploy" {
		t.Errorf("hop User: got %q, want deploy", got.Hops[0].User)
	}
	if got.Hops[0].AuthMethod != "key" {
		t.Errorf("hop AuthMethod: got %q, want key", got.Hops[0].AuthMethod)
	}
	if got.Hops[0].KeyID != "/home/deploy/.ssh/id_ed25519" {
		t.Errorf("hop KeyID: got %q", got.Hops[0].KeyID)
	}
}

func TestCreateRequestPassesServerValidation(t *testing.T) {
	got := decodeAsServer(t, sampleCreateRequest())

	if errs := api.ValidateRequest(&got); len(errs) != 0 {
		t.Fatalf("the server would reject the CLI's own request: %+v", errs)
	}
}

func TestCreateRequestRejectsNanosecondKeepAlive(t *testing.T) {
	// Regression guard for the second half of the original defect.
	// types.TunnelSpec.KeepAlive is a time.Duration, so marshalling it sent
	// 30000000000 into a field validated as whole seconds with max=300.
	// Fixing only the JSON key names would have left the command broken, so
	// this test pins the encoding as well as the name.
	req := sampleCreateRequest()
	req.KeepAlive = int(30 * time.Second) // 30000000000, the old wire value

	got := decodeAsServer(t, req)

	errs := api.ValidateRequest(&got)
	if len(errs) == 0 {
		t.Fatal("a nanosecond keepalive must fail server validation; if this passes, the seconds-vs-nanoseconds guard is gone")
	}
}

func TestDynamicTunnelNeedsNoDestination(t *testing.T) {
	// A SOCKS5 proxy has no fixed destination. The API validator required
	// remoteHost and remotePort for every type, so a dynamic tunnel could
	// never be created — despite create.go's help text documenting it.
	req := createTunnelRequest{
		Name: "socks",
		Type: "dynamic",
		Hops: []types.Hop{{
			Host:       "jumphost.example.com",
			Port:       22,
			User:       "deploy",
			AuthMethod: types.AuthMethodKey,
			KeyID:      "/home/deploy/.ssh/id_ed25519",
		}},
		LocalPort:     1080,
		AutoReconnect: true,
		KeepAlive:     30,
		MaxRetries:    3,
	}

	got := decodeAsServer(t, req)

	if errs := api.ValidateRequest(&got); len(errs) != 0 {
		t.Fatalf("the server must accept a dynamic tunnel with no destination: %+v", errs)
	}
}

func TestLocalTunnelStillRequiresADestination(t *testing.T) {
	// Making the destination conditional must not weaken it for the types
	// that genuinely need one.
	req := sampleCreateRequest()
	req.RemoteHost = ""
	req.RemotePort = 0

	got := decodeAsServer(t, req)

	if errs := api.ValidateRequest(&got); len(errs) == 0 {
		t.Fatal("a local tunnel with no destination must still be rejected")
	}
}

func TestRemoteTunnelDestinationIsTransmitted(t *testing.T) {
	// The original builder discarded --remote-host for every non-local type.
	withCreateFlags(t)
	tunnelType = "remote"
	remoteHost = "internal.example.com"
	remotePort = 9090

	req, err := buildCreateRequest()
	if err != nil {
		t.Fatalf("buildCreateRequest returned error: %v", err)
	}
	if req.RemoteHost != "internal.example.com" {
		t.Errorf("got RemoteHost %q, want internal.example.com — the flag must not be discarded", req.RemoteHost)
	}
	if req.RemotePort != 9090 {
		t.Errorf("got RemotePort %d, want 9090", req.RemotePort)
	}

	got := decodeAsServer(t, req)
	if errs := api.ValidateRequest(&got); len(errs) != 0 {
		t.Fatalf("server would reject a remote tunnel: %+v", errs)
	}
}

func TestDynamicTunnelSendsNoDestination(t *testing.T) {
	withCreateFlags(t)
	tunnelType = "dynamic"
	remoteHost = "should-be-ignored.example.com"
	remotePort = 9090
	localPort = 1080

	req, err := buildCreateRequest()
	if err != nil {
		t.Fatalf("buildCreateRequest returned error: %v", err)
	}
	if req.RemoteHost != "" {
		t.Errorf("got RemoteHost %q, want empty — a SOCKS5 proxy has no destination", req.RemoteHost)
	}
	if req.RemotePort != 0 {
		t.Errorf("got RemotePort %d, want 0", req.RemotePort)
	}
}

func TestCreateRequestOmitsFieldsTheCLICannotSet(t *testing.T) {
	// agentId has no flag, so the CLI must not send it — an empty value
	// would be indistinguishable from a deliberate choice.
	body, err := json.Marshal(sampleCreateRequest())
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if _, present := raw["agentId"]; present {
		t.Error("request must not carry agentId; the CLI has no flag for it")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestCreateRequest -v`
Expected: FAIL — the package does not compile (`undefined: createTunnelRequest`).

- [ ] **Step 3: Write minimal implementation**

In `internal/cli/create.go`, add this type immediately after the `var (...)` flag block:

```go
// createTunnelRequest mirrors the wire format of api.CreateTunnelRequest.
//
// It exists because types.TunnelSpec disagrees with the API in two ways: its
// top-level JSON tags are snake_case where the API expects camelCase, and its
// KeepAlive is a time.Duration that marshals to nanoseconds where the API
// expects whole seconds (validated max=300). Marshalling the spec directly
// produced a body the server silently decoded into zero values and then
// rejected as invalid.
//
// This mirrors the existing pattern in this package — list.go declares
// tunnelListItem, import.go declares importReport — of restating server shapes
// rather than importing them. create_contract_test.go pins this struct against
// the real api.CreateTunnelRequest so the two cannot drift apart again.
//
// Hops are sent as []types.Hop deliberately: types.Hop and api.HopReq already
// agree on their snake_case tags, and the extra fields types.Hop carries are
// ignored by the server's decoder.
type createTunnelRequest struct {
	Name          string      `json:"name"`
	Type          string      `json:"type"`
	Hops          []types.Hop `json:"hops"`
	LocalPort     int         `json:"localPort"`
	RemoteHost    string      `json:"remoteHost,omitempty"`
	RemotePort    int         `json:"remotePort,omitempty"`
	AutoReconnect bool        `json:"autoReconnect"`
	KeepAlive     int         `json:"keepAlive"`
	MaxRetries    int         `json:"maxRetries"`
}
```

`RemoteHost` and `RemotePort` are `omitempty` because a dynamic tunnel has no
destination, and omitting the keys makes that visible on the wire rather than
sending empty values that look like an oversight.

Then relax the API validator so a dynamic tunnel is legal. In
`internal/api/validation.go`, change the two destination fields of
`CreateTunnelRequest`:

```go
	RemoteHost       string   `json:"remoteHost" validate:"required_unless=Type dynamic,omitempty,hostname|ip_addr"`
	RemotePort       int      `json:"remotePort" validate:"required_unless=Type dynamic,omitempty,min=1,max=65535"`
```

`required_unless=Type dynamic` keeps both mandatory for `local` and `remote`
while permitting their absence for `dynamic`. The `omitempty` that follows
short-circuits the format checks when the value is absent, so a dynamic tunnel
does not trip `hostname|ip_addr` on an empty string.

This also removes a disagreement between two validators: `internal/backup/validate.go`
already treats the destination as optional for dynamic tunnels, so before this
change a dynamic tunnel could be restored from a backup but never created.

Now extract the request construction out of `runCreate` so it can be tested without a server. Replace the block that currently builds `spec := types.TunnelSpec{...}` — and everything above it that parses flags — with a call to a new function. The parsing logic moves verbatim; only the final struct literal changes.

Add this function above `runCreate`:

```go
// buildCreateRequest turns the parsed flags into the request body the API
// expects. It performs no I/O so it can be tested directly.
func buildCreateRequest() (createTunnelRequest, error) {
	ttype := types.TunnelType(tunnelType)

	// Parse hops
	hopList := make([]types.Hop, 0, len(hops))
	for _, h := range hops {
		parts := strings.Split(h, ":")
		if len(parts) != 2 {
			return createTunnelRequest{}, fmt.Errorf("invalid hop format: %s (expected host:port)", h)
		}
		var port int
		if _, err := fmt.Sscanf(parts[1], "%d", &port); err != nil {
			return createTunnelRequest{}, fmt.Errorf("invalid port in hop: %s", h)
		}

		authMethod := types.AuthMethodAgent
		keyID := ""
		if sshKey != "" {
			authMethod = types.AuthMethodKey
			keyID = sshKey
		}

		hopList = append(hopList, types.Hop{
			Host:       parts[0],
			Port:       port,
			User:       sshUser,
			AuthMethod: authMethod,
			KeyID:      keyID,
		})
	}

	// Destination, which differs per tunnel type:
	//   local   — --remote-host carries a combined host:port
	//   remote  — --remote-host is a bare host, --remote-port the port
	//   dynamic — a SOCKS5 proxy has no fixed destination, so both stay empty
	var remHost string
	var remPort int
	switch ttype {
	case types.TunnelTypeLocal:
		parts := strings.Split(remoteHost, ":")
		if len(parts) != 2 {
			return createTunnelRequest{}, fmt.Errorf("invalid remote host format: %s (expected host:port)", remoteHost)
		}
		remHost = parts[0]
		if _, err := fmt.Sscanf(parts[1], "%d", &remPort); err != nil {
			return createTunnelRequest{}, fmt.Errorf("invalid port in remote host: %s", remoteHost)
		}
	case types.TunnelTypeRemote:
		remHost = remoteHost
		remPort = remotePort
	}

	return createTunnelRequest{
		Name:          tunnelName,
		Type:          string(ttype),
		Hops:          hopList,
		LocalPort:     localPort,
		RemoteHost:    remHost,
		RemotePort:    remPort,
		AutoReconnect: autoReconnect,
		KeepAlive:     keepAlive,
		MaxRetries:    maxRetries,
	}, nil
}
```

Then in `runCreate`, replace the flag-parsing and spec-building section with:

```go
	req, err := buildCreateRequest()
	if err != nil {
		return err
	}

	serverURL := viper.GetString("server")
	url := fmt.Sprintf("%s/api/v1/tunnels", serverURL)

	jsonData, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal tunnel request: %w", err)
	}
```

Leave the rest of `runCreate` — the POST, the status check, the response parsing and printing — exactly as it is.

Remove the now-unused `time` import from `create.go` if nothing else in the file uses it. Run `go build ./...` to confirm.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/ ./internal/api/ -v 2>&1 | tail -30 && go build ./... && go test ./...`
Expected: PASS — 8 new tests in `internal/cli`, and no regressions in
`internal/api` from the validator change. Pay attention to
`internal/api/validation_test.go`, which has existing `CreateTunnelRequest`
cases: if any of them asserted that a missing `remoteHost` is rejected for a
tunnel it did not give a type, the `required_unless` change may alter that
outcome. If a test there fails, read it before touching it — decide whether the
old expectation was correct, and say so in your report either way.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/create.go internal/cli/create_contract_test.go internal/api/validation.go
git commit -m "fix(cli): send the request body the API actually decodes

tunnelctl create marshalled a types.TunnelSpec into a handler decoding
api.CreateTunnelRequest. Two mismatches, both fatal: snake_case vs
camelCase top-level keys, and KeepAlive marshalling as nanoseconds into
a field validated as seconds with max=300. Every create failed.

Adds a contract test that decodes the CLI's body into the real server
type and runs the server's own validator, so a renamed key and a wrong
encoding both fail in CI rather than in production.

Also makes remoteHost/remotePort conditional on tunnel type. They were
required for every type, so a dynamic SOCKS5 tunnel could never be
created despite create.go documenting exactly that — and it could be
restored from a backup, since internal/backup/validate.go already
treated the destination as optional for dynamic. And --remote-host is
now actually transmitted for remote tunnels instead of discarded."
```

---

### Task 2: Add `--local-bind-address`

**Files:**
- Modify: `internal/cli/create.go` (flag var, registration, request field)
- Modify: `docs/cli-reference.md` (document the flag in the `create` section)
- Test: `internal/cli/create_contract_test.go` (append)

**Interfaces:**
- Consumes: `createTunnelRequest` and `buildCreateRequest()` from Task 1.
- Produces: package-level `localBindAddress string`; a `LocalBindAddress` field on `createTunnelRequest`.

**Why the default is `127.0.0.1`:** `internal/tunnel/forward.go:105` and `:618` both treat an empty bind address as `0.0.0.0`, so every CLI-created tunnel currently listens on all interfaces. Task 1's defect means the command has never worked, so no script depends on that behaviour and the default can be chosen freely. Loopback matches the schema's own declared default at `internal/storage/sqlite.go:52`.

- [ ] **Step 1: Write the failing test**

Append to `internal/cli/create_contract_test.go`:

```go
// Extend the existing withCreateFlags helper from Task 1 so it also captures,
// restores and sets localBindAddress. Add `prevBind := localBindAddress` to the
// capture block, `localBindAddress = prevBind` to the cleanup closure, and
// `localBindAddress = "127.0.0.1"` to the assignment block. Do not change its
// signature — tests that need a different value call it and then override.

func TestLocalBindAddressFlagDefaultsToLoopback(t *testing.T) {
	f := createCmd.Flags().Lookup("local-bind-address")
	if f == nil {
		t.Fatal("--local-bind-address is not registered")
	}
	if f.DefValue != "127.0.0.1" {
		t.Fatalf("got default %q, want 127.0.0.1 — a CLI-created tunnel must not listen on all interfaces by accident", f.DefValue)
	}
}

func TestBuildCreateRequestCarriesBindAddress(t *testing.T) {
	withCreateFlags(t)

	req, err := buildCreateRequest()
	if err != nil {
		t.Fatalf("buildCreateRequest returned error: %v", err)
	}
	if req.LocalBindAddress != "127.0.0.1" {
		t.Fatalf("got LocalBindAddress %q, want 127.0.0.1", req.LocalBindAddress)
	}

	got := decodeAsServer(t, req)
	if got.LocalBindAddress != "127.0.0.1" {
		t.Fatalf("the server received LocalBindAddress %q, want 127.0.0.1", got.LocalBindAddress)
	}
	if errs := api.ValidateRequest(&got); len(errs) != 0 {
		t.Fatalf("server would reject a loopback bind address: %+v", errs)
	}
}

func TestBuildCreateRequestPassesExplicitAllInterfaces(t *testing.T) {
	// The escape hatch matters as much as the safe default: an operator who
	// genuinely wants all interfaces must be able to say so.
	withCreateFlags(t)
	localBindAddress = "0.0.0.0"

	req, err := buildCreateRequest()
	if err != nil {
		t.Fatalf("buildCreateRequest returned error: %v", err)
	}

	got := decodeAsServer(t, req)
	if got.LocalBindAddress != "0.0.0.0" {
		t.Fatalf("got LocalBindAddress %q, want 0.0.0.0 transmitted unchanged", got.LocalBindAddress)
	}
	if errs := api.ValidateRequest(&got); len(errs) != 0 {
		t.Fatalf("server would reject an explicit 0.0.0.0: %+v", errs)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run 'TestLocalBindAddress|TestBuildCreateRequest' -v`
Expected: FAIL — `undefined: localBindAddress`.

- [ ] **Step 3: Write minimal implementation**

In `internal/cli/create.go`, add to the `var (...)` block, after `localPort int`:

```go
	localBindAddress string
```

Add to `createTunnelRequest`, after the `LocalPort` field:

```go
	LocalBindAddress string      `json:"localBindAddress,omitempty"`
```

Add to the returned struct literal in `buildCreateRequest`, after `LocalPort: localPort,`:

```go
		LocalBindAddress: localBindAddress,
```

Register the flag in `init()`, immediately after the `local-port` line:

```go
	createCmd.Flags().StringVar(&localBindAddress, "local-bind-address", "127.0.0.1",
		"local address to bind (127.0.0.1 for loopback only, 0.0.0.0 for all interfaces)")
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/ -v && go build ./...`
Expected: PASS — 7 create tests plus the pre-existing export, import and list tests.

- [ ] **Step 5: Document the flag**

In `docs/cli-reference.md`, in the `create` command's flag list, add an entry for `--local-bind-address` matching the surrounding style. State that it defaults to `127.0.0.1` so the tunnel listens on loopback only, and that `0.0.0.0` exposes it on all interfaces.

Read the surrounding section first and match its existing formatting — that file uses prose and bullets, not tables.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/create.go internal/cli/create_contract_test.go docs/cli-reference.md
git commit -m "feat(cli): add --local-bind-address, defaulting to loopback

tunnelctl create had no bind-address flag, so every tunnel it created
was stored with an empty value that forward.go treats as 0.0.0.0 —
listening on all interfaces with no way to ask for loopback.

Defaults to 127.0.0.1. Safe to choose freely because the command has
never worked, so no script depends on the old behaviour; 0.0.0.0
remains available explicitly."
```

---

### Task 3: Wire the CORS allowlist and deny by default

**Files:**
- Modify: `internal/api/server.go` (Config field, Server field, middleware, startup log)
- Modify: `internal/config/config.go` (default changes to empty)
- Modify: `cmd/server/main.go` (pass the configured origins)
- Modify: `internal/api/backup_handlers_test.go` (a comment there describes the old CORS posture)
- Test: `internal/api/cors_test.go`

**Interfaces:**
- Consumes: `config.ServerConfig.CORS.AllowedOrigins` (`internal/config/config.go:28`).
- Produces: `api.Config.AllowedOrigins []string`; method `(*Server).originAllowed(origin string) (allowed, exact bool)`.

- [ ] **Step 1: Write the failing test**

Create `internal/api/cors_test.go`:

```go
package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"
)

func corsTestServer(t *testing.T, origins []string) *Server {
	t.Helper()
	return NewServer(context.Background(), Config{
		Addr:           ":0",
		Logger:         zerolog.Nop(),
		AllowedOrigins: origins,
	})
}

// corsResponse runs a request through the middleware and returns the headers.
func corsResponse(t *testing.T, srv *Server, method, origin string) http.Header {
	t.Helper()

	handler := srv.corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(method, "/api/v1/tunnels", nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec.Header()
}

func TestCORSNoOriginHeaderEmitsNothing(t *testing.T) {
	// A same-origin request carries no Origin header. Stamping CORS headers
	// onto it is meaningless, and the old middleware did exactly that.
	h := corsResponse(t, corsTestServer(t, []string{"*"}), http.MethodGet, "")

	if got := h.Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("got Access-Control-Allow-Origin %q, want none for a request with no Origin", got)
	}
}

func TestCORSEmptyAllowlistDeniesEverything(t *testing.T) {
	// This is the new default. Nothing cross-origin gets through.
	h := corsResponse(t, corsTestServer(t, nil), http.MethodGet, "https://evil.example.com")

	if got := h.Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("got Access-Control-Allow-Origin %q, want none — an empty allowlist must deny", got)
	}
}

func TestCORSWildcardEchoesWildcard(t *testing.T) {
	h := corsResponse(t, corsTestServer(t, []string{"*"}), http.MethodGet, "https://app.example.com")

	if got := h.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("got %q, want *", got)
	}
	if got := h.Get("Vary"); got != "" {
		t.Errorf("got Vary %q, want none — a wildcard response does not vary by origin", got)
	}
}

func TestCORSExactMatchEchoesOriginAndVaries(t *testing.T) {
	srv := corsTestServer(t, []string{"https://app.example.com", "https://admin.example.com"})
	h := corsResponse(t, srv, http.MethodGet, "https://admin.example.com")

	if got := h.Get("Access-Control-Allow-Origin"); got != "https://admin.example.com" {
		t.Errorf("got %q, want the matching origin echoed back", got)
	}
	if got := h.Get("Vary"); got != "Origin" {
		t.Errorf("got Vary %q, want Origin — without it a shared cache can serve one origin's response to another", got)
	}
	if got := h.Get("Access-Control-Allow-Methods"); got == "" {
		t.Error("Allow-Methods must accompany an allow-origin header")
	}
	if got := h.Get("Access-Control-Allow-Headers"); got == "" {
		t.Error("Allow-Headers must accompany an allow-origin header")
	}
}

func TestCORSNonMatchingOriginIsDenied(t *testing.T) {
	srv := corsTestServer(t, []string{"https://app.example.com"})
	h := corsResponse(t, srv, http.MethodGet, "https://evil.example.com")

	if got := h.Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("got %q, want none for an unlisted origin", got)
	}
	if got := h.Get("Access-Control-Allow-Methods"); got != "" {
		t.Errorf("got Allow-Methods %q, want none — it is meaningless without an allow-origin header", got)
	}
}

func TestCORSMatchingIsCaseSensitiveAndExact(t *testing.T) {
	srv := corsTestServer(t, []string{"https://app.example.com"})

	for _, origin := range []string{
		"https://APP.example.com",
		"https://app.example.com.evil.com",
		"https://evil.com?x=https://app.example.com",
		"http://app.example.com",
	} {
		h := corsResponse(t, srv, http.MethodGet, origin)
		if got := h.Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("origin %q was allowed (got %q); matching must be exact", origin, got)
		}
	}
}

func TestCORSWildcardWinsOverSpecificEntries(t *testing.T) {
	srv := corsTestServer(t, []string{"https://app.example.com", "*"})
	h := corsResponse(t, srv, http.MethodGet, "https://app.example.com")

	if got := h.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("got %q, want * — a wildcard in the list must not be silently narrowed", got)
	}
}

func TestCORSPreflightAllowedAndDenied(t *testing.T) {
	srv := corsTestServer(t, []string{"https://app.example.com"})

	allowed := corsResponse(t, srv, http.MethodOptions, "https://app.example.com")
	if got := allowed.Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Errorf("allowed preflight: got %q, want the origin echoed", got)
	}

	denied := corsResponse(t, srv, http.MethodOptions, "https://evil.example.com")
	if got := denied.Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("denied preflight: got %q, want no allow-origin header", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run TestCORS -v`
Expected: FAIL — `unknown field AllowedOrigins in struct literal`.

- [ ] **Step 3: Write minimal implementation**

In `internal/api/server.go`, add to the `Config` struct after `Version`:

```go
	AllowedOrigins []string          // CORS allowlist; empty denies all cross-origin access
```

Add to the `Server` struct after `version string`:

```go
	allowedOrigins []string
```

In `NewServer`, add `allowedOrigins: config.AllowedOrigins,` to the `&Server{...}` literal, then immediately after `s.setupRoutes()` add the startup log:

```go
	if len(s.allowedOrigins) == 0 {
		config.Logger.Info().Msg("CORS: cross-origin access denied (server.cors.allowed_origins is empty)")
	} else {
		config.Logger.Info().Strs("origins", s.allowedOrigins).Msg("CORS: cross-origin access allowed")
	}
```

Replace `corsMiddleware` entirely:

```go
// corsMiddleware applies the configured CORS allowlist.
//
// A request with no Origin header is not cross-origin, so it gets no CORS
// headers at all. An origin that is not allowed gets no
// Access-Control-Allow-Origin, which is what makes the browser block it —
// the server still answers, the browser enforces.
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" {
			if allowed, exact := s.originAllowed(origin); allowed {
				if exact {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					// Without this a shared cache can serve one origin's
					// response to another.
					w.Header().Add("Vary", "Origin")
				} else {
					w.Header().Set("Access-Control-Allow-Origin", "*")
				}
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			}
		}

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// originAllowed reports whether origin may access the API, and whether the
// match was exact rather than via a wildcard entry.
//
// A wildcard anywhere in the list wins over any specific entry, so a list
// containing "*" is never silently narrowed. Matching is exact and
// case-sensitive: no subdomain patterns, which is where CORS implementations
// usually acquire their bypasses.
func (s *Server) originAllowed(origin string) (allowed, exact bool) {
	for _, o := range s.allowedOrigins {
		if o == "*" {
			return true, false
		}
	}
	for _, o := range s.allowedOrigins {
		if o == origin {
			return true, true
		}
	}
	return false, false
}
```

In `internal/config/config.go`, change the CORS default:

```go
	v.SetDefault("server.cors.allowed_origins", []string{})
```

In `cmd/server/main.go`, add to the `api.Config` literal:

```go
		AllowedOrigins: cfg.Server.CORS.AllowedOrigins,
```

- [ ] **Step 4: Update the stale comment**

`internal/api/backup_handlers_test.go:378` carries a comment justifying the import route's Content-Type check by citing `Access-Control-Allow-Origin: *` on every route. Read it and adjust the wording: the wildcard is no longer unconditional, but the check is still worth keeping as defence-in-depth for deployments that configure `*` with auth disabled. Do not remove the check.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/api/ ./internal/config/ -v 2>&1 | tail -30 && go build ./... && go test ./...`
Expected: PASS — 8 CORS tests, and no regressions anywhere.

- [ ] **Step 6: Commit**

```bash
git add internal/api/server.go internal/api/cors_test.go internal/config/config.go cmd/server/main.go internal/api/backup_handlers_test.go
git commit -m "fix(api): honour server.cors.allowed_origins and deny by default

The middleware hardcoded Access-Control-Allow-Origin: * on every
response while config.Server.CORS.AllowedOrigins was parsed and never
read. With auth disabled by default, any page the operator visited
could drive the whole API from their browser.

The allowlist is now wired through and defaults to empty. A request
with no Origin gets no CORS headers; an exact match echoes the origin
with Vary: Origin; a wildcard entry still echoes *. Nothing in the repo
breaks: the bundled UI is same-origin and npm run dev proxies /api."
```

---

### Task 4: Make `config.example.yaml` describe the real schema

**Files:**
- Modify: `config.example.yaml` (full rewrite)
- Test: `internal/config/config_test.go` (append)

**Interfaces:**
- Consumes: `config.Load(path string, overrides map[string]interface{}) (*Config, error)`; the `Config` struct in `internal/config/config.go:13-45`.

The file currently documents `server.host`, `server.port`, `server.tls.*`, `database.host`, `database.port`, `database.database`, `database.user`, `kms:`, `auth.provider`, `tunnel:`, `metrics:`, `audit:`, and `logging.output`. `Load` reads none of them.

- [ ] **Step 1: Write the failing test**

Append to `internal/config/config_test.go`:

```go
func TestExampleConfigMatchesRealSchema(t *testing.T) {
	// The example file is what operators copy. If it documents keys Load
	// does not read, they get a server that silently ignores their config.
	cfg, err := Load("../../config.example.yaml", nil)
	if err != nil {
		t.Fatalf("config.example.yaml does not load: %v", err)
	}

	if cfg.Server.Addr != ":8080" {
		t.Errorf("Server.Addr = %q, want :8080", cfg.Server.Addr)
	}
	if cfg.Database.Path != "tunnels.db" {
		t.Errorf("Database.Path = %q, want tunnels.db", cfg.Database.Path)
	}
	if cfg.Auth.JWTSecretEnv != "LAZYTUNNEL_JWT_SECRET" {
		t.Errorf("Auth.JWTSecretEnv = %q, want LAZYTUNNEL_JWT_SECRET", cfg.Auth.JWTSecretEnv)
	}
	if cfg.Auth.TokenExpiration != 24*time.Hour {
		t.Errorf("Auth.TokenExpiration = %v, want 24h", cfg.Auth.TokenExpiration)
	}
	if cfg.Logging.Level != "info" {
		t.Errorf("Logging.Level = %q, want info", cfg.Logging.Level)
	}
	if cfg.Logging.Format != "console" {
		t.Errorf("Logging.Format = %q, want console", cfg.Logging.Format)
	}

	// The example must ship the safe CORS default, not a wildcard.
	if len(cfg.Server.CORS.AllowedOrigins) != 0 {
		t.Errorf("Server.CORS.AllowedOrigins = %v, want empty — the example must not hand operators a wide-open default",
			cfg.Server.CORS.AllowedOrigins)
	}
}
```

Add `"time"` to that file's imports if it is not already there.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestExampleConfig -v`
Expected: FAIL — the current example sets `logging.format: "json"` and has no `server.addr`, so several assertions fail.

- [ ] **Step 3: Rewrite the example**

Replace the entire contents of `config.example.yaml`:

```yaml
# lazytunnel server configuration
#
# Copy to config.yaml and adjust. Every key below is read by
# internal/config/config.go — there are no other supported keys.
#
# Values may also be supplied as environment variables, prefixed with
# LAZYTUNNEL_ and with dots replaced by underscores:
#   LAZYTUNNEL_SERVER_ADDR=":9090"

server:
  # Address the API server listens on.
  addr: ":8080"

  # TLS is enabled only when BOTH of these are set.
  tls_cert: ""
  tls_key: ""

  cors:
    # Origins permitted to call the API from a browser.
    #
    # Empty (the default) denies all cross-origin access. The bundled web UI
    # is served by this same server, so it is same-origin and unaffected;
    # `npm run dev` proxies /api, so local development is unaffected too.
    #
    # Add an origin only if you host a frontend somewhere else:
    #   allowed_origins: ["https://tunnels.example.com"]
    #
    # ["*"] allows any origin. Combined with authentication disabled, that
    # lets any website your browser visits drive this API.
    allowed_origins: []

database:
  # SQLite database file. Created on first run if absent.
  path: "tunnels.db"

auth:
  # Authentication is DISABLED unless a JWT secret is configured, either
  # here or via the environment variable named by jwt_secret_env.
  jwt_secret: ""
  jwt_secret_env: "LAZYTUNNEL_JWT_SECRET"

  # How long issued tokens remain valid.
  token_expiration: "24h"

  # Reconnect tunnels marked active in the database when the server starts.
  auto_start_tunnels: false

logging:
  # debug, info, warn, error
  level: "info"

  # console for human-readable output, json for structured logs
  format: "console"
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/ -v && go test ./...`
Expected: PASS — the example loads and every assertion holds.

- [ ] **Step 5: Verify the server actually starts with it**

Run:
```bash
go build -o /tmp/lazytunnel-server cmd/server/main.go
/tmp/lazytunnel-server --config config.example.yaml &
sleep 2
curl -s -o /dev/null -w '%{http_code}\n' http://localhost:8080/api/v1/health
kill %1
```
Expected: `200`. This is the check the old example would have failed in a way no unit test catches — the file parsing is only half the promise.

- [ ] **Step 6: Commit**

```bash
git add config.example.yaml internal/config/config_test.go
git commit -m "docs: make config.example.yaml describe the real schema

The example documented server.host, server.port, database.host,
database.user, a kms block and auth.provider, none of which
internal/config/config.go reads. An operator copying it got a server
that silently ignored most of what they wrote.

Rewritten to exactly the keys Load parses, including the now-live cors
block, with a test that loads the real file so it cannot rot again."
```

---

## Self-review notes

**Spec coverage:** Fix 1 → Task 1 (both the key-name and nanosecond defects, with the validator run in the contract test). Fix 1b → Task 2 (flag, `127.0.0.1` default, `0.0.0.0` escape hatch, docs). Fix 2 → Task 3 (Config plumbing, all four middleware rules, `Vary`, exact matching, wildcard precedence, startup log, empty default). Fix 3 → Task 4 (rewrite plus load test). The spec's build order maps to Tasks 1-4 in sequence.

**Naming consistency:** `createTunnelRequest` and `buildCreateRequest()` (Task 1) are extended, not renamed, by Task 2. `decodeAsServer` and `sampleCreateRequest` (Task 1) are reused by Task 2's tests. `originAllowed` (Task 3) is used only by `corsMiddleware` in the same file.

**Known divergence, recorded not fixed:** after Task 2 a CLI-created tunnel binds loopback while a UI-created one still binds all interfaces, because `web/src` never sends `localBindAddress`. Closing that changes a working UI flow and belongs in its own change with its own decision about defaults. The spec records this under Fix 1b.

**Deliberate deviation from the spec:** the spec describes only adding the request struct; this plan also extracts `buildCreateRequest()` from `runCreate`. That extraction is what makes Task 2's flag testable without standing up a server — `runCreate` performs I/O, the builder does not.
