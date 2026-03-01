package domain_test

import (
	"testing"

	"github.com/bse/notifyd/internal/domain"
	"github.com/stretchr/testify/assert"
)

func TestIsValidNotificationStatus(t *testing.T) {
	t.Run("accepts pending", func(t *testing.T) {
		assert.True(t, domain.IsValidNotificationStatus(domain.StatusPending))
	})

	t.Run("accepts processing", func(t *testing.T) {
		assert.True(t, domain.IsValidNotificationStatus(domain.StatusProcessing))
	})

	t.Run("accepts delivered", func(t *testing.T) {
		assert.True(t, domain.IsValidNotificationStatus(domain.StatusDelivered))
	})

	t.Run("accepts failed", func(t *testing.T) {
		assert.True(t, domain.IsValidNotificationStatus(domain.StatusFailed))
	})

	t.Run("accepts retrying", func(t *testing.T) {
		assert.True(t, domain.IsValidNotificationStatus(domain.StatusRetrying))
	})

	t.Run("rejects unknown status", func(t *testing.T) {
		assert.False(t, domain.IsValidNotificationStatus(domain.NotificationStatus("cancelled")))
	})
}
