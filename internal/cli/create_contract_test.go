package cli

import (
	"encoding/json"
	"strings"
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
	prevLocalBindAddress := localBindAddress
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
		localBindAddress = prevLocalBindAddress
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
	localBindAddress = "127.0.0.1"
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

// TestBuildCreateRequestValidatesTunnelType pins the per-type validation
// switch that the original runCreate opened with (git show
// 0206f33:internal/cli/create.go, lines 79-97). Task 1's brief said "the
// parsing logic moves verbatim" but its code sample omitted this switch,
// which meant a `remote` tunnel with no `--local-port` silently built a
// request instead of failing fast — the request would have been accepted by
// the server and only failed asynchronously once the agent tried to forward,
// per internal/tunnel/forward.go:327. These guards must live in
// buildCreateRequest so a false success is impossible again.
func TestBuildCreateRequestValidatesTunnelType(t *testing.T) {
	tests := []struct {
		name      string
		configure func()
		wantErr   string // substring expected in the error
	}{
		{
			name: "remote without --local-port errors",
			configure: func() {
				tunnelType = "remote"
				localPort = 0
				remotePort = 9090
			},
			wantErr: "--local-port is required for remote tunnels",
		},
		{
			name: "remote without --remote-port errors",
			configure: func() {
				tunnelType = "remote"
				localPort = 8080
				remotePort = 0
			},
			wantErr: "--remote-port is required for remote tunnels",
		},
		{
			name: "local without --remote-host errors",
			configure: func() {
				tunnelType = "local"
				remoteHost = ""
			},
			wantErr: "--remote-host is required for local tunnels",
		},
		{
			name: "unrecognized type errors with the friendly message",
			configure: func() {
				tunnelType = "BOGUS"
			},
			wantErr: "invalid tunnel type: BOGUS (must be local, remote, or dynamic)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withCreateFlags(t)
			tt.configure()

			_, err := buildCreateRequest()
			if err == nil {
				t.Fatalf("buildCreateRequest returned no error, want one containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("got error %q, want it to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

// TestBuildCreateRequestAcceptsUppercaseType pins strings.ToLower(tunnelType)
// in the restored switch: --type LOCAL (or any other casing) must still work.
func TestBuildCreateRequestAcceptsUppercaseType(t *testing.T) {
	withCreateFlags(t)
	tunnelType = "LOCAL"

	req, err := buildCreateRequest()
	if err != nil {
		t.Fatalf("buildCreateRequest returned error for --type LOCAL: %v", err)
	}
	if req.Type != "local" {
		t.Errorf("got Type %q, want the lowercased canonical value %q", req.Type, "local")
	}
}

// TestBuildCreateRequestDefaultsHopAuthToKey pins the SSH auth default that
// the original runCreate used (git show 0206f33:internal/cli/create.go,
// lines 113-117): key auth, falling back to $HOME/.ssh/id_rsa when --key is
// not set. An earlier draft of this fix silently switched the default to
// agent auth with an empty KeyID, which is a security-relevant behavior
// change nobody asked for.
func TestBuildCreateRequestDefaultsHopAuthToKey(t *testing.T) {
	t.Run("no --key set", func(t *testing.T) {
		withCreateFlags(t)
		sshKey = ""

		req, err := buildCreateRequest()
		if err != nil {
			t.Fatalf("buildCreateRequest returned error: %v", err)
		}
		if len(req.Hops) != 1 {
			t.Fatalf("got %d hops, want 1", len(req.Hops))
		}
		hop := req.Hops[0]
		if hop.AuthMethod != types.AuthMethodKey {
			t.Errorf("got AuthMethod %q, want %q", hop.AuthMethod, types.AuthMethodKey)
		}
		if !strings.HasSuffix(hop.KeyID, "/.ssh/id_rsa") {
			t.Errorf("got KeyID %q, want it to end in /.ssh/id_rsa", hop.KeyID)
		}
	})

	t.Run("--key set", func(t *testing.T) {
		withCreateFlags(t)
		sshKey = "/home/deploy/.ssh/id_ed25519"

		req, err := buildCreateRequest()
		if err != nil {
			t.Fatalf("buildCreateRequest returned error: %v", err)
		}
		if len(req.Hops) != 1 {
			t.Fatalf("got %d hops, want 1", len(req.Hops))
		}
		hop := req.Hops[0]
		if hop.AuthMethod != types.AuthMethodKey {
			t.Errorf("got AuthMethod %q, want %q", hop.AuthMethod, types.AuthMethodKey)
		}
		if hop.KeyID != sshKey {
			t.Errorf("got KeyID %q, want %q", hop.KeyID, sshKey)
		}
	})
}

// TestBuildCreateRequestKeepAliveIsSeconds asserts directly on the CLI's own
// encoding, rather than on a hand-built createTunnelRequest fed into the
// server's validator. TestCreateRequestRejectsNanosecondKeepAlive guards the
// API's max=300 tag but never exercises buildCreateRequest itself: a reviewer
// mutated the builder back to nanoseconds and only
// TestRemoteTunnelDestinationIsTransmitted caught it — incidental coverage
// under a name that doesn't mention keepalive at all. This test pins the
// builder's encoding directly so that regression fails under its own name.
func TestBuildCreateRequestKeepAliveIsSeconds(t *testing.T) {
	withCreateFlags(t)
	keepAlive = 30

	req, err := buildCreateRequest()
	if err != nil {
		t.Fatalf("buildCreateRequest returned error: %v", err)
	}
	if req.KeepAlive != 30 {
		t.Errorf("got KeepAlive %d, want 30 (whole seconds, not nanoseconds)", req.KeepAlive)
	}
}

func TestLocalBindAddressFlagDefaultsToLoopback(t *testing.T) {
	f := createCmd.Flags().Lookup("local-bind-address")
	if f == nil {
		t.Fatal("--local-bind-address is not registered")
	}
	if f.DefValue != "127.0.0.1" {
		t.Fatalf("got default %q, want 127.0.0.1 — a CLI-created tunnel must not listen on all interfaces by accident", f.DefValue)
	}
}

func TestBuildCreateRequestCarriesBindAddress(t *testing.T) {
	withCreateFlags(t)

	req, err := buildCreateRequest()
	if err != nil {
		t.Fatalf("buildCreateRequest returned error: %v", err)
	}
	if req.LocalBindAddress != "127.0.0.1" {
		t.Fatalf("got LocalBindAddress %q, want 127.0.0.1", req.LocalBindAddress)
	}

	got := decodeAsServer(t, req)
	if got.LocalBindAddress != "127.0.0.1" {
		t.Fatalf("the server received LocalBindAddress %q, want 127.0.0.1", got.LocalBindAddress)
	}
	if errs := api.ValidateRequest(&got); len(errs) != 0 {
		t.Fatalf("server would reject a loopback bind address: %+v", errs)
	}
}

func TestBuildCreateRequestPassesExplicitAllInterfaces(t *testing.T) {
	// The escape hatch matters as much as the safe default: an operator who
	// genuinely wants all interfaces must be able to say so.
	withCreateFlags(t)
	localBindAddress = "0.0.0.0"

	req, err := buildCreateRequest()
	if err != nil {
		t.Fatalf("buildCreateRequest returned error: %v", err)
	}

	got := decodeAsServer(t, req)
	if got.LocalBindAddress != "0.0.0.0" {
		t.Fatalf("got LocalBindAddress %q, want 0.0.0.0 transmitted unchanged", got.LocalBindAddress)
	}
	if errs := api.ValidateRequest(&got); len(errs) != 0 {
		t.Fatalf("server would reject an explicit 0.0.0.0: %+v", errs)
	}
}
