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

	// Always preview first: the plan is what gets printed, and with --replace
	// it is what the confirmation prompt is based on.
	preview, err := postImport(archive, mode, true)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
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
		if strings.ToLower(strings.TrimSpace(answer)) != "y" {
			fmt.Fprintln(out, "Aborted.")
			return nil
		}
	}

	report, err := postImport(archive, mode, false)
	if err != nil {
		return err
	}

	fmt.Fprint(out, formatImportReport(report))
	return nil
}
