package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/craigderington/lazytunnel/internal/tunnel"
	"github.com/craigderington/lazytunnel/pkg/types"
)

// Example demonstrating remote SSH port forwarding
// Remote forwarding: bind remote port -> forward to local
func main() {
	ctx := context.Background()

	// Define the SSH hop (bastion host)
	hop := &types.Hop{
		Host:       "bastion.example.com",
		Port:       22,
		User:       "deploy",
		AuthMethod: types.AuthMethodKey,
		KeyID:      "/home/user/.ssh/id_rsa",
	}

	// Configure the tunnel spec for remote forwarding
	// This will:
	// 1. Bind port 9090 on the remote SSH server
	// 2. Forward connections to port 8080 on this local machine
	spec := &types.TunnelSpec{
		ID:            "remote-tunnel-example",
		Name:          "Remote Tunnel Example",
		Type:          types.TunnelTypeRemote,
		LocalPort:     8080, // Local service port
		RemotePort:    9090, // Remote listening port
		AutoReconnect: true,
		KeepAlive:     30 * time.Second,
		MaxRetries:    3,
		Hops:          []types.Hop{*hop},
	}

	// Create tunnel manager
	manager := tunnel.NewManager(ctx)
	defer manager.Shutdown()

	fmt.Println("Creating remote tunnel...")
	fmt.Printf("Remote port %d will forward to local port %d\n",
		spec.RemotePort, spec.LocalPort)

	// Create the tunnel
	if err := manager.Create(ctx, spec); err != nil {
		log.Fatalf("Failed to create tunnel: %v", err)
	}

	fmt.Println("Remote tunnel created successfully!")
	fmt.Println("Remote server can now connect to port 9090")
	fmt.Println("Traffic will be forwarded to localhost:8080")

	// Get tunnel status
	tunnel, err := manager.Get(spec.ID)
	if err != nil {
		log.Fatalf("Failed to get tunnel: %v", err)
	}

	status := tunnel.GetStatus()
	fmt.Printf("\nTunnel Status:\n")
	fmt.Printf("  State: %s\n", status.State)
	fmt.Printf("  Connected At: %v\n", status.ConnectedAt)
	fmt.Printf("  Bytes Sent: %d\n", status.BytesSent)
	fmt.Printf("  Bytes Received: %d\n", status.BytesReceived)

	// Keep the tunnel open
	fmt.Println("\nTunnel is active. Press Ctrl+C to stop.")
	select {}
}
