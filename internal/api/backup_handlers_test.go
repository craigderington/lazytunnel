package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/craigderington/lazytunnel/internal/backup"
	"github.com/craigderington/lazytunnel/pkg/types"
)

// backupTestStorage implements tunnel.Storage in memory.
type backupTestStorage struct {
	specs map[string]*types.TunnelSpec
	order []string
}

func newBackupTestStorage(specs ...*types.TunnelSpec) *backupTestStorage {
	s := &backupTestStorage{specs: make(map[string]*types.TunnelSpec, len(specs))}
	for _, spec := range specs {
		s.order = append(s.order, spec.ID)
		s.specs[spec.ID] = spec
	}
	return s
}

// Save, Delete and Get all check ctx.Err() before doing anything, so tests
// can tell a Background context apart from an already-cancelled request
// context: a handler that mistakenly passed r.Context() into backup.Apply or
// Manager.Reconcile (both of which call through to these) would observe
// these calls fail instead of silently succeeding regardless of the context
// it was given.
func (s *backupTestStorage) Save(ctx context.Context, spec *types.TunnelSpec) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, exists := s.specs[spec.ID]; !exists {
		s.order = append(s.order, spec.ID)
	}
	s.specs[spec.ID] = spec
	return nil
}

func (s *backupTestStorage) Update(ctx context.Context, spec *types.TunnelSpec) error {
	return s.Save(ctx, spec)
}

func (s *backupTestStorage) UpdateStatus(ctx context.Context, tunnelID, status string) error {
	return nil
}

func (s *backupTestStorage) UpdateDesiredStatus(ctx context.Context, tunnelID string, status types.DesiredStatus) error {
	return nil
}

func (s *backupTestStorage) Delete(ctx context.Context, tunnelID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	delete(s.specs, tunnelID)
	for i, id := range s.order {
		if id == tunnelID {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
	return nil
}

func (s *backupTestStorage) Get(ctx context.Context, tunnelID string) (*types.TunnelSpec, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if spec, ok := s.specs[tunnelID]; ok {
		return spec, nil
	}
	return nil, context.Canceled
}

func (s *backupTestStorage) List(ctx context.Context) ([]*types.TunnelSpec, error) {
	out := make([]*types.TunnelSpec, 0, len(s.order))
	for _, id := range s.order {
		if spec, ok := s.specs[id]; ok {
			out = append(out, spec)
		}
	}
	return out, nil
}

func (s *backupTestStorage) ListByAgent(ctx context.Context, agentID string) ([]*types.TunnelSpec, error) {
	return s.List(ctx)
}

func (s *backupTestStorage) Close() error { return nil }

func backupTestSpec(id, name string, port int) *types.TunnelSpec {
	return &types.TunnelSpec{
		ID:            id,
		Name:          name,
		Owner:         "admin",
		AgentID:       "remote-agent", // never connects on this node
		DesiredStatus: types.DesiredStatusStopped,
		Type:          types.TunnelTypeLocal,
		Hops:          []types.Hop{{Host: "bastion", Port: 22, User: "deploy", AuthMethod: types.AuthMethodKey}},
		LocalPort:     port,
		RemoteHost:    "db.internal",
		RemotePort:    5432,
		KeepAlive:     30 * time.Second,
		MaxRetries:    5,
	}
}

// failOnSaveStorage wraps backupTestStorage and fails Save for a chosen set
// of tunnel names, so tests can exercise Apply's partial-failure path without
// reaching into backup.Apply directly.
type failOnSaveStorage struct {
	*backupTestStorage
	failNames map[string]bool
}

func (s *failOnSaveStorage) Save(ctx context.Context, spec *types.TunnelSpec) error {
	if s.failNames[spec.Name] {
		return fmt.Errorf("simulated write failure for %s", spec.Name)
	}
	return s.backupTestStorage.Save(ctx, spec)
}

func newBackupTestServer(t *testing.T, store *backupTestStorage) *Server {
	t.Helper()
	return NewServer(context.Background(), Config{
		Addr:    ":0",
		Logger:  zerolog.Nop(),
		Storage: store,
		Version: "test",
	})
}

func TestHandleExportConfigReturnsArchive(t *testing.T) {
	srv := newBackupTestServer(t, newBackupTestStorage(backupTestSpec("id-1", "prod-db", 5432)))

	rec := httptest.NewRecorder()
	srv.handleExportConfig(rec, httptest.NewRequest(http.MethodGet, "/api/v1/config/export", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if cd := rec.Header().Get("Content-Disposition"); cd == "" {
		t.Error("expected a Content-Disposition header so the UI can download by link")
	}
	if cl := rec.Header().Get("Content-Length"); cl != strconv.Itoa(rec.Body.Len()) {
		t.Errorf("got Content-Length %q, want %d (the actual body size) — without it the response is chunked and a browser download shows no size or progress bar", cl, rec.Body.Len())
	}

	var archive backup.Archive
	if err := json.Unmarshal(rec.Body.Bytes(), &archive); err != nil {
		t.Fatalf("response is not a valid archive: %v", err)
	}
	if archive.Version != backup.SchemaVersion {
		t.Errorf("got version %d, want %d", archive.Version, backup.SchemaVersion)
	}
	if archive.Source != "lazytunnel/test" {
		t.Errorf("got Source %q, want %q (stamped from Config.Version)", archive.Source, "lazytunnel/test")
	}
	if len(archive.Tunnels) != 1 || archive.Tunnels[0].Name != "prod-db" {
		t.Fatalf("unexpected tunnels in archive: %+v", archive.Tunnels)
	}
}

func TestNewServerDefaultsVersionToDev(t *testing.T) {
	srv := NewServer(context.Background(), Config{
		Addr:   ":0",
		Logger: zerolog.Nop(),
		// Version deliberately omitted.
	})

	if srv.version != "dev" {
		t.Errorf("got version %q, want %q when Config.Version is empty", srv.version, "dev")
	}
}

func postImport(t *testing.T, srv *Server, query string, archive backup.Archive) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(archive)
	if err != nil {
		t.Fatalf("failed to marshal archive: %v", err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/config/import"+query, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	srv.handleImportConfig(rec, req)
	return rec
}

func decodeReport(t *testing.T, rec *httptest.ResponseRecorder) backup.Report {
	t.Helper()
	var report backup.Report
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatalf("response is not a report: %v (body: %s)", err, rec.Body.String())
	}
	return report
}

func TestHandleImportConfigMergesByDefault(t *testing.T) {
	store := newBackupTestStorage(backupTestSpec("id-1", "prod-db", 5432))
	srv := newBackupTestServer(t, store)

	archive := backup.Archive{
		Version: backup.SchemaVersion,
		Tunnels: []backup.TunnelEntry{backup.EntryFromSpec(backupTestSpec("", "staging-api", 8081))},
	}

	rec := postImport(t, srv, "", archive)
	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200: %s", rec.Code, rec.Body.String())
	}

	report := decodeReport(t, rec)
	if report.Created != 1 {
		t.Errorf("got Created %d, want 1", report.Created)
	}
	if report.Deleted != 0 {
		t.Errorf("got Deleted %d, want 0 — merge must never delete", report.Deleted)
	}
	if len(store.specs) != 2 {
		t.Errorf("got %d stored tunnels, want 2", len(store.specs))
	}
}

func TestHandleImportConfigReplaceDeletesAbsentTunnels(t *testing.T) {
	store := newBackupTestStorage(backupTestSpec("id-1", "prod-db", 5432))
	srv := newBackupTestServer(t, store)

	archive := backup.Archive{
		Version: backup.SchemaVersion,
		Tunnels: []backup.TunnelEntry{backup.EntryFromSpec(backupTestSpec("", "staging-api", 8081))},
	}

	rec := postImport(t, srv, "?mode=replace", archive)
	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200: %s", rec.Code, rec.Body.String())
	}

	report := decodeReport(t, rec)
	if report.Deleted != 1 {
		t.Errorf("got Deleted %d, want 1", report.Deleted)
	}
	if _, exists := store.specs["id-1"]; exists {
		t.Error("prod-db should have been deleted in replace mode")
	}
}

func TestHandleImportConfigDryRunWritesNothing(t *testing.T) {
	store := newBackupTestStorage(backupTestSpec("id-1", "prod-db", 5432))
	srv := newBackupTestServer(t, store)

	archive := backup.Archive{
		Version: backup.SchemaVersion,
		Tunnels: []backup.TunnelEntry{backup.EntryFromSpec(backupTestSpec("", "staging-api", 8081))},
	}

	rec := postImport(t, srv, "?dry_run=true", archive)
	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200: %s", rec.Code, rec.Body.String())
	}

	report := decodeReport(t, rec)
	if !report.DryRun {
		t.Error("report should be flagged as a dry run")
	}
	if report.Created != 1 {
		t.Errorf("got Created %d, want 1 (as an intention)", report.Created)
	}
	if len(store.specs) != 1 {
		t.Errorf("got %d stored tunnels, want 1 — a dry run must not write", len(store.specs))
	}
}

func TestHandleImportConfigParsesDryRunStrictly(t *testing.T) {
	// Query.Get("dry_run") == "true" treats any other spelling as false, so a
	// typo in the one parameter that means "don't touch anything" — combined
	// with ?mode=replace — would silently perform a real, destructive import.
	// strconv.ParseBool must be used instead, and an unparseable value must be
	// rejected rather than silently treated as false.
	tests := []struct {
		name       string
		query      string
		wantStatus int
		wantDryRun bool
	}{
		{name: "absent defaults to false", query: "", wantStatus: http.StatusOK, wantDryRun: false},
		{name: "true", query: "?dry_run=true", wantStatus: http.StatusOK, wantDryRun: true},
		{name: "1", query: "?dry_run=1", wantStatus: http.StatusOK, wantDryRun: true},
		{name: "TRUE", query: "?dry_run=TRUE", wantStatus: http.StatusOK, wantDryRun: true},
		{name: "false", query: "?dry_run=false", wantStatus: http.StatusOK, wantDryRun: false},
		{name: "0", query: "?dry_run=0", wantStatus: http.StatusOK, wantDryRun: false},
		{name: "yes is rejected, not silently treated as a real import", query: "?dry_run=yes", wantStatus: http.StatusBadRequest},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := newBackupTestStorage()
			srv := newBackupTestServer(t, store)
			archive := backup.Archive{
				Version: backup.SchemaVersion,
				Tunnels: []backup.TunnelEntry{backup.EntryFromSpec(backupTestSpec("", "dry-run-probe", 6100))},
			}

			rec := postImport(t, srv, tc.query, archive)
			if rec.Code != tc.wantStatus {
				t.Fatalf("got status %d, want %d: %s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.wantStatus != http.StatusOK {
				return
			}

			report := decodeReport(t, rec)
			if report.DryRun != tc.wantDryRun {
				t.Errorf("got DryRun=%v, want %v", report.DryRun, tc.wantDryRun)
			}
			wrote := len(store.specs) == 1
			if tc.wantDryRun && wrote {
				t.Error("dry_run=true must not write")
			}
			if !tc.wantDryRun && !wrote {
				t.Error("dry_run=false must write")
			}
		})
	}
}

func TestHandleImportConfigRejectsOversizedBodyWith413(t *testing.T) {
	srv := newBackupTestServer(t, newBackupTestStorage())

	// A single invalid byte would fail the JSON syntax check on the first
	// token, long before MaxBytesReader's limit is ever reached. To actually
	// exercise the size limit, the payload must be syntactically valid enough
	// that the decoder keeps reading through it — a giant string value does
	// that, since the decoder must consume it up to the closing quote.
	oversized := append([]byte(`{"source":"`), bytes.Repeat([]byte("a"), maxImportBytes+1)...)
	oversized = append(oversized, []byte(`"}`)...)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/config/import", bytes.NewReader(oversized))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.handleImportConfig(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("got status %d, want 413: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if strings.Contains(body.Message, "http: request body too large") {
		t.Error("message leaks the raw Go MaxBytesReader string instead of stating the limit in plain language")
	}
	if !strings.Contains(body.Message, "10 MiB") {
		t.Errorf("message should state the 10 MiB limit, got %q", body.Message)
	}
}

// TestHandleImportConfigRejectsNonJSONContentTypeWith415 guards against a
// cross-origin, preflight-free POST reaching this route: an HTML form with
// enctype="text/plain" submits without a CORS preflight (text/plain is a
// CORS-simple content type) and json.Decoder.Decode accepts the exact byte
// stream such a form produces. corsMiddleware no longer sends
// Access-Control-Allow-Origin: * unconditionally — it only echoes a
// wildcard when server.cors.allowed_origins is explicitly configured with
// "*" — but a simple request like this reaches the handler and mutates
// state regardless of what the response header says; the browser only
// blocks the *response* from being read, not the request from being sent.
// So for a deployment that does set "*" with auth disabled, a required
// Content-Type remains the only thing standing between an arbitrary origin
// and the most destructive route in the API. Kept as defence-in-depth.
func TestHandleImportConfigRejectsNonJSONContentTypeWith415(t *testing.T) {
	store := newBackupTestStorage(backupTestSpec("id-1", "prod-db", 5432))
	srv := newBackupTestServer(t, store)

	archive := backup.Archive{
		Version: backup.SchemaVersion,
		Tunnels: []backup.TunnelEntry{backup.EntryFromSpec(backupTestSpec("", "staging-api", 8081))},
	}
	body, err := json.Marshal(archive)
	if err != nil {
		t.Fatalf("failed to marshal archive: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/config/import", bytes.NewReader(body))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	srv.handleImportConfig(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("got status %d, want 415: %s", rec.Code, rec.Body.String())
	}
	if len(store.specs) != 1 {
		t.Fatalf("a rejected Content-Type must write nothing, got %d stored tunnels", len(store.specs))
	}
}

func TestHandleImportConfigPartialFailureIncludesMessageAndReport(t *testing.T) {
	// web/src/api/client.ts's parseError reads body.message, falling back to
	// the bare statusText "Internal Server Error" otherwise — which would
	// also discard the report, the only thing that names which tunnels
	// landed and which did not on a partial failure.
	store := &failOnSaveStorage{
		backupTestStorage: newBackupTestStorage(),
		failNames:         map[string]bool{"will-fail": true},
	}
	srv := NewServer(context.Background(), Config{
		Addr:    ":0",
		Logger:  zerolog.Nop(),
		Storage: store,
		Version: "test",
	})

	archive := backup.Archive{
		Version: backup.SchemaVersion,
		Tunnels: []backup.TunnelEntry{
			backup.EntryFromSpec(backupTestSpec("", "will-fail", 6200)),
			backup.EntryFromSpec(backupTestSpec("", "will-succeed", 6201)),
		},
	}

	rec := postImport(t, srv, "", archive)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("got status %d, want 500: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Message string        `json:"message"`
		Report  backup.Report `json:"report"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body.Message == "" {
		t.Error("500 response must carry a non-empty message field")
	}
	if len(body.Report.Items) != 2 {
		t.Fatalf("500 response must still carry the full report, got %d item(s)", len(body.Report.Items))
	}
	if body.Report.Failed != 1 {
		t.Errorf("got Failed %d, want 1", body.Report.Failed)
	}
	landed := false
	for _, item := range body.Report.Items {
		if item.Name == "will-succeed" && item.Error == "" {
			landed = true
		}
	}
	if !landed {
		t.Error("report should show will-succeed landed even though will-fail did not")
	}
}

func TestHandleImportConfigAdoptsImportedTunnelViaReconcile(t *testing.T) {
	// Apply only writes to storage; Manager.Get only finds a tunnel that
	// Reconcile has adopted into its in-memory map via upsertTunnel. If
	// handleImportConfig stopped calling Reconcile after Apply, this tunnel
	// would land in storage but stay invisible to the manager, and this
	// lookup would fail.
	store := newBackupTestStorage()
	srv := newBackupTestServer(t, store)

	archive := backup.Archive{
		Version: backup.SchemaVersion,
		Tunnels: []backup.TunnelEntry{backup.EntryFromSpec(backupTestSpec("", "reconciled-tunnel", 9090))},
	}

	rec := postImport(t, srv, "", archive)
	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200: %s", rec.Code, rec.Body.String())
	}

	report := decodeReport(t, rec)
	if len(report.Items) != 1 {
		t.Fatalf("expected exactly one report item, got %d", len(report.Items))
	}
	id := report.Items[0].ID

	if _, err := srv.manager.Get(id); err != nil {
		t.Fatalf("manager does not know about imported tunnel %s — Reconcile did not run after import: %v", id, err)
	}
}

func TestHandleImportConfigWritesAndReconcilesWithBackgroundContext(t *testing.T) {
	// backupTestStorage.Save and .Get both fail if given a context whose
	// Err() is non-nil. Simulating a client that disconnects before the
	// handler finishes pins that Apply and Reconcile use context.Background()
	// rather than r.Context(): if either call site were swapped, the write
	// (Apply -> Save) or the adoption (Reconcile -> Get, in its "isNew"
	// re-verify branch) would fail against the already-cancelled context,
	// even though every other test in this file passes an uncancelled
	// context and would not catch the regression.
	store := newBackupTestStorage()
	srv := newBackupTestServer(t, store)

	archive := backup.Archive{
		Version: backup.SchemaVersion,
		Tunnels: []backup.TunnelEntry{backup.EntryFromSpec(backupTestSpec("", "outlives-request", 7000))},
	}
	body, err := json.Marshal(archive)
	if err != nil {
		t.Fatalf("failed to marshal archive: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // the request context is already dead before the handler runs

	req := httptest.NewRequest(http.MethodPost, "/api/v1/config/import", bytes.NewReader(body)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.handleImportConfig(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200 even though the request context was already cancelled: %s", rec.Code, rec.Body.String())
	}
	if len(store.specs) != 1 {
		t.Fatalf("import should have written to storage via a Background context, got %d stored tunnels", len(store.specs))
	}

	report := decodeReport(t, rec)
	if len(report.Items) != 1 {
		t.Fatalf("expected exactly one report item, got %d", len(report.Items))
	}
	if _, err := srv.manager.Get(report.Items[0].ID); err != nil {
		t.Fatalf("manager should have adopted the tunnel via a Background-context Reconcile even though the request was cancelled: %v", err)
	}
}

func TestHandleImportConfigRejectsBadVersion(t *testing.T) {
	srv := newBackupTestServer(t, newBackupTestStorage())

	archive := backup.Archive{
		Version: 99,
		Tunnels: []backup.TunnelEntry{backup.EntryFromSpec(backupTestSpec("", "staging-api", 8081))},
	}

	rec := postImport(t, srv, "", archive)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got status %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleImportConfigRejectsUnknownMode(t *testing.T) {
	srv := newBackupTestServer(t, newBackupTestStorage())
	archive := backup.Archive{Version: backup.SchemaVersion}

	rec := postImport(t, srv, "?mode=obliterate", archive)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got status %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleImportConfigReportsStoredNameConflictAsConflict(t *testing.T) {
	// Two stored tunnels whose names differ only by surrounding whitespace make
	// an archive entry genuinely ambiguous, so backup.Plan refuses. The fault is
	// in server state, not in the user's file — reporting 400 would tell them a
	// perfectly valid archive is broken.
	store := newBackupTestStorage(
		backupTestSpec("id-1", "prod-db", 5432),
		backupTestSpec("id-2", "prod-db ", 5433),
	)
	srv := newBackupTestServer(t, store)

	archive := backup.Archive{
		Version: backup.SchemaVersion,
		Tunnels: []backup.TunnelEntry{backup.EntryFromSpec(backupTestSpec("", "prod-db", 5432))},
	}

	rec := postImport(t, srv, "", archive)
	if rec.Code != http.StatusConflict {
		t.Fatalf("got status %d, want 409: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "prod-db") {
		t.Errorf("response should name the conflicting tunnels, got %s", rec.Body.String())
	}
}

func TestHandleImportConfigReturnsValidationDetails(t *testing.T) {
	srv := newBackupTestServer(t, newBackupTestStorage())

	archive := backup.Archive{
		Version: backup.SchemaVersion,
		Tunnels: []backup.TunnelEntry{{Name: "", Type: "sideways"}},
	}

	rec := postImport(t, srv, "", archive)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got status %d, want 400", rec.Code)
	}

	var body struct {
		Code    string              `json:"code"`
		Message string              `json:"message"`
		Details []backup.EntryError `json:"details"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(body.Details) < 2 {
		t.Fatalf("got %d details, want every problem reported at once", len(body.Details))
	}
	// web/src/api/client.ts reads body.message and falls back to statusText, so
	// an empty message here means the UI shows a bare "Bad Request".
	if body.Message == "" {
		t.Error("response must carry a non-empty message field or the web UI cannot show why the import failed")
	}
	if body.Code != string(ErrCodeValidation) {
		t.Errorf("got code %q, want %q (the package's own constant, so a UI switching on it isn't tripped by casing)", body.Code, ErrCodeValidation)
	}
}

func TestExportImportRoundTripIsAllSkip(t *testing.T) {
	store := newBackupTestStorage(backupTestSpec("id-1", "prod-db", 5432))
	srv := newBackupTestServer(t, store)

	rec := httptest.NewRecorder()
	srv.handleExportConfig(rec, httptest.NewRequest(http.MethodGet, "/api/v1/config/export", nil))

	var archive backup.Archive
	if err := json.Unmarshal(rec.Body.Bytes(), &archive); err != nil {
		t.Fatalf("export is not a valid archive: %v", err)
	}

	imported := postImport(t, srv, "?mode=replace", archive)
	if imported.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200: %s", imported.Code, imported.Body.String())
	}

	report := decodeReport(t, imported)
	if report.Skipped != 1 || report.Created != 0 || report.Updated != 0 || report.Deleted != 0 {
		t.Fatalf("re-importing an unmodified export should change nothing, got %+v", report)
	}
}
