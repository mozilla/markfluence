# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at https://mozilla.org/MPL/2.0/.

"""Markdown -> Confluence storage-format conversion.

The frontmatter helpers and the ordered pipeline of regex transforms were ported
verbatim from the original ``confluence_publish.py`` script (and previously lived
inline in ``update.py``). :func:`md_to_confluence` runs the whole pipeline; both
the ``update`` and ``create`` subcommands call it.
"""

import html as html_lib
import json
import os
import re
import urllib.parse

from marko.ext.gfm import gfm


def _strip_inline_comment(value):
    """Strip a trailing YAML-style inline comment from a frontmatter value.

    A comment begins at the first ``#`` preceded by whitespace (matching YAML).
    Returns the value with the comment removed and surrounding whitespace stripped.
    Used for unquoted values; quoted values are handled by :func:`parse_value`,
    which suppresses inline-comment parsing inside the quotes.
    """
    match = re.search(r"\s#", value)
    if match:
        value = value[: match.start()]
    return value.strip()


def _scan_quoted(s):
    """Parse a leading quoted token from ``s`` (which starts with ``'`` or ``"``).

    Returns the unquoted value, or ``None`` if the quote is unterminated. Single
    quotes are literal with ``''`` -> ``'``; double quotes honor ``\\"`` and
    ``\\\\`` escapes. Anything after the closing quote (e.g. a trailing inline
    comment) is ignored.
    """
    quote = s[0]
    out = []
    i = 1
    while i < len(s):
        c = s[i]
        if quote == "'":
            if c == "'":
                if s[i + 1 : i + 2] == "'":  # doubled '' -> literal '
                    out.append("'")
                    i += 2
                    continue
                return "".join(out)  # closing quote
            out.append(c)
            i += 1
        else:  # double quote
            if c == "\\" and s[i + 1 : i + 2] in ('"', "\\"):
                out.append(s[i + 1])
                i += 2
                continue
            if c == '"':
                return "".join(out)  # closing quote
            out.append(c)
            i += 1
    return None  # unterminated


def parse_value(raw):
    """Parse a frontmatter value (the text after the first ``:``).

    A value whose first non-space character is ``'`` or ``"`` is read as a quoted
    string (inline ``#`` comments inside it are preserved); otherwise a trailing
    inline comment is stripped. An unterminated quote falls back to unquoted
    handling.
    """
    stripped = raw.lstrip()
    if stripped[:1] in ("'", '"'):
        value = _scan_quoted(stripped)
        if value is not None:
            return value
    return _strip_inline_comment(raw)


def extract_frontmatter(md_content):
    """Extract YAML frontmatter from markdown content.

    Returns ``(frontmatter_dict, body)`` where body is md_content with the
    frontmatter block removed. If there's no frontmatter, returns
    ``({}, md_content)``.

    Only handles flat ``key: value`` pairs -- no nested structures, lists, or
    multiline values. Full-line ``#`` comments are skipped, and a trailing
    inline ``#`` comment (whitespace then ``#``) is stripped from each unquoted
    value. Values may be single- or double-quoted to include characters that
    inline-comment stripping would otherwise eat (see :func:`parse_value`).
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
            frontmatter[key.strip()] = parse_value(value)
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


def _quote_value(value):
    """Quote ``value`` for frontmatter. Prefers single quotes."""
    if "'" not in value:
        return f"'{value}'"
    if '"' not in value:
        return f'"{value}"'
    escaped = value.replace("\\", "\\\\").replace('"', '\\"')
    return f'"{escaped}"'


def _render_value(value):
    """Render a value for a frontmatter line, quoting it only when necessary.

    A value is quoted iff it wouldn't survive a bare round-trip through
    :func:`parse_value` (e.g. it contains a whitespace-then-``#``, has
    significant leading/trailing whitespace, or starts with a quote character).
    """
    text = str(value)
    if parse_value(f" {text}") != text:
        return _quote_value(text)
    return text


def update_frontmatter_field(md_content, key, value, comment=None):
    """Add or update a key in the markdown's YAML frontmatter.

    If the key already exists, its value is replaced. If the key doesn't
    exist, it's appended to the end of the frontmatter block. If there's no
    frontmatter block, one is created at the top of the document. The value is
    quoted automatically when needed to round-trip. An optional ``comment`` is
    written as a trailing ``  # comment`` annotation (kept distinct from the
    value, which is what lets the value round-trip cleanly).

    Returns the new markdown content as a string.
    """
    rendered = _render_value(value)
    if comment:
        rendered = f"{rendered}  # {comment}"
    new_line = f"{key}: {rendered}"

    match = re.match(r"^---\n(.*?)\n---\n", md_content, re.DOTALL)
    if not match:
        return f"---\n{new_line}\n---\n{md_content}"

    fm_text = match.group(1)
    body = md_content[match.end() :]

    new_lines = []
    replaced = False
    key_pattern = re.compile(rf"^\s*{re.escape(key)}\s*:")
    for line in fm_text.splitlines():
        if key_pattern.match(line):
            new_lines.append(new_line)
            replaced = True
        else:
            new_lines.append(line)
    if not replaced:
        new_lines.append(new_line)

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


# Image extensions Confluence renders. Local images with other extensions are
# treated as broken (see replace_images).
SUPPORTED_IMAGE_EXTS = {".png", ".jpg", ".jpeg", ".gif", ".svg", ".webp", ".bmp"}

_IMG_RE = re.compile(r"<img\b([^>]*?)/?>", re.IGNORECASE)


def _image_attr(attrs, name):
    """Read the value of an HTML attribute from a captured tag-attribute string."""
    match = re.search(rf'\b{name}\s*=\s*"([^"]*)"', attrs, re.IGNORECASE)
    return match.group(1) if match else ""


def _attachment_filename(src):
    """Derive a stable, collision-free attachment filename from an image path.

    The path (relative to the markdown file) has ``/`` replaced by ``_`` so images
    from different directories don't collide; the name is independent of content so
    editing an image updates the same attachment in place. Matches mark's scheme.
    """
    name = src[2:] if src.startswith("./") else src
    return name.replace("/", "_")


_ALLOWED_ALIGN = {"left", "center", "right"}


def _parse_image_title(title_raw, src, warnings):
    """Turn the markdown image title into extra ``<ac:image>`` attributes.

    ``title_raw`` is the (marko-HTML-escaped) title attribute. If it decodes to a
    JSON object, its ``title``/``width``/``height``/``align`` keys become image
    attributes (``alt`` stays native and can't be overridden here); otherwise the
    whole string is used verbatim as the tooltip (``ac:title``). Invalid
    ``width``/``height``/``align`` values are dropped with a message appended to
    ``warnings``. Returns a dict of attribute name -> value.
    """
    if not title_raw:
        return {}

    text = html_lib.unescape(title_raw)
    try:
        data = json.loads(text)
    except ValueError:
        data = None
    if not isinstance(data, dict):
        return {"title": text}

    attrs = {}
    if data.get("title"):
        attrs["title"] = str(data["title"])
    for dimension in ("width", "height"):
        value = data.get(dimension)
        if value in (None, ""):
            continue
        if str(value).isdigit():
            attrs[dimension] = str(value)
        else:
            warnings.append(f"{src}: ignoring {dimension}={value!r} (must be a number)")
    align = data.get("align")
    if align:
        if str(align) in _ALLOWED_ALIGN:
            attrs["align"] = str(align)
        else:
            warnings.append(
                f"{src}: ignoring align={align!r} (must be left, center, or right)"
            )
    return attrs


def _ac_image(alt, attrs, *, ri_filename=None, ri_url=None):
    """Build a Confluence ``<ac:image>`` referencing an attachment or an URL.

    ``alt`` is the (raw) alt text; ``attrs`` may carry ``title``/``width``/
    ``height``/``align``. All attribute values are XML-escaped.
    """
    parts = []
    if alt:
        parts.append(f'ac:alt="{html_lib.escape(alt, quote=True)}"')
    for key in ("title", "width", "height", "align"):
        value = attrs.get(key)
        if value:
            parts.append(f'ac:{key}="{html_lib.escape(str(value), quote=True)}"')
    leading = (" " + " ".join(parts)) if parts else ""

    if ri_filename is not None:
        resource = f'<ri:attachment ri:filename="{ri_filename}" />'
    else:
        resource = f'<ri:url ri:value="{ri_url}" />'
    return f"<ac:image{leading}>{resource}</ac:image>"


def replace_images(html, base_dir):
    """Rewrite ``<img>`` tags to Confluence images, collecting local uploads.

    Returns ``(html, attachments, broken, warnings)``:

    * remote images (``http(s)://``) become ``<ac:image><ri:url/></ac:image>`` (no
      upload);
    * a local file with a supported extension that exists becomes
      ``<ac:image><ri:attachment/></ac:image>`` and is recorded in ``attachments``
      (``{"path", "filename"}``, deduped by filename) for the caller to upload;
    * a missing file or an unsupported extension is replaced with the literal text
      ``IMAGE BROKEN: <src> (<reason>)`` (also collected in ``broken``).

    The markdown title (``![alt](src "title")``) supplies extra attributes -- see
    :func:`_parse_image_title`; invalid property values are collected in
    ``warnings``. Paths resolve relative to ``base_dir``.
    """
    attachments = []
    broken = []
    warnings = []
    seen = set()

    def _sub(match):
        attrs = match.group(1)
        src = _image_attr(attrs, "src")
        alt = html_lib.unescape(_image_attr(attrs, "alt"))
        if not src:
            return match.group(0)

        img_attrs = _parse_image_title(_image_attr(attrs, "title"), src, warnings)

        if src.startswith(("http://", "https://", "//")):
            return _ac_image(alt, img_attrs, ri_url=src)

        if os.path.splitext(src)[1].lower() not in SUPPORTED_IMAGE_EXTS:
            message = f"IMAGE BROKEN: {src} (unsupported type)"
            broken.append(message)
            return html_lib.escape(message)

        local_path = os.path.join(base_dir or ".", src)
        if not os.path.isfile(local_path):
            message = f"IMAGE BROKEN: {src} (not found)"
            broken.append(message)
            return html_lib.escape(message)

        filename = _attachment_filename(src)
        if filename not in seen:
            seen.add(filename)
            attachments.append(
                {"path": os.path.abspath(local_path), "filename": filename}
            )
        return _ac_image(alt, img_attrs, ri_filename=filename)

    return _IMG_RE.sub(_sub, html), attachments, broken, warnings


def md_to_confluence(md_body, filename, base_url, space_key):
    """Convert a markdown body to Confluence storage-format HTML.

    ``md_body`` must already have its frontmatter stripped. ``filename`` is used
    to locate sibling ``.md`` files for anchor/link rewriting and to resolve image
    paths; ``base_url`` and ``space_key`` build the Confluence URLs those links
    point at.

    Returns ``(html, images)`` where ``images`` is
    ``{"attachments": [...], "broken": [...], "warnings": [...]}`` -- the local
    images to upload, the human-readable broken-image messages, and any
    image-property warnings, respectively.

    The step order encodes dependencies -- see the inline notes -- so keep it.
    """
    # Convert to HTML using GFM for table support.
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
        base_url,
        space_key,
    )

    # Replace <!-- confluence-toc --> placeholder with Confluence TOC macro.
    html_content = html_content.replace(
        "<!-- confluence-toc -->",
        '<ac:structured-macro ac:name="toc" ac:schema-version="1" />',
    )

    # Replace <!-- confluence-note --> ... <!-- /confluence-note --> with a
    # Confluence note macro (yellow info panel).
    html_content = replace_confluence_notes(html_content)

    # Replace <!-- chart:TYPE [OPTIONS] --> + next <table> with a chart macro.
    html_content = replace_chart_directives(html_content)

    # Replace Mark-style <!-- ac:layout --> directives with layout macros.
    html_content = replace_layout_blocks(html_content)

    # Replace GitHub-style callouts (> [!NOTE], etc.) with panel macros.
    html_content = replace_github_callouts(html_content)

    # Rewrite <img> tags to Confluence images, collecting local files to upload.
    html_content, attachments, broken, warnings = replace_images(
        html_content, os.path.dirname(filename)
    )

    # Collapse soft-wrapped newlines inside <p> tags to spaces so Confluence
    # doesn't render them as hard line breaks.
    html_content = collapse_paragraph_newlines(html_content)

    # Convert fenced code blocks to Confluence code macros (after collapse so
    # the <pre> stash protects them during the newline pass).
    html_content = replace_code_blocks(html_content)

    return html_content, {
        "attachments": attachments,
        "broken": broken,
        "warnings": warnings,
    }
