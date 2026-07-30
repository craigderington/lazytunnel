package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/craigderington/lazytunnel/pkg/types"
)

var (
	tunnelName       string
	tunnelType       string
	localPort        int
	localBindAddress string
	remoteHost       string
	remotePort       int
	hops             []string
	sshUser          string
	sshKey           string
	autoReconnect    bool
	keepAlive        int
	maxRetries       int
)

// createTunnelRequest mirrors the wire format of api.CreateTunnelRequest.
//
// It exists because types.TunnelSpec disagrees with the API in two ways: its
// top-level JSON tags are snake_case where the API expects camelCase, and its
// KeepAlive is a time.Duration that marshals to nanoseconds where the API
// expects whole seconds (validated max=300). Marshalling the spec directly
// produced a body the server silently decoded into zero values and then
// rejected as invalid.
//
// This mirrors the existing pattern in this package — list.go declares
// tunnelListItem, import.go declares importReport — of restating server shapes
// rather than importing them. create_contract_test.go pins this struct against
// the real api.CreateTunnelRequest so the two cannot drift apart again.
//
// Hops are sent as []types.Hop deliberately: types.Hop and api.HopReq already
// agree on their snake_case tags, and the extra fields types.Hop carries are
// ignored by the server's decoder.
type createTunnelRequest struct {
	Name             string      `json:"name"`
	Type             string      `json:"type"`
	Hops             []types.Hop `json:"hops"`
	LocalPort        int         `json:"localPort"`
	LocalBindAddress string      `json:"localBindAddress,omitempty"`
	RemoteHost       string      `json:"remoteHost,omitempty"`
	RemotePort       int         `json:"remotePort,omitempty"`
	AutoReconnect    bool        `json:"autoReconnect"`
	KeepAlive        int         `json:"keepAlive"`
	MaxRetries       int         `json:"maxRetries"`
}

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
	createCmd.Flags().StringVar(&localBindAddress, "local-bind-address", "127.0.0.1",
		"local address to bind (127.0.0.1 for loopback only, 0.0.0.0 for all interfaces)")
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

// buildCreateRequest turns the parsed flags into the request body the API
// expects. It performs no I/O so it can be tested directly.
func buildCreateRequest() (createTunnelRequest, error) {
	// Parse tunnel type
	var ttype types.TunnelType
	switch strings.ToLower(tunnelType) {
	case "local":
		ttype = types.TunnelTypeLocal
		if remoteHost == "" {
			return createTunnelRequest{}, fmt.Errorf("--remote-host is required for local tunnels")
		}
	case "remote":
		ttype = types.TunnelTypeRemote
		if remotePort == 0 {
			return createTunnelRequest{}, fmt.Errorf("--remote-port is required for remote tunnels")
		}
		if localPort == 0 {
			return createTunnelRequest{}, fmt.Errorf("--local-port is required for remote tunnels")
		}
	case "dynamic":
		ttype = types.TunnelTypeDynamic
	default:
		return createTunnelRequest{}, fmt.Errorf("invalid tunnel type: %s (must be local, remote, or dynamic)", tunnelType)
	}

	// Parse hops
	hopList := make([]types.Hop, 0, len(hops))
	for _, h := range hops {
		parts := strings.Split(h, ":")
		if len(parts) != 2 {
			return createTunnelRequest{}, fmt.Errorf("invalid hop format: %s (expected host:port)", h)
		}
		var port int
		if _, err := fmt.Sscanf(parts[1], "%d", &port); err != nil {
			return createTunnelRequest{}, fmt.Errorf("invalid port in hop: %s", h)
		}

		authMethod := types.AuthMethodKey
		keyID := sshKey
		if keyID == "" {
			keyID = os.ExpandEnv("$HOME/.ssh/id_rsa")
		}

		hopList = append(hopList, types.Hop{
			Host:       parts[0],
			Port:       port,
			User:       sshUser,
			AuthMethod: authMethod,
			KeyID:      keyID,
		})
	}

	// Destination, which differs per tunnel type:
	//   local   — --remote-host carries a combined host:port
	//   remote  — --remote-host is a bare host, --remote-port the port
	//   dynamic — a SOCKS5 proxy has no fixed destination, so both stay empty
	var remHost string
	var remPort int
	switch ttype {
	case types.TunnelTypeLocal:
		parts := strings.Split(remoteHost, ":")
		if len(parts) != 2 {
			return createTunnelRequest{}, fmt.Errorf("invalid remote host format: %s (expected host:port)", remoteHost)
		}
		remHost = parts[0]
		if _, err := fmt.Sscanf(parts[1], "%d", &remPort); err != nil {
			return createTunnelRequest{}, fmt.Errorf("invalid port in remote host: %s", remoteHost)
		}
	case types.TunnelTypeRemote:
		remHost = remoteHost
		remPort = remotePort
	}

	return createTunnelRequest{
		Name:             tunnelName,
		Type:             string(ttype),
		Hops:             hopList,
		LocalPort:        localPort,
		LocalBindAddress: localBindAddress,
		RemoteHost:       remHost,
		RemotePort:       remPort,
		AutoReconnect:    autoReconnect,
		KeepAlive:        keepAlive,
		MaxRetries:       maxRetries,
	}, nil
}

func runCreate(cmd *cobra.Command, args []string) error {
	req, err := buildCreateRequest()
	if err != nil {
		return err
	}

	serverURL := viper.GetString("server")
	url := fmt.Sprintf("%s/api/v1/tunnels", serverURL)

	jsonData, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal tunnel request: %w", err)
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
	fmt.Printf("  Type: %s\n", req.Type)

	switch types.TunnelType(req.Type) {
	case types.TunnelTypeLocal:
		fmt.Printf("  Listening: localhost:%d → %s\n", localPort, remoteHost)
	case types.TunnelTypeRemote:
		fmt.Printf("  Listening: remote:%d → localhost:%d\n", remotePort, localPort)
	case types.TunnelTypeDynamic:
		fmt.Printf("  SOCKS5 Proxy: localhost:%d\n", localPort)
	}

	return nil
}
