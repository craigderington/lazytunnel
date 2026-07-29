package backup

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/craigderington/lazytunnel/pkg/types"
)

func sampleSpec() *types.TunnelSpec {
	created := time.Date(2026, 7, 29, 8, 0, 0, 0, time.UTC)
	return &types.TunnelSpec{
		ID:            "9f1c2b4e-7a30-4d51-9c8f-2e6b1a0d3f55",
		Name:          "prod-db",
		Owner:         "admin",
		AgentID:       "",
		DesiredStatus: types.DesiredStatusActive,
		Type:          types.TunnelTypeLocal,
		Hops: []types.Hop{{
			Host:                "bastion.example.com",
			Port:                22,
			User:                "deploy",
			AuthMethod:          types.AuthMethodKey,
			KeyID:               "/home/deploy/.ssh/id_ed25519",
			HostKeyVerification: types.HostKeyVerifyStrict,
			KnownHostsPath:      "/home/deploy/.ssh/known_hosts",
		}},
		LocalPort:        5432,
		LocalBindAddress: "127.0.0.1",
		RemoteHost:       "db.internal.example.com",
		RemotePort:       5432,
		AutoReconnect:    true,
		KeepAlive:        30 * time.Second,
		MaxRetries:       5,
		CreatedAt:        created,
		UpdatedAt:        created,
	}
}

func TestEntryFromSpecRoundTrips(t *testing.T) {
	spec := sampleSpec()
	got := SpecFromEntry(EntryFromSpec(spec))

	// CreatedAt/UpdatedAt are deliberately not carried by the archive — Plan
	// resolves them — so compare everything else.
	got.CreatedAt = spec.CreatedAt
	got.UpdatedAt = spec.UpdatedAt

	if !reflect.DeepEqual(got, spec) {
		t.Fatalf("round trip changed the spec:\n got %+v\nwant %+v", got, spec)
	}
}

func TestEntryUsesWholeSecondsNotNanoseconds(t *testing.T) {
	entry := EntryFromSpec(sampleSpec())
	if entry.KeepAliveSeconds != 30 {
		t.Fatalf("got KeepAliveSeconds %d, want 30", entry.KeepAliveSeconds)
	}

	blob, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(blob, &raw); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if _, present := raw["keep_alive"]; present {
		t.Error("archive must not emit a nanosecond keep_alive field")
	}
	if raw["keep_alive_seconds"] != float64(30) {
		t.Errorf("got keep_alive_seconds %v, want 30", raw["keep_alive_seconds"])
	}
}

func TestEntryOmitsUnpersistedCredentialFields(t *testing.T) {
	// AuthConfig and PolicySpec are never written by internal/storage/sqlite.go.
	// Emitting them would imply the backup carries credentials it does not have.
	blob, err := json.Marshal(EntryFromSpec(sampleSpec()))
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(blob, &raw); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	for _, field := range []string{"auth", "policy"} {
		if _, present := raw[field]; present {
			t.Errorf("archive entry must not contain %q", field)
		}
	}
}

func TestSpecFromEntryDefaultsDesiredStatusToStopped(t *testing.T) {
	spec := SpecFromEntry(TunnelEntry{Name: "x", Type: "local"})
	if spec.DesiredStatus != types.DesiredStatusStopped {
		t.Fatalf("got DesiredStatus %q, want stopped", spec.DesiredStatus)
	}
}

// The forwarder (internal/tunnel/forward.go:106,619) already treats an empty
// LocalBindAddress as "0.0.0.0" at connect time. EntryFromSpec and
// SpecFromEntry must both normalize "" to the explicit "0.0.0.0" rather than
// leaving it blank or defaulting to 127.0.0.1: leaving it blank would hide a
// network-wide exposure from a hand-edited archive, and 127.0.0.1 would
// silently narrow every tunnel in the live database (all 8 currently store
// "") on first re-import, breaking off-box access that exists today.
func TestEntryFromSpecNormalizesEmptyLocalBindAddressToExplicitZero(t *testing.T) {
	spec := sampleSpec()
	spec.LocalBindAddress = ""

	entry := EntryFromSpec(spec)
	if entry.LocalBindAddress != "0.0.0.0" {
		t.Fatalf("got LocalBindAddress %q, want 0.0.0.0", entry.LocalBindAddress)
	}
}

func TestSpecFromEntryNormalizesEmptyLocalBindAddressToExplicitZero(t *testing.T) {
	spec := SpecFromEntry(TunnelEntry{Name: "x", Type: "local"}) // omits local_bind_address entirely
	if spec.LocalBindAddress != "0.0.0.0" {
		t.Fatalf("got LocalBindAddress %q, want 0.0.0.0", spec.LocalBindAddress)
	}
}
