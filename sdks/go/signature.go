package notifyd

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
)

// ErrInvalidSignature is returned by VerifyWebhookSignature when the
// provided signature header does not match the expected HMAC, or is
// malformed.
var ErrInvalidSignature = errors.New("notifyd: invalid webhook signature")

// VerifyWebhookSignature checks a notifyd webhook delivery's signature
// header against the request body, using the same scheme notifyd uses to
// sign both the "webhook" channel type and status-webhook deliveries:
//
//	HMAC-SHA256(secret, timestamp + "." + body), hex-encoded, carried in a
//	header formatted as "sha256=<hex>".
//
// secret is the signing secret shown once at endpoint creation. timestamp
// is the raw value of the X-Notifyd-Timestamp header. body is the exact
// raw request body bytes (parse JSON only after verifying — re-serializing
// and re-signing will not reproduce the original signature). signatureHeader
// is the raw value of the X-Notifyd-Signature header.
//
// Returns nil if the signature is valid, or ErrInvalidSignature otherwise.
// This function does not check timestamp freshness — callers who want
// replay protection should separately reject requests whose timestamp is
// older than an acceptable window before calling this.
func VerifyWebhookSignature(secret, timestamp string, body []byte, signatureHeader string) error {
	const prefix = "sha256="
	if !strings.HasPrefix(signatureHeader, prefix) {
		return ErrInvalidSignature
	}
	provided := strings.TrimPrefix(signatureHeader, prefix)

	providedBytes, err := hex.DecodeString(provided)
	if err != nil {
		return ErrInvalidSignature
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	expected := mac.Sum(nil)

	if !hmac.Equal(providedBytes, expected) {
		return ErrInvalidSignature
	}
	return nil
}
