package agent

import (
	"sync"
	"time"

	"github.com/craigderington/lazytunnel/pkg/types"
)

const offlineThreshold = 45 * time.Second

// Registry tracks connected data-plane agents.
type Registry struct {
	mu     sync.RWMutex
	agents map[string]*types.AgentInfo
}

func NewRegistry() *Registry {
	return &Registry{agents: make(map[string]*types.AgentInfo)}
}

func (r *Registry) Register(id, hostname, version string) *types.AgentInfo {
	r.mu.Lock()
	defer r.mu.Unlock()

	info, ok := r.agents[id]
	if !ok {
		info = &types.AgentInfo{ID: id}
		r.agents[id] = info
	}
	info.Hostname = hostname
	info.Version = version
	info.LastSeen = time.Now()
	info.Status = "online"
	return info
}

func (r *Registry) Heartbeat(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	info, ok := r.agents[id]
	if !ok {
		return false
	}
	info.LastSeen = time.Now()
	info.Status = "online"
	return true
}

func (r *Registry) IsOnline(id string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	info, ok := r.agents[id]
	if !ok {
		return false
	}
	return time.Since(info.LastSeen) < offlineThreshold
}

func (r *Registry) List() []types.AgentInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]types.AgentInfo, 0, len(r.agents))
	now := time.Now()
	for _, a := range r.agents {
		copy := *a
		if now.Sub(copy.LastSeen) >= offlineThreshold {
			copy.Status = "offline"
		}
		out = append(out, copy)
	}
	return out
}

func (r *Registry) Get(id string) (*types.AgentInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.agents[id]
	if !ok {
		return nil, false
	}
	copy := *a
	if time.Since(copy.LastSeen) >= offlineThreshold {
		copy.Status = "offline"
	}
	return &copy, true
}