"""Token exchange, refresh-on-401, and one representative call per resource group."""

from __future__ import annotations

import json

import pytest
import responses as responses_lib

from notifyd_sdk import NotifydClient, NotifydConfigError, NotifydRequestError

from .conftest import MOCK_BASE_URL, bearer_token, register_token_endpoint


def test_new_requires_credentials() -> None:
    with pytest.raises(NotifydConfigError):
        NotifydClient(api_key="", api_secret="")
    with pytest.raises(NotifydConfigError):
        NotifydClient(api_key="k", api_secret="")


@responses_lib.activate
def test_token_exchange_and_caching(notifyd_client: NotifydClient) -> None:
    register_token_endpoint(responses_lib.mock)
    seen_tokens = []

    def channels_callback(request):
        seen_tokens.append(bearer_token(request))
        return (200, {}, json.dumps([]))

    responses_lib.add_callback(
        responses_lib.GET, f"{MOCK_BASE_URL}/channels", callback=channels_callback, content_type="application/json"
    )

    notifyd_client.list_channels()
    notifyd_client.list_channels()

    assert seen_tokens == ["test-token-1", "test-token-1"]


@responses_lib.activate
def test_refresh_once_on_401(notifyd_client: NotifydClient) -> None:
    register_token_endpoint(responses_lib.mock)
    seen_tokens = []

    def channels_callback(request):
        token = bearer_token(request)
        seen_tokens.append(token)
        if token == "test-token-1":
            return (401, {}, json.dumps({"error": "TOKEN_EXPIRED"}))
        return (200, {}, json.dumps([]))

    responses_lib.add_callback(
        responses_lib.GET, f"{MOCK_BASE_URL}/channels", callback=channels_callback, content_type="application/json"
    )

    notifyd_client.list_channels()

    assert seen_tokens == ["test-token-1", "test-token-2"]


@responses_lib.activate
def test_refresh_only_once_not_looped(notifyd_client: NotifydClient) -> None:
    register_token_endpoint(responses_lib.mock)
    attempts = 0

    def channels_callback(request):
        nonlocal attempts
        attempts += 1
        return (401, {}, json.dumps({"error": "INVALID_TOKEN"}))

    responses_lib.add_callback(
        responses_lib.GET, f"{MOCK_BASE_URL}/channels", callback=channels_callback, content_type="application/json"
    )

    with pytest.raises(NotifydRequestError) as exc_info:
        notifyd_client.list_channels()

    assert exc_info.value.status_code == 401
    assert exc_info.value.code == "INVALID_TOKEN"
    assert attempts == 2


@responses_lib.activate
def test_send(notifyd_client: NotifydClient) -> None:
    register_token_endpoint(responses_lib.mock)
    responses_lib.add(
        responses_lib.POST,
        f"{MOCK_BASE_URL}/notifications/send",
        json={"id": "notif-1", "status": "pending"},
        status=202,
    )

    notification = notifyd_client.send(channel_config_id="chan-1", body="hello")

    assert notification.id == "notif-1"
    assert notification.status == "pending"


@responses_lib.activate
def test_send_multi(notifyd_client: NotifydClient) -> None:
    register_token_endpoint(responses_lib.mock)
    responses_lib.add(
        responses_lib.POST,
        f"{MOCK_BASE_URL}/notifications/send-multi",
        json={"sent": [{"id": "notif-1"}], "errors": ["chan-2: CHANNEL_NOT_FOUND"]},
        status=202,
    )

    result = notifyd_client.send_multi(
        [
            {"channel_config_id": "chan-1", "body": "a"},
            {"channel_config_id": "chan-2", "body": "b"},
        ]
    )

    assert len(result.sent) == 1
    assert result.errors == ["chan-2: CHANNEL_NOT_FOUND"]


@responses_lib.activate
def test_list_notifications(notifyd_client: NotifydClient) -> None:
    register_token_endpoint(responses_lib.mock)
    responses_lib.add(
        responses_lib.GET,
        f"{MOCK_BASE_URL}/notifications",
        json={"data": [{"id": "notif-1", "status": "delivered"}], "total": 1, "limit": 20, "offset": 0},
        status=200,
    )

    result = notifyd_client.list_notifications(status="delivered")

    assert result.total == 1
    assert result.data[0].status == "delivered"
    sent_request = responses_lib.calls[-1].request
    assert sent_request.params["status"] == "delivered"


@responses_lib.activate
def test_get_notification(notifyd_client: NotifydClient) -> None:
    register_token_endpoint(responses_lib.mock)
    responses_lib.add(
        responses_lib.GET,
        f"{MOCK_BASE_URL}/notifications/notif-1",
        json={"id": "notif-1", "status": "delivered"},
        status=200,
    )

    notification = notifyd_client.get_notification("notif-1")

    assert notification.status == "delivered"


@responses_lib.activate
def test_list_attempts(notifyd_client: NotifydClient) -> None:
    register_token_endpoint(responses_lib.mock)
    responses_lib.add(
        responses_lib.GET,
        f"{MOCK_BASE_URL}/notifications/notif-1/attempts",
        json=[{"id": "attempt-1", "attempt_number": 1, "status": "success"}],
        status=200,
    )

    attempts = notifyd_client.list_attempts("notif-1")

    assert len(attempts) == 1
    assert attempts[0].status == "success"


@responses_lib.activate
def test_channels_crud(notifyd_client: NotifydClient) -> None:
    register_token_endpoint(responses_lib.mock)
    responses_lib.add(
        responses_lib.POST,
        f"{MOCK_BASE_URL}/channels",
        json={"id": "chan-1", "channel": "telegram", "tenant_id": "t1", "name": "ops", "config": {}, "is_active": True, "created_at": "", "updated_at": ""},
        status=201,
    )
    responses_lib.add(
        responses_lib.GET,
        f"{MOCK_BASE_URL}/channels/chan-1",
        json={"id": "chan-1", "channel": "telegram", "tenant_id": "t1", "name": "ops", "config": {}, "is_active": True, "created_at": "", "updated_at": ""},
        status=200,
    )
    responses_lib.add(
        responses_lib.PATCH,
        f"{MOCK_BASE_URL}/channels/chan-1",
        json={"id": "chan-1", "channel": "telegram", "tenant_id": "t1", "name": "ops", "config": {}, "is_active": False, "created_at": "", "updated_at": ""},
        status=200,
    )
    responses_lib.add(responses_lib.DELETE, f"{MOCK_BASE_URL}/channels/chan-1", status=204)

    created = notifyd_client.create_channel(channel="telegram", name="ops-alerts", config={"bot_token": "x", "chat_id": "y"})
    assert created.id == "chan-1"

    fetched = notifyd_client.get_channel("chan-1")
    assert fetched.id == "chan-1"

    updated = notifyd_client.update_channel("chan-1", is_active=False)
    assert updated.is_active is False

    notifyd_client.delete_channel("chan-1")


@responses_lib.activate
def test_keys_crud(notifyd_client: NotifydClient) -> None:
    register_token_endpoint(responses_lib.mock)
    responses_lib.add(
        responses_lib.GET,
        f"{MOCK_BASE_URL}/keys",
        json=[{"id": "key-1", "tenant_id": "t1", "api_key": "k1", "label": "ci", "created_at": "", "revoked_at": None}],
        status=200,
    )
    responses_lib.add(
        responses_lib.POST,
        f"{MOCK_BASE_URL}/keys",
        json={"key": {"id": "key-2", "tenant_id": "t1", "api_key": "k2", "label": "new", "created_at": "", "revoked_at": None}, "api_secret": "shown-once"},
        status=201,
    )
    responses_lib.add(responses_lib.DELETE, f"{MOCK_BASE_URL}/keys/key-1", status=204)

    keys = notifyd_client.list_api_keys()
    assert len(keys) == 1

    created = notifyd_client.create_api_key("new")
    assert created.api_secret == "shown-once"

    notifyd_client.revoke_api_key("key-1")


@responses_lib.activate
def test_webhooks_crud(notifyd_client: NotifydClient) -> None:
    register_token_endpoint(responses_lib.mock)
    responses_lib.add(
        responses_lib.GET,
        f"{MOCK_BASE_URL}/webhooks",
        json=[{"id": "wh-1", "tenant_id": "t1", "url": "https://example.com", "events": [], "is_active": True, "created_at": ""}],
        status=200,
    )
    responses_lib.add(
        responses_lib.POST,
        f"{MOCK_BASE_URL}/webhooks",
        json={
            "id": "wh-2",
            "tenant_id": "t1",
            "url": "https://example.com/hook",
            "events": ["notification.delivered"],
            "is_active": True,
            "created_at": "",
            "secret": "whsec_shown_once",
        },
        status=201,
    )
    responses_lib.add(
        responses_lib.PUT,
        f"{MOCK_BASE_URL}/webhooks/wh-1",
        json={"id": "wh-1", "tenant_id": "t1", "url": "https://example.com", "events": [], "is_active": False, "created_at": ""},
        status=200,
    )
    responses_lib.add(responses_lib.DELETE, f"{MOCK_BASE_URL}/webhooks/wh-1", status=204)

    webhooks = notifyd_client.list_webhooks()
    assert len(webhooks) == 1

    created = notifyd_client.create_webhook(url="https://example.com/hook", events=["notification.delivered"])
    assert created.secret == "whsec_shown_once"

    updated = notifyd_client.update_webhook("wh-1", is_active=False)
    assert updated.is_active is False

    notifyd_client.delete_webhook("wh-1")
