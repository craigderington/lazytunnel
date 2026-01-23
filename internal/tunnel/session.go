package tunnel

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	"github.com/craigderington/lazytunnel/pkg/types"
)

// expandPath expands ~ to the user's home directory
func expandPath(path string) (string, error) {
	if path == "" {
		return path, nil
	}

	// If path starts with ~/, expand to home directory
	if strings.HasPrefix(path, "~/") {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get home directory: %w", err)
		}
		return filepath.Join(homeDir, path[2:]), nil
	}

	// If path is just ~, return home directory
	if path == "~" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get home directory: %w", err)
		}
		return homeDir, nil
	}

	// Otherwise, return as-is
	return path, nil
}

// Session represents an SSH session with connection management
type Session struct {
	hop    *types.Hop
	client *ssh.Client
	config *ssh.ClientConfig

	// Connection state
	connected    bool
	lastError    error
	retryCount   int
	connectedAt  *time.Time
	mu           sync.RWMutex

	// Keep-alive
	keepAlive     time.Duration
	stopKeepAlive chan struct{}

	// Auto-reconnect
	autoReconnect bool
	maxRetries    int
	backoffConfig BackoffConfig

	// Callbacks
	onDisconnect DisconnectCallback
	onReconnect  ReconnectCallback

	// Context for cancellation
	ctx    context.Context
	cancel context.CancelFunc
}

// BackoffConfig defines exponential backoff parameters
type BackoffConfig struct {
	Initial    time.Duration
	Max        time.Duration
	Multiplier float64
}

// DefaultBackoffConfig returns default backoff configuration
func DefaultBackoffConfig() BackoffConfig {
	return BackoffConfig{
		Initial:    1 * time.Second,
		Max:        60 * time.Second,
		Multiplier: 2.0,
	}
}

// DisconnectCallback is called when a session disconnects
type DisconnectCallback func(err error)

// ReconnectCallback is called when a session successfully reconnects
type ReconnectCallback func()

// SessionConfig contains configuration for creating an SSH session
type SessionConfig struct {
	Hop           *types.Hop
	KeepAlive     time.Duration
	AutoReconnect bool
	MaxRetries    int
	Timeout       time.Duration
	BackoffConfig BackoffConfig
	OnDisconnect  DisconnectCallback // Called when connection is lost
	OnReconnect   ReconnectCallback  // Called when reconnection succeeds
}

// NewSession creates a new SSH session
func NewSession(ctx context.Context, config SessionConfig) (*Session, error) {
	if config.Hop == nil {
		return nil, fmt.Errorf("hop configuration is required")
	}

	// Set defaults
	if config.KeepAlive == 0 {
		config.KeepAlive = 30 * time.Second
	}
	if config.MaxRetries == 0 {
		config.MaxRetries = 3
	}
	if config.Timeout == 0 {
		config.Timeout = 10 * time.Second
	}
	if config.BackoffConfig.Initial == 0 {
		config.BackoffConfig = DefaultBackoffConfig()
	}

	sessionCtx, cancel := context.WithCancel(ctx)

	session := &Session{
		hop:           config.Hop,
		keepAlive:     config.KeepAlive,
		autoReconnect: config.AutoReconnect,
		maxRetries:    config.MaxRetries,
		backoffConfig: config.BackoffConfig,
		onDisconnect:  config.OnDisconnect,
		onReconnect:   config.OnReconnect,
		stopKeepAlive: make(chan struct{}),
		ctx:           sessionCtx,
		cancel:        cancel,
	}

	// Note: SSH client config is built lazily when Connect() is called
	// This allows session creation without immediate authentication

	return session, nil
}

// Connect establishes the SSH connection
func (s *Session) Connect() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.connected {
		return nil
	}

	// Build SSH client config if not already built
	if s.config == nil {
		config, err := s.buildSSHConfig(10 * time.Second) // Default timeout
		if err != nil {
			s.lastError = fmt.Errorf("failed to build SSH config: %w", err)
			return s.lastError
		}
		s.config = config
	}

	addr := fmt.Sprintf("%s:%d", s.hop.Host, s.hop.Port)
	client, err := ssh.Dial("tcp", addr, s.config)
	if err != nil {
		s.lastError = fmt.Errorf("failed to connect to %s: %w", addr, err)
		return s.lastError
	}

	s.client = client
	s.connected = true
	now := time.Now()
	s.connectedAt = &now
	s.retryCount = 0
	s.lastError = nil

	// Start keep-alive
	go s.keepAliveLoop()

	return nil
}

// connectOverConn establishes an SSH connection over an existing net.Conn
// This is used for multi-hop tunneling where we tunnel through a previous SSH session
func (s *Session) connectOverConn(conn net.Conn) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.connected {
		return nil
	}

	// Build SSH client config if not already built
	if s.config == nil {
		config, err := s.buildSSHConfig(10 * time.Second)
		if err != nil {
			s.lastError = fmt.Errorf("failed to build SSH config: %w", err)
			return s.lastError
		}
		s.config = config
	}

	// Create SSH client connection over the existing conn
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, s.hop.Host, s.config)
	if err != nil {
		s.lastError = fmt.Errorf("failed to establish SSH over connection: %w", err)
		return s.lastError
	}

	s.client = ssh.NewClient(sshConn, chans, reqs)
	s.connected = true
	now := time.Now()
	s.connectedAt = &now
	s.retryCount = 0
	s.lastError = nil

	// Start keep-alive
	go s.keepAliveLoop()

	return nil
}

// Disconnect closes the SSH connection
func (s *Session) Disconnect() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.connected {
		return nil
	}

	// Stop keep-alive
	close(s.stopKeepAlive)

	if s.client != nil {
		if err := s.client.Close(); err != nil {
			return fmt.Errorf("failed to close SSH client: %w", err)
		}
	}

	s.connected = false
	s.client = nil
	s.connectedAt = nil

	return nil
}

// Close closes the session and cancels the context
func (s *Session) Close() error {
	s.cancel()
	return s.Disconnect()
}

// IsConnected returns whether the session is currently connected
func (s *Session) IsConnected() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.connected
}

// Client returns the underlying SSH client (thread-safe)
func (s *Session) Client() *ssh.Client {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.client
}

// Dial creates a connection through this SSH session
func (s *Session) Dial(network, address string) (net.Conn, error) {
	client := s.Client()
	if client == nil {
		return nil, fmt.Errorf("session not connected")
	}

	return client.Dial(network, address)
}

// ConnectWithRetry connects with automatic retry logic
func (s *Session) ConnectWithRetry() error {
	backoff := s.backoffConfig.Initial

	for attempt := 0; attempt <= s.maxRetries; attempt++ {
		select {
		case <-s.ctx.Done():
			return s.ctx.Err()
		default:
		}

		err := s.Connect()
		if err == nil {
			return nil
		}

		s.mu.Lock()
		s.retryCount = attempt + 1
		s.mu.Unlock()

		if attempt < s.maxRetries {
			select {
			case <-time.After(backoff):
				// Calculate next backoff
				backoff = time.Duration(float64(backoff) * s.backoffConfig.Multiplier)
				if backoff > s.backoffConfig.Max {
					backoff = s.backoffConfig.Max
				}
			case <-s.ctx.Done():
				return s.ctx.Err()
			}
		}
	}

	return fmt.Errorf("failed to connect after %d attempts: %w", s.maxRetries+1, s.lastError)
}

// buildSSHConfig builds an ssh.ClientConfig based on the hop configuration
func (s *Session) buildSSHConfig(timeout time.Duration) (*ssh.ClientConfig, error) {
	config := &ssh.ClientConfig{
		User:            s.hop.User,
		Timeout:         timeout,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // TODO: Implement proper host key verification
	}

	// Configure authentication based on method
	switch s.hop.AuthMethod {
	case types.AuthMethodKey:
		auth, err := s.keyAuth()
		if err != nil {
			return nil, fmt.Errorf("key authentication failed: %w", err)
		}
		config.Auth = []ssh.AuthMethod{auth}

	case types.AuthMethodPassword:
		return nil, fmt.Errorf("password authentication not yet implemented")

	case types.AuthMethodAgent:
		auth, err := s.agentAuth()
		if err != nil {
			return nil, fmt.Errorf("agent authentication failed: %w", err)
		}
		config.Auth = []ssh.AuthMethod{auth}

	case types.AuthMethodCert:
		return nil, fmt.Errorf("certificate authentication not yet implemented")

	default:
		return nil, fmt.Errorf("unsupported auth method: %s", s.hop.AuthMethod)
	}

	return config, nil
}

// keyAuth creates SSH public key authentication
func (s *Session) keyAuth() (ssh.AuthMethod, error) {
	if s.hop.KeyID == "" {
		return nil, fmt.Errorf("key_id is required for key authentication")
	}

	// Expand ~ to home directory
	expandedPath, err := expandPath(s.hop.KeyID)
	if err != nil {
		return nil, fmt.Errorf("failed to expand key path: %w", err)
	}

	// TODO: Integrate with KMS to retrieve private key
	// For now, load from filesystem (development only)
	key, err := os.ReadFile(expandedPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read private key from %s: %w", expandedPath, err)
	}

	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	return ssh.PublicKeys(signer), nil
}

// agentAuth creates SSH agent authentication
func (s *Session) agentAuth() (ssh.AuthMethod, error) {
	socket := os.Getenv("SSH_AUTH_SOCK")
	if socket == "" {
		return nil, fmt.Errorf("SSH_AUTH_SOCK not set")
	}

	conn, err := net.Dial("unix", socket)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to SSH agent: %w", err)
	}

	agentClient := agent.NewClient(conn)
	return ssh.PublicKeysCallback(agentClient.Signers), nil
}

// keepAliveLoop sends periodic keep-alive packets
func (s *Session) keepAliveLoop() {
	ticker := time.NewTicker(s.keepAlive)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := s.sendKeepAlive(); err != nil {
				// Notify listeners about disconnection
				if s.onDisconnect != nil {
					s.onDisconnect(err)
				}

				// Connection lost, attempt reconnect if enabled
				if s.autoReconnect {
					go s.reconnect()
				}
				return
			}
		case <-s.stopKeepAlive:
			return
		case <-s.ctx.Done():
			return
		}
	}
}

// sendKeepAlive sends a keep-alive packet
func (s *Session) sendKeepAlive() error {
	client := s.Client()
	if client == nil {
		return fmt.Errorf("client not connected")
	}

	// Send a keep-alive request
	_, _, err := client.SendRequest("keepalive@openssh.com", true, nil)
	if err != nil {
		s.mu.Lock()
		s.connected = false
		s.lastError = fmt.Errorf("keep-alive failed: %w", err)
		s.mu.Unlock()
		return err
	}

	return nil
}

// reconnect attempts to reconnect the session
func (s *Session) reconnect() {
	s.mu.Lock()
	// If we're already connected, something else reconnected us
	if s.connected {
		s.mu.Unlock()
		return
	}

	// Check if we're already in a reconnection attempt (retryCount > 0 means we're retrying)
	// This prevents multiple goroutines from attempting reconnection simultaneously
	if s.retryCount > 0 {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()

	// Close the old client
	if s.client != nil {
		s.client.Close()
		s.client = nil
	}

	// Attempt reconnection
	if err := s.ConnectWithRetry(); err != nil {
		s.mu.Lock()
		s.lastError = fmt.Errorf("reconnect failed: %w", err)
		s.mu.Unlock()

		// Notify about final reconnection failure
		if s.onDisconnect != nil {
			s.onDisconnect(s.lastError)
		}
	} else {
		// Reconnection succeeded!
		if s.onReconnect != nil {
			s.onReconnect()
		}
	}
}

// Status returns the current session status
func (s *Session) Status() SessionStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return SessionStatus{
		Connected:   s.connected,
		ConnectedAt: s.connectedAt,
		LastError:   s.lastError,
		RetryCount:  s.retryCount,
		Host:        s.hop.Host,
		Port:        s.hop.Port,
		User:        s.hop.User,
	}
}

// SessionStatus represents the current status of an SSH session
type SessionStatus struct {
	Connected   bool
	ConnectedAt *time.Time
	LastError   error
	RetryCount  int
	Host        string
	Port        int
	User        string
}

// MultiHopSession manages a chain of SSH sessions for multi-hop tunneling
type MultiHopSession struct {
	hops     []*Session
	mu       sync.RWMutex
	ctx      context.Context
	cancel   context.CancelFunc
}

// NewMultiHopSession creates a new multi-hop SSH session chain
func NewMultiHopSession(ctx context.Context, hops []types.Hop, config SessionConfig) (*MultiHopSession, error) {
	if len(hops) == 0 {
		return nil, fmt.Errorf("at least one hop is required")
	}

	mhCtx, cancel := context.WithCancel(ctx)

	mhs := &MultiHopSession{
		hops:   make([]*Session, 0, len(hops)),
		ctx:    mhCtx,
		cancel: cancel,
	}

	// Create sessions for each hop
	for i := range hops {
		hopConfig := config
		hopConfig.Hop = &hops[i]

		session, err := NewSession(mhCtx, hopConfig)
		if err != nil {
			// Clean up previously created sessions
			mhs.Close()
			return nil, fmt.Errorf("failed to create session for hop %d: %w", i, err)
		}

		mhs.hops = append(mhs.hops, session)
	}

	return mhs, nil
}

// Connect establishes all hop connections in sequence, chaining through previous hops
func (mhs *MultiHopSession) Connect() error {
	mhs.mu.Lock()
	defer mhs.mu.Unlock()

	// Connect first hop directly
	if len(mhs.hops) > 0 {
		if err := mhs.hops[0].ConnectWithRetry(); err != nil {
			return fmt.Errorf("failed to connect hop 0 (%s): %w", mhs.hops[0].hop.Host, err)
		}
	}

	// For subsequent hops, connect through the previous hop
	for i := 1; i < len(mhs.hops); i++ {
		prevSession := mhs.hops[i-1]
		currentSession := mhs.hops[i]

		// Dial through previous hop to current hop
		addr := fmt.Sprintf("%s:%d", currentSession.hop.Host, currentSession.hop.Port)
		conn, err := prevSession.Dial("tcp", addr)
		if err != nil {
			return fmt.Errorf("failed to dial hop %d through hop %d: %w", i, i-1, err)
		}

		// Establish SSH connection over the tunneled connection
		if err := currentSession.connectOverConn(conn); err != nil {
			conn.Close()
			return fmt.Errorf("failed to connect hop %d (%s): %w", i, currentSession.hop.Host, err)
		}
	}

	return nil
}

// Dial creates a connection through the multi-hop chain to the final destination
func (mhs *MultiHopSession) Dial(network, address string) (net.Conn, error) {
	mhs.mu.RLock()
	defer mhs.mu.RUnlock()

	if len(mhs.hops) == 0 {
		return nil, fmt.Errorf("no hops configured")
	}

	// Since Connect() already chained all the hops together,
	// we can just use the last hop to dial the final destination.
	// The connection will automatically be routed through all previous hops.
	lastHop := mhs.hops[len(mhs.hops)-1]
	return lastHop.Dial(network, address)
}

// Close closes all hop sessions
func (mhs *MultiHopSession) Close() error {
	mhs.cancel()

	mhs.mu.Lock()
	defer mhs.mu.Unlock()

	var errs []error
	for i, session := range mhs.hops {
		if err := session.Close(); err != nil {
			errs = append(errs, fmt.Errorf("hop %d: %w", i, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors closing sessions: %v", errs)
	}

	return nil
}

// AllConnected returns true if all hops are connected
func (mhs *MultiHopSession) AllConnected() bool {
	mhs.mu.RLock()
	defer mhs.mu.RUnlock()

	for _, session := range mhs.hops {
		if !session.IsConnected() {
			return false
		}
	}

	return true
}

// IsConnected returns true if all hops are connected (alias for AllConnected)
func (mhs *MultiHopSession) IsConnected() bool {
	return mhs.AllConnected()
}

// Status returns status for all hops
func (mhs *MultiHopSession) Status() []SessionStatus {
	mhs.mu.RLock()
	defer mhs.mu.RUnlock()

	statuses := make([]SessionStatus, len(mhs.hops))
	for i, session := range mhs.hops {
		statuses[i] = session.Status()
	}

	return statuses
}

// getLastHopClient returns the SSH client of the last hop (for remote forwarding)
func (mhs *MultiHopSession) getLastHopClient() interface{} {
	mhs.mu.RLock()
	defer mhs.mu.RUnlock()

	if len(mhs.hops) == 0 {
		return nil
	}

	return mhs.hops[len(mhs.hops)-1].Client()
}

// ProxyConn wraps a net.Conn to allow closing both the connection and underlying resources
type ProxyConn struct {
	net.Conn
	closer io.Closer
}

// Close closes both the connection and underlying resources
func (pc *ProxyConn) Close() error {
	var errs []error

	if err := pc.Conn.Close(); err != nil {
		errs = append(errs, err)
	}

	if pc.closer != nil {
		if err := pc.closer.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors closing proxy connection: %v", errs)
	}

	return nil
}
