# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at https://mozilla.org/MPL/2.0/.

"""Tests for info's content-property rendering (no network)."""

from markfluence.info import _properties_section, _render_value


def test_render_value_strings_and_json():
    assert _render_value("max") == "max"
    assert _render_value({"version": "v2"}) == '{"version":"v2"}'
    assert _render_value(3) == "3"


def test_render_value_truncates_long_values():
    rendered = _render_value("x" * 500)
    assert len(rendered) == 100
    assert rendered.endswith("…")


def test_properties_section_sorts_by_key():
    props = [
        {"key": "editor", "value": "v2"},
        {"key": "content-appearance-published", "value": "max"},
    ]
    assert _properties_section(props, None) == (
        "content properties:\n  content-appearance-published: max\n  editor: v2"
    )


def test_properties_section_empty():
    assert _properties_section([], None) == "content properties: (none)"


def test_properties_section_fetch_error():
    assert _properties_section(None, "boom") == (
        "content properties: (could not fetch: boom)"
    )
