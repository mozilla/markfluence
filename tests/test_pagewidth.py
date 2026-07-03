# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at https://mozilla.org/MPL/2.0/.

"""Tests for the page-width vocabulary and content-property logic (no network)."""

import httpx2
import pytest

from markfluence import pagewidth as pw
from markfluence.libclient import ConfluenceClient

# --- declared_width: normalize / default / validate --------------------------


def test_declared_width_defaults_to_max_when_unset_or_blank():
    assert pw.declared_width({}) == "max"
    assert pw.declared_width({"page_width": ""}) == "max"
    assert pw.declared_width({"page_width": "   "}) == "max"


def test_declared_width_normalizes_case_and_whitespace():
    assert pw.declared_width({"page_width": "Wide"}) == "wide"
    assert pw.declared_width({"page_width": "  MAX "}) == "max"


def test_declared_width_rejects_unknown_value():
    with pytest.raises(ValueError, match="invalid page_width"):
        pw.declared_width({"page_width": "mx"})


# --- fake HTTP layer ---------------------------------------------------------


class _Resp:
    def __init__(self, js=None, boom=False):
        self._js = js or {}
        self._boom = boom

    def json(self):
        return self._js

    def raise_for_status(self):
        if self._boom:
            raise httpx2.HTTPError("boom")


class _FakeHTTP:
    """Serves scripted responses in order and records the calls made."""

    def __init__(self, scripted):
        self._scripted = list(scripted)
        self.calls = []

    def get(self, url, params=None, **kw):
        self.calls.append("GET")
        return self._scripted.pop(0)

    def put(self, url, **kw):
        self.calls.append("PUT")
        return self._scripted.pop(0)

    def post(self, url, **kw):
        self.calls.append("POST")
        return self._scripted.pop(0)


def _client(scripted):
    client = ConfluenceClient.__new__(ConfluenceClient)
    client.base_url = "https://example.atlassian.net"
    client._client = _FakeHTTP(scripted)
    return client


def _none():
    """A properties GET returning no match."""
    return _Resp({"results": []})


def _prop(value, version=2):
    """A properties GET returning one existing property."""
    prop = {"id": "p", "value": value, "version": {"number": version}}
    return _Resp({"results": [prop]})


# --- set_content_property: create / update / skip / retry --------------------


def test_set_content_property_creates_when_absent():
    client = _client([_none(), _Resp({})])
    assert client.set_content_property("1", "k", "max") == "set"
    assert client._client.calls == ["GET", "POST"]


def test_set_content_property_skips_when_already_equal():
    client = _client([_prop("max")])
    assert client.set_content_property("1", "k", "max") == "unchanged"
    assert client._client.calls == ["GET"]  # no write


def test_set_content_property_updates_when_different():
    client = _client([_prop("default"), _Resp({})])
    assert client.set_content_property("1", "k", "max") == "set"
    assert client._client.calls == ["GET", "PUT"]


def test_list_content_properties_follows_pagination():
    page1 = _Resp(
        {
            "results": [{"key": "a"}],
            "_links": {"next": "/wiki/api/v2/pages/1/properties?cursor=X"},
        }
    )
    page2 = _Resp({"results": [{"key": "b"}], "_links": {}})
    client = _client([page1, page2])
    props = client.list_content_properties("1")
    assert [p["key"] for p in props] == ["a", "b"]
    assert client._client.calls == ["GET", "GET"]


def test_set_content_property_retries_once_and_detects_applied_write():
    # First GET throws; the retry re-reads and finds the value already applied.
    client = _client([_Resp(boom=True), _prop("max")])
    assert client.set_content_property("1", "k", "max") == "unchanged"
    assert client._client.calls == ["GET", "GET"]


# --- apply / read page width -------------------------------------------------


def test_apply_page_width_sets_both_appearance_properties():
    client = _client([_none(), _Resp({}), _none(), _Resp({})])
    actions = pw.apply_page_width(client, "1", "wide")
    assert [key for key, _ in actions] == [pw.PUBLISHED_KEY, pw.DRAFT_KEY]


def test_read_page_width_reverse_maps_and_flags_unset():
    assert pw.read_page_width(_client([_none()]), "1") == ("narrow", False)
    assert pw.read_page_width(_client([_prop("full-width")]), "1") == ("wide", True)
    assert pw.read_page_width(_client([_prop("max")]), "1") == ("max", True)


def test_width_from_properties_reverse_maps_and_flags_unset():
    props = [
        {"key": pw.PUBLISHED_KEY, "value": "full-width"},
        {"key": "editor", "value": "v2"},
    ]
    assert pw.width_from_properties(props) == ("wide", True)
    assert pw.width_from_properties([{"key": "editor", "value": "v2"}]) == (
        "narrow",
        False,
    )
