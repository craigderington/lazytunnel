package cli

import (
	"fmt"
	"io"
	"net/http"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var stopCmd = &cobra.Command{
	Use:   "stop [tunnel-id-or-name]",
	Short: "Stop a tunnel",
	Long:  `Stop and remove an active SSH tunnel.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runStop,
}

func runStop(cmd *cobra.Command, args []string) error {
	tunnelID := args[0]

	serverURL := viper.GetString("server")
	url := fmt.Sprintf("%s/api/v1/tunnels/%s", serverURL, tunnelID)

	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to stop tunnel: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("tunnel not found: %s", tunnelID)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to stop tunnel: %s", string(body))
	}

	fmt.Printf("✓ Tunnel stopped: %s\n", tunnelID)

	return nil
}
