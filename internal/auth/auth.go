package auth

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/craigderington/lazytunnel/pkg/types"
)

// expandPath expands ~ to the user's home directory
func expandPath(path string) (string, error) {
	if path == "" {
		return path, nil
	}

	// If path starts with ~/, expand to home directory
	if strings.HasPrefix(path, "~/") {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get home directory: %w", err)
		}
		return filepath.Join(homeDir, path[2:]), nil
	}

	// If path is just ~, return home directory
	if path == "~" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get home directory: %w", err)
		}
		return homeDir, nil
	}

	// Otherwise, return as-is
	return path, nil
}

// Authenticator handles SSH authentication
type Authenticator interface {
	GetAuthMethod() (ssh.AuthMethod, error)
}

// KeyAuthenticator implements key-based authentication
type KeyAuthenticator struct {
	keyPath    string
	passphrase string
}

// NewKeyAuthenticator creates a new key-based authenticator
func NewKeyAuthenticator(keyPath, passphrase string) *KeyAuthenticator {
	return &KeyAuthenticator{
		keyPath:    keyPath,
		passphrase: passphrase,
	}
}

// GetAuthMethod returns the SSH auth method for key authentication
func (k *KeyAuthenticator) GetAuthMethod() (ssh.AuthMethod, error) {
	// Expand ~ to home directory
	expandedPath, err := expandPath(k.keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to expand key path %s: %w", k.keyPath, err)
	}

	key, err := os.ReadFile(expandedPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read private key from %s: %w", expandedPath, err)
	}

	var signer ssh.Signer
	if k.passphrase != "" {
		signer, err = ssh.ParsePrivateKeyWithPassphrase(key, []byte(k.passphrase))
	} else {
		signer, err = ssh.ParsePrivateKey(key)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	return ssh.PublicKeys(signer), nil
}

// AgentAuthenticator implements SSH agent-based authentication
type AgentAuthenticator struct {
	socket string
}

// NewAgentAuthenticator creates a new agent-based authenticator
func NewAgentAuthenticator() *AgentAuthenticator {
	socket := os.Getenv("SSH_AUTH_SOCK")
	return &AgentAuthenticator{
		socket: socket,
	}
}

// NewAgentAuthenticatorWithSocket creates an agent authenticator with custom socket
func NewAgentAuthenticatorWithSocket(socket string) *AgentAuthenticator {
	return &AgentAuthenticator{
		socket: socket,
	}
}

// GetAuthMethod returns the SSH auth method for agent authentication
func (a *AgentAuthenticator) GetAuthMethod() (ssh.AuthMethod, error) {
	if a.socket == "" {
		return nil, fmt.Errorf("SSH_AUTH_SOCK not set and no socket provided")
	}

	conn, err := net.Dial("unix", a.socket)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to SSH agent at %s: %w", a.socket, err)
	}

	agentClient := agent.NewClient(conn)
	return ssh.PublicKeysCallback(agentClient.Signers), nil
}

// PasswordAuthenticator implements password-based authentication
type PasswordAuthenticator struct {
	password string
}

// NewPasswordAuthenticator creates a new password-based authenticator
func NewPasswordAuthenticator(password string) *PasswordAuthenticator {
	return &PasswordAuthenticator{
		password: password,
	}
}

// GetAuthMethod returns the SSH auth method for password authentication
func (p *PasswordAuthenticator) GetAuthMethod() (ssh.AuthMethod, error) {
	return ssh.Password(p.password), nil
}

// InteractiveAuthenticator implements keyboard-interactive authentication
type InteractiveAuthenticator struct {
	challenge func(user, instruction string, questions []string, echos []bool) ([]string, error)
}

// NewInteractiveAuthenticator creates a new keyboard-interactive authenticator
func NewInteractiveAuthenticator(challenge func(string, string, []string, []bool) ([]string, error)) *InteractiveAuthenticator {
	return &InteractiveAuthenticator{
		challenge: challenge,
	}
}

// GetAuthMethod returns the SSH auth method for keyboard-interactive authentication
func (i *InteractiveAuthenticator) GetAuthMethod() (ssh.AuthMethod, error) {
	return ssh.KeyboardInteractive(i.challenge), nil
}

// CertAuthenticator implements certificate-based authentication
type CertAuthenticator struct {
	certPath string
	keyPath  string
}

// NewCertAuthenticator creates a new certificate-based authenticator
func NewCertAuthenticator(certPath, keyPath string) *CertAuthenticator {
	return &CertAuthenticator{
		certPath: certPath,
		keyPath:  keyPath,
	}
}

// GetAuthMethod returns the SSH auth method for certificate authentication
func (c *CertAuthenticator) GetAuthMethod() (ssh.AuthMethod, error) {
	// Expand paths
	expandedKeyPath, err := expandPath(c.keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to expand key path: %w", err)
	}
	expandedCertPath, err := expandPath(c.certPath)
	if err != nil {
		return nil, fmt.Errorf("failed to expand cert path: %w", err)
	}

	// Read the private key
	keyData, err := os.ReadFile(expandedKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read private key: %w", err)
	}

	privateKey, err := ssh.ParsePrivateKey(keyData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	// Read the certificate
	certData, err := os.ReadFile(expandedCertPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read certificate: %w", err)
	}

	pubKey, _, _, _, err := ssh.ParseAuthorizedKey(certData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate: %w", err)
	}

	cert, ok := pubKey.(*ssh.Certificate)
	if !ok {
		return nil, fmt.Errorf("public key is not a certificate")
	}

	// Create a cert signer
	certSigner, err := ssh.NewCertSigner(cert, privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create cert signer: %w", err)
	}

	return ssh.PublicKeys(certSigner), nil
}

// MultiAuthenticator tries multiple authentication methods in order
type MultiAuthenticator struct {
	authenticators []Authenticator
}

// NewMultiAuthenticator creates a new multi-method authenticator
func NewMultiAuthenticator(authenticators ...Authenticator) *MultiAuthenticator {
	return &MultiAuthenticator{
		authenticators: authenticators,
	}
}

// GetAuthMethods returns all SSH auth methods to try
func (m *MultiAuthenticator) GetAuthMethods() ([]ssh.AuthMethod, error) {
	methods := make([]ssh.AuthMethod, 0, len(m.authenticators))

	for _, auth := range m.authenticators {
		method, err := auth.GetAuthMethod()
		if err != nil {
			// Log but continue with other methods
			continue
		}
		methods = append(methods, method)
	}

	if len(methods) == 0 {
		return nil, fmt.Errorf("no valid authentication methods available")
	}

	return methods, nil
}

// AuthFactory creates authenticators based on configuration
type AuthFactory struct{}

// NewAuthFactory creates a new authentication factory
func NewAuthFactory() *AuthFactory {
	return &AuthFactory{}
}

// CreateAuthenticator creates an authenticator based on the auth configuration
func (f *AuthFactory) CreateAuthenticator(authConfig *types.AuthConfig, hop *types.Hop) (Authenticator, error) {
	switch hop.AuthMethod {
	case types.AuthMethodKey:
		if hop.KeyID == "" {
			return nil, fmt.Errorf("key_id is required for key authentication")
		}
		// TODO: Integrate with KMS to retrieve key
		// For now, treat KeyID as file path
		return NewKeyAuthenticator(hop.KeyID, ""), nil

	case types.AuthMethodPassword:
		// TODO: Retrieve password from secure storage
		return nil, fmt.Errorf("password authentication requires secure storage integration")

	case types.AuthMethodAgent:
		return NewAgentAuthenticator(), nil

	case types.AuthMethodCert:
		// TODO: Implement certificate authentication
		return nil, fmt.Errorf("certificate authentication not yet implemented")

	default:
		return nil, fmt.Errorf("unsupported authentication method: %s", hop.AuthMethod)
	}
}

// CreateMultiAuthenticator creates a multi-method authenticator trying multiple strategies
func (f *AuthFactory) CreateMultiAuthenticator(authConfig *types.AuthConfig, hop *types.Hop) (*MultiAuthenticator, error) {
	var authenticators []Authenticator

	// Try agent first if UseAgent is enabled
	if authConfig.UseAgent {
		authenticators = append(authenticators, NewAgentAuthenticator())
	}

	// Then try the specified method
	auth, err := f.CreateAuthenticator(authConfig, hop)
	if err == nil {
		authenticators = append(authenticators, auth)
	}

	if len(authenticators) == 0 {
		return nil, fmt.Errorf("no authentication methods available")
	}

	return NewMultiAuthenticator(authenticators...), nil
}

// HostKeyCallback provides host key verification strategies
type HostKeyCallback interface {
	GetCallback() ssh.HostKeyCallback
}

// InsecureHostKeyCallback accepts any host key (INSECURE - for development only)
type InsecureHostKeyCallback struct{}

// GetCallback returns the insecure host key callback
func (i *InsecureHostKeyCallback) GetCallback() ssh.HostKeyCallback {
	return ssh.InsecureIgnoreHostKey()
}

// KnownHostsCallback verifies host keys against known_hosts file
type KnownHostsCallback struct {
	knownHostsPath string
}

// NewKnownHostsCallback creates a new known hosts callback
func NewKnownHostsCallback(path string) *KnownHostsCallback {
	if path == "" {
		path = os.ExpandEnv("$HOME/.ssh/known_hosts")
	}
	return &KnownHostsCallback{
		knownHostsPath: path,
	}
}

// GetCallback returns the known hosts callback
func (k *KnownHostsCallback) GetCallback() ssh.HostKeyCallback {
	callback, err := knownHostsCallback(k.knownHostsPath)
	if err != nil {
		// Fall back to insecure if known_hosts can't be loaded
		return ssh.InsecureIgnoreHostKey()
	}
	return callback
}

// knownHostsCallback creates a host key callback from known_hosts file
func knownHostsCallback(path string) (ssh.HostKeyCallback, error) {
	callback, err := knownhosts.New(path)
	if err != nil {
		return nil, fmt.Errorf("failed to parse known_hosts file: %w", err)
	}

	return callback, nil
}
