package api

import (
	"time"

	runtunnel "github.com/craigderington/lazytunnel/internal/tunnel"
	"github.com/craigderington/lazytunnel/pkg/types"
)

// tunnelHopsFromRequest converts validated request hops into domain hops,
// defaulting to strict host-key verification.
func tunnelHopsFromRequest(reqHops []HopReq) []types.Hop {
	hops := make([]types.Hop, len(reqHops))
	for i, h := range reqHops {
		hops[i] = types.Hop{
			Host:                h.Host,
			Port:                h.Port,
			User:                h.User,
			AuthMethod:          types.AuthMethod(h.AuthMethod),
			KeyID:               h.KeyID,
			HostKeyVerification: types.HostKeyVerifyStrict,
		}
	}
	return hops
}

// tunnelResponse renders a tunnel in the JSON shape the frontend expects.
func tunnelResponse(t *runtunnel.Tunnel, status, errorMsg string) map[string]interface{} {
	spec := t.Spec
	return map[string]interface{}{
		"id":               spec.ID,
		"name":             spec.Name,
		"owner":            spec.Owner,
		"agentId":          spec.AgentID,
		"desiredStatus":    string(spec.DesiredStatus),
		"type":             spec.Type,
		"hops":             spec.Hops,
		"localPort":        spec.LocalPort,
		"localBindAddress": spec.LocalBindAddress,
		"remoteHost":       spec.RemoteHost,
		"remotePort":       spec.RemotePort,
		"autoReconnect":    spec.AutoReconnect,
		"keepAlive":        spec.KeepAlive.Seconds(),
		"maxRetries":       spec.MaxRetries,
		"status":           status,
		"createdAt":        t.CreatedAt.Format(time.RFC3339),
		"updatedAt":        spec.UpdatedAt.Format(time.RFC3339),
		"errorMessage":     errorMsg,
	}
}
