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
