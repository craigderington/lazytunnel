package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// TestNewAuthMiddleware tests the creation of auth middleware
func TestNewAuthMiddleware(t *testing.T) {
	tests := []struct {
		name               string
		secret             string
		tokenExpiration    time.Duration
		expectedExpiration time.Duration
	}{
		{
			name:               "Default expiration",
			secret:             "test-secret",
			tokenExpiration:    0,
			expectedExpiration: 24 * time.Hour,
		},
		{
			name:               "Custom expiration",
			secret:             "test-secret",
			tokenExpiration:    1 * time.Hour,
			expectedExpiration: 1 * time.Hour,
		},
		{
			name:               "Short expiration",
			secret:             "test-secret",
			tokenExpiration:    15 * time.Minute,
			expectedExpiration: 15 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			am := NewAuthMiddleware(tt.secret, tt.tokenExpiration)
			if am == nil {
				t.Fatal("NewAuthMiddleware() returned nil")
			}
			if string(am.secret) != tt.secret {
				t.Errorf("secret = %v, want %v", string(am.secret), tt.secret)
			}
			if am.tokenExpiration != tt.expectedExpiration {
				t.Errorf("tokenExpiration = %v, want %v", am.tokenExpiration, tt.expectedExpiration)
			}
		})
	}
}

// TestGenerateToken tests JWT token generation
func TestGenerateToken(t *testing.T) {
	am := NewAuthMiddleware("test-secret-key", 1*time.Hour)

	tests := []struct {
		name     string
		userID   string
		username string
		email    string
		roles    []string
		wantErr  bool
	}{
		{
			name:     "Valid token with all fields",
			userID:   "user-123",
			username: "john.doe",
			email:    "john@example.com",
			roles:    []string{"admin", "user"},
			wantErr:  false,
		},
		{
			name:     "Valid token with single role",
			userID:   "user-456",
			username: "jane.doe",
			email:    "jane@example.com",
			roles:    []string{"user"},
			wantErr:  false,
		},
		{
			name:     "Valid token with empty roles",
			userID:   "user-789",
			username: "guest",
			email:    "guest@example.com",
			roles:    []string{},
			wantErr:  false,
		},
		{
			name:     "Valid token with nil roles",
			userID:   "user-000",
			username: "anonymous",
			email:    "anon@example.com",
			roles:    nil,
			wantErr:  false,
		},
		{
			name:     "Valid token with special characters",
			userID:   "user-special-123",
			username: "user@domain.com",
			email:    "user+tag@example.com",
			roles:    []string{"role-with-dashes", "role_with_underscores"},
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, err := am.GenerateToken(tt.userID, tt.username, tt.email, tt.roles)
			if (err != nil) != tt.wantErr {
				t.Errorf("GenerateToken() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err == nil {
				if token == "" {
					t.Error("GenerateToken() returned empty token")
				}
				// Verify token can be parsed
				parsedToken, parseErr := jwt.ParseWithClaims(token, &JWTClaims{}, func(token *jwt.Token) (interface{}, error) {
					return am.secret, nil
				})
				if parseErr != nil {
					t.Errorf("Failed to parse generated token: %v", parseErr)
					return
				}
				if claims, ok := parsedToken.Claims.(*JWTClaims); ok && parsedToken.Valid {
					if claims.UserID != tt.userID {
						t.Errorf("UserID = %v, want %v", claims.UserID, tt.userID)
					}
					if claims.Username != tt.username {
						t.Errorf("Username = %v, want %v", claims.Username, tt.username)
					}
					if claims.Email != tt.email {
						t.Errorf("Email = %v, want %v", claims.Email, tt.email)
					}
					if len(claims.Roles) != len(tt.roles) {
						t.Errorf("Roles length = %v, want %v", len(claims.Roles), len(tt.roles))
					}
					if claims.ExpiresAt == nil {
						t.Error("ExpiresAt is nil")
					}
					if claims.IssuedAt == nil {
						t.Error("IssuedAt is nil")
					}
					if claims.NotBefore == nil {
						t.Error("NotBefore is nil")
					}
				} else {
					t.Error("Failed to extract claims from valid token")
				}
			}
		})
	}
}

// TestTokenExpiration tests that tokens expire correctly
func TestTokenExpiration(t *testing.T) {
	tests := []struct {
		name          string
		expiration    time.Duration
		waitTime      time.Duration
		shouldBeValid bool
	}{
		{
			name:          "Token still valid",
			expiration:    1 * time.Hour,
			waitTime:      0,
			shouldBeValid: true,
		},
		{
			name:          "Token expired after short duration",
			expiration:    1 * time.Millisecond,
			waitTime:      5 * time.Millisecond,
			shouldBeValid: false,
		},
		{
			name:          "Token valid within 5 second window",
			expiration:    10 * time.Second,
			waitTime:      0,
			shouldBeValid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			am := NewAuthMiddleware("test-secret", tt.expiration)
			token, err := am.GenerateToken("user-123", "testuser", "test@test.com", []string{"user"})
			if err != nil {
				t.Fatalf("Failed to generate token: %v", err)
			}

			if tt.waitTime > 0 {
				time.Sleep(tt.waitTime)
			}

			parsedToken, parseErr := jwt.ParseWithClaims(token, &JWTClaims{}, func(token *jwt.Token) (interface{}, error) {
				return am.secret, nil
			})

			isValid := parseErr == nil && parsedToken.Valid
			if isValid != tt.shouldBeValid {
				t.Errorf("Token validity = %v, want %v. Error: %v", isValid, tt.shouldBeValid, parseErr)
			}
		})
	}
}

// TestExtractToken tests the extractToken function
func TestExtractToken(t *testing.T) {
	am := NewAuthMiddleware("test-secret", 1*time.Hour)

	tests := []struct {
		name       string
		authHeader string
		expected   string
	}{
		{
			name:       "Valid Bearer token",
			authHeader: "Bearer validtoken123",
			expected:   "validtoken123",
		},
		{
			name:       "Valid bearer token (lowercase)",
			authHeader: "bearer validtoken456",
			expected:   "validtoken456",
		},
		{
			name:       "Missing Authorization header",
			authHeader: "",
			expected:   "",
		},
		{
			name:       "Invalid format - no space",
			authHeader: "Bearertoken123",
			expected:   "",
		},
		{
			name:       "Invalid format - wrong prefix",
			authHeader: "Basic dXNlcjpwYXNz",
			expected:   "",
		},
		{
			name:       "Invalid format - empty token",
			authHeader: "Bearer ",
			expected:   "",
		},
		{
			name:       "Token with spaces",
			authHeader: "Bearer token with spaces",
			expected:   "token with spaces",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			result := am.extractToken(req)
			if result != tt.expected {
				t.Errorf("extractToken() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// TestMiddlewareValidToken tests the middleware with valid tokens
func TestMiddlewareValidToken(t *testing.T) {
	am := NewAuthMiddleware("test-secret-key", 1*time.Hour)

	tests := []struct {
		name       string
		setupReq   func(*http.Request)
		wantNext   bool
		wantStatus int
	}{
		{
			name: "Valid token",
			setupReq: func(req *http.Request) {
				token, _ := am.GenerateToken("user-123", "testuser", "test@test.com", []string{"admin"})
				req.Header.Set("Authorization", "Bearer "+token)
			},
			wantNext:   true,
			wantStatus: http.StatusOK,
		},
		{
			name: "Missing Authorization header",
			setupReq: func(req *http.Request) {
				// Don't set any header
			},
			wantNext:   false,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "Empty Authorization header",
			setupReq: func(req *http.Request) {
				req.Header.Set("Authorization", "")
			},
			wantNext:   false,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "Invalid token format",
			setupReq: func(req *http.Request) {
				req.Header.Set("Authorization", "Basic invalid")
			},
			wantNext:   false,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "Invalid token",
			setupReq: func(req *http.Request) {
				req.Header.Set("Authorization", "Bearer invalid.token.here")
			},
			wantNext:   false,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "Tampered token",
			setupReq: func(req *http.Request) {
				token, _ := am.GenerateToken("user-123", "testuser", "test@test.com", []string{"admin"})
				// Tamper with the token
				tampered := token[:len(token)-5] + "XXXXX"
				req.Header.Set("Authorization", "Bearer "+tampered)
			},
			wantNext:   false,
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			tt.setupReq(req)
			w := httptest.NewRecorder()

			nextCalled := false
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				nextCalled = true
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("OK"))
			})

			am.Middleware(next).ServeHTTP(w, req)

			if nextCalled != tt.wantNext {
				t.Errorf("next handler called = %v, want %v", nextCalled, tt.wantNext)
			}

			if w.Code != tt.wantStatus {
				t.Errorf("Status code = %d, want %d", w.Code, tt.wantStatus)
			}

			contentType := w.Header().Get("Content-Type")
			if !tt.wantNext && contentType != "application/json" {
				t.Errorf("Content-Type = %s, want application/json", contentType)
			}
		})
	}
}

// TestMiddlewareExpiredToken tests the middleware with expired tokens
func TestMiddlewareExpiredToken(t *testing.T) {
	am := NewAuthMiddleware("test-secret", 1*time.Millisecond)

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	token, _ := am.GenerateToken("user-123", "testuser", "test@test.com", []string{"user"})
	req.Header.Set("Authorization", "Bearer "+token)

	// Wait for token to expire
	time.Sleep(5 * time.Millisecond)

	w := httptest.NewRecorder()
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
	})

	am.Middleware(next).ServeHTTP(w, req)

	if nextCalled {
		t.Error("Next handler should not be called for expired token")
	}

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Status code = %d, want %d", w.Code, http.StatusUnauthorized)
	}

	body := w.Body.String()
	if !strings.Contains(body, "Invalid or expired token") {
		t.Errorf("Response body should contain 'Invalid or expired token', got: %s", body)
	}
}

// TestMiddlewareContextValues tests that middleware sets context values correctly
func TestMiddlewareContextValues(t *testing.T) {
	am := NewAuthMiddleware("test-secret-key", 1*time.Hour)
	token, _ := am.GenerateToken("user-456", "contextuser", "context@test.com", []string{"admin", "editor"})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	var contextUser *User
	var contextClaims *JWTClaims

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contextUser, _ = GetUser(r.Context())
		contextClaims, _ = GetClaims(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	am.Middleware(next).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Status code = %d, want %d", w.Code, http.StatusOK)
	}

	if contextUser == nil {
		t.Fatal("Context user is nil")
	}

	if contextUser.ID != "user-456" {
		t.Errorf("Context user ID = %v, want user-456", contextUser.ID)
	}

	if contextUser.Username != "contextuser" {
		t.Errorf("Context user Username = %v, want contextuser", contextUser.Username)
	}

	if contextUser.Email != "context@test.com" {
		t.Errorf("Context user Email = %v, want context@test.com", contextUser.Email)
	}

	if len(contextUser.Roles) != 2 {
		t.Errorf("Context user Roles length = %v, want 2", len(contextUser.Roles))
	}

	if contextClaims == nil {
		t.Fatal("Context claims is nil")
	}

	if contextClaims.UserID != "user-456" {
		t.Errorf("Context claims UserID = %v, want user-456", contextClaims.UserID)
	}
}

// TestGetUser tests the GetUser helper function
func TestGetUser(t *testing.T) {
	tests := []struct {
		name     string
		setupCtx func() context.Context
		wantUser bool
		wantID   string
	}{
		{
			name: "Valid user in context",
			setupCtx: func() context.Context {
				user := &User{
					ID:       "user-789",
					Username: "test",
					Email:    "test@test.com",
					Roles:    []string{"user"},
				}
				return context.WithValue(context.Background(), userContextKey, user)
			},
			wantUser: true,
			wantID:   "user-789",
		},
		{
			name: "No user in context",
			setupCtx: func() context.Context {
				return context.Background()
			},
			wantUser: false,
		},
		{
			name: "Wrong type in context",
			setupCtx: func() context.Context {
				return context.WithValue(context.Background(), userContextKey, "not a user")
			},
			wantUser: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.setupCtx()
			user, ok := GetUser(ctx)

			if ok != tt.wantUser {
				t.Errorf("GetUser() ok = %v, want %v", ok, tt.wantUser)
			}

			if tt.wantUser {
				if user == nil {
					t.Fatal("GetUser() returned nil user when ok is true")
				}
				if user.ID != tt.wantID {
					t.Errorf("GetUser() user.ID = %v, want %v", user.ID, tt.wantID)
				}
			}
		})
	}
}

// TestGetClaims tests the GetClaims helper function
func TestGetClaims(t *testing.T) {
	tests := []struct {
		name       string
		setupCtx   func() context.Context
		wantClaims bool
		wantUserID string
	}{
		{
			name: "Valid claims in context",
			setupCtx: func() context.Context {
				claims := &JWTClaims{
					UserID:   "user-999",
					Username: "claimsuser",
					Email:    "claims@test.com",
					Roles:    []string{"admin"},
				}
				return context.WithValue(context.Background(), claimsContextKey, claims)
			},
			wantClaims: true,
			wantUserID: "user-999",
		},
		{
			name: "No claims in context",
			setupCtx: func() context.Context {
				return context.Background()
			},
			wantClaims: false,
		},
		{
			name: "Wrong type in context",
			setupCtx: func() context.Context {
				return context.WithValue(context.Background(), claimsContextKey, "not claims")
			},
			wantClaims: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.setupCtx()
			claims, ok := GetClaims(ctx)

			if ok != tt.wantClaims {
				t.Errorf("GetClaims() ok = %v, want %v", ok, tt.wantClaims)
			}

			if tt.wantClaims {
				if claims == nil {
					t.Fatal("GetClaims() returned nil claims when ok is true")
				}
				if claims.UserID != tt.wantUserID {
					t.Errorf("GetClaims() claims.UserID = %v, want %v", claims.UserID, tt.wantUserID)
				}
			}
		})
	}
}

// TestIsAuthenticated tests the IsAuthenticated helper function
func TestIsAuthenticated(t *testing.T) {
	tests := []struct {
		name     string
		setupCtx func() context.Context
		wantAuth bool
	}{
		{
			name: "Authenticated",
			setupCtx: func() context.Context {
				user := &User{ID: "user-1", Username: "test"}
				return context.WithValue(context.Background(), userContextKey, user)
			},
			wantAuth: true,
		},
		{
			name: "Not authenticated",
			setupCtx: func() context.Context {
				return context.Background()
			},
			wantAuth: false,
		},
		{
			name: "Wrong type in context",
			setupCtx: func() context.Context {
				return context.WithValue(context.Background(), userContextKey, "string")
			},
			wantAuth: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.setupCtx()
			authenticated := IsAuthenticated(ctx)

			if authenticated != tt.wantAuth {
				t.Errorf("IsAuthenticated() = %v, want %v", authenticated, tt.wantAuth)
			}
		})
	}
}

// TestDifferentSigningMethods tests tokens with different signing methods
func TestDifferentSigningMethods(t *testing.T) {
	am := NewAuthMiddleware("test-secret", 1*time.Hour)

	// Create a token with a different signing method (RS256 instead of HS256)
	invalidToken := "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiYWRtaW4iOnRydWV9.Signature"

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+invalidToken)
	w := httptest.NewRecorder()

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
	})

	am.Middleware(next).ServeHTTP(w, req)

	if nextCalled {
		t.Error("Next handler should not be called for token with different signing method")
	}

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Status code = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// TestWrongSecret tests tokens signed with a different secret
func TestWrongSecret(t *testing.T) {
	am1 := NewAuthMiddleware("correct-secret", 1*time.Hour)
	am2 := NewAuthMiddleware("wrong-secret", 1*time.Hour)

	// Generate token with am1
	token, _ := am1.GenerateToken("user-1", "user", "user@test.com", []string{"user"})

	// Try to validate with am2 (different secret)
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
	})

	am2.Middleware(next).ServeHTTP(w, req)

	if nextCalled {
		t.Error("Next handler should not be called for token with wrong secret")
	}

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Status code = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// TestTokenWithClaimsAndExpiry tests complex token scenarios
func TestTokenWithClaimsAndExpiry(t *testing.T) {
	am := NewAuthMiddleware("test-secret", 15*time.Minute)

	tests := []struct {
		name     string
		userID   string
		username string
		email    string
		roles    []string
	}{
		{
			name:     "Token with multiple roles",
			userID:   "user-multi",
			username: "multirole",
			email:    "multi@test.com",
			roles:    []string{"admin", "editor", "viewer", "user"},
		},
		{
			name:     "Token with empty email",
			userID:   "user-empty",
			username: "emptyemail",
			email:    "",
			roles:    []string{"user"},
		},
		{
			name:     "Token with long username",
			userID:   "user-long",
			username: "verylongusername123456789",
			email:    "long@test.com",
			roles:    []string{"user"},
		},
		{
			name:     "Token with special characters in email",
			userID:   "user-special",
			username: "special",
			email:    "user+tag.sub@sub.domain.example.com",
			roles:    []string{"user"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, err := am.GenerateToken(tt.userID, tt.username, tt.email, tt.roles)
			if err != nil {
				t.Fatalf("Failed to generate token: %v", err)
			}

			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()

			var contextUser *User
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				contextUser, _ = GetUser(r.Context())
				w.WriteHeader(http.StatusOK)
			})

			am.Middleware(next).ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("Status code = %d, want %d", w.Code, http.StatusOK)
			}

			if contextUser == nil {
				t.Fatal("Context user is nil")
			}

			if contextUser.ID != tt.userID {
				t.Errorf("UserID = %v, want %v", contextUser.ID, tt.userID)
			}

			if contextUser.Username != tt.username {
				t.Errorf("Username = %v, want %v", contextUser.Username, tt.username)
			}

			if contextUser.Email != tt.email {
				t.Errorf("Email = %v, want %v", contextUser.Email, tt.email)
			}

			if len(contextUser.Roles) != len(tt.roles) {
				t.Errorf("Roles length = %v, want %v", len(contextUser.Roles), len(tt.roles))
			}
		})
	}
}
