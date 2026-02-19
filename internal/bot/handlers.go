package bot

import (
	"context"
	"fmt"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"

	"github.com/bse/notifyd/internal/domain"
)

func (b *Bot) handleStart(chatID int64) {
	b.sendHTML(chatID, welcomeMessage)
}

func (b *Bot) handleTenants(ctx context.Context, chatID int64) {
	tenants, total, err := b.tenantSvc.List(ctx, 50, 0)
	if err != nil {
		b.sendError(chatID, "Failed to fetch tenants", err)
		return
	}
	b.sendHTML(chatID, formatTenantList(tenants, total))
}

func (b *Bot) handleTenant(ctx context.Context, chatID int64, args string) {
	id, ok := b.parseUUID(chatID, args, "Usage: /tenant <id>")
	if !ok {
		return
	}

	tenant, err := b.tenantSvc.GetByID(ctx, id)
	if err != nil {
		b.sendError(chatID, "Tenant not found", err)
		return
	}

	b.sendHTML(chatID, formatTenantDetail(tenant))
}

func (b *Bot) handleCreateTenant(ctx context.Context, chatID int64, args string) {
	parts := strings.Fields(args)
	if len(parts) < 2 {
		b.sendHTML(chatID, "Usage: /create_tenant &lt;name&gt; &lt;slug&gt;")
		return
	}

	name := parts[0]
	slug := parts[1]

	result, err := b.tenantSvc.Create(ctx, domain.CreateTenantInput{
		Name: name,
		Slug: slug,
	})
	if err != nil {
		b.sendError(chatID, "Failed to create tenant", err)
		return
	}

	b.sendHTML(chatID, formatNewTenant(result))
}

func (b *Bot) handleToggleTenant(ctx context.Context, chatID int64, args string) {
	id, ok := b.parseUUID(chatID, args, "Usage: /toggle_tenant <id>")
	if !ok {
		return
	}

	tenant, err := b.tenantSvc.GetByID(ctx, id)
	if err != nil {
		b.sendError(chatID, "Tenant not found", err)
		return
	}

	flipped := !tenant.IsActive
	updated, err := b.tenantSvc.Update(ctx, id, domain.UpdateTenantInput{
		IsActive: &flipped,
	})
	if err != nil {
		b.sendError(chatID, "Failed to update tenant", err)
		return
	}

	b.sendHTML(chatID, formatToggleResult(updated))
}

func (b *Bot) handleDeleteTenant(chatID int64, args string) {
	id, ok := b.parseUUID(chatID, args, "Usage: /delete_tenant <id>")
	if !ok {
		return
	}

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Yes, delete it", callbackDeleteConfirm+id.String()),
			tgbotapi.NewInlineKeyboardButtonData("Cancel", callbackDeleteCancel),
		),
	)

	msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("Are you sure you want to delete tenant <code>%s</code>?", id))
	msg.ParseMode = tgbotapi.ModeHTML
	msg.ReplyMarkup = keyboard
	b.sendMessage(msg)
}

func (b *Bot) handleDeleteConfirm(ctx context.Context, query *tgbotapi.CallbackQuery, tenantIDStr string) {
	chatID := query.Message.Chat.ID

	id, err := uuid.Parse(tenantIDStr)
	if err != nil {
		b.answerCallback(query.ID, "Invalid tenant ID")
		b.sendHTML(chatID, "Invalid tenant ID.")
		return
	}

	if err := b.tenantSvc.Delete(ctx, id); err != nil {
		b.answerCallback(query.ID, "Delete failed")
		b.sendError(chatID, "Failed to delete tenant", err)
		return
	}

	b.answerCallback(query.ID, "Deleted")
	b.editMessageText(query.Message.Chat.ID, query.Message.MessageID,
		fmt.Sprintf("Tenant <code>%s</code> has been deleted.", id))
}

func (b *Bot) handleDeleteCancel(query *tgbotapi.CallbackQuery) {
	b.answerCallback(query.ID, "Cancelled")
	b.editMessageText(query.Message.Chat.ID, query.Message.MessageID, "Deletion cancelled.")
}

func (b *Bot) handleNotifications(ctx context.Context, chatID int64, args string) {
	id, ok := b.parseUUID(chatID, args, "Usage: /notifications <tenant_id>")
	if !ok {
		return
	}

	filter := domain.NotificationFilter{
		TenantID: id,
		Limit:    10,
		Offset:   0,
	}

	notifications, total, err := b.notifSvc.List(ctx, filter)
	if err != nil {
		b.sendError(chatID, "Failed to fetch notifications", err)
		return
	}

	b.sendHTML(chatID, formatNotificationList(notifications, total))
}

func (b *Bot) handleNotification(ctx context.Context, chatID int64, args string) {
	id, ok := b.parseUUID(chatID, args, "Usage: /notification <id>")
	if !ok {
		return
	}

	notification, err := b.notifSvc.GetByID(ctx, id)
	if err != nil {
		b.sendError(chatID, "Notification not found", err)
		return
	}

	b.sendHTML(chatID, formatNotificationDetail(notification))
}

func (b *Bot) handleStats(ctx context.Context, chatID int64) {
	_, tenantTotal, err := b.tenantSvc.List(ctx, 1, 0)
	if err != nil {
		b.sendError(chatID, "Failed to fetch tenant stats", err)
		return
	}

	notifCounts, err := b.fetchNotificationCountsByStatus(ctx)
	if err != nil {
		b.sendError(chatID, "Failed to fetch notification stats", err)
		return
	}

	b.sendHTML(chatID, formatStats(tenantTotal, notifCounts))
}

// fetchNotificationCountsByStatus aggregates notification counts by status
// across all tenants. The notification repository always scopes queries by
// tenant_id, so we must iterate all tenants and sum their counts.
// This is intentionally simple — /stats is an admin utility, not a hot path.
func (b *Bot) fetchNotificationCountsByStatus(ctx context.Context) (map[domain.NotificationStatus]int, error) {
	statuses := []domain.NotificationStatus{
		domain.StatusPending,
		domain.StatusProcessing,
		domain.StatusRetrying,
		domain.StatusDelivered,
		domain.StatusFailed,
	}

	tenants, _, err := b.tenantSvc.List(ctx, 500, 0)
	if err != nil {
		return nil, fmt.Errorf("fetch tenants for stats: %w", err)
	}

	totals := make(map[domain.NotificationStatus]int, len(statuses))

	for _, tenant := range tenants {
		for _, status := range statuses {
			s := status
			filter := domain.NotificationFilter{
				TenantID: tenant.ID,
				Status:   &s,
				Limit:    1,
				Offset:   0,
			}

			_, count, err := b.notifSvc.List(ctx, filter)
			if err != nil {
				return nil, fmt.Errorf("count %s notifications for tenant %s: %w", status, tenant.ID, err)
			}

			totals[status] += count
		}
	}

	return totals, nil
}
