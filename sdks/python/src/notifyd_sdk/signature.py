"""Webhook signature verification for notifyd deliveries."""

from __future__ import annotations

import hashlib
import hmac
from typing import Union

_SIGNATURE_PREFIX = "sha256="


def verify_webhook_signature(
    secret: str,
    timestamp: str,
    body: Union[str, bytes],
    signature_header: str,
) -> bool:
    """Checks a notifyd webhook delivery's signature header against the
    request body, using the same scheme notifyd uses to sign both the
    "webhook" channel type and status-webhook deliveries:

        HMAC-SHA256(secret, timestamp + "." + body), hex-encoded, carried
        in a header formatted as "sha256=<hex>".

    Args:
        secret: The signing secret shown once at endpoint creation.
        timestamp: The raw value of the X-Notifyd-Timestamp header.
        body: The exact raw request body bytes (or str). Parse JSON only
            after verifying -- re-serializing and re-signing will not
            reproduce the original signature.
        signature_header: The raw value of the X-Notifyd-Signature header.

    Returns:
        True if the signature is valid, False otherwise.

    This function does not check timestamp freshness -- callers who want
    replay protection should separately reject requests whose timestamp is
    older than an acceptable window before calling this.
    """
    if not signature_header.startswith(_SIGNATURE_PREFIX):
        return False
    provided_hex = signature_header[len(_SIGNATURE_PREFIX) :]

    try:
        provided_bytes = bytes.fromhex(provided_hex)
    except ValueError:
        return False

    body_bytes = body.encode("utf-8") if isinstance(body, str) else body

    mac = hmac.new(secret.encode("utf-8"), digestmod=hashlib.sha256)
    mac.update(timestamp.encode("utf-8"))
    mac.update(b".")
    mac.update(body_bytes)
    expected_bytes = mac.digest()

    return hmac.compare_digest(provided_bytes, expected_bytes)
