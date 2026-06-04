package agent

import (
	"context"
	"fmt"

	"github.com/craigderington/lazytunnel/internal/tunnel"
	"github.com/craigderington/lazytunnel/pkg/types"
)

// Coordinator routes tunnel lifecycle to the API server or remote agents.
type Coordinator struct {
	manager  *tunnel.Manager
	storage  tunnel.Storage
	registry *Registry
}

func NewCoordinator(manager *tunnel.Manager, storage tunnel.Storage, registry *Registry) *Coordinator {
	return &Coordinator{manager: manager, storage: storage, registry: registry}
}

func (c *Coordinator) Start(ctx context.Context, tunnelID string) error {
	t, err := c.manager.Get(tunnelID)
	if err != nil {
		return err
	}
	spec := t.Spec

	if tunnel.IsLocalAgent(spec.AgentID) {
		return c.manager.Start(ctx, tunnelID)
	}

	if c.registry != nil && !c.registry.IsOnline(spec.AgentID) {
		return fmt.Errorf("agent %q is offline", spec.AgentID)
	}

	if c.storage != nil {
		if err := c.storage.UpdateDesiredStatus(ctx, tunnelID, types.DesiredStatusActive); err != nil {
			return err
		}
	}
	spec.DesiredStatus = types.DesiredStatusActive

	t.UpdateStatus(types.TunnelStatePending, "awaiting agent "+spec.AgentID)
	return nil
}

func (c *Coordinator) Stop(ctx context.Context, tunnelID string) error {
	t, err := c.manager.Get(tunnelID)
	if err != nil {
		return err
	}

	if tunnel.IsLocalAgent(t.Spec.AgentID) {
		return c.manager.Stop(ctx, tunnelID)
	}

	if c.storage != nil {
		if err := c.storage.UpdateDesiredStatus(ctx, tunnelID, types.DesiredStatusStopped); err != nil {
			return err
		}
	}
	t.Spec.DesiredStatus = types.DesiredStatusStopped

	if err := c.manager.Stop(ctx, tunnelID); err != nil {
		// Tunnel may only exist as delegated placeholder
		t.UpdateStatus(types.TunnelStateStopped, "")
	}
	return nil
}

// ApplyReports updates in-memory tunnel status from agent reports.
func (c *Coordinator) ApplyReports(reports []types.AgentStatusReport) {
	for _, r := range reports {
		t, err := c.manager.Get(r.TunnelID)
		if err != nil {
			continue
		}
		state := mapReportStatus(r.Status)
		t.UpdateStatus(state, r.LastError)
		if c.storage != nil {
			_ = c.storage.UpdateStatus(context.Background(), r.TunnelID, r.Status)
		}
	}
}

func mapReportStatus(s string) types.TunnelState {
	switch s {
	case "active":
		return types.TunnelStateActive
	case "connecting", "pending":
		return types.TunnelStatePending
	case "failed":
		return types.TunnelStateFailed
	default:
		return types.TunnelStateStopped
	}
}