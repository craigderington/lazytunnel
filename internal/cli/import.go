package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	importReplace bool
	importDryRun  bool
	importYes     bool
)

var importCmd = &cobra.Command{
	Use:   "import FILE",
	Short: "Restore tunnel definitions from a backup archive",
	Long: `Restore tunnel definitions from an archive produced by 'tunnelctl export'.

By default the import merges: tunnels matching by name are updated in place,
missing ones are created, and anything not mentioned in the archive is left
alone. Merging never deletes.

With --replace, tunnels absent from the archive are deleted so the server
mirrors the file exactly. Deletions are always previewed and confirmed unless
--yes is given.

  tunnelctl import tunnels.json
  tunnelctl import --dry-run tunnels.json
  tunnelctl import --replace tunnels.json`,
	Args: cobra.ExactArgs(1),
	RunE: runImport,
}

func init() {
	importCmd.Flags().BoolVar(&importReplace, "replace", false,
		"delete tunnels that are not present in the archive")
	importCmd.Flags().BoolVar(&importDryRun, "dry-run", false,
		"show what would change without writing anything")
	importCmd.Flags().BoolVarP(&importYes, "yes", "y", false,
		"skip the confirmation prompt for deletions")
}

// importItem mirrors one entry of the server's import report.
type importItem struct {
	Action string `json:"action"`
	Name   string `json:"name"`
	ID     string `json:"id"`
	Reason string `json:"reason"`
	Error  string `json:"error"`
}

// importReport mirrors the JSON returned by POST /api/v1/config/import.
type importReport struct {
	Mode    string       `json:"mode"`
	DryRun  bool         `json:"dry_run"`
	Items   []importItem `json:"items"`
	Created int          `json:"created"`
	Updated int          `json:"updated"`
	Skipped int          `json:"skipped"`
	Deleted int          `json:"deleted"`
	Failed  int          `json:"failed"`
}

func parseImportReport(body []byte) (*importReport, error) {
	var report importReport
	if err := json.Unmarshal(body, &report); err != nil {
		return nil, fmt.Errorf("failed to parse import report: %w", err)
	}
	return &report, nil
}

func countDeletions(report *importReport) int {
	n := 0
	for _, item := range report.Items {
		if item.Action == "delete" {
			n++
		}
	}
	return n
}

// formatImportReport renders a report as an aligned table plus a summary line.
// Deletions are uppercased so they stand out before a confirmation prompt.
func formatImportReport(report *importReport) string {
	var buf bytes.Buffer
	w := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)

	for _, item := range report.Items {
		action := item.Action
		if action == "delete" {
			action = "DELETE"
		}

		note := item.Reason
		if item.Error != "" {
			note = "ERROR: " + item.Error
		}

		fmt.Fprintf(w, "  %s\t%s\t%s\n", action, item.Name, note)
	}
	w.Flush()

	parts := []string{
		fmt.Sprintf("%d created", report.Created),
		fmt.Sprintf("%d updated", report.Updated),
		fmt.Sprintf("%d skipped", report.Skipped),
		fmt.Sprintf("%d deleted", report.Deleted),
	}
	if report.Failed > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", report.Failed))
	}

	fmt.Fprintf(&buf, "\n%s\n", strings.Join(parts, ", "))
	return buf.String()
}

// importErrorBody mirrors the server's error envelope for a failed import
// request. On a partially-completed apply, internal/api/backup_handlers.go
// deliberately nests the report that was produced before the failure under
// "report" — per its own comment, that report is "the only thing that names
// which tunnels landed and which did not". When present, postImport surfaces
// it so runImport can render it through formatImportReport instead of
// dumping the raw JSON envelope.
type importErrorBody struct {
	Code    string        `json:"code"`
	Message string        `json:"message"`
	Report  *importReport `json:"report"`
}

// postImport returns the parsed report on success. On failure it returns a
// non-nil error; if the server's error body carried a partial report (see
// importErrorBody), that report is also returned alongside the error so the
// caller can render it before surfacing the failure. Callers must check the
// returned report even when err != nil.
func postImport(archive []byte, mode string, dryRun bool) (*importReport, error) {
	url := fmt.Sprintf("%s/api/v1/config/import?mode=%s", viper.GetString("server"), mode)
	if dryRun {
		url += "&dry_run=true"
	}

	resp, err := http.Post(url, "application/json", bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("failed to import config: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read import response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errBody importErrorBody
		if json.Unmarshal(body, &errBody) == nil && errBody.Report != nil {
			msg := errBody.Message
			if msg == "" {
				msg = fmt.Sprintf("HTTP %d", resp.StatusCode)
			}
			return errBody.Report, fmt.Errorf("import failed: %s", msg)
		}
		return nil, fmt.Errorf("import failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	return parseImportReport(body)
}

func runImport(cmd *cobra.Command, args []string) error {
	archive, err := os.ReadFile(args[0])
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", args[0], err)
	}

	mode := "merge"
	if importReplace {
		mode = "replace"
	}

	out := cmd.OutOrStdout()

	// Always preview first: the plan is what gets printed, and with --replace
	// it is what the confirmation prompt is based on.
	preview, err := postImport(archive, mode, true)
	if err != nil {
		if preview != nil {
			fmt.Fprintln(out, "Plan:")
			fmt.Fprint(out, formatImportReport(preview))
		}
		return err
	}

	fmt.Fprintln(out, "Plan:")
	fmt.Fprint(out, formatImportReport(preview))

	if importDryRun {
		fmt.Fprintln(out, "Dry run: nothing was written.")
		return nil
	}

	if deletions := countDeletions(preview); deletions > 0 && !importYes {
		fmt.Fprintf(out, "\n--replace deletes %d tunnel(s). Continue? [y/N] ", deletions)

		answer, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
		if err != nil && err != io.EOF {
			return fmt.Errorf("failed to read confirmation: %w", err)
		}
		// An immediate EOF with no answer at all means there was nobody
		// there to ask — stdin is not a terminal (e.g. cron). That is
		// different from a deliberate "n": silently treating it as a
		// decline would exit 0, so a scheduled --replace would report
		// success while having deleted nothing. Fail loudly instead and
		// name the escape hatch.
		if err == io.EOF && strings.TrimSpace(answer) == "" {
			return fmt.Errorf("cannot confirm deletions: stdin is not interactive; re-run with --yes to proceed")
		}
		if strings.ToLower(strings.TrimSpace(answer)) != "y" {
			fmt.Fprintln(out, "Aborted.")
			return nil
		}
	}

	report, err := postImport(archive, mode, false)
	if err != nil {
		if report != nil {
			fmt.Fprintln(out, "Applied:")
			fmt.Fprint(out, formatImportReport(report))
		}
		return err
	}

	fmt.Fprintln(out, "Applied:")
	fmt.Fprint(out, formatImportReport(report))
	return nil
}
