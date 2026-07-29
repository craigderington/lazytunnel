package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

const sampleArchiveJSON = `{"version":1,"exported_at":"2026-07-29T08:40:12Z","source":"lazytunnel/test","tunnels":[]}`

// exportTestServer stands in for the API, asserting the command calls the
// right path.
func exportTestServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/config/export" {
			t.Errorf("got path %q, want /api/v1/config/export", r.URL.Path)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(ts.Close)
	return ts
}

// callExport runs the command against a stub server, restoring the global
// flag state afterwards so tests do not leak into each other.
func callExport(t *testing.T, serverURL, output string) (string, error) {
	t.Helper()

	withTestServerURL(t, serverURL)

	prevOutput := exportOutput
	exportOutput = output
	t.Cleanup(func() { exportOutput = prevOutput })

	var stdout, stderr bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	err := runExport(cmd, nil)
	return stdout.String(), err
}

func TestExportWritesArchiveToStdout(t *testing.T) {
	ts := exportTestServer(t, http.StatusOK, sampleArchiveJSON)

	out, err := callExport(t, ts.URL, "")
	if err != nil {
		t.Fatalf("runExport returned error: %v", err)
	}
	if out != sampleArchiveJSON {
		t.Fatalf("stdout mismatch:\n got %s\nwant %s", out, sampleArchiveJSON)
	}
}

func TestExportWritesFileWithRestrictivePermissions(t *testing.T) {
	ts := exportTestServer(t, http.StatusOK, sampleArchiveJSON)
	path := filepath.Join(t.TempDir(), "backup.json")

	out, err := callExport(t, ts.URL, path)
	if err != nil {
		t.Fatalf("runExport returned error: %v", err)
	}
	if out != "" {
		t.Errorf("stdout should be empty when -o is used, got %q", out)
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read the written file: %v", err)
	}
	if string(contents) != sampleArchiveJSON {
		t.Errorf("file contents mismatch:\n got %s\nwant %s", contents, sampleArchiveJSON)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("failed to stat the written file: %v", err)
	}
	// The archive lists hostnames, usernames and key paths — it must not be
	// world-readable.
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("got file mode %o, want 600", perm)
	}
}

func TestExportReportsServerErrors(t *testing.T) {
	ts := exportTestServer(t, http.StatusInternalServerError, `{"error":"storage unavailable"}`)

	if _, err := callExport(t, ts.URL, ""); err == nil {
		t.Fatal("expected an error for a 500 response, got nil")
	} else if !strings.Contains(err.Error(), "storage unavailable") {
		t.Errorf("error should surface the server's message, got %q", err)
	}
}

func TestExportDoesNotWriteFileOnServerError(t *testing.T) {
	ts := exportTestServer(t, http.StatusInternalServerError, `{"error":"boom"}`)
	path := filepath.Join(t.TempDir(), "backup.json")

	if _, err := callExport(t, ts.URL, path); err == nil {
		t.Fatal("expected an error, got nil")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("a failed export must not leave a file behind")
	}
}

func TestExportFixesPermissionsOnExistingFile(t *testing.T) {
	ts := exportTestServer(t, http.StatusOK, sampleArchiveJSON)
	path := filepath.Join(t.TempDir(), "backup.json")

	// Pre-create the file with world-readable permissions (simulating a previous export).
	if err := os.WriteFile(path, []byte("old content"), 0o644); err != nil {
		t.Fatalf("failed to pre-create file: %v", err)
	}

	// Verify it starts with 0644 permissions.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("failed to stat pre-created file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o644 {
		t.Fatalf("pre-created file should have 0644, got %o", perm)
	}

	// Run the export to overwrite it.
	_, err = callExport(t, ts.URL, path)
	if err != nil {
		t.Fatalf("runExport returned error: %v", err)
	}

	// Verify the file now has 0600 permissions.
	info, err = os.Stat(path)
	if err != nil {
		t.Fatalf("failed to stat file after export: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("exported file should have 0600 after overwriting, got %o", perm)
	}

	// Verify the content was updated.
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if string(contents) != sampleArchiveJSON {
		t.Errorf("file contents mismatch:\n got %s\nwant %s", contents, sampleArchiveJSON)
	}
}
