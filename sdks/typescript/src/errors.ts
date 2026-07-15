import type { ApiErrorBody } from "./types.js";

/**
 * Thrown for any non-2xx API response. `code` is the machine-readable
 * `error` field when the body parsed as JSON, so callers can branch on
 * specific failures (e.g. "QUOTA_EXCEEDED") without string-matching a
 * message.
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
}

/** Thrown by `new NotifydClient(...)` when required credentials are missing. */
export class NotifydConfigError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "NotifydConfigError";
  }
}
