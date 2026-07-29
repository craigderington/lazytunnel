package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

func (s *backupTestStorage) Save(ctx context.Context, spec *types.TunnelSpec) error {
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

	var archive backup.Archive
	if err := json.Unmarshal(rec.Body.Bytes(), &archive); err != nil {
		t.Fatalf("response is not a valid archive: %v", err)
	}
	if archive.Version != backup.SchemaVersion {
		t.Errorf("got version %d, want %d", archive.Version, backup.SchemaVersion)
	}
	if len(archive.Tunnels) != 1 || archive.Tunnels[0].Name != "prod-db" {
		t.Fatalf("unexpected tunnels in archive: %+v", archive.Tunnels)
	}
}

func postImport(t *testing.T, srv *Server, query string, archive backup.Archive) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(archive)
	if err != nil {
		t.Fatalf("failed to marshal archive: %v", err)
	}
	rec := httptest.NewRecorder()
	srv.handleImportConfig(rec, httptest.NewRequest(http.MethodPost, "/api/v1/config/import"+query, bytes.NewReader(body)))
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
