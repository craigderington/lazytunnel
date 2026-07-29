package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"strconv"
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

	// Marshal into a buffer before writing any header, so the response can
	// carry a Content-Length instead of falling back to chunked transfer
	// encoding — without it, a browser download shows no size and no
	// progress bar. json.Encoder.Encode can only fail once bytes are already
	// on the wire and the connection has gone bad, so buffering first here
	// doesn't trade away any error handling: nothing has been written yet.
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(archive); err != nil {
		s.logger.Error().Err(err).Msg("Failed to encode export")
		s.InternalError(w, "Failed to encode export")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(buf.Bytes()); err != nil {
		s.logger.Error().Err(err).Msg("Failed to write export response")
	}
}

// handleImportConfig restores tunnel definitions from an archive.
//
// ?mode=merge (default) updates and creates; ?mode=replace additionally
// deletes tunnels absent from the archive. ?dry_run=true returns the intended
// plan without writing anything.
func (s *Server) handleImportConfig(w http.ResponseWriter, r *http.Request) {
	// Requiring an explicit application/json Content-Type closes off a
	// cross-origin attack surface that the wider (pre-existing, out of scope
	// here) CORS posture leaves open: corsMiddleware sends
	// Access-Control-Allow-Origin: * on every route, and auth is disabled
	// entirely unless a JWT secret is configured, so with no auth configured
	// there is nothing else standing between an arbitrary origin and this,
	// the most destructive route in the API. A plain HTML
	// <form enctype="text/plain"> submits a POST with Content-Type: text/plain
	// with no CORS preflight at all (text/plain is a CORS-simple content
	// type), and json.Decoder.Decode happily parses the exact byte stream
	// such a form produces. mime.ParseMediaType is used (rather than a bare
	// string compare) so a legitimate "application/json; charset=utf-8" is
	// still accepted.
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		s.respondJSON(w, http.StatusUnsupportedMediaType, map[string]interface{}{
			"code":    "UNSUPPORTED_MEDIA_TYPE",
			"message": "Content-Type must be application/json",
		})
		return
	}

	if s.storage == nil {
		s.ServiceUnavailableError(w, "Persistent storage is not configured")
		return
	}

	mode, err := backup.ParseMode(r.URL.Query().Get("mode"))
	if err != nil {
		s.BadRequest(w, err.Error())
		return
	}

	dryRun, err := parseBoolQuery(r, "dry_run")
	if err != nil {
		s.BadRequest(w, err.Error())
		return
	}

	var archive backup.Archive
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxImportBytes)).Decode(&archive); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			s.respondJSON(w, http.StatusRequestEntityTooLarge, map[string]interface{}{
				"code":    "PAYLOAD_TOO_LARGE",
				"message": fmt.Sprintf("archive exceeds the %d MiB import limit", maxImportBytes/(1<<20)),
			})
			return
		}
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
			// per-entry specifics, matching the shape of APIError. The code
			// uses the package's own ErrCodeValidation constant so a UI
			// switching on body.code is not tripped up by casing.
			s.respondJSON(w, http.StatusBadRequest, map[string]interface{}{
				"code":    string(ErrCodeValidation),
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

	// Background context: the writes and the reconcile below must outlive the
	// HTTP request, matching handleCreateTunnel (handlers.go:149, "not request
	// context!").
	report, applyErr := backup.Apply(context.Background(), s.storage, plan)

	// Converge the running fleet onto the newly stored desired state. Also
	// context.Background(), for the same reason as the Apply call above.
	//
	// Manager.Reconcile returns an error only when storage is nil or its List
	// call fails; every per-tunnel Start/Stop failure inside applyDesired is
	// logged there and never returned. So neither a nil error here nor the 200
	// response below promises that every tunnel is now running as desired —
	// only that the archive was written to storage and reconciliation was
	// attempted.
	if err := s.manager.Reconcile(context.Background()); err != nil {
		s.logger.Error().Err(err).Msg("Failed to reconcile tunnels after import")
	}

	if applyErr != nil {
		s.logger.Error().Err(applyErr).Msg("Import partially failed")
		// Carry a "message" alongside the report for the same reason as the
		// validation-error path above: parseError only reads body.message, and
		// without one the browser would show a bare "Internal Server Error"
		// and discard the report — the only thing that names which tunnels
		// landed and which did not.
		s.respondJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"code":    "IMPORT_PARTIAL_FAILURE",
			"message": fmt.Sprintf("import partially failed: %d of %d item(s)", report.Failed, len(report.Items)),
			"report":  report,
		})
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

// parseBoolQuery parses a boolean query parameter strictly: an absent
// parameter is false, and any present value must be one strconv.ParseBool
// accepts ("true"/"false", "1"/"0", "T"/"F", etc). A bare `== "true"` string
// comparison would treat any other spelling ("1", "yes", "True") as false, so
// a typo in the one parameter that means "don't touch anything" would
// silently perform a real — and, combined with ?mode=replace, destructive —
// import instead of a dry run.
func parseBoolQuery(r *http.Request, name string) (bool, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return false, nil
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("invalid %s value %q: must be a boolean", name, raw)
	}
	return v, nil
}
