"""N concurrent threads through the token path must produce exactly ONE
/auth/token exchange -- proves the _token_lock hold-lock-across-I/O design
documented in NotifydClient._authenticated_token.
"""

from __future__ import annotations

import json
import threading
import time
from typing import Any

import responses as responses_lib

from notifyd_sdk import NotifydClient

from .conftest import MOCK_BASE_URL


@responses_lib.activate
def test_concurrent_threads_share_one_token_exchange() -> None:
    client = NotifydClient(api_key="test-key", api_secret="test-secret", base_url=MOCK_BASE_URL)

    token_call_count = 0
    token_call_count_lock = threading.Lock()

    def token_callback(request: Any) -> tuple[int, dict[str, str], str]:
        nonlocal token_call_count
        with token_call_count_lock:
            token_call_count += 1
            this_call_number = token_call_count
        # Widens the race window: without correct serialization, every
        # thread would observe an empty cache and start its own token
        # exchange before the first one completes.
        time.sleep(0.01)
        payload = {"token": f"test-token-{this_call_number}", "expires_in": "24h0m0s"}
        return (200, {}, json.dumps(payload))

    responses_lib.add_callback(
        responses_lib.POST,
        f"{MOCK_BASE_URL}/auth/token",
        callback=token_callback,
        content_type="application/json",
    )
    responses_lib.add(
        responses_lib.GET,
        f"{MOCK_BASE_URL}/channels",
        json=[],
        status=200,
    )

    errors: list[BaseException] = []
    errors_lock = threading.Lock()

    def call_list_channels() -> None:
        try:
            client.list_channels()
        except BaseException as exc:  # noqa: BLE001 -- surfacing to the main thread for assertion
            with errors_lock:
                errors.append(exc)

    concurrent_callers = 50
    threads = [threading.Thread(target=call_list_channels) for _ in range(concurrent_callers)]
    for thread in threads:
        thread.start()
    for thread in threads:
        thread.join()

    assert not errors, f"list_channels raised in {len(errors)} thread(s): {errors}"
    assert token_call_count == 1, f"got {token_call_count} /auth/token exchanges, want exactly 1"
