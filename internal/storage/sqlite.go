package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	"github.com/craigderington/lazytunnel/pkg/types"
)

// SQLiteStore provides persistent storage for tunnel specifications
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore creates a new SQLite storage backend
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Enable WAL mode for better concurrency
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return nil, fmt.Errorf("failed to enable WAL mode: %w", err)
	}

	store := &SQLiteStore{db: db}

	// Initialize schema
	if err := store.initSchema(); err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return store, nil
}

// initSchema creates the database schema
func (s *SQLiteStore) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS tunnels (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		owner TEXT NOT NULL,
		type TEXT NOT NULL,
		hops TEXT NOT NULL, -- JSON array
		local_port INTEGER NOT NULL,
		local_bind_address TEXT DEFAULT '127.0.0.1',
		remote_host TEXT NOT NULL,
		remote_port INTEGER NOT NULL,
		auto_reconnect BOOLEAN NOT NULL,
		keep_alive INTEGER NOT NULL, -- seconds
		max_retries INTEGER NOT NULL,
		status TEXT NOT NULL, -- active, stopped, failed, etc.
		created_at TIMESTAMP NOT NULL,
		updated_at TIMESTAMP NOT NULL,
		UNIQUE(name)
	);

	CREATE INDEX IF NOT EXISTS idx_tunnels_status ON tunnels(status);
	CREATE INDEX IF NOT EXISTS idx_tunnels_owner ON tunnels(owner);
	CREATE INDEX IF NOT EXISTS idx_tunnels_created_at ON tunnels(created_at DESC);
	`

	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	// Add local_bind_address column if it doesn't exist (for backward compatibility)
	if _, err := s.db.Exec(`
		ALTER TABLE tunnels ADD COLUMN local_bind_address TEXT DEFAULT '127.0.0.1'
	`); err != nil {
		// Ignore "duplicate column name" error
		if !isDuplicateColumnError(err) {
			return fmt.Errorf("failed to add local_bind_address column: %w", err)
		}
	}

	return nil
}

// isDuplicateColumnError checks if error is about duplicate column
func isDuplicateColumnError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return contains(errStr, "duplicate column") || contains(errStr, "already exists")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) &&
		(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
			findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Save saves a tunnel spec to the database
func (s *SQLiteStore) Save(ctx context.Context, spec *types.TunnelSpec) error {
	hopsJSON, err := json.Marshal(spec.Hops)
	if err != nil {
		return fmt.Errorf("failed to marshal hops: %w", err)
	}

	query := `
		INSERT OR REPLACE INTO tunnels (
			id, name, owner, type, hops, local_port, local_bind_address, remote_host, remote_port,
			auto_reconnect, keep_alive, max_retries, status, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err = s.db.ExecContext(ctx, query,
		spec.ID,
		spec.Name,
		spec.Owner,
		spec.Type,
		string(hopsJSON),
		spec.LocalPort,
		spec.LocalBindAddress,
		spec.RemoteHost,
		spec.RemotePort,
		spec.AutoReconnect,
		int(spec.KeepAlive.Seconds()),
		spec.MaxRetries,
		"stopped", // Initially saved as stopped
		spec.CreatedAt,
		spec.UpdatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to save tunnel: %w", err)
	}

	return nil
}

// UpdateStatus updates the status of a tunnel
func (s *SQLiteStore) UpdateStatus(ctx context.Context, tunnelID, status string) error {
	query := `UPDATE tunnels SET status = ?, updated_at = ? WHERE id = ?`

	result, err := s.db.ExecContext(ctx, query, status, time.Now(), tunnelID)
	if err != nil {
		return fmt.Errorf("failed to update status: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("tunnel not found: %s", tunnelID)
	}

	return nil
}

// Delete removes a tunnel from the database
func (s *SQLiteStore) Delete(ctx context.Context, tunnelID string) error {
	query := `DELETE FROM tunnels WHERE id = ?`

	result, err := s.db.ExecContext(ctx, query, tunnelID)
	if err != nil {
		return fmt.Errorf("failed to delete tunnel: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("tunnel not found: %s", tunnelID)
	}

	return nil
}

// Get retrieves a tunnel spec by ID
func (s *SQLiteStore) Get(ctx context.Context, tunnelID string) (*types.TunnelSpec, error) {
	query := `
		SELECT id, name, owner, type, hops, local_port, local_bind_address, remote_host, remote_port,
		       auto_reconnect, keep_alive, max_retries, status, created_at, updated_at
		FROM tunnels
		WHERE id = ?
	`

	var spec types.TunnelSpec
	var hopsJSON string
	var keepAliveSeconds int
	var status string

	err := s.db.QueryRowContext(ctx, query, tunnelID).Scan(
		&spec.ID,
		&spec.Name,
		&spec.Owner,
		&spec.Type,
		&hopsJSON,
		&spec.LocalPort,
		&spec.LocalBindAddress,
		&spec.RemoteHost,
		&spec.RemotePort,
		&spec.AutoReconnect,
		&keepAliveSeconds,
		&spec.MaxRetries,
		&status,
		&spec.CreatedAt,
		&spec.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("tunnel not found: %s", tunnelID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get tunnel: %w", err)
	}

	// Unmarshal hops
	if err := json.Unmarshal([]byte(hopsJSON), &spec.Hops); err != nil {
		return nil, fmt.Errorf("failed to unmarshal hops: %w", err)
	}

	spec.KeepAlive = time.Duration(keepAliveSeconds) * time.Second

	return &spec, nil
}

// List retrieves all tunnel specs
func (s *SQLiteStore) List(ctx context.Context) ([]*types.TunnelSpec, error) {
	query := `
		SELECT id, name, owner, type, hops, local_port, local_bind_address, remote_host, remote_port,
		       auto_reconnect, keep_alive, max_retries, status, created_at, updated_at
		FROM tunnels
		ORDER BY created_at DESC
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list tunnels: %w", err)
	}
	defer rows.Close()

	var specs []*types.TunnelSpec

	for rows.Next() {
		var spec types.TunnelSpec
		var hopsJSON string
		var keepAliveSeconds int
		var status string

		err := rows.Scan(
			&spec.ID,
			&spec.Name,
			&spec.Owner,
			&spec.Type,
			&hopsJSON,
			&spec.LocalPort,
			&spec.LocalBindAddress,
			&spec.RemoteHost,
			&spec.RemotePort,
			&spec.AutoReconnect,
			&keepAliveSeconds,
			&spec.MaxRetries,
			&status,
			&spec.CreatedAt,
			&spec.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan tunnel: %w", err)
		}

		// Unmarshal hops
		if err := json.Unmarshal([]byte(hopsJSON), &spec.Hops); err != nil {
			return nil, fmt.Errorf("failed to unmarshal hops: %w", err)
		}

		spec.KeepAlive = time.Duration(keepAliveSeconds) * time.Second

		specs = append(specs, &spec)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return specs, nil
}

// Close closes the database connection
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}
