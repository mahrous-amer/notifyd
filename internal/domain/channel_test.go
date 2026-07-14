package domain_test

import (
	"errors"
	"testing"

	"github.com/bse/notifyd/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsValidChannelType(t *testing.T) {
	t.Run("accepts discord", func(t *testing.T) {
		assert.True(t, domain.IsValidChannelType(domain.ChannelDiscord))
	})

	t.Run("accepts telegram", func(t *testing.T) {
		assert.True(t, domain.IsValidChannelType(domain.ChannelTelegram))
	})

	t.Run("accepts whatsapp", func(t *testing.T) {
		assert.True(t, domain.IsValidChannelType(domain.ChannelWhatsApp))
	})

	t.Run("accepts email", func(t *testing.T) {
		assert.True(t, domain.IsValidChannelType(domain.ChannelEmail))
	})

	t.Run("accepts slack", func(t *testing.T) {
		assert.True(t, domain.IsValidChannelType(domain.ChannelSlack))
	})

	t.Run("accepts webhook", func(t *testing.T) {
		assert.True(t, domain.IsValidChannelType(domain.ChannelWebhook))
	})

	t.Run("rejects unknown type", func(t *testing.T) {
		assert.False(t, domain.IsValidChannelType(domain.ChannelType("carrier-pigeon")))
	})
}

func TestValidChannelTypes(t *testing.T) {
	types := domain.ValidChannelTypes()

	require.Len(t, types, 6, "expected exactly six valid channel types")
	assert.Contains(t, types, domain.ChannelDiscord)
	assert.Contains(t, types, domain.ChannelTelegram)
	assert.Contains(t, types, domain.ChannelWhatsApp)
	assert.Contains(t, types, domain.ChannelEmail)
	assert.Contains(t, types, domain.ChannelSlack)
	assert.Contains(t, types, domain.ChannelWebhook)
}

func TestDeliveryPreferencesValidate(t *testing.T) {
	negativeRetries := -1
	zeroRetries := 0
	positiveRetries := 5

	t.Run("nil receiver returns nil", func(t *testing.T) {
		var dp *domain.DeliveryPreferences
		assert.NoError(t, dp.Validate())
	})

	t.Run("empty struct is valid", func(t *testing.T) {
		dp := &domain.DeliveryPreferences{}
		assert.NoError(t, dp.Validate())
	})

	t.Run("priority critical is valid", func(t *testing.T) {
		dp := &domain.DeliveryPreferences{Priority: "critical"}
		assert.NoError(t, dp.Validate())
	})

	t.Run("priority normal is valid", func(t *testing.T) {
		dp := &domain.DeliveryPreferences{Priority: "normal"}
		assert.NoError(t, dp.Validate())
	})

	t.Run("priority low is valid", func(t *testing.T) {
		dp := &domain.DeliveryPreferences{Priority: "low"}
		assert.NoError(t, dp.Validate())
	})

	t.Run("invalid priority returns validation error", func(t *testing.T) {
		dp := &domain.DeliveryPreferences{Priority: "urgent"}
		err := dp.Validate()
		require.Error(t, err)
		assert.True(t, errors.Is(err, domain.ErrValidationFailed))
	})

	t.Run("format_mode plain is valid", func(t *testing.T) {
		dp := &domain.DeliveryPreferences{FormatMode: "plain"}
		assert.NoError(t, dp.Validate())
	})

	t.Run("format_mode markdown is valid", func(t *testing.T) {
		dp := &domain.DeliveryPreferences{FormatMode: "markdown"}
		assert.NoError(t, dp.Validate())
	})

	t.Run("format_mode html is valid", func(t *testing.T) {
		dp := &domain.DeliveryPreferences{FormatMode: "html"}
		assert.NoError(t, dp.Validate())
	})

	t.Run("invalid format_mode returns validation error", func(t *testing.T) {
		dp := &domain.DeliveryPreferences{FormatMode: "rtf"}
		err := dp.Validate()
		require.Error(t, err)
		assert.True(t, errors.Is(err, domain.ErrValidationFailed))
	})

	t.Run("negative max_retries returns validation error", func(t *testing.T) {
		dp := &domain.DeliveryPreferences{MaxRetries: &negativeRetries}
		err := dp.Validate()
		require.Error(t, err)
		assert.True(t, errors.Is(err, domain.ErrValidationFailed))
	})

	t.Run("zero max_retries is valid", func(t *testing.T) {
		dp := &domain.DeliveryPreferences{MaxRetries: &zeroRetries}
		assert.NoError(t, dp.Validate())
	})

	t.Run("positive max_retries is valid", func(t *testing.T) {
		dp := &domain.DeliveryPreferences{MaxRetries: &positiveRetries}
		assert.NoError(t, dp.Validate())
	})

	t.Run("nil max_retries pointer is valid", func(t *testing.T) {
		dp := &domain.DeliveryPreferences{MaxRetries: nil}
		assert.NoError(t, dp.Validate())
	})
}
