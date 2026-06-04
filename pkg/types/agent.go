package types

import "time"

// AgentInfo describes a registered data-plane agent.
type AgentInfo struct {
	ID        string    `json:"id"`
	Hostname  string    `json:"hostname"`
	Version   string    `json:"version"`
	Status    string    `json:"status"` // online, offline
	LastSeen  time.Time `json:"last_seen"`
	TunnelCount int     `json:"tunnel_count,omitempty"`
}

// AgentAssignment is a tunnel the agent should reconcile.
type AgentAssignment struct {
	Spec           TunnelSpec    `json:"spec"`
	DesiredStatus  DesiredStatus `json:"desired_status"`
	ReportedStatus string        `json:"reported_status,omitempty"`
}

// AgentRegisterRequest registers or updates an agent.
type AgentRegisterRequest struct {
	ID       string `json:"id"`
	Hostname string `json:"hostname"`
	Version  string `json:"version"`
}

// AgentStatusReport is sent by agents after reconciliation.
type AgentStatusReport struct {
	TunnelID  string `json:"tunnel_id"`
	Status    string `json:"status"`
	LastError string `json:"last_error,omitempty"`
}