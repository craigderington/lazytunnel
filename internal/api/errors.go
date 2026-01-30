package api

import (
	"encoding/json"
	"net/http"
	"time"
)

// ErrorCode represents a standardized error code
type ErrorCode string

const (
	// General errors
	ErrCodeInternal           ErrorCode = "INTERNAL_ERROR"
	ErrCodeNotFound           ErrorCode = "NOT_FOUND"
	ErrCodeUnauthorized       ErrorCode = "UNAUTHORIZED"
	ErrCodeForbidden          ErrorCode = "FORBIDDEN"
	ErrCodeBadRequest         ErrorCode = "BAD_REQUEST"
	ErrCodeValidation         ErrorCode = "VALIDATION_ERROR"
	ErrCodeRateLimit          ErrorCode = "RATE_LIMIT_EXCEEDED"
	ErrCodeConflict           ErrorCode = "CONFLICT"
	ErrCodeServiceUnavailable ErrorCode = "SERVICE_UNAVAILABLE"
	ErrCodeTimeout            ErrorCode = "TIMEOUT"

	// Tunnel-specific errors
	ErrCodeTunnelNotFound    ErrorCode = "TUNNEL_NOT_FOUND"
	ErrCodeTunnelExists      ErrorCode = "TUNNEL_EXISTS"
	ErrCodeTunnelConnection  ErrorCode = "TUNNEL_CONNECTION_FAILED"
	ErrCodeTunnelAuth        ErrorCode = "TUNNEL_AUTH_FAILED"
	ErrCodeTunnelInvalidSpec ErrorCode = "TUNNEL_INVALID_SPEC"
	ErrCodeCircuitOpen       ErrorCode = "CIRCUIT_BREAKER_OPEN"
	ErrCodeHostKeyVerify     ErrorCode = "HOST_KEY_VERIFICATION_FAILED"

	// Auth errors
	ErrCodeInvalidCredentials ErrorCode = "INVALID_CREDENTIALS"
	ErrCodeTokenExpired       ErrorCode = "TOKEN_EXPIRED"
	ErrCodeTokenInvalid       ErrorCode = "TOKEN_INVALID"
	ErrCodeMissingAuth        ErrorCode = "MISSING_AUTHORIZATION"
)

// ErrorDetail represents additional error details
type ErrorDetail struct {
	Field string      `json:"field,omitempty"`
	Value interface{} `json:"value,omitempty"`
	Issue string      `json:"issue,omitempty"`
}

// APIError represents a standardized API error response
type APIError struct {
	Code      ErrorCode     `json:"code"`
	Message   string        `json:"message"`
	Details   []ErrorDetail `json:"details,omitempty"`
	RequestID string        `json:"request_id,omitempty"`
	Timestamp time.Time     `json:"timestamp"`
}

// NewAPIError creates a new API error
func NewAPIError(code ErrorCode, message string) *APIError {
	return &APIError{
		Code:      code,
		Message:   message,
		Timestamp: time.Now().UTC(),
	}
}

// WithDetails adds error details
func (e *APIError) WithDetails(details ...ErrorDetail) *APIError {
	e.Details = details
	return e
}

// WithRequestID adds a request ID
func (e *APIError) WithRequestID(id string) *APIError {
	e.RequestID = id
	return e
}

// Error implements the error interface
func (e *APIError) Error() string {
	return e.Message
}

// ErrorResponse sends a standardized error response
func (s *Server) ErrorResponse(w http.ResponseWriter, status int, err *APIError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if encodeErr := json.NewEncoder(w).Encode(err); encodeErr != nil {
		s.logger.Error().Err(encodeErr).Msg("Failed to encode error response")
	}
}

// Common error response helpers

// InternalError responds with a 500 internal server error
func (s *Server) InternalError(w http.ResponseWriter, message string) {
	err := NewAPIError(ErrCodeInternal, message)
	s.ErrorResponse(w, http.StatusInternalServerError, err)
}

// NotFound responds with a 404 not found error
func (s *Server) NotFound(w http.ResponseWriter, resource string) {
	err := NewAPIError(ErrCodeNotFound, resource+" not found")
	s.ErrorResponse(w, http.StatusNotFound, err)
}

// Unauthorized responds with a 401 unauthorized error
func (s *Server) Unauthorized(w http.ResponseWriter, message string) {
	if message == "" {
		message = "Authentication required"
	}
	err := NewAPIError(ErrCodeUnauthorized, message)
	s.ErrorResponse(w, http.StatusUnauthorized, err)
}

// Forbidden responds with a 403 forbidden error
func (s *Server) Forbidden(w http.ResponseWriter, message string) {
	if message == "" {
		message = "Access denied"
	}
	err := NewAPIError(ErrCodeForbidden, message)
	s.ErrorResponse(w, http.StatusForbidden, err)
}

// BadRequest responds with a 400 bad request error
func (s *Server) BadRequest(w http.ResponseWriter, message string) {
	err := NewAPIError(ErrCodeBadRequest, message)
	s.ErrorResponse(w, http.StatusBadRequest, err)
}

// ValidationError responds with a 400 validation error
func (s *Server) ValidationError(w http.ResponseWriter, message string, details []ValidationError) {
	err := NewAPIError(ErrCodeValidation, message)

	// Convert ValidationError to ErrorDetail
	if len(details) > 0 {
		errDetails := make([]ErrorDetail, len(details))
		for i, d := range details {
			errDetails[i] = ErrorDetail{
				Field: d.Field,
				Issue: d.Message,
			}
		}
		err.WithDetails(errDetails...)
	}

	s.ErrorResponse(w, http.StatusBadRequest, err)
}

// RateLimitError responds with a 429 rate limit error
func (s *Server) RateLimitError(w http.ResponseWriter, retryAfter int) {
	err := NewAPIError(ErrCodeRateLimit, "Rate limit exceeded. Please try again later.")
	w.Header().Set("Retry-After", string(rune(retryAfter)))
	s.ErrorResponse(w, http.StatusTooManyRequests, err)
}

// ConflictError responds with a 409 conflict error
func (s *Server) ConflictError(w http.ResponseWriter, message string) {
	err := NewAPIError(ErrCodeConflict, message)
	s.ErrorResponse(w, http.StatusConflict, err)
}

// ServiceUnavailableError responds with a 503 service unavailable error
func (s *Server) ServiceUnavailableError(w http.ResponseWriter, message string) {
	if message == "" {
		message = "Service temporarily unavailable"
	}
	err := NewAPIError(ErrCodeServiceUnavailable, message)
	s.ErrorResponse(w, http.StatusServiceUnavailable, err)
}

// TimeoutError responds with a 504 gateway timeout error
func (s *Server) TimeoutError(w http.ResponseWriter, message string) {
	if message == "" {
		message = "Request timeout"
	}
	err := NewAPIError(ErrCodeTimeout, message)
	s.ErrorResponse(w, http.StatusGatewayTimeout, err)
}

// Tunnel-specific error helpers

// TunnelNotFound responds with a tunnel not found error
func (s *Server) TunnelNotFound(w http.ResponseWriter, tunnelID string) {
	err := NewAPIError(ErrCodeTunnelNotFound, "Tunnel not found").
		WithDetails(ErrorDetail{Field: "id", Value: tunnelID})
	s.ErrorResponse(w, http.StatusNotFound, err)
}

// TunnelExists responds with a tunnel already exists error
func (s *Server) TunnelExists(w http.ResponseWriter, name string) {
	err := NewAPIError(ErrCodeTunnelExists, "Tunnel with this name already exists").
		WithDetails(ErrorDetail{Field: "name", Value: name})
	s.ErrorResponse(w, http.StatusConflict, err)
}

// TunnelConnectionError responds with a tunnel connection error
func (s *Server) TunnelConnectionError(w http.ResponseWriter, tunnelID string, reason string) {
	err := NewAPIError(ErrCodeTunnelConnection, "Failed to establish tunnel connection").
		WithDetails(
			ErrorDetail{Field: "tunnel_id", Value: tunnelID},
			ErrorDetail{Field: "reason", Value: reason},
		)
	s.ErrorResponse(w, http.StatusBadGateway, err)
}

// TunnelAuthError responds with a tunnel authentication error
func (s *Server) TunnelAuthError(w http.ResponseWriter, tunnelID string, reason string) {
	err := NewAPIError(ErrCodeTunnelAuth, "Tunnel authentication failed").
		WithDetails(
			ErrorDetail{Field: "tunnel_id", Value: tunnelID},
			ErrorDetail{Field: "reason", Value: reason},
		)
	s.ErrorResponse(w, http.StatusUnauthorized, err)
}

// CircuitBreakerOpenError responds with a circuit breaker open error
func (s *Server) CircuitBreakerOpenError(w http.ResponseWriter, tunnelID string) {
	err := NewAPIError(ErrCodeCircuitOpen, "Circuit breaker is open - too many connection failures").
		WithDetails(ErrorDetail{Field: "tunnel_id", Value: tunnelID})
	s.ErrorResponse(w, http.StatusServiceUnavailable, err)
}

// HostKeyVerificationError responds with a host key verification error
func (s *Server) HostKeyVerificationError(w http.ResponseWriter, host string, reason string) {
	err := NewAPIError(ErrCodeHostKeyVerify, "Host key verification failed").
		WithDetails(
			ErrorDetail{Field: "host", Value: host},
			ErrorDetail{Field: "reason", Value: reason},
		)
	s.ErrorResponse(w, http.StatusForbidden, err)
}

// Auth-specific error helpers

// InvalidCredentialsError responds with an invalid credentials error
func (s *Server) InvalidCredentialsError(w http.ResponseWriter) {
	err := NewAPIError(ErrCodeInvalidCredentials, "Invalid username or password")
	s.ErrorResponse(w, http.StatusUnauthorized, err)
}

// TokenExpiredError responds with a token expired error
func (s *Server) TokenExpiredError(w http.ResponseWriter) {
	err := NewAPIError(ErrCodeTokenExpired, "Authentication token has expired")
	s.ErrorResponse(w, http.StatusUnauthorized, err)
}

// TokenInvalidError responds with a token invalid error
func (s *Server) TokenInvalidError(w http.ResponseWriter, reason string) {
	err := NewAPIError(ErrCodeTokenInvalid, "Invalid authentication token")
	if reason != "" {
		err = err.WithDetails(ErrorDetail{Field: "reason", Value: reason})
	}
	s.ErrorResponse(w, http.StatusUnauthorized, err)
}

// MissingAuthError responds with a missing authorization error
func (s *Server) MissingAuthError(w http.ResponseWriter) {
	err := NewAPIError(ErrCodeMissingAuth, "Authorization header required")
	s.ErrorResponse(w, http.StatusUnauthorized, err)
}
