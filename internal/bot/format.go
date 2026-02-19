package bot

import (
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/bse/notifyd/internal/domain"
	"github.com/bse/notifyd/internal/service"
)

const welcomeMessage = `<b>notifyd Admin Bot</b>

Available commands:

<b>Tenants</b>
/tenants — list all tenants
/tenant &lt;id&gt; — show tenant details
/create_tenant &lt;name&gt; &lt;slug&gt; — create a new tenant
/toggle_tenant &lt;id&gt; — toggle tenant active state
/delete_tenant &lt;id&gt; — delete a tenant (asks for confirmation)

<b>Notifications</b>
/notifications &lt;tenant_id&gt; — list recent notifications (last 10)
/notification &lt;id&gt; — show notification details

<b>Stats</b>
/stats — show system-wide counts`

func formatTenantList(tenants []*domain.Tenant, total int) string {
	if len(tenants) == 0 {
		return "<b>No tenants found.</b>"
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "<b>Tenants</b> (%d total)\n\n", total)

	for _, t := range tenants {
		activeLabel := activeLabel(t.IsActive)
		fmt.Fprintf(&sb, "%s <code>%s</code>\n", activeLabel, html.EscapeString(t.Name))
		fmt.Fprintf(&sb, "   ID: <code>%s</code>\n", t.ID)
		fmt.Fprintf(&sb, "   Slug: <code>%s</code>\n\n", html.EscapeString(t.Slug))
	}

	return strings.TrimRight(sb.String(), "\n")
}

func formatTenantDetail(t *domain.Tenant) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "<b>Tenant: %s</b>\n\n", html.EscapeString(t.Name))
	fmt.Fprintf(&sb, "ID:      <code>%s</code>\n", t.ID)
	fmt.Fprintf(&sb, "Slug:    <code>%s</code>\n", html.EscapeString(t.Slug))
	fmt.Fprintf(&sb, "Active:  %s\n", activeLabel(t.IsActive))
	fmt.Fprintf(&sb, "API Key: <code>%s</code>\n", html.EscapeString(t.APIKey))
	fmt.Fprintf(&sb, "Created: %s\n", formatTime(t.CreatedAt))
	fmt.Fprintf(&sb, "Updated: %s", formatTime(t.UpdatedAt))
	return sb.String()
}

func formatNewTenant(result *service.CreateTenantResult) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "<b>Tenant created successfully.</b>\n\n")
	fmt.Fprintf(&sb, "ID:         <code>%s</code>\n", result.Tenant.ID)
	fmt.Fprintf(&sb, "Name:       <code>%s</code>\n", html.EscapeString(result.Tenant.Name))
	fmt.Fprintf(&sb, "Slug:       <code>%s</code>\n", html.EscapeString(result.Tenant.Slug))
	fmt.Fprintf(&sb, "API Key:    <code>%s</code>\n", html.EscapeString(result.APIKey))
	fmt.Fprintf(&sb, "API Secret: <code>%s</code>\n\n", html.EscapeString(result.APISecret))
	fmt.Fprintf(&sb, "<i>Store the API secret now — it will not be shown again.</i>")
	return sb.String()
}

func formatToggleResult(t *domain.Tenant) string {
	return fmt.Sprintf(
		"Tenant <code>%s</code> is now <b>%s</b>.",
		html.EscapeString(t.Name),
		activeWord(t.IsActive),
	)
}

func formatNotificationList(notifications []*domain.Notification, total int) string {
	if len(notifications) == 0 {
		return "<b>No notifications found for this tenant.</b>"
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "<b>Recent Notifications</b> (%d total)\n\n", total)

	for _, n := range notifications {
		fmt.Fprintf(&sb, "• <code>%s</code>\n", n.ID)
		fmt.Fprintf(&sb, "  Status: <b>%s</b>  Channel: <code>%s</code>\n", n.Status, n.Channel)
		fmt.Fprintf(&sb, "  Sent: %s\n\n", formatTime(n.CreatedAt))
	}

	return strings.TrimRight(sb.String(), "\n")
}

func formatNotificationDetail(n *domain.Notification) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "<b>Notification</b>\n\n")
	fmt.Fprintf(&sb, "ID:      <code>%s</code>\n", n.ID)
	fmt.Fprintf(&sb, "Tenant:  <code>%s</code>\n", n.TenantID)
	fmt.Fprintf(&sb, "Channel: <code>%s</code>\n", n.Channel)
	fmt.Fprintf(&sb, "Status:  <b>%s</b>\n", n.Status)
	fmt.Fprintf(&sb, "Retries: %d / %d\n", n.RetryCount, n.MaxRetries)

	if n.LastError != nil {
		fmt.Fprintf(&sb, "Error:   <code>%s</code>\n", html.EscapeString(*n.LastError))
	}
	if n.DeliveredAt != nil {
		fmt.Fprintf(&sb, "Delivered: %s\n", formatTime(*n.DeliveredAt))
	}

	fmt.Fprintf(&sb, "Created: %s\n", formatTime(n.CreatedAt))
	fmt.Fprintf(&sb, "Updated: %s", formatTime(n.UpdatedAt))
	return sb.String()
}

func formatStats(tenantCount int, notifCounts map[domain.NotificationStatus]int) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "<b>System Stats</b>\n\n")
	fmt.Fprintf(&sb, "Total Tenants: <b>%d</b>\n\n", tenantCount)
	fmt.Fprintf(&sb, "<b>Notifications by Status</b>\n")

	statuses := []domain.NotificationStatus{
		domain.StatusPending,
		domain.StatusProcessing,
		domain.StatusRetrying,
		domain.StatusDelivered,
		domain.StatusFailed,
	}
	for _, s := range statuses {
		fmt.Fprintf(&sb, "  %s: <b>%d</b>\n", s, notifCounts[s])
	}

	return strings.TrimRight(sb.String(), "\n")
}

func activeLabel(isActive bool) string {
	if isActive {
		return "✅"
	}
	return "❌"
}

func activeWord(isActive bool) string {
	if isActive {
		return "active"
	}
	return "inactive"
}

func formatTime(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04 UTC")
}
