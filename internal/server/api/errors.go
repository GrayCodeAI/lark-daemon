package api

// APIError is a structured error response with a machine-readable code.
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Standard error codes.
const (
	ErrCodeAuthRequired       = "AUTH_REQUIRED"
	ErrCodeAuthInvalidToken   = "AUTH_INVALID_TOKEN"
	ErrCodeAuthInvalidCreds   = "AUTH_INVALID_CREDENTIALS"
	ErrCodeAuthEmailExists    = "AUTH_EMAIL_EXISTS"
	ErrCodeForbidden          = "FORBIDDEN"
	ErrCodeNotFound           = "NOT_FOUND"
	ErrCodeBadRequest         = "BAD_REQUEST"
	ErrCodeRateLimited        = "RATE_LIMITED"
	ErrCodeInternal           = "INTERNAL_ERROR"
	ErrCodeConflict           = "CONFLICT"
	ErrCodeTooLarge           = "PAYLOAD_TOO_LARGE"
)
