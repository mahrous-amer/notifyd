"""Exceptions raised by the notifyd client."""

from __future__ import annotations

from typing import Any


class NotifydConfigError(Exception):
    """Raised by NotifydClient() when required credentials are missing."""


class NotifydRequestError(Exception):
    """Raised for any non-2xx API response.

    `code` is the machine-readable `error` field from the response body
    when it parsed as JSON, so callers can branch on specific failures
    (e.g. "QUOTA_EXCEEDED") without string-matching a message.
    """

    def __init__(
        self,
        status_code: int,
        code: str | None = None,
        body: dict[str, Any] | None = None,
        raw_body: str = "",
    ) -> None:
        self.status_code = status_code
        self.code = code
        self.body = body
        message = f"notifyd: {status_code} {code}" if code else f"notifyd: {status_code} {raw_body}"
        super().__init__(message)
