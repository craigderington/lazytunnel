package tunnel

import (
	"context"
	"testing"
	"time"

	"github.com/craigderington/lazytunnel/pkg/types"
)

func TestNewManager(t *testing.T) {
	ctx := context.Background()
	manager := NewManager(ctx)

	if manager == nil {
		t.Fatal("Expected manager to be created")
	}

	if len(manager.List()) != 0 {
		t.Errorf("Expected empty tunnel list, got %d tunnels", len(manager.List()))
	}
}

func TestManagerCreateGetList(t *testing.T) {
	ctx := context.Background()
	manager := NewManager(ctx)

	// Create a tunnel spec (but don't actually connect to SSH)
	// We'll use a mock for this test
	spec := &types.TunnelSpec{
		ID:            "test-tunnel-1",
		Name:          "Test Tunnel",
		Type:          types.TunnelTypeLocal,
		LocalPort:     0, // Ephemeral
		RemoteHost:    "example.com",
		RemotePort:    80,
		AutoReconnect: true,
		KeepAlive:     30 * time.Second,
		MaxRetries:    0,
		Hops: []types.Hop{
			{
				Host:       "bastion.example.com",
				Port:       22,
				User:       "testuser",
				AuthMethod: types.AuthMethodKey,
				KeyID:      "/tmp/nonexistent.key",
			},
		},
	}

	// Create tunnel - this will succeed and start connecting in background
	// The connection will fail due to invalid SSH config, but the tunnel is created
	err := manager.Create(ctx, spec)
	if err != nil {
		t.Errorf("Expected no immediate error for async tunnel creation, got: %v", err)
	}

	// Poll for up to 15 seconds waiting for the tunnel to reach failed state
	// (SSH connection timeout is 10s, so we need to wait longer than that)
	var failed bool
	for i := 0; i < 150; i++ {
		time.Sleep(100 * time.Millisecond)
		tunnels := manager.List()
		if len(tunnels) == 1 {
			status := tunnels[0].GetStatus()
			if status != nil && status.State == types.TunnelStateFailed {
				failed = true
				break
			}
		}
	}

	// Verify tunnel exists and is in failed state
	tunnels := manager.List()
	if len(tunnels) != 1 {
		t.Errorf("Expected 1 tunnel, got %d", len(tunnels))
	} else if !failed {
		status := tunnels[0].GetStatus()
		t.Errorf("Expected tunnel to be in failed state, got %s", status.State)
	}
}

func TestManagerDuplicateID(t *testing.T) {
	ctx := context.Background()
	manager := NewManager(ctx)

	spec1 := &types.TunnelSpec{
		ID:         "duplicate-id",
		Name:       "Tunnel 1",
		Type:       types.TunnelTypeLocal,
		LocalPort:  8080,
		RemoteHost: "example.com",
		RemotePort: 80,
		Hops: []types.Hop{
			{
				Host:       "bastion.example.com",
				Port:       22,
				User:       "testuser",
				AuthMethod: types.AuthMethodKey,
				KeyID:      "/tmp/test.key",
			},
		},
	}

	spec2 := &types.TunnelSpec{
		ID:         "duplicate-id", // Same ID
		Name:       "Tunnel 2",
		Type:       types.TunnelTypeLocal,
		LocalPort:  9090,
		RemoteHost: "example.com",
		RemotePort: 443,
		Hops: []types.Hop{
			{
				Host:       "bastion.example.com",
				Port:       22,
				User:       "testuser",
				AuthMethod: types.AuthMethodKey,
				KeyID:      "/tmp/test.key",
			},
		},
	}

	// First creation will fail due to SSH connection
	_ = manager.Create(ctx, spec1)

	// Manually add a tunnel with the ID to test duplicate detection
	manager.mu.Lock()
	manager.tunnels["duplicate-id"] = &Tunnel{
		Spec:      spec1,
		CreatedAt: time.Now(),
	}
	manager.mu.Unlock()

	// Second creation should fail due to duplicate ID
	err := manager.Create(ctx, spec2)
	if err == nil {
		t.Error("Expected error when creating tunnel with duplicate ID")
	}
	if err != nil && err.Error() != "tunnel duplicate-id already exists" {
		t.Errorf("Expected duplicate ID error, got: %v", err)
	}
}

func TestManagerGetNonexistent(t *testing.T) {
	ctx := context.Background()
	manager := NewManager(ctx)

	_, err := manager.Get("nonexistent")
	if err == nil {
		t.Error("Expected error when getting nonexistent tunnel")
	}
}

func TestManagerStop(t *testing.T) {
	ctx := context.Background()
	manager := NewManager(ctx)

	// Try to stop a nonexistent tunnel
	err := manager.Stop(ctx, "nonexistent")
	if err == nil {
		t.Error("Expected error when stopping nonexistent tunnel")
	}
}

func TestManagerShutdown(t *testing.T) {
	ctx := context.Background()
	manager := NewManager(ctx)

	// Add a mock tunnel
	manager.mu.Lock()
	manager.tunnels["test-tunnel"] = &Tunnel{
		Spec: &types.TunnelSpec{
			ID:   "test-tunnel",
			Type: types.TunnelTypeLocal,
		},
		CreatedAt: time.Now(),
		ctx:       ctx,
	}
	manager.mu.Unlock()

	// Shutdown should clean up all tunnels
	err := manager.Shutdown()
	if err != nil {
		t.Errorf("Unexpected error during shutdown: %v", err)
	}

	if len(manager.List()) != 0 {
		t.Errorf("Expected no tunnels after shutdown, got %d", len(manager.List()))
	}
}

func TestTunnelStatusUpdate(t *testing.T) {
	ctx := context.Background()

	tunnel := &Tunnel{
		Spec: &types.TunnelSpec{
			ID:   "test-status",
			Type: types.TunnelTypeLocal,
		},
		CreatedAt: time.Now(),
		ctx:       ctx,
	}

	// Update status to active
	tunnel.updateStatus(types.TunnelStateActive, "")

	status := tunnel.GetStatus()
	if status == nil {
		t.Fatal("Expected status to be set")
	}

	if status.State != types.TunnelStateActive {
		t.Errorf("Expected state to be active, got %s", status.State)
	}

	if status.ConnectedAt == nil {
		t.Error("Expected ConnectedAt to be set when state is active")
	}

	// Update status to failed with error
	tunnel.updateStatus(types.TunnelStateFailed, "connection lost")

	status = tunnel.GetStatus()
	if status.State != types.TunnelStateFailed {
		t.Errorf("Expected state to be failed, got %s", status.State)
	}

	if status.LastError != "connection lost" {
		t.Errorf("Expected error message, got %s", status.LastError)
	}
}
