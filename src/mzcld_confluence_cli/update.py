"""The ``update`` subcommand: publish markdown files to Confluence pages.

The markdown-to-Confluence-storage conversion functions are ported verbatim from
the original ``confluence_publish.py`` script. HTTP calls are routed through
:class:`mzcld_confluence_cli.libclient.ConfluenceClient`.

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
"""

import datetime
import html as html_lib
import os
import re
import sys
import urllib.parse

import click
from marko.ext.gfm import gfm

from .libclient import ConfluenceClient


def extract_frontmatter(md_content):
    """Extract YAML frontmatter from markdown content.

    Returns ``(frontmatter_dict, body)`` where body is md_content with the
    frontmatter block removed. If there's no frontmatter, returns
    ``({}, md_content)``.

    Only handles flat ``key: value`` pairs -- no nested structures, lists, or
    multiline values.
    """
    match = re.match(r"^---\n(.*?)\n---\n", md_content, re.DOTALL)
    if not match:
        return {}, md_content

    fm_text = match.group(1)
    body = md_content[match.end() :]

    frontmatter = {}
    for line in fm_text.splitlines():
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        if ":" in line:
            key, _, value = line.partition(":")
            frontmatter[key.strip()] = value.strip()
    return frontmatter, body


def extract_title_from_markdown(md_content):
    """Extract the page title from markdown content.

    Looks for ``title:`` in YAML frontmatter first, then falls back to the
    first H1 heading in the body.
    """
    frontmatter, body = extract_frontmatter(md_content)
    title = frontmatter.get("title")
    if title:
        return title
    for line in body.splitlines():
        match = re.match(r"^#\s+(.+)$", line)
        if match:
            return match.group(1).strip()
    return None


def update_frontmatter_field(md_content, key, value):
    """Add or update a key in the markdown's YAML frontmatter.

    If the key already exists, its value is replaced. If the key doesn't
    exist, it's appended to the end of the frontmatter block. If there's no
    frontmatter block, one is created at the top of the document.

    Returns the new markdown content as a string.
    """
    match = re.match(r"^---\n(.*?)\n---\n", md_content, re.DOTALL)
    if not match:
        return f"---\n{key}: {value}\n---\n{md_content}"

    fm_text = match.group(1)
    body = md_content[match.end() :]

    new_lines = []
    replaced = False
    key_pattern = re.compile(rf"^\s*{re.escape(key)}\s*:")
    for line in fm_text.splitlines():
        if key_pattern.match(line):
            new_lines.append(f"{key}: {value}")
            replaced = True
        else:
            new_lines.append(line)
    if not replaced:
        new_lines.append(f"{key}: {value}")

    new_fm = "\n".join(new_lines)
    return f"---\n{new_fm}\n---\n{body}"


def replace_confluence_notes(html):
    """Replace <!-- confluence-note --> ... <!-- /confluence-note --> with a Confluence note macro.

    Usage in markdown::

        <!-- confluence-note -->
        Content here (can include HTML from marko conversion).
        <!-- /confluence-note -->
    """
    pattern = re.compile(
        r"<!--\s*confluence-note\s*-->"
        r"(.*?)"
        r"<!--\s*/confluence-note\s*-->",
        re.DOTALL,
    )

    def _build_macro(match):
        body = match.group(1).strip()
        return (
            '<ac:structured-macro ac:name="note" ac:schema-version="1">'
            f"<ac:rich-text-body>{body}</ac:rich-text-body>"
            "</ac:structured-macro>"
        )

    return pattern.sub(_build_macro, html)


def collapse_paragraph_newlines(html):
    """Collapse soft-wrap newlines in HTML text content.

    Markdown source files use soft-wrapping for readability. The marko
    converter keeps those newlines in the HTML output, and Confluence
    treats them as hard line breaks (rendering them as ``<br />``).
    This function flattens them to spaces so wrapped text renders as
    continuous content.

    Strategy: replace every ``\\n`` outside ``<pre>...</pre>`` blocks
    with a single space. Whitespace between block tags (e.g., between
    ``</li>`` and the next ``<li>``) becomes a space rather than a
    newline, which has no display impact. ``<pre>`` blocks are stashed
    and restored so their internal newlines stay intact.
    """
    pre_blocks = []

    def _stash(match):
        pre_blocks.append(match.group(0))
        return f"\x00PRE{len(pre_blocks) - 1}\x00"

    # Stash <pre>...</pre> blocks (their newlines are significant).
    html = re.sub(r"<pre[\s>].*?</pre>", _stash, html, flags=re.DOTALL)

    # Collapse all remaining newlines to spaces.
    html = html.replace("\n", " ")

    # Restore stashed <pre> blocks.
    for i, block in enumerate(pre_blocks):
        html = html.replace(f"\x00PRE{i}\x00", block)

    return html


def replace_code_blocks(html):
    """Convert ``<pre><code>...</code></pre>`` blocks to Confluence code macros.

    marko renders fenced code blocks as ``<pre><code class="language-X">``
    elements with HTML-escaped content. Confluence's storage format renders
    those as plain inline code (one styled line per line) rather than a
    proper code block. This function rewrites them as ``code`` macros with
    CDATA-wrapped raw source.

    The ``language`` parameter is set when marko emitted a
    ``class="language-X"``; otherwise it's omitted (Confluence will display
    a generic code block).

    Run this **after** ``collapse_paragraph_newlines`` so that function's
    ``<pre>`` stashing still protects the original blocks during the
    text-collapse step.
    """
    pattern = re.compile(
        r'<pre><code(?:\s+class="language-([^"]+)")?>(.*?)</code></pre>',
        re.DOTALL,
    )

    def _build_macro(match):
        language = match.group(1)
        # Decode HTML entities to restore the original source code.
        content = html_lib.unescape(match.group(2))
        # Strip the trailing newline marko appends inside the <code> tag.
        if content.endswith("\n"):
            content = content[:-1]
        # Escape any literal "]]>" so it doesn't terminate the CDATA section.
        content = content.replace("]]>", "]]]]><![CDATA[>")

        params = ""
        if language:
            params = f'<ac:parameter ac:name="language">{language}</ac:parameter>'

        return (
            '<ac:structured-macro ac:name="code" ac:schema-version="1">'
            f"{params}"
            f"<ac:plain-text-body><![CDATA[{content}]]></ac:plain-text-body>"
            "</ac:structured-macro>"
        )

    return pattern.sub(_build_macro, html)


def replace_github_callouts(html):
    """Replace GitHub-style callout blockquotes with Confluence panel macros.

    GitHub markdown alerts look like::

        > [!NOTE]
        > Some content here.

    The marko GFM extension recognizes these natively and renders them as::

        <blockquote class="alert alert-note">
        <p>Note</p>
        <p>Some content here.</p>
        </blockquote>

    This function rewrites them as Confluence info/tip/note/warning macros
    (see https://confluence.atlassian.com/doc/info-tip-note-and-warning-macros-51872369.html).
    The "Note"/"Tip"/etc. label paragraph is dropped because the Confluence
    panel renders its own title.

    Mapping (GitHub callout -> Confluence macro):
        [!NOTE]      -> info     (blue)
        [!TIP]       -> tip      (green)
        [!IMPORTANT] -> note     (yellow)
        [!WARNING]   -> warning  (red)
        [!CAUTION]   -> warning  (red -- Confluence has no separate caution)
    """
    callout_map = {
        "note": "info",
        "tip": "tip",
        "important": "note",
        "warning": "warning",
        "caution": "warning",
    }

    pattern = re.compile(
        r'<blockquote class="alert alert-(note|tip|important|warning|caution)">\s*'
        r"<p>(?:Note|Tip|Important|Warning|Caution)</p>\s*"
        r"(.*?)\s*"
        r"</blockquote>",
        re.DOTALL,
    )

    def _build_macro(match):
        callout_type = match.group(1)
        body = match.group(2).strip()
        macro_name = callout_map[callout_type]

        return (
            f'<ac:structured-macro ac:name="{macro_name}" ac:schema-version="1">'
            f"<ac:rich-text-body>{body}</ac:rich-text-body>"
            "</ac:structured-macro>"
        )

    return pattern.sub(_build_macro, html)


def github_slug(heading_text):
    """Replicate GitHub-flavored markdown's heading anchor slugger.

    Rules (matches what GitHub uses when rendering markdown headings):
    lowercase; strip every character except letters, digits, hyphens,
    underscores, and whitespace; replace runs of whitespace with a single
    hyphen; trim leading/trailing hyphens.
    """
    slug = heading_text.lower()
    slug = re.sub(r"[^\w\s-]", "", slug)
    # Each whitespace char becomes one hyphen -- so "Detect & Verify" (where
    # the `&` gets stripped, leaving two adjacent spaces) yields a double
    # hyphen, matching GitHub's actual behavior.
    slug = re.sub(r"\s", "-", slug)
    return slug.strip("-")


def confluence_slug(heading_text):
    """Replicate Confluence's heading-anchor scheme.

    Confluence preserves case and punctuation; only runs of whitespace are
    collapsed to single hyphens. URL encoding (e.g. ``?`` -> ``%3F``) happens
    when the browser resolves the fragment, not in the stored id.
    """
    return re.sub(r"\s+", "-", heading_text.strip())


def extract_headings(md_body):
    """Yield heading text for each ATX heading in ``md_body``.

    Frontmatter must already be stripped. Fenced code blocks are skipped so
    ``#`` lines inside code samples don't get treated as headings.
    """
    headings = []
    in_code_block = False
    for line in md_body.splitlines():
        if re.match(r"^```", line):
            in_code_block = not in_code_block
            continue
        if in_code_block:
            continue
        match = re.match(r"^#+\s+(.+?)\s*$", line)
        if match:
            headings.append(match.group(1))
    return headings


def build_headings_anchor_map(directory):
    """Map each ``*.md`` filename to a ``{github_slug: confluence_slug}`` dict.

    Used by :func:`rewrite_anchor_links` to translate the GitHub-style anchor
    fragments authored in the source markdown into the Confluence-style ids
    that the published pages actually use.
    """
    anchor_map = {}
    try:
        entries = os.listdir(directory or ".")
    except OSError:
        return anchor_map
    for entry in entries:
        if not entry.endswith(".md"):
            continue
        path = os.path.join(directory or ".", entry)
        try:
            with open(path) as f:
                content = f.read()
        except OSError:
            continue
        _, body = extract_frontmatter(content)
        file_anchors = {}
        for heading in extract_headings(body):
            gh = github_slug(heading)
            if gh:
                file_anchors[gh] = confluence_slug(heading)
        anchor_map[entry] = file_anchors
    return anchor_map


def rewrite_anchor_links(html, anchor_map, current_filename):
    """Rewrite GitHub-style anchor fragments to Confluence-style ids.

    Handles both same-page (``href="#fragment"``) and cross-file
    (``href="other.md#fragment"``) anchors. Must run before
    :func:`replace_internal_doc_links`, so that step rewrites the ``.md`` URL
    while preserving the now-Confluence-style fragment verbatim.

    Fragments that don't match any known heading are left alone -- better to
    keep an authored link intact than mangle one we don't recognize.
    """
    current_basename = os.path.basename(current_filename)

    def _rewrite(match):
        href = match.group(1)
        if href.startswith("#"):
            fragment = href[1:]
            new_fragment = anchor_map.get(current_basename, {}).get(fragment)
            if new_fragment:
                # Rewrite same-page anchors as fake cross-file links to the
                # current file. Confluence's storage parser strips bare
                # ``href="#fragment"`` values during publish, but
                # ``replace_internal_doc_links`` will turn this into a fully
                # qualified ``<base_url>/.../pages/<id>/<title>#fragment``
                # URL, which survives the parser and resolves in-page.
                escaped = html_lib.escape(new_fragment, quote=False)
                return f'href="{current_basename}#{escaped}"'
            return match.group(0)
        cross = re.match(r"^(.+\.md)#(.+)$", href)
        if cross:
            target_path = cross.group(1)
            fragment = cross.group(2)
            target_basename = os.path.basename(target_path)
            new_fragment = anchor_map.get(target_basename, {}).get(fragment)
            if new_fragment:
                escaped = html_lib.escape(new_fragment, quote=False)
                return f'href="{target_path}#{escaped}"'
        return match.group(0)

    return re.sub(r'href="([^"]+)"', _rewrite, html)


def build_docs_page_map(directory):
    """Map each ``*.md`` filename in ``directory`` to its frontmatter
    ``page_id`` and ``title``.

    Returns ``{filename: {"page_id": ..., "title": ...}}``. Files without a
    usable ``page_id`` (missing or ``null``) are skipped. Used to rewrite
    links between sibling docs to their Confluence URLs -- the title is
    needed in the URL path or Confluence's redirect strips the fragment.
    """
    page_map = {}
    try:
        entries = os.listdir(directory or ".")
    except OSError:
        return page_map
    for entry in entries:
        if not entry.endswith(".md"):
            continue
        path = os.path.join(directory or ".", entry)
        try:
            with open(path) as f:
                content = f.read()
        except OSError:
            continue
        frontmatter, _ = extract_frontmatter(content)
        page_id = frontmatter.get("page_id")
        if page_id and page_id != "null":
            page_map[entry] = {
                "page_id": page_id,
                "title": frontmatter.get("title", ""),
            }
    return page_map


def extract_space_key(webui_link):
    """Extract the space key from a ``/spaces/{key}/pages/...`` webui link."""
    match = re.match(r"^/spaces/([^/]+)/", webui_link or "")
    if match:
        return match.group(1)
    return None


def replace_internal_doc_links(html, page_map, base_url, space_key):
    """Rewrite ``<a href="X.md">`` links to point at their Confluence pages.

    The href is looked up by basename in ``page_map`` to get the target
    page_id and title, then replaced with
    ``{base_url}/wiki/spaces/{space_key}/pages/{page_id}/{title_slug}``
    (or the universal ``viewpage.action?pageId=...`` form when ``space_key``
    is None). The title slug is required for anchor links to resolve --
    without it Confluence's server-side redirect to the canonical URL drops
    the ``#fragment``. A trailing ``#fragment`` is preserved verbatim.

    Links to ``.md`` files not in ``page_map`` (e.g., absolute URLs, files
    in other directories, or files whose frontmatter lacks a ``page_id``)
    are left unchanged.
    """
    pattern = re.compile(
        r'href="([^"#]+?\.md)(#[^"]*)?"',
        re.IGNORECASE,
    )

    def _rewrite(match):
        href = match.group(1)
        fragment = match.group(2) or ""

        if "://" in href or href.startswith("//"):
            return match.group(0)

        basename = os.path.basename(href)
        entry = page_map.get(basename)
        if not entry:
            return match.group(0)

        page_id = entry["page_id"]
        title = entry.get("title", "")

        if space_key:
            title_slug = urllib.parse.quote_plus(title) if title else ""
            new_href = (
                f"{base_url}/wiki/spaces/{space_key}/pages/{page_id}/{title_slug}"
            )
        else:
            new_href = f"{base_url}/wiki/pages/viewpage.action?pageId={page_id}"

        return f'href="{new_href}{fragment}"'

    return pattern.sub(_rewrite, html)


def replace_chart_directives(html):
    """Replace <!-- chart:TYPE [OPTIONS] --> + following <table> with a Confluence chart macro.

    Supported directives:
        <!-- chart:pie -->        -- pie chart from the next table
        <!-- chart:bar -->        -- bar chart from the next table
        <!-- chart:bar stacked --> -- stacked bar chart from the next table

    The directive and the table it references are replaced by an
    <ac:structured-macro ac:name="chart"> element.  The table that follows
    the directive is consumed by the chart (not displayed separately).
    """
    pattern = re.compile(
        r"<!--\s*chart:(\w+)"  # chart type (pie, bar, ...)
        r"(?:\s+([\w\s]+?))?"  # optional space-separated options (e.g. "stacked")
        r"\s*-->"
        r"(.*?)"  # any whitespace / stray tags between comment and table
        r"(<table.*?</table>)",  # the next table element
        re.DOTALL,
    )

    def _build_macro(match):
        chart_type = match.group(1)
        options = (match.group(2) or "").split()
        table_html = match.group(4)

        params = f'<ac:parameter ac:name="type">{chart_type}</ac:parameter>'
        if "stacked" in options:
            params += '<ac:parameter ac:name="stacked">true</ac:parameter>'

        return (
            f'<ac:structured-macro ac:name="chart" ac:schema-version="1">'
            f"{params}"
            f"<ac:rich-text-body>{table_html}</ac:rich-text-body>"
            f"</ac:structured-macro>"
        )

    return pattern.sub(_build_macro, html)


def replace_layout_blocks(html):
    """Replace Mark-style layout directives with Confluence layout macros.

    Mirrors the directive syntax used by https://github.com/kovetskiy/mark::

        <!-- ac:layout -->
        <!-- ac:layout-section type:two_right_sidebar -->
        <!-- ac:layout-cell -->
        Left column content.
        <!-- ac:layout-cell end -->
        <!-- ac:layout-cell -->
        Right column content.
        <!-- ac:layout-cell end -->
        <!-- ac:layout-section end -->
        <!-- ac:layout end -->

    Each marker is a standalone HTML comment, so this is a sequence of
    independent substitutions -- no structural parsing or validation. A
    malformed layout will be caught by Confluence at publish time. The
    ``end`` substitutions run before their open counterparts so the open
    patterns don't accidentally match the close.

    Supported ``type:`` values are Confluence's standard set: ``single``,
    ``two_equal``, ``two_left_sidebar``, ``two_right_sidebar``,
    ``three_equal``, ``three_with_sidebars``.
    """
    substitutions = [
        (r"<!--\s*ac:layout-cell\s+end\s*-->", "</ac:layout-cell>"),
        (r"<!--\s*ac:layout-cell\s*-->", "<ac:layout-cell>"),
        (r"<!--\s*ac:layout-section\s+end\s*-->", "</ac:layout-section>"),
        (
            r"<!--\s*ac:layout-section\s+type:(\w+)\s*-->",
            r'<ac:layout-section ac:type="\1">',
        ),
        (r"<!--\s*ac:layout\s+end\s*-->", "</ac:layout>"),
        (r"<!--\s*ac:layout\s*-->", "<ac:layout>"),
    ]
    for pattern, replacement in substitutions:
        html = re.sub(pattern, replacement, html)
    return html


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
                f"{prefix} Error: found {len(matches)} pages with title '{page_title}':",
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

    # Strip YAML frontmatter before converting to HTML
    _, md_body = extract_frontmatter(md_content)

    # Convert to HTML using GFM for table support
    html_content = gfm.convert(md_body)

    # Rewrite GitHub-style anchor fragments (e.g. "#is-this-an-incident") to
    # the Confluence-style ids the published pages actually use
    # ("#Is-this-an-incident"). Run before replace_internal_doc_links so the
    # .md -> Confluence URL step carries the corrected fragment through.
    anchor_map = build_headings_anchor_map(os.path.dirname(filename))
    html_content = rewrite_anchor_links(html_content, anchor_map, filename)

    # Rewrite links to sibling .md files (e.g. "managing_an_incident.md") to
    # the Confluence URL of the page they describe. Other transforms below
    # don't touch <a href="..."> values, so the order here doesn't matter.
    page_map = build_docs_page_map(os.path.dirname(filename))
    html_content = replace_internal_doc_links(
        html_content,
        page_map,
        client.base_url,
        space_key,
    )

    # Replace <!-- confluence-toc --> placeholder with Confluence TOC macro
    html_content = html_content.replace(
        "<!-- confluence-toc -->",
        '<ac:structured-macro ac:name="toc" ac:schema-version="1" />',
    )

    # Replace <!-- confluence-note --> ... <!-- /confluence-note --> with Confluence
    # note macro (yellow info panel).
    html_content = replace_confluence_notes(html_content)

    # Replace <!-- chart:TYPE [OPTIONS] --> + next <table> with Confluence chart macro
    html_content = replace_chart_directives(html_content)

    # Replace Mark-style <!-- ac:layout --> directives with Confluence layout macros.
    html_content = replace_layout_blocks(html_content)

    # Replace GitHub-style callouts (> [!NOTE], etc.) with Confluence panel macros.
    html_content = replace_github_callouts(html_content)

    # Collapse soft-wrapped newlines inside <p> tags to spaces so Confluence
    # doesn't render them as hard line breaks.
    html_content = collapse_paragraph_newlines(html_content)

    # Convert fenced code blocks to Confluence code macros (after collapse so
    # the <pre> stash protects them during the newline pass).
    html_content = replace_code_blocks(html_content)

    current_version = page["version"]["number"]

    click.echo(
        f"{prefix} Updating '{page_title}' (v{current_version} -> v{current_version + 1})..."
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
