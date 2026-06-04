package api

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"

	"github.com/craigderington/lazytunnel/pkg/types"
)

func (s *Server) handleListAgents(w http.ResponseWriter, r *http.Request) {
	if s.agents == nil {
		s.respondJSON(w, http.StatusOK, []types.AgentInfo{})
		return
	}
	s.respondJSON(w, http.StatusOK, s.agents.List())
}

func (s *Server) handleRegisterAgent(w http.ResponseWriter, r *http.Request) {
	var req types.AgentRegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.BadRequest(w, "Invalid request body")
		return
	}
	if req.ID == "" {
		s.BadRequest(w, "Agent id is required")
		return
	}
	if req.Hostname == "" {
		req.Hostname = req.ID
	}
	if s.agents == nil {
		s.InternalError(w, "Agent registry not configured")
		return
	}
	info := s.agents.Register(req.ID, req.Hostname, req.Version)
	s.respondJSON(w, http.StatusOK, info)
}

func (s *Server) handleAgentHeartbeat(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if s.agents == nil || !s.agents.Heartbeat(id) {
		s.NotFound(w, "Agent")
		return
	}
	s.respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleAgentAssignments(w http.ResponseWriter, r *http.Request) {
	agentID := mux.Vars(r)["id"]
	if s.storage == nil {
		s.respondJSON(w, http.StatusOK, []types.AgentAssignment{})
		return
	}

	specs, err := s.storage.ListByAgent(r.Context(), agentID)
	if err != nil {
		s.InternalError(w, "Failed to list assignments")
		return
	}

	assignments := make([]types.AgentAssignment, 0, len(specs))
	for _, spec := range specs {
		reported := "stopped"
		if t, err := s.manager.Get(spec.ID); err == nil {
			if st := t.GetStatus(); st != nil {
				reported = string(st.State)
				if reported == "pending" {
					reported = "connecting"
				}
			}
		}
		assignments = append(assignments, types.AgentAssignment{
			Spec:           *spec,
			DesiredStatus:  spec.DesiredStatus,
			ReportedStatus: reported,
		})
	}

	s.respondJSON(w, http.StatusOK, assignments)
}

func (s *Server) handleAgentReport(w http.ResponseWriter, r *http.Request) {
	agentID := mux.Vars(r)["id"]
	if s.agents != nil {
		s.agents.Heartbeat(agentID)
	}

	var reports []types.AgentStatusReport
	if err := json.NewDecoder(r.Body).Decode(&reports); err != nil {
		s.BadRequest(w, "Invalid report payload")
		return
	}

	if s.coordinator != nil {
		s.coordinator.ApplyReports(reports)
	}

	s.respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}