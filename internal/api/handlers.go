package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"

	"github.com/craigderington/lazytunnel/pkg/types"
)

// handleHealth returns server health status
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.respondJSON(w, http.StatusOK, map[string]interface{}{
		"status": "healthy",
		"time":   time.Now().UTC(),
	})
}

// handleListTunnels returns all active tunnels
func (s *Server) handleListTunnels(w http.ResponseWriter, r *http.Request) {
	tunnels := s.manager.List()

	response := make([]map[string]interface{}, len(tunnels))
	for i, t := range tunnels {
		status := t.GetStatus()
		var statusStr string
		var errorMsg string

		if status != nil {
			switch status.State {
			case types.TunnelStateActive:
				statusStr = "active"
			case types.TunnelStatePending:
				statusStr = "connecting"
			case types.TunnelStateFailed:
				statusStr = "failed"
			case types.TunnelStateStopped:
				statusStr = "disconnected"
			default:
				statusStr = "disconnected"
			}
			errorMsg = status.LastError
		} else {
			statusStr = "disconnected"
		}

		response[i] = map[string]interface{}{
			"id":               t.Spec.ID,
			"name":             t.Spec.Name,
			"owner":            t.Spec.Owner,
			"type":             t.Spec.Type,
			"hops":             t.Spec.Hops,
			"localPort":        t.Spec.LocalPort,
			"localBindAddress": t.Spec.LocalBindAddress,
			"remoteHost":       t.Spec.RemoteHost,
			"remotePort":       t.Spec.RemotePort,
			"autoReconnect":    t.Spec.AutoReconnect,
			"keepAlive":        t.Spec.KeepAlive.Seconds(),
			"maxRetries":       t.Spec.MaxRetries,
			"status":           statusStr,
			"createdAt":        t.CreatedAt.Format(time.RFC3339),
			"updatedAt":        t.Spec.UpdatedAt.Format(time.RFC3339),
			"errorMessage":     errorMsg,
		}
	}

	s.respondJSON(w, http.StatusOK, response)
}

// handleCreateTunnel creates a new tunnel
func (s *Server) handleCreateTunnel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name             string           `json:"name"`
		Type             types.TunnelType `json:"type"`
		Hops             []types.Hop      `json:"hops"`
		LocalPort        int              `json:"localPort"`
		LocalBindAddress string           `json:"localBindAddress"`
		RemoteHost       string           `json:"remoteHost"`
		RemotePort       int              `json:"remotePort"`
		AutoReconnect    bool             `json:"autoReconnect"`
		KeepAlive        int              `json:"keepAlive"` // seconds
		MaxRetries       int              `json:"maxRetries"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	// Build spec
	spec := types.TunnelSpec{
		ID:               uuid.New().String(),
		Name:             req.Name,
		Owner:            "api-user", // TODO: Get from auth context
		Type:             req.Type,
		Hops:             req.Hops,
		LocalPort:        req.LocalPort,
		LocalBindAddress: req.LocalBindAddress,
		RemoteHost:       req.RemoteHost,
		RemotePort:       req.RemotePort,
		AutoReconnect:    req.AutoReconnect,
		KeepAlive:        time.Duration(req.KeepAlive) * time.Second,
		MaxRetries:       req.MaxRetries,
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}

	// Set defaults
	if spec.KeepAlive == 0 {
		spec.KeepAlive = 30 * time.Second
	}
	if spec.MaxRetries == 0 {
		spec.MaxRetries = 5
	}

	// Create tunnel with background context (not request context!)
	// Using context.Background() so tunnel lives beyond HTTP request
	if err := s.manager.Create(context.Background(), &spec); err != nil {
		s.logger.Error().Err(err).Str("tunnel_id", spec.ID).Msg("Failed to create tunnel")
		s.respondError(w, http.StatusInternalServerError, "Failed to create tunnel: "+err.Error())
		return
	}

	s.logger.Info().
		Str("tunnel_id", spec.ID).
		Str("name", spec.Name).
		Str("type", string(spec.Type)).
		Msg("Tunnel created, connecting in background")

	// Return the created tunnel in the format the frontend expects
	// Status will be "connecting" initially, then transition to "active" or "failed"
	s.respondJSON(w, http.StatusCreated, map[string]interface{}{
		"id":               spec.ID,
		"name":             spec.Name,
		"owner":            spec.Owner,
		"type":             spec.Type,
		"hops":             spec.Hops,
		"localPort":        spec.LocalPort,
		"localBindAddress": spec.LocalBindAddress,
		"remoteHost":       spec.RemoteHost,
		"remotePort":       spec.RemotePort,
		"autoReconnect":    spec.AutoReconnect,
		"keepAlive":        spec.KeepAlive.Seconds(),
		"maxRetries":       spec.MaxRetries,
		"status":           "connecting", // Connecting in background
		"createdAt":        spec.CreatedAt.Format(time.RFC3339),
		"updatedAt":        spec.UpdatedAt.Format(time.RFC3339),
	})
}

// handleGetTunnel returns details for a specific tunnel
func (s *Server) handleGetTunnel(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	tunnelID := vars["id"]

	tunnel, err := s.manager.Get(tunnelID)
	if err != nil {
		s.respondError(w, http.StatusNotFound, "Tunnel not found")
		return
	}

	status := tunnel.GetStatus()
	var statusStr string
	var errorMsg string

	if status != nil {
		switch status.State {
		case types.TunnelStateActive:
			statusStr = "active"
		case types.TunnelStatePending:
			statusStr = "connecting"
		case types.TunnelStateFailed:
			statusStr = "failed"
		case types.TunnelStateStopped:
			statusStr = "disconnected"
		default:
			statusStr = "disconnected"
		}
		errorMsg = status.LastError
	} else {
		statusStr = "disconnected"
	}

	s.respondJSON(w, http.StatusOK, map[string]interface{}{
		"id":               tunnel.Spec.ID,
		"name":             tunnel.Spec.Name,
		"owner":            tunnel.Spec.Owner,
		"type":             tunnel.Spec.Type,
		"hops":             tunnel.Spec.Hops,
		"localPort":        tunnel.Spec.LocalPort,
		"localBindAddress": tunnel.Spec.LocalBindAddress,
		"remoteHost":       tunnel.Spec.RemoteHost,
		"remotePort":       tunnel.Spec.RemotePort,
		"autoReconnect":    tunnel.Spec.AutoReconnect,
		"keepAlive":        tunnel.Spec.KeepAlive.Seconds(),
		"maxRetries":       tunnel.Spec.MaxRetries,
		"status":           statusStr,
		"createdAt":        tunnel.CreatedAt.Format(time.RFC3339),
		"updatedAt":        tunnel.Spec.UpdatedAt.Format(time.RFC3339),
		"errorMessage":     errorMsg,
	})
}

// handleGetTunnelStatus returns status for a specific tunnel
func (s *Server) handleGetTunnelStatus(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	tunnelID := vars["id"]

	tunnel, err := s.manager.Get(tunnelID)
	if err != nil {
		s.respondError(w, http.StatusNotFound, "Tunnel not found")
		return
	}

	status := tunnel.GetStatus()
	s.respondJSON(w, http.StatusOK, status)
}

// handleDeleteTunnel stops and deletes a tunnel (removes from manager)
func (s *Server) handleDeleteTunnel(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	tunnelID := vars["id"]

	err := s.manager.Delete(context.Background(), tunnelID)
	if err != nil {
		// Check if it's a "not found" error - that's a real error
		if err.Error() == fmt.Sprintf("tunnel %s not found", tunnelID) {
			s.logger.Error().Err(err).Str("tunnel_id", tunnelID).Msg("Tunnel not found")
			s.respondError(w, http.StatusNotFound, "Tunnel not found")
			return
		}
		// Otherwise, tunnel was deleted but had stop errors (e.g. already failed)
		// Log the error but return success
		s.logger.Warn().Err(err).Str("tunnel_id", tunnelID).Msg("Tunnel deleted with warnings")
	} else {
		s.logger.Info().Str("tunnel_id", tunnelID).Msg("Tunnel deleted successfully")
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleStartTunnel starts a stopped tunnel
func (s *Server) handleStartTunnel(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	tunnelID := vars["id"]

	if err := s.manager.Start(r.Context(), tunnelID); err != nil {
		s.logger.Error().Err(err).Str("tunnel_id", tunnelID).Msg("Failed to start tunnel")
		s.respondError(w, http.StatusInternalServerError, "Failed to start tunnel: "+err.Error())
		return
	}

	s.logger.Info().Str("tunnel_id", tunnelID).Msg("Tunnel start initiated")

	// Get updated tunnel state
	tunnel, err := s.manager.Get(tunnelID)
	if err != nil {
		s.respondError(w, http.StatusNotFound, "Tunnel not found after start")
		return
	}

	s.respondJSON(w, http.StatusOK, map[string]interface{}{
		"id":               tunnel.Spec.ID,
		"name":             tunnel.Spec.Name,
		"owner":            tunnel.Spec.Owner,
		"type":             tunnel.Spec.Type,
		"hops":             tunnel.Spec.Hops,
		"localPort":        tunnel.Spec.LocalPort,
		"localBindAddress": tunnel.Spec.LocalBindAddress,
		"remoteHost":       tunnel.Spec.RemoteHost,
		"remotePort":       tunnel.Spec.RemotePort,
		"autoReconnect":    tunnel.Spec.AutoReconnect,
		"keepAlive":        tunnel.Spec.KeepAlive.Seconds(),
		"maxRetries":       tunnel.Spec.MaxRetries,
		"status":           "connecting",
		"createdAt":        tunnel.CreatedAt.Format(time.RFC3339),
		"updatedAt":        tunnel.Spec.UpdatedAt.Format(time.RFC3339),
	})
}

// handleStopTunnel stops a running tunnel (keeps it in the manager)
func (s *Server) handleStopTunnel(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	tunnelID := vars["id"]

	if err := s.manager.Stop(r.Context(), tunnelID); err != nil {
		s.logger.Error().Err(err).Str("tunnel_id", tunnelID).Msg("Failed to stop tunnel")
		s.respondError(w, http.StatusInternalServerError, "Failed to stop tunnel: "+err.Error())
		return
	}

	s.logger.Info().Str("tunnel_id", tunnelID).Msg("Tunnel stopped")

	// Get updated tunnel state
	tunnel, err := s.manager.Get(tunnelID)
	if err != nil {
		s.respondError(w, http.StatusNotFound, "Tunnel not found after stop")
		return
	}

	s.respondJSON(w, http.StatusOK, map[string]interface{}{
		"id":               tunnel.Spec.ID,
		"name":             tunnel.Spec.Name,
		"owner":            tunnel.Spec.Owner,
		"type":             tunnel.Spec.Type,
		"hops":             tunnel.Spec.Hops,
		"localPort":        tunnel.Spec.LocalPort,
		"localBindAddress": tunnel.Spec.LocalBindAddress,
		"remoteHost":       tunnel.Spec.RemoteHost,
		"remotePort":       tunnel.Spec.RemotePort,
		"autoReconnect":    tunnel.Spec.AutoReconnect,
		"keepAlive":        tunnel.Spec.KeepAlive.Seconds(),
		"maxRetries":       tunnel.Spec.MaxRetries,
		"status":           "stopped",
		"createdAt":        tunnel.CreatedAt.Format(time.RFC3339),
		"updatedAt":        tunnel.Spec.UpdatedAt.Format(time.RFC3339),
	})
}

// handleGetTunnelMetrics returns metrics for a specific tunnel
func (s *Server) handleGetTunnelMetrics(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	tunnelID := vars["id"]

	tunnel, err := s.manager.Get(tunnelID)
	if err != nil {
		s.respondError(w, http.StatusNotFound, "Tunnel not found")
		return
	}

	status := tunnel.GetStatus()
	if status == nil {
		s.respondError(w, http.StatusNotFound, "Tunnel status not available")
		return
	}

	var uptime int64
	if status.ConnectedAt != nil {
		uptime = int64(time.Since(*status.ConnectedAt).Seconds())
	}

	s.respondJSON(w, http.StatusOK, map[string]interface{}{
		"tunnelId":          tunnelID,
		"bytesIn":           status.BytesReceived,
		"bytesOut":          status.BytesSent,
		"connectionsActive": 1, // TODO: Track actual connections
		"uptime":            uptime,
		"lastHeartbeat":     time.Now().Format(time.RFC3339),
	})
}

// handleGetLogs returns systemd service logs
func (s *Server) handleGetLogs(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters
	lines := r.URL.Query().Get("lines")
	if lines == "" {
		lines = "100" // Default to 100 lines
	}

	follow := r.URL.Query().Get("follow") == "true"

	// Build journalctl command
	args := []string{
		"-u", "lazytunnel-server.service",
		"-n", lines,
		"--no-pager",
		"-o", "json",
	}

	if follow {
		// For SSE/streaming logs
		args = append(args, "-f")
	}

	// Execute journalctl command
	cmd := exec.Command("journalctl", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		s.logger.Error().Err(err).Msg("Failed to fetch logs from journalctl")
		s.respondError(w, http.StatusInternalServerError, "Failed to fetch logs: "+err.Error())
		return
	}

	// Parse JSON lines into array
	lines_output := []map[string]interface{}{}
	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		var entry map[string]interface{}
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			s.logger.Warn().Err(err).Msg("Failed to parse log entry")
			continue
		}

		// Convert MESSAGE field from byte array to string if needed
		if msg, ok := entry["MESSAGE"]; ok {
			switch v := msg.(type) {
			case []interface{}:
				// Convert byte array to string
				byteArr := make([]byte, len(v))
				for i, b := range v {
					if num, ok := b.(float64); ok {
						byteArr[i] = byte(num)
					}
				}
				entry["MESSAGE"] = string(byteArr)
			case string:
				// Already a string, keep as is
			}
		}

		lines_output = append(lines_output, entry)
	}

	if err := scanner.Err(); err != nil {
		s.logger.Error().Err(err).Msg("Error reading logs")
		s.respondError(w, http.StatusInternalServerError, "Error reading logs")
		return
	}

	s.respondJSON(w, http.StatusOK, map[string]interface{}{
		"logs": lines_output,
	})
}
