"""verify_webhook_signature against the shared cross-language fixture."""

from __future__ import annotations

import json
from pathlib import Path

import pytest

from notifyd_sdk import verify_webhook_signature

VECTORS_PATH = Path(__file__).parent.parent.parent / "testdata" / "signature_vectors.json"


def load_signature_vectors() -> list[dict]:
    vectors = json.loads(VECTORS_PATH.read_text())
    assert vectors, "signature_vectors.json is empty"
    return vectors


@pytest.mark.parametrize("vector", load_signature_vectors(), ids=lambda v: v["name"])
def test_verify_webhook_signature_shared_vectors(vector: dict) -> None:
    assert verify_webhook_signature(vector["secret"], vector["timestamp"], vector["body"], vector["header_value"])


def test_verify_webhook_signature_rejects_tampered_body() -> None:
    vector = load_signature_vectors()[0]
    assert not verify_webhook_signature(
        vector["secret"], vector["timestamp"], vector["body"] + "tampered", vector["header_value"]
    )


def test_verify_webhook_signature_rejects_wrong_secret() -> None:
    vector = load_signature_vectors()[0]
    assert not verify_webhook_signature(
        "wrong-secret", vector["timestamp"], vector["body"], vector["header_value"]
    )


@pytest.mark.parametrize(
    "build_malformed_header",
    [
        lambda signature_hex: signature_hex,  # missing "sha256=" prefix
        lambda signature_hex: f"sha1={signature_hex}",  # wrong algorithm prefix
        lambda _signature_hex: "sha256=not-hex!!",  # undecodable hex
        lambda _signature_hex: "",
    ],
)
def test_verify_webhook_signature_rejects_malformed_header(build_malformed_header) -> None:
    vector = load_signature_vectors()[0]
    header = build_malformed_header(vector["signature_hex"])
    assert not verify_webhook_signature(vector["secret"], vector["timestamp"], vector["body"], header)
