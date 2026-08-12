# Page width

Confluence stores a page's width as **content properties**, not on the page body.
There are two of them, and both have to be written or the viewed page and the
editor disagree about how wide the page is.

| property | |
|---|---|
| `content-appearance-published` | what a reader sees |
| `content-appearance-draft` | what the editor shows |

**Verified 2026-08-07** on a page markfluence published with `page_width: max`:

```
content-appearance-published -> ["max"]
content-appearance-draft     -> ["max"]
```

Both set, both agreeing. This is why `pagewidth.Apply` writes the pair rather
than just the published one.

## The vocabulary

Authors write the UI's words in frontmatter; the property takes a different set
of values:

| `page_width` | property value |
|---|---|
| `narrow` | `default` |
| `wide` | `full-width` |
| `max` | `max` |

**Transcribed** — the correspondence between the UI labels and the property
values comes from the original implementation and cannot be checked through the
API, which only shows the property value. Confirming it means setting each width
in the Confluence UI and reading the property back.

An older `fixed` value also appears in the wild and is surfaced as `narrow`.

Unset or blank `page_width` means `max`: the markdown file is the source of
truth for width. Note the asymmetry in `update` — it asserts the width only when
one was set by flag or frontmatter, and otherwise leaves the live page alone, so
a page whose width was set by hand in the UI is not silently overwritten.
