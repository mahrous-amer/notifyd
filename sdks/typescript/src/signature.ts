import { createHmac, timingSafeEqual } from "node:crypto";

/**
 * Checks a notifyd webhook delivery's signature header against the request
 * body, using the same scheme notifyd uses to sign both the "webhook"
 * channel type and status-webhook deliveries:
 *
 *   HMAC-SHA256(secret, timestamp + "." + body), hex-encoded, carried in a
 *   header formatted as "sha256=<hex>".
 *
 * @param secret - The signing secret shown once at endpoint creation.
 * @param timestamp - The raw value of the X-Notifyd-Timestamp header.
 * @param body - The exact raw request body bytes. Parse JSON only after
 *   verifying — re-serializing and re-signing will not reproduce the
 *   original signature.
 * @param signatureHeader - The raw value of the X-Notifyd-Signature header.
 * @returns true if the signature is valid, false otherwise.
 *
 * This function does not check timestamp freshness — callers who want
 * replay protection should separately reject requests whose timestamp is
 * older than an acceptable window before calling this.
 */
export function verifyWebhookSignature(
  secret: string,
  timestamp: string,
  body: string | Buffer,
  signatureHeader: string,
): boolean {
  const prefix = "sha256=";
  if (!signatureHeader.startsWith(prefix)) {
    return false;
  }
  const providedHex = signatureHeader.slice(prefix.length);
  if (!/^[0-9a-f]+$/i.test(providedHex)) {
    return false;
  }

  const expectedHex = createHmac("sha256", secret)
    .update(timestamp)
    .update(".")
    .update(body)
    .digest("hex");

  const provided = Buffer.from(providedHex, "hex");
  const expected = Buffer.from(expectedHex, "hex");
  if (provided.length !== expected.length) {
    return false;
  }
  return timingSafeEqual(provided, expected);
}
