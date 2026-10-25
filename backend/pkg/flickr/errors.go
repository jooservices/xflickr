package flickr

import (
	"errors"
	"fmt"
)

var (
	ErrInvalidKey         = errors.New("flickr: invalid API key")
	ErrServiceUnavailable = errors.New("flickr: service unavailable")
	ErrQuotaExceeded      = errors.New("flickr: quota exceeded")
	ErrRateLimited        = errors.New("flickr: rate limited")
	ErrIPBlocked          = errors.New("flickr: IP blocked")
	ErrNotFound           = errors.New("flickr: resource not found")
	ErrPermission         = errors.New("flickr: permission denied")
	// ErrResourceDenied marks a permission failure scoped to one resource
	// (a private or restricted subject), so the credential is still good.
	ErrResourceDenied    = errors.New("flickr: resource access denied")
	ErrTokenRevoked      = errors.New("flickr: authentication token invalid or revoked")
	ErrTransient         = errors.New("flickr: transient failure")
	ErrMalformedResponse = errors.New("flickr: malformed JSON response")
	ErrHTTPStatus        = errors.New("flickr: unexpected HTTP status")
)

// CallError describes a failed Flickr call. Its unwrap targets allow callers
// to classify errors with errors.Is while retaining Flickr and transport data.
type CallError struct {
	Kind    error
	Code    int
	Status  int
	Message string
	Cause   error
}

func (e *CallError) Error() string {
	if e == nil {
		return "flickr: call failed"
	}

	message := e.Message
	if message == "" && e.Cause != nil {
		message = e.Cause.Error()
	}
	if message == "" && e.Kind != nil {
		message = e.Kind.Error()
	}
	if message == "" {
		message = "request failed"
	}
	if e.Code != 0 {
		return fmt.Sprintf("flickr: %s (code %d)", message, e.Code)
	}
	if e.Status != 0 {
		return fmt.Sprintf("flickr: %s (HTTP %d)", message, e.Status)
	}
	return "flickr: " + message
}

// Unwrap preserves both the classification and the underlying cause.
func (e *CallError) Unwrap() []error {
	if e == nil {
		return nil
	}
	causes := make([]error, 0, 2)
	if e.Kind != nil {
		causes = append(causes, e.Kind)
	}
	if e.Cause != nil {
		causes = append(causes, e.Cause)
	}
	return causes
}
