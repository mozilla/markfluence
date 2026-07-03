# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at https://mozilla.org/MPL/2.0/.

"""Page-width support: the ``page_width`` frontmatter field.

Confluence stores a page's width as two content properties rather than on the
page body. Authors express the width with the UI vocabulary -- ``narrow``,
``wide``, ``max`` -- which maps to the underlying property values:

===========  ======================
``page_width``  content-property value
===========  ======================
``narrow``      ``default``
``wide``        ``full-width``
``max``         ``max``
===========  ======================

An unset/blank ``page_width`` defaults to ``max``: markfluence treats the
markdown file as the source of truth for width, so ``create``/``update`` assert
it on every publish (both the published and draft appearance properties, so the
viewed page and the editor agree).
"""

import click
import httpx2

# The two content properties Confluence uses for page appearance/width. We write
# both so the published view and the editor render the same width.
PUBLISHED_KEY = "content-appearance-published"
DRAFT_KEY = "content-appearance-draft"

DEFAULT_WIDTH = "max"

# page_width vocabulary -> content-property value.
_VOCAB_TO_PROPERTY = {
    "narrow": "default",
    "wide": "full-width",
    "max": "max",
}

# content-property value -> page_width vocabulary. Includes the legacy "fixed"
# value (mark's narrow option), which we surface as "narrow".
_PROPERTY_TO_VOCAB = {
    "max": "max",
    "full-width": "wide",
    "default": "narrow",
    "fixed": "narrow",
}


def declared_width(frontmatter):
    """Return the ``page_width`` a file declares, defaulting to ``max``.

    Unset or blank -> ``max``. A present but unrecognized value raises
    :class:`ValueError` (callers turn this into a validation error).
    """
    raw = frontmatter.get("page_width")
    if raw is None or str(raw).strip() == "":
        return DEFAULT_WIDTH
    value = str(raw).strip().lower()
    if value not in _VOCAB_TO_PROPERTY:
        raise ValueError(f"invalid page_width {raw!r}; expected narrow, wide, or max")
    return value


def apply_page_width(client, page_id, width):
    """Set both appearance properties for ``page_id`` to match ``width``.

    Returns a list of ``(key, action)`` where action is "set"/"unchanged".
    Raises on an HTTP failure that survives the client's retry.
    """
    prop_value = _VOCAB_TO_PROPERTY[width]
    return [
        (key, client.set_content_property(page_id, key, prop_value))
        for key in (PUBLISHED_KEY, DRAFT_KEY)
    ]


def set_page_width(client, page_id, width, prefix):
    """Apply ``width`` to a live page and report via click.

    Best-effort: the page write itself has already succeeded by the time this
    runs, so a content-property failure is reported as a warning and swallowed
    rather than failing the command.
    """
    try:
        actions = apply_page_width(client, page_id, width)
    except httpx2.HTTPError as exc:
        click.echo(f"{prefix} warning: could not set page width: {exc}", err=True)
        return
    if any(action == "set" for _key, action in actions):
        click.echo(f"{prefix} page width: {width}")


def read_page_width(client, page_id):
    """Return ``(width, explicit)`` for a live page.

    ``width`` is the published appearance property reverse-mapped to the
    ``page_width`` vocabulary. ``explicit`` is False when the property isn't set
    (the page renders at Confluence's site default, which we surface as
    ``narrow``).
    """
    prop = client.get_content_property(page_id, PUBLISHED_KEY)
    if prop is None:
        return ("narrow", False)
    return (_PROPERTY_TO_VOCAB.get(prop.get("value"), "narrow"), True)


def width_from_properties(properties):
    """Like :func:`read_page_width` but from an already-fetched property list.

    Returns ``(width, explicit)``; ``explicit`` is False when the published
    appearance property isn't present.
    """
    for prop in properties:
        if prop.get("key") == PUBLISHED_KEY:
            return (_PROPERTY_TO_VOCAB.get(prop.get("value"), "narrow"), True)
    return ("narrow", False)
