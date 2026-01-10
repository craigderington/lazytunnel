package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/craigderington/lazytunnel/internal/tunnel"
	"github.com/craigderington/lazytunnel/pkg/types"
)

// Example demonstrating basic SSH tunnel usage
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

	// Configure the session
	config := tunnel.SessionConfig{
		Hop:           hop,
		KeepAlive:     30 * time.Second,
		AutoReconnect: true,
		MaxRetries:    3,
		Timeout:       10 * time.Second,
		BackoffConfig: tunnel.DefaultBackoffConfig(),
	}

	// Create a new SSH session
	session, err := tunnel.NewSession(ctx, config)
	if err != nil {
		log.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	fmt.Println("Session created successfully")
	fmt.Printf("Status: %+v\n", session.Status())

	// Connect to the SSH server
	fmt.Println("Connecting to bastion host...")
	if err := session.ConnectWithRetry(); err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}

	fmt.Println("Connected successfully!")
	fmt.Printf("Status: %+v\n", session.Status())

	// Example: Dial through the SSH session to an internal service
	conn, err := session.Dial("tcp", "internal-db.example.com:5432")
	if err != nil {
		log.Fatalf("Failed to dial through tunnel: %v", err)
	}
	defer conn.Close()

	fmt.Println("Tunnel established to internal-db.example.com:5432")

	// Use the connection for your application
	// ...

	// Keep the tunnel open for a while
	time.Sleep(30 * time.Second)

	fmt.Println("Closing tunnel...")
}
