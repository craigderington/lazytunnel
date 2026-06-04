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
	"github.com/craigderington/lazytunnel/internal/config"
	"github.com/craigderington/lazytunnel/internal/storage"
)

var version = "dev"

func main() {
	configPath := flag.String("config", "", "Path to config.yaml")
	addr := flag.String("addr", "", "HTTP listen address (overrides config)")
	debug := flag.Bool("debug", false, "Enable debug logging (overrides config)")
	dbPath := flag.String("db", "", "SQLite database path (overrides config)")
	jwtSecret := flag.String("jwt-secret", "", "JWT secret (overrides config)")
	tlsCert := flag.String("tls-cert", "", "TLS certificate file")
	tlsKey := flag.String("tls-key", "", "TLS key file")
	flag.Parse()

	overrides := map[string]interface{}{
		"server.addr":      *addr,
		"database.path":    *dbPath,
		"auth.jwt_secret":  *jwtSecret,
		"server.tls_cert":  *tlsCert,
		"server.tls_key":   *tlsKey,
	}
	if *debug {
		overrides["logging.level"] = "debug"
	}

	cfg, err := config.Load(*configPath, overrides)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load configuration")
	}

	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	if cfg.DebugEnabled() {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339})
	} else {
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}

	log.Info().
		Str("version", version).
		Str("addr", cfg.Server.Addr).
		Msg("Starting lazytunnel server")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store, err := storage.NewSQLiteStore(cfg.Database.Path)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize storage")
	}
	defer store.Close()

	log.Info().Str("db_path", cfg.Database.Path).Msg("Initialized SQLite storage")

	var auth *api.AuthMiddleware
	if cfg.Auth.JWTSecret != "" {
		auth = api.NewAuthMiddleware(cfg.Auth.JWTSecret, cfg.Auth.TokenExpiration)
		log.Info().Msg("Authentication enabled with JWT")
	} else {
		log.Warn().Msg("No JWT secret configured - API will run without authentication")
	}

	var tlsConfig *api.TLSConfig
	if cfg.Server.TLSCert != "" && cfg.Server.TLSKey != "" {
		tlsConfig = &api.TLSConfig{
			CertFile: cfg.Server.TLSCert,
			KeyFile:  cfg.Server.TLSKey,
		}
		log.Info().Str("cert", cfg.Server.TLSCert).Msg("TLS enabled")
	}

	server := api.NewServer(ctx, api.Config{
		Addr:    cfg.Server.Addr,
		Logger:  log.Logger,
		Storage: store,
		Auth:    auth,
		TLS:     tlsConfig,
	})

	go func() {
		var err error
		if tlsConfig != nil {
			err = server.StartTLS(tlsConfig.CertFile, tlsConfig.KeyFile)
		} else {
			err = server.Start()
		}
		if err != nil {
			log.Fatal().Err(err).Msg("Server failed")
		}
	}()

	log.Info().Msg("Server started successfully")
	log.Info().Str("openapi", "http://localhost"+cfg.Server.Addr+"/api/v1/openapi.yaml").Msg("API documentation")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Info().Msg("Received shutdown signal")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("Server shutdown failed")
		os.Exit(1)
	}

	log.Info().Msg("Server stopped gracefully")
}