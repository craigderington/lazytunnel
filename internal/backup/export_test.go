package backup

import (
	"context"
	"testing"
	"time"
)

func fixedClock(t time.Time) Clock { return func() time.Time { return t } }

func TestExportProducesVersionedArchive(t *testing.T) {
	at := time.Date(2026, 7, 29, 8, 40, 12, 0, time.UTC)
	store := newFakeStore(sampleSpec())

	archive, err := Export(context.Background(), store, "lazytunnel/1.0.0", fixedClock(at))
	if err != nil {
		t.Fatalf("Export returned error: %v", err)
	}
	if archive.Version != SchemaVersion {
		t.Errorf("got version %d, want %d", archive.Version, SchemaVersion)
	}
	if !archive.ExportedAt.Equal(at) {
		t.Errorf("got ExportedAt %v, want %v", archive.ExportedAt, at)
	}
	if archive.Source != "lazytunnel/1.0.0" {
		t.Errorf("got Source %q, want lazytunnel/1.0.0", archive.Source)
	}
	if len(archive.Tunnels) != 1 {
		t.Fatalf("got %d tunnels, want 1", len(archive.Tunnels))
	}
	if archive.Tunnels[0].Name != "prod-db" {
		t.Errorf("got name %q, want prod-db", archive.Tunnels[0].Name)
	}
}

func TestExportOfEmptyStoreIsValid(t *testing.T) {
	archive, err := Export(context.Background(), newFakeStore(), "test", fixedClock(time.Now()))
	if err != nil {
		t.Fatalf("Export returned error: %v", err)
	}
	if len(archive.Tunnels) != 0 {
		t.Fatalf("got %d tunnels, want 0", len(archive.Tunnels))
	}
	if errs := ValidateArchive(archive); len(errs) != 0 {
		t.Fatalf("empty export failed validation: %v", errs)
	}
}

func TestExportOutputPassesValidation(t *testing.T) {
	archive, err := Export(context.Background(), newFakeStore(sampleSpec()), "test", fixedClock(time.Now()))
	if err != nil {
		t.Fatalf("Export returned error: %v", err)
	}
	if errs := ValidateArchive(archive); len(errs) != 0 {
		t.Fatalf("exported archive failed its own validation: %v", errs)
	}
}
