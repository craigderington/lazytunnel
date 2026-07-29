package cli

import (
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var exportOutput string

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export all tunnel definitions to a backup archive",
	Long: `Export every tunnel definition on the server as a versioned JSON archive.

Writes to stdout by default, so it pipes and redirects:

  tunnelctl export > tunnels.json
  tunnelctl export -o tunnels.json

The archive contains hostnames, SSH usernames and key paths, but no key
material. When -o is used the file is created with 0600 permissions.`,
	RunE: runExport,
}

func init() {
	exportCmd.Flags().StringVarP(&exportOutput, "output", "o", "",
		"write the archive to FILE instead of stdout (created with 0600)")
}

func runExport(cmd *cobra.Command, args []string) error {
	url := fmt.Sprintf("%s/api/v1/config/export", viper.GetString("server"))

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to export config: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read export response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to export config: %s", string(body))
	}

	if exportOutput == "" {
		_, err = cmd.OutOrStdout().Write(body)
		return err
	}

	if err := os.WriteFile(exportOutput, body, 0o600); err != nil {
		return fmt.Errorf("failed to write %s: %w", exportOutput, err)
	}

	fmt.Fprintf(cmd.ErrOrStderr(), "Wrote %s (%d bytes)\n", exportOutput, len(body))
	return nil
}
