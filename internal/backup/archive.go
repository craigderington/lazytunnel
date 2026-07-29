// Package backup exports and restores lazytunnel tunnel definitions as a
// portable, versioned JSON archive.
package backup

import (
	"time"

	"github.com/craigderington/lazytunnel/pkg/types"
)

// SchemaVersion is the archive format version written by Export, and the only
// version Import accepts.
const SchemaVersion = 1

// Archive is the top-level backup document.
type Archive struct {
	Version    int           `json:"version"`
	ExportedAt time.Time     `json:"exported_at"`
	Source     string        `json:"source"`
	Tunnels    []TunnelEntry `json:"tunnels"`
}

// TunnelEntry is the on-disk representation of one tunnel.
//
// It is deliberately not types.TunnelSpec. TunnelSpec.KeepAlive is a
// time.Duration that marshals to raw nanoseconds (30000000000), which is
// unreadable in a file meant to be hand-edited; and TunnelSpec.Auth and
// .Policy are never persisted by internal/storage/sqlite.go, so exporting the
// spec directly would emit an empty "auth" object implying the backup carries
// credentials it does not have.
type TunnelEntry struct {
	ID               string     `json:"id,omitempty"`
	Name             string     `json:"name"`
	Owner            string     `json:"owner,omitempty"`
	AgentID          string     `json:"agent_id,omitempty"`
	DesiredStatus    string     `json:"desired_status,omitempty"`
	Type             string     `json:"type"`
	Hops             []HopEntry `json:"hops"`
	LocalPort        int        `json:"local_port,omitempty"`
	LocalBindAddress string     `json:"local_bind_address,omitempty"`
	RemoteHost       string     `json:"remote_host,omitempty"`
	RemotePort       int        `json:"remote_port,omitempty"`
	AutoReconnect    bool       `json:"auto_reconnect"`
	KeepAliveSeconds int        `json:"keep_alive_seconds,omitempty"`
	MaxRetries       int        `json:"max_retries,omitempty"`
}

// HopEntry is the on-disk representation of a single SSH hop.
type HopEntry struct {
	Host                string `json:"host"`
	Port                int    `json:"port"`
	User                string `json:"user"`
	AuthMethod          string `json:"auth_method"`
	KeyID               string `json:"key_id,omitempty"`
	HostKeyVerification string `json:"host_key_verification,omitempty"`
	KnownHostsPath      string `json:"known_hosts_path,omitempty"`
}

// EntryFromSpec converts a stored spec into its archive representation.
func EntryFromSpec(spec *types.TunnelSpec) TunnelEntry {
	hops := make([]HopEntry, 0, len(spec.Hops))
	for _, h := range spec.Hops {
		hops = append(hops, HopEntry{
			Host:                h.Host,
			Port:                h.Port,
			User:                h.User,
			AuthMethod:          string(h.AuthMethod),
			KeyID:               h.KeyID,
			HostKeyVerification: string(h.HostKeyVerification),
			KnownHostsPath:      h.KnownHostsPath,
		})
	}

	desired := string(spec.DesiredStatus)
	if desired == "" {
		desired = string(types.DesiredStatusStopped)
	}

	return TunnelEntry{
		ID:               spec.ID,
		Name:             spec.Name,
		Owner:            spec.Owner,
		AgentID:          spec.AgentID,
		DesiredStatus:    desired,
		Type:             string(spec.Type),
		Hops:             hops,
		LocalPort:        spec.LocalPort,
		LocalBindAddress: spec.LocalBindAddress,
		RemoteHost:       spec.RemoteHost,
		RemotePort:       spec.RemotePort,
		AutoReconnect:    spec.AutoReconnect,
		KeepAliveSeconds: int(spec.KeepAlive.Seconds()),
		MaxRetries:       spec.MaxRetries,
	}
}

// SpecFromEntry converts an archive entry into a tunnel spec.
//
// Identity fields are left as the archive gave them: Plan resolves ID, Owner
// and CreatedAt against what is already stored.
func SpecFromEntry(e TunnelEntry) *types.TunnelSpec {
	hops := make([]types.Hop, 0, len(e.Hops))
	for _, h := range e.Hops {
		hops = append(hops, types.Hop{
			Host:                h.Host,
			Port:                h.Port,
			User:                h.User,
			AuthMethod:          types.AuthMethod(h.AuthMethod),
			KeyID:               h.KeyID,
			HostKeyVerification: types.HostKeyVerification(h.HostKeyVerification),
			KnownHostsPath:      h.KnownHostsPath,
		})
	}

	desired := e.DesiredStatus
	if desired == "" {
		desired = string(types.DesiredStatusStopped)
	}

	return &types.TunnelSpec{
		ID:               e.ID,
		Name:             e.Name,
		Owner:            e.Owner,
		AgentID:          e.AgentID,
		DesiredStatus:    types.DesiredStatus(desired),
		Type:             types.TunnelType(e.Type),
		Hops:             hops,
		LocalPort:        e.LocalPort,
		LocalBindAddress: e.LocalBindAddress,
		RemoteHost:       e.RemoteHost,
		RemotePort:       e.RemotePort,
		AutoReconnect:    e.AutoReconnect,
		KeepAlive:        time.Duration(e.KeepAliveSeconds) * time.Second,
		MaxRetries:       e.MaxRetries,
	}
}
