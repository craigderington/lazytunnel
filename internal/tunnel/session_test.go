package tunnel

import (
	"context"
	"testing"
	"time"

	"github.com/craigderington/lazytunnel/pkg/types"
)

func TestNewSession(t *testing.T) {
	tests := []struct {
		name      string
		config    SessionConfig
		wantError bool
	}{
		{
			name: "valid configuration with key auth",
			config: SessionConfig{
				Hop: &types.Hop{
					Host:       "example.com",
					Port:       22,
					User:       "testuser",
					AuthMethod: types.AuthMethodKey,
					KeyID:      "/tmp/test_key", // Mock key path
				},
				KeepAlive:     30 * time.Second,
				AutoReconnect: true,
				MaxRetries:    3,
			},
			wantError: false,
		},
		{
			name: "configuration with defaults",
			config: SessionConfig{
				Hop: &types.Hop{
					Host:       "example.com",
					Port:       22,
					User:       "testuser",
					AuthMethod: types.AuthMethodKey,
					KeyID:      "/tmp/test_key",
				},
			},
			wantError: false,
		},
		{
			name: "missing hop configuration",
			config: SessionConfig{
				KeepAlive: 30 * time.Second,
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			session, err := NewSession(ctx, tt.config)

			if tt.wantError {
				if err == nil {
					t.Errorf("NewSession() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("NewSession() unexpected error: %v", err)
				return
			}

			if session == nil {
				t.Errorf("NewSession() returned nil session")
				return
			}

			// Verify defaults were applied
			if session.keepAlive == 0 {
				t.Errorf("keepAlive not set")
			}
			if session.maxRetries == 0 {
				t.Errorf("maxRetries not set")
			}
			if session.backoffConfig.Initial == 0 {
				t.Errorf("backoff config not set")
			}

			// Clean up
			session.Close()
		})
	}
}

func TestSessionStatus(t *testing.T) {
	ctx := context.Background()
	hop := &types.Hop{
		Host:       "example.com",
		Port:       22,
		User:       "testuser",
		AuthMethod: types.AuthMethodKey,
		KeyID:      "/tmp/test_key",
	}

	session, err := NewSession(ctx, SessionConfig{Hop: hop})
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	status := session.Status()

	if status.Host != hop.Host {
		t.Errorf("Status.Host = %s, want %s", status.Host, hop.Host)
	}
	if status.Port != hop.Port {
		t.Errorf("Status.Port = %d, want %d", status.Port, hop.Port)
	}
	if status.User != hop.User {
		t.Errorf("Status.User = %s, want %s", status.User, hop.User)
	}
	if status.Connected {
		t.Errorf("Status.Connected = true, want false (not connected)")
	}
}

func TestBackoffCalculation(t *testing.T) {
	config := BackoffConfig{
		Initial:    1 * time.Second,
		Max:        60 * time.Second,
		Multiplier: 2.0,
	}

	tests := []struct {
		attempt  int
		expected time.Duration
	}{
		{0, 1 * time.Second},
		{1, 2 * time.Second},
		{2, 4 * time.Second},
		{3, 8 * time.Second},
		{4, 16 * time.Second},
		{5, 32 * time.Second},
		{6, 60 * time.Second}, // Capped at max
		{7, 60 * time.Second}, // Still capped
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			backoff := config.Initial
			for i := 0; i < tt.attempt; i++ {
				backoff = time.Duration(float64(backoff) * config.Multiplier)
				if backoff > config.Max {
					backoff = config.Max
				}
			}

			if backoff != tt.expected {
				t.Errorf("attempt %d: backoff = %v, want %v", tt.attempt, backoff, tt.expected)
			}
		})
	}
}

func TestMultiHopSession(t *testing.T) {
	ctx := context.Background()

	hops := []types.Hop{
		{
			Host:       "bastion.example.com",
			Port:       22,
			User:       "deploy",
			AuthMethod: types.AuthMethodKey,
			KeyID:      "/tmp/test_key",
		},
		{
			Host:       "internal.example.com",
			Port:       22,
			User:       "admin",
			AuthMethod: types.AuthMethodKey,
			KeyID:      "/tmp/test_key",
		},
	}

	config := SessionConfig{
		KeepAlive:     30 * time.Second,
		AutoReconnect: true,
		MaxRetries:    3,
	}

	mhs, err := NewMultiHopSession(ctx, hops, config)
	if err != nil {
		t.Fatalf("Failed to create multi-hop session: %v", err)
	}
	defer mhs.Close()

	if len(mhs.hops) != len(hops) {
		t.Errorf("MultiHopSession has %d hops, want %d", len(mhs.hops), len(hops))
	}

	// Verify all sessions are created
	for i, session := range mhs.hops {
		if session == nil {
			t.Errorf("Session %d is nil", i)
		}
		if session.hop.Host != hops[i].Host {
			t.Errorf("Session %d host = %s, want %s", i, session.hop.Host, hops[i].Host)
		}
	}

	// Test status
	statuses := mhs.Status()
	if len(statuses) != len(hops) {
		t.Errorf("Got %d statuses, want %d", len(statuses), len(hops))
	}

	// Initially, no connections should be established
	if mhs.AllConnected() {
		t.Errorf("AllConnected() = true, want false (not connected)")
	}
}

func TestMultiHopSessionEmpty(t *testing.T) {
	ctx := context.Background()
	var hops []types.Hop

	config := SessionConfig{
		KeepAlive: 30 * time.Second,
	}

	_, err := NewMultiHopSession(ctx, hops, config)
	if err == nil {
		t.Errorf("NewMultiHopSession() with empty hops should return error")
	}
}

func TestSessionContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	hop := &types.Hop{
		Host:       "example.com",
		Port:       22,
		User:       "testuser",
		AuthMethod: types.AuthMethodKey,
		KeyID:      "/tmp/test_key",
	}

	session, err := NewSession(ctx, SessionConfig{Hop: hop})
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	// Cancel the context
	cancel()

	// Give it a moment to process cancellation
	time.Sleep(100 * time.Millisecond)

	// Session should respect context cancellation
	select {
	case <-session.ctx.Done():
		// Expected
	default:
		t.Errorf("Session context not cancelled")
	}

	session.Close()
}

func TestDefaultBackoffConfig(t *testing.T) {
	config := DefaultBackoffConfig()

	if config.Initial == 0 {
		t.Errorf("Default initial backoff is 0")
	}
	if config.Max == 0 {
		t.Errorf("Default max backoff is 0")
	}
	if config.Multiplier == 0 {
		t.Errorf("Default multiplier is 0")
	}

	// Verify sensible defaults
	if config.Initial > config.Max {
		t.Errorf("Initial backoff (%v) greater than max (%v)", config.Initial, config.Max)
	}
	if config.Multiplier < 1 {
		t.Errorf("Multiplier (%v) less than 1, backoff won't increase", config.Multiplier)
	}
}
