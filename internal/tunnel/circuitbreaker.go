package tunnel

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// CircuitBreakerState represents the state of a circuit breaker
type CircuitBreakerState int

const (
	// StateClosed means the circuit is closed and requests flow through normally
	StateClosed CircuitBreakerState = iota
	// StateOpen means the circuit is open and requests fail immediately
	StateOpen
	// StateHalfOpen means the circuit is testing if the service has recovered
	StateHalfOpen
)

func (s CircuitBreakerState) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// CircuitBreaker implements the circuit breaker pattern
type CircuitBreaker struct {
	maxFailures     int
	timeout         time.Duration
	recoveryTimeout time.Duration

	failures     int
	lastFailure  time.Time
	state        CircuitBreakerState
	stateChanged time.Time

	mu sync.RWMutex
}

// CircuitBreakerConfig holds configuration for a circuit breaker
type CircuitBreakerConfig struct {
	// MaxFailures before opening the circuit (default: 5)
	MaxFailures int
	// Timeout for operations protected by the circuit breaker (default: 30s)
	Timeout time.Duration
	// RecoveryTimeout before attempting to close the circuit (default: 60s)
	RecoveryTimeout time.Duration
}

// DefaultCircuitBreakerConfig returns default configuration
func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		MaxFailures:     5,
		Timeout:         30 * time.Second,
		RecoveryTimeout: 60 * time.Second,
	}
}

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(config CircuitBreakerConfig) *CircuitBreaker {
	if config.MaxFailures <= 0 {
		config.MaxFailures = 5
	}
	if config.Timeout <= 0 {
		config.Timeout = 30 * time.Second
	}
	if config.RecoveryTimeout <= 0 {
		config.RecoveryTimeout = 60 * time.Second
	}

	return &CircuitBreaker{
		maxFailures:     config.MaxFailures,
		timeout:         config.Timeout,
		recoveryTimeout: config.RecoveryTimeout,
		state:           StateClosed,
		stateChanged:    time.Now(),
	}
}

// ErrCircuitOpen is returned when the circuit is open
var ErrCircuitOpen = errors.New("circuit breaker is open")

// ErrTimeout is returned when the operation times out
var ErrTimeout = errors.New("circuit breaker timeout")

// Allow checks if the request should be allowed
func (cb *CircuitBreaker) Allow() error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		return nil
	case StateOpen:
		// Check if recovery timeout has elapsed
		if time.Since(cb.stateChanged) > cb.recoveryTimeout {
			cb.transitionTo(StateHalfOpen)
			return nil
		}
		return fmt.Errorf("%w: circuit has been open for %v", ErrCircuitOpen, time.Since(cb.stateChanged))
	case StateHalfOpen:
		// Allow one test request
		return nil
	default:
		return errors.New("unknown circuit breaker state")
	}
}

// RecordSuccess records a successful operation
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateHalfOpen:
		// Success in half-open state closes the circuit
		cb.transitionTo(StateClosed)
		cb.failures = 0
	case StateClosed:
		// Reset failures on success in closed state
		cb.failures = 0
	}
}

// RecordFailure records a failed operation
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures++
	cb.lastFailure = time.Now()

	switch cb.state {
	case StateHalfOpen:
		// Failure in half-open state reopens the circuit
		cb.transitionTo(StateOpen)
	case StateClosed:
		// Check if we've reached the threshold
		if cb.failures >= cb.maxFailures {
			cb.transitionTo(StateOpen)
		}
	}
}

// transitionTo changes the circuit breaker state
func (cb *CircuitBreaker) transitionTo(newState CircuitBreakerState) {
	cb.state = newState
	cb.stateChanged = time.Now()

	// Reset failures when transitioning to closed or open
	if newState == StateClosed || newState == StateOpen {
		cb.failures = 0
	}
}

// State returns the current state
func (cb *CircuitBreaker) State() CircuitBreakerState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// Stats returns current statistics
func (cb *CircuitBreaker) Stats() CircuitBreakerStats {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	return CircuitBreakerStats{
		State:        cb.state.String(),
		Failures:     cb.failures,
		LastFailure:  cb.lastFailure,
		StateChanged: cb.stateChanged,
	}
}

// CircuitBreakerStats contains circuit breaker statistics
type CircuitBreakerStats struct {
	State        string    `json:"state"`
	Failures     int       `json:"failures"`
	LastFailure  time.Time `json:"last_failure"`
	StateChanged time.Time `json:"state_changed"`
}

// Execute runs the given function with circuit breaker protection
func (cb *CircuitBreaker) Execute(fn func() error) error {
	if err := cb.Allow(); err != nil {
		return err
	}

	// Execute the function
	err := fn()

	// Record result
	if err != nil {
		cb.RecordFailure()
	} else {
		cb.RecordSuccess()
	}

	return err
}

// TunnelCircuitBreaker manages circuit breakers for multiple tunnels
type TunnelCircuitBreaker struct {
	breakers map[string]*CircuitBreaker
	config   CircuitBreakerConfig
	mu       sync.RWMutex
}

// NewTunnelCircuitBreaker creates a new circuit breaker manager for tunnels
func NewTunnelCircuitBreaker(config CircuitBreakerConfig) *TunnelCircuitBreaker {
	return &TunnelCircuitBreaker{
		breakers: make(map[string]*CircuitBreaker),
		config:   config,
	}
}

// GetBreaker gets or creates a circuit breaker for a tunnel
func (tcb *TunnelCircuitBreaker) GetBreaker(tunnelID string) *CircuitBreaker {
	tcb.mu.RLock()
	cb, exists := tcb.breakers[tunnelID]
	tcb.mu.RUnlock()

	if exists {
		return cb
	}

	tcb.mu.Lock()
	defer tcb.mu.Unlock()

	// Double-check after acquiring write lock
	if cb, exists := tcb.breakers[tunnelID]; exists {
		return cb
	}

	cb = NewCircuitBreaker(tcb.config)
	tcb.breakers[tunnelID] = cb
	return cb
}

// RemoveBreaker removes a circuit breaker for a tunnel
func (tcb *TunnelCircuitBreaker) RemoveBreaker(tunnelID string) {
	tcb.mu.Lock()
	defer tcb.mu.Unlock()
	delete(tcb.breakers, tunnelID)
}

// GetAllStats returns stats for all circuit breakers
func (tcb *TunnelCircuitBreaker) GetAllStats() map[string]CircuitBreakerStats {
	tcb.mu.RLock()
	defer tcb.mu.RUnlock()

	stats := make(map[string]CircuitBreakerStats)
	for id, cb := range tcb.breakers {
		stats[id] = cb.Stats()
	}
	return stats
}
