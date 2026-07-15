package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bse/notifyd/internal/domain"
)

func TestWebhookEndpoint_SubscribesTo(t *testing.T) {
	t.Run("matches a subscribed event on an active endpoint", func(t *testing.T) {
		e := &domain.WebhookEndpoint{IsActive: true, Events: []string{"notification.delivered"}}
		assert.True(t, e.SubscribesTo(domain.WebhookEventDelivered))
	})

	t.Run("does not match an unsubscribed event", func(t *testing.T) {
		e := &domain.WebhookEndpoint{IsActive: true, Events: []string{"notification.delivered"}}
		assert.False(t, e.SubscribesTo(domain.WebhookEventFailed))
	})

	t.Run("does not match on an inactive endpoint even if subscribed", func(t *testing.T) {
		e := &domain.WebhookEndpoint{IsActive: false, Events: []string{"notification.delivered"}}
		assert.False(t, e.SubscribesTo(domain.WebhookEventDelivered))
	})
}

func TestIsValidWebhookEventType(t *testing.T) {
	t.Run("accepts notification.delivered", func(t *testing.T) {
		assert.True(t, domain.IsValidWebhookEventType(domain.WebhookEventDelivered))
	})

	t.Run("accepts notification.failed", func(t *testing.T) {
		assert.True(t, domain.IsValidWebhookEventType(domain.WebhookEventFailed))
	})

	t.Run("rejects an unknown event type", func(t *testing.T) {
		assert.False(t, domain.IsValidWebhookEventType("notification.queued"))
	})
}

func TestValidateWebhookEvents(t *testing.T) {
	t.Run("accepts a single valid event", func(t *testing.T) {
		err := domain.ValidateWebhookEvents([]string{"notification.delivered"})
		assert.NoError(t, err)
	})

	t.Run("accepts both valid events", func(t *testing.T) {
		err := domain.ValidateWebhookEvents([]string{"notification.delivered", "notification.failed"})
		assert.NoError(t, err)
	})

	t.Run("rejects an empty event list", func(t *testing.T) {
		err := domain.ValidateWebhookEvents(nil)
		assert.ErrorIs(t, err, domain.ErrValidationFailed)
	})

	t.Run("rejects an unknown event type", func(t *testing.T) {
		err := domain.ValidateWebhookEvents([]string{"notification.queued"})
		assert.ErrorIs(t, err, domain.ErrValidationFailed)
	})

	t.Run("rejects a duplicate event type", func(t *testing.T) {
		err := domain.ValidateWebhookEvents([]string{"notification.delivered", "notification.delivered"})
		assert.ErrorIs(t, err, domain.ErrValidationFailed)
	})
}
