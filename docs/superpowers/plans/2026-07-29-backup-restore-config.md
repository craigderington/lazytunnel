# Backup / Restore Config Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Export every tunnel definition to a portable, versioned JSON archive and restore it — merging into a running fleet or replacing it wholesale — from the CLI, the REST API, and the web UI.

**Architecture:** A new `internal/backup` package owns the on-disk format and a pure, I/O-free diff engine (`Plan`), writing through a three-method `Store` interface. A new `Manager.Reconcile(ctx)` converges the running fleet onto stored desired state, which is the only part that touches tunnel lifecycle. API handlers, CLI commands, and the web UI are all thin clients over those two pieces.

**Tech Stack:** Go 1.24, `gorilla/mux`, `google/uuid`, `spf13/cobra` + `viper`, `modernc.org/sqlite`; React 19 + TypeScript, TanStack Query, Vitest.

**Spec:** `docs/superpowers/specs/2026-07-29-backup-restore-config-design.md`

## Global Constraints

- Archive schema version is **1**. `Export` writes only version 1; `Import` accepts only version 1 and rejects anything else with HTTP 400.
- The archive uses its own DTO (`backup.TunnelEntry`), **never** `types.TunnelSpec` directly. `TunnelSpec.KeepAlive` is a `time.Duration` that marshals to raw nanoseconds, and `TunnelSpec.Auth` / `.Policy` are never persisted by `internal/storage/sqlite.go`.
- Tunnels are matched on **`name`**, the `UNIQUE` column in the `tunnels` table — never on ID.
- An existing tunnel keeps its `ID`, `Owner`, and `CreatedAt` across an import. A restore must not reassign ownership or churn IDs that `web/src/store/orderStore.ts` keys its drag ordering on.
- No new Go or npm dependencies. Everything needed is already in `go.mod` and `web/package.json`.
- All new API routes go on the existing `protected` subrouter in `internal/api/server.go`, inheriting auth and rate limiting.
- Import is **validate-then-write, not transactional**. `tunnel.Storage` exposes no transaction API. Never describe it as atomic in code comments, docs, or UI copy.
- Go tests run with `go test ./...`; frontend tests with `cd web && npm run test:run`.

### Three verified facts about existing code that this plan works around

1. `Manager.Update` (`internal/tunnel/manager.go:190`) returns `"tunnel %s must be stopped before editing"` for `Active` or `Pending` tunnels.
2. `Manager.Create` (`internal/tunnel/manager.go:168`) unconditionally calls `connectTunnel`, so it cannot honour `desired_status`.
3. `Manager.LoadFromStorage` (`internal/tunnel/manager.go:111`) overwrites `m.tunnels[id]` outright, orphaning live SSH goroutines. It is boot-time only.

Because of these, restore writes through `Store` and then calls `Manager.Reconcile` — it must never call `Create`, `Update`, or `LoadFromStorage`.

---

### Task 1: Archive format and spec conversion

**Files:**
- Create: `internal/backup/archive.go`
- Test: `internal/backup/archive_test.go`

**Interfaces:**
- Consumes: `pkg/types` (`TunnelSpec`, `Hop`, `TunnelType`, `AuthMethod`, `DesiredStatus`, `HostKeyVerification`).
- Produces: `SchemaVersion` const; types `Archive`, `TunnelEntry`, `HopEntry`; functions `EntryFromSpec(*types.TunnelSpec) TunnelEntry` and `SpecFromEntry(TunnelEntry) *types.TunnelSpec`.

- [ ] **Step 1: Write the failing test**

Create `internal/backup/archive_test.go`:

```go
package backup

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/craigderington/lazytunnel/pkg/types"
)

func sampleSpec() *types.TunnelSpec {
	created := time.Date(2026, 7, 29, 8, 0, 0, 0, time.UTC)
	return &types.TunnelSpec{
		ID:            "9f1c2b4e-7a30-4d51-9c8f-2e6b1a0d3f55",
		Name:          "prod-db",
		Owner:         "admin",
		AgentID:       "",
		DesiredStatus: types.DesiredStatusActive,
		Type:          types.TunnelTypeLocal,
		Hops: []types.Hop{{
			Host:                "bastion.example.com",
			Port:                22,
			User:                "deploy",
			AuthMethod:          types.AuthMethodKey,
			KeyID:               "/home/deploy/.ssh/id_ed25519",
			HostKeyVerification: types.HostKeyVerifyStrict,
			KnownHostsPath:      "/home/deploy/.ssh/known_hosts",
		}},
		LocalPort:        5432,
		LocalBindAddress: "127.0.0.1",
		RemoteHost:       "db.internal.example.com",
		RemotePort:       5432,
		AutoReconnect:    true,
		KeepAlive:        30 * time.Second,
		MaxRetries:       5,
		CreatedAt:        created,
		UpdatedAt:        created,
	}
}

func TestEntryFromSpecRoundTrips(t *testing.T) {
	spec := sampleSpec()
	got := SpecFromEntry(EntryFromSpec(spec))

	// CreatedAt/UpdatedAt are deliberately not carried by the archive — Plan
	// resolves them — so compare everything else.
	got.CreatedAt = spec.CreatedAt
	got.UpdatedAt = spec.UpdatedAt

	if !reflect.DeepEqual(got, spec) {
		t.Fatalf("round trip changed the spec:\n got %+v\nwant %+v", got, spec)
	}
}

func TestEntryUsesWholeSecondsNotNanoseconds(t *testing.T) {
	entry := EntryFromSpec(sampleSpec())
	if entry.KeepAliveSeconds != 30 {
		t.Fatalf("got KeepAliveSeconds %d, want 30", entry.KeepAliveSeconds)
	}

	blob, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(blob, &raw); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if _, present := raw["keep_alive"]; present {
		t.Error("archive must not emit a nanosecond keep_alive field")
	}
	if raw["keep_alive_seconds"] != float64(30) {
		t.Errorf("got keep_alive_seconds %v, want 30", raw["keep_alive_seconds"])
	}
}

func TestEntryOmitsUnpersistedCredentialFields(t *testing.T) {
	// AuthConfig and PolicySpec are never written by internal/storage/sqlite.go.
	// Emitting them would imply the backup carries credentials it does not have.
	blob, err := json.Marshal(EntryFromSpec(sampleSpec()))
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(blob, &raw); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	for _, field := range []string{"auth", "policy"} {
		if _, present := raw[field]; present {
			t.Errorf("archive entry must not contain %q", field)
		}
	}
}

func TestSpecFromEntryDefaultsDesiredStatusToStopped(t *testing.T) {
	spec := SpecFromEntry(TunnelEntry{Name: "x", Type: "local"})
	if spec.DesiredStatus != types.DesiredStatusStopped {
		t.Fatalf("got DesiredStatus %q, want stopped", spec.DesiredStatus)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/backup/ -run 'TestEntry|TestSpecFrom' -v`
Expected: FAIL — the package does not compile (`undefined: SpecFromEntry`, `undefined: TunnelEntry`).

- [ ] **Step 3: Write minimal implementation**

Create `internal/backup/archive.go`:

```go
// Package backup exports and restores lazytunnel tunnel definitions as a
// portable, versioned JSON archive.
package backup

import (
	"time"

	"github.com/craigderington/lazytunnel/pkg/types"
)

// SchemaVersion is the archive format version written by Export, and the only
// version Import accepts.
const SchemaVersion = 1

// Archive is the top-level backup document.
type Archive struct {
	Version    int           `json:"version"`
	ExportedAt time.Time     `json:"exported_at"`
	Source     string        `json:"source"`
	Tunnels    []TunnelEntry `json:"tunnels"`
}

// TunnelEntry is the on-disk representation of one tunnel.
//
// It is deliberately not types.TunnelSpec. TunnelSpec.KeepAlive is a
// time.Duration that marshals to raw nanoseconds (30000000000), which is
// unreadable in a file meant to be hand-edited; and TunnelSpec.Auth and
// .Policy are never persisted by internal/storage/sqlite.go, so exporting the
// spec directly would emit an empty "auth" object implying the backup carries
// credentials it does not have.
type TunnelEntry struct {
	ID               string     `json:"id,omitempty"`
	Name             string     `json:"name"`
	Owner            string     `json:"owner,omitempty"`
	AgentID          string     `json:"agent_id,omitempty"`
	DesiredStatus    string     `json:"desired_status,omitempty"`
	Type             string     `json:"type"`
	Hops             []HopEntry `json:"hops"`
	LocalPort        int        `json:"local_port,omitempty"`
	LocalBindAddress string     `json:"local_bind_address,omitempty"`
	RemoteHost       string     `json:"remote_host,omitempty"`
	RemotePort       int        `json:"remote_port,omitempty"`
	AutoReconnect    bool       `json:"auto_reconnect"`
	KeepAliveSeconds int        `json:"keep_alive_seconds,omitempty"`
	MaxRetries       int        `json:"max_retries,omitempty"`
}

// HopEntry is the on-disk representation of a single SSH hop.
type HopEntry struct {
	Host                string `json:"host"`
	Port                int    `json:"port"`
	User                string `json:"user"`
	AuthMethod          string `json:"auth_method"`
	KeyID               string `json:"key_id,omitempty"`
	HostKeyVerification string `json:"host_key_verification,omitempty"`
	KnownHostsPath      string `json:"known_hosts_path,omitempty"`
}

// EntryFromSpec converts a stored spec into its archive representation.
func EntryFromSpec(spec *types.TunnelSpec) TunnelEntry {
	hops := make([]HopEntry, 0, len(spec.Hops))
	for _, h := range spec.Hops {
		hops = append(hops, HopEntry{
			Host:                h.Host,
			Port:                h.Port,
			User:                h.User,
			AuthMethod:          string(h.AuthMethod),
			KeyID:               h.KeyID,
			HostKeyVerification: string(h.HostKeyVerification),
			KnownHostsPath:      h.KnownHostsPath,
		})
	}

	desired := string(spec.DesiredStatus)
	if desired == "" {
		desired = string(types.DesiredStatusStopped)
	}

	return TunnelEntry{
		ID:               spec.ID,
		Name:             spec.Name,
		Owner:            spec.Owner,
		AgentID:          spec.AgentID,
		DesiredStatus:    desired,
		Type:             string(spec.Type),
		Hops:             hops,
		LocalPort:        spec.LocalPort,
		LocalBindAddress: spec.LocalBindAddress,
		RemoteHost:       spec.RemoteHost,
		RemotePort:       spec.RemotePort,
		AutoReconnect:    spec.AutoReconnect,
		KeepAliveSeconds: int(spec.KeepAlive.Seconds()),
		MaxRetries:       spec.MaxRetries,
	}
}

// SpecFromEntry converts an archive entry into a tunnel spec.
//
// Identity fields are left as the archive gave them: Plan resolves ID, Owner
// and CreatedAt against what is already stored.
func SpecFromEntry(e TunnelEntry) *types.TunnelSpec {
	hops := make([]types.Hop, 0, len(e.Hops))
	for _, h := range e.Hops {
		hops = append(hops, types.Hop{
			Host:                h.Host,
			Port:                h.Port,
			User:                h.User,
			AuthMethod:          types.AuthMethod(h.AuthMethod),
			KeyID:               h.KeyID,
			HostKeyVerification: types.HostKeyVerification(h.HostKeyVerification),
			KnownHostsPath:      h.KnownHostsPath,
		})
	}

	desired := e.DesiredStatus
	if desired == "" {
		desired = string(types.DesiredStatusStopped)
	}

	return &types.TunnelSpec{
		ID:               e.ID,
		Name:             e.Name,
		Owner:            e.Owner,
		AgentID:          e.AgentID,
		DesiredStatus:    types.DesiredStatus(desired),
		Type:             types.TunnelType(e.Type),
		Hops:             hops,
		LocalPort:        e.LocalPort,
		LocalBindAddress: e.LocalBindAddress,
		RemoteHost:       e.RemoteHost,
		RemotePort:       e.RemotePort,
		AutoReconnect:    e.AutoReconnect,
		KeepAlive:        time.Duration(e.KeepAliveSeconds) * time.Second,
		MaxRetries:       e.MaxRetries,
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/backup/ -v`
Expected: PASS — 4 tests.

- [ ] **Step 5: Commit**

```bash
git add internal/backup/archive.go internal/backup/archive_test.go
git commit -m "feat(backup): archive format and spec conversion"
```

---

### Task 2: Archive validation

**Files:**
- Create: `internal/backup/validate.go`
- Test: `internal/backup/validate_test.go`

**Interfaces:**
- Consumes: `Archive`, `TunnelEntry`, `SchemaVersion` from Task 1.
- Produces: type `EntryError` (fields `Index int`, `Name string`, `Field string`, `Msg string`; implements `error`); function `ValidateArchive(*Archive) []EntryError` returning nil when valid.

**Note on permissiveness:** validation is deliberately looser than `api.CreateTunnelRequest`. `remote_host` / `remote_port` are only required for `local` and `remote` types, so a `dynamic` SOCKS5 entry with no destination imports cleanly. This guarantees any exported archive re-imports without rejection.

- [ ] **Step 1: Write the failing test**

Create `internal/backup/validate_test.go`:

```go
package backup

import (
	"strings"
	"testing"
)

func validEntry(name string) TunnelEntry {
	return TunnelEntry{
		Name:       name,
		Type:       "local",
		LocalPort:  5432,
		RemoteHost: "db.internal",
		RemotePort: 5432,
		Hops:       []HopEntry{{Host: "bastion", Port: 22, User: "deploy", AuthMethod: "key"}},
	}
}

func fieldsWithErrors(errs []EntryError) []string {
	out := make([]string, 0, len(errs))
	for _, e := range errs {
		out = append(out, e.Field)
	}
	return out
}

func hasField(errs []EntryError, field string) bool {
	for _, e := range errs {
		if e.Field == field {
			return true
		}
	}
	return false
}

func TestValidateArchiveAcceptsValidArchive(t *testing.T) {
	a := &Archive{Version: SchemaVersion, Tunnels: []TunnelEntry{validEntry("prod-db")}}
	if errs := ValidateArchive(a); len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", fieldsWithErrors(errs))
	}
}

func TestValidateArchiveRejectsWrongVersion(t *testing.T) {
	for _, version := range []int{0, 2, 99} {
		a := &Archive{Version: version, Tunnels: []TunnelEntry{validEntry("prod-db")}}
		errs := ValidateArchive(a)
		if len(errs) == 0 {
			t.Fatalf("version %d: expected an error, got none", version)
		}
		if !hasField(errs, "version") {
			t.Errorf("version %d: expected a version error, got %v", version, fieldsWithErrors(errs))
		}
	}
}

func TestValidateArchiveRejectsDuplicateNames(t *testing.T) {
	a := &Archive{Version: SchemaVersion, Tunnels: []TunnelEntry{
		validEntry("prod-db"),
		validEntry("prod-db"),
	}}
	errs := ValidateArchive(a)
	if !hasField(errs, "name") {
		t.Fatalf("expected a duplicate-name error, got %v", fieldsWithErrors(errs))
	}
	if !strings.Contains(errs[0].Msg, "duplicate") {
		t.Errorf("expected message to mention duplication, got %q", errs[0].Msg)
	}
}

func TestValidateArchiveReportsEveryProblemAtOnce(t *testing.T) {
	// One entry, four independent problems. All must come back together so the
	// file can be fixed in a single pass.
	a := &Archive{Version: SchemaVersion, Tunnels: []TunnelEntry{{
		Name:          "",
		Type:          "sideways",
		DesiredStatus: "humming",
		Hops:          nil,
	}}}
	errs := ValidateArchive(a)
	for _, want := range []string{"name", "type", "desired_status", "hops"} {
		if !hasField(errs, want) {
			t.Errorf("missing error for %q; got %v", want, fieldsWithErrors(errs))
		}
	}
}

func TestValidateArchiveChecksHopFields(t *testing.T) {
	e := validEntry("prod-db")
	e.Hops = []HopEntry{{Host: "", Port: 0, User: "", AuthMethod: "telepathy"}}
	errs := ValidateArchive(&Archive{Version: SchemaVersion, Tunnels: []TunnelEntry{e}})
	for _, want := range []string{"hops[0].host", "hops[0].port", "hops[0].user", "hops[0].auth_method"} {
		if !hasField(errs, want) {
			t.Errorf("missing error for %q; got %v", want, fieldsWithErrors(errs))
		}
	}
}

func TestValidateArchiveAllowsDynamicWithoutRemoteHost(t *testing.T) {
	// A SOCKS5 proxy has no fixed destination. Rejecting it here would make
	// exported dynamic tunnels fail to re-import.
	e := TunnelEntry{
		Name:      "socks",
		Type:      "dynamic",
		LocalPort: 1080,
		Hops:      []HopEntry{{Host: "jump", Port: 22, User: "deploy", AuthMethod: "key"}},
	}
	if errs := ValidateArchive(&Archive{Version: SchemaVersion, Tunnels: []TunnelEntry{e}}); len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", fieldsWithErrors(errs))
	}
}

func TestValidateArchiveRequiresRemoteHostForLocal(t *testing.T) {
	e := validEntry("prod-db")
	e.RemoteHost = ""
	e.RemotePort = 0
	errs := ValidateArchive(&Archive{Version: SchemaVersion, Tunnels: []TunnelEntry{e}})
	if !hasField(errs, "remote_host") || !hasField(errs, "remote_port") {
		t.Fatalf("expected remote_host and remote_port errors, got %v", fieldsWithErrors(errs))
	}
}

func TestValidateArchiveAcceptsEmptyTunnelList(t *testing.T) {
	if errs := ValidateArchive(&Archive{Version: SchemaVersion}); len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", fieldsWithErrors(errs))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/backup/ -run TestValidateArchive -v`
Expected: FAIL — `undefined: ValidateArchive`, `undefined: EntryError`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/backup/validate.go`:

```go
package backup

import (
	"fmt"
	"strings"
)

// EntryError reports one validation failure. Index is -1 for archive-level
// problems such as an unsupported version.
type EntryError struct {
	Index int    `json:"index"`
	Name  string `json:"name,omitempty"`
	Field string `json:"field"`
	Msg   string `json:"message"`
}

func (e EntryError) Error() string {
	if e.Index < 0 {
		return fmt.Sprintf("%s: %s", e.Field, e.Msg)
	}
	return fmt.Sprintf("tunnels[%d] (%s): %s: %s", e.Index, e.Name, e.Field, e.Msg)
}

var (
	validTypes       = map[string]bool{"local": true, "remote": true, "dynamic": true}
	validAuthMethods = map[string]bool{"key": true, "password": true, "agent": true, "cert": true}
	validDesired     = map[string]bool{"": true, "active": true, "stopped": true}
)

// ValidateArchive checks every entry and returns all problems at once, so a
// broken file can be fixed in a single pass. It returns nil when valid.
//
// Validation is deliberately more permissive than api.CreateTunnelRequest:
// remote_host and remote_port are only required for local and remote tunnels,
// so any archive this package exports can always be re-imported.
func ValidateArchive(a *Archive) []EntryError {
	if a.Version != SchemaVersion {
		return []EntryError{{
			Index: -1,
			Field: "version",
			Msg:   fmt.Sprintf("unsupported archive version %d, expected %d", a.Version, SchemaVersion),
		}}
	}

	var errs []EntryError
	seen := make(map[string]int, len(a.Tunnels))

	for i, e := range a.Tunnels {
		add := func(field, msg string) {
			errs = append(errs, EntryError{Index: i, Name: e.Name, Field: field, Msg: msg})
		}

		name := strings.TrimSpace(e.Name)
		switch {
		case name == "":
			add("name", "must not be empty")
		case len(name) > 100:
			add("name", "must be 100 characters or fewer")
		default:
			if prev, dup := seen[name]; dup {
				add("name", fmt.Sprintf("duplicate name, already used by tunnels[%d]", prev))
			} else {
				seen[name] = i
			}
		}

		if !validTypes[e.Type] {
			add("type", fmt.Sprintf("must be local, remote or dynamic (got %q)", e.Type))
		}

		if !validDesired[e.DesiredStatus] {
			add("desired_status", fmt.Sprintf("must be active or stopped (got %q)", e.DesiredStatus))
		}

		if e.LocalPort < 0 || e.LocalPort > 65535 {
			add("local_port", "must be between 0 and 65535")
		}

		// A dynamic tunnel is a SOCKS5 proxy with no fixed destination.
		if e.Type != "dynamic" {
			if strings.TrimSpace(e.RemoteHost) == "" {
				add("remote_host", "must not be empty")
			}
			if e.RemotePort < 1 || e.RemotePort > 65535 {
				add("remote_port", "must be between 1 and 65535")
			}
		}

		if len(e.Hops) == 0 {
			add("hops", "at least one hop is required")
		}
		for j, h := range e.Hops {
			if strings.TrimSpace(h.Host) == "" {
				add(fmt.Sprintf("hops[%d].host", j), "must not be empty")
			}
			if strings.TrimSpace(h.User) == "" {
				add(fmt.Sprintf("hops[%d].user", j), "must not be empty")
			}
			if h.Port < 1 || h.Port > 65535 {
				add(fmt.Sprintf("hops[%d].port", j), "must be between 1 and 65535")
			}
			if h.AuthMethod != "" && !validAuthMethods[h.AuthMethod] {
				add(fmt.Sprintf("hops[%d].auth_method", j),
					fmt.Sprintf("must be key, password, agent or cert (got %q)", h.AuthMethod))
			}
		}
	}

	return errs
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/backup/ -v`
Expected: PASS — Task 1 and Task 2 tests, 12 total.

- [ ] **Step 5: Commit**

```bash
git add internal/backup/validate.go internal/backup/validate_test.go
git commit -m "feat(backup): archive validation reporting all problems at once"
```

---

### Task 3: Store interface and Export

**Files:**
- Create: `internal/backup/export.go`
- Test: `internal/backup/export_test.go`, `internal/backup/fake_store_test.go`

**Interfaces:**
- Consumes: `Archive`, `TunnelEntry`, `EntryFromSpec`, `SchemaVersion` from Task 1.
- Produces: interface `Store` (`List`, `Save`, `Delete`); type `Clock func() time.Time`; function `Export(ctx context.Context, store Store, source string, now Clock) (*Archive, error)`. Also the shared test double `fakeStore` used by Tasks 3 and 5.

- [ ] **Step 1: Write the failing test**

Create `internal/backup/fake_store_test.go` — the shared test double:

```go
package backup

import (
	"context"
	"fmt"

	"github.com/craigderington/lazytunnel/pkg/types"
)

// fakeStore is an in-memory Store for tests. Insertion order is preserved so
// List is deterministic.
type fakeStore struct {
	order   []string
	specs   map[string]*types.TunnelSpec
	saveErr map[string]error // by tunnel name
	delErr  map[string]error // by tunnel ID
	saves   int
	deletes int
}

func newFakeStore(specs ...*types.TunnelSpec) *fakeStore {
	f := &fakeStore{
		specs:   make(map[string]*types.TunnelSpec, len(specs)),
		saveErr: make(map[string]error),
		delErr:  make(map[string]error),
	}
	for _, s := range specs {
		f.order = append(f.order, s.ID)
		f.specs[s.ID] = s
	}
	return f
}

func (f *fakeStore) List(ctx context.Context) ([]*types.TunnelSpec, error) {
	out := make([]*types.TunnelSpec, 0, len(f.order))
	for _, id := range f.order {
		if spec, ok := f.specs[id]; ok {
			out = append(out, spec)
		}
	}
	return out, nil
}

func (f *fakeStore) Save(ctx context.Context, spec *types.TunnelSpec) error {
	if err, ok := f.saveErr[spec.Name]; ok {
		return err
	}
	f.saves++
	if _, exists := f.specs[spec.ID]; !exists {
		f.order = append(f.order, spec.ID)
	}
	f.specs[spec.ID] = spec
	return nil
}

func (f *fakeStore) Delete(ctx context.Context, id string) error {
	if err, ok := f.delErr[id]; ok {
		return err
	}
	if _, exists := f.specs[id]; !exists {
		return fmt.Errorf("tunnel not found: %s", id)
	}
	f.deletes++
	delete(f.specs, id)
	for i, existing := range f.order {
		if existing == id {
			f.order = append(f.order[:i], f.order[i+1:]...)
			break
		}
	}
	return nil
}
```

Create `internal/backup/export_test.go`:

```go
package backup

import (
	"context"
	"testing"
	"time"
)

func fixedClock(t time.Time) Clock { return func() time.Time { return t } }

func TestExportProducesVersionedArchive(t *testing.T) {
	at := time.Date(2026, 7, 29, 8, 40, 12, 0, time.UTC)
	store := newFakeStore(sampleSpec())

	archive, err := Export(context.Background(), store, "lazytunnel/1.0.0", fixedClock(at))
	if err != nil {
		t.Fatalf("Export returned error: %v", err)
	}
	if archive.Version != SchemaVersion {
		t.Errorf("got version %d, want %d", archive.Version, SchemaVersion)
	}
	if !archive.ExportedAt.Equal(at) {
		t.Errorf("got ExportedAt %v, want %v", archive.ExportedAt, at)
	}
	if archive.Source != "lazytunnel/1.0.0" {
		t.Errorf("got Source %q, want lazytunnel/1.0.0", archive.Source)
	}
	if len(archive.Tunnels) != 1 {
		t.Fatalf("got %d tunnels, want 1", len(archive.Tunnels))
	}
	if archive.Tunnels[0].Name != "prod-db" {
		t.Errorf("got name %q, want prod-db", archive.Tunnels[0].Name)
	}
}

func TestExportOfEmptyStoreIsValid(t *testing.T) {
	archive, err := Export(context.Background(), newFakeStore(), "test", fixedClock(time.Now()))
	if err != nil {
		t.Fatalf("Export returned error: %v", err)
	}
	if len(archive.Tunnels) != 0 {
		t.Fatalf("got %d tunnels, want 0", len(archive.Tunnels))
	}
	if errs := ValidateArchive(archive); len(errs) != 0 {
		t.Fatalf("empty export failed validation: %v", errs)
	}
}

func TestExportOutputPassesValidation(t *testing.T) {
	archive, err := Export(context.Background(), newFakeStore(sampleSpec()), "test", fixedClock(time.Now()))
	if err != nil {
		t.Fatalf("Export returned error: %v", err)
	}
	if errs := ValidateArchive(archive); len(errs) != 0 {
		t.Fatalf("exported archive failed its own validation: %v", errs)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/backup/ -run TestExport -v`
Expected: FAIL — `undefined: Export`, `undefined: Clock`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/backup/export.go`:

```go
package backup

import (
	"context"
	"fmt"
	"time"

	"github.com/craigderington/lazytunnel/pkg/types"
)

// Store is the narrow slice of persistence this package needs.
// *storage.SQLiteStore and tunnel.Storage both satisfy it.
type Store interface {
	List(ctx context.Context) ([]*types.TunnelSpec, error)
	Save(ctx context.Context, spec *types.TunnelSpec) error
	Delete(ctx context.Context, tunnelID string) error
}

// Clock supplies the current time. Injected so tests are deterministic.
type Clock func() time.Time

// Export reads every stored tunnel and returns a complete archive.
func Export(ctx context.Context, store Store, source string, now Clock) (*Archive, error) {
	if now == nil {
		now = time.Now
	}

	specs, err := store.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list tunnels for export: %w", err)
	}

	entries := make([]TunnelEntry, 0, len(specs))
	for _, spec := range specs {
		entries = append(entries, EntryFromSpec(spec))
	}

	return &Archive{
		Version:    SchemaVersion,
		ExportedAt: now().UTC(),
		Source:     source,
		Tunnels:    entries,
	}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/backup/ -v`
Expected: PASS — 15 tests.

- [ ] **Step 5: Commit**

```bash
git add internal/backup/export.go internal/backup/export_test.go internal/backup/fake_store_test.go
git commit -m "feat(backup): Store interface and Export"
```

---

### Task 4: The diff engine

This is the heart of the feature. `Plan` performs no I/O, so every rule is unit-testable without a database, HTTP, or SSH.

**Files:**
- Create: `internal/backup/plan.go`
- Test: `internal/backup/plan_test.go`

**Interfaces:**
- Consumes: `Archive`, `TunnelEntry`, `EntryFromSpec`, `SpecFromEntry` (Task 1); `ValidateArchive`, `EntryError` (Task 2); `Clock` (Task 3).
- Produces: type `Mode` with `ModeMerge` / `ModeReplace`; `ParseMode(string) (Mode, error)`; type `Action` with `ActionCreate` / `ActionUpdate` / `ActionSkip` / `ActionDelete`; types `PlanItem`, `ImportPlan`, `PlanOptions`, `ArchiveInvalidError`; function `Plan(current []*types.TunnelSpec, archive *Archive, opts PlanOptions) (*ImportPlan, error)`.

- [ ] **Step 1: Write the failing test**

Create `internal/backup/plan_test.go`:

```go
package backup

import (
	"errors"
	"testing"
	"time"

	"github.com/craigderington/lazytunnel/pkg/types"
)

func testOptions(mode Mode) PlanOptions {
	n := 0
	return PlanOptions{
		Mode:         mode,
		DefaultOwner: "importer",
		NewID: func() string {
			n++
			return "generated-id-" + string(rune('0'+n))
		},
		Now: fixedClock(time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)),
	}
}

func archiveOf(entries ...TunnelEntry) *Archive {
	return &Archive{Version: SchemaVersion, Tunnels: entries}
}

func itemFor(t *testing.T, plan *ImportPlan, name string) PlanItem {
	t.Helper()
	for _, item := range plan.Items {
		if item.Name == name {
			return item
		}
	}
	t.Fatalf("no plan item for %q; plan has %d items", name, len(plan.Items))
	return PlanItem{}
}

func TestPlanCreatesTunnelWithNoNameMatch(t *testing.T) {
	plan, err := Plan(nil, archiveOf(validEntry("staging-api")), testOptions(ModeMerge))
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	item := itemFor(t, plan, "staging-api")
	if item.Action != ActionCreate {
		t.Errorf("got action %q, want create", item.Action)
	}
	if item.Spec == nil {
		t.Fatal("create item must carry a spec")
	}
	if item.Spec.Owner != "importer" {
		t.Errorf("got owner %q, want importer (the DefaultOwner)", item.Spec.Owner)
	}
}

func TestPlanUpdatesOnNameMatchAndPreservesIdentity(t *testing.T) {
	stored := sampleSpec()
	stored.Owner = "original-owner"

	changed := EntryFromSpec(stored)
	changed.ID = "a-completely-different-id"
	changed.Owner = "someone-else"
	changed.LocalPort = 15432

	plan, err := Plan([]*types.TunnelSpec{stored}, archiveOf(changed), testOptions(ModeMerge))
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}

	item := itemFor(t, plan, "prod-db")
	if item.Action != ActionUpdate {
		t.Fatalf("got action %q, want update", item.Action)
	}
	if item.Spec.ID != stored.ID {
		t.Errorf("got ID %q, want the stored ID %q — the UI's saved ordering keys on it",
			item.Spec.ID, stored.ID)
	}
	if item.Spec.Owner != "original-owner" {
		t.Errorf("got owner %q, want original-owner — a restore must not reassign ownership",
			item.Spec.Owner)
	}
	if !item.Spec.CreatedAt.Equal(stored.CreatedAt) {
		t.Errorf("got CreatedAt %v, want the stored %v", item.Spec.CreatedAt, stored.CreatedAt)
	}
	if item.Spec.LocalPort != 15432 {
		t.Errorf("got LocalPort %d, want 15432 — the change should be applied", item.Spec.LocalPort)
	}
}

func TestPlanSkipsIdenticalTunnel(t *testing.T) {
	stored := sampleSpec()
	plan, err := Plan([]*types.TunnelSpec{stored}, archiveOf(EntryFromSpec(stored)), testOptions(ModeMerge))
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	if item := itemFor(t, plan, "prod-db"); item.Action != ActionSkip {
		t.Fatalf("got action %q, want skip — re-import must not bounce a healthy tunnel", item.Action)
	}
}

func TestPlanDoesNotSkipWhenOnlyDesiredStatusChanges(t *testing.T) {
	stored := sampleSpec()
	stored.DesiredStatus = types.DesiredStatusStopped

	entry := EntryFromSpec(stored)
	entry.DesiredStatus = "active"

	plan, err := Plan([]*types.TunnelSpec{stored}, archiveOf(entry), testOptions(ModeMerge))
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	if item := itemFor(t, plan, "prod-db"); item.Action != ActionUpdate {
		t.Fatalf("got action %q, want update — stopped to active is a real change", item.Action)
	}
}

func TestPlanTreatsOmittedDesiredStatusAsStopped(t *testing.T) {
	stored := sampleSpec()
	stored.DesiredStatus = types.DesiredStatusStopped

	entry := EntryFromSpec(stored)
	entry.DesiredStatus = "" // hand-written archive omitting the field

	plan, err := Plan([]*types.TunnelSpec{stored}, archiveOf(entry), testOptions(ModeMerge))
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	if item := itemFor(t, plan, "prod-db"); item.Action != ActionSkip {
		t.Fatalf("got action %q, want skip — an omitted field is not a change", item.Action)
	}
}

func TestPlanMergeNeverDeletes(t *testing.T) {
	stored := sampleSpec()
	plan, err := Plan([]*types.TunnelSpec{stored}, archiveOf(validEntry("other")), testOptions(ModeMerge))
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	for _, item := range plan.Items {
		if item.Action == ActionDelete {
			t.Fatalf("merge produced a delete for %q", item.Name)
		}
	}
}

func TestPlanReplaceDeletesTunnelsAbsentFromArchive(t *testing.T) {
	stored := sampleSpec()
	plan, err := Plan([]*types.TunnelSpec{stored}, archiveOf(validEntry("other")), testOptions(ModeReplace))
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	item := itemFor(t, plan, "prod-db")
	if item.Action != ActionDelete {
		t.Fatalf("got action %q, want delete", item.Action)
	}
	if item.ID != stored.ID {
		t.Errorf("got delete ID %q, want %q", item.ID, stored.ID)
	}
	if item.Spec != nil {
		t.Error("a delete item must not carry a spec")
	}
}

func TestPlanReusesFreeArchiveID(t *testing.T) {
	entry := validEntry("staging-api")
	entry.ID = "id-from-the-backup"

	plan, err := Plan(nil, archiveOf(entry), testOptions(ModeMerge))
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	if item := itemFor(t, plan, "staging-api"); item.Spec.ID != "id-from-the-backup" {
		t.Fatalf("got ID %q, want the archive's ID reused for a faithful restore", item.Spec.ID)
	}
}

func TestPlanRegeneratesCollidingArchiveID(t *testing.T) {
	stored := sampleSpec() // name prod-db, ID 9f1c...

	entry := validEntry("staging-api")
	entry.ID = stored.ID // same ID, different name

	plan, err := Plan([]*types.TunnelSpec{stored}, archiveOf(entry), testOptions(ModeMerge))
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	item := itemFor(t, plan, "staging-api")
	if item.Spec.ID == stored.ID {
		t.Fatal("a colliding ID must be regenerated, not allowed to clobber an unrelated tunnel")
	}
	if item.Spec.ID != "generated-id-1" {
		t.Errorf("got ID %q, want generated-id-1", item.Spec.ID)
	}
}

func TestPlanMintsIDWhenArchiveHasNone(t *testing.T) {
	plan, err := Plan(nil, archiveOf(validEntry("staging-api")), testOptions(ModeMerge))
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	if item := itemFor(t, plan, "staging-api"); item.Spec.ID != "generated-id-1" {
		t.Fatalf("got ID %q, want generated-id-1", item.Spec.ID)
	}
}

func TestPlanRejectsInvalidArchive(t *testing.T) {
	bad := archiveOf(validEntry("a"), validEntry("a")) // duplicate names
	_, err := Plan(nil, bad, testOptions(ModeMerge))
	if err == nil {
		t.Fatal("expected an error for an invalid archive, got nil")
	}
	var invalid ArchiveInvalidError
	if !errors.As(err, &invalid) {
		t.Fatalf("got %T, want ArchiveInvalidError", err)
	}
	if len(invalid.Errors) == 0 {
		t.Error("ArchiveInvalidError must carry the underlying entry errors")
	}
}

func TestPlanOfUnchangedArchiveIsAllSkip(t *testing.T) {
	stored := []*types.TunnelSpec{sampleSpec()}
	archive := archiveOf(EntryFromSpec(stored[0]))

	for round := 1; round <= 2; round++ {
		plan, err := Plan(stored, archive, testOptions(ModeReplace))
		if err != nil {
			t.Fatalf("round %d: Plan returned error: %v", round, err)
		}
		for _, item := range plan.Items {
			if item.Action != ActionSkip {
				t.Fatalf("round %d: got action %q for %q, want skip — import must be idempotent",
					round, item.Action, item.Name)
			}
		}
	}
}

func TestPlanEmptyArchiveInMergeModeChangesNothing(t *testing.T) {
	plan, err := Plan([]*types.TunnelSpec{sampleSpec()}, archiveOf(), testOptions(ModeMerge))
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	if len(plan.Items) != 0 {
		t.Fatalf("got %d items, want 0", len(plan.Items))
	}
}

func TestParseMode(t *testing.T) {
	for input, want := range map[string]Mode{"": ModeMerge, "merge": ModeMerge, "replace": ModeReplace} {
		got, err := ParseMode(input)
		if err != nil {
			t.Errorf("ParseMode(%q) returned error: %v", input, err)
		}
		if got != want {
			t.Errorf("ParseMode(%q) = %q, want %q", input, got, want)
		}
	}
	if _, err := ParseMode("obliterate"); err == nil {
		t.Error("expected an error for an unknown mode, got nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/backup/ -run 'TestPlan|TestParseMode' -v`
Expected: FAIL — `undefined: Plan`, `undefined: PlanOptions`, `undefined: ModeMerge`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/backup/plan.go`:

```go
package backup

import (
	"fmt"
	"reflect"
	"time"

	"github.com/google/uuid"

	"github.com/craigderington/lazytunnel/pkg/types"
)

// Mode selects how an import treats tunnels that are stored but absent from
// the archive.
type Mode string

const (
	// ModeMerge updates and creates, and never deletes.
	ModeMerge Mode = "merge"
	// ModeReplace additionally deletes stored tunnels absent from the archive,
	// so the fleet ends up mirroring the file exactly.
	ModeReplace Mode = "replace"
)

// ParseMode maps a ?mode= query value onto a Mode, defaulting to merge.
func ParseMode(s string) (Mode, error) {
	switch s {
	case "", string(ModeMerge):
		return ModeMerge, nil
	case string(ModeReplace):
		return ModeReplace, nil
	default:
		return "", fmt.Errorf("unknown import mode %q, expected merge or replace", s)
	}
}

// Action is what an import will do to a single tunnel.
type Action string

const (
	ActionCreate Action = "create"
	ActionUpdate Action = "update"
	ActionSkip   Action = "skip"
	ActionDelete Action = "delete"
)

// PlanItem is one tunnel's worth of intended change.
type PlanItem struct {
	Action Action            `json:"action"`
	Name   string            `json:"name"`
	ID     string            `json:"id"`
	Reason string            `json:"reason,omitempty"`
	Spec   *types.TunnelSpec `json:"-"` // nil for skip and delete
}

// ImportPlan is the full set of intended changes.
type ImportPlan struct {
	Mode  Mode       `json:"mode"`
	Items []PlanItem `json:"items"`
}

// PlanOptions carries the identity inputs Plan cannot derive from the archive.
type PlanOptions struct {
	Mode Mode
	// DefaultOwner is applied to created tunnels whose entry has no owner.
	DefaultOwner string
	// NewID mints IDs for created tunnels. Injected for deterministic tests.
	NewID func() string
	// Now stamps CreatedAt and UpdatedAt. Injected for deterministic tests.
	Now Clock
}

// ArchiveInvalidError reports every validation failure in a single value.
type ArchiveInvalidError struct {
	Errors []EntryError
}

func (e ArchiveInvalidError) Error() string {
	if len(e.Errors) == 0 {
		return "archive is invalid"
	}
	return fmt.Sprintf("archive is invalid: %d problem(s), first: %s", len(e.Errors), e.Errors[0].Error())
}

// Plan diffs an archive against the currently stored tunnels.
//
// It performs no I/O, so every rule here is unit-testable without a database.
// Tunnels are matched on name — the UNIQUE column in the tunnels table — and a
// matched tunnel keeps its stored ID, Owner and CreatedAt, because a restore
// must not reassign ownership or churn the IDs the web UI's saved ordering
// depends on.
func Plan(current []*types.TunnelSpec, archive *Archive, opts PlanOptions) (*ImportPlan, error) {
	if errs := ValidateArchive(archive); len(errs) > 0 {
		return nil, ArchiveInvalidError{Errors: errs}
	}

	if opts.Mode == "" {
		opts.Mode = ModeMerge
	}
	if opts.NewID == nil {
		opts.NewID = func() string { return uuid.New().String() }
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.DefaultOwner == "" {
		opts.DefaultOwner = "api-user"
	}

	byName := make(map[string]*types.TunnelSpec, len(current))
	usedIDs := make(map[string]bool, len(current))
	for _, spec := range current {
		byName[spec.Name] = spec
		usedIDs[spec.ID] = true
	}

	plan := &ImportPlan{Mode: opts.Mode}
	matched := make(map[string]bool, len(archive.Tunnels))

	for _, entry := range archive.Tunnels {
		spec := SpecFromEntry(entry)
		existing, found := byName[entry.Name]

		if !found {
			id := entry.ID
			if id == "" || usedIDs[id] {
				id = opts.NewID()
			}
			usedIDs[id] = true

			spec.ID = id
			if spec.Owner == "" {
				spec.Owner = opts.DefaultOwner
			}
			spec.CreatedAt = opts.Now().UTC()
			spec.UpdatedAt = spec.CreatedAt

			plan.Items = append(plan.Items, PlanItem{
				Action: ActionCreate,
				Name:   spec.Name,
				ID:     spec.ID,
				Spec:   spec,
			})
			continue
		}

		matched[entry.Name] = true

		// An existing tunnel keeps its identity across a restore.
		spec.ID = existing.ID
		spec.Owner = existing.Owner
		spec.CreatedAt = existing.CreatedAt

		if entriesEqual(entry, existing) {
			plan.Items = append(plan.Items, PlanItem{
				Action: ActionSkip,
				Name:   spec.Name,
				ID:     spec.ID,
				Reason: "identical to stored tunnel",
			})
			continue
		}

		spec.UpdatedAt = opts.Now().UTC()
		plan.Items = append(plan.Items, PlanItem{
			Action: ActionUpdate,
			Name:   spec.Name,
			ID:     spec.ID,
			Spec:   spec,
		})
	}

	if opts.Mode == ModeReplace {
		for _, spec := range current {
			if matched[spec.Name] {
				continue
			}
			plan.Items = append(plan.Items, PlanItem{
				Action: ActionDelete,
				Name:   spec.Name,
				ID:     spec.ID,
				Reason: "not present in archive",
			})
		}
	}

	return plan, nil
}

// entriesEqual reports whether an archive entry describes exactly what is
// already stored.
//
// Both sides are round-tripped through SpecFromEntry and EntryFromSpec so that
// representation differences — a nil versus empty hop slice, an omitted
// desired_status — never register as a change. ID and Owner are zeroed because
// Plan resolves them from the stored tunnel rather than the file. Everything
// else, desired_status included, counts: a tunnel whose only change is stopped
// to active must not be skipped.
func entriesEqual(entry TunnelEntry, stored *types.TunnelSpec) bool {
	a := EntryFromSpec(SpecFromEntry(entry))
	b := EntryFromSpec(stored)
	a.ID, b.ID = "", ""
	a.Owner, b.Owner = "", ""
	return reflect.DeepEqual(a, b)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/backup/ -v`
Expected: PASS — 29 tests.

- [ ] **Step 5: Commit**

```bash
git add internal/backup/plan.go internal/backup/plan_test.go
git commit -m "feat(backup): pure diff engine for merge and replace imports"
```

---

### Task 5: Apply a plan to storage

**Files:**
- Create: `internal/backup/apply.go`
- Test: `internal/backup/apply_test.go`

**Interfaces:**
- Consumes: `Store` (Task 3); `ImportPlan`, `PlanItem`, `Action`, `Mode` (Task 4).
- Produces: types `ItemResult`, `Report`; functions `NewDryRunReport(*ImportPlan) *Report` and `Apply(ctx context.Context, store Store, plan *ImportPlan) (*Report, error)`. `Apply` always returns a non-nil report, even on error.

- [ ] **Step 1: Write the failing test**

Create `internal/backup/apply_test.go`:

```go
package backup

import (
	"context"
	"errors"
	"testing"

	"github.com/craigderington/lazytunnel/pkg/types"
)

func TestApplyWritesCreatesAndUpdates(t *testing.T) {
	stored := sampleSpec()
	store := newFakeStore(stored)

	changed := EntryFromSpec(stored)
	changed.LocalPort = 15432

	plan, err := Plan([]*types.TunnelSpec{stored}, archiveOf(changed, validEntry("staging-api")), testOptions(ModeMerge))
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}

	report, err := Apply(context.Background(), store, plan)
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if report.Created != 1 {
		t.Errorf("got Created %d, want 1", report.Created)
	}
	if report.Updated != 1 {
		t.Errorf("got Updated %d, want 1", report.Updated)
	}
	if store.saves != 2 {
		t.Errorf("got %d saves, want 2", store.saves)
	}
}

func TestApplySkipDoesNotWrite(t *testing.T) {
	stored := sampleSpec()
	store := newFakeStore(stored)

	plan, err := Plan([]*types.TunnelSpec{stored}, archiveOf(EntryFromSpec(stored)), testOptions(ModeMerge))
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}

	report, err := Apply(context.Background(), store, plan)
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if report.Skipped != 1 {
		t.Errorf("got Skipped %d, want 1", report.Skipped)
	}
	if store.saves != 0 {
		t.Errorf("got %d saves, want 0 — a skip must not touch storage", store.saves)
	}
}

func TestApplyReplaceDeletes(t *testing.T) {
	stored := sampleSpec()
	store := newFakeStore(stored)

	plan, err := Plan([]*types.TunnelSpec{stored}, archiveOf(validEntry("other")), testOptions(ModeReplace))
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}

	report, err := Apply(context.Background(), store, plan)
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if report.Deleted != 1 {
		t.Errorf("got Deleted %d, want 1", report.Deleted)
	}
	if store.deletes != 1 {
		t.Errorf("got %d deletes, want 1", store.deletes)
	}
}

func TestApplyReportsWhichItemsLandedOnPartialFailure(t *testing.T) {
	// Storage has no transaction API, so this is validate-then-write, not
	// rollback. The report must name exactly what succeeded.
	store := newFakeStore()
	store.saveErr["bad"] = errors.New("disk on fire")

	good := validEntry("good")
	bad := validEntry("bad")

	plan, err := Plan(nil, archiveOf(good, bad), testOptions(ModeMerge))
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}

	report, err := Apply(context.Background(), store, plan)
	if err == nil {
		t.Fatal("expected an error when a write fails, got nil")
	}
	if report == nil {
		t.Fatal("Apply must return the report even on failure")
	}
	if report.Created != 1 {
		t.Errorf("got Created %d, want 1 — the good tunnel did land", report.Created)
	}
	if report.Failed != 1 {
		t.Errorf("got Failed %d, want 1", report.Failed)
	}

	var failed *ItemResult
	for i := range report.Items {
		if report.Items[i].Name == "bad" {
			failed = &report.Items[i]
		}
	}
	if failed == nil {
		t.Fatal("report has no item for the failing tunnel")
	}
	if failed.Error == "" {
		t.Error("the failing item must carry its error message")
	}
}

func TestNewDryRunReportWritesNothing(t *testing.T) {
	stored := sampleSpec()
	store := newFakeStore(stored)

	plan, err := Plan([]*types.TunnelSpec{stored}, archiveOf(validEntry("staging-api")), testOptions(ModeMerge))
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}

	report := NewDryRunReport(plan)
	if !report.DryRun {
		t.Error("dry-run report must be flagged as such")
	}
	if report.Created != 1 {
		t.Errorf("got Created %d, want 1", report.Created)
	}
	if store.saves != 0 || store.deletes != 0 {
		t.Errorf("dry run touched storage: %d saves, %d deletes", store.saves, store.deletes)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/backup/ -run 'TestApply|TestNewDryRun' -v`
Expected: FAIL — `undefined: Apply`, `undefined: NewDryRunReport`, `undefined: ItemResult`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/backup/apply.go`:

```go
package backup

import (
	"context"
	"fmt"
)

// ItemResult records what happened to one planned item.
type ItemResult struct {
	Action Action `json:"action"`
	Name   string `json:"name"`
	ID     string `json:"id"`
	Reason string `json:"reason,omitempty"`
	Error  string `json:"error,omitempty"`
}

// Report is the outcome of an import. A dry-run report carries the intended
// actions with no Error fields set.
type Report struct {
	Mode    Mode         `json:"mode"`
	DryRun  bool         `json:"dry_run"`
	Items   []ItemResult `json:"items"`
	Created int          `json:"created"`
	Updated int          `json:"updated"`
	Skipped int          `json:"skipped"`
	Deleted int          `json:"deleted"`
	Failed  int          `json:"failed"`
}

func (r *Report) add(item ItemResult) {
	r.Items = append(r.Items, item)

	if item.Error != "" {
		r.Failed++
		return
	}

	switch item.Action {
	case ActionCreate:
		r.Created++
	case ActionUpdate:
		r.Updated++
	case ActionSkip:
		r.Skipped++
	case ActionDelete:
		r.Deleted++
	}
}

// NewDryRunReport renders a plan as a report without touching storage.
func NewDryRunReport(plan *ImportPlan) *Report {
	report := &Report{Mode: plan.Mode, DryRun: true, Items: []ItemResult{}}
	for _, item := range plan.Items {
		report.add(ItemResult{
			Action: item.Action,
			Name:   item.Name,
			ID:     item.ID,
			Reason: item.Reason,
		})
	}
	return report
}

// Apply writes a plan to storage.
//
// Plan has already validated the whole archive, so most failures are caught
// before anything is written. tunnel.Storage exposes no transaction API, so
// this is validate-then-write rather than rollback: if a write fails partway,
// the report names exactly which items landed and Apply returns an error
// alongside it. Merge is idempotent, so re-running converges.
func Apply(ctx context.Context, store Store, plan *ImportPlan) (*Report, error) {
	report := &Report{Mode: plan.Mode, Items: []ItemResult{}}
	var firstErr error

	for _, item := range plan.Items {
		result := ItemResult{
			Action: item.Action,
			Name:   item.Name,
			ID:     item.ID,
			Reason: item.Reason,
		}

		var err error
		switch item.Action {
		case ActionCreate, ActionUpdate:
			err = store.Save(ctx, item.Spec)
		case ActionDelete:
			err = store.Delete(ctx, item.ID)
		case ActionSkip:
			// Nothing to write.
		}

		if err != nil {
			result.Error = err.Error()
			if firstErr == nil {
				firstErr = fmt.Errorf("failed to %s tunnel %q: %w", item.Action, item.Name, err)
			}
		}

		report.add(result)
	}

	return report, firstErr
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/backup/ -v`
Expected: PASS — 34 tests.

- [ ] **Step 5: Commit**

```bash
git add internal/backup/apply.go internal/backup/apply_test.go
git commit -m "feat(backup): apply plans to storage with a per-item report"
```

---

### Task 6: Manager.Reconcile

**Files:**
- Create: `internal/tunnel/reconcile.go`
- Test: `internal/tunnel/reconcile_test.go`

**Interfaces:**
- Consumes: existing `Manager` internals (`m.tunnels`, `m.mu`, `m.storage`, `m.nodeAgentID`, `m.statusCallback`, `m.circuitBreaker`), `Tunnel`, `RunOnThisNode`, `Manager.Start`, `Manager.Stop`, `Tunnel.Stop`, `Tunnel.GetStatus`.
- Produces: `func (m *Manager) Reconcile(ctx context.Context) error`, plus unexported helpers `dropTunnel`, `upsertTunnel`, `applyDesired`.

**Locking rule:** compute the delta under `RLock`, release it, then act. `Start`, `Stop`, `dropTunnel` and `upsertTunnel` all acquire `m.mu` themselves, so holding it across the delta deadlocks. Collect `*Tunnel` pointers under the lock and call `GetStatus()` only after releasing it — never nest `m.mu` and `t.mu`.

**Why not the existing methods:** `Manager.Delete` also deletes the storage row, which is already gone during a reconcile, so `SQLiteStore.Delete` would error on zero rows affected. `dropTunnel` therefore stops and unregisters without touching storage.

All test tunnels carry `AgentID: "remote-agent"` so `RunOnThisNode` is false and nothing attempts a real SSH connection.

- [ ] **Step 1: Write the failing test**

Create `internal/tunnel/reconcile_test.go`:

```go
package tunnel

import (
	"context"
	"testing"
	"time"

	"github.com/craigderington/lazytunnel/pkg/types"
)

// fakeStorage implements the full tunnel.Storage interface in memory.
type fakeStorage struct {
	specs map[string]*types.TunnelSpec
	order []string
}

func newFakeStorage(specs ...*types.TunnelSpec) *fakeStorage {
	f := &fakeStorage{specs: make(map[string]*types.TunnelSpec, len(specs))}
	for _, s := range specs {
		f.order = append(f.order, s.ID)
		f.specs[s.ID] = s
	}
	return f
}

func (f *fakeStorage) Save(ctx context.Context, spec *types.TunnelSpec) error {
	if _, exists := f.specs[spec.ID]; !exists {
		f.order = append(f.order, spec.ID)
	}
	f.specs[spec.ID] = spec
	return nil
}

func (f *fakeStorage) Update(ctx context.Context, spec *types.TunnelSpec) error {
	return f.Save(ctx, spec)
}

func (f *fakeStorage) UpdateStatus(ctx context.Context, tunnelID, status string) error { return nil }

func (f *fakeStorage) UpdateDesiredStatus(ctx context.Context, tunnelID string, status types.DesiredStatus) error {
	if spec, ok := f.specs[tunnelID]; ok {
		spec.DesiredStatus = status
	}
	return nil
}

func (f *fakeStorage) Delete(ctx context.Context, tunnelID string) error {
	delete(f.specs, tunnelID)
	for i, id := range f.order {
		if id == tunnelID {
			f.order = append(f.order[:i], f.order[i+1:]...)
			break
		}
	}
	return nil
}

func (f *fakeStorage) Get(ctx context.Context, tunnelID string) (*types.TunnelSpec, error) {
	if spec, ok := f.specs[tunnelID]; ok {
		return spec, nil
	}
	return nil, context.Canceled
}

func (f *fakeStorage) List(ctx context.Context) ([]*types.TunnelSpec, error) {
	out := make([]*types.TunnelSpec, 0, len(f.order))
	for _, id := range f.order {
		if spec, ok := f.specs[id]; ok {
			out = append(out, spec)
		}
	}
	return out, nil
}

func (f *fakeStorage) ListByAgent(ctx context.Context, agentID string) ([]*types.TunnelSpec, error) {
	return f.List(ctx)
}

func (f *fakeStorage) Close() error { return nil }

// remoteSpec builds a spec assigned to a remote agent, so RunOnThisNode is
// false and reconcile never attempts a real SSH connection.
func remoteSpec(id, name string, port int) *types.TunnelSpec {
	return &types.TunnelSpec{
		ID:            id,
		Name:          name,
		Owner:         "admin",
		AgentID:       "remote-agent",
		DesiredStatus: types.DesiredStatusStopped,
		Type:          types.TunnelTypeLocal,
		Hops:          []types.Hop{{Host: "bastion", Port: 22, User: "deploy", AuthMethod: types.AuthMethodKey}},
		LocalPort:     port,
		RemoteHost:    "db.internal",
		RemotePort:    5432,
		KeepAlive:     30 * time.Second,
		MaxRetries:    5,
	}
}

func managerWith(store Storage) *Manager {
	m := NewManager(context.Background())
	m.SetStorage(store)
	return m
}

func TestReconcileAddsTunnelsPresentOnlyInStorage(t *testing.T) {
	store := newFakeStorage(remoteSpec("id-1", "prod-db", 5432))
	m := managerWith(store)

	if err := m.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	if _, err := m.Get("id-1"); err != nil {
		t.Fatalf("expected tunnel id-1 to be adopted: %v", err)
	}
}

func TestReconcileRemovesTunnelsMissingFromStorage(t *testing.T) {
	spec := remoteSpec("id-1", "prod-db", 5432)
	store := newFakeStorage(spec)
	m := managerWith(store)

	if err := m.Reconcile(context.Background()); err != nil {
		t.Fatalf("first Reconcile returned error: %v", err)
	}

	// Storage row disappears, as it would after a replace-mode import.
	_ = store.Delete(context.Background(), "id-1")

	if err := m.Reconcile(context.Background()); err != nil {
		t.Fatalf("second Reconcile returned error: %v", err)
	}
	if _, err := m.Get("id-1"); err == nil {
		t.Fatal("expected tunnel id-1 to be removed from the manager")
	}
}

func TestReconcilePicksUpSpecChanges(t *testing.T) {
	spec := remoteSpec("id-1", "prod-db", 5432)
	store := newFakeStorage(spec)
	m := managerWith(store)

	if err := m.Reconcile(context.Background()); err != nil {
		t.Fatalf("first Reconcile returned error: %v", err)
	}

	updated := remoteSpec("id-1", "prod-db", 15432)
	_ = store.Save(context.Background(), updated)

	if err := m.Reconcile(context.Background()); err != nil {
		t.Fatalf("second Reconcile returned error: %v", err)
	}

	tun, err := m.Get("id-1")
	if err != nil {
		t.Fatalf("tunnel id-1 vanished: %v", err)
	}
	if tun.Spec.LocalPort != 15432 {
		t.Fatalf("got LocalPort %d, want 15432", tun.Spec.LocalPort)
	}
}

func TestReconcileLeavesUnchangedTunnelsAlone(t *testing.T) {
	spec := remoteSpec("id-1", "prod-db", 5432)
	store := newFakeStorage(spec)
	m := managerWith(store)

	if err := m.Reconcile(context.Background()); err != nil {
		t.Fatalf("first Reconcile returned error: %v", err)
	}

	before, err := m.Get("id-1")
	if err != nil {
		t.Fatalf("tunnel id-1 missing: %v", err)
	}

	if err := m.Reconcile(context.Background()); err != nil {
		t.Fatalf("second Reconcile returned error: %v", err)
	}

	after, err := m.Get("id-1")
	if err != nil {
		t.Fatalf("tunnel id-1 missing after second reconcile: %v", err)
	}
	if before != after {
		t.Fatal("an unchanged tunnel must not be rebuilt — that would bounce a live connection")
	}
}

func TestReconcileIsIdempotent(t *testing.T) {
	store := newFakeStorage(
		remoteSpec("id-1", "prod-db", 5432),
		remoteSpec("id-2", "staging-api", 8081),
	)
	m := managerWith(store)

	for round := 1; round <= 3; round++ {
		if err := m.Reconcile(context.Background()); err != nil {
			t.Fatalf("round %d: Reconcile returned error: %v", round, err)
		}
		if got := len(m.List()); got != 2 {
			t.Fatalf("round %d: got %d tunnels, want 2", round, got)
		}
	}
}

func TestReconcileRequiresStorage(t *testing.T) {
	m := NewManager(context.Background())
	if err := m.Reconcile(context.Background()); err == nil {
		t.Fatal("expected an error when no storage is configured, got nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tunnel/ -run TestReconcile -v`
Expected: FAIL — `m.Reconcile undefined (type *Manager has no field or method Reconcile)`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/tunnel/reconcile.go`:

```go
package tunnel

import (
	"context"
	"fmt"
	"reflect"

	"github.com/craigderington/lazytunnel/pkg/types"
)

// Reconcile converges the running fleet onto whatever is currently in storage.
//
// It exists because none of the obvious alternatives work on a live process:
// Manager.Update refuses to modify a running tunnel, Manager.Create always
// connects regardless of desired status, and LoadFromStorage overwrites
// m.tunnels entries outright, orphaning any live SSH goroutine.
//
// The delta is computed under RLock and the lock released before acting,
// because Start, Stop, dropTunnel and upsertTunnel all acquire m.mu themselves.
func (m *Manager) Reconcile(ctx context.Context) error {
	m.mu.RLock()
	store := m.storage
	m.mu.RUnlock()

	if store == nil {
		return fmt.Errorf("no storage configured")
	}

	specs, err := store.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list tunnels from storage: %w", err)
	}

	stored := make(map[string]*types.TunnelSpec, len(specs))
	for _, spec := range specs {
		stored[spec.ID] = spec
	}

	var (
		remove []string
		upsert []*types.TunnelSpec
	)

	m.mu.RLock()
	for id, tun := range m.tunnels {
		if _, ok := stored[id]; !ok {
			remove = append(remove, id)
		} else if !reflect.DeepEqual(tun.Spec, stored[id]) {
			upsert = append(upsert, stored[id])
		}
	}
	for id, spec := range stored {
		if _, ok := m.tunnels[id]; !ok {
			upsert = append(upsert, spec)
		}
	}
	m.mu.RUnlock()

	for _, id := range remove {
		m.dropTunnel(id)
	}
	for _, spec := range upsert {
		m.upsertTunnel(ctx, spec)
	}

	m.applyDesired(ctx)

	return nil
}

// dropTunnel stops a tunnel and unregisters it without touching storage.
//
// Manager.Delete cannot be used here: it also deletes the storage row, which
// has already been removed by the time Reconcile runs, so SQLiteStore.Delete
// would fail on zero rows affected.
func (m *Manager) dropTunnel(id string) {
	m.mu.Lock()
	tun, ok := m.tunnels[id]
	if ok {
		delete(m.tunnels, id)
	}
	breaker := m.circuitBreaker
	m.mu.Unlock()

	if !ok {
		return
	}

	// A tunnel that never connected returns an error here; it is not fatal.
	_ = tun.Stop()

	if breaker != nil {
		breaker.RemoveBreaker(id)
	}
}

// upsertTunnel installs a spec into the manager, stopping any live connection
// first so the swap cannot orphan a running SSH goroutine. The tunnel is
// registered stopped; applyDesired starts it if it should be running.
func (m *Manager) upsertTunnel(ctx context.Context, spec *types.TunnelSpec) {
	m.mu.RLock()
	existing, ok := m.tunnels[spec.ID]
	m.mu.RUnlock()

	if ok {
		_ = existing.Stop()
	}

	m.mu.Lock()
	m.tunnels[spec.ID] = &Tunnel{
		Spec:           spec,
		CreatedAt:      spec.CreatedAt,
		ctx:            ctx,
		statusCallback: m.statusCallback,
		Status: &types.TunnelStatus{
			TunnelID:  spec.ID,
			State:     types.TunnelStateStopped,
			LastError: "",
		},
	}
	m.mu.Unlock()
}

// applyDesired starts tunnels that should be running and stops those that
// should not, for tunnels that run on this node.
//
// Tunnel pointers are collected under RLock and inspected after releasing it,
// so m.mu and Tunnel.mu are never held at the same time.
func (m *Manager) applyDesired(ctx context.Context) {
	m.mu.RLock()
	nodeID := m.nodeAgentID
	tunnels := make([]*Tunnel, 0, len(m.tunnels))
	for _, tun := range m.tunnels {
		tunnels = append(tunnels, tun)
	}
	m.mu.RUnlock()

	for _, tun := range tunnels {
		if !RunOnThisNode(nodeID, tun.Spec.AgentID) {
			continue
		}

		state := types.TunnelStateStopped
		if status := tun.GetStatus(); status != nil {
			state = status.State
		}

		isUp := state == types.TunnelStateActive || state == types.TunnelStatePending
		wantUp := tun.Spec.DesiredStatus == types.DesiredStatusActive

		switch {
		case wantUp && !isUp:
			_ = m.Start(ctx, tun.Spec.ID)
		case !wantUp && isUp:
			_ = m.Stop(ctx, tun.Spec.ID)
		}
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tunnel/ -run TestReconcile -v && go test ./... 2>&1 | tail -20`
Expected: PASS — 6 reconcile tests, and no regressions elsewhere.

- [ ] **Step 5: Commit**

```bash
git add internal/tunnel/reconcile.go internal/tunnel/reconcile_test.go
git commit -m "feat(tunnel): Reconcile converges the running fleet onto stored state"
```

---

### Task 7: API endpoints

**Files:**
- Create: `internal/api/backup_handlers.go`
- Create: `internal/api/backup_handlers_test.go`
- Modify: `internal/api/server.go` (add `Version` to `Config` and `Server`, register two routes)
- Modify: `cmd/server/main.go:88-94` (pass `Version: version`)

**Interfaces:**
- Consumes: `backup.Export`, `backup.Plan`, `backup.Apply`, `backup.NewDryRunReport`, `backup.ParseMode`, `backup.ArchiveInvalidError`, `backup.Archive` (Tasks 1-5); `Manager.Reconcile` (Task 6); existing `s.respondJSON`, `s.BadRequest`, `s.InternalError`, `s.ServiceUnavailableError`, `GetUser`.
- Produces: `(*Server).handleExportConfig`, `(*Server).handleImportConfig`; routes `GET /api/v1/config/export` and `POST /api/v1/config/import`.

**Note:** `tunnel.Storage` declares `List`, `Save` and `Delete` among its methods, so its method set is a superset of `backup.Store` and `s.storage` assigns to a `backup.Store` variable directly — no type assertion needed.

- [ ] **Step 1: Write the failing test**

Create `internal/api/backup_handlers_test.go`:

```go
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/craigderington/lazytunnel/internal/backup"
	"github.com/craigderington/lazytunnel/pkg/types"
)

// backupTestStorage implements tunnel.Storage in memory.
type backupTestStorage struct {
	specs map[string]*types.TunnelSpec
	order []string
}

func newBackupTestStorage(specs ...*types.TunnelSpec) *backupTestStorage {
	s := &backupTestStorage{specs: make(map[string]*types.TunnelSpec, len(specs))}
	for _, spec := range specs {
		s.order = append(s.order, spec.ID)
		s.specs[spec.ID] = spec
	}
	return s
}

func (s *backupTestStorage) Save(ctx context.Context, spec *types.TunnelSpec) error {
	if _, exists := s.specs[spec.ID]; !exists {
		s.order = append(s.order, spec.ID)
	}
	s.specs[spec.ID] = spec
	return nil
}

func (s *backupTestStorage) Update(ctx context.Context, spec *types.TunnelSpec) error {
	return s.Save(ctx, spec)
}

func (s *backupTestStorage) UpdateStatus(ctx context.Context, tunnelID, status string) error {
	return nil
}

func (s *backupTestStorage) UpdateDesiredStatus(ctx context.Context, tunnelID string, status types.DesiredStatus) error {
	return nil
}

func (s *backupTestStorage) Delete(ctx context.Context, tunnelID string) error {
	delete(s.specs, tunnelID)
	for i, id := range s.order {
		if id == tunnelID {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
	return nil
}

func (s *backupTestStorage) Get(ctx context.Context, tunnelID string) (*types.TunnelSpec, error) {
	if spec, ok := s.specs[tunnelID]; ok {
		return spec, nil
	}
	return nil, context.Canceled
}

func (s *backupTestStorage) List(ctx context.Context) ([]*types.TunnelSpec, error) {
	out := make([]*types.TunnelSpec, 0, len(s.order))
	for _, id := range s.order {
		if spec, ok := s.specs[id]; ok {
			out = append(out, spec)
		}
	}
	return out, nil
}

func (s *backupTestStorage) ListByAgent(ctx context.Context, agentID string) ([]*types.TunnelSpec, error) {
	return s.List(ctx)
}

func (s *backupTestStorage) Close() error { return nil }

func backupTestSpec(id, name string, port int) *types.TunnelSpec {
	return &types.TunnelSpec{
		ID:            id,
		Name:          name,
		Owner:         "admin",
		AgentID:       "remote-agent", // never connects on this node
		DesiredStatus: types.DesiredStatusStopped,
		Type:          types.TunnelTypeLocal,
		Hops:          []types.Hop{{Host: "bastion", Port: 22, User: "deploy", AuthMethod: types.AuthMethodKey}},
		LocalPort:     port,
		RemoteHost:    "db.internal",
		RemotePort:    5432,
		KeepAlive:     30 * time.Second,
		MaxRetries:    5,
	}
}

func newBackupTestServer(t *testing.T, store *backupTestStorage) *Server {
	t.Helper()
	return NewServer(context.Background(), Config{
		Addr:    ":0",
		Logger:  zerolog.Nop(),
		Storage: store,
		Version: "test",
	})
}

func TestHandleExportConfigReturnsArchive(t *testing.T) {
	srv := newBackupTestServer(t, newBackupTestStorage(backupTestSpec("id-1", "prod-db", 5432)))

	rec := httptest.NewRecorder()
	srv.handleExportConfig(rec, httptest.NewRequest(http.MethodGet, "/api/v1/config/export", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if cd := rec.Header().Get("Content-Disposition"); cd == "" {
		t.Error("expected a Content-Disposition header so the UI can download by link")
	}

	var archive backup.Archive
	if err := json.Unmarshal(rec.Body.Bytes(), &archive); err != nil {
		t.Fatalf("response is not a valid archive: %v", err)
	}
	if archive.Version != backup.SchemaVersion {
		t.Errorf("got version %d, want %d", archive.Version, backup.SchemaVersion)
	}
	if len(archive.Tunnels) != 1 || archive.Tunnels[0].Name != "prod-db" {
		t.Fatalf("unexpected tunnels in archive: %+v", archive.Tunnels)
	}
}

func postImport(t *testing.T, srv *Server, query string, archive backup.Archive) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(archive)
	if err != nil {
		t.Fatalf("failed to marshal archive: %v", err)
	}
	rec := httptest.NewRecorder()
	srv.handleImportConfig(rec, httptest.NewRequest(http.MethodPost, "/api/v1/config/import"+query, bytes.NewReader(body)))
	return rec
}

func decodeReport(t *testing.T, rec *httptest.ResponseRecorder) backup.Report {
	t.Helper()
	var report backup.Report
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatalf("response is not a report: %v (body: %s)", err, rec.Body.String())
	}
	return report
}

func TestHandleImportConfigMergesByDefault(t *testing.T) {
	store := newBackupTestStorage(backupTestSpec("id-1", "prod-db", 5432))
	srv := newBackupTestServer(t, store)

	archive := backup.Archive{
		Version: backup.SchemaVersion,
		Tunnels: []backup.TunnelEntry{backup.EntryFromSpec(backupTestSpec("", "staging-api", 8081))},
	}

	rec := postImport(t, srv, "", archive)
	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200: %s", rec.Code, rec.Body.String())
	}

	report := decodeReport(t, rec)
	if report.Created != 1 {
		t.Errorf("got Created %d, want 1", report.Created)
	}
	if report.Deleted != 0 {
		t.Errorf("got Deleted %d, want 0 — merge must never delete", report.Deleted)
	}
	if len(store.specs) != 2 {
		t.Errorf("got %d stored tunnels, want 2", len(store.specs))
	}
}

func TestHandleImportConfigReplaceDeletesAbsentTunnels(t *testing.T) {
	store := newBackupTestStorage(backupTestSpec("id-1", "prod-db", 5432))
	srv := newBackupTestServer(t, store)

	archive := backup.Archive{
		Version: backup.SchemaVersion,
		Tunnels: []backup.TunnelEntry{backup.EntryFromSpec(backupTestSpec("", "staging-api", 8081))},
	}

	rec := postImport(t, srv, "?mode=replace", archive)
	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200: %s", rec.Code, rec.Body.String())
	}

	report := decodeReport(t, rec)
	if report.Deleted != 1 {
		t.Errorf("got Deleted %d, want 1", report.Deleted)
	}
	if _, exists := store.specs["id-1"]; exists {
		t.Error("prod-db should have been deleted in replace mode")
	}
}

func TestHandleImportConfigDryRunWritesNothing(t *testing.T) {
	store := newBackupTestStorage(backupTestSpec("id-1", "prod-db", 5432))
	srv := newBackupTestServer(t, store)

	archive := backup.Archive{
		Version: backup.SchemaVersion,
		Tunnels: []backup.TunnelEntry{backup.EntryFromSpec(backupTestSpec("", "staging-api", 8081))},
	}

	rec := postImport(t, srv, "?dry_run=true", archive)
	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200: %s", rec.Code, rec.Body.String())
	}

	report := decodeReport(t, rec)
	if !report.DryRun {
		t.Error("report should be flagged as a dry run")
	}
	if report.Created != 1 {
		t.Errorf("got Created %d, want 1 (as an intention)", report.Created)
	}
	if len(store.specs) != 1 {
		t.Errorf("got %d stored tunnels, want 1 — a dry run must not write", len(store.specs))
	}
}

func TestHandleImportConfigRejectsBadVersion(t *testing.T) {
	srv := newBackupTestServer(t, newBackupTestStorage())

	archive := backup.Archive{
		Version: 99,
		Tunnels: []backup.TunnelEntry{backup.EntryFromSpec(backupTestSpec("", "staging-api", 8081))},
	}

	rec := postImport(t, srv, "", archive)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got status %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleImportConfigRejectsUnknownMode(t *testing.T) {
	srv := newBackupTestServer(t, newBackupTestStorage())
	archive := backup.Archive{Version: backup.SchemaVersion}

	rec := postImport(t, srv, "?mode=obliterate", archive)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got status %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleImportConfigReturnsValidationDetails(t *testing.T) {
	srv := newBackupTestServer(t, newBackupTestStorage())

	archive := backup.Archive{
		Version: backup.SchemaVersion,
		Tunnels: []backup.TunnelEntry{{Name: "", Type: "sideways"}},
	}

	rec := postImport(t, srv, "", archive)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got status %d, want 400", rec.Code)
	}

	var body struct {
		Details []backup.EntryError `json:"details"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(body.Details) < 2 {
		t.Fatalf("got %d details, want every problem reported at once", len(body.Details))
	}
}

func TestExportImportRoundTripIsAllSkip(t *testing.T) {
	store := newBackupTestStorage(backupTestSpec("id-1", "prod-db", 5432))
	srv := newBackupTestServer(t, store)

	rec := httptest.NewRecorder()
	srv.handleExportConfig(rec, httptest.NewRequest(http.MethodGet, "/api/v1/config/export", nil))

	var archive backup.Archive
	if err := json.Unmarshal(rec.Body.Bytes(), &archive); err != nil {
		t.Fatalf("export is not a valid archive: %v", err)
	}

	imported := postImport(t, srv, "?mode=replace", archive)
	if imported.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200: %s", imported.Code, imported.Body.String())
	}

	report := decodeReport(t, imported)
	if report.Skipped != 1 || report.Created != 0 || report.Updated != 0 || report.Deleted != 0 {
		t.Fatalf("re-importing an unmodified export should change nothing, got %+v", report)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run 'TestHandleExport|TestHandleImport|TestExportImport' -v`
Expected: FAIL — `unknown field Version in struct literal`, `srv.handleExportConfig undefined`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/api/backup_handlers.go`:

```go
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/craigderington/lazytunnel/internal/backup"
)

// maxImportBytes caps the archive an import will read, so a malformed or
// hostile upload cannot exhaust memory.
const maxImportBytes = 10 << 20 // 10 MiB

// handleExportConfig returns every stored tunnel as a versioned archive.
func (s *Server) handleExportConfig(w http.ResponseWriter, r *http.Request) {
	if s.storage == nil {
		s.ServiceUnavailableError(w, "Persistent storage is not configured")
		return
	}

	archive, err := backup.Export(r.Context(), s.storage, "lazytunnel/"+s.version, time.Now)
	if err != nil {
		s.logger.Error().Err(err).Msg("Failed to export configuration")
		s.InternalError(w, "Failed to export configuration")
		return
	}

	filename := fmt.Sprintf("lazytunnel-backup-%s.json", archive.ExportedAt.Format("20060102-150405"))

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.WriteHeader(http.StatusOK)

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(archive); err != nil {
		s.logger.Error().Err(err).Msg("Failed to encode export")
	}
}

// handleImportConfig restores tunnel definitions from an archive.
//
// ?mode=merge (default) updates and creates; ?mode=replace additionally
// deletes tunnels absent from the archive. ?dry_run=true returns the intended
// plan without writing anything.
func (s *Server) handleImportConfig(w http.ResponseWriter, r *http.Request) {
	if s.storage == nil {
		s.ServiceUnavailableError(w, "Persistent storage is not configured")
		return
	}

	mode, err := backup.ParseMode(r.URL.Query().Get("mode"))
	if err != nil {
		s.BadRequest(w, err.Error())
		return
	}
	dryRun := r.URL.Query().Get("dry_run") == "true"

	var archive backup.Archive
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxImportBytes)).Decode(&archive); err != nil {
		s.BadRequest(w, "Invalid archive JSON: "+err.Error())
		return
	}

	owner := "api-user"
	if user, ok := GetUser(r.Context()); ok {
		owner = user.Username
	}

	current, err := s.storage.List(r.Context())
	if err != nil {
		s.logger.Error().Err(err).Msg("Failed to list tunnels for import")
		s.InternalError(w, "Failed to read existing tunnels")
		return
	}

	plan, err := backup.Plan(current, &archive, backup.PlanOptions{Mode: mode, DefaultOwner: owner})
	if err != nil {
		var invalid backup.ArchiveInvalidError
		if errors.As(err, &invalid) {
			s.respondJSON(w, http.StatusBadRequest, map[string]interface{}{
				"error":   "archive validation failed",
				"details": invalid.Errors,
			})
			return
		}
		s.BadRequest(w, err.Error())
		return
	}

	if dryRun {
		s.respondJSON(w, http.StatusOK, backup.NewDryRunReport(plan))
		return
	}

	// Background context: the writes and the reconnects that follow must
	// outlive the HTTP request, matching handleCreateTunnel.
	report, applyErr := backup.Apply(context.Background(), s.storage, plan)

	// Converge the running fleet onto the newly stored desired state.
	if err := s.manager.Reconcile(context.Background()); err != nil {
		s.logger.Error().Err(err).Msg("Failed to reconcile tunnels after import")
	}

	if applyErr != nil {
		s.logger.Error().Err(applyErr).Msg("Import partially failed")
		s.respondJSON(w, http.StatusInternalServerError, report)
		return
	}

	s.logger.Info().
		Str("mode", string(mode)).
		Int("created", report.Created).
		Int("updated", report.Updated).
		Int("deleted", report.Deleted).
		Int("skipped", report.Skipped).
		Msg("Configuration imported")

	s.respondJSON(w, http.StatusOK, report)
}
```

- [ ] **Step 4: Wire the Version field and routes**

In `internal/api/server.go`, add `version string` to the `Server` struct, after the `coordinator` field:

```go
	coordinator *agent.Coordinator
	version     string
```

Add `Version` to `Config`, after the `WebSocket` field:

```go
	WebSocket   *WebSocketManager // Optional WebSocket manager
	Version     string            // Build version, stamped into config exports
```

In `NewServer`, immediately before `s := &Server{`, add:

```go
	version := config.Version
	if version == "" {
		version = "dev"
	}
```

and add `version: version,` to the `&Server{...}` literal, after `coordinator: coord,`.

In `setupRoutes`, after the `protected.HandleFunc("/logs", ...)` line, add:

```go
	// Configuration backup and restore (protected)
	protected.HandleFunc("/config/export", s.handleExportConfig).Methods("GET", "OPTIONS")
	protected.HandleFunc("/config/import", s.handleImportConfig).Methods("POST", "OPTIONS")
```

In `cmd/server/main.go`, change the `api.Config` literal at lines 88-94 to include the version:

```go
	server := api.NewServer(ctx, api.Config{
		Addr:    cfg.Server.Addr,
		Logger:  log.Logger,
		Storage: store,
		Auth:    auth,
		TLS:     tlsConfig,
		Version: version,
	})
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go build ./... && go test ./internal/api/ -run 'TestHandleExport|TestHandleImport|TestExportImport' -v && go test ./...`
Expected: PASS — 8 new API tests, everything else green.

- [ ] **Step 6: Commit**

```bash
git add internal/api/backup_handlers.go internal/api/backup_handlers_test.go internal/api/server.go cmd/server/main.go
git commit -m "feat(api): config export and import endpoints"
```

---

### Task 8: CLI export command

**Files:**
- Create: `internal/cli/export.go`
- Modify: `internal/cli/root.go` (register `exportCmd`)

**Interfaces:**
- Consumes: the `GET /api/v1/config/export` route from Task 7; `viper.GetString("server")` as in `internal/cli/list.go`.
- Produces: `exportCmd` (`tunnelctl export [-o FILE]`).

- [ ] **Step 1: Write the implementation**

Create `internal/cli/export.go`:

```go
package cli

import (
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var exportOutput string

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export all tunnel definitions to a backup archive",
	Long: `Export every tunnel definition on the server as a versioned JSON archive.

Writes to stdout by default, so it pipes and redirects:

  tunnelctl export > tunnels.json
  tunnelctl export -o tunnels.json

The archive contains hostnames, SSH usernames and key paths, but no key
material. When -o is used the file is created with 0600 permissions.`,
	RunE: runExport,
}

func init() {
	exportCmd.Flags().StringVarP(&exportOutput, "output", "o", "",
		"write the archive to FILE instead of stdout (created with 0600)")
}

func runExport(cmd *cobra.Command, args []string) error {
	url := fmt.Sprintf("%s/api/v1/config/export", viper.GetString("server"))

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to export config: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read export response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to export config: %s", string(body))
	}

	if exportOutput == "" {
		_, err = cmd.OutOrStdout().Write(body)
		return err
	}

	if err := os.WriteFile(exportOutput, body, 0o600); err != nil {
		return fmt.Errorf("failed to write %s: %w", exportOutput, err)
	}

	fmt.Fprintf(cmd.ErrOrStderr(), "Wrote %s (%d bytes)\n", exportOutput, len(body))
	return nil
}
```

In `internal/cli/root.go`, add to the subcommand block in `init()`, after `rootCmd.AddCommand(stopCmd)`:

```go
	rootCmd.AddCommand(exportCmd)
```

- [ ] **Step 2: Verify it builds and registers**

Run: `go build -o /tmp/tunnelctl cmd/tunnelctl/main.go && /tmp/tunnelctl export --help`
Expected: the help text above, listing the `-o, --output` flag.

- [ ] **Step 3: Verify against a running server**

Run:
```bash
go run cmd/server/main.go --config config.example.yaml &
sleep 2
/tmp/tunnelctl export | head -5
kill %1
```
Expected: a JSON archive beginning `{`, `"version": 1,`.

- [ ] **Step 4: Commit**

```bash
git add internal/cli/export.go internal/cli/root.go
git commit -m "feat(cli): tunnelctl export"
```

---

### Task 9: CLI import command

**Files:**
- Create: `internal/cli/import.go`
- Test: `internal/cli/import_test.go`
- Modify: `internal/cli/root.go` (register `importCmd`)

**Interfaces:**
- Consumes: the `POST /api/v1/config/import` route from Task 7.
- Produces: `importCmd` (`tunnelctl import FILE [--replace] [--dry-run] [--yes]`); helpers `parseImportReport([]byte) (*importReport, error)`, `formatImportReport(*importReport) string`, `countDeletions(*importReport) int`.

The three helpers are pure so they can be tested without a server, matching the `parseTunnelList` pattern in `internal/cli/list_test.go`.

- [ ] **Step 1: Write the failing test**

Create `internal/cli/import_test.go`:

```go
package cli

import (
	"strings"
	"testing"
)

const sampleReportJSON = `{
	"mode": "replace",
	"dry_run": true,
	"items": [
		{"action":"update","name":"prod-db","id":"id-1"},
		{"action":"create","name":"staging-api","id":"id-2"},
		{"action":"skip","name":"socks-jump","id":"id-3","reason":"identical to stored tunnel"},
		{"action":"delete","name":"old-bastion","id":"id-4","reason":"not present in archive"}
	],
	"created": 1,
	"updated": 1,
	"skipped": 1,
	"deleted": 1,
	"failed": 0
}`

func TestParseImportReport(t *testing.T) {
	report, err := parseImportReport([]byte(sampleReportJSON))
	if err != nil {
		t.Fatalf("parseImportReport returned error: %v", err)
	}
	if report.Mode != "replace" {
		t.Errorf("got Mode %q, want replace", report.Mode)
	}
	if !report.DryRun {
		t.Error("got DryRun false, want true")
	}
	if len(report.Items) != 4 {
		t.Fatalf("got %d items, want 4", len(report.Items))
	}
	if report.Items[0].Action != "update" || report.Items[0].Name != "prod-db" {
		t.Errorf("unexpected first item: %+v", report.Items[0])
	}
}

func TestParseImportReportRejectsMalformed(t *testing.T) {
	if _, err := parseImportReport([]byte(`not json`)); err == nil {
		t.Fatal("expected an error for malformed JSON, got nil")
	}
}

func TestCountDeletions(t *testing.T) {
	report, err := parseImportReport([]byte(sampleReportJSON))
	if err != nil {
		t.Fatalf("parseImportReport returned error: %v", err)
	}
	if got := countDeletions(report); got != 1 {
		t.Fatalf("got %d deletions, want 1", got)
	}
}

func TestCountDeletionsIsZeroForMerge(t *testing.T) {
	report, err := parseImportReport([]byte(`{"mode":"merge","items":[{"action":"create","name":"a"}]}`))
	if err != nil {
		t.Fatalf("parseImportReport returned error: %v", err)
	}
	if got := countDeletions(report); got != 0 {
		t.Fatalf("got %d deletions, want 0", got)
	}
}

func TestFormatImportReportListsEveryItem(t *testing.T) {
	report, err := parseImportReport([]byte(sampleReportJSON))
	if err != nil {
		t.Fatalf("parseImportReport returned error: %v", err)
	}

	out := formatImportReport(report)
	for _, want := range []string{"prod-db", "staging-api", "socks-jump", "old-bastion", "update", "create", "skip", "DELETE"} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing %q:\n%s", want, out)
		}
	}
}

func TestFormatImportReportShowsSummaryCounts(t *testing.T) {
	report, err := parseImportReport([]byte(sampleReportJSON))
	if err != nil {
		t.Fatalf("parseImportReport returned error: %v", err)
	}

	out := formatImportReport(report)
	for _, want := range []string{"1 created", "1 updated", "1 deleted"} {
		if !strings.Contains(out, want) {
			t.Errorf("summary is missing %q:\n%s", want, out)
		}
	}
}

func TestFormatImportReportSurfacesErrors(t *testing.T) {
	report, err := parseImportReport([]byte(`{
		"mode":"merge",
		"items":[{"action":"create","name":"broken","error":"disk on fire"}],
		"failed":1
	}`))
	if err != nil {
		t.Fatalf("parseImportReport returned error: %v", err)
	}

	out := formatImportReport(report)
	if !strings.Contains(out, "disk on fire") {
		t.Errorf("output must surface the failure message:\n%s", out)
	}
	if !strings.Contains(out, "1 failed") {
		t.Errorf("summary must report failures:\n%s", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run 'TestParseImportReport|TestCountDeletions|TestFormatImportReport' -v`
Expected: FAIL — `undefined: parseImportReport`, `undefined: countDeletions`, `undefined: formatImportReport`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/cli/import.go`:

```go
package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	importReplace bool
	importDryRun  bool
	importYes     bool
)

var importCmd = &cobra.Command{
	Use:   "import FILE",
	Short: "Restore tunnel definitions from a backup archive",
	Long: `Restore tunnel definitions from an archive produced by 'tunnelctl export'.

By default the import merges: tunnels matching by name are updated in place,
missing ones are created, and anything not mentioned in the archive is left
alone. Merging never deletes.

With --replace, tunnels absent from the archive are deleted so the server
mirrors the file exactly. Deletions are always previewed and confirmed unless
--yes is given.

  tunnelctl import tunnels.json
  tunnelctl import --dry-run tunnels.json
  tunnelctl import --replace tunnels.json`,
	Args: cobra.ExactArgs(1),
	RunE: runImport,
}

func init() {
	importCmd.Flags().BoolVar(&importReplace, "replace", false,
		"delete tunnels that are not present in the archive")
	importCmd.Flags().BoolVar(&importDryRun, "dry-run", false,
		"show what would change without writing anything")
	importCmd.Flags().BoolVarP(&importYes, "yes", "y", false,
		"skip the confirmation prompt for deletions")
}

// importItem mirrors one entry of the server's import report.
type importItem struct {
	Action string `json:"action"`
	Name   string `json:"name"`
	ID     string `json:"id"`
	Reason string `json:"reason"`
	Error  string `json:"error"`
}

// importReport mirrors the JSON returned by POST /api/v1/config/import.
type importReport struct {
	Mode    string       `json:"mode"`
	DryRun  bool         `json:"dry_run"`
	Items   []importItem `json:"items"`
	Created int          `json:"created"`
	Updated int          `json:"updated"`
	Skipped int          `json:"skipped"`
	Deleted int          `json:"deleted"`
	Failed  int          `json:"failed"`
}

func parseImportReport(body []byte) (*importReport, error) {
	var report importReport
	if err := json.Unmarshal(body, &report); err != nil {
		return nil, fmt.Errorf("failed to parse import report: %w", err)
	}
	return &report, nil
}

func countDeletions(report *importReport) int {
	n := 0
	for _, item := range report.Items {
		if item.Action == "delete" {
			n++
		}
	}
	return n
}

// formatImportReport renders a report as an aligned table plus a summary line.
// Deletions are uppercased so they stand out before a confirmation prompt.
func formatImportReport(report *importReport) string {
	var buf bytes.Buffer
	w := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)

	for _, item := range report.Items {
		action := item.Action
		if action == "delete" {
			action = "DELETE"
		}

		note := item.Reason
		if item.Error != "" {
			note = "ERROR: " + item.Error
		}

		fmt.Fprintf(w, "  %s\t%s\t%s\n", action, item.Name, note)
	}
	w.Flush()

	parts := []string{
		fmt.Sprintf("%d created", report.Created),
		fmt.Sprintf("%d updated", report.Updated),
		fmt.Sprintf("%d skipped", report.Skipped),
		fmt.Sprintf("%d deleted", report.Deleted),
	}
	if report.Failed > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", report.Failed))
	}

	fmt.Fprintf(&buf, "\n%s\n", strings.Join(parts, ", "))
	return buf.String()
}

func postImport(archive []byte, mode string, dryRun bool) (*importReport, error) {
	url := fmt.Sprintf("%s/api/v1/config/import?mode=%s", viper.GetString("server"), mode)
	if dryRun {
		url += "&dry_run=true"
	}

	resp, err := http.Post(url, "application/json", bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("failed to import config: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read import response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("import failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	return parseImportReport(body)
}

func runImport(cmd *cobra.Command, args []string) error {
	archive, err := os.ReadFile(args[0])
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", args[0], err)
	}

	mode := "merge"
	if importReplace {
		mode = "replace"
	}

	// Always preview first: the plan is what gets printed, and with --replace
	// it is what the confirmation prompt is based on.
	preview, err := postImport(archive, mode, true)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	fmt.Fprint(out, formatImportReport(preview))

	if importDryRun {
		fmt.Fprintln(out, "Dry run: nothing was written.")
		return nil
	}

	if deletions := countDeletions(preview); deletions > 0 && !importYes {
		fmt.Fprintf(out, "\n--replace deletes %d tunnel(s). Continue? [y/N] ", deletions)

		answer, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
		if err != nil && err != io.EOF {
			return fmt.Errorf("failed to read confirmation: %w", err)
		}
		if strings.ToLower(strings.TrimSpace(answer)) != "y" {
			fmt.Fprintln(out, "Aborted.")
			return nil
		}
	}

	report, err := postImport(archive, mode, false)
	if err != nil {
		return err
	}

	fmt.Fprint(out, formatImportReport(report))
	return nil
}
```

In `internal/cli/root.go`, add to `init()` after `rootCmd.AddCommand(exportCmd)`:

```go
	rootCmd.AddCommand(importCmd)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/ -v && go build ./...`
Expected: PASS — 7 new tests plus the 3 existing `parseTunnelList` tests.

- [ ] **Step 5: Verify the round trip against a running server**

Run:
```bash
go build -o /tmp/tunnelctl cmd/tunnelctl/main.go
go run cmd/server/main.go --config config.example.yaml &
sleep 2
/tmp/tunnelctl export -o /tmp/backup.json
/tmp/tunnelctl import --dry-run /tmp/backup.json
kill %1
```
Expected: every item reported as `skip`, and `Dry run: nothing was written.`

- [ ] **Step 6: Commit**

```bash
git add internal/cli/import.go internal/cli/import_test.go internal/cli/root.go
git commit -m "feat(cli): tunnelctl import with preview and delete confirmation"
```

---

### Task 10: Web UI backup section

**Files:**
- Modify: `web/src/api/types.ts` (add `ImportReport`, `ImportItem`)
- Modify: `web/src/api/client.ts` (add `exportConfig`, `importConfig`)
- Create: `web/src/components/BackupSection.tsx`
- Modify: `web/src/components/Settings.tsx` (mount `BackupSection`)

**Interfaces:**
- Consumes: `GET /api/v1/config/export`, `POST /api/v1/config/import` from Task 7; the existing `LazytunnelClient.request` pattern; `Button`, `Switch`, `Label` from `web/src/components/ui/`.
- Produces: `client.exportConfig(): Promise<Blob>`, `client.importConfig(archive: unknown, opts): Promise<ImportReport>`, and the `<BackupSection />` component.

- [ ] **Step 1: Add the report types**

Append to `web/src/api/types.ts`:

```ts
export interface ImportItem {
  action: 'create' | 'update' | 'skip' | 'delete'
  name: string
  id: string
  reason?: string
  error?: string
}

export interface ImportReport {
  mode: 'merge' | 'replace'
  dry_run: boolean
  items: ImportItem[]
  created: number
  updated: number
  skipped: number
  deleted: number
  failed: number
}
```

- [ ] **Step 2: Add the client methods**

In `web/src/api/client.ts`, add `ImportReport` to the type import list from `@/api/types`, then add these two methods to `LazytunnelClient` after `stopTunnel`:

```ts
  async exportConfig(): Promise<Blob> {
    const token = await getAuthToken()
    const headers: Record<string, string> = {}
    if (token) {
      headers.Authorization = `Bearer ${token}`
    }

    const response = await fetch(apiUrl('/config/export'), { headers })
    if (!response.ok) {
      throw await parseError(response)
    }
    return response.blob()
  }

  importConfig(
    archive: unknown,
    opts: { replace?: boolean; dryRun?: boolean } = {}
  ): Promise<ImportReport> {
    const params = new URLSearchParams({ mode: opts.replace ? 'replace' : 'merge' })
    if (opts.dryRun) {
      params.set('dry_run', 'true')
    }
    return this.request<ImportReport>(`/config/import?${params}`, {
      method: 'POST',
      body: JSON.stringify(archive),
    })
  }
```

`exportConfig` bypasses `request` because it needs the raw body as a `Blob` for the download, not parsed JSON.

- [ ] **Step 3: Create the component**

Create `web/src/components/BackupSection.tsx`:

```tsx
import { useRef, useState } from 'react'
import { api } from '@/api/client'
import type { ImportReport } from '@/api/types'
import { Button } from './ui/button'
import { Label } from './ui/label'
import { Switch } from './ui/switch'

const ACTION_STYLES: Record<string, string> = {
  create: 'text-[hsl(var(--live))]',
  update: 'text-foreground',
  skip: 'text-muted-foreground',
  delete: 'text-destructive',
}

export function BackupSection() {
  const fileInput = useRef<HTMLInputElement>(null)
  const [replace, setReplace] = useState(false)
  const [preview, setPreview] = useState<ImportReport | null>(null)
  const [archive, setArchive] = useState<unknown>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const reset = () => {
    setPreview(null)
    setArchive(null)
    setError(null)
    if (fileInput.current) {
      fileInput.current.value = ''
    }
  }

  const handleDownload = async () => {
    setError(null)
    try {
      const blob = await api.exportConfig()
      const url = URL.createObjectURL(blob)
      const link = document.createElement('a')
      link.href = url
      link.download = `lazytunnel-backup-${new Date().toISOString().slice(0, 10)}.json`
      link.click()
      URL.revokeObjectURL(url)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Export failed')
    }
  }

  const handleFile = async (file: File) => {
    setBusy(true)
    setError(null)
    try {
      const parsed = JSON.parse(await file.text())
      setArchive(parsed)
      setPreview(await api.importConfig(parsed, { replace, dryRun: true }))
    } catch (err) {
      setArchive(null)
      setPreview(null)
      setError(err instanceof Error ? err.message : 'Could not read that file')
    } finally {
      setBusy(false)
    }
  }

  const handleApply = async () => {
    if (!archive) return
    setBusy(true)
    setError(null)
    try {
      setPreview(await api.importConfig(archive, { replace }))
      setArchive(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Import failed')
    } finally {
      setBusy(false)
    }
  }

  return (
    <section className="mb-10 space-y-4 border-t border-border pt-8">
      <p className="text-xs uppercase tracking-wider text-muted-foreground">Backup</p>

      <div className="flex items-center justify-between">
        <div>
          <p className="text-sm">Download backup</p>
          <p className="text-xs text-muted-foreground">
            All tunnel definitions as JSON. Contains hostnames, usernames and key paths —
            no key material.
          </p>
        </div>
        <Button variant="outline" size="sm" onClick={handleDownload}>
          Download
        </Button>
      </div>

      <div className="flex items-center justify-between">
        <div>
          <p className="text-sm">Replace mode</p>
          <p className="text-xs text-muted-foreground">
            {replace
              ? 'Deletes tunnels missing from the backup'
              : 'Merge: updates and adds only, never deletes'}
          </p>
        </div>
        <Switch
          checked={replace}
          onCheckedChange={(value) => {
            setReplace(value)
            reset()
          }}
        />
      </div>

      <div className="space-y-2">
        <Label className="text-xs text-muted-foreground">Restore from file</Label>
        <input
          ref={fileInput}
          type="file"
          accept="application/json,.json"
          disabled={busy}
          onChange={(e) => {
            const file = e.target.files?.[0]
            if (file) void handleFile(file)
          }}
          className="block w-full text-sm text-muted-foreground file:mr-3 file:rounded-md file:border file:border-border file:bg-transparent file:px-3 file:py-1.5 file:text-sm file:text-foreground"
        />
      </div>

      {error && <p className="text-sm text-destructive">{error}</p>}

      {preview && (
        <div className="space-y-3 border-t border-border pt-4">
          <p className="text-xs uppercase tracking-wider text-muted-foreground">
            {preview.dry_run ? 'Preview' : 'Applied'}
          </p>

          <ul className="divide-y divide-border border-t border-border font-mono text-sm">
            {preview.items.map((item) => (
              <li key={item.id || item.name} className="flex justify-between gap-4 py-2">
                <span className={ACTION_STYLES[item.action] ?? 'text-foreground'}>
                  {item.action === 'delete' ? 'DELETE' : item.action}
                </span>
                <span className="flex-1 truncate">{item.name}</span>
                {item.error && <span className="text-destructive">{item.error}</span>}
              </li>
            ))}
          </ul>

          <p className="text-xs text-muted-foreground">
            {preview.created} created, {preview.updated} updated, {preview.skipped} skipped,{' '}
            {preview.deleted} deleted
            {preview.failed > 0 && `, ${preview.failed} failed`}
          </p>

          {preview.dry_run && (
            <div className="flex gap-2">
              <Button size="sm" disabled={busy} onClick={handleApply}>
                {preview.deleted > 0
                  ? `Apply and delete ${preview.deleted}`
                  : 'Apply'}
              </Button>
              <Button variant="ghost" size="sm" disabled={busy} onClick={reset}>
                Cancel
              </Button>
            </div>
          )}
        </div>
      )}
    </section>
  )
}
```

- [ ] **Step 4: Mount it in Settings**

In `web/src/components/Settings.tsx`, add the import after the `PageHeader` import:

```tsx
import { BackupSection } from './BackupSection'
```

and place `<BackupSection />` immediately before the final `</>`, after the closing `</section>` of the preferences block.

- [ ] **Step 5: Verify the build and existing tests**

Run: `cd web && npm run build && npm run test:run`
Expected: a clean `tsc -b` and Vite build; the existing `tunnelOrder` tests still pass.

- [ ] **Step 6: Verify in the browser**

Run:
```bash
go run cmd/server/main.go --config config.example.yaml &
sleep 2
cd web && npm run dev
```
Open Settings. Confirm: Download produces a JSON file; re-uploading that same file previews every tunnel as `skip`; toggling Replace mode and re-selecting the file shows the delete count in the Apply button label.

- [ ] **Step 7: Commit**

```bash
git add web/src/api/types.ts web/src/api/client.ts web/src/components/BackupSection.tsx web/src/components/Settings.tsx
git commit -m "feat(web): backup download and restore with diff preview"
```

---

### Task 11: Documentation

**Files:**
- Modify: `docs/api-reference.md`
- Modify: `docs/cli-reference.md`

**Interfaces:**
- Consumes: the finished behaviour from Tasks 7-9. No code changes.

- [ ] **Step 1: Read the current structure of both files**

Run: `head -40 docs/api-reference.md && head -40 docs/cli-reference.md`
Match the existing heading levels, table style, and code-fence conventions rather than inventing new ones.

- [ ] **Step 2: Add the API reference section**

Append to `docs/api-reference.md`, following the file's existing endpoint format:

```markdown
## Configuration backup and restore

### `GET /api/v1/config/export`

Returns every stored tunnel definition as a versioned JSON archive. Sets
`Content-Disposition: attachment`, so a browser downloads it directly.

The archive contains hostnames, SSH usernames and key **paths**. It contains no
key material — `AuthConfig` is not persisted by the storage layer, so a restore
does not return credentials.

### `POST /api/v1/config/import`

Restores tunnel definitions from an archive.

| Query parameter | Values | Default | Meaning |
|---|---|---|---|
| `mode` | `merge`, `replace` | `merge` | `merge` updates and creates, never deletes. `replace` also deletes stored tunnels absent from the archive. |
| `dry_run` | `true` | unset | Return the intended plan without writing anything. |

Tunnels are matched by **name**. A matched tunnel keeps its existing ID, owner
and creation time; only its configuration is updated. An entry identical to what
is already stored is reported as `skip` and is not rewritten, so re-importing an
unmodified archive changes nothing and does not interrupt running tunnels.

After a successful import the server reconciles the running fleet against the
restored `desired_status`: tunnels recorded as active are started, tunnels
recorded as stopped are stopped, and unchanged tunnels are left connected.

Responses:

- `200` — the report (see below).
- `400` — malformed JSON, an unsupported `version`, an unknown `mode`, or entry
  validation failures. Validation failures are returned together under
  `details` so the file can be fixed in one pass.
- `500` — a write failed partway. The report body names exactly which items
  landed. **Import is validate-then-write, not transactional** — the storage
  layer exposes no transaction API. Merge mode is idempotent, so re-running a
  failed import converges.

Report body:

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
```

- [ ] **Step 3: Add the CLI reference section**

Append to `docs/cli-reference.md`, following the file's existing command format:

```markdown
## `tunnelctl export`

Export every tunnel definition as a versioned JSON archive.

```bash
tunnelctl export > tunnels.json
tunnelctl export -o tunnels.json
```

| Flag | Description |
|---|---|
| `-o`, `--output FILE` | Write to `FILE` instead of stdout. The file is created with `0600` permissions. |

Output goes to stdout by default so it pipes and redirects cleanly — useful in a
cron job or committed to a config repository.

## `tunnelctl import FILE`

Restore tunnel definitions from an archive produced by `tunnelctl export`.

```bash
tunnelctl import tunnels.json
tunnelctl import --dry-run tunnels.json
tunnelctl import --replace tunnels.json
```

| Flag | Description |
|---|---|
| `--replace` | Delete tunnels not present in the archive, so the server mirrors the file exactly. |
| `--dry-run` | Print what would change and write nothing. |
| `-y`, `--yes` | Skip the confirmation prompt for deletions. |

By default the import merges: tunnels matching by name are updated in place,
missing ones are created, and anything not mentioned is left alone. Merging
never deletes.

Every run previews first, then applies:

```
$ tunnelctl import tunnels.json
  update  prod-db
  create  staging-api
  skip    socks-jump   identical to stored tunnel

1 created, 1 updated, 1 skipped, 0 deleted
```

With `--replace`, deletions are listed and confirmed before anything is written:

```
$ tunnelctl import --replace tunnels.json
  update  prod-db
  DELETE  old-bastion  not present in archive

--replace deletes 1 tunnel(s). Continue? [y/N]
```
```

- [ ] **Step 4: Verify the whole suite one more time**

Run: `go build ./... && go test ./... && cd web && npm run build && npm run test:run`
Expected: all green.

- [ ] **Step 5: Commit**

```bash
git add docs/api-reference.md docs/cli-reference.md
git commit -m "docs: backup and restore API and CLI reference"
```

---

## Self-review notes

**Spec coverage:** scope (Task 1 DTO), file format and field semantics (Tasks 1-2), merge/replace modes (Task 4), name matching and ID rules (Task 4), the `entriesEqual` definition (Task 4), `owner` preservation (Task 4), `Reconcile` including the lock ordering rule (Task 6), the API surface (Task 7), CLI (Tasks 8-9), web UI (Task 10), error handling and the non-atomicity disclosure (Tasks 5, 7, 11), security note (Tasks 8, 10, 11), and every listed test case (Tasks 1-9). The spec's build order maps to Tasks 1-11 in sequence.

**Naming consistency:** `EntryFromSpec` / `SpecFromEntry` (Task 1) are used unchanged in Tasks 3, 4, and 7. `Store` (Task 3) is consumed by Tasks 5 and 7. `ImportPlan`, `PlanItem`, `Action`, `Mode` (Task 4) are consumed by Task 5. `Report` / `ItemResult` (Task 5) match the CLI's `importReport` / `importItem` JSON tags (Task 9) and the TypeScript `ImportReport` / `ImportItem` (Task 10) field-for-field.

**Deliberate deviations from the spec, both safe:**
- Validation is more permissive than `api.CreateTunnelRequest` for `dynamic` tunnels (no `remote_host` required), so every exported archive re-imports. Recorded in Task 2.
- `Export` and `Plan` take injected `Clock` / `NewID` function values that the spec does not mention. They exist purely so tests are deterministic; production callers pass `time.Now` and get `uuid.New()` by default.
