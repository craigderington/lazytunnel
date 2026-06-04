package agent

import (
	"context"
	"time"

	"github.com/rs/zerolog"

	"github.com/craigderington/lazytunnel/internal/tunnel"
	"github.com/craigderington/lazytunnel/pkg/agentclient"
	"github.com/craigderington/lazytunnel/pkg/types"
)

// Worker reconciles tunnel desired state on a data-plane agent.
type Worker struct {
	ID       string
	Client   *agentclient.Client
	Manager  *tunnel.Manager
	Logger   zerolog.Logger
	Interval time.Duration
}

func (w *Worker) Run(ctx context.Context) error {
	if _, err := w.Client.Register(types.AgentRegisterRequest{
		ID:       w.ID,
		Hostname: w.ID,
		Version:  "dev",
	}); err != nil {
		return err
	}
	w.Logger.Info().Str("agent_id", w.ID).Msg("Registered with control plane")

	ticker := time.NewTicker(w.Interval)
	defer ticker.Stop()

	for {
		if err := w.reconcile(ctx); err != nil {
			w.Logger.Warn().Err(err).Msg("Reconcile failed")
		}
		_ = w.Client.Heartbeat(w.ID)

		select {
		case <-ctx.Done():
			return w.Manager.Shutdown()
		case <-ticker.C:
		}
	}
}

func (w *Worker) reconcile(ctx context.Context) error {
	assignments, err := w.Client.Assignments(w.ID)
	if err != nil {
		return err
	}

	reports := make([]types.AgentStatusReport, 0, len(assignments))

	for _, a := range assignments {
		spec := a.Spec

		t, exists := w.managerGet(spec.ID)
		if !exists {
			_ = w.Manager.Create(ctx, &spec)
			t, _ = w.managerGet(spec.ID)
		}

		wantActive := a.DesiredStatus == types.DesiredStatusActive
		state := types.TunnelStateStopped
		if t != nil {
			if st := t.GetStatus(); st != nil {
				state = st.State
			}
		}

		switch {
		case wantActive && state != types.TunnelStateActive && state != types.TunnelStatePending:
			_ = w.Manager.Start(ctx, spec.ID)
		case !wantActive && (state == types.TunnelStateActive || state == types.TunnelStatePending):
			_ = w.Manager.Stop(ctx, spec.ID)
		}

		if t, err := w.Manager.Get(spec.ID); err == nil {
			st := t.GetStatus()
			status := "stopped"
			errMsg := ""
			if st != nil {
				status = string(st.State)
				if status == "pending" {
					status = "connecting"
				}
				errMsg = st.LastError
			}
			reports = append(reports, types.AgentStatusReport{
				TunnelID:  spec.ID,
				Status:    status,
				LastError: errMsg,
			})
		}
	}

	return w.Client.Report(w.ID, reports)
}

func (w *Worker) managerGet(id string) (*tunnel.Tunnel, bool) {
	t, err := w.Manager.Get(id)
	return t, err == nil
}