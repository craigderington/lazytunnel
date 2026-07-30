package cli

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/craigderington/lazytunnel/internal/api"
	"github.com/craigderington/lazytunnel/pkg/types"
)

// sampleCreateRequest is a realistic request with every field populated.
func sampleCreateRequest() createTunnelRequest {
	return createTunnelRequest{
		Name: "prod-db",
		Type: "local",
		Hops: []types.Hop{{
			Host:       "bastion.example.com",
			Port:       22,
			User:       "deploy",
			AuthMethod: types.AuthMethodKey,
			KeyID:      "/home/deploy/.ssh/id_ed25519",
		}},
		LocalPort:     5432,
		RemoteHost:    "db.internal.example.com",
		RemotePort:    5432,
		AutoReconnect: true,
		KeepAlive:     30,
		MaxRetries:    3,
	}
}

// withCreateFlags sets the package-level create flags to a valid local-tunnel
// configuration and restores them afterwards, so tests in this package cannot
// leak state into each other. Capture happens before assignment — see
// callExport in export_test.go for the same pattern.
//
// Tests that need a different shape call this and then override the specific
// vars they care about; the cleanup still restores everything.
func withCreateFlags(t *testing.T) {
	t.Helper()

	prevName := tunnelName
	prevType := tunnelType
	prevLocalPort := localPort
	prevRemoteHost := remoteHost
	prevRemotePort := remotePort
	prevHops := hops
	prevUser := sshUser
	prevKey := sshKey
	prevAutoReconnect := autoReconnect
	prevKeepAlive := keepAlive
	prevMaxRetries := maxRetries

	t.Cleanup(func() {
		tunnelName = prevName
		tunnelType = prevType
		localPort = prevLocalPort
		remoteHost = prevRemoteHost
		remotePort = prevRemotePort
		hops = prevHops
		sshUser = prevUser
		sshKey = prevKey
		autoReconnect = prevAutoReconnect
		keepAlive = prevKeepAlive
		maxRetries = prevMaxRetries
	})

	tunnelName = "prod-db"
	tunnelType = "local"
	localPort = 5432
	remoteHost = "db.internal.example.com:5432"
	remotePort = 0
	hops = []string{"bastion.example.com:22"}
	sshUser = "deploy"
	sshKey = "/home/deploy/.ssh/id_ed25519"
	autoReconnect = true
	keepAlive = 30
	maxRetries = 3
}

// decodeAsServer marshals the CLI's request and decodes it exactly as the
// server does, returning what the handler would actually see.
func decodeAsServer(t *testing.T, req createTunnelRequest) api.CreateTunnelRequest {
	t.Helper()

	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshalling the CLI request failed: %v", err)
	}

	var server api.CreateTunnelRequest
	if err := json.Unmarshal(body, &server); err != nil {
		t.Fatalf("the server could not decode the CLI's body: %v\nbody: %s", err, body)
	}
	return server
}

func TestCreateRequestSurvivesTheWire(t *testing.T) {
	req := sampleCreateRequest()
	got := decodeAsServer(t, req)

	if got.Name != req.Name {
		t.Errorf("Name: got %q, want %q", got.Name, req.Name)
	}
	if got.Type != req.Type {
		t.Errorf("Type: got %q, want %q", got.Type, req.Type)
	}
	if got.LocalPort != req.LocalPort {
		t.Errorf("LocalPort: got %d, want %d", got.LocalPort, req.LocalPort)
	}
	if got.RemoteHost != req.RemoteHost {
		t.Errorf("RemoteHost: got %q, want %q — this is the field the original bug dropped", got.RemoteHost, req.RemoteHost)
	}
	if got.RemotePort != req.RemotePort {
		t.Errorf("RemotePort: got %d, want %d", got.RemotePort, req.RemotePort)
	}
	if got.AutoReconnect != req.AutoReconnect {
		t.Errorf("AutoReconnect: got %v, want %v", got.AutoReconnect, req.AutoReconnect)
	}
	if got.KeepAlive != req.KeepAlive {
		t.Errorf("KeepAlive: got %d, want %d", got.KeepAlive, req.KeepAlive)
	}
	if got.MaxRetries != req.MaxRetries {
		t.Errorf("MaxRetries: got %d, want %d", got.MaxRetries, req.MaxRetries)
	}

	if len(got.Hops) != 1 {
		t.Fatalf("Hops: got %d, want 1", len(got.Hops))
	}
	if got.Hops[0].Host != "bastion.example.com" {
		t.Errorf("hop Host: got %q, want bastion.example.com", got.Hops[0].Host)
	}
	if got.Hops[0].Port != 22 {
		t.Errorf("hop Port: got %d, want 22", got.Hops[0].Port)
	}
	if got.Hops[0].User != "deploy" {
		t.Errorf("hop User: got %q, want deploy", got.Hops[0].User)
	}
	if got.Hops[0].AuthMethod != "key" {
		t.Errorf("hop AuthMethod: got %q, want key", got.Hops[0].AuthMethod)
	}
	if got.Hops[0].KeyID != "/home/deploy/.ssh/id_ed25519" {
		t.Errorf("hop KeyID: got %q", got.Hops[0].KeyID)
	}
}

func TestCreateRequestPassesServerValidation(t *testing.T) {
	got := decodeAsServer(t, sampleCreateRequest())

	if errs := api.ValidateRequest(&got); len(errs) != 0 {
		t.Fatalf("the server would reject the CLI's own request: %+v", errs)
	}
}

func TestCreateRequestRejectsNanosecondKeepAlive(t *testing.T) {
	// Regression guard for the second half of the original defect.
	// types.TunnelSpec.KeepAlive is a time.Duration, so marshalling it sent
	// 30000000000 into a field validated as whole seconds with max=300.
	// Fixing only the JSON key names would have left the command broken, so
	// this test pins the encoding as well as the name.
	req := sampleCreateRequest()
	req.KeepAlive = int(30 * time.Second) // 30000000000, the old wire value

	got := decodeAsServer(t, req)

	errs := api.ValidateRequest(&got)
	if len(errs) == 0 {
		t.Fatal("a nanosecond keepalive must fail server validation; if this passes, the seconds-vs-nanoseconds guard is gone")
	}
}

func TestDynamicTunnelNeedsNoDestination(t *testing.T) {
	// A SOCKS5 proxy has no fixed destination. The API validator required
	// remoteHost and remotePort for every type, so a dynamic tunnel could
	// never be created — despite create.go's help text documenting it.
	req := createTunnelRequest{
		Name: "socks",
		Type: "dynamic",
		Hops: []types.Hop{{
			Host:       "jumphost.example.com",
			Port:       22,
			User:       "deploy",
			AuthMethod: types.AuthMethodKey,
			KeyID:      "/home/deploy/.ssh/id_ed25519",
		}},
		LocalPort:     1080,
		AutoReconnect: true,
		KeepAlive:     30,
		MaxRetries:    3,
	}

	got := decodeAsServer(t, req)

	if errs := api.ValidateRequest(&got); len(errs) != 0 {
		t.Fatalf("the server must accept a dynamic tunnel with no destination: %+v", errs)
	}
}

func TestLocalTunnelStillRequiresADestination(t *testing.T) {
	// Making the destination conditional must not weaken it for the types
	// that genuinely need one.
	req := sampleCreateRequest()
	req.RemoteHost = ""
	req.RemotePort = 0

	got := decodeAsServer(t, req)

	if errs := api.ValidateRequest(&got); len(errs) == 0 {
		t.Fatal("a local tunnel with no destination must still be rejected")
	}
}

func TestRemoteTunnelDestinationIsTransmitted(t *testing.T) {
	// The original builder discarded --remote-host for every non-local type.
	withCreateFlags(t)
	tunnelType = "remote"
	remoteHost = "internal.example.com"
	remotePort = 9090

	req, err := buildCreateRequest()
	if err != nil {
		t.Fatalf("buildCreateRequest returned error: %v", err)
	}
	if req.RemoteHost != "internal.example.com" {
		t.Errorf("got RemoteHost %q, want internal.example.com — the flag must not be discarded", req.RemoteHost)
	}
	if req.RemotePort != 9090 {
		t.Errorf("got RemotePort %d, want 9090", req.RemotePort)
	}

	got := decodeAsServer(t, req)
	if errs := api.ValidateRequest(&got); len(errs) != 0 {
		t.Fatalf("server would reject a remote tunnel: %+v", errs)
	}
}

func TestDynamicTunnelSendsNoDestination(t *testing.T) {
	withCreateFlags(t)
	tunnelType = "dynamic"
	remoteHost = "should-be-ignored.example.com"
	remotePort = 9090
	localPort = 1080

	req, err := buildCreateRequest()
	if err != nil {
		t.Fatalf("buildCreateRequest returned error: %v", err)
	}
	if req.RemoteHost != "" {
		t.Errorf("got RemoteHost %q, want empty — a SOCKS5 proxy has no destination", req.RemoteHost)
	}
	if req.RemotePort != 0 {
		t.Errorf("got RemotePort %d, want 0", req.RemotePort)
	}
}

func TestCreateRequestOmitsFieldsTheCLICannotSet(t *testing.T) {
	// agentId has no flag, so the CLI must not send it — an empty value
	// would be indistinguishable from a deliberate choice.
	body, err := json.Marshal(sampleCreateRequest())
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if _, present := raw["agentId"]; present {
		t.Error("request must not carry agentId; the CLI has no flag for it")
	}
}
