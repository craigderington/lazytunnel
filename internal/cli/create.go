package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/craigderington/lazytunnel/pkg/types"
)

var (
	tunnelName    string
	tunnelType    string
	localPort     int
	remoteHost    string
	remotePort    int
	hops          []string
	sshUser       string
	sshKey        string
	autoReconnect bool
	keepAlive     int
	maxRetries    int
)

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new SSH tunnel",
	Long: `Create a new SSH tunnel with the specified configuration.

Tunnel types:
  - local:   Local port forwarding (bind local port → forward to remote)
  - remote:  Remote port forwarding (bind remote port → forward to local)
  - dynamic: SOCKS5 proxy (dynamic destinations)

Examples:
  # Create local tunnel through bastion
  tunnelctl create --name prod-db --type local \
    --local-port 5432 --remote-host db.internal:5432 \
    --hop bastion.example.com:22 --user deploy --key ~/.ssh/id_rsa

  # Create SOCKS5 proxy
  tunnelctl create --name socks --type dynamic \
    --local-port 1080 --hop jumphost:22 --user admin --key ~/.ssh/id_rsa

  # Create remote tunnel
  tunnelctl create --name expose-local --type remote \
    --local-port 8080 --remote-port 9090 \
    --hop server.example.com:22 --user deploy --key ~/.ssh/id_rsa`,
	RunE: runCreate,
}

func init() {
	createCmd.Flags().StringVar(&tunnelName, "name", "", "tunnel name (required)")
	createCmd.Flags().StringVar(&tunnelType, "type", "local", "tunnel type: local, remote, or dynamic")
	createCmd.Flags().IntVar(&localPort, "local-port", 0, "local port to bind")
	createCmd.Flags().StringVar(&remoteHost, "remote-host", "", "remote host:port (for local tunnels)")
	createCmd.Flags().IntVar(&remotePort, "remote-port", 0, "remote port (for remote tunnels)")
	createCmd.Flags().StringArrayVar(&hops, "hop", []string{}, "SSH hop in format host:port (can specify multiple for multi-hop)")
	createCmd.Flags().StringVar(&sshUser, "user", os.Getenv("USER"), "SSH username")
	createCmd.Flags().StringVar(&sshKey, "key", "", "path to SSH private key")
	createCmd.Flags().BoolVar(&autoReconnect, "auto-reconnect", true, "automatically reconnect on failure")
	createCmd.Flags().IntVar(&keepAlive, "keep-alive", 30, "SSH keep-alive interval in seconds")
	createCmd.Flags().IntVar(&maxRetries, "max-retries", 3, "maximum reconnection attempts")

	createCmd.MarkFlagRequired("name")
	createCmd.MarkFlagRequired("hop")
}

func runCreate(cmd *cobra.Command, args []string) error {
	// Parse tunnel type
	var ttype types.TunnelType
	switch strings.ToLower(tunnelType) {
	case "local":
		ttype = types.TunnelTypeLocal
		if remoteHost == "" {
			return fmt.Errorf("--remote-host is required for local tunnels")
		}
	case "remote":
		ttype = types.TunnelTypeRemote
		if remotePort == 0 {
			return fmt.Errorf("--remote-port is required for remote tunnels")
		}
		if localPort == 0 {
			return fmt.Errorf("--local-port is required for remote tunnels")
		}
	case "dynamic":
		ttype = types.TunnelTypeDynamic
	default:
		return fmt.Errorf("invalid tunnel type: %s (must be local, remote, or dynamic)", tunnelType)
	}

	// Parse hops
	hopList := make([]types.Hop, len(hops))
	for i, h := range hops {
		parts := strings.Split(h, ":")
		if len(parts) != 2 {
			return fmt.Errorf("invalid hop format: %s (expected host:port)", h)
		}

		var port int
		if _, err := fmt.Sscanf(parts[1], "%d", &port); err != nil {
			return fmt.Errorf("invalid port in hop: %s", h)
		}

		authMethod := types.AuthMethodKey
		keyID := sshKey
		if keyID == "" {
			keyID = os.ExpandEnv("$HOME/.ssh/id_rsa")
		}

		hopList[i] = types.Hop{
			Host:       parts[0],
			Port:       port,
			User:       sshUser,
			AuthMethod: authMethod,
			KeyID:      keyID,
		}
	}

	// Parse remote host/port for local tunnels
	var remHost string
	var remPort int
	if ttype == types.TunnelTypeLocal {
		parts := strings.Split(remoteHost, ":")
		if len(parts) == 2 {
			remHost = parts[0]
			if _, err := fmt.Sscanf(parts[1], "%d", &remPort); err != nil {
				return fmt.Errorf("invalid port in remote host: %s", remoteHost)
			}
		} else {
			return fmt.Errorf("invalid remote host format: %s (expected host:port)", remoteHost)
		}
	} else {
		remPort = remotePort
	}

	// Create tunnel spec
	spec := types.TunnelSpec{
		Name:          tunnelName,
		Type:          ttype,
		LocalPort:     localPort,
		RemoteHost:    remHost,
		RemotePort:    remPort,
		Hops:          hopList,
		AutoReconnect: autoReconnect,
		KeepAlive:     time.Duration(keepAlive) * time.Second,
		MaxRetries:    maxRetries,
	}

	// Make API request
	serverURL := viper.GetString("server")
	url := fmt.Sprintf("%s/api/v1/tunnels", serverURL)

	jsonData, err := json.Marshal(spec)
	if err != nil {
		return fmt.Errorf("failed to marshal tunnel spec: %w", err)
	}

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create tunnel: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("failed to create tunnel: %s", string(body))
	}

	// Parse response
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	fmt.Printf("✓ Tunnel created successfully\n")
	fmt.Printf("  ID: %s\n", result["id"])
	fmt.Printf("  Name: %s\n", tunnelName)
	fmt.Printf("  Type: %s\n", tunnelType)

	if ttype == types.TunnelTypeLocal {
		fmt.Printf("  Listening: localhost:%d → %s\n", localPort, remoteHost)
	} else if ttype == types.TunnelTypeRemote {
		fmt.Printf("  Listening: remote:%d → localhost:%d\n", remotePort, localPort)
	} else if ttype == types.TunnelTypeDynamic {
		fmt.Printf("  SOCKS5 Proxy: localhost:%d\n", localPort)
	}

	return nil
}
