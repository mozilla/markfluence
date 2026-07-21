# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at https://mozilla.org/MPL/2.0/.

"""The ``update`` subcommand: publish markdown files to Confluence pages.

Both the page title and the page ID are read from each markdown file's YAML
frontmatter::

    ---
    title: My Page Title
    page_id: 1234567890
    ---

If ``title:`` is missing, the file is skipped with an error. If ``page_id:`` is
missing, we search Confluence for a page matching ``title:``; on a single match
we write the page ID back to the frontmatter so subsequent runs skip the search;
on zero or multiple matches the file is skipped.

When multiple files are passed, each is processed independently -- failures on
one file don't stop the others. The command exits non-zero if any file failed.

The markdown-to-Confluence-storage conversion lives in
:mod:`markfluence.libmarkdown`; HTTP calls go through
:class:`markfluence.libclient.ConfluenceClient`.
"""

import datetime
import os
import sys

import click

from .libclient import ConfluenceClient
from .libmarkdown import (
    MarkdownFile,
    extract_space_key,
    md_to_confluence,
    update_frontmatter_field,
)
from .pagewidth import declared_width, set_page_width


def process_file(filename, client, message, force):
    """Publish a single markdown file. Returns True on success."""
    prefix = f"[{filename}]"

    # Read and parse the markdown once.
    mdfile = MarkdownFile.from_path(filename)

    # The page title must come from the frontmatter.
    page_title = mdfile.title
    if not page_title:
        click.echo(
            f"{prefix} Error: no 'title' field found in frontmatter.\n"
            f"{prefix} Add a 'title: <Page Title>' line to the YAML frontmatter at "
            f"the top of the file.",
            err=True,
        )
        return False

    # Validate page_width up front (unset/blank -> the max default). A typo'd
    # value is surfaced even on files that would otherwise be mtime-skipped.
    try:
        page_width = declared_width(mdfile.frontmatter)
    except ValueError as exc:
        click.echo(f"{prefix} Error: {exc}", err=True)
        return False

    # Resolve page ID from frontmatter; if absent, search by title and write back.
    page_id = mdfile.page_id
    if not page_id:
        click.echo(f"{prefix} Searching for page titled '{page_title}'...")
        matches = client.search_pages_by_title(page_title)
        if len(matches) == 0:
            click.echo(
                f"{prefix} Error: no Confluence page found with title '{page_title}'",
                err=True,
            )
            return False
        if len(matches) > 1:
            click.echo(
                f"{prefix} Error: found {len(matches)} pages with title "
                f"'{page_title}':",
                err=True,
            )
            for m in matches:
                page_url = (
                    f"{client.base_url}/wiki/pages/viewpage.action?pageId={m['id']}"
                )
                click.echo(
                    f"{prefix}   - {m['id']}: {m['title']} ({page_url})", err=True
                )
            return False

        page_id = matches[0]["id"]
        click.echo(f"{prefix} Found page ID: {page_id}; writing to frontmatter")

        # Persist the page_id back to the markdown file's frontmatter so future
        # runs skip the title search.
        new_content = update_frontmatter_field(mdfile.content, "page_id", page_id)
        with open(filename, "w") as f:
            f.write(new_content)

    # Fetch page metadata once: we need the version for the update call, and
    # the webui link to derive the space key for rewriting internal .md links.
    page = client.get_page(page_id)
    links = page.get("_links", {})
    webui = links.get("webui", "")
    space_key = extract_space_key(webui)

    # Skip if the file hasn't been modified since the last Confluence update.
    # Both mtime (float, UTC seconds since epoch) and version.createdAt (ISO
    # 8601 with tz) are compared as UTC timestamps.
    if not force:
        version_created = page["version"].get("createdAt")
        if version_created:
            page_updated = datetime.datetime.fromisoformat(
                version_created.replace("Z", "+00:00"),
            ).timestamp()
            file_mtime = os.path.getmtime(filename)
            if file_mtime <= page_updated:
                click.echo(f"{prefix} Skipping -- no changes")
                return True

    # Convert the markdown body (frontmatter stripped) to Confluence storage HTML.
    page_content = md_to_confluence(mdfile, client.base_url, space_key)
    html_content = page_content.html
    for message in page_content.broken + page_content.warnings:
        click.echo(f"{prefix} warning: {message}", err=True)

    # Upload referenced local images as attachments (the body references them by
    # filename, so this must happen before the page renders).
    for att_name, action in client.sync_attachments(page_id, page_content.attachments):
        click.echo(f"{prefix} attachment {action}: {att_name}")

    current_version = page["version"]["number"]

    click.echo(
        f"{prefix} Updating '{page_title}' "
        f"(v{current_version} -> v{current_version + 1})..."
    )

    # Update
    result = client.update_page(
        page_id,
        page_title,
        html_content,
        current_version + 1,
        message,
    )

    links = result.get("_links", {})
    webui = links.get("webui", "")
    base = links.get("base", client.base_url + "/wiki")
    url = (
        base + webui
        if webui
        else f"{client.base_url}/wiki/pages/viewpage.action?pageId={page_id}"
    )

    # Assert the page width (a content property, so a separate call) as part of
    # the publish. Skipped files never reach here, so width isn't reasserted on
    # a no-op run. A failure here is non-fatal: the page update already landed.
    set_page_width(client, page_id, page_width, prefix)

    click.echo(f"{prefix} Published v{current_version + 1}: {url}")
    return True


@click.command()
@click.argument("filenames", nargs=-1, required=True)
@click.option(
    "--message",
    default="Updated via markfluence",
    help="Version message.",
)
@click.option(
    "--force",
    is_flag=True,
    help="Skip the file-mtime check and always update the page.",
)
def update(filenames, message, force):
    """Publish one or more markdown FILENAMES to Confluence pages.

    Title and page ID are read from each file's YAML frontmatter. Each file is
    processed independently; the command exits non-zero if any file failed.
    """
    client = ConfluenceClient.from_env()

    failures = 0
    for filename in filenames:
        try:
            ok = process_file(filename, client, message, force)
        except Exception as exc:
            click.echo(f"[{filename}] Error: {exc}", err=True)
            ok = False
        if not ok:
            failures += 1

    if failures > 0:
        click.echo(f"\n{failures} of {len(filenames)} file(s) failed.", err=True)
        sys.exit(1)
