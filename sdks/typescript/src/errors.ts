import type { ApiErrorBody } from "./types.js";

/**
 * Thrown for any non-2xx API response. `code` is the machine-readable
 * `error` field when the body parsed as JSON, so callers can branch on
 * specific failures (e.g. "QUOTA_EXCEEDED") without string-matching a
 * message. `body` holds the full parsed response, so error shapes that
 * carry extra context beyond `error` -- QuotaExceededError's
 * `upgrade_url`, SubscriptionExpiredError's `renew_url` -- are still
 * reachable without a bespoke type for every error variant the API might
 * add.
 */
export class NotifydRequestError extends Error {
  readonly statusCode: number;
  readonly code?: string;
  readonly body?: ApiErrorBody;

  constructor(statusCode: number, body?: ApiErrorBody, rawBody?: string) {
    super(body?.error ? `notifyd: ${statusCode} ${body.error}` : `notifyd: ${statusCode} ${rawBody ?? ""}`);
    this.name = "NotifydRequestError";
    this.statusCode = statusCode;
    this.code = body?.error;
    this.body = body;
  }

  /**
   * Returns a string-valued field from the parsed error body, such as
   * `upgrade_url` on a 429 QUOTA_EXCEEDED or `renew_url` on a 402
   * SUBSCRIPTION_PERIOD_EXPIRED. Returns undefined if the body has no such
   * field or the field isn't a string.
   */
  field(key: string): string | undefined {
    const value = this.body?.[key];
    return typeof value === "string" ? value : undefined;
  }
}

/** Thrown by `new NotifydClient(...)` when required credentials are missing. */
export class NotifydConfigError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "NotifydConfigError";
  }
}
