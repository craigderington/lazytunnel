package backup

import (
	"strings"
	"testing"
)

func validEntry(name string) TunnelEntry {
	return TunnelEntry{
		Name:       name,
		Type:       "local",
		LocalPort:  5432,
		RemoteHost: "db.internal",
		RemotePort: 5432,
		Hops:       []HopEntry{{Host: "bastion", Port: 22, User: "deploy", AuthMethod: "key"}},
	}
}

func fieldsWithErrors(errs []EntryError) []string {
	out := make([]string, 0, len(errs))
	for _, e := range errs {
		out = append(out, e.Field)
	}
	return out
}

func hasField(errs []EntryError, field string) bool {
	for _, e := range errs {
		if e.Field == field {
			return true
		}
	}
	return false
}

func TestValidateArchiveAcceptsValidArchive(t *testing.T) {
	a := &Archive{Version: SchemaVersion, Tunnels: []TunnelEntry{validEntry("prod-db")}}
	if errs := ValidateArchive(a); len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", fieldsWithErrors(errs))
	}
}

func TestValidateArchiveRejectsWrongVersion(t *testing.T) {
	for _, version := range []int{0, 2, 99} {
		a := &Archive{Version: version, Tunnels: []TunnelEntry{validEntry("prod-db")}}
		errs := ValidateArchive(a)
		if len(errs) == 0 {
			t.Fatalf("version %d: expected an error, got none", version)
		}
		if !hasField(errs, "version") {
			t.Errorf("version %d: expected a version error, got %v", version, fieldsWithErrors(errs))
		}
	}
}

func TestValidateArchiveRejectsDuplicateNames(t *testing.T) {
	a := &Archive{Version: SchemaVersion, Tunnels: []TunnelEntry{
		validEntry("prod-db"),
		validEntry("prod-db"),
	}}
	errs := ValidateArchive(a)
	if !hasField(errs, "name") {
		t.Fatalf("expected a duplicate-name error, got %v", fieldsWithErrors(errs))
	}
	if !strings.Contains(errs[0].Msg, "duplicate") {
		t.Errorf("expected message to mention duplication, got %q", errs[0].Msg)
	}
}

func TestValidateArchiveReportsEveryProblemAtOnce(t *testing.T) {
	// One entry, four independent problems. All must come back together so the
	// file can be fixed in a single pass.
	a := &Archive{Version: SchemaVersion, Tunnels: []TunnelEntry{{
		Name:          "",
		Type:          "sideways",
		DesiredStatus: "humming",
		Hops:          nil,
	}}}
	errs := ValidateArchive(a)
	for _, want := range []string{"name", "type", "desired_status", "hops"} {
		if !hasField(errs, want) {
			t.Errorf("missing error for %q; got %v", want, fieldsWithErrors(errs))
		}
	}
}

func TestValidateArchiveChecksHopFields(t *testing.T) {
	e := validEntry("prod-db")
	e.Hops = []HopEntry{{Host: "", Port: 0, User: "", AuthMethod: "telepathy"}}
	errs := ValidateArchive(&Archive{Version: SchemaVersion, Tunnels: []TunnelEntry{e}})
	for _, want := range []string{"hops[0].host", "hops[0].port", "hops[0].user", "hops[0].auth_method"} {
		if !hasField(errs, want) {
			t.Errorf("missing error for %q; got %v", want, fieldsWithErrors(errs))
		}
	}
}

func TestValidateArchiveAllowsDynamicWithoutRemoteHost(t *testing.T) {
	// A SOCKS5 proxy has no fixed destination. Rejecting it here would make
	// exported dynamic tunnels fail to re-import.
	e := TunnelEntry{
		Name:      "socks",
		Type:      "dynamic",
		LocalPort: 1080,
		Hops:      []HopEntry{{Host: "jump", Port: 22, User: "deploy", AuthMethod: "key"}},
	}
	if errs := ValidateArchive(&Archive{Version: SchemaVersion, Tunnels: []TunnelEntry{e}}); len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", fieldsWithErrors(errs))
	}
}

func TestValidateArchiveRequiresRemoteHostForLocal(t *testing.T) {
	e := validEntry("prod-db")
	e.RemoteHost = ""
	e.RemotePort = 0
	errs := ValidateArchive(&Archive{Version: SchemaVersion, Tunnels: []TunnelEntry{e}})
	if !hasField(errs, "remote_host") || !hasField(errs, "remote_port") {
		t.Fatalf("expected remote_host and remote_port errors, got %v", fieldsWithErrors(errs))
	}
}

func TestValidateArchiveAcceptsEmptyTunnelList(t *testing.T) {
	if errs := ValidateArchive(&Archive{Version: SchemaVersion}); len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", fieldsWithErrors(errs))
	}
}
