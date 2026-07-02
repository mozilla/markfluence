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
:mod:`mzcld_confluence_cli.libmarkdown`; HTTP calls go through
:class:`mzcld_confluence_cli.libclient.ConfluenceClient`.
"""

import datetime
import os
import sys

import click

from .libclient import ConfluenceClient
from .libmarkdown import (
    extract_frontmatter,
    extract_space_key,
    md_to_confluence,
    update_frontmatter_field,
)


def process_file(filename, client, message, resolve_only, force):
    """Publish (or resolve) a single markdown file. Returns True on success."""
    prefix = f"[{filename}]"

    # Read markdown and parse frontmatter
    with open(filename) as f:
        md_content = f.read()

    frontmatter, _ = extract_frontmatter(md_content)

    # The page title must come from the frontmatter.
    page_title = frontmatter.get("title")
    if not page_title:
        click.echo(
            f"{prefix} Error: no 'title' field found in frontmatter.\n"
            f"{prefix} Add a 'title: <Page Title>' line to the YAML frontmatter at "
            f"the top of the file.",
            err=True,
        )
        return False

    # Resolve page ID from frontmatter; if absent, search by title and write back.
    page_id = frontmatter.get("page_id")
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
        md_content = update_frontmatter_field(md_content, "page_id", page_id)
        with open(filename, "w") as f:
            f.write(md_content)

    # Fetch page metadata once: we need the version for the update call, and
    # the webui link to derive the space key for rewriting internal .md links.
    page = client.get_page(page_id)
    links = page.get("_links", {})
    webui = links.get("webui", "")
    base = links.get("base", client.base_url + "/wiki")
    space_key = extract_space_key(webui)

    if resolve_only:
        page_url = (
            base + webui
            if webui
            else f"{client.base_url}/wiki/pages/viewpage.action?pageId={page_id}"
        )
        click.echo(f"{prefix} page_id: {page_id}")
        click.echo(f"{prefix} title: {page['title']}")
        click.echo(f"{prefix} version: {page['version']['number']}")
        click.echo(f"{prefix} url: {page_url}")
        return True

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
    _, md_body = extract_frontmatter(md_content)
    html_content = md_to_confluence(md_body, filename, client.base_url, space_key)

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

    click.echo(f"{prefix} Published v{current_version + 1}: {url}")
    return True


@click.command()
@click.argument("filenames", nargs=-1, required=True)
@click.option(
    "--message",
    default="Updated via mzcld-confluence-cli",
    help="Version message.",
)
@click.option(
    "--resolve",
    "resolve_only",
    is_flag=True,
    help="Only resolve the page ID (search by frontmatter title if not in "
    "frontmatter) and print page info, then exit.",
)
@click.option(
    "--force",
    is_flag=True,
    help="Skip the file-mtime check and always update the page.",
)
def update(filenames, message, resolve_only, force):
    """Publish one or more markdown FILENAMES to Confluence pages.

    Title and page ID are read from each file's YAML frontmatter. Each file is
    processed independently; the command exits non-zero if any file failed.
    """
    client = ConfluenceClient.from_env()

    failures = 0
    for filename in filenames:
        try:
            ok = process_file(filename, client, message, resolve_only, force)
        except Exception as exc:
            click.echo(f"[{filename}] Error: {exc}", err=True)
            ok = False
        if not ok:
            failures += 1

    if failures > 0:
        click.echo(f"\n{failures} of {len(filenames)} file(s) failed.", err=True)
        sys.exit(1)
