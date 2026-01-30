package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	ut "github.com/go-playground/universal-translator"
)

// TestCreateTunnelRequestValidation tests the CreateTunnelRequest struct validation
func TestCreateTunnelRequestValidation(t *testing.T) {
	tests := []struct {
		name    string
		req     CreateTunnelRequest
		wantErr bool
		fields  []string // Expected fields with errors
	}{
		{
			name: "Valid local tunnel",
			req: CreateTunnelRequest{
				Name:       "test-tunnel",
				Type:       "local",
				Hops:       []HopReq{{Host: "bastion.example.com", Port: 22, User: "admin", AuthMethod: "key"}},
				LocalPort:  8080,
				RemoteHost: "internal.example.com",
				RemotePort: 5432,
			},
			wantErr: false,
		},
		{
			name: "Valid remote tunnel",
			req: CreateTunnelRequest{
				Name:       "remote-tunnel",
				Type:       "remote",
				Hops:       []HopReq{{Host: "1.2.3.4", Port: 22, User: "root", AuthMethod: "password"}},
				LocalPort:  0,
				RemoteHost: "192.168.1.1",
				RemotePort: 443,
			},
			wantErr: false,
		},
		{
			name: "Valid dynamic tunnel (SOCKS5)",
			req: CreateTunnelRequest{
				Name:       "socks-proxy",
				Type:       "dynamic",
				Hops:       []HopReq{{Host: "proxy.example.com", Port: 2222, User: "proxy", AuthMethod: "agent"}},
				LocalPort:  1080,
				RemoteHost: "target.example.com",
				RemotePort: 80,
			},
			wantErr: false,
		},
		{
			name: "Valid tunnel with IP addresses",
			req: CreateTunnelRequest{
				Name:             "ip-tunnel",
				Type:             "local",
				Hops:             []HopReq{{Host: "10.0.0.1", Port: 22, User: "user", AuthMethod: "cert"}},
				LocalPort:        3306,
				LocalBindAddress: "127.0.0.1",
				RemoteHost:       "10.0.0.5",
				RemotePort:       3306,
			},
			wantErr: false,
		},
		{
			name: "Missing required name",
			req: CreateTunnelRequest{
				Name:       "",
				Type:       "local",
				Hops:       []HopReq{{Host: "host.com", Port: 22, User: "user", AuthMethod: "key"}},
				RemoteHost: "target.com",
				RemotePort: 80,
			},
			wantErr: true,
			fields:  []string{"Name"},
		},
		{
			name: "Missing required type",
			req: CreateTunnelRequest{
				Name:       "test",
				Type:       "",
				Hops:       []HopReq{{Host: "host.com", Port: 22, User: "user", AuthMethod: "key"}},
				RemoteHost: "target.com",
				RemotePort: 80,
			},
			wantErr: true,
			fields:  []string{"Type"},
		},
		{
			name: "Missing required hops",
			req: CreateTunnelRequest{
				Name:       "test",
				Type:       "local",
				Hops:       []HopReq{},
				RemoteHost: "target.com",
				RemotePort: 80,
			},
			wantErr: true,
			fields:  []string{"Hops"},
		},
		{
			name: "Missing remote host",
			req: CreateTunnelRequest{
				Name:       "test",
				Type:       "local",
				Hops:       []HopReq{{Host: "host.com", Port: 22, User: "user", AuthMethod: "key"}},
				RemoteHost: "",
				RemotePort: 80,
			},
			wantErr: true,
			fields:  []string{"RemoteHost"},
		},
		{
			name: "Missing remote port",
			req: CreateTunnelRequest{
				Name:       "test",
				Type:       "local",
				Hops:       []HopReq{{Host: "host.com", Port: 22, User: "user", AuthMethod: "key"}},
				RemoteHost: "target.com",
				RemotePort: 0,
			},
			wantErr: true,
			fields:  []string{"RemotePort"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors := ValidateRequest(tt.req)
			if tt.wantErr {
				if len(errors) == 0 {
					t.Errorf("ValidateRequest() expected errors but got none")
					return
				}
				if len(tt.fields) > 0 {
					errorFields := make(map[string]bool)
					for _, e := range errors {
						errorFields[e.Field] = true
					}
					for _, field := range tt.fields {
						if !errorFields[field] {
							t.Errorf("Expected error for field %s, but not found in errors: %v", field, errors)
						}
					}
				}
			} else {
				if len(errors) > 0 {
					t.Errorf("ValidateRequest() expected no errors but got: %v", errors)
				}
			}
		})
	}
}

// TestTunnelTypeValidator tests the tunnel type custom validator
func TestTunnelTypeValidator(t *testing.T) {
	tests := []struct {
		name      string
		typeValue string
		wantValid bool
	}{
		{"Valid local", "local", true},
		{"Valid remote", "remote", true},
		{"Valid dynamic", "dynamic", true},
		{"Invalid type - invalid", "invalid", false},
		{"Invalid type - socks", "socks", false},
		{"Invalid type - empty", "", false},
		{"Invalid type - LOCAL (case sensitive)", "LOCAL", false},
		{"Invalid type - Local", "Local", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := CreateTunnelRequest{
				Name:       "test",
				Type:       tt.typeValue,
				Hops:       []HopReq{{Host: "host.com", Port: 22, User: "user", AuthMethod: "key"}},
				RemoteHost: "target.com",
				RemotePort: 80,
			}
			errors := ValidateRequest(req)
			if tt.wantValid {
				for _, err := range errors {
					if err.Field == "Type" {
						t.Errorf("Expected Type to be valid, but got error: %s", err.Message)
					}
				}
			} else {
				found := false
				for _, err := range errors {
					if err.Field == "Type" {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected Type validation error, but got none. Errors: %v", errors)
				}
			}
		})
	}
}

// TestAuthMethodValidator tests the auth method custom validator
func TestAuthMethodValidator(t *testing.T) {
	tests := []struct {
		name       string
		authMethod string
		wantValid  bool
	}{
		{"Valid key", "key", true},
		{"Valid password", "password", true},
		{"Valid agent", "agent", true},
		{"Valid cert", "cert", true},
		{"Invalid - token", "token", false},
		{"Invalid - oauth", "oauth", false},
		{"Invalid - empty", "", false},
		{"Invalid - KEY", "KEY", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := CreateTunnelRequest{
				Name: "test",
				Type: "local",
				Hops: []HopReq{{
					Host:       "host.com",
					Port:       22,
					User:       "user",
					AuthMethod: tt.authMethod,
				}},
				RemoteHost: "target.com",
				RemotePort: 80,
			}
			errors := ValidateRequest(req)
			if tt.wantValid {
				for _, err := range errors {
					if strings.Contains(err.Field, "AuthMethod") {
						t.Errorf("Expected AuthMethod to be valid, but got error: %s", err.Message)
					}
				}
			} else {
				found := false
				for _, err := range errors {
					if strings.Contains(err.Field, "AuthMethod") {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected AuthMethod validation error, but got none. Errors: %v", errors)
				}
			}
		})
	}
}

// TestFieldValidation tests various field validations
func TestFieldValidation(t *testing.T) {
	tests := []struct {
		name        string
		req         CreateTunnelRequest
		expectField string
		expectTag   string
	}{
		{
			name: "Name too long",
			req: CreateTunnelRequest{
				Name:       strings.Repeat("a", 101),
				Type:       "local",
				Hops:       []HopReq{{Host: "host.com", Port: 22, User: "user", AuthMethod: "key"}},
				RemoteHost: "target.com",
				RemotePort: 80,
			},
			expectField: "Name",
			expectTag:   "max",
		},
		{
			name: "Local port negative",
			req: CreateTunnelRequest{
				Name:       "test",
				Type:       "local",
				Hops:       []HopReq{{Host: "host.com", Port: 22, User: "user", AuthMethod: "key"}},
				LocalPort:  -1,
				RemoteHost: "target.com",
				RemotePort: 80,
			},
			expectField: "LocalPort",
			expectTag:   "min",
		},
		{
			name: "Local port too high",
			req: CreateTunnelRequest{
				Name:       "test",
				Type:       "local",
				Hops:       []HopReq{{Host: "host.com", Port: 22, User: "user", AuthMethod: "key"}},
				LocalPort:  70000,
				RemoteHost: "target.com",
				RemotePort: 80,
			},
			expectField: "LocalPort",
			expectTag:   "max",
		},
		{
			name: "Remote port negative",
			req: CreateTunnelRequest{
				Name:       "test",
				Type:       "local",
				Hops:       []HopReq{{Host: "host.com", Port: 22, User: "user", AuthMethod: "key"}},
				RemoteHost: "target.com",
				RemotePort: -1,
			},
			expectField: "RemotePort",
			expectTag:   "min",
		},
		{
			name: "Remote port too high",
			req: CreateTunnelRequest{
				Name:       "test",
				Type:       "local",
				Hops:       []HopReq{{Host: "host.com", Port: 22, User: "user", AuthMethod: "key"}},
				RemoteHost: "target.com",
				RemotePort: 100000,
			},
			expectField: "RemotePort",
			expectTag:   "max",
		},
		{
			name: "Hop port too low",
			req: CreateTunnelRequest{
				Name: "test",
				Type: "local",
				Hops: []HopReq{{
					Host:       "host.com",
					Port:       0,
					User:       "user",
					AuthMethod: "key",
				}},
				RemoteHost: "target.com",
				RemotePort: 80,
			},
			expectField: "Port",
			expectTag:   "min",
		},
		{
			name: "Hop port too high",
			req: CreateTunnelRequest{
				Name: "test",
				Type: "local",
				Hops: []HopReq{{
					Host:       "host.com",
					Port:       99999,
					User:       "user",
					AuthMethod: "key",
				}},
				RemoteHost: "target.com",
				RemotePort: 80,
			},
			expectField: "Port",
			expectTag:   "max",
		},
		{
			name: "Hop user too long",
			req: CreateTunnelRequest{
				Name: "test",
				Type: "local",
				Hops: []HopReq{{
					Host:       "host.com",
					Port:       22,
					User:       strings.Repeat("u", 101),
					AuthMethod: "key",
				}},
				RemoteHost: "target.com",
				RemotePort: 80,
			},
			expectField: "User",
			expectTag:   "max",
		},
		{
			name: "Invalid hostname",
			req: CreateTunnelRequest{
				Name:       "test",
				Type:       "local",
				Hops:       []HopReq{{Host: "host..com", Port: 22, User: "user", AuthMethod: "key"}},
				RemoteHost: "target.com",
				RemotePort: 80,
			},
			expectField: "Host",
			expectTag:   "hostname",
		},

		{
			name: "Invalid remote host",
			req: CreateTunnelRequest{
				Name:       "test",
				Type:       "local",
				Hops:       []HopReq{{Host: "host.com", Port: 22, User: "user", AuthMethod: "key"}},
				RemoteHost: "not a valid hostname!",
				RemotePort: 80,
			},
			expectField: "RemoteHost",
		},
		{
			name: "KeepAlive too high",
			req: CreateTunnelRequest{
				Name:       "test",
				Type:       "local",
				Hops:       []HopReq{{Host: "host.com", Port: 22, User: "user", AuthMethod: "key"}},
				RemoteHost: "target.com",
				RemotePort: 80,
				KeepAlive:  301,
			},
			expectField: "KeepAlive",
			expectTag:   "max",
		},
		{
			name: "MaxRetries too high",
			req: CreateTunnelRequest{
				Name:       "test",
				Type:       "local",
				Hops:       []HopReq{{Host: "host.com", Port: 22, User: "user", AuthMethod: "key"}},
				RemoteHost: "target.com",
				RemotePort: 80,
				MaxRetries: 101,
			},
			expectField: "MaxRetries",
			expectTag:   "max",
		},
		{
			name: "MaxRetries negative",
			req: CreateTunnelRequest{
				Name:       "test",
				Type:       "local",
				Hops:       []HopReq{{Host: "host.com", Port: 22, User: "user", AuthMethod: "key"}},
				RemoteHost: "target.com",
				RemotePort: 80,
				MaxRetries: -1,
			},
			expectField: "MaxRetries",
			expectTag:   "min",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors := ValidateRequest(tt.req)
			found := false
			for _, err := range errors {
				if err.Field == tt.expectField {
					found = true
					t.Logf("Found expected error for field %s: %s", tt.expectField, err.Message)
					break
				}
			}
			if !found {
				t.Errorf("Expected validation error for field %s, but not found. Errors: %v", tt.expectField, errors)
			}
		})
	}
}

// TestMultiHopValidation tests validation with multiple hops
func TestMultiHopValidation(t *testing.T) {
	tests := []struct {
		name    string
		hops    []HopReq
		wantErr bool
	}{
		{
			name: "Single hop",
			hops: []HopReq{
				{Host: "bastion.example.com", Port: 22, User: "admin", AuthMethod: "key"},
			},
			wantErr: false,
		},
		{
			name: "Two hops",
			hops: []HopReq{
				{Host: "bastion1.example.com", Port: 22, User: "admin", AuthMethod: "key"},
				{Host: "bastion2.example.com", Port: 22, User: "admin", AuthMethod: "key"},
			},
			wantErr: false,
		},
		{
			name: "Three hops",
			hops: []HopReq{
				{Host: "hop1.example.com", Port: 22, User: "user1", AuthMethod: "key"},
				{Host: "hop2.internal.com", Port: 22, User: "user2", AuthMethod: "agent"},
				{Host: "hop3.target.com", Port: 2222, User: "root", AuthMethod: "password"},
			},
			wantErr: false,
		},
		{
			name: "Invalid second hop - missing user",
			hops: []HopReq{
				{Host: "bastion.example.com", Port: 22, User: "admin", AuthMethod: "key"},
				{Host: "target.example.com", Port: 22, User: "", AuthMethod: "key"},
			},
			wantErr: true,
		},
		{
			name: "Invalid second hop - bad port",
			hops: []HopReq{
				{Host: "bastion.example.com", Port: 22, User: "admin", AuthMethod: "key"},
				{Host: "target.example.com", Port: 999999, User: "admin", AuthMethod: "key"},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := CreateTunnelRequest{
				Name:       "multi-hop-test",
				Type:       "local",
				Hops:       tt.hops,
				RemoteHost: "final.target.com",
				RemotePort: 5432,
			}
			errors := ValidateRequest(req)
			if tt.wantErr && len(errors) == 0 {
				t.Errorf("Expected validation errors but got none")
			}
			if !tt.wantErr && len(errors) > 0 {
				t.Errorf("Expected no validation errors but got: %v", errors)
			}
		})
	}
}

// TestFormatValidationError tests error message formatting
func TestFormatValidationError(t *testing.T) {
	tests := []struct {
		tag      string
		param    string
		expected string
	}{
		{"required", "", "Field is required"},
		{"min", "1", "Field cannot be empty"},
		{"min", "5", "Field must be at least 5"},
		{"max", "100", "Field must be at most 100"},
		{"hostname", "", "Field must be a valid hostname or IP address"},
		{"ip_addr", "", "Field must be a valid IP address"},
		{"hostname|ip_addr", "", "Field must be a valid hostname or IP address"},
		{"tunneltype", "", "Field must be one of: local, remote, dynamic"},
		{"authmethod", "", "Field must be one of: key, password, agent, cert"},
		{"unknown", "", "Field failed validation: unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.tag, func(t *testing.T) {
			// Create a mock field error
			mockErr := &mockFieldError{
				field: "Field",
				tag:   tt.tag,
				param: tt.param,
			}
			result := formatValidationError(mockErr)
			if result != tt.expected {
				t.Errorf("formatValidationError() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// Mock field error for testing
type mockFieldError struct {
	field string
	tag   string
	param string
}

func (m *mockFieldError) Tag() string                       { return m.tag }
func (m *mockFieldError) ActualTag() string                 { return m.tag }
func (m *mockFieldError) Namespace() string                 { return "" }
func (m *mockFieldError) StructNamespace() string           { return "" }
func (m *mockFieldError) Field() string                     { return m.field }
func (m *mockFieldError) StructField() string               { return "" }
func (m *mockFieldError) Value() interface{}                { return nil }
func (m *mockFieldError) Param() string                     { return m.param }
func (m *mockFieldError) Kind() reflect.Kind                { return reflect.Invalid }
func (m *mockFieldError) Type() reflect.Type                { return nil }
func (m *mockFieldError) Translate(tr ut.Translator) string { return "" }
func (m *mockFieldError) Error() string                     { return "" }

// TestDecodeAndValidate tests the decodeAndValidate function
func TestDecodeAndValidate(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantValid  bool
		wantStatus int
	}{
		{
			name:       "Valid JSON",
			body:       `{"name":"test","type":"local","hops":[{"host":"host.example.com","port":22,"user":"admin","auth_method":"key"}],"remoteHost":"target.example.com","remotePort":80}`,
			wantValid:  true,
			wantStatus: http.StatusOK,
		},
		{
			name:       "Invalid JSON",
			body:       `{"name":"test","type":"local"`,
			wantValid:  false,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "Valid JSON but invalid data",
			body:       `{"name":"","type":"invalid","hops":[],"remoteHost":"","remotePort":0}`,
			wantValid:  false,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "Empty body",
			body:       `{}`,
			wantValid:  false,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/tunnels", bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			var tunnelReq CreateTunnelRequest
			server := &Server{}
			result := server.decodeAndValidate(w, req, &tunnelReq)

			if result != tt.wantValid {
				t.Errorf("decodeAndValidate() = %v, want %v", result, tt.wantValid)
			}

			if w.Code != tt.wantStatus {
				t.Errorf("Status code = %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}

// TestServerValidationError tests the Server.ValidationError method
func TestServerValidationError(t *testing.T) {
	errors := []ValidationError{
		{Field: "Name", Message: "Name is required"},
		{Field: "Type", Message: "Type must be one of: local, remote, dynamic"},
	}

	w := httptest.NewRecorder()
	server := &Server{}
	server.ValidationError(w, "Validation failed", errors)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status code = %d, want %d", w.Code, http.StatusBadRequest)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Content-Type = %s, want application/json", contentType)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if response["message"] != "Validation failed" {
		t.Errorf("Response message = %v, want 'Validation failed'", response["message"])
	}

	// The standardized error response uses "details" not "validations"
	details, ok := response["details"].([]interface{})
	if !ok || len(details) != 2 {
		t.Errorf("Expected 2 validation errors in details, got %v", details)
	}
}

// TestSanitizeString tests string sanitization
func TestSanitizeString(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"normal string", "normal string"},
		{"string\x00with\x00null", "stringwithnull"},
		{"\x01\x02\x03", ""},
		{"safe\x00\x01\x02\x03content", "safecontent"},
		{"", ""},
		{"no control chars", "no control chars"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := SanitizeString(tt.input)
			if result != tt.expected {
				t.Errorf("SanitizeString(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// TestEdgeCases tests various edge cases
func TestEdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		req     CreateTunnelRequest
		wantErr bool
	}{
		{
			name: "IPv6 localhost",
			req: CreateTunnelRequest{
				Name:       "ipv6-test",
				Type:       "local",
				Hops:       []HopReq{{Host: "::1", Port: 22, User: "root", AuthMethod: "key"}},
				RemoteHost: "target.example.com",
				RemotePort: 80,
			},
			wantErr: false,
		},
		{
			name: "Valid IPv4",
			req: CreateTunnelRequest{
				Name:       "ipv4-test",
				Type:       "local",
				Hops:       []HopReq{{Host: "192.168.1.1", Port: 22, User: "admin", AuthMethod: "key"}},
				RemoteHost: "10.0.0.5",
				RemotePort: 443,
			},
			wantErr: false,
		},
		{
			name: "Valid ports at boundaries",
			req: CreateTunnelRequest{
				Name:       "boundary-ports",
				Type:       "local",
				Hops:       []HopReq{{Host: "host.com", Port: 1, User: "user", AuthMethod: "key"}},
				LocalPort:  65535,
				RemoteHost: "target.com",
				RemotePort: 65535,
			},
			wantErr: false,
		},
		{
			name: "Zero local port (ephemeral)",
			req: CreateTunnelRequest{
				Name:       "ephemeral",
				Type:       "local",
				Hops:       []HopReq{{Host: "host.com", Port: 22, User: "user", AuthMethod: "key"}},
				LocalPort:  0,
				RemoteHost: "target.com",
				RemotePort: 80,
			},
			wantErr: false,
		},
		{
			name: "Subdomain hostname",
			req: CreateTunnelRequest{
				Name:       "subdomain-test",
				Type:       "local",
				Hops:       []HopReq{{Host: "deep.subdomain.example.com", Port: 22, User: "user", AuthMethod: "key"}},
				RemoteHost: "api.service.internal.company.io",
				RemotePort: 8080,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors := ValidateRequest(tt.req)
			if tt.wantErr && len(errors) == 0 {
				t.Errorf("Expected validation errors but got none")
			}
			if !tt.wantErr && len(errors) > 0 {
				t.Errorf("Expected no validation errors but got: %v", errors)
			}
		})
	}
}
