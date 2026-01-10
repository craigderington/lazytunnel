package auth

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/craigderington/lazytunnel/pkg/types"
)

func TestAuthFactory(t *testing.T) {
	factory := NewAuthFactory()

	tests := []struct {
		name       string
		authConfig *types.AuthConfig
		hop        *types.Hop
		wantError  bool
	}{
		{
			name:       "agent auth",
			authConfig: &types.AuthConfig{Method: types.AuthMethodAgent},
			hop: &types.Hop{
				Host:       "example.com",
				Port:       22,
				User:       "testuser",
				AuthMethod: types.AuthMethodAgent,
			},
			wantError: false,
		},
		{
			name:       "key auth without key_id",
			authConfig: &types.AuthConfig{Method: types.AuthMethodKey},
			hop: &types.Hop{
				Host:       "example.com",
				Port:       22,
				User:       "testuser",
				AuthMethod: types.AuthMethodKey,
				KeyID:      "",
			},
			wantError: true,
		},
		{
			name:       "unsupported auth method",
			authConfig: &types.AuthConfig{},
			hop: &types.Hop{
				Host:       "example.com",
				Port:       22,
				User:       "testuser",
				AuthMethod: "unsupported",
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auth, err := factory.CreateAuthenticator(tt.authConfig, tt.hop)

			if tt.wantError {
				if err == nil {
					t.Errorf("CreateAuthenticator() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("CreateAuthenticator() unexpected error: %v", err)
				return
			}

			if auth == nil {
				t.Errorf("CreateAuthenticator() returned nil authenticator")
			}
		})
	}
}

func TestMultiAuthenticator(t *testing.T) {
	// Create a multi-authenticator with agent auth
	agentAuth := NewAgentAuthenticator()
	multiAuth := NewMultiAuthenticator(agentAuth)

	if len(multiAuth.authenticators) != 1 {
		t.Errorf("MultiAuthenticator has %d authenticators, want 1", len(multiAuth.authenticators))
	}

	// GetAuthMethods should try all authenticators
	methods, err := multiAuth.GetAuthMethods()

	// This may fail if SSH_AUTH_SOCK is not set, which is expected in CI
	// We just verify the method was attempted
	if err != nil && os.Getenv("SSH_AUTH_SOCK") != "" {
		t.Logf("GetAuthMethods() error (expected if no agent available): %v", err)
	}

	// If SSH agent is available, we should get at least one method
	if err == nil && len(methods) == 0 {
		t.Errorf("GetAuthMethods() returned 0 methods")
	}
}

func TestPasswordAuthenticator(t *testing.T) {
	password := "testpassword123"
	auth := NewPasswordAuthenticator(password)

	method, err := auth.GetAuthMethod()
	if err != nil {
		t.Errorf("GetAuthMethod() unexpected error: %v", err)
	}

	if method == nil {
		t.Errorf("GetAuthMethod() returned nil")
	}
}

func TestInsecureHostKeyCallback(t *testing.T) {
	callback := &InsecureHostKeyCallback{}
	cb := callback.GetCallback()

	if cb == nil {
		t.Errorf("GetCallback() returned nil")
	}
}

func TestKnownHostsCallback(t *testing.T) {
	// Create a temporary known_hosts file
	tmpDir := t.TempDir()
	knownHostsPath := filepath.Join(tmpDir, "known_hosts")

	// Write a simple known_hosts entry
	content := "example.com ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQC...\n"
	if err := os.WriteFile(knownHostsPath, []byte(content), 0600); err != nil {
		t.Fatalf("Failed to write test known_hosts: %v", err)
	}

	callback := NewKnownHostsCallback(knownHostsPath)
	if callback.knownHostsPath != knownHostsPath {
		t.Errorf("knownHostsPath = %s, want %s", callback.knownHostsPath, knownHostsPath)
	}

	// GetCallback should not return nil even if parsing fails
	cb := callback.GetCallback()
	if cb == nil {
		t.Errorf("GetCallback() returned nil")
	}
}

func TestKnownHostsCallbackNonexistent(t *testing.T) {
	callback := NewKnownHostsCallback("/nonexistent/path/known_hosts")

	// Should fall back to insecure callback
	cb := callback.GetCallback()
	if cb == nil {
		t.Errorf("GetCallback() returned nil")
	}
}

func TestAgentAuthenticatorNoSocket(t *testing.T) {
	// Save current SSH_AUTH_SOCK
	originalSocket := os.Getenv("SSH_AUTH_SOCK")
	defer os.Setenv("SSH_AUTH_SOCK", originalSocket)

	// Unset SSH_AUTH_SOCK
	os.Unsetenv("SSH_AUTH_SOCK")

	auth := NewAgentAuthenticator()
	_, err := auth.GetAuthMethod()

	if err == nil {
		t.Errorf("GetAuthMethod() expected error with no SSH_AUTH_SOCK, got nil")
	}
}

func TestAgentAuthenticatorWithSocket(t *testing.T) {
	customSocket := "/tmp/custom-ssh-agent.sock"
	auth := NewAgentAuthenticatorWithSocket(customSocket)

	if auth.socket != customSocket {
		t.Errorf("socket = %s, want %s", auth.socket, customSocket)
	}

	// This will fail since the socket doesn't exist, but we're testing the path
	_, err := auth.GetAuthMethod()
	if err == nil {
		t.Errorf("GetAuthMethod() expected error with nonexistent socket, got nil")
	}
}

func TestKeyAuthenticatorMissingFile(t *testing.T) {
	auth := NewKeyAuthenticator("/nonexistent/key", "")
	_, err := auth.GetAuthMethod()

	if err == nil {
		t.Errorf("GetAuthMethod() expected error with nonexistent key file, got nil")
	}
}

func TestInteractiveAuthenticator(t *testing.T) {
	challenge := func(user, instruction string, questions []string, echos []bool) ([]string, error) {
		return []string{"response1"}, nil
	}

	auth := NewInteractiveAuthenticator(challenge)
	method, err := auth.GetAuthMethod()

	if err != nil {
		t.Errorf("GetAuthMethod() unexpected error: %v", err)
	}

	if method == nil {
		t.Errorf("GetAuthMethod() returned nil")
	}

	// Note: We can't actually invoke the challenge without a real SSH connection
	// So we just verify the method was created
}

func TestCreateMultiAuthenticator(t *testing.T) {
	factory := NewAuthFactory()

	tests := []struct {
		name       string
		authConfig *types.AuthConfig
		hop        *types.Hop
		wantError  bool
	}{
		{
			name: "with agent enabled",
			authConfig: &types.AuthConfig{
				Method:   types.AuthMethodAgent,
				UseAgent: true,
			},
			hop: &types.Hop{
				Host:       "example.com",
				Port:       22,
				User:       "testuser",
				AuthMethod: types.AuthMethodAgent,
			},
			wantError: false,
		},
		{
			name: "agent disabled",
			authConfig: &types.AuthConfig{
				Method:   types.AuthMethodAgent,
				UseAgent: false,
			},
			hop: &types.Hop{
				Host:       "example.com",
				Port:       22,
				User:       "testuser",
				AuthMethod: types.AuthMethodAgent,
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auth, err := factory.CreateMultiAuthenticator(tt.authConfig, tt.hop)

			if tt.wantError {
				if err == nil {
					t.Errorf("CreateMultiAuthenticator() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("CreateMultiAuthenticator() unexpected error: %v", err)
				return
			}

			if auth == nil {
				t.Errorf("CreateMultiAuthenticator() returned nil")
				return
			}

			if len(auth.authenticators) == 0 {
				t.Errorf("CreateMultiAuthenticator() returned empty authenticator list")
			}
		})
	}
}
