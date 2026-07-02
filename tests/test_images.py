# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at https://mozilla.org/MPL/2.0/.

"""Tests for the pure image-rewrite step (no network)."""

import html
import json

from mzcld_confluence_cli.libmarkdown import replace_images


def _escaped_title(obj):
    """Build a title attribute value the way marko emits it (JSON, HTML-escaped)."""
    return html.escape(json.dumps(obj), quote=True)


def test_local_image_becomes_attachment(tmp_path):
    (tmp_path / "assets").mkdir()
    (tmp_path / "assets" / "x.png").write_bytes(b"\x89PNG\r\n")

    html, attachments, broken, warnings = replace_images(
        '<p><img src="assets/x.png" alt="My shot" /></p>', str(tmp_path)
    )

    assert '<ri:attachment ri:filename="assets_x.png" />' in html
    assert 'ac:alt="My shot"' in html
    assert broken == []
    assert warnings == []
    assert len(attachments) == 1
    assert attachments[0]["filename"] == "assets_x.png"
    assert attachments[0]["path"].endswith("assets/x.png")


def test_missing_image_is_broken(tmp_path):
    html, attachments, broken, warnings = replace_images(
        '<img src="assets/nope.png" alt="x" />', str(tmp_path)
    )

    assert "IMAGE BROKEN: assets/nope.png (not found)" in html
    assert attachments == []
    assert broken == ["IMAGE BROKEN: assets/nope.png (not found)"]


def test_unsupported_extension_is_broken(tmp_path):
    (tmp_path / "notes.pdf").write_bytes(b"%PDF-1.4")

    html, attachments, broken, warnings = replace_images(
        '<img src="notes.pdf" alt="x" />', str(tmp_path)
    )

    assert "IMAGE BROKEN: notes.pdf (unsupported type)" in html
    assert attachments == []


def test_remote_image_uses_ri_url():
    html, attachments, broken, warnings = replace_images(
        '<img src="https://example.com/x.png" alt="a" />', "."
    )

    assert '<ri:url ri:value="https://example.com/x.png" />' in html
    assert attachments == []
    assert broken == []


def test_same_basename_distinct_filenames(tmp_path):
    for directory in ("a", "b"):
        (tmp_path / directory).mkdir()
        (tmp_path / directory / "logo.png").write_bytes(b"\x89PNG" + directory.encode())

    html, attachments, broken, warnings = replace_images(
        '<img src="a/logo.png" alt="" /><img src="b/logo.png" alt="" />',
        str(tmp_path),
    )

    names = sorted(a["filename"] for a in attachments)
    assert names == ["a_logo.png", "b_logo.png"]
    assert broken == []


def test_plain_title_becomes_ac_title(tmp_path):
    (tmp_path / "x.png").write_bytes(b"\x89PNG")

    # marko renders ![alt](x.png "A tooltip") as <img ... title="A tooltip">.
    html, _, _, warnings = replace_images(
        '<img src="x.png" alt="Shot" title="A tooltip" />', str(tmp_path)
    )

    assert 'ac:alt="Shot"' in html
    assert 'ac:title="A tooltip"' in html
    assert warnings == []


def test_json_title_sets_properties(tmp_path):
    (tmp_path / "x.png").write_bytes(b"\x89PNG")

    # A JSON object in the title carries title/width/height/align; marko
    # HTML-escapes the quotes, which replace_images must decode.
    title = _escaped_title({"title": "T", "width": "100", "align": "center"})
    out, _, _, warnings = replace_images(
        f'<img src="x.png" alt="Shot" title="{title}" />', str(tmp_path)
    )

    assert 'ac:alt="Shot"' in out
    assert 'ac:title="T"' in out
    assert 'ac:width="100"' in out
    assert 'ac:align="center"' in out
    assert warnings == []


def test_json_title_invalid_values_warn(tmp_path):
    (tmp_path / "x.png").write_bytes(b"\x89PNG")

    title = _escaped_title({"width": "wide", "align": "middle"})
    out, _, _, warnings = replace_images(
        f'<img src="x.png" alt="" title="{title}" />', str(tmp_path)
    )

    assert "ac:width" not in out
    assert "ac:align" not in out
    assert len(warnings) == 2
