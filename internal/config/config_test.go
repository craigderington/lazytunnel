package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load("", nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Addr != ":8080" {
		t.Errorf("addr = %q", cfg.Server.Addr)
	}
	if cfg.Database.Path != "tunnels.db" {
		t.Errorf("db = %q", cfg.Database.Path)
	}
}

func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
server:
  addr: ":9090"
database:
  path: "test.db"
auth:
  jwt_secret: "test-secret"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Addr != ":9090" {
		t.Errorf("addr = %q", cfg.Server.Addr)
	}
	if cfg.Auth.JWTSecret != "test-secret" {
		t.Errorf("jwt secret not loaded")
	}
}

func TestExampleConfigMatchesRealSchema(t *testing.T) {
	// The example file is what operators copy. If it documents keys Load
	// does not read, they get a server that silently ignores their config.
	cfg, err := Load("../../config.example.yaml", nil)
	if err != nil {
		t.Fatalf("config.example.yaml does not load: %v", err)
	}

	if cfg.Server.Addr != ":8080" {
		t.Errorf("Server.Addr = %q, want :8080", cfg.Server.Addr)
	}
	if cfg.Database.Path != "tunnels.db" {
		t.Errorf("Database.Path = %q, want tunnels.db", cfg.Database.Path)
	}
	if cfg.Auth.JWTSecretEnv != "LAZYTUNNEL_JWT_SECRET" {
		t.Errorf("Auth.JWTSecretEnv = %q, want LAZYTUNNEL_JWT_SECRET", cfg.Auth.JWTSecretEnv)
	}
	if cfg.Auth.TokenExpiration != 24*time.Hour {
		t.Errorf("Auth.TokenExpiration = %v, want 24h", cfg.Auth.TokenExpiration)
	}
	if cfg.Logging.Level != "info" {
		t.Errorf("Logging.Level = %q, want info", cfg.Logging.Level)
	}
	if cfg.Logging.Format != "console" {
		t.Errorf("Logging.Format = %q, want console", cfg.Logging.Format)
	}

	// The example must ship the safe CORS default, not a wildcard.
	if len(cfg.Server.CORS.AllowedOrigins) != 0 {
		t.Errorf("Server.CORS.AllowedOrigins = %v, want empty — the example must not hand operators a wide-open default",
			cfg.Server.CORS.AllowedOrigins)
	}
}
