package tunnel

import (
	"context"
	"fmt"
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
		f.specs[s.ID] = normalizedCopy(s)
	}
	return f
}

// normalizedCopy deep-copies spec and rewrites an empty DesiredStatus to
// "stopped", mirroring SQLiteStore.Save (internal/storage/sqlite.go), which
// computes that substitution into a local variable used only for the SQL
// write — it never mutates spec.DesiredStatus in the caller's copy. That
// asymmetry is exactly the C1 defect: handleCreateTunnel (internal/api/
// handlers.go) never sets DesiredStatus, so the in-memory spec a live Tunnel
// holds keeps "" forever while storage always reports "stopped" for the same
// row. Storing (and returning, via List/Get) an independent copy — rather
// than mutating or aliasing the caller's pointer — is what lets a test
// reproduce that split.
func normalizedCopy(spec *types.TunnelSpec) *types.TunnelSpec {
	cp := deepCopySpec(spec)
	if cp.DesiredStatus == "" {
		cp.DesiredStatus = types.DesiredStatusStopped
	}
	return cp
}

func (f *fakeStorage) Save(ctx context.Context, spec *types.TunnelSpec) error {
	if _, exists := f.specs[spec.ID]; !exists {
		f.order = append(f.order, spec.ID)
	}
	f.specs[spec.ID] = normalizedCopy(spec)
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

// List returns a deep copy of each stored spec, exactly as SQLiteStore does —
// callers never get back the same *TunnelSpec pointer they saved. Without
// this, TestReconcileLeavesUnchangedTunnelsAlone would pass for the wrong
// reason (pointer identity, not spec equivalence), which is how the
// reflect.DeepEqual defect in an earlier version of Reconcile went undetected.
func (f *fakeStorage) List(ctx context.Context) ([]*types.TunnelSpec, error) {
	out := make([]*types.TunnelSpec, 0, len(f.order))
	for _, id := range f.order {
		if spec, ok := f.specs[id]; ok {
			out = append(out, deepCopySpec(spec))
		}
	}
	return out, nil
}

func deepCopySpec(spec *types.TunnelSpec) *types.TunnelSpec {
	cp := *spec
	if spec.Hops != nil {
		cp.Hops = make([]types.Hop, len(spec.Hops))
		copy(cp.Hops, spec.Hops)
	}
	return &cp
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

// TestReconcileTreatsCreateStyleSpecAsUnchangedAfterStorageNormalizesDesiredStatus
// is the regression guard for C1. An earlier version of Reconcile compared
// specs with reflect.DeepEqual, which is always false for a real
// round-tripped spec: handleCreateTunnel (internal/api/handlers.go) never
// sets DesiredStatus, so the in-memory value a live Tunnel holds is "",
// while SQLiteStore.Save (internal/storage/sqlite.go) always normalizes an
// empty DesiredStatus to "stopped" on write. Every reconcile therefore saw
// every live, API-created tunnel as "changed", tore it down via
// upsertTunnel, installed a fresh Tunnel carrying storage's "stopped" spec,
// and applyDesired never restarted it — the customer's forward stayed
// dropped, forever, which is the exact failure Reconcile exists to prevent.
func TestReconcileTreatsCreateStyleSpecAsUnchangedAfterStorageNormalizesDesiredStatus(t *testing.T) {
	spec := remoteSpec("id-1", "prod-db", 5432)
	spec.DesiredStatus = "" // exactly how handleCreateTunnel builds a spec

	store := newFakeStorage(spec) // normalizes to "stopped" on the way into storage
	m := managerWith(store)

	// Install the tunnel directly with the create-style, un-normalized spec —
	// standing in for what Manager.Create does in production: it stores the
	// exact *TunnelSpec it was given (DesiredStatus: "") as the live Tunnel's
	// Spec, never the normalized value that ends up in the database.
	m.mu.Lock()
	m.tunnels["id-1"] = &Tunnel{
		Spec:      spec,
		CreatedAt: spec.CreatedAt,
		ctx:       context.Background(),
		Status: &types.TunnelStatus{
			TunnelID: "id-1",
			State:    types.TunnelStateStopped,
		},
	}
	m.mu.Unlock()

	before, err := m.Get("id-1")
	if err != nil {
		t.Fatalf("tunnel id-1 missing: %v", err)
	}

	if err := m.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	after, err := m.Get("id-1")
	if err != nil {
		t.Fatalf("tunnel id-1 missing after reconcile: %v", err)
	}
	if before != after {
		t.Fatal(`a spec that only differs by DesiredStatus normalization ("" vs "stopped") must not be rebuilt`)
	}
}

// driftingStorage wraps a Storage so List and Get can diverge, modeling a
// Reconcile pass whose List snapshot has gone stale by the time it acts on
// the delta: a tunnel change that happens after List runs but before the
// later Get re-check is exactly the race C2 is about.
type driftingStorage struct {
	Storage
	hideFromList string // present via Get, absent from List: looks like it appeared after the snapshot
	hideFromGet  string // present in List, absent from Get: looks like it disappeared after the snapshot
}

func (s *driftingStorage) List(ctx context.Context) ([]*types.TunnelSpec, error) {
	specs, err := s.Storage.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*types.TunnelSpec, 0, len(specs))
	for _, spec := range specs {
		if spec.ID == s.hideFromList {
			continue
		}
		out = append(out, spec)
	}
	return out, nil
}

func (s *driftingStorage) Get(ctx context.Context, tunnelID string) (*types.TunnelSpec, error) {
	if tunnelID == s.hideFromGet {
		return nil, fmt.Errorf("not found: %s", tunnelID)
	}
	return s.Storage.Get(ctx, tunnelID)
}

// TestReconcileDoesNotDropTunnelMissingOnlyFromAStaleListSnapshot is C2's
// dangerous direction: Reconcile's store.List result, taken once at the top
// of the pass, can be stale by the time the delta is acted on. Without a
// store.Get re-check immediately before dropping, a tunnel that storage
// still has — but that this pass's stale List happened to omit — would be
// dropped, orphaning its live SSH forward.
func TestReconcileDoesNotDropTunnelMissingOnlyFromAStaleListSnapshot(t *testing.T) {
	spec := remoteSpec("id-1", "prod-db", 5432)
	store := newFakeStorage(spec)
	m := managerWith(store)

	if err := m.Reconcile(context.Background()); err != nil {
		t.Fatalf("first Reconcile returned error: %v", err)
	}
	if _, err := m.Get("id-1"); err != nil {
		t.Fatalf("expected id-1 to be adopted: %v", err)
	}

	// id-1 still exists (a fresh Get would find it), but this pass's List
	// omits it — as if it were momentarily invisible to the List query that
	// started the pass while still present a moment later.
	m.SetStorage(&driftingStorage{Storage: store, hideFromList: "id-1"})

	if err := m.Reconcile(context.Background()); err != nil {
		t.Fatalf("second Reconcile returned error: %v", err)
	}

	if _, err := m.Get("id-1"); err != nil {
		t.Fatalf("expected id-1 to survive a stale-List drop candidate via the store.Get re-check: %v", err)
	}
}

// TestReconcileDoesNotAdoptTunnelGoneByTheTimeOfTheGetRecheck is C2's mirror
// case: a tunnel deleted around the same time as a Reconcile pass can still
// appear in that pass's List result as "new" (an ID never seen in memory),
// which would resurrect it. The store.Get re-check must also guard adoption,
// not just drops.
func TestReconcileDoesNotAdoptTunnelGoneByTheTimeOfTheGetRecheck(t *testing.T) {
	spec := remoteSpec("id-1", "prod-db", 5432)
	store := newFakeStorage(spec)
	m := managerWith(store)

	// id-1 has never been reconciled into m.tunnels. This pass's List still
	// contains it, but a fresh Get reports it gone — as if it were deleted
	// between the List call and the Get re-check.
	m.SetStorage(&driftingStorage{Storage: store, hideFromGet: "id-1"})

	if err := m.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	if _, err := m.Get("id-1"); err == nil {
		t.Fatal("expected id-1 to NOT be adopted: its storage row was already gone by the time Reconcile re-checked it")
	}
}

// TestReconcileDefersActionOnPendingTunnel is the I1 mitigation guard.
// Stopping a tunnel while it is Pending (mid-connect) races with
// connectTunnel's unlocked writes to session/multiSession/forwarder
// (manager.go), which Tunnel.Stop later reads under t.mu — see the "Known
// issue" section of the task report for the root cause. Reconcile must defer
// action on a Pending tunnel rather than stop it, even when its storage row
// has disappeared; a later pass, once the tunnel has settled into Active or
// Failed, is responsible for cleaning it up.
func TestReconcileDefersActionOnPendingTunnel(t *testing.T) {
	spec := remoteSpec("id-1", "prod-db", 5432)
	store := newFakeStorage(spec)
	m := managerWith(store)

	// Install directly as Pending, bypassing a real connect, to simulate a
	// tunnel that is mid-connect when its storage row disappears.
	m.mu.Lock()
	m.tunnels["id-1"] = &Tunnel{
		Spec:      spec,
		CreatedAt: spec.CreatedAt,
		ctx:       context.Background(),
		Status: &types.TunnelStatus{
			TunnelID: "id-1",
			State:    types.TunnelStatePending,
		},
	}
	m.mu.Unlock()

	_ = store.Delete(context.Background(), "id-1")

	if err := m.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	tun, err := m.Get("id-1")
	if err != nil {
		t.Fatalf("expected the Pending tunnel to be deferred, not dropped: %v", err)
	}
	if got := tun.GetStatus().State; got != types.TunnelStatePending {
		t.Fatalf("got state %s, want pending (deferred, untouched)", got)
	}
}

// TestReconcileStopsRunningTunnelWhenDesiredBecomesStopped covers the
// applyDesired Stop branch, which no other test in this file reaches: every
// other spec carries AgentID "remote-agent" while the manager's own
// nodeAgentID is left empty, so RunOnThisNode is false and Start/Stop are
// never called. Here SetNodeAgentID("remote-agent") makes RunOnThisNode true
// for these specs, matching how a data-plane agent process is configured,
// while the tunnel itself is installed pre-connected (no real dial happens).
func TestReconcileStopsRunningTunnelWhenDesiredBecomesStopped(t *testing.T) {
	spec := remoteSpec("id-1", "prod-db", 5432) // DesiredStatus defaults to Stopped
	store := newFakeStorage(spec)
	m := managerWith(store)
	m.SetNodeAgentID("remote-agent")

	// Install directly as Active, bypassing a real SSH connect, to simulate a
	// tunnel that is currently running while storage's desired_status says it
	// should be stopped.
	m.mu.Lock()
	m.tunnels["id-1"] = &Tunnel{
		Spec:      spec,
		CreatedAt: spec.CreatedAt,
		ctx:       context.Background(),
		Status: &types.TunnelStatus{
			TunnelID: "id-1",
			State:    types.TunnelStateActive,
		},
	}
	m.mu.Unlock()

	if err := m.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	tun, err := m.Get("id-1")
	if err != nil {
		t.Fatalf("tunnel id-1 missing: %v", err)
	}
	if got := tun.GetStatus().State; got != types.TunnelStateStopped {
		t.Fatalf("got state %s, want stopped (applyDesired should have called Stop)", got)
	}
}

// TestReconcileStartsStoppedTunnelWhenDesiredBecomesActive is the mirror of
// the test above: applyDesired's Start branch. Manager.Start transitions the
// tunnel to Pending synchronously, under lock, before spawning connectTunnel
// in a goroutine; we assert that Start was reached (Pending or, if the
// background goroutine's dial has already failed by the time we check,
// Failed) without waiting on that goroutine. The hop points at a local port
// nothing listens on, so any dial resolves — refused — in well under a
// millisecond; nothing here dials a real or unroutable host.
func TestReconcileStartsStoppedTunnelWhenDesiredBecomesActive(t *testing.T) {
	spec := remoteSpec("id-1", "prod-db", 5432)
	spec.DesiredStatus = types.DesiredStatusActive
	spec.Hops[0].Host = "127.0.0.1"
	spec.Hops[0].Port = 1 // nothing listens here; dial fails (refused) immediately

	store := newFakeStorage(spec)
	m := managerWith(store)
	m.SetNodeAgentID("remote-agent")

	// Install directly as Stopped so Reconcile must bring it up to match
	// storage's desired_status=active.
	m.mu.Lock()
	m.tunnels["id-1"] = &Tunnel{
		Spec:      spec,
		CreatedAt: spec.CreatedAt,
		ctx:       context.Background(),
		Status: &types.TunnelStatus{
			TunnelID: "id-1",
			State:    types.TunnelStateStopped,
		},
	}
	m.mu.Unlock()

	if err := m.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	tun, err := m.Get("id-1")
	if err != nil {
		t.Fatalf("tunnel id-1 missing: %v", err)
	}
	got := tun.GetStatus().State
	if got != types.TunnelStatePending && got != types.TunnelStateFailed {
		t.Fatalf("got state %s, want pending or failed (either proves Start was reached)", got)
	}
}
