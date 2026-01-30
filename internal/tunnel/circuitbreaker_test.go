package tunnel

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// TestCircuitBreakerStateString tests the String() method of CircuitBreakerState
func TestCircuitBreakerStateString(t *testing.T) {
	tests := []struct {
		state    CircuitBreakerState
		expected string
	}{
		{StateClosed, "closed"},
		{StateOpen, "open"},
		{StateHalfOpen, "half-open"},
		{CircuitBreakerState(999), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := tt.state.String()
			if result != tt.expected {
				t.Errorf("State.String() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// TestDefaultCircuitBreakerConfig tests the default configuration
func TestDefaultCircuitBreakerConfig(t *testing.T) {
	config := DefaultCircuitBreakerConfig()

	if config.MaxFailures != 5 {
		t.Errorf("MaxFailures = %v, want %v", config.MaxFailures, 5)
	}

	if config.Timeout != 30*time.Second {
		t.Errorf("Timeout = %v, want %v", config.Timeout, 30*time.Second)
	}

	if config.RecoveryTimeout != 60*time.Second {
		t.Errorf("RecoveryTimeout = %v, want %v", config.RecoveryTimeout, 60*time.Second)
	}
}

// TestNewCircuitBreaker tests the creation of a new circuit breaker
func TestNewCircuitBreaker(t *testing.T) {
	tests := []struct {
		name            string
		config          CircuitBreakerConfig
		wantMaxFailures int
		wantTimeout     time.Duration
		wantRecovery    time.Duration
		wantState       CircuitBreakerState
	}{
		{
			name:            "Default config",
			config:          CircuitBreakerConfig{},
			wantMaxFailures: 5,
			wantTimeout:     30 * time.Second,
			wantRecovery:    60 * time.Second,
			wantState:       StateClosed,
		},
		{
			name: "Custom values",
			config: CircuitBreakerConfig{
				MaxFailures:     3,
				Timeout:         10 * time.Second,
				RecoveryTimeout: 30 * time.Second,
			},
			wantMaxFailures: 3,
			wantTimeout:     10 * time.Second,
			wantRecovery:    30 * time.Second,
			wantState:       StateClosed,
		},
		{
			name: "Zero values use defaults",
			config: CircuitBreakerConfig{
				MaxFailures:     0,
				Timeout:         0,
				RecoveryTimeout: 0,
			},
			wantMaxFailures: 5,
			wantTimeout:     30 * time.Second,
			wantRecovery:    60 * time.Second,
			wantState:       StateClosed,
		},
		{
			name: "Negative values use defaults",
			config: CircuitBreakerConfig{
				MaxFailures:     -1,
				Timeout:         -5 * time.Second,
				RecoveryTimeout: -10 * time.Second,
			},
			wantMaxFailures: 5,
			wantTimeout:     30 * time.Second,
			wantRecovery:    60 * time.Second,
			wantState:       StateClosed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cb := NewCircuitBreaker(tt.config)

			if cb.maxFailures != tt.wantMaxFailures {
				t.Errorf("maxFailures = %v, want %v", cb.maxFailures, tt.wantMaxFailures)
			}

			if cb.timeout != tt.wantTimeout {
				t.Errorf("timeout = %v, want %v", cb.timeout, tt.wantTimeout)
			}

			if cb.recoveryTimeout != tt.wantRecovery {
				t.Errorf("recoveryTimeout = %v, want %v", cb.recoveryTimeout, tt.wantRecovery)
			}

			if cb.state != tt.wantState {
				t.Errorf("state = %v, want %v", cb.state, tt.wantState)
			}

			if cb.failures != 0 {
				t.Errorf("failures = %v, want 0", cb.failures)
			}
		})
	}
}

// TestCircuitBreakerStateTransitions tests state transitions
func TestCircuitBreakerStateTransitions(t *testing.T) {
	tests := []struct {
		name         string
		operations   []func(*CircuitBreaker)
		wantStates   []CircuitBreakerState
		wantFailures []int
	}{
		{
			name: "Closed to Open on failures",
			operations: []func(*CircuitBreaker){
				func(cb *CircuitBreaker) { cb.RecordFailure() },
				func(cb *CircuitBreaker) { cb.RecordFailure() },
				func(cb *CircuitBreaker) { cb.RecordFailure() },
				func(cb *CircuitBreaker) { cb.RecordFailure() },
				func(cb *CircuitBreaker) { cb.RecordFailure() },
			},
			wantStates:   []CircuitBreakerState{StateClosed, StateClosed, StateClosed, StateClosed, StateOpen},
			wantFailures: []int{1, 2, 3, 4, 0},
		},
		{
			name: "Success resets failures",
			operations: []func(*CircuitBreaker){
				func(cb *CircuitBreaker) { cb.RecordFailure() },
				func(cb *CircuitBreaker) { cb.RecordFailure() },
				func(cb *CircuitBreaker) { cb.RecordSuccess() },
				func(cb *CircuitBreaker) { cb.RecordFailure() },
			},
			wantStates:   []CircuitBreakerState{StateClosed, StateClosed, StateClosed, StateClosed},
			wantFailures: []int{1, 2, 0, 1},
		},
		{
			name: "Mixed success and failure",
			operations: []func(*CircuitBreaker){
				func(cb *CircuitBreaker) { cb.RecordFailure() },
				func(cb *CircuitBreaker) { cb.RecordSuccess() },
				func(cb *CircuitBreaker) { cb.RecordFailure() },
				func(cb *CircuitBreaker) { cb.RecordSuccess() },
				func(cb *CircuitBreaker) { cb.RecordSuccess() },
			},
			wantStates:   []CircuitBreakerState{StateClosed, StateClosed, StateClosed, StateClosed, StateClosed},
			wantFailures: []int{1, 0, 1, 0, 0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cb := NewCircuitBreaker(CircuitBreakerConfig{
				MaxFailures:     5,
				RecoveryTimeout: 100 * time.Millisecond,
			})

			for i, op := range tt.operations {
				op(cb)

				if cb.State() != tt.wantStates[i] {
					t.Errorf("Operation %d: state = %v, want %v", i, cb.State(), tt.wantStates[i])
				}

				stats := cb.Stats()
				if stats.Failures != tt.wantFailures[i] {
					t.Errorf("Operation %d: failures = %v, want %v", i, stats.Failures, tt.wantFailures[i])
				}
			}
		})
	}
}

// TestClosedToOpenTransition tests transition from closed to open state
func TestClosedToOpenTransition(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		MaxFailures:     3,
		RecoveryTimeout: 100 * time.Millisecond,
	})

	// Should start closed
	if cb.State() != StateClosed {
		t.Errorf("Initial state = %v, want closed", cb.State())
	}

	// Record failures up to the threshold
	for i := 0; i < 3; i++ {
		err := cb.Allow()
		if err != nil {
			t.Errorf("Allow() should succeed in closed state, got: %v", err)
		}
		cb.RecordFailure()
	}

	// Should now be open
	if cb.State() != StateOpen {
		t.Errorf("After max failures, state = %v, want open", cb.State())
	}

	// Should reject requests when open
	err := cb.Allow()
	if err == nil {
		t.Error("Allow() should fail when circuit is open")
	}
	if !errors.Is(err, ErrCircuitOpen) {
		t.Errorf("Error should be ErrCircuitOpen, got: %v", err)
	}
}

// TestOpenToHalfOpenTransition tests transition from open to half-open state
func TestOpenToHalfOpenTransition(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		MaxFailures:     2,
		RecoveryTimeout: 50 * time.Millisecond,
	})

	// Open the circuit
	cb.RecordFailure()
	cb.RecordFailure()

	if cb.State() != StateOpen {
		t.Fatal("Circuit should be open")
	}

	// Should still reject requests immediately after opening
	err := cb.Allow()
	if err == nil {
		t.Error("Allow() should fail immediately after opening")
	}

	// Wait for recovery timeout
	time.Sleep(75 * time.Millisecond)

	// Should now allow a test request (transition to half-open)
	err = cb.Allow()
	if err != nil {
		t.Errorf("Allow() should succeed after recovery timeout, got: %v", err)
	}

	if cb.State() != StateHalfOpen {
		t.Errorf("State should be half-open, got: %v", cb.State())
	}
}

// TestHalfOpenToClosedTransition tests transition from half-open to closed state
func TestHalfOpenToClosedTransition(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		MaxFailures:     2,
		RecoveryTimeout: 50 * time.Millisecond,
	})

	// Open the circuit
	cb.RecordFailure()
	cb.RecordFailure()

	// Wait for recovery timeout
	time.Sleep(75 * time.Millisecond)

	// Trigger transition to half-open by calling Allow
	cb.Allow()

	if cb.State() != StateHalfOpen {
		t.Fatal("Circuit should be half-open")
	}

	// Record success should close the circuit
	cb.RecordSuccess()

	if cb.State() != StateClosed {
		t.Errorf("After success in half-open, state = %v, want closed", cb.State())
	}

	// Should allow normal operations again
	err := cb.Allow()
	if err != nil {
		t.Errorf("Allow() should succeed in closed state, got: %v", err)
	}
}

// TestHalfOpenToOpenTransition tests transition from half-open back to open state
func TestHalfOpenToOpenTransition(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		MaxFailures:     2,
		RecoveryTimeout: 50 * time.Millisecond,
	})

	// Open the circuit
	cb.RecordFailure()
	cb.RecordFailure()

	// Wait for recovery timeout
	time.Sleep(75 * time.Millisecond)

	// Trigger transition to half-open
	cb.Allow()

	if cb.State() != StateHalfOpen {
		t.Fatal("Circuit should be half-open")
	}

	// Record failure should reopen the circuit
	cb.RecordFailure()

	if cb.State() != StateOpen {
		t.Errorf("After failure in half-open, state = %v, want open", cb.State())
	}

	// Should reject requests again
	err := cb.Allow()
	if err == nil {
		t.Error("Allow() should fail when circuit is reopened")
	}
}

// TestFailureCounting tests the failure counting logic
func TestFailureCounting(t *testing.T) {
	tests := []struct {
		name           string
		maxFailures    int
		operations     []string // "fail" or "success"
		wantFailures   []int
		wantFinalState CircuitBreakerState
	}{
		{
			name:           "Failures accumulate",
			maxFailures:    5,
			operations:     []string{"fail", "fail", "fail"},
			wantFailures:   []int{1, 2, 3},
			wantFinalState: StateClosed,
		},
		{
			name:           "Success resets count",
			maxFailures:    5,
			operations:     []string{"fail", "fail", "success", "fail"},
			wantFailures:   []int{1, 2, 0, 1},
			wantFinalState: StateClosed,
		},
		{
			name:           "Max failures triggers open",
			maxFailures:    3,
			operations:     []string{"fail", "fail", "fail"},
			wantFailures:   []int{1, 2, 0}, // Reset on transition to open
			wantFinalState: StateOpen,
		},
		{
			name:           "Alternating success and failure",
			maxFailures:    5,
			operations:     []string{"fail", "success", "fail", "success", "fail"},
			wantFailures:   []int{1, 0, 1, 0, 1},
			wantFinalState: StateClosed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cb := NewCircuitBreaker(CircuitBreakerConfig{
				MaxFailures:     tt.maxFailures,
				RecoveryTimeout: 100 * time.Millisecond,
			})

			for i, op := range tt.operations {
				if op == "fail" {
					cb.RecordFailure()
				} else {
					cb.RecordSuccess()
				}

				stats := cb.Stats()
				if stats.Failures != tt.wantFailures[i] {
					t.Errorf("Operation %d: failures = %v, want %v", i, stats.Failures, tt.wantFailures[i])
				}
			}

			if cb.State() != tt.wantFinalState {
				t.Errorf("Final state = %v, want %v", cb.State(), tt.wantFinalState)
			}
		})
	}
}

// TestRecoveryTimeout tests the recovery timeout behavior
func TestRecoveryTimeout(t *testing.T) {
	tests := []struct {
		name            string
		recoveryTimeout time.Duration
		waitTime        time.Duration
		shouldAllow     bool
	}{
		{
			name:            "Before recovery timeout",
			recoveryTimeout: 100 * time.Millisecond,
			waitTime:        50 * time.Millisecond,
			shouldAllow:     false,
		},
		{
			name:            "After recovery timeout",
			recoveryTimeout: 50 * time.Millisecond,
			waitTime:        75 * time.Millisecond,
			shouldAllow:     true, // Transition to half-open
		},
		{
			name:            "Exactly at recovery timeout",
			recoveryTimeout: 50 * time.Millisecond,
			waitTime:        50 * time.Millisecond,
			shouldAllow:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cb := NewCircuitBreaker(CircuitBreakerConfig{
				MaxFailures:     1,
				RecoveryTimeout: tt.recoveryTimeout,
			})

			// Open the circuit
			cb.RecordFailure()

			if cb.State() != StateOpen {
				t.Fatal("Circuit should be open")
			}

			// Wait
			time.Sleep(tt.waitTime)

			// Try to allow
			err := cb.Allow()
			allowed := err == nil

			if allowed != tt.shouldAllow {
				t.Errorf("Allow() = %v (error: %v), want %v", allowed, err, tt.shouldAllow)
			}

			if tt.shouldAllow && cb.State() != StateHalfOpen {
				t.Errorf("State should be half-open, got: %v", cb.State())
			}
		})
	}
}

// TestExecute tests the Execute method
func TestExecute(t *testing.T) {
	tests := []struct {
		name        string
		fn          func() error
		wantErr     bool
		wantErrType error
	}{
		{
			name: "Successful execution",
			fn: func() error {
				return nil
			},
			wantErr:     false,
			wantErrType: nil,
		},
		{
			name: "Failed execution",
			fn: func() error {
				return errors.New("test error")
			},
			wantErr:     true,
			wantErrType: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cb := NewCircuitBreaker(DefaultCircuitBreakerConfig())
			err := cb.Execute(tt.fn)

			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantErrType != nil && !errors.Is(err, tt.wantErrType) {
				t.Errorf("Execute() error type = %v, want %v", err, tt.wantErrType)
			}
		})
	}
}

// TestExecuteWithOpenCircuit tests Execute when circuit is open
func TestExecuteWithOpenCircuit(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		MaxFailures:     1,
		RecoveryTimeout: 1 * time.Hour, // Long recovery to keep it open
	})

	// Open the circuit
	cb.RecordFailure()

	if cb.State() != StateOpen {
		t.Fatal("Circuit should be open")
	}

	// Execute should fail immediately with circuit open error
	executed := false
	err := cb.Execute(func() error {
		executed = true
		return nil
	})

	if executed {
		t.Error("Execute function should not be called when circuit is open")
	}

	if err == nil {
		t.Error("Execute() should return error when circuit is open")
	}

	if !errors.Is(err, ErrCircuitOpen) {
		t.Errorf("Execute() error should be ErrCircuitOpen, got: %v", err)
	}
}

// TestStats tests the Stats method
func TestStats(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		MaxFailures:     3,
		RecoveryTimeout: 100 * time.Millisecond,
	})

	// Initial stats
	stats := cb.Stats()
	if stats.State != "closed" {
		t.Errorf("Initial state = %v, want closed", stats.State)
	}
	if stats.Failures != 0 {
		t.Errorf("Initial failures = %v, want 0", stats.Failures)
	}

	// Record some failures
	cb.RecordFailure()
	cb.RecordFailure()

	stats = cb.Stats()
	if stats.Failures != 2 {
		t.Errorf("After 2 failures, failures = %v, want 2", stats.Failures)
	}
	if stats.LastFailure.IsZero() {
		t.Error("LastFailure should not be zero after failures")
	}
	if stats.StateChanged.IsZero() {
		t.Error("StateChanged should not be zero")
	}

	// Open the circuit
	cb.RecordFailure()
	stats = cb.Stats()
	if stats.State != "open" {
		t.Errorf("After opening, state = %v, want open", stats.State)
	}
}

// TestConcurrentAccess tests thread safety
func TestConcurrentAccess(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		MaxFailures:     10,
		RecoveryTimeout: 50 * time.Millisecond,
	})

	var wg sync.WaitGroup
	numGoroutines := 50
	operationsPerGoroutine := 20

	// Concurrent failures
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < operationsPerGoroutine; j++ {
				cb.RecordFailure()
				time.Sleep(time.Millisecond)
			}
		}()
	}
	wg.Wait()

	// Should have recorded all failures
	stats := cb.Stats()
	if stats.Failures != numGoroutines*operationsPerGoroutine {
		// If circuit opened, failures reset, so either state is acceptable
		if cb.State() != StateOpen {
			t.Errorf("Expected all failures to be recorded or circuit to be open")
		}
	}

	// Reset and test concurrent Allow calls
	cb = NewCircuitBreaker(CircuitBreakerConfig{
		MaxFailures:     100,
		RecoveryTimeout: 1 * time.Hour,
	})

	var allowCount int
	var mu sync.Mutex

	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < operationsPerGoroutine; j++ {
				if err := cb.Allow(); err == nil {
					mu.Lock()
					allowCount++
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()

	// All Allow calls should have succeeded
	expectedAllows := numGoroutines * operationsPerGoroutine
	if allowCount != expectedAllows {
		t.Errorf("Allow() succeeded %d times, want %d", allowCount, expectedAllows)
	}
}

// TestTunnelCircuitBreaker tests the TunnelCircuitBreaker manager
func TestTunnelCircuitBreaker(t *testing.T) {
	config := DefaultCircuitBreakerConfig()
	tcb := NewTunnelCircuitBreaker(config)

	// Test GetBreaker creates new breaker
	cb1 := tcb.GetBreaker("tunnel-1")
	if cb1 == nil {
		t.Fatal("GetBreaker() returned nil")
	}

	// Test GetBreaker returns same breaker for same ID
	cb1Again := tcb.GetBreaker("tunnel-1")
	if cb1 != cb1Again {
		t.Error("GetBreaker() should return same breaker for same tunnel ID")
	}

	// Test GetBreaker creates different breaker for different ID
	cb2 := tcb.GetBreaker("tunnel-2")
	if cb1 == cb2 {
		t.Error("GetBreaker() should return different breaker for different tunnel ID")
	}

	// Test RemoveBreaker
	tcb.RemoveBreaker("tunnel-1")
	cb1New := tcb.GetBreaker("tunnel-1")
	if cb1 == cb1New {
		t.Error("After RemoveBreaker(), GetBreaker() should return new breaker")
	}

	// Test GetAllStats
	stats := tcb.GetAllStats()
	if len(stats) != 2 {
		t.Errorf("GetAllStats() returned %d breakers, want 2", len(stats))
	}

	if _, exists := stats["tunnel-2"]; !exists {
		t.Error("GetAllStats() should include tunnel-2")
	}
}

// TestTunnelCircuitBreakerConcurrent tests concurrent access to TunnelCircuitBreaker
func TestTunnelCircuitBreakerConcurrent(t *testing.T) {
	config := DefaultCircuitBreakerConfig()
	tcb := NewTunnelCircuitBreaker(config)

	var wg sync.WaitGroup
	numGoroutines := 20

	// Concurrent GetBreaker calls
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			tunnelID := "tunnel-concurrent"
			cb := tcb.GetBreaker(tunnelID)
			if cb == nil {
				t.Error("GetBreaker() returned nil")
			}
			cb.RecordFailure()
		}(i)
	}
	wg.Wait()

	// All goroutines should have used the same breaker
	stats := tcb.GetAllStats()
	if len(stats) != 1 {
		t.Errorf("Expected 1 breaker, got %d", len(stats))
	}

	tunnelStats, exists := stats["tunnel-concurrent"]
	if !exists {
		t.Fatal("tunnel-concurrent not found in stats")
	}

	// Note: Due to concurrent state transitions (closed -> open -> reset),
	// we may not get exactly numGoroutines recorded. Just ensure we got some failures.
	if tunnelStats.Failures == 0 {
		t.Errorf("Expected some failures, got 0")
	}
}

// TestEdgeCases tests various edge cases
func TestEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		testFunc func(t *testing.T)
	}{
		{
			name: "RecordSuccess in open state",
			testFunc: func(t *testing.T) {
				cb := NewCircuitBreaker(CircuitBreakerConfig{
					MaxFailures:     1,
					RecoveryTimeout: 1 * time.Hour,
				})
				cb.RecordFailure() // Opens circuit
				cb.RecordSuccess() // Should not change state
				if cb.State() != StateOpen {
					t.Errorf("State = %v, want open", cb.State())
				}
			},
		},
		{
			name: "Multiple RecordSuccess calls",
			testFunc: func(t *testing.T) {
				cb := NewCircuitBreaker(DefaultCircuitBreakerConfig())
				cb.RecordSuccess()
				cb.RecordSuccess()
				cb.RecordSuccess()
				if cb.State() != StateClosed {
					t.Errorf("State = %v, want closed", cb.State())
				}
				if cb.Stats().Failures != 0 {
					t.Errorf("Failures = %v, want 0", cb.Stats().Failures)
				}
			},
		},
		{
			name: "MaxFailures of 1",
			testFunc: func(t *testing.T) {
				cb := NewCircuitBreaker(CircuitBreakerConfig{
					MaxFailures:     1,
					RecoveryTimeout: 100 * time.Millisecond,
				})
				cb.RecordFailure()
				if cb.State() != StateOpen {
					t.Errorf("State = %v, want open", cb.State())
				}
			},
		},
		{
			name: "Very long recovery timeout",
			testFunc: func(t *testing.T) {
				cb := NewCircuitBreaker(CircuitBreakerConfig{
					MaxFailures:     1,
					RecoveryTimeout: 24 * time.Hour,
				})
				cb.RecordFailure()
				time.Sleep(10 * time.Millisecond)
				err := cb.Allow()
				if err == nil {
					t.Error("Allow() should fail with long recovery timeout")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.testFunc)
	}
}

// TestCircuitBreakerErrors tests the error variables
func TestCircuitBreakerErrors(t *testing.T) {
	if ErrCircuitOpen == nil {
		t.Error("ErrCircuitOpen should not be nil")
	}

	if ErrTimeout == nil {
		t.Error("ErrTimeout should not be nil")
	}

	// Test error messages
	if ErrCircuitOpen.Error() != "circuit breaker is open" {
		t.Errorf("ErrCircuitOpen message = %v", ErrCircuitOpen.Error())
	}

	if ErrTimeout.Error() != "circuit breaker timeout" {
		t.Errorf("ErrTimeout message = %v", ErrTimeout.Error())
	}
}
