package cli

import "testing"

func TestParseTunnelListReadsBareArray(t *testing.T) {
	// Exactly the shape handleListTunnels emits: a bare array, flat status
	// string, camelCase createdAt.
	body := []byte(`[
		{"id":"abc123","name":"prod-db","type":"local","status":"active","createdAt":"2026-07-21T10:00:00Z"},
		{"id":"def456","name":"socks","type":"dynamic","status":"stopped","createdAt":"2026-07-20T09:00:00Z"}
	]`)

	items, err := parseTunnelList(body)
	if err != nil {
		t.Fatalf("parseTunnelList returned error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
	if items[0].ID != "abc123" {
		t.Errorf("got ID %q, want abc123", items[0].ID)
	}
	if items[0].Name != "prod-db" {
		t.Errorf("got Name %q, want prod-db", items[0].Name)
	}
	if items[0].Status != "active" {
		t.Errorf("got Status %q, want active", items[0].Status)
	}
	if items[0].CreatedAt != "2026-07-21T10:00:00Z" {
		t.Errorf("got CreatedAt %q, want 2026-07-21T10:00:00Z", items[0].CreatedAt)
	}
}

func TestParseTunnelListEmptyArray(t *testing.T) {
	items, err := parseTunnelList([]byte(`[]`))
	if err != nil {
		t.Fatalf("parseTunnelList returned error: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("got %d items, want 0", len(items))
	}
}

func TestParseTunnelListRejectsMalformed(t *testing.T) {
	if _, err := parseTunnelList([]byte(`{"tunnels":[]}`)); err == nil {
		t.Fatal("expected an error for an object body, got nil")
	}
}
