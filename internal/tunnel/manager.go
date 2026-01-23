package tunnel

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/craigderington/lazytunnel/pkg/types"
)

// Storage defines the interface for tunnel persistence
type Storage interface {
	Save(ctx context.Context, spec *types.TunnelSpec) error
	UpdateStatus(ctx context.Context, tunnelID, status string) error
	Delete(ctx context.Context, tunnelID string) error
	Get(ctx context.Context, tunnelID string) (*types.TunnelSpec, error)
	List(ctx context.Context) ([]*types.TunnelSpec, error)
	Close() error
}

// Manager handles the lifecycle of SSH tunnels
type Manager struct {
	tunnels map[string]*Tunnel
	mu      sync.RWMutex
	ctx     context.Context
	storage Storage // Optional persistent storage
}

// NewManager creates a new tunnel manager
func NewManager(ctx context.Context) *Manager {
	return &Manager{
		tunnels: make(map[string]*Tunnel),
		ctx:     ctx,
	}
}

// SetStorage configures persistent storage for the manager
func (m *Manager) SetStorage(storage Storage) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.storage = storage
}

// LoadFromStorage restores tunnels from persistent storage
// Stopped tunnels remain stopped, active tunnels are not auto-started
func (m *Manager) LoadFromStorage(ctx context.Context) error {
	if m.storage == nil {
		return fmt.Errorf("no storage configured")
	}

	specs, err := m.storage.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list tunnels from storage: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, spec := range specs {
		// Create tunnel in memory with stopped status
		tunnel := &Tunnel{
			Spec:      spec,
			CreatedAt: spec.CreatedAt,
			ctx:       ctx,
			Status: &types.TunnelStatus{
				TunnelID:  spec.ID,
				State:     types.TunnelStateStopped,
				LastError: "",
			},
		}

		m.tunnels[spec.ID] = tunnel
	}

	return nil
}

// Create creates and starts a new tunnel asynchronously
func (m *Manager) Create(ctx context.Context, spec *types.TunnelSpec) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.tunnels[spec.ID]; exists {
		return fmt.Errorf("tunnel %s already exists", spec.ID)
	}

	// Save to persistent storage first
	if m.storage != nil {
		if err := m.storage.Save(ctx, spec); err != nil {
			return fmt.Errorf("failed to save tunnel to storage: %w", err)
		}
	}

	// Initialize tunnel with "connecting" status
	tunnel := &Tunnel{
		Spec:      spec,
		CreatedAt: time.Now(),
		ctx:       ctx,
		Status: &types.TunnelStatus{
			TunnelID:  spec.ID,
			State:     types.TunnelStatePending,
			LastError: "",
		},
	}

	// Store the tunnel immediately
	m.tunnels[spec.ID] = tunnel

	// Start connection in background
	go m.connectTunnel(tunnel)

	return nil
}

// connectTunnel establishes the SSH connection and starts forwarding in a goroutine
func (m *Manager) connectTunnel(tunnel *Tunnel) {
	// Create and connect the tunnel
	err := m.initializeTunnel(tunnel.ctx, tunnel)
	if err != nil {
		tunnel.updateStatus(types.TunnelStateFailed, fmt.Sprintf("Failed to connect: %v", err))
		return
	}

	// Success!
	tunnel.updateStatus(types.TunnelStateActive, "")
}

// initializeTunnel establishes SSH connection and starts forwarding for an existing tunnel
func (m *Manager) initializeTunnel(ctx context.Context, tunnel *Tunnel) error {
	spec := tunnel.Spec

	// Create disconnect callback to update tunnel status
	onDisconnect := func(err error) {
		errMsg := ""
		if err != nil {
			errMsg = fmt.Sprintf("Connection lost: %v", err)
		} else {
			errMsg = "Connection lost"
		}
		tunnel.updateStatus(types.TunnelStateFailed, errMsg)
	}

	// Create reconnect callback to restore tunnel status
	onReconnect := func() {
		tunnel.updateStatus(types.TunnelStateActive, "")
	}

	// Create session configuration
	sessionConfig := SessionConfig{
		KeepAlive:     spec.KeepAlive,
		AutoReconnect: spec.AutoReconnect,
		MaxRetries:    spec.MaxRetries,
		Timeout:       10 * time.Second,
		BackoffConfig: DefaultBackoffConfig(),
		OnDisconnect:  onDisconnect,
		OnReconnect:   onReconnect,
	}

	// Create SSH session (single or multi-hop)
	var session SessionDialer

	if len(spec.Hops) == 0 {
		return fmt.Errorf("at least one hop is required")
	} else if len(spec.Hops) == 1 {
		// Single hop
		sessionConfig.Hop = &spec.Hops[0]
		singleSession, err := NewSession(ctx, sessionConfig)
		if err != nil {
			return fmt.Errorf("failed to create session: %w", err)
		}
		session = singleSession
		tunnel.session = singleSession
	} else {
		// Multi-hop
		multiSession, err := NewMultiHopSession(ctx, spec.Hops, sessionConfig)
		if err != nil {
			return fmt.Errorf("failed to create multi-hop session: %w", err)
		}
		session = multiSession
		tunnel.multiSession = multiSession
	}

	// Connect the session
	if err := tunnel.connect(); err != nil {
		return fmt.Errorf("failed to connect session: %w", err)
	}

	// Create and start forwarder based on tunnel type
	switch spec.Type {
	case types.TunnelTypeLocal:
		forwarder, err := NewLocalForwarder(ctx, spec, session)
		if err != nil {
			tunnel.cleanup()
			return fmt.Errorf("failed to create local forwarder: %w", err)
		}
		if err := forwarder.Start(); err != nil {
			tunnel.cleanup()
			return fmt.Errorf("failed to start forwarder: %w", err)
		}
		tunnel.forwarder = forwarder

	case types.TunnelTypeRemote:
		forwarder, err := NewRemoteForwarder(ctx, spec, session)
		if err != nil {
			tunnel.cleanup()
			return fmt.Errorf("failed to create remote forwarder: %w", err)
		}
		if err := forwarder.Start(); err != nil {
			tunnel.cleanup()
			return fmt.Errorf("failed to start forwarder: %w", err)
		}
		tunnel.forwarder = forwarder

	case types.TunnelTypeDynamic:
		forwarder, err := NewDynamicForwarder(ctx, spec, session)
		if err != nil {
			tunnel.cleanup()
			return fmt.Errorf("failed to create dynamic forwarder: %w", err)
		}
		if err := forwarder.Start(); err != nil {
			tunnel.cleanup()
			return fmt.Errorf("failed to start forwarder: %w", err)
		}
		tunnel.forwarder = forwarder

	default:
		tunnel.cleanup()
		return fmt.Errorf("unsupported tunnel type: %s", spec.Type)
	}

	return nil
}

// Stop stops a running tunnel but keeps it in the manager
func (m *Manager) Stop(ctx context.Context, tunnelID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	tunnel, exists := m.tunnels[tunnelID]
	if !exists {
		return fmt.Errorf("tunnel %s not found", tunnelID)
	}

	// Stop the tunnel (closes SSH session and frees ports)
	if err := tunnel.Stop(); err != nil {
		return fmt.Errorf("failed to stop tunnel: %w", err)
	}

	// Update status in persistent storage
	if m.storage != nil {
		if err := m.storage.UpdateStatus(ctx, tunnelID, "stopped"); err != nil {
			return fmt.Errorf("failed to update tunnel status in storage: %w", err)
		}
	}

	// Tunnel remains in map with "stopped" status
	return nil
}

// Delete stops and removes a tunnel from the manager
func (m *Manager) Delete(ctx context.Context, tunnelID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	tunnel, exists := m.tunnels[tunnelID]
	if !exists {
		return fmt.Errorf("tunnel %s not found", tunnelID)
	}

	// Try to stop the tunnel (may fail if already failed/stopped)
	stopErr := tunnel.Stop()

	// Remove from persistent storage
	if m.storage != nil {
		if err := m.storage.Delete(ctx, tunnelID); err != nil {
			return fmt.Errorf("failed to delete tunnel from storage: %w", err)
		}
	}

	// Always remove from active tunnels, even if Stop() failed
	// (failed tunnels need to be deletable)
	delete(m.tunnels, tunnelID)

	// Return stop error only if it was something serious
	// (but tunnel is already deleted from map)
	if stopErr != nil {
		return fmt.Errorf("tunnel removed, but stop had errors: %w", stopErr)
	}

	return nil
}

// Start starts a stopped tunnel
func (m *Manager) Start(ctx context.Context, tunnelID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	tunnel, exists := m.tunnels[tunnelID]
	if !exists {
		return fmt.Errorf("tunnel %s not found", tunnelID)
	}

	// Check current status
	status := tunnel.GetStatus()
	if status != nil && status.State == types.TunnelStateActive {
		return fmt.Errorf("tunnel is already active")
	}

	// Update to connecting state
	tunnel.updateStatus(types.TunnelStatePending, "")

	// Restart the tunnel in background
	go m.connectTunnel(tunnel)

	return nil
}

// Get retrieves a tunnel by ID
func (m *Manager) Get(tunnelID string) (*Tunnel, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	tunnel, exists := m.tunnels[tunnelID]
	if !exists {
		return nil, fmt.Errorf("tunnel %s not found", tunnelID)
	}

	return tunnel, nil
}

// List returns all tunnels
func (m *Manager) List() []*Tunnel {
	m.mu.RLock()
	defer m.mu.RUnlock()

	tunnels := make([]*Tunnel, 0, len(m.tunnels))
	for _, tunnel := range m.tunnels {
		tunnels = append(tunnels, tunnel)
	}

	return tunnels
}

// Shutdown stops all tunnels and cleans up resources
func (m *Manager) Shutdown() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var errors []error
	for id, tunnel := range m.tunnels {
		if err := tunnel.Stop(); err != nil {
			errors = append(errors, fmt.Errorf("failed to stop tunnel %s: %w", id, err))
		}
	}

	m.tunnels = make(map[string]*Tunnel)

	if len(errors) > 0 {
		return fmt.Errorf("errors during shutdown: %v", errors)
	}

	return nil
}

// Tunnel represents an active SSH tunnel
type Tunnel struct {
	Spec      *types.TunnelSpec
	Status    *types.TunnelStatus
	CreatedAt time.Time

	// Session can be either single or multi-hop
	session      *Session
	multiSession *MultiHopSession

	// Forwarder handles port forwarding
	forwarder Forwarder

	// Lifecycle
	ctx context.Context
	mu  sync.RWMutex
}

// connect establishes the SSH session
func (t *Tunnel) connect() error {
	if t.session != nil {
		return t.session.ConnectWithRetry()
	}
	if t.multiSession != nil {
		return t.multiSession.Connect()
	}
	return fmt.Errorf("no session configured")
}

// Stop stops the tunnel (idempotent - can be called multiple times)
func (t *Tunnel) Stop() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Check if already stopped
	if t.Status != nil && t.Status.State == types.TunnelStateStopped {
		return nil // Already stopped, no-op
	}

	var err error

	// Stop forwarder
	if t.forwarder != nil {
		if stopErr := t.forwarder.Stop(); stopErr != nil {
			err = stopErr
		}
		t.forwarder = nil // Clear forwarder reference
	}

	// Close session
	if closeErr := t.cleanup(); closeErr != nil && err == nil {
		err = closeErr
	}

	// Clear session references so they can be recreated on restart
	t.session = nil
	t.multiSession = nil

	// Update status
	if t.Status == nil {
		t.Status = &types.TunnelStatus{
			TunnelID: t.Spec.ID,
		}
	}
	t.Status.State = types.TunnelStateStopped
	t.Status.LastError = ""

	return err
}

// cleanup closes SSH sessions
func (t *Tunnel) cleanup() error {
	if t.session != nil {
		return t.session.Close()
	}
	if t.multiSession != nil {
		return t.multiSession.Close()
	}
	return nil
}

// updateStatus updates the tunnel status
func (t *Tunnel) updateStatus(state types.TunnelState, errorMsg string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()

	if t.Status == nil {
		t.Status = &types.TunnelStatus{
			TunnelID: t.Spec.ID,
		}
	}

	t.Status.State = state
	t.Status.LastError = errorMsg

	if state == types.TunnelStateActive && t.Status.ConnectedAt == nil {
		t.Status.ConnectedAt = &now
	}

	// Update metrics from forwarder if available
	if t.forwarder != nil {
		stats := t.forwarder.Stats()
		t.Status.BytesSent = stats.BytesSent
		t.Status.BytesReceived = stats.BytesReceived
	}
}

// GetStatus returns the current tunnel status
func (t *Tunnel) GetStatus() *types.TunnelStatus {
	t.mu.RLock()
	defer t.mu.RUnlock()

	// Update stats from forwarder
	if t.forwarder != nil {
		stats := t.forwarder.Stats()
		if t.Status != nil {
			t.Status.BytesSent = stats.BytesSent
			t.Status.BytesReceived = stats.BytesReceived
		}
	}

	// Return a copy to avoid race conditions
	if t.Status == nil {
		return nil
	}

	statusCopy := *t.Status
	return &statusCopy
}
