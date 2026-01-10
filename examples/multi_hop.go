package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/craigderington/lazytunnel/internal/tunnel"
	"github.com/craigderington/lazytunnel/pkg/types"
)

// Example demonstrating multi-hop SSH tunneling
func main() {
	ctx := context.Background()

	// Define multiple hops
	hops := []types.Hop{
		{
			Host:       "bastion1.example.com",
			Port:       22,
			User:       "deploy",
			AuthMethod: types.AuthMethodAgent, // Use SSH agent
		},
		{
			Host:       "bastion2.internal.example.com",
			Port:       22,
			User:       "admin",
			AuthMethod: types.AuthMethodKey,
			KeyID:      "/home/user/.ssh/internal_key",
		},
		{
			Host:       "private-server.internal.example.com",
			Port:       22,
			User:       "app",
			AuthMethod: types.AuthMethodKey,
			KeyID:      "/home/user/.ssh/app_key",
		},
	}

	// Configure the multi-hop session
	config := tunnel.SessionConfig{
		KeepAlive:     30 * time.Second,
		AutoReconnect: true,
		MaxRetries:    3,
		Timeout:       10 * time.Second,
		BackoffConfig: tunnel.DefaultBackoffConfig(),
	}

	// Create multi-hop session
	session, err := tunnel.NewMultiHopSession(ctx, hops, config)
	if err != nil {
		log.Fatalf("Failed to create multi-hop session: %v", err)
	}
	defer session.Close()

	fmt.Println("Multi-hop session created successfully")

	// Connect through all hops
	fmt.Println("Connecting through hop chain...")
	if err := session.Connect(); err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}

	fmt.Println("Connected successfully through all hops!")

	// Print status of each hop
	statuses := session.Status()
	for i, status := range statuses {
		fmt.Printf("Hop %d: %s@%s:%d - Connected: %v\n",
			i+1, status.User, status.Host, status.Port, status.Connected)
	}

	// Dial to final destination through the chain
	conn, err := session.Dial("tcp", "database.private.example.com:5432")
	if err != nil {
		log.Fatalf("Failed to dial through multi-hop tunnel: %v", err)
	}
	defer conn.Close()

	fmt.Println("Multi-hop tunnel established successfully!")

	// Keep the tunnel open
	time.Sleep(60 * time.Second)

	fmt.Println("Closing multi-hop tunnel...")
}
