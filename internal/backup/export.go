package backup

import (
	"context"
	"fmt"
	"time"

	"github.com/craigderington/lazytunnel/pkg/types"
)

// Store is the narrow slice of persistence this package needs.
// *storage.SQLiteStore and tunnel.Storage both satisfy it.
type Store interface {
	List(ctx context.Context) ([]*types.TunnelSpec, error)
	Save(ctx context.Context, spec *types.TunnelSpec) error
	Delete(ctx context.Context, tunnelID string) error
}

// Clock supplies the current time. Injected so tests are deterministic.
type Clock func() time.Time

// Export reads every stored tunnel and returns a complete archive.
func Export(ctx context.Context, store Store, source string, now Clock) (*Archive, error) {
	if now == nil {
		now = time.Now
	}

	specs, err := store.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list tunnels for export: %w", err)
	}

	entries := make([]TunnelEntry, 0, len(specs))
	for _, spec := range specs {
		entries = append(entries, EntryFromSpec(spec))
	}

	return &Archive{
		Version:    SchemaVersion,
		ExportedAt: now().UTC(),
		Source:     source,
		Tunnels:    entries,
	}, nil
}
