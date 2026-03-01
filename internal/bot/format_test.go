package bot

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/bse/notifyd/internal/domain"
	"github.com/bse/notifyd/internal/service"
)

// fixedTime is a stable timestamp used across all time-sensitive assertions.
var fixedTime = time.Date(2024, 6, 15, 10, 30, 0, 0, time.UTC)

// fixedTimeFormatted is the expected string representation of fixedTime.
const fixedTimeFormatted = "2024-06-15 10:30 UTC"

func TestFormatTenantList_EmptySlice(t *testing.T) {
	result := formatTenantList([]*domain.Tenant{}, 0)

	assert.Equal(t, "<b>No tenants found.</b>", result)
}

func TestFormatTenantList_NonEmpty(t *testing.T) {
	tenantID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	tenants := []*domain.Tenant{
		{
			ID:       tenantID,
			Name:     "Acme Corp",
			Slug:     "acme-corp",
			IsActive: true,
		},
	}

	result := formatTenantList(tenants, 1)

	assert.Contains(t, result, "Acme Corp")
	assert.Contains(t, result, tenantID.String())
	assert.Contains(t, result, "acme-corp")
	assert.Contains(t, result, "1 total")
	// Active tenant should show the check mark label.
	assert.Contains(t, result, "✅")
}

func TestFormatTenantList_InactiveTenant(t *testing.T) {
	tenants := []*domain.Tenant{
		{
			ID:       uuid.New(),
			Name:     "Dormant Inc",
			Slug:     "dormant",
			IsActive: false,
		},
	}

	result := formatTenantList(tenants, 1)

	assert.Contains(t, result, "❌")
}

func TestFormatTenantDetail_ContainsAllFields(t *testing.T) {
	tenantID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	tenant := &domain.Tenant{
		ID:        tenantID,
		Name:      "Test Tenant",
		Slug:      "test-tenant",
		APIKey:    "apikey-abc123",
		IsActive:  true,
		CreatedAt: fixedTime,
		UpdatedAt: fixedTime,
	}

	result := formatTenantDetail(tenant)

	assert.Contains(t, result, "Test Tenant")
	assert.Contains(t, result, tenantID.String())
	assert.Contains(t, result, "test-tenant")
	assert.Contains(t, result, "apikey-abc123")
	assert.Contains(t, result, "✅")
	assert.Contains(t, result, fixedTimeFormatted)
}

func TestFormatNewTenant_ContainsAllFields(t *testing.T) {
	tenantID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	result := &service.CreateTenantResult{
		Tenant: &domain.Tenant{
			ID:   tenantID,
			Name: "New Corp",
			Slug: "new-corp",
		},
		APIKey:    "key-xyz",
		APISecret: "secret-abc",
	}

	output := formatNewTenant(result)

	assert.Contains(t, output, tenantID.String())
	assert.Contains(t, output, "New Corp")
	assert.Contains(t, output, "new-corp")
	assert.Contains(t, output, "key-xyz")
	assert.Contains(t, output, "secret-abc")
	// Must warn the user to store the secret immediately.
	assert.Contains(t, output, "Store the API secret now")
}

func TestFormatToggleResult_ActiveTenant(t *testing.T) {
	tenant := &domain.Tenant{Name: "Alpha", IsActive: true}

	result := formatToggleResult(tenant)

	assert.Contains(t, result, "Alpha")
	assert.Contains(t, result, "active")
	assert.NotContains(t, result, "inactive")
}

func TestFormatToggleResult_InactiveTenant(t *testing.T) {
	tenant := &domain.Tenant{Name: "Beta", IsActive: false}

	result := formatToggleResult(tenant)

	assert.Contains(t, result, "Beta")
	assert.Contains(t, result, "inactive")
}

func TestFormatNotificationList_EmptySlice(t *testing.T) {
	result := formatNotificationList([]*domain.Notification{}, 0)

	assert.Equal(t, "<b>No notifications found for this tenant.</b>", result)
}

func TestFormatNotificationList_NonEmpty(t *testing.T) {
	notifID := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	notifications := []*domain.Notification{
		{
			ID:        notifID,
			Status:    domain.StatusDelivered,
			Channel:   domain.ChannelTelegram,
			CreatedAt: fixedTime,
		},
	}

	result := formatNotificationList(notifications, 1)

	assert.Contains(t, result, notifID.String())
	assert.Contains(t, result, string(domain.StatusDelivered))
	assert.Contains(t, result, "1 total")
}

func TestFormatNotificationDetail_WithNilOptionalFields(t *testing.T) {
	notifID := uuid.MustParse("55555555-5555-5555-5555-555555555555")
	tenantID := uuid.MustParse("66666666-6666-6666-6666-666666666666")
	notification := &domain.Notification{
		ID:          notifID,
		TenantID:    tenantID,
		Channel:     domain.ChannelDiscord,
		Status:      domain.StatusPending,
		RetryCount:  1,
		MaxRetries:  3,
		LastError:   nil,
		DeliveredAt: nil,
		CreatedAt:   fixedTime,
		UpdatedAt:   fixedTime,
	}

	result := formatNotificationDetail(notification)

	assert.Contains(t, result, notifID.String())
	assert.Contains(t, result, tenantID.String())
	assert.Contains(t, result, string(domain.ChannelDiscord))
	assert.Contains(t, result, string(domain.StatusPending))
	assert.Contains(t, result, "1 / 3")
	assert.Contains(t, result, fixedTimeFormatted)
	// Neither the error nor the delivered-at line should appear when nil.
	assert.NotContains(t, result, "Error:")
	assert.NotContains(t, result, "Delivered:")
}

func TestFormatNotificationDetail_WithPopulatedOptionalFields(t *testing.T) {
	errMsg := "timeout connecting to provider"
	deliveredAt := fixedTime.Add(5 * time.Minute)
	notification := &domain.Notification{
		ID:          uuid.New(),
		TenantID:    uuid.New(),
		Channel:     domain.ChannelWhatsApp,
		Status:      domain.StatusDelivered,
		RetryCount:  0,
		MaxRetries:  3,
		LastError:   &errMsg,
		DeliveredAt: &deliveredAt,
		CreatedAt:   fixedTime,
		UpdatedAt:   fixedTime,
	}

	result := formatNotificationDetail(notification)

	assert.Contains(t, result, errMsg)
	assert.Contains(t, result, "Delivered:")
}

func TestFormatStats_ContainsTenantCountAndAllStatuses(t *testing.T) {
	counts := map[domain.NotificationStatus]int{
		domain.StatusPending:    10,
		domain.StatusProcessing: 3,
		domain.StatusRetrying:   2,
		domain.StatusDelivered:  50,
		domain.StatusFailed:     5,
	}

	result := formatStats(7, counts)

	assert.Contains(t, result, "7")
	assert.Contains(t, result, string(domain.StatusPending))
	assert.Contains(t, result, string(domain.StatusProcessing))
	assert.Contains(t, result, string(domain.StatusRetrying))
	assert.Contains(t, result, string(domain.StatusDelivered))
	assert.Contains(t, result, string(domain.StatusFailed))
}

func TestActiveLabel_TrueReturnsCheckMark(t *testing.T) {
	assert.Equal(t, "✅", activeLabel(true))
}

func TestActiveLabel_FalseReturnsX(t *testing.T) {
	assert.Equal(t, "❌", activeLabel(false))
}

func TestActiveWord_TrueReturnsActive(t *testing.T) {
	assert.Equal(t, "active", activeWord(true))
}

func TestActiveWord_FalseReturnsInactive(t *testing.T) {
	assert.Equal(t, "inactive", activeWord(false))
}

func TestFormatTime_FormatsInUTC(t *testing.T) {
	// Create a time in a non-UTC zone to confirm the output is always UTC.
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("America/New_York timezone not available; skipping timezone test")
	}
	newYorkTime := time.Date(2024, 6, 15, 6, 30, 0, 0, loc)

	result := formatTime(newYorkTime)

	// 06:30 ET is 10:30 UTC.
	assert.Equal(t, fixedTimeFormatted, result)
}

func TestFormatTime_IncludesUTCSuffix(t *testing.T) {
	result := formatTime(fixedTime)

	assert.True(t, strings.HasSuffix(result, "UTC"), "expected result to end with UTC, got: %s", result)
}
