package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-playground/validator/v10"
)

// Validator instance for request validation
var validate *validator.Validate

func init() {
	validate = validator.New()

	// Register custom validation functions
	validate.RegisterValidation("tunneltype", validateTunnelType)
	validate.RegisterValidation("authmethod", validateAuthMethod)
}

// validateTunnelType validates tunnel type values
func validateTunnelType(fl validator.FieldLevel) bool {
	value := fl.Field().String()
	validTypes := []string{"local", "remote", "dynamic"}
	for _, t := range validTypes {
		if value == t {
			return true
		}
	}
	return false
}

// validateAuthMethod validates authentication method values
func validateAuthMethod(fl validator.FieldLevel) bool {
	value := fl.Field().String()
	validMethods := []string{"key", "password", "agent", "cert"}
	for _, m := range validMethods {
		if value == m {
			return true
		}
	}
	return false
}

// CreateTunnelRequest represents the validated request for creating a tunnel
type CreateTunnelRequest struct {
	Name             string   `json:"name" validate:"required,min=1,max=100"`
	Type             string   `json:"type" validate:"required,tunneltype"`
	Hops             []HopReq `json:"hops" validate:"required,min=1,dive"`
	LocalPort        int      `json:"localPort" validate:"min=0,max=65535"`
	LocalBindAddress string   `json:"localBindAddress" validate:"omitempty,ip_addr|hostname"`
	RemoteHost       string   `json:"remoteHost" validate:"required,hostname|ip_addr"`
	RemotePort       int      `json:"remotePort" validate:"required,min=1,max=65535"`
	AutoReconnect    bool     `json:"autoReconnect"`
	KeepAlive        int      `json:"keepAlive" validate:"min=0,max=300"`
	MaxRetries       int      `json:"maxRetries" validate:"min=0,max=100"`
}

// HopReq represents a single hop in a validated tunnel request
type HopReq struct {
	Host       string `json:"host" validate:"required,hostname|ip_addr"`
	Port       int    `json:"port" validate:"min=1,max=65535"`
	User       string `json:"user" validate:"required,min=1,max=100"`
	AuthMethod string `json:"auth_method" validate:"required,authmethod"`
	KeyID      string `json:"key_id,omitempty"`
}

// ValidationError represents a validation error response
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// ValidateRequest validates a struct and returns validation errors
func ValidateRequest(req interface{}) []ValidationError {
	if err := validate.Struct(req); err != nil {
		if validationErrors, ok := err.(validator.ValidationErrors); ok {
			var errors []ValidationError
			for _, e := range validationErrors {
				errors = append(errors, ValidationError{
					Field:   e.Field(),
					Message: formatValidationError(e),
				})
			}
			return errors
		}
	}
	return nil
}

// formatValidationError creates a human-readable error message from a validation error
func formatValidationError(e validator.FieldError) string {
	field := e.Field()
	tag := e.Tag()
	param := e.Param()

	switch tag {
	case "required":
		return fmt.Sprintf("%s is required", field)
	case "min":
		if param == "1" {
			return fmt.Sprintf("%s cannot be empty", field)
		}
		return fmt.Sprintf("%s must be at least %s", field, param)
	case "max":
		return fmt.Sprintf("%s must be at most %s", field, param)
	case "hostname":
		return fmt.Sprintf("%s must be a valid hostname or IP address", field)
	case "ip_addr":
		return fmt.Sprintf("%s must be a valid IP address", field)
	case "hostname|ip_addr":
		return fmt.Sprintf("%s must be a valid hostname or IP address", field)
	case "tunneltype":
		return fmt.Sprintf("%s must be one of: local, remote, dynamic", field)
	case "authmethod":
		return fmt.Sprintf("%s must be one of: key, password, agent, cert", field)
	default:
		return fmt.Sprintf("%s failed validation: %s", field, tag)
	}
}

// respondValidationErrors sends a validation error response using Server method
func (s *Server) respondValidationErrors(w http.ResponseWriter, errors []ValidationError) {
	s.ValidationError(w, "Validation failed", errors)
}

// decodeAndValidate decodes a JSON request body and validates it
func (s *Server) decodeAndValidate(w http.ResponseWriter, r *http.Request, req interface{}) bool {
	// Decode request
	if err := json.NewDecoder(r.Body).Decode(req); err != nil {
		s.BadRequest(w, "Invalid request body: "+err.Error())
		return false
	}

	// Validate request
	if errors := ValidateRequest(req); len(errors) > 0 {
		s.respondValidationErrors(w, errors)
		return false
	}

	return true
}

// SanitizeString removes potentially dangerous characters from a string
func SanitizeString(s string) string {
	// Remove null bytes and control characters
	s = strings.ReplaceAll(s, "\x00", "")
	s = strings.ReplaceAll(s, "\x01", "")
	s = strings.ReplaceAll(s, "\x02", "")
	s = strings.ReplaceAll(s, "\x03", "")
	return s
}
