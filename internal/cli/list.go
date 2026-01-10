package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all active tunnels",
	Long:  `List all currently active SSH tunnels on the server.`,
	RunE:  runList,
}

func runList(cmd *cobra.Command, args []string) error {
	serverURL := viper.GetString("server")
	url := fmt.Sprintf("%s/api/v1/tunnels", serverURL)

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to list tunnels: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to list tunnels: %s", string(body))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	tunnels, ok := result["tunnels"].([]interface{})
	if !ok {
		return fmt.Errorf("invalid response format")
	}

	if len(tunnels) == 0 {
		fmt.Println("No active tunnels")
		return nil
	}

	// Print table
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tTYPE\tSTATE\tCREATED")
	fmt.Fprintln(w, "──\t────\t────\t─────\t───────")

	for _, t := range tunnels {
		tunnel := t.(map[string]interface{})

		id := tunnel["id"].(string)
		name := tunnel["name"].(string)
		ttype := tunnel["type"].(string)

		status := tunnel["status"].(map[string]interface{})
		state := status["state"].(string)

		createdAt := tunnel["created_at"].(string)
		created, _ := time.Parse(time.RFC3339, createdAt)

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			truncate(id, 8),
			name,
			ttype,
			state,
			created.Format("2006-01-02 15:04"),
		)
	}

	w.Flush()

	fmt.Printf("\nTotal: %d tunnel(s)\n", len(tunnels))

	return nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
