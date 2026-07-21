# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at https://mozilla.org/MPL/2.0/.

"""Tests for raw Confluence storage-format passthrough (shield/unshield)."""

from markfluence.libmarkdown import MarkdownFile, _shield_storage, md_to_confluence


def _convert(md, tmp_path):
    # md_to_confluence takes a MarkdownFile; write the body to a real file in
    # tmp_path so sibling-doc/image resolution has a directory to scan.
    f = tmp_path / "page.md"
    f.write_text(md)
    page = md_to_confluence(
        MarkdownFile.from_path(str(f)), "https://ex.atlassian.net", "ENG"
    )
    return page.html


# --- sentinel scheme ---------------------------------------------------------


def test_sentinel_avoids_collision_by_growing():
    # Source literally contains the base sentinel, so it must grow.
    md = "prefix MFAC and MFAF and an <ac:tag/>"
    shielded, _unshield = _shield_storage(md)
    # "MFAC" is in the text, so the ac sentinel grew past it and the literal
    # "MFAC" in the prose is left untouched by the shield.
    assert "ac:" not in shielded
    assert "MFAC and MFAF" in shielded  # original prose preserved


def test_shield_round_trips_prose_and_links():
    md = "Talk about ac: and ri: and a [link](fileac:.md)."
    shielded, unshield = _shield_storage(md)
    assert unshield(shielded) == md  # transparent round-trip


# --- passthrough via md_to_confluence ---------------------------------------


def test_raw_macro_passes_through(tmp_path):
    macro = '<ac:structured-macro ac:name="status" ac:schema-version="1"/>'
    html = _convert(f"Before.\n\n{macro}\n\nAfter.", tmp_path)
    assert macro in html
    assert "MFAC" not in html  # sentinel fully restored


def test_layout_cells_convert_markdown_between_tags(tmp_path):
    md = (
        "<ac:layout>\n"
        '<ac:layout-section ac:type="two_equal">\n'
        "<ac:layout-cell>\n\n"
        "Left with **bold** and a [x](https://x.com).\n\n"
        "</ac:layout-cell>\n"
        "<ac:layout-cell>\n\n"
        "Right column.\n\n"
        "</ac:layout-cell>\n"
        "</ac:layout-section>\n"
        "</ac:layout>"
    )
    html = _convert(md, tmp_path)
    assert '<ac:layout-section ac:type="two_equal">' in html
    assert "<strong>bold</strong>" in html
    assert '<a href="https://x.com">x</a>' in html


def test_storage_in_code_fence_stays_literal(tmp_path):
    md = 'Example:\n\n```\n<ac:structured-macro ac:name="info"/>\n```\n'
    html = _convert(md, tmp_path)
    # The fence becomes a code macro; the storage example sits literally inside
    # its plain-text-body CDATA rather than being emitted as a live macro.
    assert '<![CDATA[<ac:structured-macro ac:name="info"/>]]>' in html
