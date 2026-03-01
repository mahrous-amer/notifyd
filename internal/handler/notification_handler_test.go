package handler

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bse/notifyd/internal/domain"
)

// TestSanitizeNotificationError exercises every branch of the function,
// verifying that only a small set of sentinel errors produce specific messages
// while all other errors collapse to a generic safe string.

func TestSanitizeNotificationError_ErrNotFound_ReturnsChannelConfigNotFound(t *testing.T) {
	result := sanitizeNotificationError(domain.ErrNotFound)

	assert.Equal(t, "channel config not found", result)
}

func TestSanitizeNotificationError_WrappedErrNotFound_ReturnsChannelConfigNotFound(t *testing.T) {
	wrapped := errors.Join(errors.New("pg: no rows"), domain.ErrNotFound)

	result := sanitizeNotificationError(wrapped)

	assert.Equal(t, "channel config not found", result)
}

func TestSanitizeNotificationError_ErrValidationFailed_ReturnsValidationFailed(t *testing.T) {
	result := sanitizeNotificationError(domain.ErrValidationFailed)

	assert.Equal(t, "validation failed", result)
}

func TestSanitizeNotificationError_WrappedErrValidationFailed_ReturnsValidationFailed(t *testing.T) {
	wrapped := errors.Join(errors.New("body cannot be empty"), domain.ErrValidationFailed)

	result := sanitizeNotificationError(wrapped)

	assert.Equal(t, "validation failed", result)
}

func TestSanitizeNotificationError_ErrUnsupportedChannel_ReturnsUnsupportedChannel(t *testing.T) {
	result := sanitizeNotificationError(domain.ErrUnsupportedChannel)

	assert.Equal(t, "unsupported channel", result)
}

func TestSanitizeNotificationError_WrappedErrUnsupportedChannel_ReturnsUnsupportedChannel(t *testing.T) {
	wrapped := errors.Join(errors.New("provider not registered"), domain.ErrUnsupportedChannel)

	result := sanitizeNotificationError(wrapped)

	assert.Equal(t, "unsupported channel", result)
}

func TestSanitizeNotificationError_GenericError_ReturnsInternalServerError(t *testing.T) {
	result := sanitizeNotificationError(errors.New("connection refused"))

	assert.Equal(t, "internal server error", result)
}

func TestSanitizeNotificationError_DoesNotLeakInternalDetails(t *testing.T) {
	sensitiveErr := errors.New("SELECT * FROM tenants WHERE api_key='secret'")

	result := sanitizeNotificationError(sensitiveErr)

	// The raw error message must never appear in the sanitized output.
	assert.NotContains(t, result, "SELECT")
	assert.NotContains(t, result, "secret")
	assert.Equal(t, "internal server error", result)
}
