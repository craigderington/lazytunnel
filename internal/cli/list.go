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

// tunnelListItem mirrors the fields handleListTunnels emits for each tunnel.
// The server responds with a bare array, a flat status string, and camelCase
// timestamp keys.
type tunnelListItem struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	Status    string `json:"status"`
	CreatedAt string `json:"createdAt"`
}

func parseTunnelList(body []byte) ([]tunnelListItem, error) {
	var items []tunnelListItem
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	return items, nil
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

	tunnels, err := parseTunnelList(body)
	if err != nil {
		return err
	}

	if len(tunnels) == 0 {
		fmt.Println("No active tunnels")
		return nil
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tTYPE\tSTATE\tCREATED")
	fmt.Fprintln(w, "──\t────\t────\t─────\t───────")

	for _, tunnel := range tunnels {
		created := tunnel.CreatedAt
		if parsed, err := time.Parse(time.RFC3339, tunnel.CreatedAt); err == nil {
			created = parsed.Format("2006-01-02 15:04")
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			truncate(tunnel.ID, 8),
			tunnel.Name,
			tunnel.Type,
			tunnel.Status,
			created,
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
