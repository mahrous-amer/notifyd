"""Shared test fixtures: a notifyd client wired against `responses`-mocked HTTP."""

from __future__ import annotations

import json as json_module
import re
from typing import Any, Callable

import pytest
import responses as responses_lib

from notifyd_sdk import NotifydClient

MOCK_BASE_URL = "https://mock.invalid/api"


@pytest.fixture
def notifyd_client() -> NotifydClient:
    return NotifydClient(api_key="test-key", api_secret="test-secret", base_url=MOCK_BASE_URL)


def register_token_endpoint(
    rsps: responses_lib.RequestsMock,
    *,
    valid_api_key: str = "test-key",
    valid_api_secret: str = "test-secret",
) -> Callable[[], int]:
    """Registers POST /auth/token, issuing "test-token-<n>" on each call
    (n increments per call) so tests can distinguish a cached token from a
    freshly-issued one. Returns a callable reporting the current call count.
    """
    call_count = 0

    def callback(request: Any) -> tuple[int, dict[str, str], str]:
        nonlocal call_count
        call_count += 1
        body = json_module.loads(request.body)
        if body.get("api_key") != valid_api_key or body.get("api_secret") != valid_api_secret:
            return (401, {}, json_module.dumps({"error": "INVALID_CREDENTIALS"}))
        payload = {"token": f"test-token-{call_count}", "expires_in": "24h0m0s"}
        return (200, {}, json_module.dumps(payload))

    rsps.add_callback(
        responses_lib.POST,
        f"{MOCK_BASE_URL}/auth/token",
        callback=callback,
        content_type="application/json",
    )
    return lambda: call_count


def bearer_token(request: Any) -> str:
    auth_header = request.headers.get("Authorization", "")
    return auth_header.removeprefix("Bearer ")
