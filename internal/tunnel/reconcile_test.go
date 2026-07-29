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
