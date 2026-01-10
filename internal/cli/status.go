package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var statusCmd = &cobra.Command{
	Use:   "status [tunnel-id-or-name]",
	Short: "Get tunnel status",
	Long:  `Get detailed status information for a specific tunnel.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runStatus,
}

func runStatus(cmd *cobra.Command, args []string) error {
	tunnelID := args[0]

	serverURL := viper.GetString("server")
	url := fmt.Sprintf("%s/api/v1/tunnels/%s/status", serverURL, tunnelID)

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to get tunnel status: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("tunnel not found: %s", tunnelID)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to get tunnel status: %s", string(body))
	}

	var status map[string]interface{}
	if err := json.Unmarshal(body, &status); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	fmt.Printf("Tunnel Status: %s\n", tunnelID)
	fmt.Println("─────────────────────────────")
	fmt.Printf("  State: %v\n", status["state"])

	if connectedAt, ok := status["connected_at"]; ok && connectedAt != nil {
		fmt.Printf("  Connected: %v\n", connectedAt)
	}

	if lastError, ok := status["last_error"]; ok && lastError != nil && lastError != "" {
		fmt.Printf("  Last Error: %v\n", lastError)
	}

	fmt.Printf("  Bytes Sent: %v\n", formatBytes(status["bytes_sent"]))
	fmt.Printf("  Bytes Received: %v\n", formatBytes(status["bytes_received"]))

	if retryCount, ok := status["retry_count"]; ok {
		fmt.Printf("  Retry Count: %v\n", retryCount)
	}

	return nil
}

func formatBytes(val interface{}) string {
	bytes, ok := val.(float64)
	if !ok {
		return "0 B"
	}

	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%.0f B", bytes)
	}

	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}

	units := []string{"B", "KB", "MB", "GB", "TB"}
	return fmt.Sprintf("%.1f %s", bytes/float64(div), units[exp+1])
}
