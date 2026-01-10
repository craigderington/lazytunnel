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

	"github.com/craigderington/lazytunnel/internal/api"
	"github.com/craigderington/lazytunnel/internal/storage"
)

var (
	version = "dev"
	addr    = flag.String("addr", ":8080", "HTTP server address")
	debug   = flag.Bool("debug", false, "Enable debug logging")
	dbPath  = flag.String("db", "tunnels.db", "Path to SQLite database file")
)

func main() {
	flag.Parse()

	// Setup logging
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	if *debug {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339})
	} else {
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}

	log.Info().
		Str("version", version).
		Str("addr", *addr).
		Msg("Starting lazytunnel server")

	// Create context that cancels on interrupt
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize persistent storage
	store, err := storage.NewSQLiteStore(*dbPath)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize storage")
	}
	defer store.Close()

	log.Info().Str("db_path", *dbPath).Msg("Initialized SQLite storage")

	// Create API server
	server := api.NewServer(ctx, api.Config{
		Addr:    *addr,
		Logger:  log.Logger,
		Storage: store,
	})

	// Start server in goroutine
	go func() {
		if err := server.Start(); err != nil {
			log.Fatal().Err(err).Msg("Server failed")
		}
	}()

	log.Info().Msg("Server started successfully")
	log.Info().Msgf("API available at http://localhost%s/api/v1", *addr)
	log.Info().Msgf("Health check: http://localhost%s/api/v1/health", *addr)

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Info().Msg("Received shutdown signal")

	// Graceful shutdown with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("Server shutdown failed")
		os.Exit(1)
	}

	log.Info().Msg("Server stopped gracefully")
}
