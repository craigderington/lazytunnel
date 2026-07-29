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

const sampleReportJSON = `{
	"mode": "replace",
	"dry_run": true,
	"items": [
		{"action":"update","name":"prod-db","id":"id-1"},
		{"action":"create","name":"staging-api","id":"id-2"},
		{"action":"skip","name":"socks-jump","id":"id-3","reason":"identical to stored tunnel"},
		{"action":"delete","name":"old-bastion","id":"id-4","reason":"not present in archive"}
	],
	"created": 1,
	"updated": 1,
	"skipped": 1,
	"deleted": 1,
	"failed": 0
}`

func TestParseImportReport(t *testing.T) {
	report, err := parseImportReport([]byte(sampleReportJSON))
	if err != nil {
		t.Fatalf("parseImportReport returned error: %v", err)
	}
	if report.Mode != "replace" {
		t.Errorf("got Mode %q, want replace", report.Mode)
	}
	if !report.DryRun {
		t.Error("got DryRun false, want true")
	}
	if len(report.Items) != 4 {
		t.Fatalf("got %d items, want 4", len(report.Items))
	}
	if report.Items[0].Action != "update" || report.Items[0].Name != "prod-db" {
		t.Errorf("unexpected first item: %+v", report.Items[0])
	}
}

func TestParseImportReportRejectsMalformed(t *testing.T) {
	if _, err := parseImportReport([]byte(`not json`)); err == nil {
		t.Fatal("expected an error for malformed JSON, got nil")
	}
}

func TestCountDeletions(t *testing.T) {
	report, err := parseImportReport([]byte(sampleReportJSON))
	if err != nil {
		t.Fatalf("parseImportReport returned error: %v", err)
	}
	if got := countDeletions(report); got != 1 {
		t.Fatalf("got %d deletions, want 1", got)
	}
}

func TestCountDeletionsIsZeroForMerge(t *testing.T) {
	report, err := parseImportReport([]byte(`{"mode":"merge","items":[{"action":"create","name":"a"}]}`))
	if err != nil {
		t.Fatalf("parseImportReport returned error: %v", err)
	}
	if got := countDeletions(report); got != 0 {
		t.Fatalf("got %d deletions, want 0", got)
	}
}

func TestFormatImportReportListsEveryItem(t *testing.T) {
	report, err := parseImportReport([]byte(sampleReportJSON))
	if err != nil {
		t.Fatalf("parseImportReport returned error: %v", err)
	}

	out := formatImportReport(report)
	for _, want := range []string{"prod-db", "staging-api", "socks-jump", "old-bastion", "update", "create", "skip", "DELETE"} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing %q:\n%s", want, out)
		}
	}
}

func TestFormatImportReportShowsSummaryCounts(t *testing.T) {
	report, err := parseImportReport([]byte(sampleReportJSON))
	if err != nil {
		t.Fatalf("parseImportReport returned error: %v", err)
	}

	out := formatImportReport(report)
	for _, want := range []string{"1 created", "1 updated", "1 deleted"} {
		if !strings.Contains(out, want) {
			t.Errorf("summary is missing %q:\n%s", want, out)
		}
	}
}

func TestFormatImportReportSurfacesErrors(t *testing.T) {
	report, err := parseImportReport([]byte(`{
		"mode":"merge",
		"items":[{"action":"create","name":"broken","error":"disk on fire"}],
		"failed":1
	}`))
	if err != nil {
		t.Fatalf("parseImportReport returned error: %v", err)
	}

	out := formatImportReport(report)
	if !strings.Contains(out, "disk on fire") {
		t.Errorf("output must surface the failure message:\n%s", out)
	}
	if !strings.Contains(out, "1 failed") {
		t.Errorf("summary must report failures:\n%s", out)
	}
}

// --- Tests exercising runImport itself. The pure-function tests above cover
// parsing and formatting, but say nothing about whether the command actually
// previews before applying, or actually stops when the user declines. These
// tests establish that behavior by counting requests and recording the query
// string of each one against a real httptest.Server, and by driving the
// confirmation prompt through cmd.InOrStdin().

// mergeReportJSON has no deletions: used for the plain preview/merge path.
const mergeReportJSON = `{
	"mode": "merge",
	"dry_run": true,
	"items": [{"action":"create","name":"staging-api","id":"id-2"}],
	"created": 1,
	"updated": 0,
	"skipped": 0,
	"deleted": 0,
	"failed": 0
}`

// replaceReportWithDeletionJSON has one deletion: used to exercise the
// confirmation prompt (--replace only deletes, so only --replace plans ever
// need confirming).
const replaceReportWithDeletionJSON = `{
	"mode": "replace",
	"dry_run": true,
	"items": [
		{"action":"create","name":"staging-api","id":"id-2"},
		{"action":"delete","name":"old-bastion","id":"id-4","reason":"not present in archive"}
	],
	"created": 1,
	"updated": 0,
	"skipped": 0,
	"deleted": 1,
	"failed": 0
}`

// importRequest records one call made to the stub import endpoint.
type importRequest struct {
	path  string
	query string
}

// importTestServer stands in for the API. It records every request it
// receives and always answers with reportBody, regardless of dry_run — the
// tests care about how many requests runImport makes and what their query
// strings look like, not about the server's own merge/replace logic (that
// belongs to Task 7's tests, not this one).
func importTestServer(t *testing.T, reportBody string) (*httptest.Server, *[]importRequest) {
	t.Helper()
	var requests []importRequest
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, importRequest{path: r.URL.Path, query: r.URL.RawQuery})
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(reportBody))
	}))
	t.Cleanup(ts.Close)
	return ts, &requests
}

// callImport writes archiveBody to a temp file and runs runImport against it,
// following the callExport pattern for wiring cobra's in/out streams and
// resetting package-level flag state afterwards.
func callImport(t *testing.T, serverURL, archiveBody, stdin string) (stdout string, err error) {
	t.Helper()

	withTestServerURL(t, serverURL)

	prevReplace, prevDryRun, prevYes := importReplace, importDryRun, importYes
	t.Cleanup(func() {
		importReplace, importDryRun, importYes = prevReplace, prevDryRun, prevYes
	})

	path := filepath.Join(t.TempDir(), "archive.json")
	if err := os.WriteFile(path, []byte(archiveBody), 0o600); err != nil {
		t.Fatalf("failed to write archive fixture: %v", err)
	}

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader(stdin))

	err = runImport(cmd, []string{path})
	return out.String(), err
}

func TestImportDryRunMakesExactlyOneRequest(t *testing.T) {
	ts, requests := importTestServer(t, replaceReportWithDeletionJSON)

	importReplace = true
	importDryRun = true
	importYes = false

	out, err := callImport(t, ts.URL, `{"tunnels":[]}`, "")
	if err != nil {
		t.Fatalf("callImport returned error: %v", err)
	}

	if len(*requests) != 1 {
		t.Fatalf("got %d requests, want exactly 1 (the dry-run preview); requests: %+v", len(*requests), *requests)
	}
	if !strings.Contains((*requests)[0].query, "dry_run=true") {
		t.Errorf("the one request must be the dry-run request, got query %q", (*requests)[0].query)
	}
	if !strings.Contains(out, "Dry run: nothing was written.") {
		t.Errorf("output must say nothing was written:\n%s", out)
	}
}

func TestImportDeclineAbortsWithoutApplying(t *testing.T) {
	ts, requests := importTestServer(t, replaceReportWithDeletionJSON)

	importReplace = true
	importDryRun = false
	importYes = false

	// Anything other than "y" must decline.
	out, err := callImport(t, ts.URL, `{"tunnels":[]}`, "n\n")
	if err != nil {
		t.Fatalf("callImport returned error: %v", err)
	}

	if len(*requests) != 1 {
		t.Fatalf("got %d requests, want exactly 1 (the preview only); an apply request must not follow a decline; requests: %+v", len(*requests), *requests)
	}
	if !strings.Contains(out, "Aborted.") {
		t.Errorf("declining must print an abort message:\n%s", out)
	}
}

func TestImportYesSkipsPromptButApplies(t *testing.T) {
	ts, requests := importTestServer(t, replaceReportWithDeletionJSON)

	importReplace = true
	importDryRun = false
	importYes = true

	out, err := callImport(t, ts.URL, `{"tunnels":[]}`, "")
	if err != nil {
		t.Fatalf("callImport returned error: %v", err)
	}

	if len(*requests) != 2 {
		t.Fatalf("got %d requests, want exactly 2 (preview + apply); requests: %+v", len(*requests), *requests)
	}
	if !strings.Contains((*requests)[0].query, "dry_run=true") {
		t.Errorf("first request must be the dry-run preview, got query %q", (*requests)[0].query)
	}
	if strings.Contains((*requests)[1].query, "dry_run=true") {
		t.Errorf("second request must be the real apply (no dry_run=true), got query %q", (*requests)[1].query)
	}
	if strings.Contains(out, "Continue?") {
		t.Errorf("--yes must skip the confirmation prompt, but it appeared:\n%s", out)
	}
}

func TestImportAcceptAppliesAfterPrompt(t *testing.T) {
	ts, requests := importTestServer(t, replaceReportWithDeletionJSON)

	importReplace = true
	importDryRun = false
	importYes = false

	out, err := callImport(t, ts.URL, `{"tunnels":[]}`, "y\n")
	if err != nil {
		t.Fatalf("callImport returned error: %v", err)
	}

	if len(*requests) != 2 {
		t.Fatalf("got %d requests, want exactly 2 (preview + apply) after answering y; requests: %+v", len(*requests), *requests)
	}
	if strings.Contains(out, "Aborted.") {
		t.Errorf("answering y must not abort:\n%s", out)
	}
}

func TestImportMergeWithoutDeletionsNeverPrompts(t *testing.T) {
	ts, requests := importTestServer(t, mergeReportJSON)

	importReplace = false
	importDryRun = false
	importYes = false

	// No stdin provided at all: if the command tried to read a confirmation
	// here, ReadString would hit EOF immediately and (per the code) treat
	// that as a decline, aborting the run. A merge with no deletions must
	// apply without ever consulting stdin.
	out, err := callImport(t, ts.URL, "", "")
	if err != nil {
		t.Fatalf("callImport returned error: %v", err)
	}

	if len(*requests) != 2 {
		t.Fatalf("got %d requests, want exactly 2 (preview + apply); requests: %+v", len(*requests), *requests)
	}
	if strings.Contains(out, "Continue?") || strings.Contains(out, "Aborted.") {
		t.Errorf("a merge with no deletions must not prompt:\n%s", out)
	}
}
