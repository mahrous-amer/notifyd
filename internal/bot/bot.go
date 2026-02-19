// Package bot implements the Telegram admin bot for the notifyd service.
// It provides a conversational interface over the tenant and notification
// services, restricted to a single configured admin chat ID.
package bot

import (
	"context"
	"fmt"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/bse/notifyd/internal/service"
)

// Callback data prefixes used by inline keyboards.
const (
	callbackDeleteConfirm = "delete_confirm:"
	callbackDeleteCancel  = "delete_cancel"
)

// Bot is the Telegram admin bot. It holds references to the services it needs
// and the Telegram API client. All command handling is restricted to the
// configured adminChatID.
type Bot struct {
	api         *tgbotapi.BotAPI
	tenantSvc   *service.TenantService
	notifSvc    *service.NotificationService
	adminChatID int64
	logger      zerolog.Logger
}

// BotConfig groups the fields required to construct a Bot, keeping the
// constructor to a single structured argument rather than four positional
// parameters.
type BotConfig struct {
	Token       string
	AdminChatID int64
	TenantSvc   *service.TenantService
	NotifSvc    *service.NotificationService
	Logger      zerolog.Logger
}

// New creates a Bot and connects to the Telegram API. It returns an error
// if the token is invalid or the API is unreachable.
func New(cfg BotConfig) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(cfg.Token)
	if err != nil {
		return nil, fmt.Errorf("connect to telegram api: %w", err)
	}

	cfg.Logger.Info().Str("username", api.Self.UserName).Msg("telegram bot authenticated")

	return &Bot{
		api:         api,
		tenantSvc:   cfg.TenantSvc,
		notifSvc:    cfg.NotifSvc,
		adminChatID: cfg.AdminChatID,
		logger:      cfg.Logger,
	}, nil
}

// Start begins the Telegram long-poll update loop. It blocks until ctx is
// cancelled, at which point it stops polling and returns nil.
func (b *Bot) Start(ctx context.Context) error {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 30

	updates := b.api.GetUpdatesChan(u)

	b.logger.Info().Int64("admin_chat_id", b.adminChatID).Msg("bot update loop started")

	for {
		select {
		case <-ctx.Done():
			b.api.StopReceivingUpdates()
			b.logger.Info().Msg("bot update loop stopped")
			return nil

		case update, ok := <-updates:
			if !ok {
				return nil
			}
			b.dispatchUpdate(ctx, update)
		}
	}
}

// dispatchUpdate routes an incoming update to the appropriate handler based
// on whether it is a message or a callback query.
func (b *Bot) dispatchUpdate(ctx context.Context, update tgbotapi.Update) {
	switch {
	case update.Message != nil:
		b.dispatchMessage(ctx, update.Message)
	case update.CallbackQuery != nil:
		b.dispatchCallback(ctx, update.CallbackQuery)
	}
}

// dispatchMessage routes a chat message to the correct command handler.
func (b *Bot) dispatchMessage(ctx context.Context, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID

	if !b.isAuthorized(chatID) {
		b.sendHTML(chatID, "<b>Unauthorized.</b> This bot is restricted to the configured admin chat.")
		return
	}

	if !msg.IsCommand() {
		return
	}

	command := msg.Command()
	args := strings.TrimSpace(msg.CommandArguments())

	b.logger.Info().
		Int64("chat_id", chatID).
		Str("command", command).
		Str("args", args).
		Msg("received command")

	switch command {
	case "start", "help":
		b.handleStart(chatID)
	case "tenants":
		b.handleTenants(ctx, chatID)
	case "tenant":
		b.handleTenant(ctx, chatID, args)
	case "create_tenant":
		b.handleCreateTenant(ctx, chatID, args)
	case "toggle_tenant":
		b.handleToggleTenant(ctx, chatID, args)
	case "delete_tenant":
		b.handleDeleteTenant(chatID, args)
	case "notifications":
		b.handleNotifications(ctx, chatID, args)
	case "notification":
		b.handleNotification(ctx, chatID, args)
	case "stats":
		b.handleStats(ctx, chatID)
	default:
		b.sendHTML(chatID, "Unknown command. Send /help for a list of available commands.")
	}
}

// dispatchCallback routes inline keyboard button presses to their handler.
func (b *Bot) dispatchCallback(ctx context.Context, query *tgbotapi.CallbackQuery) {
	chatID := query.Message.Chat.ID

	if !b.isAuthorized(chatID) {
		b.answerCallback(query.ID, "Unauthorized")
		return
	}

	data := query.Data

	switch {
	case strings.HasPrefix(data, callbackDeleteConfirm):
		tenantIDStr := strings.TrimPrefix(data, callbackDeleteConfirm)
		b.handleDeleteConfirm(ctx, query, tenantIDStr)

	case data == callbackDeleteCancel:
		b.handleDeleteCancel(query)

	default:
		b.answerCallback(query.ID, "Unknown action")
	}
}

// isAuthorized returns true only when the incoming chat matches the configured
// admin chat ID. A zero admin chat ID is treated as "no restriction configured"
// and rejects all messages to avoid accidentally operating unsecured.
func (b *Bot) isAuthorized(chatID int64) bool {
	return b.adminChatID != 0 && chatID == b.adminChatID
}

// parseUUID parses a UUID from a raw string argument. On failure it sends a
// formatted usage hint to the chat and returns false.
func (b *Bot) parseUUID(chatID int64, raw, usageHint string) (uuid.UUID, bool) {
	id, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil {
		b.sendHTML(chatID, fmt.Sprintf("Invalid UUID.\n<i>%s</i>", usageHint))
		return uuid.UUID{}, false
	}
	return id, true
}

// --- Low-level send helpers -------------------------------------------------

func (b *Bot) sendHTML(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = tgbotapi.ModeHTML
	b.sendMessage(msg)
}

func (b *Bot) sendError(chatID int64, description string, err error) {
	b.logger.Error().Err(err).Str("description", description).Msg("bot handler error")
	b.sendHTML(chatID, fmt.Sprintf("<b>Error:</b> %s\n<code>%s</code>", description, err.Error()))
}

func (b *Bot) sendMessage(msg tgbotapi.MessageConfig) {
	if _, err := b.api.Send(msg); err != nil {
		b.logger.Error().Err(err).Int64("chat_id", msg.ChatID).Msg("failed to send message")
	}
}

func (b *Bot) answerCallback(callbackID, text string) {
	answer := tgbotapi.NewCallback(callbackID, text)
	if _, err := b.api.Request(answer); err != nil {
		b.logger.Error().Err(err).Str("callback_id", callbackID).Msg("failed to answer callback")
	}
}

func (b *Bot) editMessageText(chatID int64, messageID int, text string) {
	edit := tgbotapi.NewEditMessageText(chatID, messageID, text)
	edit.ParseMode = tgbotapi.ModeHTML
	if _, err := b.api.Request(edit); err != nil {
		b.logger.Error().Err(err).Msg("failed to edit message")
	}
}
