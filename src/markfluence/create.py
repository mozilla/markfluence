# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at https://mozilla.org/MPL/2.0/.

"""The ``create`` subcommand: create new Confluence pages from markdown files.

Title comes from each file's frontmatter (like ``update``); the target space is
given by ``--space KEY`` or a ``space:`` frontmatter field. The optional parent
comes from ``--parent PAGE_ID`` or a ``parent:`` frontmatter field, which may be
``null`` (top-level), a numeric page id (an existing external page), or a path to
a sibling ``.md`` file (a parent authored in the same doc set).

Creation is two-phase and transactional at the validation boundary:

* **Phase 1** validates *every* file (title present, space resolves, no live page
  already at ``page_id``, no title clash in the space, parent resolvable and in the
  space, hierarchy acyclic). If any file fails, all failures are reported and
  nothing is created.
* **Phase 2** creates the pages in topological order (parents before children,
  resolving in-set ``.md`` parents to their freshly created ids) and writes
  ``page_id``/``space``/``parent`` back into each file's frontmatter.
"""

import os
import sys
from collections import defaultdict, deque

import click

from .libclient import ConfluenceClient
from .libmarkdown import (
    extract_frontmatter,
    md_to_confluence,
    update_frontmatter_field,
)
from .pagewidth import declared_width, set_page_width


class _ValidationError(Exception):
    """A single file failed phase-1 validation."""


def _resolve_parent(filename, frontmatter, parent_opt, in_set_abs, client, space_id):
    """Resolve the parent for one file. Returns a dict with parent metadata.

    Keys: ``kind`` (top|inset|published|external), ``id`` (page id or None),
    ``abs`` (absolute path of an in-set parent, else None), ``display`` (the
    original ``.md`` path when the parent was a file reference, else None).
    """
    fm_parent = frontmatter.get("parent")
    fm_parent_set = fm_parent is not None and fm_parent != "null"

    if parent_opt is not None and fm_parent_set:
        raise _ValidationError(
            "both --parent and a frontmatter 'parent' are set; use only one"
        )

    parent_value = parent_opt if parent_opt is not None else fm_parent
    if not parent_value or parent_value == "null":
        return {"kind": "top", "id": None, "abs": None, "display": None}

    if parent_value.endswith(".md"):
        parent_path = os.path.join(os.path.dirname(filename), parent_value)
        if not os.path.isfile(parent_path):
            raise _ValidationError(f"parent file not found: {parent_value}")
        parent_abs = os.path.abspath(parent_path)
        if parent_abs in in_set_abs:
            # Created in this same run; its id is resolved during phase 2.
            return {
                "kind": "inset",
                "id": None,
                "abs": parent_abs,
                "display": parent_value,
            }
        # Already-published sibling: read its page_id from frontmatter.
        with open(parent_path) as f:
            p_fm, _ = extract_frontmatter(f.read())
        p_id = p_fm.get("page_id")
        if not p_id or p_id == "null":
            raise _ValidationError(
                f"parent not yet published (no page_id): {parent_value}"
            )
        _check_parent_in_space(client, p_id, space_id)
        return {"kind": "published", "id": p_id, "abs": None, "display": parent_value}

    # A bare page id: an existing external page.
    _check_parent_in_space(client, parent_value, space_id)
    return {"kind": "external", "id": parent_value, "abs": None, "display": None}


def _check_parent_in_space(client, parent_id, space_id):
    """Raise unless ``parent_id`` names a live page in ``space_id``."""
    parent_page = client.get_page_or_none(parent_id)
    if parent_page is None:
        raise _ValidationError(f"parent page {parent_id} not found")
    if str(parent_page.get("spaceId")) != str(space_id):
        raise _ValidationError(f"parent page {parent_id} is not in the target space")


def _resolve_file(filename, space_opt, parent_opt, client, in_set_abs, space_cache):
    """Validate one file and return its resolved record. Raises _ValidationError."""
    try:
        with open(filename) as f:
            md_content = f.read()
    except OSError as exc:
        raise _ValidationError(str(exc)) from exc

    frontmatter, _ = extract_frontmatter(md_content)

    title = frontmatter.get("title")
    if not title:
        raise _ValidationError("no 'title' field found in frontmatter")

    # Validate page_width up front (unset/blank -> the max default).
    try:
        page_width = declared_width(frontmatter)
    except ValueError as exc:
        raise _ValidationError(str(exc)) from exc

    # Space precedence: --space or frontmatter 'space'; both-and-differ -> error.
    fm_space = frontmatter.get("space")
    if space_opt and fm_space and space_opt != fm_space:
        raise _ValidationError(
            f"--space {space_opt!r} conflicts with frontmatter space {fm_space!r}"
        )
    space_key = space_opt or fm_space
    if not space_key:
        raise _ValidationError(
            "no space given (pass --space or add a 'space:' frontmatter field)"
        )
    if space_key not in space_cache:
        space_cache[space_key] = client.resolve_space_id(space_key)
    space_id = space_cache[space_key]
    if space_id is None:
        raise _ValidationError(f"space {space_key!r} not found")

    parent = _resolve_parent(
        filename, frontmatter, parent_opt, in_set_abs, client, space_id
    )

    # A frontmatter page_id that points at a live page blocks creation.
    existing_page_id = frontmatter.get("page_id")
    if existing_page_id and existing_page_id != "null":
        if client.page_exists(existing_page_id):
            raise _ValidationError("Page exists.")

    # Confluence requires unique titles per space.
    if client.search_pages_by_title(title, space_id=space_id):
        raise _ValidationError(
            f"a page titled {title!r} already exists in space {space_key}"
        )

    return {
        "filename": filename,
        "abs_path": os.path.abspath(filename),
        "md_content": md_content,
        "title": title,
        "space_key": space_key,
        "space_id": space_id,
        "parent": parent,
        "page_width": page_width,
    }


def _topo_sort(records, records_by_abs):
    """Order records parents-before-children. Raises _ValidationError on a cycle."""
    indegree = {r["abs_path"]: 0 for r in records}
    children = defaultdict(list)
    for r in records:
        if r["parent"]["kind"] == "inset":
            children[r["parent"]["abs"]].append(r["abs_path"])
            indegree[r["abs_path"]] += 1

    queue = deque(a for a in indegree if indegree[a] == 0)
    order = []
    while queue:
        current = queue.popleft()
        order.append(current)
        for child in children[current]:
            indegree[child] -= 1
            if indegree[child] == 0:
                queue.append(child)

    if len(order) != len(records):
        raise _ValidationError("parent cycle detected among the given files")
    return [records_by_abs[a] for a in order]


def _parent_field(parent, parent_id):
    """Build the ``(value, comment)`` for the frontmatter ``parent`` line.

    The comment (the original ``.md`` path, when the parent was a sibling doc) is
    kept separate from the value so ``update_frontmatter_field`` writes it as a
    trailing annotation rather than folding it into the value.
    """
    if parent["kind"] == "top":
        return "null", None
    return str(parent_id), parent["display"]


def _create_one(record, parent_id, client):
    """Create the page for one record and write frontmatter back. Returns the URL."""
    prefix = f"[{record['filename']}]"
    _, md_body = extract_frontmatter(record["md_content"])
    html_content, images = md_to_confluence(
        md_body, record["filename"], client.base_url, record["space_key"]
    )
    for message in images["broken"] + images["warnings"]:
        click.echo(f"{prefix} warning: {message}", err=True)

    result = client.create_page(
        record["space_id"], record["title"], html_content, parent_id
    )
    new_id = result["id"]

    # Upload referenced local images now that the page (and its id) exists.
    for att_name, action in client.sync_attachments(new_id, images["attachments"]):
        click.echo(f"{prefix} attachment {action}: {att_name}")

    # Assert the page width (a content property, so a separate call). A failure
    # here is non-fatal: the page itself was created.
    set_page_width(client, new_id, record["page_width"], prefix)

    parent_value, parent_comment = _parent_field(record["parent"], parent_id)
    content = record["md_content"]
    content = update_frontmatter_field(content, "page_id", new_id)
    content = update_frontmatter_field(content, "space", record["space_key"])
    content = update_frontmatter_field(
        content, "parent", parent_value, comment=parent_comment
    )
    with open(record["filename"], "w") as f:
        f.write(content)

    links = result.get("_links", {})
    webui = links.get("webui", "")
    base = links.get("base", client.base_url + "/wiki")
    url = (
        base + webui
        if webui
        else f"{client.base_url}/wiki/pages/viewpage.action?pageId={new_id}"
    )
    return new_id, url


@click.command()
@click.argument("filenames", nargs=-1, required=True)
@click.option("--space", "space_opt", default=None, help="Target space key.")
@click.option(
    "--parent",
    "parent_opt",
    default=None,
    help="Parent page id for the new page(s).",
)
def create(filenames, space_opt, parent_opt):
    """Create new Confluence pages from markdown FILENAMES.

    All files are validated first; if any would fail (a live page already exists,
    a title clash, an unresolvable parent, ...), nothing is created. Otherwise the
    pages are created parents-first and their page_id/space/parent are written back
    into the frontmatter.
    """
    client = ConfluenceClient.from_env()
    in_set_abs = {os.path.abspath(f) for f in filenames}
    space_cache = {}

    # Phase 1: validate every file, create nothing.
    records = []
    errors = []
    for filename in filenames:
        try:
            records.append(
                _resolve_file(
                    filename, space_opt, parent_opt, client, in_set_abs, space_cache
                )
            )
        except _ValidationError as exc:
            errors.append((filename, str(exc)))

    ordered = []
    if not errors:
        records_by_abs = {r["abs_path"]: r for r in records}
        # In-set parent must resolve to the same space as its child.
        for r in records:
            parent = r["parent"]
            if parent["kind"] == "inset":
                parent_rec = records_by_abs[parent["abs"]]
                if parent_rec["space_id"] != r["space_id"]:
                    errors.append(
                        (r["filename"], "parent page is not in the target space")
                    )
        if not errors:
            try:
                ordered = _topo_sort(records, records_by_abs)
            except _ValidationError as exc:
                errors.append(("(hierarchy)", str(exc)))

    if errors:
        for filename, message in errors:
            click.echo(f"[{filename}] {message}", err=True)
        click.echo(
            f"\nAborting: {len(errors)} file(s) failed validation; "
            f"nothing was created.",
            err=True,
        )
        sys.exit(1)

    # Phase 2: create in topological order.
    created = {}
    failures = 0
    for record in ordered:
        prefix = f"[{record['filename']}]"
        parent = record["parent"]
        if parent["kind"] == "inset":
            parent_id = created.get(parent["abs"])
            if parent_id is None:
                click.echo(
                    f"{prefix} Error: parent page was not created; skipping",
                    err=True,
                )
                failures += 1
                continue
        else:
            parent_id = parent["id"]

        try:
            new_id, url = _create_one(record, parent_id, client)
        except Exception as exc:
            click.echo(f"{prefix} Error: {exc}", err=True)
            failures += 1
            continue

        created[record["abs_path"]] = new_id
        click.echo(f"{prefix} Created page {new_id}: {url}")

    if failures > 0:
        click.echo(f"\n{failures} of {len(ordered)} file(s) failed.", err=True)
        sys.exit(1)
