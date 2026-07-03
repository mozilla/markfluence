# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at https://mozilla.org/MPL/2.0/.

"""The ``info`` subcommand: print metadata about a single Confluence page.

The page is identified either by a numeric page id or by a markdown file whose
frontmatter carries a ``page_id``. Output is metadata only (no page body),
modeled on confluence-cli's ``info`` command.

``info`` is read-only: it never modifies Confluence or local files.
"""

import os
import sys

import click
import httpx2

from .libclient import ConfluenceClient
from .libmarkdown import extract_frontmatter, extract_space_key
from .pagewidth import read_page_width


class _InfoError(Exception):
    """The requested page could not be resolved or fetched."""


def _resolve_page_id(arg):
    """Resolve the CLI argument to a page id. Raises :class:`_InfoError`."""
    if os.path.isfile(arg):
        with open(arg) as f:
            frontmatter, _ = extract_frontmatter(f.read())
        page_id = frontmatter.get("page_id")
        if page_id is None or str(page_id).strip() == "":
            raise _InfoError(f"no page_id in frontmatter of {arg}")
        return str(page_id).strip()
    if arg.isdigit():
        return arg
    raise _InfoError(f"{arg} is not a file or a numeric page id")


def _author_name(client, account_id, cache):
    """Resolve an account id to a display name, falling back to the raw id."""
    if not account_id:
        return None
    if account_id not in cache:
        cache[account_id] = client.get_user(account_id) or account_id
    return cache[account_id]


def _format_page(page, client):
    """Build the aligned ``label: value`` lines for a v2 page dict."""
    links = page.get("_links", {})
    version = page.get("version", {})

    space_key = extract_space_key(links.get("webui", ""))
    parent_id = page.get("parentId")
    parent = parent_id if parent_id else "none (top-level)"

    webui = links.get("webui", "")
    base = links.get("base", client.base_url + "/wiki")
    url = (
        base + webui
        if webui
        else f"{client.base_url}/wiki/pages/viewpage.action?pageId={page['id']}"
    )

    name_cache = {}
    creator = _author_name(client, page.get("authorId"), name_cache)
    editor = _author_name(client, version.get("authorId"), name_cache)

    created = page.get("createdAt", "")
    updated = version.get("createdAt", "")

    # Page width is a content property (a separate call); tolerate a failure.
    try:
        width_value, width_explicit = read_page_width(client, page["id"])
        page_width = (
            width_value if width_explicit else f"{width_value} (Confluence default)"
        )
    except httpx2.HTTPError:
        page_width = "unknown"

    rows = [
        ("id", page["id"]),
        ("title", page.get("title", "")),
        ("status", page.get("status", "")),
        ("space", space_key or ""),
        ("parent", parent),
        ("version", version.get("number", "")),
        ("page_width", page_width),
        ("created", f"{created} by {creator}" if creator else created),
        ("updated", f"{updated} by {editor}" if editor else updated),
        ("message", version.get("message") or ""),
        ("url", url),
    ]
    label_width = max(len(label) for label, _ in rows) + 1
    return "\n".join(
        f"{(label + ':').ljust(label_width)} {value}"
        for label, value in rows
        if value != ""
    )


@click.command()
@click.argument("arg")
def info(arg):
    """Print metadata about a Confluence page.

    ARG is a numeric page id or a markdown file whose frontmatter has a page_id.
    """
    client = ConfluenceClient.from_env()

    try:
        page_id = _resolve_page_id(arg)
        page = client.get_page_or_none(page_id)
        if page is None:
            raise _InfoError(f"page {page_id} not found")
        click.echo(_format_page(page, client))
    except _InfoError as exc:
        click.echo(f"Error: {exc}", err=True)
        sys.exit(1)
