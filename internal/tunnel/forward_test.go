package tunnel

import (
	"context"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/craigderington/lazytunnel/pkg/types"
)

// MockSessionDialer is a mock implementation for testing
type MockSessionDialer struct {
	connected bool
	dialFunc  func(network, address string) (net.Conn, error)
}

func (m *MockSessionDialer) Dial(network, address string) (net.Conn, error) {
	if m.dialFunc != nil {
		return m.dialFunc(network, address)
	}
	return nil, fmt.Errorf("no dial function configured")
}

func (m *MockSessionDialer) IsConnected() bool {
	return m.connected
}

func TestNewLocalForwarder(t *testing.T) {
	tests := []struct {
		name    string
		spec    *types.TunnelSpec
		wantErr bool
	}{
		{
			name: "valid local tunnel spec",
			spec: &types.TunnelSpec{
				ID:         "test-1",
				Type:       types.TunnelTypeLocal,
				LocalPort:  8080,
				RemoteHost: "example.com",
				RemotePort: 80,
			},
			wantErr: false,
		},
		{
			name: "invalid tunnel type",
			spec: &types.TunnelSpec{
				ID:   "test-2",
				Type: types.TunnelTypeRemote,
			},
			wantErr: true,
		},
		{
			name: "ephemeral local port (port 0)",
			spec: &types.TunnelSpec{
				ID:         "test-3",
				Type:       types.TunnelTypeLocal,
				LocalPort:  0, // Ephemeral port is allowed
				RemoteHost: "example.com",
				RemotePort: 80,
			},
			wantErr: false,
		},
		{
			name: "missing remote host",
			spec: &types.TunnelSpec{
				ID:         "test-4",
				Type:       types.TunnelTypeLocal,
				LocalPort:  8080,
				RemotePort: 80,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			mockSession := &MockSessionDialer{connected: true}

			_, err := NewLocalForwarder(ctx, tt.spec, mockSession)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewLocalForwarder() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestLocalForwarderStartStop(t *testing.T) {
	ctx := context.Background()
	spec := &types.TunnelSpec{
		ID:         "test-start-stop",
		Type:       types.TunnelTypeLocal,
		LocalPort:  0, // Use port 0 to get an ephemeral port
		RemoteHost: "example.com",
		RemotePort: 80,
	}

	mockSession := &MockSessionDialer{connected: true}
	forwarder, err := NewLocalForwarder(ctx, spec, mockSession)
	if err != nil {
		t.Fatalf("Failed to create forwarder: %v", err)
	}

	// Start the forwarder
	if err := forwarder.Start(); err != nil {
		t.Fatalf("Failed to start forwarder: %v", err)
	}

	// Verify listener is created
	if forwarder.LocalAddr() == "" {
		t.Error("Expected local address to be set")
	}

	// Try to start again (should fail)
	if err := forwarder.Start(); err == nil {
		t.Error("Expected error when starting already-started forwarder")
	}

	// Stop the forwarder
	if err := forwarder.Stop(); err != nil {
		t.Errorf("Failed to stop forwarder: %v", err)
	}

	// Verify listener is closed
	time.Sleep(100 * time.Millisecond)
	if forwarder.LocalAddr() != "" {
		t.Error("Expected local address to be empty after stop")
	}
}

func TestLocalForwarderConnection(t *testing.T) {
	ctx := context.Background()

	// Create a mock remote server
	remoteServer, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create remote server: %v", err)
	}
	defer remoteServer.Close()

	remoteAddr := remoteServer.Addr().String()

	// Handle one connection on the remote server
	go func() {
		conn, err := remoteServer.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Echo server - read all, then write back
		buf := make([]byte, 4096)
		n, err := conn.Read(buf)
		if err != nil && err != io.EOF {
			return
		}
		conn.Write(buf[:n])
	}()

	// Create mock session that dials to our mock remote server
	mockSession := &MockSessionDialer{
		connected: true,
		dialFunc: func(network, address string) (net.Conn, error) {
			return net.Dial(network, remoteAddr)
		},
	}

	// Create forwarder with ephemeral port
	spec := &types.TunnelSpec{
		ID:         "test-connection",
		Type:       types.TunnelTypeLocal,
		LocalPort:  0,
		RemoteHost: "example.com", // Mock session overrides this
		RemotePort: 80,            // Mock session overrides this
	}

	forwarder, err := NewLocalForwarder(ctx, spec, mockSession)
	if err != nil {
		t.Fatalf("Failed to create forwarder: %v", err)
	}

	if err := forwarder.Start(); err != nil {
		t.Fatalf("Failed to start forwarder: %v", err)
	}
	defer forwarder.Stop()

	// Connect to the forwarder
	localAddr := forwarder.LocalAddr()
	conn, err := net.Dial("tcp", localAddr)
	if err != nil {
		t.Fatalf("Failed to connect to forwarder: %v", err)
	}
	defer conn.Close()

	// Send test data
	testData := []byte("hello world")
	if _, err := conn.Write(testData); err != nil {
		t.Fatalf("Failed to write to connection: %v", err)
	}

	// Close write side to signal EOF
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		tcpConn.CloseWrite()
	}

	// Read echo response
	buf := make([]byte, len(testData))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("Failed to read from connection: %v", err)
	}

	if string(buf) != string(testData) {
		t.Errorf("Expected %q, got %q", testData, buf)
	}

	// Check stats
	time.Sleep(100 * time.Millisecond)
	stats := forwarder.Stats()
	if stats.Connections == 0 {
		t.Error("Expected at least one connection")
	}
	if stats.BytesSent == 0 || stats.BytesReceived == 0 {
		t.Errorf("Expected bytes transferred, got sent=%d received=%d",
			stats.BytesSent, stats.BytesReceived)
	}
}

func TestLocalForwarderStats(t *testing.T) {
	ctx := context.Background()
	spec := &types.TunnelSpec{
		ID:         "test-stats",
		Type:       types.TunnelTypeLocal,
		LocalPort:  0,
		RemoteHost: "example.com",
		RemotePort: 80,
	}

	mockSession := &MockSessionDialer{connected: true}
	forwarder, err := NewLocalForwarder(ctx, spec, mockSession)
	if err != nil {
		t.Fatalf("Failed to create forwarder: %v", err)
	}

	// Check initial stats
	stats := forwarder.Stats()
	if stats.Connections != 0 {
		t.Errorf("Expected 0 connections, got %d", stats.Connections)
	}
	if stats.ActiveConns != 0 {
		t.Errorf("Expected 0 active connections, got %d", stats.ActiveConns)
	}
	if stats.StartedAt.IsZero() {
		t.Error("Expected StartedAt to be set")
	}
}

func TestNewRemoteForwarder(t *testing.T) {
	tests := []struct {
		name    string
		spec    *types.TunnelSpec
		wantErr bool
	}{
		{
			name: "valid remote tunnel spec",
			spec: &types.TunnelSpec{
				ID:         "test-1",
				Type:       types.TunnelTypeRemote,
				LocalPort:  8080,
				RemoteHost: "example.com",
				RemotePort: 8080,
			},
			wantErr: false,
		},
		{
			name: "invalid tunnel type",
			spec: &types.TunnelSpec{
				ID:   "test-2",
				Type: types.TunnelTypeLocal,
			},
			wantErr: true,
		},
		{
			name: "missing local port",
			spec: &types.TunnelSpec{
				ID:         "test-3",
				Type:       types.TunnelTypeRemote,
				RemotePort: 8080,
			},
			wantErr: true,
		},
		{
			name: "missing remote port",
			spec: &types.TunnelSpec{
				ID:        "test-4",
				Type:      types.TunnelTypeRemote,
				LocalPort: 8080,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			mockSession := &MockSessionDialer{connected: true}

			_, err := NewRemoteForwarder(ctx, tt.spec, mockSession)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewRemoteForwarder() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRemoteForwarderStats(t *testing.T) {
	ctx := context.Background()
	spec := &types.TunnelSpec{
		ID:         "test-remote-stats",
		Type:       types.TunnelTypeRemote,
		LocalPort:  8080,
		RemotePort: 9090,
	}

	mockSession := &MockSessionDialer{connected: true}
	forwarder, err := NewRemoteForwarder(ctx, spec, mockSession)
	if err != nil {
		t.Fatalf("Failed to create remote forwarder: %v", err)
	}

	// Check initial stats
	stats := forwarder.Stats()
	if stats.Connections != 0 {
		t.Errorf("Expected 0 connections, got %d", stats.Connections)
	}
	if stats.ActiveConns != 0 {
		t.Errorf("Expected 0 active connections, got %d", stats.ActiveConns)
	}
	if stats.StartedAt.IsZero() {
		t.Error("Expected StartedAt to be set")
	}
}

func TestNewDynamicForwarder(t *testing.T) {
	tests := []struct {
		name    string
		spec    *types.TunnelSpec
		wantErr bool
	}{
		{
			name: "valid dynamic tunnel spec",
			spec: &types.TunnelSpec{
				ID:        "test-1",
				Type:      types.TunnelTypeDynamic,
				LocalPort: 1080,
			},
			wantErr: false,
		},
		{
			name: "invalid tunnel type",
			spec: &types.TunnelSpec{
				ID:   "test-2",
				Type: types.TunnelTypeLocal,
			},
			wantErr: true,
		},
		{
			name: "ephemeral port (port 0)",
			spec: &types.TunnelSpec{
				ID:        "test-3",
				Type:      types.TunnelTypeDynamic,
				LocalPort: 0, // Ephemeral port is allowed
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			mockSession := &MockSessionDialer{connected: true}

			_, err := NewDynamicForwarder(ctx, tt.spec, mockSession)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewDynamicForwarder() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDynamicForwarderStats(t *testing.T) {
	ctx := context.Background()
	spec := &types.TunnelSpec{
		ID:        "test-dynamic-stats",
		Type:      types.TunnelTypeDynamic,
		LocalPort: 0,
	}

	mockSession := &MockSessionDialer{connected: true}
	forwarder, err := NewDynamicForwarder(ctx, spec, mockSession)
	if err != nil {
		t.Fatalf("Failed to create dynamic forwarder: %v", err)
	}

	// Check initial stats
	stats := forwarder.Stats()
	if stats.Connections != 0 {
		t.Errorf("Expected 0 connections, got %d", stats.Connections)
	}
	if stats.ActiveConns != 0 {
		t.Errorf("Expected 0 active connections, got %d", stats.ActiveConns)
	}
	if stats.StartedAt.IsZero() {
		t.Error("Expected StartedAt to be set")
	}
}

func TestDynamicForwarderStartStop(t *testing.T) {
	ctx := context.Background()
	spec := &types.TunnelSpec{
		ID:        "test-dynamic-lifecycle",
		Type:      types.TunnelTypeDynamic,
		LocalPort: 0, // Use ephemeral port
	}

	mockSession := &MockSessionDialer{connected: true}
	forwarder, err := NewDynamicForwarder(ctx, spec, mockSession)
	if err != nil {
		t.Fatalf("Failed to create dynamic forwarder: %v", err)
	}

	// Start the forwarder
	if err := forwarder.Start(); err != nil {
		t.Fatalf("Failed to start forwarder: %v", err)
	}

	// Verify listener is created
	if forwarder.LocalAddr() == "" {
		t.Error("Expected local address to be set")
	}

	// Try to start again (should fail)
	if err := forwarder.Start(); err == nil {
		t.Error("Expected error when starting already-started forwarder")
	}

	// Stop the forwarder
	if err := forwarder.Stop(); err != nil {
		t.Errorf("Failed to stop forwarder: %v", err)
	}

	// Verify listener is closed
	time.Sleep(100 * time.Millisecond)
	if forwarder.LocalAddr() != "" {
		t.Error("Expected local address to be empty after stop")
	}
}
