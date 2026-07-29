package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/craigderington/lazytunnel/internal/backup"
)

// maxImportBytes caps the archive an import will read, so a malformed or
// hostile upload cannot exhaust memory.
const maxImportBytes = 10 << 20 // 10 MiB

// handleExportConfig returns every stored tunnel as a versioned archive.
func (s *Server) handleExportConfig(w http.ResponseWriter, r *http.Request) {
	if s.storage == nil {
		s.ServiceUnavailableError(w, "Persistent storage is not configured")
		return
	}

	archive, err := backup.Export(r.Context(), s.storage, "lazytunnel/"+s.version, time.Now)
	if err != nil {
		s.logger.Error().Err(err).Msg("Failed to export configuration")
		s.InternalError(w, "Failed to export configuration")
		return
	}

	filename := fmt.Sprintf("lazytunnel-backup-%s.json", archive.ExportedAt.Format("20060102-150405"))

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.WriteHeader(http.StatusOK)

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(archive); err != nil {
		s.logger.Error().Err(err).Msg("Failed to encode export")
	}
}

// handleImportConfig restores tunnel definitions from an archive.
//
// ?mode=merge (default) updates and creates; ?mode=replace additionally
// deletes tunnels absent from the archive. ?dry_run=true returns the intended
// plan without writing anything.
func (s *Server) handleImportConfig(w http.ResponseWriter, r *http.Request) {
	if s.storage == nil {
		s.ServiceUnavailableError(w, "Persistent storage is not configured")
		return
	}

	mode, err := backup.ParseMode(r.URL.Query().Get("mode"))
	if err != nil {
		s.BadRequest(w, err.Error())
		return
	}
	dryRun := r.URL.Query().Get("dry_run") == "true"

	var archive backup.Archive
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxImportBytes)).Decode(&archive); err != nil {
		s.BadRequest(w, "Invalid archive JSON: "+err.Error())
		return
	}

	owner := "api-user"
	if user, ok := GetUser(r.Context()); ok {
		owner = user.Username
	}

	current, err := s.storage.List(r.Context())
	if err != nil {
		s.logger.Error().Err(err).Msg("Failed to list tunnels for import")
		s.InternalError(w, "Failed to read existing tunnels")
		return
	}

	plan, err := backup.Plan(current, &archive, backup.PlanOptions{Mode: mode, DefaultOwner: owner})
	if err != nil {
		var invalid backup.ArchiveInvalidError
		if errors.As(err, &invalid) {
			// The key must be "message", not "error": web/src/api/client.ts's
			// parseError reads body.message and falls back to statusText, so an
			// "error" key would show the user a bare "Bad Request" instead of
			// what is actually wrong with their file. "details" carries the
			// per-entry specifics, matching the shape of APIError.
			s.respondJSON(w, http.StatusBadRequest, map[string]interface{}{
				"code":    "validation_error",
				"message": fmt.Sprintf("archive validation failed: %d problem(s)", len(invalid.Errors)),
				"details": invalid.Errors,
			})
			return
		}
		// Plan also refuses when the STORED data is ambiguous — two tunnels
		// whose names differ only by surrounding whitespace. That is a conflict
		// in server state, not a defect in the user's file, so it must not be
		// reported as a bad request.
		s.ConflictError(w, err.Error())
		return
	}

	if dryRun {
		s.respondJSON(w, http.StatusOK, backup.NewDryRunReport(plan))
		return
	}

	// Background context: the writes and the reconnects that follow must
	// outlive the HTTP request, matching handleCreateTunnel.
	report, applyErr := backup.Apply(context.Background(), s.storage, plan)

	// Converge the running fleet onto the newly stored desired state.
	if err := s.manager.Reconcile(context.Background()); err != nil {
		s.logger.Error().Err(err).Msg("Failed to reconcile tunnels after import")
	}

	if applyErr != nil {
		s.logger.Error().Err(applyErr).Msg("Import partially failed")
		s.respondJSON(w, http.StatusInternalServerError, report)
		return
	}

	s.logger.Info().
		Str("mode", string(mode)).
		Int("created", report.Created).
		Int("updated", report.Updated).
		Int("deleted", report.Deleted).
		Int("skipped", report.Skipped).
		Msg("Configuration imported")

	s.respondJSON(w, http.StatusOK, report)
}
