package api

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// RateLimiter implements token bucket rate limiting per client
type RateLimiter struct {
	requestsPerSecond float64
	burstSize         int
	clients           map[string]*ClientLimiter
	mu                sync.RWMutex
	cleanupInterval   time.Duration
}

// ClientLimiter tracks rate limit state for a single client
type ClientLimiter struct {
	tokens      float64
	lastUpdated time.Time
	mu          sync.Mutex
}

// NewRateLimiter creates a new rate limiter
// requestsPerSecond: sustained rate limit (e.g., 10.0 = 10 requests per second)
// burstSize: maximum burst of requests allowed (e.g., 20)
func NewRateLimiter(requestsPerSecond float64, burstSize int) *RateLimiter {
	if requestsPerSecond <= 0 {
		requestsPerSecond = 10.0
	}
	if burstSize <= 0 {
		burstSize = 20
	}

	rl := &RateLimiter{
		requestsPerSecond: requestsPerSecond,
		burstSize:         burstSize,
		clients:           make(map[string]*ClientLimiter),
		cleanupInterval:   5 * time.Minute,
	}

	// Start cleanup goroutine
	go rl.cleanupLoop()

	return rl
}

// Allow checks if a request from the given client should be allowed
func (rl *RateLimiter) Allow(clientID string) bool {
	limiter := rl.getClientLimiter(clientID)
	return limiter.allow(rl.requestsPerSecond, rl.burstSize)
}

// getClientLimiter gets or creates a rate limiter for a client
func (rl *RateLimiter) getClientLimiter(clientID string) *ClientLimiter {
	rl.mu.RLock()
	limiter, exists := rl.clients[clientID]
	rl.mu.RUnlock()

	if exists {
		return limiter
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Double-check after acquiring write lock
	if limiter, exists := rl.clients[clientID]; exists {
		return limiter
	}

	limiter = &ClientLimiter{
		tokens:      float64(rl.burstSize),
		lastUpdated: time.Now(),
	}
	rl.clients[clientID] = limiter
	return limiter
}

// allow checks if the client can make a request (token bucket algorithm)
func (cl *ClientLimiter) allow(rate float64, burst int) bool {
	cl.mu.Lock()
	defer cl.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(cl.lastUpdated).Seconds()

	// Add tokens based on time elapsed
	cl.tokens += elapsed * rate
	if cl.tokens > float64(burst) {
		cl.tokens = float64(burst)
	}

	cl.lastUpdated = now

	// Check if we have a token available
	if cl.tokens >= 1 {
		cl.tokens--
		return true
	}

	return false
}

// cleanupLoop periodically removes stale client limiters
func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(rl.cleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		rl.cleanup()
	}
}

// cleanup removes client limiters that haven't been used recently
func (rl *RateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	cutoff := time.Now().Add(-rl.cleanupInterval)
	for clientID, limiter := range rl.clients {
		limiter.mu.Lock()
		lastUpdated := limiter.lastUpdated
		limiter.mu.Unlock()

		if lastUpdated.Before(cutoff) {
			delete(rl.clients, clientID)
		}
	}
}

// Middleware returns an HTTP middleware function for rate limiting
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clientID := rl.extractClientID(r)

		if !rl.Allow(clientID) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "Rate limit exceeded. Please try again later.",
			})
			return
		}

		next.ServeHTTP(w, r)
	})
}

// extractClientID extracts a client identifier from the request
// Uses IP address by default, or authenticated user ID if available
func (rl *RateLimiter) extractClientID(r *http.Request) string {
	// Check if user is authenticated (from auth middleware context)
	if user, ok := GetUser(r.Context()); ok {
		return user.ID
	}

	// Fall back to IP address
	ip := rl.getClientIP(r)
	return ip
}

// getClientIP extracts the client IP address from the request
func (rl *RateLimiter) getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header (for requests behind proxy)
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		// Get the first IP in the chain
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			return strings.TrimSpace(ips[0])
		}
	}

	// Check X-Real-IP header
	xri := r.Header.Get("X-Real-IP")
	if xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}
