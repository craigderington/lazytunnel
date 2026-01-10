package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/rs/zerolog"

	"github.com/craigderington/lazytunnel/internal/tunnel"
)

// Server represents the API server
type Server struct {
	addr        string
	manager     *tunnel.Manager
	router      *mux.Router
	server      *http.Server
	logger      zerolog.Logger
	ctx         context.Context
}

// Config holds server configuration
type Config struct {
	Addr    string
	Logger  zerolog.Logger
	Storage tunnel.Storage // Optional persistent storage
}

// NewServer creates a new API server
func NewServer(ctx context.Context, config Config) *Server {
	manager := tunnel.NewManager(ctx)

	// Configure storage if provided
	if config.Storage != nil {
		manager.SetStorage(config.Storage)

		// Load existing tunnels from storage
		if err := manager.LoadFromStorage(ctx); err != nil {
			config.Logger.Error().Err(err).Msg("Failed to load tunnels from storage")
		} else {
			config.Logger.Info().Msg("Loaded tunnels from persistent storage")
		}
	}

	s := &Server{
		addr:    config.Addr,
		manager: manager,
		router:  mux.NewRouter(),
		logger:  config.Logger,
		ctx:     ctx,
	}

	s.setupRoutes()

	s.server = &http.Server{
		Addr:         s.addr,
		Handler:      s.router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	return s
}

// setupRoutes configures the API routes
func (s *Server) setupRoutes() {
	// Apply CORS to main router first
	s.router.Use(s.corsMiddleware)

	// API v1 routes
	api := s.router.PathPrefix("/api/v1").Subrouter()

	// Middleware
	api.Use(s.loggingMiddleware)

	// Health check
	api.HandleFunc("/health", s.handleHealth).Methods("GET", "OPTIONS")

	// Tunnel operations
	api.HandleFunc("/tunnels", s.handleListTunnels).Methods("GET", "OPTIONS")
	api.HandleFunc("/tunnels", s.handleCreateTunnel).Methods("POST", "OPTIONS")
	api.HandleFunc("/tunnels/{id}", s.handleGetTunnel).Methods("GET", "OPTIONS")
	api.HandleFunc("/tunnels/{id}", s.handleDeleteTunnel).Methods("DELETE", "OPTIONS")
	api.HandleFunc("/tunnels/{id}/start", s.handleStartTunnel).Methods("POST", "OPTIONS")
	api.HandleFunc("/tunnels/{id}/stop", s.handleStopTunnel).Methods("POST", "OPTIONS")
	api.HandleFunc("/tunnels/{id}/status", s.handleGetTunnelStatus).Methods("GET", "OPTIONS")
	api.HandleFunc("/tunnels/{id}/metrics", s.handleGetTunnelMetrics).Methods("GET", "OPTIONS")
}

// Start starts the HTTP server
func (s *Server) Start() error {
	s.logger.Info().Str("addr", s.addr).Msg("Starting API server")
	return s.server.ListenAndServe()
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info().Msg("Shutting down API server")

	// Shutdown HTTP server
	if err := s.server.Shutdown(ctx); err != nil {
		return fmt.Errorf("failed to shutdown HTTP server: %w", err)
	}

	// Shutdown tunnel manager
	if err := s.manager.Shutdown(); err != nil {
		return fmt.Errorf("failed to shutdown tunnel manager: %w", err)
	}

	return nil
}

// loggingMiddleware logs HTTP requests
func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Create response writer to capture status code
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(rw, r)

		s.logger.Info().
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Int("status", rw.statusCode).
			Dur("duration", time.Since(start)).
			Msg("HTTP request")
	})
}

// corsMiddleware adds CORS headers
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// Helper functions for JSON responses
func (s *Server) respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		s.logger.Error().Err(err).Msg("Failed to encode JSON response")
	}
}

func (s *Server) respondError(w http.ResponseWriter, status int, message string) {
	s.respondJSON(w, status, map[string]string{
		"error": message,
	})
}
