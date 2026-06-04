package config

import (
	"os"
	"path/filepath"
	"testing"
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