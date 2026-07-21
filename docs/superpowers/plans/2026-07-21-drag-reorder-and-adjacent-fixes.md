# Tunnel List Drag-to-Reorder + Adjacent Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let users drag tunnel rows into a custom order persisted in the browser, and correct seven verified defects found while scoping that feature.

**Architecture:** The reorder feature is frontend-only. A persisted zustand store holds an array of tunnel IDs; a pure function applies that array to the polled tunnel list at render time, replacing the hardcoded alphabetical sort. No backend involvement. The adjacent fixes are independent of the feature and of each other, except where noted in task ordering.

**Tech Stack:** Go 1.24 (toolchain 1.26.5), SQLite via `modernc.org/sqlite`, React 19.2, TypeScript, zustand 5, TanStack Query 5, Radix UI + Tailwind 3.4, Vite 7.

## Global Constraints

- **Go is not on the default PATH.** Every Go command must be prefixed: `export PATH=$PATH:/usr/local/go/bin`. Verify with `go version` → `go1.26.5 linux/amd64`.
- **Go tests use plain stdlib `testing`.** No testify, no assertion library. Match the style in `internal/tunnel/manager_test.go`.
- **Baseline is green.** `go build ./...` succeeds and `go test ./...` passes (`internal/api`, `internal/auth`, `internal/config`, `internal/tunnel`). Any failure you see is yours.
- **Frontend commands run from `/home/cd/Work/lazytunnel/web`.**
- **No new Go dependencies.** The Go fixes use only stdlib (`sort`, `encoding/json`).
- **New frontend dependencies, exact versions:** `@dnd-kit/core@^6.3.1`, `@dnd-kit/sortable@^10.0.0`, `@dnd-kit/modifiers`, `@radix-ui/react-alert-dialog`, and `vitest` (devDependency). All install clean on React 19.2 — peer ranges are `react: >=16.8.0`, so no `--legacy-peer-deps`.
- **localStorage key:** `lazytunnel-tunnel-order`. Matches the existing `lazytunnel-theme` / `lazytunnel-settings` convention.
- **Commit after every task.** Do not batch commits across tasks.
- **Do not fix anything not named in a task.** Several further defects were found and deliberately deferred; they are listed at the end.

## File Structure

| File | Responsibility | Task |
|---|---|---|
| `internal/tunnel/manager.go` | `List()` returns a deterministic total order | 1 |
| `internal/tunnel/manager_test.go` | ordering test | 1 |
| `internal/cli/list.go` | parse the real response shape | 2 |
| `internal/cli/list_test.go` | parsing test (new file) | 2 |
| `CLAUDE.md` | accurate project facts | 3 |
| `api/openapi.yaml` | complete, valid API contract | 4 |
| `web/vite.config.ts` | Vitest config block | 5 |
| `web/package.json` | test script + deps | 5 |
| `web/src/lib/queries.ts` | store sync moved into an effect | 6 |
| `web/src/components/TunnelForm.tsx` | preserve keepAlive/maxRetries on edit | 7 |
| `web/src/components/ui/alert-dialog.tsx` | Radix alert-dialog primitive (new) | 8 |
| `web/src/components/TunnelList.tsx` | delete confirmation, then drag wiring | 8, 11 |
| `web/src/lib/tunnelOrder.ts` | pure ordering functions (new) | 9 |
| `web/src/lib/tunnelOrder.test.ts` | ordering tests (new) | 9 |
| `web/src/store/orderStore.ts` | persisted ID array (new) | 10 |

## Task Ordering

Tasks 1–4 are independent. Task 5 must precede Task 9 (Vitest must exist before tests are written). Task 6 must precede Task 11 (the reorder effect depends on the store sync being effect-based). Task 8 must precede Task 11 (both edit `TunnelList.tsx`; doing the simpler change first avoids rebasing the drag wiring).

---

### Task 1: Deterministic tunnel ordering in `Manager.List()`

`Manager.List()` iterates a Go map, and Go randomizes map iteration order, so `GET /api/v1/tunnels` returns a different order on every request. The storage layer already declares the intended order (`ORDER BY created_at DESC`, `internal/storage/sqlite.go:290`) with a backing index (`sqlite.go:66`), but that ordering dies at `manager.go:110` when specs are inserted into the map.

**Files:**
- Modify: `internal/tunnel/manager.go:466-477`
- Test: `internal/tunnel/manager_test.go` (append)

**Interfaces:**
- Consumes: nothing.
- Produces: `Manager.List() []*Tunnel` now sorted by `CreatedAt` descending, ties broken by `Spec.ID` ascending. Callers at `internal/api/handlers.go:30`, `handlers.go:56`, and `manager.go:118` are unaffected in behavior; only order changes.

- [ ] **Step 1: Write the failing test**

Append to `internal/tunnel/manager_test.go`:

```go
func TestListReturnsDeterministicOrder(t *testing.T) {
	ctx := context.Background()
	manager := NewManager(ctx)

	base := time.Now()
	// Insert with identical CreatedAt for two of them so the ID tiebreaker is exercised.
	seed := []struct {
		id        string
		createdAt time.Time
	}{
		{"ccc", base.Add(-2 * time.Hour)},
		{"aaa", base},
		{"bbb", base},
		{"ddd", base.Add(-1 * time.Hour)},
	}

	for _, s := range seed {
		manager.tunnels[s.id] = &Tunnel{
			Spec:      &types.TunnelSpec{ID: s.id, Name: s.id},
			CreatedAt: s.createdAt,
		}
	}

	// Newest first; equal timestamps broken by ID ascending.
	want := []string{"aaa", "bbb", "ddd", "ccc"}

	// Repeat: a single pass can pass by luck under randomized map iteration.
	for i := 0; i < 20; i++ {
		got := make([]string, 0, len(want))
		for _, tunnel := range manager.List() {
			got = append(got, tunnel.Spec.ID)
		}
		if len(got) != len(want) {
			t.Fatalf("iteration %d: got %d tunnels, want %d", i, len(got), len(want))
		}
		for j := range want {
			if got[j] != want[j] {
				t.Fatalf("iteration %d: got order %v, want %v", i, got, want)
			}
		}
	}
}
```

- [ ] **Step 2: Run the test and verify it fails**

```bash
export PATH=$PATH:/usr/local/go/bin
cd /home/cd/Work/lazytunnel
go test ./internal/tunnel/ -run TestListReturnsDeterministicOrder -v
```

Expected: FAIL, reporting a mismatched order on some iteration. If it passes, the map happened to iterate in order across all 20 passes — rerun; it will fail.

- [ ] **Step 3: Implement the sort**

Add `"sort"` to the import block at the top of `internal/tunnel/manager.go`, then replace lines 466-477 with:

```go
// List returns all tunnels, newest first, matching the storage layer's
// ORDER BY created_at DESC. Ties are broken by ID so the order is a stable
// total order rather than dependent on Go's randomized map iteration.
func (m *Manager) List() []*Tunnel {
	m.mu.RLock()
	defer m.mu.RUnlock()

	tunnels := make([]*Tunnel, 0, len(m.tunnels))
	for _, tunnel := range m.tunnels {
		tunnels = append(tunnels, tunnel)
	}

	sort.Slice(tunnels, func(i, j int) bool {
		if c := tunnels[i].CreatedAt.Compare(tunnels[j].CreatedAt); c != 0 {
			return c > 0 // descending: newest first
		}
		return tunnels[i].Spec.ID < tunnels[j].Spec.ID
	})

	return tunnels
}
```

`time.Time.Compare` is used rather than `Before`/`After` so the tiebreaker fires only on genuine equality. `Spec.ID` is the primary key and is a UUID, so it is unique and non-empty — that guarantees a strict total order.

- [ ] **Step 4: Run the test and verify it passes**

```bash
export PATH=$PATH:/usr/local/go/bin
go test ./internal/tunnel/ -run TestListReturnsDeterministicOrder -v
```

Expected: PASS.

- [ ] **Step 5: Run the full Go suite**

```bash
export PATH=$PATH:/usr/local/go/bin
go build ./... && go test ./...
```

Expected: build clean, all previously-passing packages still pass. `internal/tunnel` takes ~8s.

- [ ] **Step 6: Commit**

```bash
git add internal/tunnel/manager.go internal/tunnel/manager_test.go
git commit -m "fix: return tunnels in deterministic order from Manager.List

Manager.List iterated a map, so GET /api/v1/tunnels returned a
different order every request. Sort by CreatedAt descending with an
ID tiebreaker, matching the ORDER BY created_at DESC the storage
layer already declares."
```

---

### Task 2: Fix `tunnelctl list` response parsing

`runList` unmarshals the response into `map[string]interface{}` and reads `result["tunnels"]`, but `handleListTunnels` responds with a **bare JSON array** (`internal/api/handlers.go:104`). Unmarshalling an array into a map errors, so the command fails at `list.go:40` with "failed to parse response" and never prints. Two further mismatches sit behind it: `list.go:65` reads `status` as a nested object with a `state` key, but the server sends a flat string (`handlers.go:97`); and `list.go:68` reads `created_at` where the server sends `createdAt` (`handlers.go:98`). Both are unchecked type assertions that would panic.

**Files:**
- Modify: `internal/cli/list.go`
- Test: `internal/cli/list_test.go` (create)

**Interfaces:**
- Consumes: nothing.
- Produces: `parseTunnelList(body []byte) ([]tunnelListItem, error)` and the `tunnelListItem` struct, both package-private to `internal/cli`.

- [ ] **Step 1: Write the failing test**

Create `internal/cli/list_test.go`:

```go
package cli

import "testing"

func TestParseTunnelListReadsBareArray(t *testing.T) {
	// Exactly the shape handleListTunnels emits: a bare array, flat status
	// string, camelCase createdAt.
	body := []byte(`[
		{"id":"abc123","name":"prod-db","type":"local","status":"active","createdAt":"2026-07-21T10:00:00Z"},
		{"id":"def456","name":"socks","type":"dynamic","status":"stopped","createdAt":"2026-07-20T09:00:00Z"}
	]`)

	items, err := parseTunnelList(body)
	if err != nil {
		t.Fatalf("parseTunnelList returned error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
	if items[0].ID != "abc123" {
		t.Errorf("got ID %q, want abc123", items[0].ID)
	}
	if items[0].Name != "prod-db" {
		t.Errorf("got Name %q, want prod-db", items[0].Name)
	}
	if items[0].Status != "active" {
		t.Errorf("got Status %q, want active", items[0].Status)
	}
	if items[0].CreatedAt != "2026-07-21T10:00:00Z" {
		t.Errorf("got CreatedAt %q, want 2026-07-21T10:00:00Z", items[0].CreatedAt)
	}
}

func TestParseTunnelListEmptyArray(t *testing.T) {
	items, err := parseTunnelList([]byte(`[]`))
	if err != nil {
		t.Fatalf("parseTunnelList returned error: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("got %d items, want 0", len(items))
	}
}

func TestParseTunnelListRejectsMalformed(t *testing.T) {
	if _, err := parseTunnelList([]byte(`{"tunnels":[]}`)); err == nil {
		t.Fatal("expected an error for an object body, got nil")
	}
}
```

- [ ] **Step 2: Run the test and verify it fails**

```bash
export PATH=$PATH:/usr/local/go/bin
cd /home/cd/Work/lazytunnel
go test ./internal/cli/ -v
```

Expected: FAIL to build — `undefined: parseTunnelList`.

- [ ] **Step 3: Implement the parser and rewrite the print loop**

Replace the whole body of `internal/cli/list.go` below the import block with:

```go
var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all active tunnels",
	Long:  `List all currently active SSH tunnels on the server.`,
	RunE:  runList,
}

// tunnelListItem mirrors the fields handleListTunnels emits for each tunnel.
// The server responds with a bare array, a flat status string, and camelCase
// timestamp keys.
type tunnelListItem struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	Status    string `json:"status"`
	CreatedAt string `json:"createdAt"`
}

func parseTunnelList(body []byte) ([]tunnelListItem, error) {
	var items []tunnelListItem
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	return items, nil
}

func runList(cmd *cobra.Command, args []string) error {
	serverURL := viper.GetString("server")
	url := fmt.Sprintf("%s/api/v1/tunnels", serverURL)

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to list tunnels: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to list tunnels: %s", string(body))
	}

	tunnels, err := parseTunnelList(body)
	if err != nil {
		return err
	}

	if len(tunnels) == 0 {
		fmt.Println("No active tunnels")
		return nil
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tTYPE\tSTATE\tCREATED")
	fmt.Fprintln(w, "──\t────\t────\t─────\t───────")

	for _, tunnel := range tunnels {
		created := tunnel.CreatedAt
		if parsed, err := time.Parse(time.RFC3339, tunnel.CreatedAt); err == nil {
			created = parsed.Format("2006-01-02 15:04")
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			truncate(tunnel.ID, 8),
			tunnel.Name,
			tunnel.Type,
			tunnel.Status,
			created,
		)
	}

	w.Flush()

	fmt.Printf("\nTotal: %d tunnel(s)\n", len(tunnels))

	return nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
```

Decoding into a struct removes every unchecked type assertion, so a malformed or changed response can no longer panic. A `createdAt` that fails to parse now falls back to the raw string instead of rendering as the zero time.

- [ ] **Step 4: Run the test and verify it passes**

```bash
export PATH=$PATH:/usr/local/go/bin
go test ./internal/cli/ -v
```

Expected: PASS, 3 tests.

- [ ] **Step 5: Verify the build and full suite**

```bash
export PATH=$PATH:/usr/local/go/bin
go build ./... && go test ./...
```

Expected: clean build, all packages pass, `internal/cli` now reports `ok` instead of `[no test files]`.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/list.go internal/cli/list_test.go
git commit -m "fix: parse the real response shape in tunnelctl list

The handler responds with a bare JSON array, but runList unmarshalled
into a map and read result[\"tunnels\"], so the command always failed
with 'failed to parse response'. It also read status as a nested
object and created_at instead of createdAt, both unchecked assertions
that would have panicked. Decode into a struct instead."
```

---

### Task 3: Correct CLAUDE.md

`CLAUDE.md` describes a system that does not exist. It specifies PostgreSQL (the project uses SQLite via `modernc.org/sqlite`), Ant Design (the project uses Radix + Tailwind), and declares itself a greenfield project in "Phase 1" when the server, agent, CLI, and web UI are all built and running. An agent following this file makes wrong decisions on its first move.

**Files:**
- Modify: `CLAUDE.md`

**Interfaces:** none — documentation only.

- [ ] **Step 1: Gather the ground truth**

Run each command and keep the output alongside you while editing. Do not write a fact you have not confirmed here.

```bash
export PATH=$PATH:/usr/local/go/bin
cd /home/cd/Work/lazytunnel
head -3 go.mod                                    # module path and Go version
grep -E "modernc|gorilla|cobra|viper|zerolog|zap|chi" go.mod
grep -n "Path" internal/config/config.go | head    # DatabaseConfig shape
ls migrations/ ; echo "(empty means no migration tool)"
ls cmd/ internal/ pkg/ web/src/
node -e "const p=require('./web/package.json');console.log(Object.keys(p.dependencies).join(', '))"
ls docker-compose.yml .air.toml 2>/dev/null
grep -n "jwt_secret\|JWTSecret" cmd/server/main.go internal/config/config.go | head
```

- [ ] **Step 2: Rewrite the wrong sections**

Make these corrections. Each one is a statement in the file that is false.

1. **Technology Stack → Database:** replace `PostgreSQL 15+ with JSONB` with SQLite via `modernc.org/sqlite` (pure Go, no cgo). Note the DB is a single file path configured by `database.path`.
2. **Technology Stack → Frontend:** replace `Ant Design or shadcn/ui` with Radix UI primitives + Tailwind CSS, shadcn-style. Replace `Zustand or React Query` with "zustand for client state, TanStack Query for server state". Correct React 18+ to React 19.
3. **Technology Stack → Language:** correct `Go 1.21+` to the version in `go.mod` (`1.24.0`).
4. **Project Structure:** replace the invented tree with the real one from Step 1. Notably `web/` exists and is built (drop "when implemented"), and `internal/` has no `kms/` directory.
5. **Development Commands → migrations:** delete the `migrate -path migrations ...` instruction. `migrations/` is empty and no migration tool is used. Replace with: the schema is applied at boot by `initSchema()` in `internal/storage/sqlite.go`, and new columns are added with a guarded `ALTER TABLE ... ADD COLUMN` following the existing `agent_id` / `desired_status` pattern.
6. **Development Commands → docker-compose:** remove `docker-compose up -d postgres` if no postgres service exists in `docker-compose.yml`.
7. **Configuration:** replace the invented `config.yaml` example with the real key set. Cross-check against `internal/config/config.go` and `config.example.yaml`; drop the `kms:` block entirely if no KMS integration exists.
8. **Security Requirements → Key Management:** the KMS requirements describe an unimplemented system. Move them under a clearly-labelled "Not yet implemented / aspirational" heading rather than stating them as current fact.
9. **Auth:** the file specifies OAuth2/OIDC. Correct to what `internal/api/handlers.go` and `cmd/server/main.go` actually do: JWT bearer tokens, with auth **disabled entirely** unless a JWT secret is configured (`main.go:71`). Note the hardcoded development credentials.
10. **Implementation Status:** delete the "greenfield project" and "Phase 1: Core SSH Engine (Current Focus)" framing. Replace with a short, accurate statement of what exists: server, agent, CLI, and web UI, with SQLite persistence and a remote-agent control plane.

Keep the file's existing structure and heading style. Do not add new sections beyond the "not yet implemented" relabel.

- [ ] **Step 3: Verify no false claims remain**

Re-run the Step 1 block and read the edited file top to bottom against it. Then confirm the two obvious markers are gone:

```bash
grep -in "postgres\|ant design\|greenfield\|migrate -path" CLAUDE.md
```

Expected: no output, or only matches inside the explicitly-labelled aspirational section.

- [ ] **Step 4: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: correct CLAUDE.md to match the actual system

The file specified PostgreSQL, Ant Design, OAuth2/OIDC, a migrate
CLI, and a greenfield Phase 1 status. The project actually uses
SQLite, Radix + Tailwind, optional JWT auth, boot-time schema init,
and is fully built. Also corrects the Go and React versions and the
project structure tree."
```

---

### Task 4: Complete `api/openapi.yaml`

The spec documents 11 of 20 registered operations and fails strict OpenAPI 3.0.3 validation. Per the project decision, the endpoint **stays public** — no auth change to `server.go`.

**Files:**
- Modify: `api/openapi.yaml`

**Interfaces:** none — documentation only.

- [ ] **Step 1: Confirm the route inventory**

```bash
cd /home/cd/Work/lazytunnel
grep -nE "HandleFunc|\.Handle\(" internal/api/server.go
grep -n "paths:" -A 200 api/openapi.yaml | grep -E "^\s+/" | head -30
```

The first list is truth. Every path in it except the `web/dist` static handler must appear in the spec.

- [ ] **Step 2: Add the 9 missing operations**

Undocumented today, all under the `/api/v1` server prefix:

| Method | Path | Handler |
|---|---|---|
| PUT | `/tunnels/{id}` | `handleUpdateTunnel` (`server.go:160`) |
| GET | `/tunnels/{id}/status` | `handleGetTunnelStatus` (`server.go:164`) |
| GET | `/openapi.yaml` | `handleOpenAPI` (`server.go:131`, public) |
| GET | `/metrics` | `HandleMetrics()` (`server.go:134`, public, Prometheus text) |
| GET | `/agents` | `handleListAgents` (`server.go:144`) |
| POST | `/agents/register` | `handleRegisterAgent` (`server.go:145`) |
| POST | `/agents/{id}/heartbeat` | `handleAgentHeartbeat` (`server.go:146`) |
| GET | `/agents/{id}/assignments` | `handleAgentAssignments` (`server.go:147`) |
| POST | `/agents/{id}/report` | `handleAgentReport` (`server.go:148`) |

Insert the PUT between the existing `get:` and `delete:` blocks under `/tunnels/{id}`:

```yaml
    put:
      operationId: updateTunnel
      summary: Update a stopped tunnel's configuration
      tags: [Tunnels]
      security:
        - bearerAuth: []
      parameters:
        - $ref: "#/components/parameters/TunnelId"
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: "#/components/schemas/CreateTunnelRequest"
      responses:
        "200":
          description: Updated tunnel
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/Tunnel"
        "400":
          $ref: "#/components/responses/BadRequest"
        "404":
          $ref: "#/components/responses/NotFound"
```

Document the remaining eight in the same style. Derive the agent request/response schemas from `pkg/types/agent.go` and the `/tunnels/{id}/status` response from `pkg/types/tunnel.go` — note that `/status` returns `types.TunnelStatus` verbatim and is **snake_case**, unlike every other response, so give it its own schema rather than reusing `Tunnel`.

- [ ] **Step 3: Fix the schema drift**

Verify each against the Go source before writing it.

- `CreateTunnelRequest` (vs `internal/api/validation.go:48-60`): add `localBindAddress` and `agentId`; **remove `localPort` from `required`** — `validation.go:52` is `min=0,max=65535` with no `required`, so 0 is valid; encode the real bounds (`name` 1-100, `remotePort` 1-65535, `keepAlive` 0-300, `maxRetries` 0-100, `hops` minItems 1).
- `Tunnel` response (vs `internal/api/responses.go:28-50`): add `agentId`, `desiredStatus`, `localBindAddress`; give `createdAt`/`updatedAt` `format: date-time` (both are RFC3339 per `responses.go:46-47`).
- `Hop`: split into `HopRequest` (what `validation.go:63-69` accepts) and `Hop` (what `pkg/types/tunnel.go:77-85` returns, which additionally emits `host_key_verification` and `known_hosts_path`). One schema cannot describe both.
- `APIError` (vs `internal/api/errors.go:48-55`): add `details` (array of `{field, value, issue}`) and `request_id`. Add a separate `SimpleError` schema for the `{"error": "..."}` shape emitted by `server.go:294-298`, used by `handleOpenAPI` and `handleGetLogs` on failure.

- [ ] **Step 4: Fix the OpenAPI 3.0.3 validity defects**

These make the document fail strict validation today, before any missing-route concern:

- A Response Object **requires** `description`. Add one to each `"200"` currently missing it (around lines 91, 116, 131, 146, 169 — verify positions after your earlier edits shift them).
- An Operation Object **requires** `responses`. `/ws` has none; add `"101": {description: Switching Protocols}`.

- [ ] **Step 5: Validate**

```bash
cd /home/cd/Work/lazytunnel
npx --yes @redocly/cli@latest lint api/openapi.yaml
```

Expected: no errors. Before this task it fails on the missing `responses` and the description-less 200s, so a clean run is the signal the validity defects are genuinely fixed.

Then confirm every registered route is documented:

```bash
grep -oE '"/[a-z{}/.]+"' internal/api/server.go | tr -d '"' | sort -u
grep -E "^  /" api/openapi.yaml | tr -d ' :' | sort -u
```

Compare the two lists by eye. The only acceptable difference is the `/agents` prefix, which `server.go:140` factors into a subrouter, and the static-file catch-all.

- [ ] **Step 6: Commit**

```bash
git add api/openapi.yaml
git commit -m "docs: complete and validate the OpenAPI spec

Documents the 9 registered operations that were missing, including
PUT /tunnels/{id} and the entire agent control plane. Corrects schema
drift in CreateTunnelRequest, Tunnel, Hop, and APIError against the
Go source, and fixes the validity defects that made the document fail
OpenAPI 3.0.3 validation."
```

---

### Task 5: Add the Vitest harness

The frontend has no test runner, no test script, and zero test files. Task 9 needs one. Vitest reuses the existing Vite config, including the `@` alias, so no duplicate path setup.

**Files:**
- Modify: `web/package.json`
- Modify: `web/vite.config.ts`

**Interfaces:**
- Produces: `npm test` (watch) and `npm run test:run` (single pass, for CI and for the verification steps in Task 9).

- [ ] **Step 1: Install Vitest**

```bash
cd /home/cd/Work/lazytunnel/web
npm install -D vitest
```

- [ ] **Step 2: Add the test scripts**

In `web/package.json`, add to `"scripts"`:

```json
    "test": "vitest",
    "test:run": "vitest run"
```

- [ ] **Step 3: Add the Vitest config block**

Replace `web/vite.config.ts` with:

```ts
import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'
import path from 'path'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  server: {
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
        ws: true,
      },
    },
  },
  test: {
    environment: 'node',
    include: ['src/**/*.test.ts'],
  },
})
```

The import moves from `vite` to `vitest/config` — that is what types the `test` block, and it is a drop-in for the Vite config itself. `environment: 'node'` is deliberate: the only tests are of a pure function, so jsdom is unnecessary weight.

- [ ] **Step 4: Verify the harness runs**

```bash
cd /home/cd/Work/lazytunnel/web
npm run test:run
```

Expected: Vitest starts and reports "No test files found" — this is success. It proves the runner and config resolve. It exits non-zero; that is expected with no tests and is fine.

- [ ] **Step 5: Verify the app still builds**

```bash
npm run build
```

Expected: `tsc -b` passes and Vite emits to `dist/`. This confirms swapping the config import broke nothing.

- [ ] **Step 6: Commit**

```bash
git add package.json package-lock.json vite.config.ts
git commit -m "test: add Vitest harness for frontend unit tests

No test runner existed. Vitest reuses the Vite config including the
@ alias. node environment, since the tests target pure functions."
```

---

### Task 6: Move the tunnel store sync into an effect

`useTunnels()` calls `setTunnels(query.data)` directly in the hook body (`web/src/lib/queries.ts:30-32`), so a zustand store write happens during React's render phase. Task 11 adds a second store write driven by the same data, and stacking that on a render-phase write risks an update loop. Fix the foundation first.

**Files:**
- Modify: `web/src/lib/queries.ts:17-35`

**Interfaces:**
- Consumes: nothing.
- Produces: `useTunnels()` keeps the same signature and return value (the TanStack `query` object). Only the timing of the store write changes.

- [ ] **Step 1: Make the change**

Add `useEffect` to the React import at the top of `web/src/lib/queries.ts` (add `import { useEffect } from 'react'` if there is no React import yet), then replace lines 17-35:

```ts
export function useTunnels() {
  const setTunnels = useTunnelStore((state) => state.setTunnels)
  const isDemoMode = useTunnelStore((state) => state.isDemoMode)

  const query = useQuery({
    queryKey: tunnelKeys.lists(),
    queryFn: () => api.listTunnels(),
    refetchInterval: 5000, // Refetch every 5 seconds for real-time updates
    retry: false, // Don't retry on error (avoid spam when server is down)
    enabled: !isDemoMode, // Don't fetch from API when in demo mode
  })

  const data = query.data

  // Sync into the store after commit, not during render. Writing to an
  // external store mid-render is a React anti-pattern and would compound
  // once the ordering effect also writes on new data.
  useEffect(() => {
    if (data && !isDemoMode) {
      setTunnels(data)
    }
  }, [data, isDemoMode, setTunnels])

  return query
}
```

- [ ] **Step 2: Verify it typechecks and builds**

```bash
cd /home/cd/Work/lazytunnel/web
npm run build
```

Expected: clean.

- [ ] **Step 3: Verify in the browser, in live mode**

Start the backend and the dev server:

```bash
export PATH=$PATH:/usr/local/go/bin
cd /home/cd/Work/lazytunnel && go run cmd/server/main.go &
cd /home/cd/Work/lazytunnel/web && npm run dev
```

Open the app, log in (`admin` / `lazytunnel`), and confirm:
- The tunnel list renders its rows.
- Rows are still present after 10+ seconds (two poll cycles).
- The browser console shows no React warnings about updating state during render.

**Do not verify this in demo mode.** Demo mode disables the list query entirely (`queries.ts:26`), so this code path never runs and the check would prove nothing.

- [ ] **Step 4: Commit**

```bash
git add src/lib/queries.ts
git commit -m "fix: sync tunnels into the store from an effect

setTunnels was called in the hook body, writing to a zustand store
during render. Move it into useEffect so the write happens after
commit."
```

---

### Task 7: Preserve `keepAlive` and `maxRetries` when editing a tunnel

`TunnelForm.tsx:149-150` hardcodes `keepAlive: 30` and `maxRetries: 5` into a payload used for **both** create and update (line 154). The form has no inputs for either field, so on every edit the tunnel's stored values are overwritten with the defaults — a tunnel configured with `keepAlive: 60` silently drops to 30 the next time anything else about it is edited.

Units are safe to round-trip: the response emits seconds (`internal/api/responses.go:43` — `spec.KeepAlive.Seconds()`) and the request accepts seconds (`internal/api/validation.go:57`, converted at `handlers.go:232`). There is no nanosecond `time.Duration` trap.

**Files:**
- Modify: `web/src/components/TunnelForm.tsx:141-152`

**Interfaces:**
- Consumes: the `tunnel` prop already in scope (used at line 155 as `tunnel!.id`) and the `editing` boolean already used at line 154.
- Produces: no signature change.

- [ ] **Step 1: Make the change**

Replace the `payload` object at `web/src/components/TunnelForm.tsx:141-152`:

```tsx
      const payload = {
        name: data.name,
        type: data.type,
        localPort: data.localPort,
        remoteHost: data.remoteHost || '',
        remotePort: data.remotePort || 0,
        hops,
        autoReconnect: data.autoReconnect,
        // The form has no inputs for these two. On edit, carry the tunnel's
        // existing values through rather than resetting them to the defaults.
        // The API emits and accepts seconds, so this round-trips safely.
        keepAlive: editing ? Math.round(tunnel!.keepAlive) : 30,
        maxRetries: editing ? tunnel!.maxRetries : 5,
        agentId: agentId || undefined,
      }
```

`Math.round` guards the type boundary: the response field comes from Go's `.Seconds()` and is a JSON float, while the request field is a Go `int` (`validation.go:57`) — an unrounded `30.5` would fail to unmarshal server-side.

- [ ] **Step 2: Verify it typechecks and builds**

```bash
cd /home/cd/Work/lazytunnel/web
npm run build
```

Expected: clean. If `tunnel` is possibly-undefined in the editing branch, do not weaken the type — the existing line 155 already uses `tunnel!.id` under the same `editing` guard, so the same assertion is consistent.

- [ ] **Step 3: Verify the round-trip against a live server**

With the backend running, create a tunnel whose `keepAlive` is not the default, then edit something unrelated and confirm the value survives:

```bash
export PATH=$PATH:/usr/local/go/bin
cd /home/cd/Work/lazytunnel && go run cmd/server/main.go &
curl -s -XPOST localhost:8080/api/v1/tunnels -H 'Content-Type: application/json' -d '{
  "name":"keepalive-probe","type":"local","localPort":19999,
  "remoteHost":"example.com","remotePort":80,"keepAlive":60,"maxRetries":9,
  "hops":[{"host":"example.com","port":22,"user":"deploy","auth_method":"key"}]
}' | python3 -m json.tool | grep -E "id|keepAlive|maxRetries"
```

Note the `id` and confirm `keepAlive: 60`, `maxRetries: 9`. Then in the web UI, edit that tunnel (change only its name) and save. Re-read it:

```bash
curl -s localhost:8080/api/v1/tunnels | python3 -c "import sys,json;[print(t['name'],t['keepAlive'],t['maxRetries']) for t in json.load(sys.stdin)]"
```

Expected: `keepAlive` is still `60` and `maxRetries` still `9`. Before this fix they would read `30` and `5`.

Clean up: delete the probe tunnel via the UI.

- [ ] **Step 4: Commit**

```bash
git add src/components/TunnelForm.tsx
git commit -m "fix: preserve keepAlive and maxRetries when editing a tunnel

The payload hardcoded keepAlive: 30 and maxRetries: 5 for both create
and update, so editing any field reset a tunnel's connection tuning to
the defaults. Carry the existing values through on edit."
```

---

### Task 8: Confirm before deleting a tunnel

The Delete button at `web/src/components/TunnelList.tsx:190-192` calls `deleteTunnel.mutate` directly. There is no confirmation, no undo, and the button sits immediately beside Edit — one misclick destroys a tunnel. No confirmation primitive exists in the codebase (`src/components/ui/` has `dialog.tsx` but no `alert-dialog.tsx`, and nothing calls `window.confirm`).

**Files:**
- Create: `web/src/components/ui/alert-dialog.tsx`
- Modify: `web/src/components/TunnelList.tsx`

**Interfaces:**
- Produces: the standard shadcn alert-dialog exports — `AlertDialog`, `AlertDialogAction`, `AlertDialogCancel`, `AlertDialogContent`, `AlertDialogDescription`, `AlertDialogFooter`, `AlertDialogHeader`, `AlertDialogTitle`. Task 11 does not use these but shares the file it edits.

- [ ] **Step 1: Install the Radix primitive**

```bash
cd /home/cd/Work/lazytunnel/web
npm install @radix-ui/react-alert-dialog
```

This matches the four `@radix-ui/*` packages already in use. Alert-dialog is the correct primitive rather than plain dialog: it traps focus, defaults focus to the cancel action, and carries `role="alertdialog"` for screen readers.

- [ ] **Step 2: Create the primitive**

Create `web/src/components/ui/alert-dialog.tsx`. The overlay and content class strings are copied verbatim from the existing `web/src/components/ui/dialog.tsx:18` and `:35` so the two dialogs are visually identical:

```tsx
import * as React from "react"
import * as AlertDialogPrimitive from "@radix-ui/react-alert-dialog"
import { cn } from "@/lib/utils"
import { buttonVariants } from "./button"

const AlertDialog = AlertDialogPrimitive.Root
const AlertDialogTrigger = AlertDialogPrimitive.Trigger
const AlertDialogPortal = AlertDialogPrimitive.Portal

const AlertDialogOverlay = React.forwardRef<
  React.ElementRef<typeof AlertDialogPrimitive.Overlay>,
  React.ComponentPropsWithoutRef<typeof AlertDialogPrimitive.Overlay>
>(({ className, ...props }, ref) => (
  <AlertDialogPrimitive.Overlay
    ref={ref}
    className={cn(
      "fixed inset-0 z-50 bg-background/80 backdrop-blur-sm data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0",
      className
    )}
    {...props}
  />
))
AlertDialogOverlay.displayName = AlertDialogPrimitive.Overlay.displayName

const AlertDialogContent = React.forwardRef<
  React.ElementRef<typeof AlertDialogPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof AlertDialogPrimitive.Content>
>(({ className, ...props }, ref) => (
  <AlertDialogPortal>
    <AlertDialogOverlay />
    <AlertDialogPrimitive.Content
      ref={ref}
      className={cn(
        "fixed left-[50%] top-[50%] z-50 grid w-full max-w-lg translate-x-[-50%] translate-y-[-50%] gap-4 border bg-background p-6 shadow-lg duration-200 data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95 sm:rounded-lg",
        className
      )}
      {...props}
    />
  </AlertDialogPortal>
))
AlertDialogContent.displayName = AlertDialogPrimitive.Content.displayName

const AlertDialogHeader = ({
  className,
  ...props
}: React.HTMLAttributes<HTMLDivElement>) => (
  <div
    className={cn("flex flex-col space-y-2 text-center sm:text-left", className)}
    {...props}
  />
)
AlertDialogHeader.displayName = "AlertDialogHeader"

const AlertDialogFooter = ({
  className,
  ...props
}: React.HTMLAttributes<HTMLDivElement>) => (
  <div
    className={cn(
      "flex flex-col-reverse sm:flex-row sm:justify-end sm:space-x-2",
      className
    )}
    {...props}
  />
)
AlertDialogFooter.displayName = "AlertDialogFooter"

const AlertDialogTitle = React.forwardRef<
  React.ElementRef<typeof AlertDialogPrimitive.Title>,
  React.ComponentPropsWithoutRef<typeof AlertDialogPrimitive.Title>
>(({ className, ...props }, ref) => (
  <AlertDialogPrimitive.Title
    ref={ref}
    className={cn("text-lg font-semibold leading-none tracking-tight", className)}
    {...props}
  />
))
AlertDialogTitle.displayName = AlertDialogPrimitive.Title.displayName

const AlertDialogDescription = React.forwardRef<
  React.ElementRef<typeof AlertDialogPrimitive.Description>,
  React.ComponentPropsWithoutRef<typeof AlertDialogPrimitive.Description>
>(({ className, ...props }, ref) => (
  <AlertDialogPrimitive.Description
    ref={ref}
    className={cn("text-sm text-muted-foreground", className)}
    {...props}
  />
))
AlertDialogDescription.displayName = AlertDialogPrimitive.Description.displayName

const AlertDialogAction = React.forwardRef<
  React.ElementRef<typeof AlertDialogPrimitive.Action>,
  React.ComponentPropsWithoutRef<typeof AlertDialogPrimitive.Action>
>(({ className, ...props }, ref) => (
  <AlertDialogPrimitive.Action
    ref={ref}
    className={cn(buttonVariants({ variant: "destructive" }), className)}
    {...props}
  />
))
AlertDialogAction.displayName = AlertDialogPrimitive.Action.displayName

const AlertDialogCancel = React.forwardRef<
  React.ElementRef<typeof AlertDialogPrimitive.Cancel>,
  React.ComponentPropsWithoutRef<typeof AlertDialogPrimitive.Cancel>
>(({ className, ...props }, ref) => (
  <AlertDialogPrimitive.Cancel
    ref={ref}
    className={cn(buttonVariants({ variant: "outline" }), "mt-2 sm:mt-0", className)}
    {...props}
  />
))
AlertDialogCancel.displayName = AlertDialogPrimitive.Cancel.displayName

export {
  AlertDialog,
  AlertDialogPortal,
  AlertDialogOverlay,
  AlertDialogTrigger,
  AlertDialogContent,
  AlertDialogHeader,
  AlertDialogFooter,
  AlertDialogTitle,
  AlertDialogDescription,
  AlertDialogAction,
  AlertDialogCancel,
}
```

Unlike `DialogContent`, there is deliberately no `X` close button — an alert dialog requires an explicit choice. The `destructive` and `outline` variants used above both already exist in `web/src/components/ui/button.tsx:12,15`, so no new variant is needed.

- [ ] **Step 3: Wire the confirmation into the row**

In `web/src/components/TunnelList.tsx`, add local state for the pending deletion alongside the existing `editing` state at line 21:

```tsx
  const [confirmingDelete, setConfirmingDelete] = useState<Tunnel | null>(null)
```

Change the row's `onDelete` prop (line 76-79) to stage the tunnel rather than delete it:

```tsx
              onDelete={() => setConfirmingDelete(tunnel)}
```

Then render the dialog next to the existing `EditTunnelDialog` block (after line 93):

```tsx
      <AlertDialog
        open={confirmingDelete !== null}
        onOpenChange={(o) => {
          if (!o) setConfirmingDelete(null)
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              Delete {confirmingDelete?.name}?
            </AlertDialogTitle>
            <AlertDialogDescription>
              This permanently removes the tunnel and its configuration. If it
              is running it will be stopped first. This cannot be undone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                const target = confirmingDelete
                if (!target) return
                setConfirmingDelete(null)
                setBusy(target.id)
                deleteTunnel.mutate(target.id, {
                  onSettled: () => setBusy(null),
                })
              }}
            >
              Delete tunnel
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
```

Add the alert-dialog imports at the top of the file. Note the handler captures `confirmingDelete` into `target` before clearing it — reading the state variable after `setConfirmingDelete(null)` inside the same callback still sees the old value, but capturing makes the intent explicit and survives future refactors.

- [ ] **Step 4: Verify it typechecks and builds**

```bash
cd /home/cd/Work/lazytunnel/web
npm run build
```

Expected: clean.

- [ ] **Step 5: Verify the behavior in the browser**

With the backend and dev server running, confirm all four paths:

1. Click Delete → dialog appears naming the correct tunnel; the tunnel still exists behind it.
2. Click Cancel → dialog closes, tunnel still in the list.
3. Press Escape → same as Cancel.
4. Click Delete in the dialog → tunnel disappears from the list.

Also confirm focus lands on Cancel when the dialog opens, so a stray Enter keypress does not delete.

- [ ] **Step 6: Commit**

```bash
git add src/components/ui/alert-dialog.tsx src/components/TunnelList.tsx package.json package-lock.json
git commit -m "feat: confirm before deleting a tunnel

The Delete button fired the mutation directly with no confirmation and
sits next to Edit, so a misclick destroyed a tunnel. Adds a Radix
alert-dialog confirmation that defaults focus to Cancel."
```

---

### Task 9: The pure ordering functions

The heart of the feature. Two pure functions, no React and no storage, so they are fully testable in isolation. See `docs/superpowers/specs/2026-07-21-tunnel-list-drag-reorder-design.md` for the design rationale.

**Files:**
- Create: `web/src/lib/tunnelOrder.ts`
- Test: `web/src/lib/tunnelOrder.test.ts`

**Interfaces:**
- Consumes: the `Tunnel` type from `@/api/types`.
- Produces:
  - `orderTunnels(tunnels: Tunnel[], saved: string[]): Tunnel[]` — what the list renders.
  - `reconcileOrder(tunnels: Tunnel[], saved: string[]): string[] | null` — what gets persisted, or `null` when no change is needed.

- [ ] **Step 1: Write the failing tests**

Create `web/src/lib/tunnelOrder.test.ts`:

```ts
import { describe, it, expect } from 'vitest'
import { orderTunnels, reconcileOrder } from './tunnelOrder'
import type { Tunnel } from '@/api/types'

// Only id and name matter to the ordering logic; the cast keeps the
// fixtures readable rather than restating all 18 Tunnel fields.
function t(id: string, name: string): Tunnel {
  return { id, name } as Tunnel
}

describe('orderTunnels', () => {
  it('follows the saved order rather than name order', () => {
    const tunnels = [t('1', 'alpha'), t('2', 'beta'), t('3', 'gamma')]
    const result = orderTunnels(tunnels, ['3', '1', '2'])
    expect(result.map((x) => x.id)).toEqual(['3', '1', '2'])
  })

  it('puts a tunnel missing from the saved order at the top', () => {
    const tunnels = [t('1', 'alpha'), t('2', 'beta'), t('9', 'zulu')]
    const result = orderTunnels(tunnels, ['1', '2'])
    expect(result.map((x) => x.id)).toEqual(['9', '1', '2'])
  })

  it('sorts several unknown tunnels by name, not by input order', () => {
    const tunnels = [t('9', 'zulu'), t('8', 'alpha'), t('1', 'known')]
    const result = orderTunnels(tunnels, ['1'])
    expect(result.map((x) => x.id)).toEqual(['8', '9', '1'])
  })

  it('falls back to alphabetical when nothing is saved', () => {
    const tunnels = [t('3', 'gamma'), t('1', 'alpha'), t('2', 'beta')]
    const result = orderTunnels(tunnels, [])
    expect(result.map((x) => x.name)).toEqual(['alpha', 'beta', 'gamma'])
  })

  it('ignores saved ids whose tunnel no longer exists', () => {
    const tunnels = [t('1', 'alpha'), t('3', 'gamma')]
    const result = orderTunnels(tunnels, ['1', '2', '3'])
    expect(result.map((x) => x.id)).toEqual(['1', '3'])
  })

  it('produces the same output regardless of input array order', () => {
    const saved = ['2', '1', '3']
    const a = orderTunnels([t('1', 'a'), t('2', 'b'), t('3', 'c')], saved)
    const b = orderTunnels([t('3', 'c'), t('1', 'a'), t('2', 'b')], saved)
    expect(a.map((x) => x.id)).toEqual(b.map((x) => x.id))
  })
})

describe('reconcileOrder', () => {
  it('returns null when the saved order is already correct', () => {
    const tunnels = [t('1', 'alpha'), t('2', 'beta')]
    expect(reconcileOrder(tunnels, ['1', '2'])).toBeNull()
  })

  it('prepends unknown ids sorted by name', () => {
    const tunnels = [t('1', 'known'), t('9', 'zulu'), t('8', 'alpha')]
    expect(reconcileOrder(tunnels, ['1'])).toEqual(['8', '9', '1'])
  })

  it('prunes ids whose tunnel was deleted', () => {
    const tunnels = [t('1', 'alpha'), t('3', 'gamma')]
    expect(reconcileOrder(tunnels, ['1', '2', '3'])).toEqual(['1', '3'])
  })

  it('seeds alphabetically from an empty saved order', () => {
    const tunnels = [t('3', 'gamma'), t('1', 'alpha'), t('2', 'beta')]
    expect(reconcileOrder(tunnels, [])).toEqual(['1', '2', '3'])
  })

  it('is idempotent: applying its result yields null', () => {
    const tunnels = [t('1', 'alpha'), t('9', 'zulu')]
    const first = reconcileOrder(tunnels, ['1'])
    expect(first).not.toBeNull()
    expect(reconcileOrder(tunnels, first!)).toBeNull()
  })

  it('returns null for an empty list and empty saved order', () => {
    expect(reconcileOrder([], [])).toBeNull()
  })
})
```

The idempotency test is the important one — it is what proves the effect in Task 11 cannot loop.

- [ ] **Step 2: Run the tests and verify they fail**

```bash
cd /home/cd/Work/lazytunnel/web
npm run test:run
```

Expected: FAIL — cannot resolve `./tunnelOrder`.

- [ ] **Step 3: Implement the functions**

Create `web/src/lib/tunnelOrder.ts`:

```ts
import type { Tunnel } from '@/api/types'

function byName(a: Tunnel, b: Tunnel): number {
  return a.name.localeCompare(b.name)
}

/**
 * Applies a saved order to the polled tunnel list.
 *
 * Saved ids render in their saved sequence; anything the saved order has
 * never seen goes to the top, sorted by name. The name sort matters: the
 * API returns tunnels in a nondeterministic order, so any unsorted fallback
 * would let rows shuffle on every 5s poll.
 */
export function orderTunnels(tunnels: Tunnel[], saved: string[]): Tunnel[] {
  const byId = new Map(tunnels.map((t) => [t.id, t]))
  const savedSet = new Set(saved)

  const known = saved
    .map((id) => byId.get(id))
    .filter((t): t is Tunnel => t !== undefined)

  const unknown = tunnels.filter((t) => !savedSet.has(t.id)).sort(byName)

  return [...unknown, ...known]
}

/**
 * Computes the order to persist, or null when the saved order already
 * matches. Returning null is load-bearing: the caller writes to a store on
 * a non-null result, so always returning an array would loop.
 *
 * An empty saved order needs no special case — every tunnel is then
 * "unknown", so the name sort seeds the alphabetical order the list showed
 * before this feature existed.
 */
export function reconcileOrder(
  tunnels: Tunnel[],
  saved: string[]
): string[] | null {
  const byId = new Map(tunnels.map((t) => [t.id, t]))
  const savedSet = new Set(saved)

  const kept = saved.filter((id) => byId.has(id))
  const unknownIds = tunnels
    .filter((t) => !savedSet.has(t.id))
    .sort(byName)
    .map((t) => t.id)

  const next = [...unknownIds, ...kept]

  if (next.length === saved.length && next.every((id, i) => id === saved[i])) {
    return null
  }

  return next
}
```

- [ ] **Step 4: Run the tests and verify they pass**

```bash
cd /home/cd/Work/lazytunnel/web
npm run test:run
```

Expected: PASS, 13 tests across 2 suites.

- [ ] **Step 5: Commit**

```bash
git add src/lib/tunnelOrder.ts src/lib/tunnelOrder.test.ts
git commit -m "feat: add pure tunnel ordering functions

orderTunnels applies a saved id order at render; reconcileOrder
computes what to persist and returns null when unchanged, which is
what keeps the consuming effect from looping."
```

---

### Task 10: The persisted order store

**Files:**
- Create: `web/src/store/orderStore.ts`

**Interfaces:**
- Produces: `useOrderStore`, a zustand store with `order: string[]` and `setOrder(ids: string[]): void`, persisted under `lazytunnel-tunnel-order`.

- [ ] **Step 1: Create the store**

Create `web/src/store/orderStore.ts`, following the `persist` pattern already used in `web/src/store/themeStore.ts`:

```ts
import { create } from 'zustand'
import { persist } from 'zustand/middleware'

interface OrderStore {
  /** Tunnel ids in the user's chosen display order. */
  order: string[]
  setOrder: (ids: string[]) => void
}

/**
 * Holds only the id array. All ordering logic lives in lib/tunnelOrder.ts
 * so it can be tested without mocking storage.
 */
export const useOrderStore = create<OrderStore>()(
  persist(
    (set) => ({
      order: [],
      setOrder: (ids) => set({ order: ids }),
    }),
    {
      name: 'lazytunnel-tunnel-order',
    }
  )
)
```

- [ ] **Step 2: Verify it typechecks**

```bash
cd /home/cd/Work/lazytunnel/web
npm run build
```

Expected: clean. An unused-export warning is acceptable here — Task 11 consumes it.

- [ ] **Step 3: Commit**

```bash
git add src/store/orderStore.ts
git commit -m "feat: add persisted tunnel order store

Holds the user's tunnel id order in localStorage under
lazytunnel-tunnel-order, matching the existing theme and settings
store conventions."
```

---

### Task 11: Wire drag-to-reorder into the tunnel list

Replaces the hardcoded alphabetical sort at `web/src/components/TunnelList.tsx:60-61` with the saved order, and makes rows draggable by a dedicated grip handle.

**Files:**
- Modify: `web/src/components/TunnelList.tsx`

**Interfaces:**
- Consumes: `orderTunnels` / `reconcileOrder` from `@/lib/tunnelOrder` (Task 9); `useOrderStore` from `@/store/orderStore` (Task 10).
- Produces: no new exports.

- [ ] **Step 1: Install dnd-kit**

```bash
cd /home/cd/Work/lazytunnel/web
npm install @dnd-kit/core@^6.3.1 @dnd-kit/sortable@^10.0.0 @dnd-kit/modifiers
```

Peer ranges are `react: >=16.8.0`, so React 19.2 resolves without `--legacy-peer-deps`. If npm reports a peer conflict, stop and report it rather than forcing the install.

- [ ] **Step 2: Replace the sort with the saved order**

In `TunnelList.tsx`, read the store and compute the order. Add near the existing hooks (around line 20):

```tsx
  const order = useOrderStore((s) => s.order)
  const setOrder = useOrderStore((s) => s.setOrder)
  const ordered = orderTunnels(tunnels, order)
```

Then replace lines 60-62 so the map iterates `ordered` instead of the inline sort:

```tsx
        {ordered.map((tunnel) => (
```

Leave the rest of the `TunnelRow` props untouched.

- [ ] **Step 3: Add the reconciliation effect**

Add below the `ordered` computation:

```tsx
  // Persist newly-seen tunnels and drop deleted ones. Skipped in demo mode:
  // demo swaps in synthetic tunnels, so reconciling there would see every
  // real tunnel as absent and prune the user's actual order.
  useEffect(() => {
    if (isDemoMode) return
    const next = reconcileOrder(tunnels, order)
    if (next) setOrder(next)
  }, [tunnels, order, isDemoMode, setOrder])
```

This runs on every poll. `reconcileOrder` returns `null` when nothing changed, so the common case is a cheap comparison and no write.

- [ ] **Step 4: Wrap the list in a DndContext**

Add the sensors above the return:

```tsx
  const sensors = useSensors(
    // A 5px threshold so a click on the handle is never read as a drag.
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates })
  )

  function handleDragEnd(event: DragEndEvent) {
    const { active, over } = event
    if (!over || active.id === over.id) return

    const ids = ordered.map((t) => t.id)
    const oldIndex = ids.indexOf(String(active.id))
    const newIndex = ids.indexOf(String(over.id))
    if (oldIndex === -1 || newIndex === -1) return

    // Persist the full displayed order, so a drag is correct even before
    // the reconciliation effect has written a newly-arrived tunnel.
    setOrder(arrayMove(ids, oldIndex, newIndex))
  }
```

Wrap the existing `<ul>` (line 59):

```tsx
      <DndContext
        sensors={sensors}
        collisionDetection={closestCenter}
        modifiers={[restrictToVerticalAxis, restrictToParentElement]}
        onDragEnd={handleDragEnd}
      >
        <SortableContext
          items={ordered.map((t) => t.id)}
          strategy={verticalListSortingStrategy}
        >
          <ul className="divide-y divide-border border-t border-border">
            {/* rows */}
          </ul>
        </SortableContext>
      </DndContext>
```

Required imports. Note `TunnelList.tsx:1` already imports `useState` from React — merge into that existing line rather than adding a second React import:

```tsx
import { useState, useEffect } from 'react'
import {
  DndContext,
  closestCenter,
  KeyboardSensor,
  PointerSensor,
  useSensor,
  useSensors,
  type DragEndEvent,
} from '@dnd-kit/core'
import {
  SortableContext,
  sortableKeyboardCoordinates,
  verticalListSortingStrategy,
  useSortable,
  arrayMove,
} from '@dnd-kit/sortable'
import { CSS } from '@dnd-kit/utilities'
import {
  restrictToVerticalAxis,
  restrictToParentElement,
} from '@dnd-kit/modifiers'
import { GripVertical } from 'lucide-react'
import { orderTunnels, reconcileOrder } from '@/lib/tunnelOrder'
import { useOrderStore } from '@/store/orderStore'
```

`@dnd-kit/utilities` arrives as a transitive dependency of `@dnd-kit/core`; if the import fails to resolve, install it explicitly.

- [ ] **Step 5: Make the row sortable**

In `TunnelRow`, call `useSortable` and restructure the `<li>`. The `<li>` currently *is* the content flex container (lines 119-124); introduce one wrapper so the handle sits beside that container rather than inside its stacking flow:

```tsx
  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({ id: tunnel.id })

  return (
    <li
      ref={setNodeRef}
      style={{ transform: CSS.Transform.toString(transform), transition }}
      className={cn(
        'flex items-start gap-3 py-5',
        tunnel.status === 'active' && 'bg-primary/[0.03]',
        isDragging && 'relative z-10 bg-background shadow-lg'
      )}
    >
      <button
        type="button"
        {...attributes}
        {...listeners}
        aria-label={`Reorder ${tunnel.name}`}
        className="mt-0.5 shrink-0 cursor-grab touch-none rounded-sm p-1 text-muted-foreground/50 transition-colors hover:bg-muted/40 hover:text-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring active:cursor-grabbing"
      >
        <GripVertical className="h-4 w-4" />
      </button>

      <div className="flex min-w-0 flex-1 flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        {/* the original two children of the <li>, unchanged */}
      </div>
    </li>
  )
```

`py-5` and the active-row background move to the `<li>`; the wrapper takes the original flex classes plus `min-w-0 flex-1` so text truncation still works. `touch-none` on the handle is required — without it, a touch drag scrolls the page instead of reordering.

- [ ] **Step 6: Stop the endpoint link from competing with the drag**

The anchor at lines 139-147 is natively draggable in every browser. Add `draggable={false}` to it:

```tsx
            <a
              href={browseUrl}
              target="_blank"
              rel="noopener noreferrer"
              draggable={false}
```

- [ ] **Step 7: Verify it typechecks and the unit tests still pass**

```bash
cd /home/cd/Work/lazytunnel/web
npm run build && npm run test:run
```

Expected: clean build, 13 tests passing.

- [ ] **Step 8: Verify the behavior in live mode**

**Demo mode will not exercise this.** It disables the list query (`queries.ts:26`), so nothing replaces the array and a broken implementation still looks correct. Run the real backend:

```bash
export PATH=$PATH:/usr/local/go/bin
cd /home/cd/Work/lazytunnel && go run cmd/server/main.go &
cd /home/cd/Work/lazytunnel/web && npm run dev
```

Work through every case:

1. Drag a row by its handle — it moves, and the others shift to make room.
2. Wait 10+ seconds (two poll cycles) — the order holds.
3. Reload the page — the order holds.
4. Start and stop a tunnel — the order holds.
5. Create a tunnel from the CLI (`go run cmd/tunnelctl/main.go create ...`) — it appears at the top within 5s, and stays put on subsequent polls rather than re-jumping.
6. Delete a tunnel — the remaining rows keep their relative order.
7. Tab to a handle, press Space to lift, Arrow Up/Down to move, Space to drop.
8. Narrow the viewport below `sm` so rows stack — the handle stays aligned with the tunnel name and dragging still works.
9. Click the endpoint link on an active tunnel — it opens the URL and does not initiate a drag.
10. Confirm `localStorage.getItem('lazytunnel-tunnel-order')` in the devtools console holds the id array.

- [ ] **Step 9: Commit**

```bash
git add src/components/TunnelList.tsx package.json package-lock.json
git commit -m "feat: drag to reorder the tunnel list

Rows drag by a dedicated grip handle; the order persists to
localStorage and replaces the hardcoded alphabetical sort. Tunnels
the saved order has not seen land at the top, so a tunnel created
from the CLI or another browser is noticeable."
```

---

## Deferred: found and verified, deliberately not fixed

Each needs its own change; none is in scope here.

- **`tunnelctl` sends no `Authorization` header on any command** (`list.go:26`, `create.go:167`, `status.go:27`, `stop.go:26`). Harmless today because auth is disabled unless a JWT secret is configured (`cmd/server/main.go:71`), but every command 401s the moment JWT is enabled. A real fix needs token storage and a `login` subcommand — its own design.
- **`tunnelctl` status/stop/create likely share the response-shape drift** that Task 2 fixes in `list`. Not verified in detail; worth an audit pass.
- **`handleListTunnels` duplicates the response shaping** inline (`handlers.go:82-101`) instead of calling `tunnelResponse` (`responses.go:28-50`), so every new field must be added in two places.
- **`handleOpenAPI` reads `api/openapi.yaml` via a CWD-relative path** (`handlers.go:302`). Works because the systemd units set `WorkingDirectory` to the repo root; 500s under any other CWD. `embed.FS` would fix it.
- **`TunnelForm` uses `window.alert` for form and submission errors** (`TunnelForm.tsx:163,173`) rather than in-form error display.
- **`Manager.Update` refuses to modify a running tunnel** (`manager.go:186-191`). Correct for config edits, but it means any future server-side ordering could not reorder a running tunnel — relevant if the order ever moves off the browser.
- **The `idx_tunnels_owner` index is created and never used** (`sqlite.go:65`); no query filters by owner. There is no per-user scoping anywhere — any authenticated user sees and mutates every tunnel.
