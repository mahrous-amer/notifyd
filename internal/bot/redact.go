package bot

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/rs/zerolog"
)

// telegramTokenPattern matches a Telegram bot token as it appears embedded in
// API request URLs (e.g. "bot123456:AA...secret"). The go-telegram-bot-api
// library logs the full request URL on transport errors, which would otherwise
// write the token to logs in cleartext. The numeric bot ID is captured so it
// can be preserved — it is not secret and is useful for debugging.
var telegramTokenPattern = regexp.MustCompile(`bot(\d+):[A-Za-z0-9_-]+`)

// redactToken strips a Telegram bot token from a log message. It scrubs the
// exact configured token literally (defense in depth, in case it appears in an
// unexpected form) and any token-shaped substring in the standard API-URL form,
// keeping the numeric bot ID for debuggability.
func redactToken(token, msg string) string {
	// Pattern first: this preserves the numeric bot ID in the standard
	// bot<id>:<secret> URL form. Then scrub any literal token that appears
	// outside that form (its boundary is untouched by the pattern pass).
	msg = telegramTokenPattern.ReplaceAllString(msg, "bot${1}:REDACTED")
	if token != "" {
		msg = strings.ReplaceAll(msg, token, "REDACTED")
	}
	return msg
}

// redactingLogger adapts the go-telegram-bot-api BotLogger interface onto
// zerolog while stripping the bot token from every line. The upstream library
// logs full request URLs (token included) on transport errors via its
// package-level logger; installing this via tgbotapi.SetLogger keeps the secret
// out of the logs. Library output is emitted at warn level since it only fires
// on API/transport errors.
type redactingLogger struct {
	logger zerolog.Logger
	token  string
}

func (l redactingLogger) Println(v ...interface{}) {
	msg := strings.TrimRight(fmt.Sprintln(v...), "\n")
	l.logger.Warn().Str("source", "telegram-lib").Msg(redactToken(l.token, msg))
}

func (l redactingLogger) Printf(format string, v ...interface{}) {
	l.logger.Warn().Str("source", "telegram-lib").Msg(redactToken(l.token, fmt.Sprintf(format, v...)))
}
