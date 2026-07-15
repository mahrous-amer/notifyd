"""Typed request/response shapes mirroring the notifyd OpenAPI schemas.

These are plain dataclasses built with `from_dict`/`to_dict` rather than a
validation library, keeping the dependency footprint at just `requests`.
Field names match the API's JSON keys (snake_case) directly, since that's
already idiomatic Python.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any, Literal, Optional

ChannelType = Literal["discord", "telegram", "whatsapp", "email", "slack", "webhook"]
NotificationStatus = Literal["pending", "processing", "delivered", "failed", "retrying"]
AttemptStatus = Literal["success", "failure"]
FormatMode = Literal["plain", "markdown", "html"]
WebhookEventType = Literal["notification.delivered", "notification.failed"]


@dataclass
class DeliveryPreferences:
    priority: Optional[str] = None
    max_retries: Optional[int] = None
    format_mode: Optional[str] = None

    def to_dict(self) -> dict[str, Any]:
        return _drop_none(
            {
                "priority": self.priority,
                "max_retries": self.max_retries,
                "format_mode": self.format_mode,
            }
        )

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "DeliveryPreferences":
        return cls(
            priority=data.get("priority"),
            max_retries=data.get("max_retries"),
            format_mode=data.get("format_mode"),
        )


@dataclass
class ChannelConfig:
    id: str
    tenant_id: str
    channel: ChannelType
    name: str
    config: dict[str, Any]
    is_active: bool
    delivery_prefs: Optional[DeliveryPreferences]
    created_at: str
    updated_at: str

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "ChannelConfig":
        prefs = data.get("delivery_prefs")
        return cls(
            id=data["id"],
            tenant_id=data["tenant_id"],
            channel=data["channel"],
            name=data["name"],
            config=data.get("config", {}),
            is_active=data.get("is_active", False),
            delivery_prefs=DeliveryPreferences.from_dict(prefs) if prefs else None,
            created_at=data["created_at"],
            updated_at=data["updated_at"],
        )


@dataclass
class Notification:
    id: str
    tenant_id: str
    channel_config_id: str
    channel: ChannelType
    subject: Optional[str]
    body: str
    status: NotificationStatus
    retry_count: int
    max_retries: int
    last_error: Optional[str]
    delivered_at: Optional[str]
    provider_msg_id: Optional[str]
    created_at: str
    updated_at: str
    metadata: dict[str, Any] = field(default_factory=dict)

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "Notification":
        return cls(
            id=data["id"],
            tenant_id=data.get("tenant_id", ""),
            channel_config_id=data.get("channel_config_id", ""),
            channel=data.get("channel", ""),
            subject=data.get("subject"),
            body=data.get("body", ""),
            status=data.get("status", "pending"),
            retry_count=data.get("retry_count", 0),
            max_retries=data.get("max_retries", 0),
            last_error=data.get("last_error"),
            delivered_at=data.get("delivered_at"),
            provider_msg_id=data.get("provider_msg_id"),
            created_at=data.get("created_at", ""),
            updated_at=data.get("updated_at", ""),
            metadata=data.get("metadata") or {},
        )


@dataclass
class SendMultiResponse:
    sent: list[Notification]
    errors: list[str]

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "SendMultiResponse":
        return cls(
            sent=[Notification.from_dict(item) for item in data.get("sent", [])],
            errors=list(data.get("errors", [])),
        )


@dataclass
class NotificationList:
    data: list[Notification]
    total: int
    limit: int
    offset: int

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "NotificationList":
        return cls(
            data=[Notification.from_dict(item) for item in data.get("data", [])],
            total=data.get("total", 0),
            limit=data.get("limit", 0),
            offset=data.get("offset", 0),
        )


@dataclass
class DeliveryAttempt:
    id: str
    notification_id: str
    attempt_number: int
    status: AttemptStatus
    error_message: Optional[str]
    duration_ms: int
    attempted_at: str
    provider_response: dict[str, Any] = field(default_factory=dict)

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "DeliveryAttempt":
        return cls(
            id=data["id"],
            notification_id=data.get("notification_id", ""),
            attempt_number=data.get("attempt_number", 0),
            status=data.get("status", "failure"),
            error_message=data.get("error_message"),
            duration_ms=data.get("duration_ms", 0),
            attempted_at=data.get("attempted_at", ""),
            provider_response=data.get("provider_response") or {},
        )


@dataclass
class DeliveryMetric:
    id: str
    notification_id: str
    provider_msg_id: str
    delivered_at: Optional[str]
    read_at: Optional[str]
    collected_at: str
    interactions: dict[str, Any] = field(default_factory=dict)

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "DeliveryMetric":
        return cls(
            id=data["id"],
            notification_id=data.get("notification_id", ""),
            provider_msg_id=data.get("provider_msg_id", ""),
            delivered_at=data.get("delivered_at"),
            read_at=data.get("read_at"),
            collected_at=data.get("collected_at", ""),
            interactions=data.get("interactions") or {},
        )


@dataclass
class ApiKey:
    id: str
    tenant_id: str
    api_key: str
    label: str
    created_at: str
    revoked_at: Optional[str]

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "ApiKey":
        return cls(
            id=data["id"],
            tenant_id=data.get("tenant_id", ""),
            api_key=data.get("api_key", ""),
            label=data.get("label", ""),
            created_at=data.get("created_at", ""),
            revoked_at=data.get("revoked_at"),
        )


@dataclass
class CreateApiKeyResponse:
    key: ApiKey
    api_secret: str

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "CreateApiKeyResponse":
        return cls(key=ApiKey.from_dict(data["key"]), api_secret=data["api_secret"])


@dataclass
class WebhookEndpoint:
    id: str
    tenant_id: str
    url: str
    events: list[WebhookEventType]
    is_active: bool
    created_at: str

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "WebhookEndpoint":
        return cls(
            id=data["id"],
            tenant_id=data.get("tenant_id", ""),
            url=data.get("url", ""),
            events=list(data.get("events", [])),
            is_active=data.get("is_active", False),
            created_at=data.get("created_at", ""),
        )


@dataclass
class WebhookEndpointCreated(WebhookEndpoint):
    secret: str = ""

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "WebhookEndpointCreated":
        base = WebhookEndpoint.from_dict(data)
        return cls(**vars(base), secret=data["secret"])


def _drop_none(values: dict[str, Any]) -> dict[str, Any]:
    """Removes keys whose value is None, so partial-update request bodies
    only send fields the caller actually set."""
    return {key: value for key, value in values.items() if value is not None}
