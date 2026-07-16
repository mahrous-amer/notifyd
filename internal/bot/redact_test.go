package bot

import (
	"bytes"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

// realWorldLeak is the exact shape the go-telegram-bot-api library writes to its
// package logger on a getUpdates transport error: the full request URL, token
// included. This is the line that was leaking the token into container logs.
const realWorldLeak = `Post "https://api.telegram.org/bot8669384730:AAHJvTvfufAgIj1YvasoJcl6ydsNqBFoAZM/getUpdates": read tcp 172.19.0.15:40702->149.154.166.110:443: read: connection timed out`

const sampleToken = "8669384730:AAHJvTvfufAgIj1YvasoJcl6ydsNqBFoAZM"
const sampleSecret = "AAHJvTvfufAgIj1YvasoJcl6ydsNqBFoAZM"

func TestRedactToken_RemovesTokenFromApiUrl(t *testing.T) {
	got := redactToken(sampleToken, realWorldLeak)
	if strings.Contains(got, sampleSecret) {
		t.Fatalf("token secret still present after redaction: %q", got)
	}
	// The numeric bot ID is not secret and is kept for debuggability.
	if !strings.Contains(got, "bot8669384730:REDACTED") {
		t.Fatalf("expected redacted form with bot id preserved, got: %q", got)
	}
	// The surrounding error context must survive so the log stays useful.
	if !strings.Contains(got, "connection timed out") {
		t.Fatalf("redaction dropped surrounding context: %q", got)
	}
}

func TestRedactToken_RedactsKnownTokenInAnyForm(t *testing.T) {
	// Defense in depth: even if the token appears outside the standard bot<id>:<secret>
	// URL form, the exact configured token is scrubbed literally.
	msg := "unexpected: raw token " + sampleToken + " somewhere"
	got := redactToken(sampleToken, msg)
	if strings.Contains(got, sampleSecret) {
		t.Fatalf("known token not scrubbed: %q", got)
	}
}

func TestRedactToken_RedactsUnknownTokenShape(t *testing.T) {
	// A different token than the configured one, still in URL form, is redacted
	// by the pattern (token-agnostic) even though it doesn't match the literal.
	msg := `Post "https://api.telegram.org/bot111222333:ZZdifferentTokenValue0000000000000/getMe": timeout`
	got := redactToken(sampleToken, msg)
	if strings.Contains(got, "ZZdifferentTokenValue0000000000000") {
		t.Fatalf("unknown-shaped token not redacted: %q", got)
	}
}

func TestRedactToken_LeavesCleanMessageUnchanged(t *testing.T) {
	msg := "Stopping the update receiver routine..."
	if got := redactToken(sampleToken, msg); got != msg {
		t.Fatalf("clean message altered: %q", got)
	}
}

func TestRedactToken_EmptyTokenStillRedactsUrlForm(t *testing.T) {
	// An empty configured token must not disable the pattern-based redaction.
	got := redactToken("", realWorldLeak)
	if strings.Contains(got, sampleSecret) {
		t.Fatalf("empty-token path skipped pattern redaction: %q", got)
	}
}

func TestRedactingLogger_ScrubsPrintlnAndPrintf(t *testing.T) {
	var buf bytes.Buffer
	lg := redactingLogger{logger: zerolog.New(&buf), token: sampleToken}

	lg.Println(realWorldLeak)
	lg.Printf("Endpoint: %s", "https://api.telegram.org/bot"+sampleToken+"/getUpdates")

	out := buf.String()
	if strings.Contains(out, sampleSecret) {
		t.Fatalf("redactingLogger leaked token: %q", out)
	}
	if !strings.Contains(out, "connection timed out") {
		t.Fatalf("redactingLogger dropped context: %q", out)
	}
}
