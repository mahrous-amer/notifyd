"""The notifyd API client: token exchange/caching and resource methods."""

from __future__ import annotations

import re
import threading
import time
from typing import Any, Optional

import requests

from .errors import NotifydConfigError, NotifydRequestError
from .types import (
    ApiKey,
    ChannelConfig,
    CreateApiKeyResponse,
    DeliveryAttempt,
    DeliveryMetric,
    DeliveryPreferences,
    Notification,
    NotificationList,
    SendMultiResponse,
    WebhookEndpoint,
    WebhookEndpointCreated,
    _drop_none,
)

DEFAULT_BASE_URL = "https://notifyd.fluxintek.com/api"

# One (amount, unit) pair, e.g. the "4ms" in "1h2m3s4ms". Unit alternatives
# list "ns"/"us"/"µs"/"μs"/"ms" before the bare "m"/"s" they'd otherwise be
# swallowed by -- regex alternation matches the first alternative that
# fits, so "m" would consume the "m" in "500ms" and leave a dangling,
# unmatched "s" if it came first.
_DURATION_UNIT = r"(\d+(?:\.\d+)?)(ns|us|µs|μs|ms|h|m|s)"

# Anchored to match one or more consecutive unit pairs spanning the ENTIRE
# string, with nothing before or after. Go's own time.ParseDuration is
# similarly strict: "45s garbage" and "garbage 45s" are both parse errors,
# not "45 seconds with some ignored trailing/leading text."
_FULL_DURATION_PATTERN = re.compile(rf"^(?:{_DURATION_UNIT})+$")
_UNIT_PATTERN = re.compile(_DURATION_UNIT)

_SECONDS_PER_UNIT = {
    "h": 3600.0,
    "m": 60.0,
    "s": 1.0,
    "ms": 1e-3,
    "us": 1e-6,
    "µs": 1e-6,  # U+00B5 MICRO SIGN
    "μs": 1e-6,  # U+03BC GREEK SMALL LETTER MU -- Go accepts both spellings
    "ns": 1e-9,
}


def _parse_go_duration_seconds(duration: str) -> float:
    """Parses a Go-style duration string into seconds.

    Unlike Go's time.ParseDuration, this rejects negative durations. A
    negative expires_in can never be legitimate for a token lifetime, so
    treating it as a parse error catches a malformed/malicious response
    instead of silently caching a token as already expired.
    """
    if duration.startswith("-"):
        raise ValueError(f'notifyd: negative duration not allowed: "{duration}"')
    if not _FULL_DURATION_PATTERN.match(duration):
        raise ValueError(f'notifyd: unparseable duration "{duration}"')

    matches = _UNIT_PATTERN.findall(duration)
    return sum(float(amount) * _SECONDS_PER_UNIT[unit] for amount, unit in matches)


class NotifydClient:
    """Official client for the notifyd notification delivery API."""

    def __init__(
        self,
        api_key: str,
        api_secret: str,
        base_url: str = DEFAULT_BASE_URL,
        session: Optional[requests.Session] = None,
    ) -> None:
        if not api_key or not api_secret:
            raise NotifydConfigError("notifyd: api_key and api_secret are required")
        self._api_key = api_key
        self._api_secret = api_secret
        self._base_url = base_url.rstrip("/")
        self._session = session or requests.Session()

        self._token_lock = threading.Lock()
        self._cached_token: Optional[str] = None
        self._token_expires_at: float = 0.0

    # -- Token exchange ----------------------------------------------------

    def _authenticated_token(self) -> str:
        """Returns a cached JWT if it isn't near expiry, else exchanges for a fresh one.

        _token_lock is deliberately held across the /auth/token HTTP round
        trip inside _fetch_and_cache_token_locked, not just around the
        cache read: concurrent threads that all observe a stale cache must
        queue behind the first exchange and then reuse its result, rather
        than each firing their own redundant request.
        """
        expiry_margin_seconds = 30
        with self._token_lock:
            if self._cached_token and time.monotonic() + expiry_margin_seconds < self._token_expires_at:
                return self._cached_token
            return self._fetch_and_cache_token_locked()

    def _fetch_and_cache_token_locked(self) -> str:
        """Exchanges credentials for a new JWT and caches it. Caller must hold _token_lock."""
        response = self._session.post(
            f"{self._base_url}/auth/token",
            json={"api_key": self._api_key, "api_secret": self._api_secret},
        )
        if not response.ok:
            raise _request_error_from_response(response)

        payload = response.json()
        self._cached_token = payload["token"]
        self._token_expires_at = time.monotonic() + _parse_go_duration_seconds(payload["expires_in"])
        return self._cached_token

    def _force_refresh_token(self) -> str:
        """Discards the cached token and fetches a new one; used by the 401 retry path."""
        with self._token_lock:
            self._cached_token = None
            return self._fetch_and_cache_token_locked()

    # -- Authenticated request path -----------------------------------------

    def _request(
        self,
        method: str,
        path: str,
        *,
        params: Optional[dict[str, Any]] = None,
        json_body: Optional[Any] = None,
        expect_empty: bool = False,
    ) -> Any:
        """Sends one authenticated API call, retrying exactly once with a
        forced token refresh if the first attempt gets a 401. A second 401
        after refresh means the credentials themselves are bad, not just
        the token, so it's surfaced as a normal NotifydRequestError instead
        of retrying again.
        """
        token = self._authenticated_token()
        response = self._send_once(token, method, path, params, json_body)

        if response.status_code == 401:
            token = self._force_refresh_token()
            response = self._send_once(token, method, path, params, json_body)

        if not response.ok:
            raise _request_error_from_response(response)
        if expect_empty or not response.content:
            return None
        return response.json()

    def _send_once(
        self,
        token: str,
        method: str,
        path: str,
        params: Optional[dict[str, Any]],
        json_body: Optional[Any],
    ) -> requests.Response:
        clean_params = _drop_none(params) if params else None
        return self._session.request(
            method,
            f"{self._base_url}{path}",
            params=clean_params,
            json=json_body,
            headers={"Authorization": f"Bearer {token}"},
        )

    # -- Notifications -------------------------------------------------------

    def send(
        self,
        channel_config_id: str,
        body: str,
        subject: Optional[str] = None,
        metadata: Optional[dict[str, Any]] = None,
    ) -> Notification:
        """Enqueues a single notification for async delivery. Returns
        immediately with the notification in "pending" status; poll
        get_notification for the terminal outcome."""
        payload = self._request(
            "POST",
            "/notifications/send",
            json_body=_drop_none(
                {
                    "channel_config_id": channel_config_id,
                    "subject": subject,
                    "body": body,
                    "metadata": metadata,
                }
            ),
        )
        return Notification.from_dict(payload)

    def send_multi(self, notifications: list[dict[str, Any]]) -> SendMultiResponse:
        """Enqueues notifications across up to 50 channel configs in one
        request. Partial success is expected: check the returned `errors`
        list even when the call itself doesn't raise. Each item in
        `notifications` has the same shape as send()'s keyword arguments:
        {"channel_config_id", "body", "subject"?, "metadata"?}.
        """
        payload = self._request(
            "POST",
            "/notifications/send-multi",
            json_body={"channels": [_drop_none(item) for item in notifications]},
        )
        return SendMultiResponse.from_dict(payload)

    def list_notifications(
        self,
        limit: Optional[int] = None,
        offset: Optional[int] = None,
        status: Optional[str] = None,
        channel: Optional[str] = None,
    ) -> NotificationList:
        """Returns a page of the authenticated tenant's notifications."""
        payload = self._request(
            "GET",
            "/notifications",
            params={"limit": limit, "offset": offset, "status": status, "channel": channel},
        )
        return NotificationList.from_dict(payload)

    def get_notification(self, notification_id: str) -> Notification:
        """Fetches one notification by ID."""
        payload = self._request("GET", f"/notifications/{notification_id}")
        return Notification.from_dict(payload)

    def list_attempts(self, notification_id: str) -> list[DeliveryAttempt]:
        """Returns every delivery attempt recorded for a notification, ordered by attempt number."""
        payload = self._request("GET", f"/notifications/{notification_id}/attempts")
        return [DeliveryAttempt.from_dict(item) for item in payload]

    def get_metrics(self, notification_id: str) -> DeliveryMetric:
        """Returns provider-reported engagement metrics for a delivered
        notification. Raises NotifydRequestError with status_code 404 if
        metrics haven't been collected yet."""
        payload = self._request("GET", f"/notifications/{notification_id}/metrics")
        return DeliveryMetric.from_dict(payload)

    # -- Channels --------------------------------------------------------

    def list_channels(self) -> list[ChannelConfig]:
        """Returns all channel configs belonging to the authenticated tenant."""
        payload = self._request("GET", "/channels")
        return [ChannelConfig.from_dict(item) for item in payload]

    def create_channel(
        self,
        channel: str,
        name: str,
        config: dict[str, Any],
        delivery_prefs: Optional[DeliveryPreferences] = None,
    ) -> ChannelConfig:
        """Creates a new channel config for the authenticated tenant."""
        payload = self._request(
            "POST",
            "/channels",
            json_body=_drop_none(
                {
                    "channel": channel,
                    "name": name,
                    "config": config,
                    "delivery_prefs": delivery_prefs.to_dict() if delivery_prefs else None,
                }
            ),
        )
        return ChannelConfig.from_dict(payload)

    def get_channel(self, channel_id: str) -> ChannelConfig:
        """Fetches one channel config by ID."""
        payload = self._request("GET", f"/channels/{channel_id}")
        return ChannelConfig.from_dict(payload)

    def update_channel(
        self,
        channel_id: str,
        name: Optional[str] = None,
        config: Optional[dict[str, Any]] = None,
        is_active: Optional[bool] = None,
        delivery_prefs: Optional[DeliveryPreferences] = None,
    ) -> ChannelConfig:
        """Updates a channel config. Omitted keyword arguments are left unchanged server-side."""
        payload = self._request(
            "PATCH",
            f"/channels/{channel_id}",
            json_body=_drop_none(
                {
                    "name": name,
                    "config": config,
                    "is_active": is_active,
                    "delivery_prefs": delivery_prefs.to_dict() if delivery_prefs else None,
                }
            ),
        )
        return ChannelConfig.from_dict(payload)

    def delete_channel(self, channel_id: str) -> None:
        """Deletes a channel config by ID."""
        self._request("DELETE", f"/channels/{channel_id}", expect_empty=True)

    # -- API keys ------------------------------------------------------------

    def list_api_keys(self) -> list[ApiKey]:
        """Returns all API keys for the authenticated tenant. Secret hashes are never included."""
        payload = self._request("GET", "/keys")
        return [ApiKey.from_dict(item) for item in payload]

    def create_api_key(self, label: str) -> CreateApiKeyResponse:
        """Creates a new API key. The returned api_secret is shown only in
        this response -- save it immediately."""
        payload = self._request("POST", "/keys", json_body={"label": label})
        return CreateApiKeyResponse.from_dict(payload)

    def revoke_api_key(self, key_id: str) -> None:
        """Revokes an API key by ID."""
        self._request("DELETE", f"/keys/{key_id}", expect_empty=True)

    # -- Webhooks --------------------------------------------------------

    def list_webhooks(self) -> list[WebhookEndpoint]:
        """Returns all status-webhook endpoints belonging to the authenticated tenant."""
        payload = self._request("GET", "/webhooks")
        return [WebhookEndpoint.from_dict(item) for item in payload]

    def create_webhook(self, url: str, events: list[str]) -> WebhookEndpointCreated:
        """Registers a new destination for notification.delivered /
        notification.failed status events. The returned secret is shown
        only in this response -- save it, then verify deliveries with
        verify_webhook_signature."""
        payload = self._request("POST", "/webhooks", json_body={"url": url, "events": events})
        return WebhookEndpointCreated.from_dict(payload)

    def update_webhook(
        self,
        webhook_id: str,
        url: Optional[str] = None,
        events: Optional[list[str]] = None,
        is_active: Optional[bool] = None,
    ) -> WebhookEndpoint:
        """Updates a webhook endpoint. Omitted keyword arguments are left
        unchanged server-side. Never returns the secret."""
        payload = self._request(
            "PUT",
            f"/webhooks/{webhook_id}",
            json_body=_drop_none({"url": url, "events": events, "is_active": is_active}),
        )
        return WebhookEndpoint.from_dict(payload)

    def delete_webhook(self, webhook_id: str) -> None:
        """Deletes a webhook endpoint by ID."""
        self._request("DELETE", f"/webhooks/{webhook_id}", expect_empty=True)


def _request_error_from_response(response: requests.Response) -> NotifydRequestError:
    try:
        payload = response.json()
    except ValueError:
        return NotifydRequestError(response.status_code, raw_body=response.text)
    if isinstance(payload, dict) and isinstance(payload.get("error"), str):
        return NotifydRequestError(response.status_code, code=payload["error"], body=payload)
    return NotifydRequestError(response.status_code, raw_body=response.text)
