package types

import "time"

// TunnelType represents the type of SSH tunnel
type TunnelType string

const (
	TunnelTypeLocal   TunnelType = "local"
	TunnelTypeRemote  TunnelType = "remote"
	TunnelTypeDynamic TunnelType = "dynamic"
)

// TunnelState represents the current state of a tunnel
type TunnelState string

const (
	TunnelStatePending TunnelState = "pending"
	TunnelStateActive  TunnelState = "active"
	TunnelStateFailed  TunnelState = "failed"
	TunnelStateStopped TunnelState = "stopped"
)

// AuthMethod represents SSH authentication methods
type AuthMethod string

const (
	AuthMethodKey      AuthMethod = "key"
	AuthMethodPassword AuthMethod = "password"
	AuthMethodAgent    AuthMethod = "agent"
	AuthMethodCert     AuthMethod = "cert"
)

// TunnelSpec defines a tunnel configuration
type TunnelSpec struct {
	ID            string        `json:"id"`
	Name          string        `json:"name"`
	Owner         string        `json:"owner"`
	Type          TunnelType    `json:"type"`
	Hops          []Hop         `json:"hops"`
	LocalPort     int           `json:"local_port,omitempty"`
	RemoteHost    string        `json:"remote_host,omitempty"`
	RemotePort    int           `json:"remote_port,omitempty"`
	Auth          AuthConfig    `json:"auth"`
	AutoReconnect bool          `json:"auto_reconnect"`
	KeepAlive     time.Duration `json:"keep_alive"`
	MaxRetries    int           `json:"max_retries"`
	Policy        PolicySpec    `json:"policy,omitempty"`
	CreatedAt     time.Time     `json:"created_at"`
	UpdatedAt     time.Time     `json:"updated_at"`
}

// Hop represents a single SSH hop in a multi-hop tunnel
type Hop struct {
	Host       string     `json:"host"`
	Port       int        `json:"port"`
	User       string     `json:"user"`
	AuthMethod AuthMethod `json:"auth_method"`
	KeyID      string     `json:"key_id,omitempty"`
}

// AuthConfig contains authentication configuration
type AuthConfig struct {
	Method   AuthMethod `json:"method"`
	KeyID    string     `json:"key_id,omitempty"`
	Username string     `json:"username"`
	UseAgent bool       `json:"use_agent"`
}

// PolicySpec defines authorization policies for a tunnel
type PolicySpec struct {
	AllowedUsers  []string `json:"allowed_users,omitempty"`
	AllowedGroups []string `json:"allowed_groups,omitempty"`
	RequiresMFA   bool     `json:"requires_mfa"`
}

// TunnelStatus represents the current status of a tunnel
type TunnelStatus struct {
	TunnelID      string        `json:"tunnel_id"`
	State         TunnelState   `json:"state"`
	ConnectedAt   *time.Time    `json:"connected_at,omitempty"`
	LastError     string        `json:"last_error,omitempty"`
	BytesSent     int64         `json:"bytes_sent"`
	BytesReceived int64         `json:"bytes_received"`
	Latency       time.Duration `json:"latency"`
	RetryCount    int           `json:"retry_count"`
}
