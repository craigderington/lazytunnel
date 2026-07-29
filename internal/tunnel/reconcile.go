package tunnel

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/craigderington/lazytunnel/pkg/types"
	"github.com/rs/zerolog/log"
)

// reconcileMu serializes Reconcile calls across the process.
//
// Manager has no dedicated lock for this, and manager.go is out of scope for
// this change (see the task report's "Known issue" section), so two
// overlapping Reconcile calls — e.g. two import requests, or a future
// periodic reconcile loop racing an import — could otherwise both observe the
// same tunnel as Stopped and both call m.Start, producing two connectTunnel
// goroutines contending for one port. This package-level mutex is a stopgap
// that only serializes Reconcile against itself; it does not serialize
// against Manager.Create/Update/Delete/Start/Stop called directly, which is
// the residual risk documented in the report.
var reconcileMu sync.Mutex

// Reconcile converges the running fleet onto whatever is currently in storage.
//
// It exists because none of the obvious alternatives work on a live process:
// Manager.Update refuses to modify a running tunnel, Manager.Create always
// connects regardless of desired status, and LoadFromStorage overwrites
// m.tunnels entries outright, orphaning any live SSH goroutine.
//
// The delta is computed under RLock and the lock released before acting,
// because Start, Stop, dropTunnel and upsertTunnel all acquire m.mu themselves.
// The storage snapshot taken here can go stale by the time each action runs
// (a concurrent create/delete can land in between), so dropTunnel and
// upsertTunnel candidates are re-verified against storage immediately before
// acting, not just at snapshot time.
func (m *Manager) Reconcile(ctx context.Context) error {
	reconcileMu.Lock()
	defer reconcileMu.Unlock()

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

	// upsertCandidate.isNew distinguishes "spec changed on a tunnel we already
	// have" from "tunnel we don't have yet" — only the latter needs the
	// re-check below, per the drop/insert race described above.
	type upsertCandidate struct {
		spec  *types.TunnelSpec
		isNew bool
	}

	var (
		remove []string
		upsert []upsertCandidate
	)

	m.mu.RLock()
	for id, tun := range m.tunnels {
		if _, ok := stored[id]; !ok {
			remove = append(remove, id)
		} else if !specsEquivalent(tun.Spec, stored[id]) {
			upsert = append(upsert, upsertCandidate{spec: stored[id], isNew: false})
		}
	}
	for id, spec := range stored {
		if _, ok := m.tunnels[id]; !ok {
			upsert = append(upsert, upsertCandidate{spec: spec, isNew: true})
		}
	}
	m.mu.RUnlock()

	for _, id := range remove {
		// The snapshot above can be stale: a tunnel created concurrently with
		// this Reconcile pass (after List, before we get here) would be
		// misread as "missing from storage" and dropped, orphaning its live
		// SSH forward. Re-verify immediately before acting.
		if _, err := store.Get(ctx, id); err == nil {
			log.Info().Str("tunnel_id", id).Msg("reconcile: storage row reappeared, skipping drop")
			continue
		}
		m.dropTunnel(id)
	}

	for _, c := range upsert {
		if c.isNew {
			// Mirror case: a tunnel deleted concurrently with this pass could
			// still appear in the stale List snapshot as "new", which would
			// resurrect it. Re-verify it still exists before adopting it.
			if _, err := store.Get(ctx, c.spec.ID); err != nil {
				log.Info().Str("tunnel_id", c.spec.ID).Msg("reconcile: storage row disappeared, skipping adoption")
				continue
			}
		}
		m.upsertTunnel(ctx, c.spec)
	}

	m.applyDesired(ctx)

	return nil
}

// specsEquivalent reports whether two tunnel specs are equivalent for the
// purpose of deciding whether a live tunnel needs to be rebuilt.
//
// This is deliberately NOT reflect.DeepEqual, which is always false for a
// real round-tripped spec and would tear down and never restart every live
// tunnel on every reconcile:
//   - handleCreateTunnel (internal/api/handlers.go) never sets DesiredStatus,
//     so the in-memory value is "", while SQLiteStore.Save (internal/storage/
//     sqlite.go) always normalizes it to "stopped" on write. Both sides are
//     normalized the same way here before comparing.
//   - CreatedAt/UpdatedAt carry a monotonic reading in memory that a DB round
//     trip drops, so they never deep-equal even when nothing changed. A
//     timestamp-only difference must never bounce a live tunnel, so these two
//     fields are excluded entirely.
//   - Auth and Policy have no columns in the schema (see initSchema in
//     internal/storage/sqlite.go); they are never persisted, so they can
//     never legitimately differ as a result of a write, and are excluded.
//   - ID is the map key the caller already matched the two specs on;
//     comparing it again would be redundant.
func specsEquivalent(a, b *types.TunnelSpec) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil {
		return false
	}

	aDesired, bDesired := a.DesiredStatus, b.DesiredStatus
	if aDesired == "" {
		aDesired = types.DesiredStatusStopped
	}
	if bDesired == "" {
		bDesired = types.DesiredStatusStopped
	}

	if a.Name != b.Name ||
		a.Owner != b.Owner ||
		a.AgentID != b.AgentID ||
		a.Type != b.Type ||
		a.LocalPort != b.LocalPort ||
		a.LocalBindAddress != b.LocalBindAddress ||
		a.RemoteHost != b.RemoteHost ||
		a.RemotePort != b.RemotePort ||
		a.AutoReconnect != b.AutoReconnect ||
		a.MaxRetries != b.MaxRetries ||
		aDesired != bDesired {
		return false
	}

	// KeepAlive is persisted as whole seconds (sqlite.go stores
	// int(spec.KeepAlive.Seconds())), so sub-second precision never survives
	// a round trip. Compare at that granularity rather than by exact Duration.
	if a.KeepAlive/time.Second != b.KeepAlive/time.Second {
		return false
	}

	return hopsEquivalent(a.Hops, b.Hops)
}

// hopsEquivalent compares hop chains field by field.
func hopsEquivalent(a, b []types.Hop) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Host != b[i].Host ||
			a[i].Port != b[i].Port ||
			a[i].User != b[i].User ||
			a[i].AuthMethod != b[i].AuthMethod ||
			a[i].KeyID != b[i].KeyID ||
			a[i].HostKeyVerification != b[i].HostKeyVerification ||
			a[i].KnownHostsPath != b[i].KnownHostsPath {
			return false
		}
	}
	return true
}

// dropTunnel stops a tunnel and unregisters it without touching storage.
//
// Manager.Delete cannot be used here: it also deletes the storage row, which
// has already been removed by the time Reconcile runs, so SQLiteStore.Delete
// would fail on zero rows affected.
func (m *Manager) dropTunnel(id string) {
	m.mu.RLock()
	tun, ok := m.tunnels[id]
	m.mu.RUnlock()
	if !ok {
		return
	}

	// I1 mitigation — this narrows the race but does NOT close it; see the
	// task report's "Known issue" section for the root cause and the real
	// fix. A Pending tunnel is mid-connect: connectTunnel (manager.go) writes
	// tunnel.session / tunnel.multiSession / tunnel.forwarder with no lock,
	// while Tunnel.Stop reads them under t.mu. Stopping here is exactly what
	// triggers that race, so defer to a later reconcile pass instead, once
	// the tunnel has settled into Active or Failed.
	if status := tun.GetStatus(); status != nil && status.State == types.TunnelStatePending {
		log.Info().Str("tunnel_id", id).Msg("reconcile: deferring drop, tunnel is mid-connect")
		return
	}

	m.mu.Lock()
	// Re-confirm identity: a concurrent Create/Delete could have replaced or
	// removed this entry between the RUnlock above and this Lock.
	if current, present := m.tunnels[id]; !present || current != tun {
		m.mu.Unlock()
		return
	}
	delete(m.tunnels, id)
	breaker := m.circuitBreaker
	m.mu.Unlock()

	// A tunnel that never connected returns an error here; it is not fatal.
	if err := tun.Stop(); err != nil {
		log.Warn().Err(err).Str("tunnel_id", id).Msg("reconcile: error stopping dropped tunnel")
	}

	if breaker != nil {
		breaker.RemoveBreaker(id)
	}

	log.Info().Str("tunnel_id", id).Msg("reconcile: tunnel dropped, no longer present in storage")
}

// upsertTunnel installs a spec into the manager, stopping any live connection
// first so the swap cannot orphan a running SSH goroutine. The tunnel is
// registered stopped; applyDesired starts it if it should be running.
func (m *Manager) upsertTunnel(ctx context.Context, spec *types.TunnelSpec) {
	m.mu.RLock()
	existing, existed := m.tunnels[spec.ID]
	m.mu.RUnlock()

	if existed {
		// I1 mitigation — see dropTunnel's comment and the report's "Known
		// issue" section: narrows, does not close, the Stop/connectTunnel race.
		if status := existing.GetStatus(); status != nil && status.State == types.TunnelStatePending {
			log.Info().Str("tunnel_id", spec.ID).Msg("reconcile: deferring upsert, tunnel is mid-connect")
			return
		}
	}

	// C3: evict (never blindly replace) under the lock, Stop outside it, then
	// re-check identity before inserting the replacement. Without this, a
	// concurrent Manager.Delete between our lookup and our write could be
	// silently undone (we'd put a deleted tunnel back), or a concurrent
	// Manager.Create's tunnel could be silently clobbered while its
	// connectTunnel goroutine keeps running and holds the port.
	m.mu.Lock()
	current, stillPresent := m.tunnels[spec.ID]
	sameTunnel := stillPresent && current == existing
	if sameTunnel {
		delete(m.tunnels, spec.ID)
	}
	breaker := m.circuitBreaker
	nodeCtx := m.ctx
	m.mu.Unlock()

	if stillPresent && !sameTunnel {
		// Something else installed a different entry for this ID while we
		// were checking status above; leave it alone rather than clobber it.
		log.Info().Str("tunnel_id", spec.ID).Msg("reconcile: skipped upsert, a concurrent operation already owns this tunnel ID")
		return
	}

	if existed && sameTunnel {
		if err := existing.Stop(); err != nil {
			log.Warn().Err(err).Str("tunnel_id", spec.ID).Msg("reconcile: error stopping tunnel being replaced")
		}
		if breaker != nil {
			// I5: reset the breaker so a config fix imported via restore
			// isn't still blocked by the old spec's open breaker.
			breaker.RemoveBreaker(spec.ID)
		}
	}

	newTunnel := &Tunnel{
		Spec:      spec,
		CreatedAt: spec.CreatedAt,
		// I4: the manager's own long-lived context, not the caller's ctx
		// (which may be an HTTP request context). handleCreateTunnel makes
		// the same choice deliberately — see "not request context!" in
		// internal/api/handlers.go — so a reconciled tunnel doesn't die when
		// the request or import that triggered this Reconcile completes.
		ctx:            nodeCtx,
		statusCallback: m.statusCallback,
		Status: &types.TunnelStatus{
			TunnelID:  spec.ID,
			State:     types.TunnelStateStopped,
			LastError: "",
		},
	}

	m.mu.Lock()
	if _, present := m.tunnels[spec.ID]; present {
		m.mu.Unlock()
		log.Info().Str("tunnel_id", spec.ID).Msg("reconcile: skipped upsert, a concurrent operation already owns this tunnel ID")
		return
	}
	m.tunnels[spec.ID] = newTunnel
	m.mu.Unlock()

	log.Info().Str("tunnel_id", spec.ID).Msg("reconcile: tunnel spec upserted")
}

// desiredSnapshot captures exactly what applyDesired needs from a Tunnel
// while m.mu is held, so tun.Spec is never dereferenced after the lock is
// released (see the package doc on the locking rule, and I2 in the report).
type desiredSnapshot struct {
	id        string
	agentID   string
	desiredUp bool
	tunnel    *Tunnel
}

// applyDesired starts tunnels that should be running and stops those that
// should not, for tunnels that run on this node.
//
// Tunnel pointers (and the Spec fields we need from them) are collected under
// RLock and inspected only after releasing it, so m.mu and Tunnel.mu are
// never held at the same time. This also avoids a genuine data race: Tunnel.
// Spec fields are mutated directly and unprotected elsewhere in the codebase
// (internal/agent/coordinator.go writes spec.DesiredStatus / t.Spec.
// DesiredStatus with no lock at all), so tun.Spec must be read exactly once,
// while still under this function's own RLock, rather than dereferenced
// again later.
func (m *Manager) applyDesired(ctx context.Context) {
	m.mu.RLock()
	nodeID := m.nodeAgentID
	snapshots := make([]desiredSnapshot, 0, len(m.tunnels))
	for _, tun := range m.tunnels {
		snapshots = append(snapshots, desiredSnapshot{
			id:        tun.Spec.ID,
			agentID:   tun.Spec.AgentID,
			desiredUp: tun.Spec.DesiredStatus == types.DesiredStatusActive,
			tunnel:    tun,
		})
	}
	m.mu.RUnlock()

	for _, s := range snapshots {
		if !RunOnThisNode(nodeID, s.agentID) {
			continue
		}

		state := types.TunnelStateStopped
		if status := s.tunnel.GetStatus(); status != nil {
			state = status.State
		}

		isUp := state == types.TunnelStateActive || state == types.TunnelStatePending

		switch {
		case s.desiredUp && !isUp:
			log.Info().Str("tunnel_id", s.id).Msg("reconcile: starting tunnel to match desired state")
			if err := m.Start(ctx, s.id); err != nil {
				log.Warn().Err(err).Str("tunnel_id", s.id).Msg("reconcile: failed to start tunnel")
			}
		case !s.desiredUp && isUp:
			log.Info().Str("tunnel_id", s.id).Msg("reconcile: stopping tunnel to match desired state")
			if err := m.Stop(ctx, s.id); err != nil {
				log.Warn().Err(err).Str("tunnel_id", s.id).Msg("reconcile: failed to stop tunnel")
			}
		}
	}
}
