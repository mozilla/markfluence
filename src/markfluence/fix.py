# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at https://mozilla.org/MPL/2.0/.

"""The ``fix`` subcommand: reconcile frontmatter coordinates to the live page.

Given a markdown file, ``fix`` locates its live Confluence page and rewrites the
frontmatter so ``page_id``, ``space`` (key), ``parent`` (``null`` or a numeric
page id), and ``page_width`` (narrow/wide/max) match reality. A missing ``title``
is filled in from the live page; a present ``title`` is never touched (it may be a
pending rename that ``update`` will push).

The page is located by ``page_id`` (fetched directly) or, if absent, by searching
for the frontmatter ``title``. A ``page_id`` that no longer resolves is an error,
not a cue to fall back to a title search -- an explicit id asserts identity and we
never silently rebind to a same-titled page.

``fix`` is read-only on the server: it never creates, updates, or moves pages. It
writes a file only when a field actually changed, so a no-op ``fix`` leaves mtime
alone (and thus doesn't trick ``update`` into re-publishing).

When multiple files are passed, each is processed independently; the command exits
non-zero if any file failed.
"""

import sys

import click
import httpx2

from .libclient import ConfluenceClient
from .libmarkdown import (
    MarkdownFile,
    extract_space_key,
    update_frontmatter_field,
)
from .pagewidth import read_page_width


class _FixError(Exception):
    """A single file could not be reconciled."""


def _norm(value):
    """Normalize a frontmatter/live value for comparison.

    ``None``, an empty/whitespace-only string, and the literal string ``"null"``
    all mean "no value"; everything else is compared as its stripped string.
    """
    if value is None:
        return None
    text = str(value).strip()
    if text in ("", "null"):
        return None
    return text


def _locate_page(filename, frontmatter, client):
    """Find the live page for one file. Returns the v2 page dict.

    Raises :class:`_FixError` when the page can't be located.
    """
    page_id = frontmatter.get("page_id")
    if _norm(page_id) is not None:
        page = client.get_page_or_none(page_id)
        if page is None:
            raise _FixError(
                f"page_id {page_id} not found (deleted, trashed, or wrong); "
                f"remove it to search by title, or correct it"
            )
        return page

    title = frontmatter.get("title")
    if not title:
        raise _FixError(
            "no page_id or title in frontmatter; add one so the page can be located"
        )

    matches = client.search_pages_by_title(title)
    if len(matches) == 0:
        raise _FixError(f"no Confluence page found with title {title!r}")
    if len(matches) > 1:
        lines = [f"found {len(matches)} pages with title {title!r}:"]
        for m in matches:
            url = f"{client.base_url}/wiki/pages/viewpage.action?pageId={m['id']}"
            lines.append(f"  - {m['id']}: {m['title']} ({url})")
        lines.append("add a page_id to the frontmatter to disambiguate")
        raise _FixError("\n".join(lines))
    return client.get_page(matches[0]["id"])


def _planned_changes(frontmatter, page, live_width):
    """Compute the field changes needed to reconcile ``frontmatter`` to ``page``.

    Returns a list of ``(field, old_display, new_value)`` where ``new_value`` is
    what to write via :func:`update_frontmatter_field` and ``old_display`` is a
    human-readable rendering of the current value. Only fields that actually
    differ are included. ``live_width`` is the page's current width (vocabulary),
    or ``None`` to skip width reconciliation.
    """
    links = page.get("_links", {})
    live = {
        "page_id": str(page["id"]),
        "space": extract_space_key(links.get("webui", "")),
        "parent": _norm(page.get("parentId")) or "null",
    }

    changes = []
    for field, new_value in live.items():
        if new_value is None:
            # space key couldn't be derived; skip rather than write a bad value.
            continue
        current = frontmatter.get(field)
        if field not in frontmatter or str(current).strip() == "":
            # Unset (absent or blank) -> fill with the live value, including a
            # top-level "null".
            changes.append((field, "(none)", new_value))
        elif _norm(current) != _norm(new_value):
            # Present but disagrees with the live page -> rewrite. A present
            # "null" and a live top-level page are equal, so left untouched.
            changes.append((field, current, new_value))

    # title: fill only when unset; never overwrite a present (possibly renamed) one.
    if not str(frontmatter.get("title") or "").strip():
        changes.append(("title", "(none)", page["title"]))

    # page_width: compare effective widths (an unset/blank page_width means the
    # max default), and write the live value when they differ. An all-max page
    # thus stays free of an explicit page_width line.
    if live_width is not None:
        raw = frontmatter.get("page_width")
        declared = (str(raw).strip().lower() if raw is not None else "") or "max"
        if declared != live_width:
            old_display = (
                raw if (raw is not None and str(raw).strip() != "") else "(none)"
            )
            changes.append(("page_width", old_display, live_width))

    return changes


def process_file(filename, client, dry_run):
    """Reconcile a single file's frontmatter. Returns True on success."""
    prefix = f"[{filename}]"

    mdfile = MarkdownFile.from_path(filename)

    page = _locate_page(filename, mdfile.frontmatter, client)

    # Read the live width to reconcile page_width. Best-effort: if the property
    # read fails, warn and skip the width field rather than failing the file.
    try:
        live_width, _explicit = read_page_width(client, page["id"])
    except httpx2.HTTPError as exc:
        click.echo(f"{prefix} warning: could not read page width: {exc}", err=True)
        live_width = None

    changes = _planned_changes(mdfile.frontmatter, page, live_width)

    if not changes:
        click.echo(f"{prefix} already consistent")
        return True

    for field, old_display, new_value in changes:
        verb = "would set" if dry_run else "set"
        click.echo(f"{prefix} {verb} {field}: {old_display} -> {new_value}")

    if dry_run:
        return True

    content = mdfile.content
    for field, _old, new_value in changes:
        content = update_frontmatter_field(content, field, new_value)
    with open(filename, "w") as f:
        f.write(content)

    return True


@click.command()
@click.argument("filenames", nargs=-1, required=True)
@click.option(
    "--dry-run",
    is_flag=True,
    help="Report the changes fix would make without writing any files.",
)
def fix(filenames, dry_run):
    """Reconcile each markdown file's frontmatter to its live Confluence page.

    Populates/refreshes page_id, space, parent, and page_width (and fills a
    missing title) from the live page. Each file is processed independently; the
    command exits non-zero if any file failed.
    """
    client = ConfluenceClient.from_env()

    failures = 0
    for filename in filenames:
        try:
            ok = process_file(filename, client, dry_run)
        except Exception as exc:
            click.echo(f"[{filename}] Error: {exc}", err=True)
            ok = False
        if not ok:
            failures += 1

    if failures > 0:
        click.echo(f"\n{failures} of {len(filenames)} file(s) failed.", err=True)
        sys.exit(1)
