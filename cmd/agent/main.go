package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/craigderington/lazytunnel/internal/agent"
	"github.com/craigderington/lazytunnel/internal/tunnel"
	"github.com/craigderington/lazytunnel/pkg/agentclient"
)

func main() {
	serverURL := flag.String("server", "http://localhost:8080/api/v1", "Control plane API URL")
	agentID := flag.String("id", "", "Agent ID (defaults to hostname)")
	username := flag.String("user", "admin", "API username")
	password := flag.String("password", "lazytunnel", "API password")
	interval := flag.Duration("interval", 5*time.Second, "Reconciliation interval")
	debug := flag.Bool("debug", false, "Debug logging")
	flag.Parse()

	if *debug {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339})
	}

	id := *agentID
	if id == "" {
		hostname, _ := os.Hostname()
		id = hostname
	}

	client := agentclient.New(*serverURL, "")
	if _, err := client.Login(*username, *password); err != nil {
		log.Fatal().Err(err).Msg("Failed to authenticate with control plane")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	manager := tunnel.NewManager(ctx)
	manager.SetNodeAgentID(id)
	worker := &agent.Worker{
		ID:       id,
		Client:   client,
		Manager:  manager,
		Logger:   log.Logger,
		Interval: *interval,
	}

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		cancel()
	}()

	log.Info().Str("id", id).Str("server", *serverURL).Msg("Starting lazytunnel agent")
	if err := worker.Run(ctx); err != nil {
		log.Fatal().Err(err).Msg("Agent stopped with error")
	}
	log.Info().Msg("Agent stopped")
}