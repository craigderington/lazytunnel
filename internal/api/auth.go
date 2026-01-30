package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Context keys for storing auth data in request context
type contextKey string

const (
	userContextKey   contextKey = "user"
	claimsContextKey contextKey = "claims"
)

// User represents the authenticated user
type User struct {
	ID       string   `json:"id"`
	Username string   `json:"username"`
	Email    string   `json:"email"`
	Roles    []string `json:"roles"`
}

// JWTClaims represents the JWT token claims
type JWTClaims struct {
	UserID   string   `json:"user_id"`
	Username string   `json:"username"`
	Email    string   `json:"email"`
	Roles    []string `json:"roles"`
	jwt.RegisteredClaims
}

// AuthMiddleware handles JWT authentication
type AuthMiddleware struct {
	secret          []byte
	tokenExpiration time.Duration
}

// NewAuthMiddleware creates a new authentication middleware
func NewAuthMiddleware(secret string, tokenExpiration time.Duration) *AuthMiddleware {
	if tokenExpiration == 0 {
		tokenExpiration = 24 * time.Hour
	}
	return &AuthMiddleware{
		secret:          []byte(secret),
		tokenExpiration: tokenExpiration,
	}
}

// Middleware is the HTTP middleware function
func (am *AuthMiddleware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract token from Authorization header
		tokenString := am.extractToken(r)
		if tokenString == "" {
			am.respondError(w, http.StatusUnauthorized, "Missing authorization token")
			return
		}

		// Parse and validate token
		token, err := jwt.ParseWithClaims(tokenString, &JWTClaims{}, func(token *jwt.Token) (interface{}, error) {
			// Verify signing method
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return am.secret, nil
		})

		if err != nil {
			am.respondError(w, http.StatusUnauthorized, "Invalid or expired token")
			return
		}

		// Extract claims
		if claims, ok := token.Claims.(*JWTClaims); ok && token.Valid {
			user := &User{
				ID:       claims.UserID,
				Username: claims.Username,
				Email:    claims.Email,
				Roles:    claims.Roles,
			}

			// Add user and claims to context
			ctx := context.WithValue(r.Context(), userContextKey, user)
			ctx = context.WithValue(ctx, claimsContextKey, claims)

			next.ServeHTTP(w, r.WithContext(ctx))
		} else {
			am.respondError(w, http.StatusUnauthorized, "Invalid token claims")
			return
		}
	})
}

// extractToken extracts the JWT token from the Authorization header
func (am *AuthMiddleware) extractToken(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return ""
	}

	// Bearer token format
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		return ""
	}

	return parts[1]
}

// GenerateToken generates a new JWT token for a user
func (am *AuthMiddleware) GenerateToken(userID, username, email string, roles []string) (string, error) {
	claims := JWTClaims{
		UserID:   userID,
		Username: username,
		Email:    email,
		Roles:    roles,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(am.tokenExpiration)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(am.secret)
}

// respondError sends a JSON error response
func (am *AuthMiddleware) respondError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{
		"error": message,
	})
}

// GetUser retrieves the authenticated user from the context
func GetUser(ctx context.Context) (*User, bool) {
	user, ok := ctx.Value(userContextKey).(*User)
	return user, ok
}

// GetClaims retrieves the JWT claims from the context
func GetClaims(ctx context.Context) (*JWTClaims, bool) {
	claims, ok := ctx.Value(claimsContextKey).(*JWTClaims)
	return claims, ok
}

// IsAuthenticated checks if the request has been authenticated
func IsAuthenticated(ctx context.Context) bool {
	_, ok := GetUser(ctx)
	return ok
}
