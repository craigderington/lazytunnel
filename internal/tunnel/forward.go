package tunnel

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/craigderington/lazytunnel/pkg/types"
)

// Forwarder represents a port forwarding instance
type Forwarder interface {
	Start() error
	Stop() error
	Stats() ForwarderStats
}

// ForwarderStats contains statistics for a forwarder
type ForwarderStats struct {
	BytesSent     int64
	BytesReceived int64
	Connections   int64
	ActiveConns   int64
	Errors        int64
	StartedAt     time.Time
	LastActivity  time.Time
}

// LocalForwarder implements local port forwarding
// Binds to a local port and forwards connections through SSH to a remote destination
type LocalForwarder struct {
	spec     *types.TunnelSpec
	session  SessionDialer
	listener net.Listener

	// Stats
	stats ForwarderStats

	// Connection tracking
	activeConns sync.WaitGroup
	mu          sync.RWMutex

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	stopCh chan struct{}
	stopOnce sync.Once
}

// SessionDialer interface allows for both single and multi-hop sessions
type SessionDialer interface {
	Dial(network, address string) (net.Conn, error)
	IsConnected() bool
}

// Ensure Session and MultiHopSession implement SessionDialer
var _ SessionDialer = (*Session)(nil)

// NewLocalForwarder creates a new local port forwarder
func NewLocalForwarder(ctx context.Context, spec *types.TunnelSpec, session SessionDialer) (*LocalForwarder, error) {
	if spec.Type != types.TunnelTypeLocal {
		return nil, fmt.Errorf("invalid tunnel type: expected local, got %s", spec.Type)
	}

	// Note: LocalPort can be 0 for ephemeral port assignment
	if spec.LocalPort < 0 {
		return nil, fmt.Errorf("invalid local port: %d", spec.LocalPort)
	}

	if spec.RemoteHost == "" || spec.RemotePort == 0 {
		return nil, fmt.Errorf("remote host and port are required for local forwarding")
	}

	fwdCtx, cancel := context.WithCancel(ctx)

	lf := &LocalForwarder{
		spec:    spec,
		session: session,
		ctx:     fwdCtx,
		cancel:  cancel,
		stopCh:  make(chan struct{}),
	}

	lf.stats.StartedAt = time.Now()
	lf.stats.LastActivity = time.Now()

	return lf, nil
}

// Start begins listening on the local port and forwarding connections
func (lf *LocalForwarder) Start() error {
	lf.mu.Lock()
	if lf.listener != nil {
		lf.mu.Unlock()
		return fmt.Errorf("forwarder already started")
	}

	// Bind to local port (port 0 means OS chooses an ephemeral port)
	addr := fmt.Sprintf("127.0.0.1:%d", lf.spec.LocalPort)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		lf.mu.Unlock()
		return fmt.Errorf("failed to bind to %s: %w", addr, err)
	}

	lf.listener = listener

	// Update spec with actual bound port if ephemeral was used
	if lf.spec.LocalPort == 0 {
		if tcpAddr, ok := listener.Addr().(*net.TCPAddr); ok {
			lf.spec.LocalPort = tcpAddr.Port
		}
	}

	lf.mu.Unlock()

	// Accept connections in a goroutine
	go lf.acceptLoop()

	return nil
}

// acceptLoop accepts incoming connections and spawns goroutines to handle them
func (lf *LocalForwarder) acceptLoop() {
	for {
		// Check if we should stop before accepting
		select {
		case <-lf.stopCh:
			return
		case <-lf.ctx.Done():
			return
		default:
		}

		// Get listener safely
		lf.mu.RLock()
		listener := lf.listener
		lf.mu.RUnlock()

		if listener == nil {
			return
		}

		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-lf.stopCh:
				// Normal shutdown
				return
			case <-lf.ctx.Done():
				return
			default:
				// Error during accept
				atomic.AddInt64(&lf.stats.Errors, 1)
				continue
			}
		}

		// Handle connection in a new goroutine
		lf.activeConns.Add(1)
		go lf.handleConnection(conn)
	}
}

// handleConnection handles a single forwarded connection
func (lf *LocalForwarder) handleConnection(localConn net.Conn) {
	defer lf.activeConns.Done()
	defer localConn.Close()

	atomic.AddInt64(&lf.stats.Connections, 1)
	atomic.AddInt64(&lf.stats.ActiveConns, 1)
	defer atomic.AddInt64(&lf.stats.ActiveConns, -1)

	// Check if session is connected
	if !lf.session.IsConnected() {
		atomic.AddInt64(&lf.stats.Errors, 1)
		return
	}

	// Dial remote destination through SSH tunnel
	remoteAddr := fmt.Sprintf("%s:%d", lf.spec.RemoteHost, lf.spec.RemotePort)
	remoteConn, err := lf.session.Dial("tcp", remoteAddr)
	if err != nil {
		atomic.AddInt64(&lf.stats.Errors, 1)
		return
	}
	defer remoteConn.Close()

	// Bidirectional copy
	lf.proxy(localConn, remoteConn)
}

// proxy copies data bidirectionally between two connections
func (lf *LocalForwarder) proxy(local, remote net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)

	// Local -> Remote
	go func() {
		defer wg.Done()
		n, _ := io.Copy(remote, local)
		atomic.AddInt64(&lf.stats.BytesSent, n)
		lf.updateActivity()
	}()

	// Remote -> Local
	go func() {
		defer wg.Done()
		n, _ := io.Copy(local, remote)
		atomic.AddInt64(&lf.stats.BytesReceived, n)
		lf.updateActivity()
	}()

	wg.Wait()
}

// updateActivity updates the last activity timestamp
func (lf *LocalForwarder) updateActivity() {
	lf.mu.Lock()
	lf.stats.LastActivity = time.Now()
	lf.mu.Unlock()
}

// Stop stops the forwarder and waits for active connections to close
func (lf *LocalForwarder) Stop() error {
	var err error
	lf.stopOnce.Do(func() {
		close(lf.stopCh)
		lf.cancel()

		lf.mu.Lock()
		if lf.listener != nil {
			err = lf.listener.Close()
			lf.listener = nil
		}
		lf.mu.Unlock()

		// Wait for active connections to finish (with timeout)
		done := make(chan struct{})
		go func() {
			lf.activeConns.Wait()
			close(done)
		}()

		select {
		case <-done:
			// All connections closed gracefully
		case <-time.After(10 * time.Second):
			// Timeout waiting for connections
			err = fmt.Errorf("timeout waiting for connections to close")
		}
	})

	return err
}

// Stats returns the current forwarder statistics
func (lf *LocalForwarder) Stats() ForwarderStats {
	lf.mu.RLock()
	defer lf.mu.RUnlock()

	return ForwarderStats{
		BytesSent:     atomic.LoadInt64(&lf.stats.BytesSent),
		BytesReceived: atomic.LoadInt64(&lf.stats.BytesReceived),
		Connections:   atomic.LoadInt64(&lf.stats.Connections),
		ActiveConns:   atomic.LoadInt64(&lf.stats.ActiveConns),
		Errors:        atomic.LoadInt64(&lf.stats.Errors),
		StartedAt:     lf.stats.StartedAt,
		LastActivity:  lf.stats.LastActivity,
	}
}

// LocalAddr returns the local listening address
func (lf *LocalForwarder) LocalAddr() string {
	lf.mu.RLock()
	defer lf.mu.RUnlock()

	if lf.listener != nil {
		return lf.listener.Addr().String()
	}
	return ""
}

// RemoteForwarder implements remote port forwarding
// Binds to a remote port on the SSH server and forwards connections back to local
type RemoteForwarder struct {
	spec     *types.TunnelSpec
	session  SessionDialer
	listener net.Listener

	// Stats
	stats ForwarderStats

	// Connection tracking
	activeConns sync.WaitGroup
	mu          sync.RWMutex

	// Lifecycle
	ctx      context.Context
	cancel   context.CancelFunc
	stopCh   chan struct{}
	stopOnce sync.Once
}

// NewRemoteForwarder creates a new remote port forwarder
func NewRemoteForwarder(ctx context.Context, spec *types.TunnelSpec, session SessionDialer) (*RemoteForwarder, error) {
	if spec.Type != types.TunnelTypeRemote {
		return nil, fmt.Errorf("invalid tunnel type: expected remote, got %s", spec.Type)
	}

	if spec.RemotePort == 0 {
		return nil, fmt.Errorf("remote port is required for remote forwarding")
	}

	if spec.LocalPort == 0 {
		return nil, fmt.Errorf("local port is required for remote forwarding")
	}

	fwdCtx, cancel := context.WithCancel(ctx)

	rf := &RemoteForwarder{
		spec:   spec,
		session: session,
		ctx:    fwdCtx,
		cancel: cancel,
		stopCh: make(chan struct{}),
	}

	rf.stats.StartedAt = time.Now()
	rf.stats.LastActivity = time.Now()

	return rf, nil
}

// Start begins listening on the remote port and forwarding connections
func (rf *RemoteForwarder) Start() error {
	rf.mu.Lock()
	if rf.listener != nil {
		rf.mu.Unlock()
		return fmt.Errorf("forwarder already started")
	}

	// Check if session is connected
	if !rf.session.IsConnected() {
		rf.mu.Unlock()
		return fmt.Errorf("session not connected")
	}

	// Get the SSH client from the session
	clientInterface := rf.getSSHClient()
	if clientInterface == nil {
		rf.mu.Unlock()
		return fmt.Errorf("failed to get SSH client from session")
	}

	// Type assert to *ssh.Client
	client, ok := clientInterface.(*ssh.Client)
	if !ok || client == nil {
		rf.mu.Unlock()
		return fmt.Errorf("invalid SSH client type")
	}

	// Request remote port forwarding
	// Listen on the remote SSH server
	remoteAddr := fmt.Sprintf("0.0.0.0:%d", rf.spec.RemotePort)
	listener, err := client.Listen("tcp", remoteAddr)
	if err != nil {
		rf.mu.Unlock()
		return fmt.Errorf("failed to bind remote port %s: %w", remoteAddr, err)
	}

	rf.listener = listener
	rf.mu.Unlock()

	// Accept connections in a goroutine
	go rf.acceptLoop()

	return nil
}

// getSSHClient extracts the SSH client from the session
func (rf *RemoteForwarder) getSSHClient() interface{} {
	// Try to cast to *Session
	if s, ok := rf.session.(*Session); ok {
		return s.Client()
	}
	// Try to cast to *MultiHopSession
	if mhs, ok := rf.session.(*MultiHopSession); ok {
		// For multi-hop, we want the last hop's client
		return mhs.getLastHopClient()
	}
	return nil
}

// acceptLoop accepts incoming connections from the remote side
func (rf *RemoteForwarder) acceptLoop() {
	for {
		// Check if we should stop before accepting
		select {
		case <-rf.stopCh:
			return
		case <-rf.ctx.Done():
			return
		default:
		}

		// Get listener safely
		rf.mu.RLock()
		listener := rf.listener
		rf.mu.RUnlock()

		if listener == nil {
			return
		}

		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-rf.stopCh:
				// Normal shutdown
				return
			case <-rf.ctx.Done():
				return
			default:
				// Error during accept
				atomic.AddInt64(&rf.stats.Errors, 1)
				continue
			}
		}

		// Handle connection in a new goroutine
		rf.activeConns.Add(1)
		go rf.handleConnection(conn)
	}
}

// handleConnection handles a single forwarded connection
func (rf *RemoteForwarder) handleConnection(remoteConn net.Conn) {
	defer rf.activeConns.Done()
	defer remoteConn.Close()

	atomic.AddInt64(&rf.stats.Connections, 1)
	atomic.AddInt64(&rf.stats.ActiveConns, 1)
	defer atomic.AddInt64(&rf.stats.ActiveConns, -1)

	// Dial local destination
	localAddr := fmt.Sprintf("127.0.0.1:%d", rf.spec.LocalPort)
	localConn, err := net.Dial("tcp", localAddr)
	if err != nil {
		atomic.AddInt64(&rf.stats.Errors, 1)
		return
	}
	defer localConn.Close()

	// Bidirectional copy
	rf.proxy(remoteConn, localConn)
}

// proxy copies data bidirectionally between two connections
func (rf *RemoteForwarder) proxy(remote, local net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)

	// Remote -> Local
	go func() {
		defer wg.Done()
		n, _ := io.Copy(local, remote)
		atomic.AddInt64(&rf.stats.BytesReceived, n)
		rf.updateActivity()
	}()

	// Local -> Remote
	go func() {
		defer wg.Done()
		n, _ := io.Copy(remote, local)
		atomic.AddInt64(&rf.stats.BytesSent, n)
		rf.updateActivity()
	}()

	wg.Wait()
}

// updateActivity updates the last activity timestamp
func (rf *RemoteForwarder) updateActivity() {
	rf.mu.Lock()
	rf.stats.LastActivity = time.Now()
	rf.mu.Unlock()
}

// Stop stops the forwarder and waits for active connections to close
func (rf *RemoteForwarder) Stop() error {
	var err error
	rf.stopOnce.Do(func() {
		close(rf.stopCh)
		rf.cancel()

		rf.mu.Lock()
		if rf.listener != nil {
			err = rf.listener.Close()
			rf.listener = nil
		}
		rf.mu.Unlock()

		// Wait for active connections to finish (with timeout)
		done := make(chan struct{})
		go func() {
			rf.activeConns.Wait()
			close(done)
		}()

		select {
		case <-done:
			// All connections closed gracefully
		case <-time.After(10 * time.Second):
			// Timeout waiting for connections
			err = fmt.Errorf("timeout waiting for connections to close")
		}
	})

	return err
}

// Stats returns the current forwarder statistics
func (rf *RemoteForwarder) Stats() ForwarderStats {
	rf.mu.RLock()
	defer rf.mu.RUnlock()

	return ForwarderStats{
		BytesSent:     atomic.LoadInt64(&rf.stats.BytesSent),
		BytesReceived: atomic.LoadInt64(&rf.stats.BytesReceived),
		Connections:   atomic.LoadInt64(&rf.stats.Connections),
		ActiveConns:   atomic.LoadInt64(&rf.stats.ActiveConns),
		Errors:        atomic.LoadInt64(&rf.stats.Errors),
		StartedAt:     rf.stats.StartedAt,
		LastActivity:  rf.stats.LastActivity,
	}
}

// RemoteAddr returns the remote listening address
func (rf *RemoteForwarder) RemoteAddr() string {
	rf.mu.RLock()
	defer rf.mu.RUnlock()

	if rf.listener != nil {
		return rf.listener.Addr().String()
	}
	return ""
}

// DynamicForwarder implements SOCKS5 dynamic port forwarding
// Binds to a local port and acts as a SOCKS5 proxy, forwarding to dynamic destinations
type DynamicForwarder struct {
	spec     *types.TunnelSpec
	session  SessionDialer
	listener net.Listener

	// Stats
	stats ForwarderStats

	// Connection tracking
	activeConns sync.WaitGroup
	mu          sync.RWMutex

	// Lifecycle
	ctx      context.Context
	cancel   context.CancelFunc
	stopCh   chan struct{}
	stopOnce sync.Once
}

// NewDynamicForwarder creates a new SOCKS5 dynamic forwarder
func NewDynamicForwarder(ctx context.Context, spec *types.TunnelSpec, session SessionDialer) (*DynamicForwarder, error) {
	if spec.Type != types.TunnelTypeDynamic {
		return nil, fmt.Errorf("invalid tunnel type: expected dynamic, got %s", spec.Type)
	}

	if spec.LocalPort < 0 {
		return nil, fmt.Errorf("invalid local port: %d", spec.LocalPort)
	}

	fwdCtx, cancel := context.WithCancel(ctx)

	df := &DynamicForwarder{
		spec:   spec,
		session: session,
		ctx:    fwdCtx,
		cancel: cancel,
		stopCh: make(chan struct{}),
	}

	df.stats.StartedAt = time.Now()
	df.stats.LastActivity = time.Now()

	return df, nil
}

// Start begins listening on the local port as a SOCKS5 proxy
func (df *DynamicForwarder) Start() error {
	df.mu.Lock()
	if df.listener != nil {
		df.mu.Unlock()
		return fmt.Errorf("forwarder already started")
	}

	// Bind to local port
	addr := fmt.Sprintf("127.0.0.1:%d", df.spec.LocalPort)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		df.mu.Unlock()
		return fmt.Errorf("failed to bind to %s: %w", addr, err)
	}

	df.listener = listener

	// Update spec with actual bound port if ephemeral was used
	if df.spec.LocalPort == 0 {
		if tcpAddr, ok := listener.Addr().(*net.TCPAddr); ok {
			df.spec.LocalPort = tcpAddr.Port
		}
	}

	df.mu.Unlock()

	// Accept connections in a goroutine
	go df.acceptLoop()

	return nil
}

// acceptLoop accepts incoming SOCKS5 connections
func (df *DynamicForwarder) acceptLoop() {
	for {
		// Check if we should stop before accepting
		select {
		case <-df.stopCh:
			return
		case <-df.ctx.Done():
			return
		default:
		}

		// Get listener safely
		df.mu.RLock()
		listener := df.listener
		df.mu.RUnlock()

		if listener == nil {
			return
		}

		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-df.stopCh:
				// Normal shutdown
				return
			case <-df.ctx.Done():
				return
			default:
				// Error during accept
				atomic.AddInt64(&df.stats.Errors, 1)
				continue
			}
		}

		// Handle SOCKS5 connection in a new goroutine
		df.activeConns.Add(1)
		go df.handleSOCKS5(conn)
	}
}

// handleSOCKS5 handles a single SOCKS5 connection
func (df *DynamicForwarder) handleSOCKS5(clientConn net.Conn) {
	defer df.activeConns.Done()
	defer clientConn.Close()

	atomic.AddInt64(&df.stats.Connections, 1)
	atomic.AddInt64(&df.stats.ActiveConns, 1)
	defer atomic.AddInt64(&df.stats.ActiveConns, -1)

	// Check if session is connected
	if !df.session.IsConnected() {
		atomic.AddInt64(&df.stats.Errors, 1)
		return
	}

	// Perform SOCKS5 handshake
	destAddr, err := df.socks5Handshake(clientConn)
	if err != nil {
		atomic.AddInt64(&df.stats.Errors, 1)
		return
	}

	// Dial destination through SSH tunnel
	remoteConn, err := df.session.Dial("tcp", destAddr)
	if err != nil {
		atomic.AddInt64(&df.stats.Errors, 1)
		// Send SOCKS5 error response
		df.socks5Error(clientConn, 0x04) // Host unreachable
		return
	}
	defer remoteConn.Close()

	// Send SOCKS5 success response
	if err := df.socks5Success(clientConn); err != nil {
		atomic.AddInt64(&df.stats.Errors, 1)
		return
	}

	// Bidirectional copy
	df.proxy(clientConn, remoteConn)
}

// socks5Handshake performs the SOCKS5 handshake and returns the destination address
func (df *DynamicForwarder) socks5Handshake(conn net.Conn) (string, error) {
	// Read greeting
	// +----+----------+----------+
	// |VER | NMETHODS | METHODS  |
	// +----+----------+----------+
	// | 1  |    1     | 1 to 255 |
	// +----+----------+----------+
	buf := make([]byte, 257)
	n, err := io.ReadAtLeast(conn, buf, 2)
	if err != nil {
		return "", fmt.Errorf("failed to read greeting: %w", err)
	}

	version := buf[0]
	if version != 0x05 {
		return "", fmt.Errorf("unsupported SOCKS version: %d", version)
	}

	nmethods := int(buf[1])
	if n < 2+nmethods {
		_, err = io.ReadAtLeast(conn, buf[n:], 2+nmethods-n)
		if err != nil {
			return "", fmt.Errorf("failed to read methods: %w", err)
		}
	}

	// Send method selection (no authentication required)
	// +----+--------+
	// |VER | METHOD |
	// +----+--------+
	// | 1  |   1    |
	// +----+--------+
	_, err = conn.Write([]byte{0x05, 0x00}) // version 5, no auth
	if err != nil {
		return "", fmt.Errorf("failed to write method selection: %w", err)
	}

	// Read request
	// +----+-----+-------+------+----------+----------+
	// |VER | CMD |  RSV  | ATYP | DST.ADDR | DST.PORT |
	// +----+-----+-------+------+----------+----------+
	// | 1  |  1  | X'00' |  1   | Variable |    2     |
	// +----+-----+-------+------+----------+----------+
	n, err = io.ReadAtLeast(conn, buf, 4)
	if err != nil {
		return "", fmt.Errorf("failed to read request: %w", err)
	}

	if buf[0] != 0x05 {
		return "", fmt.Errorf("invalid SOCKS version in request: %d", buf[0])
	}

	cmd := buf[1]
	if cmd != 0x01 { // CONNECT command
		df.socks5Error(conn, 0x07) // Command not supported
		return "", fmt.Errorf("unsupported command: %d", cmd)
	}

	atyp := buf[3]
	var destAddr string

	switch atyp {
	case 0x01: // IPv4
		if n < 10 {
			_, err = io.ReadAtLeast(conn, buf[n:], 10-n)
			if err != nil {
				return "", fmt.Errorf("failed to read IPv4 address: %w", err)
			}
		}
		ip := net.IP(buf[4:8])
		port := int(buf[8])<<8 | int(buf[9])
		destAddr = fmt.Sprintf("%s:%d", ip.String(), port)

	case 0x03: // Domain name
		if n < 5 {
			_, err = io.ReadAtLeast(conn, buf[n:], 5-n)
			if err != nil {
				return "", fmt.Errorf("failed to read domain length: %w", err)
			}
		}
		domainLen := int(buf[4])
		if n < 5+domainLen+2 {
			_, err = io.ReadAtLeast(conn, buf[n:], 5+domainLen+2-n)
			if err != nil {
				return "", fmt.Errorf("failed to read domain: %w", err)
			}
		}
		domain := string(buf[5 : 5+domainLen])
		port := int(buf[5+domainLen])<<8 | int(buf[5+domainLen+1])
		destAddr = fmt.Sprintf("%s:%d", domain, port)

	case 0x04: // IPv6
		if n < 22 {
			_, err = io.ReadAtLeast(conn, buf[n:], 22-n)
			if err != nil {
				return "", fmt.Errorf("failed to read IPv6 address: %w", err)
			}
		}
		ip := net.IP(buf[4:20])
		port := int(buf[20])<<8 | int(buf[21])
		destAddr = fmt.Sprintf("[%s]:%d", ip.String(), port)

	default:
		df.socks5Error(conn, 0x08) // Address type not supported
		return "", fmt.Errorf("unsupported address type: %d", atyp)
	}

	return destAddr, nil
}

// socks5Success sends a SOCKS5 success response
func (df *DynamicForwarder) socks5Success(conn net.Conn) error {
	// +----+-----+-------+------+----------+----------+
	// |VER | REP |  RSV  | ATYP | BND.ADDR | BND.PORT |
	// +----+-----+-------+------+----------+----------+
	// | 1  |  1  | X'00' |  1   | Variable |    2     |
	// +----+-----+-------+------+----------+----------+
	response := []byte{
		0x05, // SOCKS version
		0x00, // Success
		0x00, // Reserved
		0x01, // IPv4
		0, 0, 0, 0, // Bind address (0.0.0.0)
		0, 0, // Bind port (0)
	}
	_, err := conn.Write(response)
	return err
}

// socks5Error sends a SOCKS5 error response
func (df *DynamicForwarder) socks5Error(conn net.Conn, rep byte) error {
	response := []byte{
		0x05, // SOCKS version
		rep,  // Error code
		0x00, // Reserved
		0x01, // IPv4
		0, 0, 0, 0, // Bind address
		0, 0, // Bind port
	}
	_, err := conn.Write(response)
	return err
}

// proxy copies data bidirectionally between two connections
func (df *DynamicForwarder) proxy(client, remote net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)

	// Client -> Remote
	go func() {
		defer wg.Done()
		n, _ := io.Copy(remote, client)
		atomic.AddInt64(&df.stats.BytesSent, n)
		df.updateActivity()
	}()

	// Remote -> Client
	go func() {
		defer wg.Done()
		n, _ := io.Copy(client, remote)
		atomic.AddInt64(&df.stats.BytesReceived, n)
		df.updateActivity()
	}()

	wg.Wait()
}

// updateActivity updates the last activity timestamp
func (df *DynamicForwarder) updateActivity() {
	df.mu.Lock()
	df.stats.LastActivity = time.Now()
	df.mu.Unlock()
}

// Stop stops the forwarder and waits for active connections to close
func (df *DynamicForwarder) Stop() error {
	var err error
	df.stopOnce.Do(func() {
		close(df.stopCh)
		df.cancel()

		df.mu.Lock()
		if df.listener != nil {
			err = df.listener.Close()
			df.listener = nil
		}
		df.mu.Unlock()

		// Wait for active connections to finish (with timeout)
		done := make(chan struct{})
		go func() {
			df.activeConns.Wait()
			close(done)
		}()

		select {
		case <-done:
			// All connections closed gracefully
		case <-time.After(10 * time.Second):
			// Timeout waiting for connections
			err = fmt.Errorf("timeout waiting for connections to close")
		}
	})

	return err
}

// Stats returns the current forwarder statistics
func (df *DynamicForwarder) Stats() ForwarderStats {
	df.mu.RLock()
	defer df.mu.RUnlock()

	return ForwarderStats{
		BytesSent:     atomic.LoadInt64(&df.stats.BytesSent),
		BytesReceived: atomic.LoadInt64(&df.stats.BytesReceived),
		Connections:   atomic.LoadInt64(&df.stats.Connections),
		ActiveConns:   atomic.LoadInt64(&df.stats.ActiveConns),
		Errors:        atomic.LoadInt64(&df.stats.Errors),
		StartedAt:     df.stats.StartedAt,
		LastActivity:  df.stats.LastActivity,
	}
}

// LocalAddr returns the local listening address
func (df *DynamicForwarder) LocalAddr() string {
	df.mu.RLock()
	defer df.mu.RUnlock()

	if df.listener != nil {
		return df.listener.Addr().String()
	}
	return ""
}
