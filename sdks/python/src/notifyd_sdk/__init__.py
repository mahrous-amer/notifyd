"""Official Python client for the notifyd notification delivery API."""

from .client import DEFAULT_BASE_URL, NotifydClient
from .errors import NotifydConfigError, NotifydRequestError
from .signature import verify_webhook_signature
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
)

__all__ = [
    "NotifydClient",
    "DEFAULT_BASE_URL",
    "NotifydConfigError",
    "NotifydRequestError",
    "verify_webhook_signature",
    "ApiKey",
    "ChannelConfig",
    "CreateApiKeyResponse",
    "DeliveryAttempt",
    "DeliveryMetric",
    "DeliveryPreferences",
    "Notification",
    "NotificationList",
    "SendMultiResponse",
    "WebhookEndpoint",
    "WebhookEndpointCreated",
]
