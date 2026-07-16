"""_parse_go_duration_seconds: parses Go's time.Duration.String() output."""

from __future__ import annotations

import pytest

from notifyd_sdk.client import _parse_go_duration_seconds

VALID_CASES = [
    ("hours, minutes, seconds", "24h0m0s", 24 * 3600),
    ("hours and minutes", "1h30m", 3600 + 30 * 60),
    ("seconds only", "45s", 45),
    ("milliseconds", "500ms", 0.5),
    ("microseconds (us spelling)", "500us", 0.0005),
    ("microseconds (µs, MICRO SIGN)", "1.5µs", 0.0000015),
    ("microseconds (μs, GREEK MU)", "1.5μs", 0.0000015),
    ("nanoseconds", "1000000ns", 0.001),
    ("zero", "0s", 0),
    (
        "every unit combined",
        "1h2m3s4ms5us6ns",
        3600 + 120 + 3 + 0.004 + 0.000005 + 0.000000006,
    ),
]


@pytest.mark.parametrize("label,duration,expected_seconds", VALID_CASES, ids=[c[0] for c in VALID_CASES])
def test_parses_valid_durations(label: str, duration: str, expected_seconds: float) -> None:
    assert _parse_go_duration_seconds(duration) == pytest.approx(expected_seconds, abs=1e-9)


REJECTED_CASES = [
    ("empty string", ""),
    ("no units at all", "not-a-duration"),
    ("garbage before a valid duration", "garbage 45s"),
    ("garbage after a valid duration", "45s garbage"),
    ("negative duration", "-5m"),
    ("negative with other units", "-1h30m"),
]


@pytest.mark.parametrize("label,duration", REJECTED_CASES, ids=[c[0] for c in REJECTED_CASES])
def test_rejects_invalid_durations(label: str, duration: str) -> None:
    with pytest.raises(ValueError):
        _parse_go_duration_seconds(duration)
