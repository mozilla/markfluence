# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at https://mozilla.org/MPL/2.0/.

"""Tests for frontmatter quoting (read and write round-trips, no network)."""

import pytest

from markfluence.libmarkdown import (
    extract_frontmatter,
    parse_value,
    update_frontmatter_field,
)


def _value(body, key):
    return extract_frontmatter(body)[0][key]


# --- read: quotes suppress inline-comment stripping --------------------------


def test_double_quoted_value_keeps_hash():
    body = '---\ntitle: "Detect # Verify"\n---\nx\n'
    assert _value(body, "title") == "Detect # Verify"


def test_single_quoted_value_keeps_hash():
    body = "---\ntitle: 'Detect # Verify'\n---\nx\n"
    assert _value(body, "title") == "Detect # Verify"


def test_unquoted_value_still_strips_inline_comment():
    assert _value("---\ntitle: Detect # Verify\n---\nx\n", "title") == "Detect"


def test_parent_comment_form_reads_value_only():
    assert _value("---\nparent: 4  # foo.md\n---\nx\n", "parent") == "4"


def test_single_quote_escape():
    assert _value("---\ntitle: 'it''s here'\n---\nx\n", "title") == "it's here"


def test_double_quote_escapes():
    assert _value('---\ntitle: "say \\"hi\\""\n---\nx\n', "title") == 'say "hi"'


def test_unterminated_quote_falls_back_to_literal():
    assert parse_value(' "oops') == '"oops'


def test_plain_values_unaffected():
    fm = extract_frontmatter("---\npage_id: 5\nspace: ENG\n---\nx\n")[0]
    assert fm == {"page_id": "5", "space": "ENG"}


# --- write: auto-quote only when needed --------------------------------------


def _line(key, value, comment=None):
    md = update_frontmatter_field("---\nk: x\n---\nbody\n", key, value, comment=comment)
    return next(line for line in md.splitlines() if line.startswith(f"{key}:"))


def test_write_leaves_safe_values_bare():
    assert _line("title", "Hello World") == "title: Hello World"
    assert _line("page_id", "12345") == "page_id: 12345"
    assert _line("title", "a: b") == "title: a: b"


def test_write_quotes_value_with_inline_comment_marker():
    assert _line("title", "Detect # Verify") == "title: 'Detect # Verify'"


def test_write_quotes_leading_whitespace():
    assert _line("title", "  x") == "title: '  x'"


def test_write_comment_is_separate_from_value():
    assert _line("parent", "4", comment="foo.md") == "parent: 4  # foo.md"


@pytest.mark.parametrize(
    "value",
    ["Detect # Verify", "it's here", 'say "hi"', "  pad  ", "#lead", "plain", "a: b"],
)
def test_write_then_read_round_trips(value):
    md = update_frontmatter_field("---\nk: x\n---\nb\n", "k", value)
    assert extract_frontmatter(md)[0]["k"] == value


def test_parent_value_round_trips_without_the_comment():
    md = update_frontmatter_field(
        "---\nk: x\n---\nb\n", "parent", "4", comment="foo.md"
    )
    assert extract_frontmatter(md)[0]["parent"] == "4"
