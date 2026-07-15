"""Exceptions raised by the notifyd client."""

from __future__ import annotations

from typing import Any, Optional


class NotifydConfigError(Exception):
    """Raised by NotifydClient() when required credentials are missing."""


class NotifydRequestError(Exception):
    """Raised for any non-2xx API response.

    `code` is the machine-readable `error` field from the response body
    when it parsed as JSON, so callers can branch on specific failures
    (e.g. "QUOTA_EXCEEDED") without string-matching a message. `body` holds
    the full parsed response, so error shapes that carry extra context
    beyond `error` -- QuotaExceededError's `upgrade_url`,
    SubscriptionExpiredError's `renew_url` -- are still reachable without a
    bespoke exception type for every error variant the API might add.
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

    def field(self, key: str) -> Optional[str]:
        """Returns a string-valued field from the parsed error body, such
        as upgrade_url on a 429 QUOTA_EXCEEDED or renew_url on a 402
        SUBSCRIPTION_PERIOD_EXPIRED. Returns None if the body has no such
        field or the field isn't a string."""
        if self.body is None:
            return None
        value = self.body.get(key)
        return value if isinstance(value, str) else None
