"""_parse_go_duration_seconds: parses Go's time.Duration.String() output."""

from __future__ import annotations

import pytest

from notifyd_sdk.client import _parse_go_duration_seconds


def test_parses_hours_minutes_seconds() -> None:
    assert _parse_go_duration_seconds("24h0m0s") == 24 * 3600


def test_parses_a_subset_of_units() -> None:
    assert _parse_go_duration_seconds("1h30m") == 3600 + 30 * 60
    assert _parse_go_duration_seconds("45s") == 45


def test_parses_milliseconds_distinct_from_minutes() -> None:
    # Regression check: "ms" must not be captured as "m" leaving a
    # dangling "s" -- see the ordering comment on _GO_DURATION_UNIT_PATTERN.
    assert _parse_go_duration_seconds("500ms") == pytest.approx(0.5)


def test_raises_on_unparseable_string() -> None:
    with pytest.raises(ValueError):
        _parse_go_duration_seconds("not-a-duration")
