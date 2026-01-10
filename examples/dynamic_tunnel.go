package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/craigderington/lazytunnel/internal/tunnel"
	"github.com/craigderington/lazytunnel/pkg/types"
)

// Example demonstrating dynamic SOCKS5 port forwarding
// Dynamic forwarding: local SOCKS5 proxy → forward to any destination
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

	// Configure the tunnel spec for dynamic (SOCKS5) forwarding
	// This will:
	// 1. Bind port 1080 on local machine as a SOCKS5 proxy
	// 2. Forward connections dynamically through SSH to any destination
	spec := &types.TunnelSpec{
		ID:            "dynamic-tunnel-example",
		Name:          "SOCKS5 Proxy",
		Type:          types.TunnelTypeDynamic,
		LocalPort:     1080, // SOCKS5 proxy port
		AutoReconnect: true,
		KeepAlive:     30 * time.Second,
		MaxRetries:    3,
		Hops:          []types.Hop{*hop},
	}

	// Create tunnel manager
	manager := tunnel.NewManager(ctx)
	defer manager.Shutdown()

	fmt.Println("Creating SOCKS5 dynamic tunnel...")
	fmt.Printf("SOCKS5 proxy will listen on port %d\n", spec.LocalPort)

	// Create the tunnel
	if err := manager.Create(ctx, spec); err != nil {
		log.Fatalf("Failed to create tunnel: %v", err)
	}

	fmt.Println("SOCKS5 tunnel created successfully!")
	fmt.Println("\nUsage examples:")
	fmt.Println("  curl --socks5 localhost:1080 https://example.com")
	fmt.Println("  export ALL_PROXY=socks5://localhost:1080")
	fmt.Println("  ssh -o ProxyCommand='nc -X 5 -x localhost:1080 %h %p' user@internal-host")

	// Get tunnel status
	t, err := manager.Get(spec.ID)
	if err != nil {
		log.Fatalf("Failed to get tunnel: %v", err)
	}

	status := t.GetStatus()
	fmt.Printf("\nTunnel Status:\n")
	fmt.Printf("  State: %s\n", status.State)
	fmt.Printf("  Connected At: %v\n", status.ConnectedAt)
	fmt.Printf("  Bytes Sent: %d\n", status.BytesSent)
	fmt.Printf("  Bytes Received: %d\n", status.BytesReceived)

	fmt.Println("\nSOCKS5 proxy is active. Press Ctrl+C to stop.")
	fmt.Println("All traffic will be routed through bastion.example.com")

	// Keep the tunnel open
	select {}
}
