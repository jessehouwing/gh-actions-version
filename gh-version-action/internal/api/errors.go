package api

import (
	"errors"
	"fmt"
	"strings"

	ghapi "github.com/cli/go-gh/v2/pkg/api"
)

// StatusCode returns the HTTP status carried by err, or 0.
func StatusCode(err error) int {
	var he *ghapi.HTTPError
	if errors.As(err, &he) {
		return he.StatusCode
	}
	return 0
}

// IsNotFound reports an HTTP 404.
func IsNotFound(err error) bool { return StatusCode(err) == 404 }

// IsRateLimit reports whether err is (or wraps) a *RateLimitError.
func IsRateLimit(err error) bool { return errors.Is(err, ErrRateLimited) }

// ErrorDetail returns the API message plus structured error messages of err.
func ErrorDetail(err error) string {
	if err == nil {
		return ""
	}
	var he *ghapi.HTTPError
	if errors.As(err, &he) {
		parts := []string{he.Message}
		for _, it := range he.Errors {
			if it.Message != "" {
				parts = append(parts, it.Message)
			}
		}
		return strings.TrimSpace(strings.Join(parts, "; "))
	}
	return err.Error()
}

// IsImmutableReleaseError reports a 422 meaning a tag was used by an (already deleted)
// immutable release (Test-ImmutableReleaseError).
func IsImmutableReleaseError(err error) bool {
	if err == nil {
		return false
	}
	var he *ghapi.HTTPError
	if errors.As(err, &he) {
		if he.StatusCode != 422 {
			return false
		}
		if len(he.Errors) > 0 {
			for _, it := range he.Errors {
				if it.Field == "tag_name" && strings.Contains(it.Message, "was used by an immutable release") {
					return true
				}
			}
			return false
		}
		return strings.Contains(he.Message, "tag_name was used by an immutable release")
	}
	msg := err.Error()
	return strings.Contains(msg, "422") && strings.Contains(msg, "tag_name was used by an immutable release")
}

// APIError wraps a failure with the operation that caused it.
type APIError struct {
	Op  string
	Err error
}

func (e *APIError) Error() string {
	if s := StatusCode(e.Err); s != 0 {
		return fmt.Sprintf("GitHub API request failed during %s (HTTP %d): %s", e.Op, s, ErrorDetail(e.Err))
	}
	return fmt.Sprintf("GitHub API request failed during %s: %v", e.Op, e.Err)
}

func (e *APIError) Unwrap() error { return e.Err }

func wrap(op string, err error) error {
	if err == nil {
		return nil
	}
	return &APIError{Op: op, Err: err}
}
