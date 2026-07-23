# Plan: arbitrary Confluence macros via shield/unshield

Let authors put **raw Confluence storage format** (`<ac:…>` / `<ri:…>` elements —
macros, layouts, and anything else) directly in a markdown file and have it emitted
verbatim into the published page, with surrounding/embedded markdown still
converted.

## Background — why this is hard

marko (any CommonMark parser, really) mangles raw `ac:`/`ri:` tags, because the
CommonMark HTML grammar forbids colons in tag names and treats `ac:` as an autolink
URI scheme. Empirically:

- `<ac:structured-macro>` → HTML-escaped (`&lt;…&gt;`), and `<ac:rich-text-body>`
  even becomes `<a href="ac:rich-text-body">` (autolink).
- Only **HTML comments** pass through cleanly, which is why the existing directives
  (`confluence-toc`, the removed `ac:layout`/note/chart) rode in comments.

Approaches considered and rejected:

- **A markfluence macro dialect** (`<!-- cf:macro name=… -->body<!-- /cf:macro -->`):
  friendly, but forces authors to learn a dialect and we own param/body encoding.
- **Comment-wrapped raw passthrough** (`<!-- cf:raw … -->`): copy-paste-able but
  requires the comment wrapper.
- **Switching markdown parsers:** the mangling is CommonMark-spec behavior, so
  markdown-it-py / cmarkgfm / mistletoe all do the same. Not a parser problem;
  switching would mean rewriting the transform pipeline for no gain.

## Chosen approach — shield the colon across marko only

The colon in the tag/attr name is the sole reason marko chokes. So, **only around
`gfm.convert()`**, rename `ac:`/`ri:` to a colon-free sentinel; marko then treats
`<MFACstructured-macro>` as an ordinary (pass-through) HTML tag. Restore immediately
after. This lets authors paste storage format straight from Confluence's "View
storage format" — no dialect, no wrapper.

```
md  --shield-->  md'  --gfm.convert-->  html'  --unshield-->  html  --> rest of pipeline
```

Shield/unshield wrap **only** the marko call. Every other transform in
`md_to_confluence` then operates on real `<ac:…>` tags (so links/images inside a
pasted macro body get rewritten normally, code fences behave, etc.).

### Sentinel scheme (collision-proof, per document)

- Distinct alphabetic bases, `MFAC` for `ac:` and `MFRI` for `ri:`.
- If a base already occurs in the raw source, grow it by inserting an `F`
  (`MFAC` → `MFFAC` → …) until it doesn't. Guarantees the sentinel isn't present in
  the source, so the round-trip can't corrupt real content (e.g. the literal text
  `MFAC` must not silently become `ac:`).
- Constraints: keep the sentinel **alphabetic** (so `sentinel + tagname` stays a
  valid HTML tag name — marko's whole reason for passing it through); keep the two
  sentinels **non-substrings** of each other.
- Shield = global string replace `ac:`→sentinel, `ri:`→sentinel (global is safe once
  collision-proof: prose/links round-trip identically). Unshield = a **single**
  regex-alternation pass back to `ac:`/`ri:` (one pass so replacement order can't
  corrupt one sentinel via the other).

Why global (not scoped to tag positions): with a collision-proof sentinel the
round-trip is transparent for prose and link destinations (verified: `[x](fileac:.md)`
is byte-identical shielded vs not), so the fiddly "only inside `<…>`" regex is
unnecessary.

## Authoring conventions (document in README)

- **Blank lines gate markdown conversion inside a macro.** A blank line between an
  `ac:` tag and content makes marko parse the content as markdown (→ `<p>`, lists,
  `<strong>`, links, images); content tight against the tags passes through
  literally. This is the same block-separation rule CommonMark uses everywhere, and
  it is what makes `<ac:layout-cell>` bodies convert.
- **Opening tag on its own line (or self-closed).** A macro written entirely on one
  line as `<open>…</close>` gets `<p>`-wrapped by marko; a self-closing tag or an
  opening tag alone on its line is emitted bare. Natural block-macro style already
  satisfies this, so this is guidance, not a code fix.
- **Storage examples in code fences stay literal** (escaped, not activated) for free.

## Removal

Drop `replace_layout_blocks` and the `<!-- ac:layout … -->` comment directive: shield
handles layouts in real storage format with converted cell markdown. Consistent with
the already-removed `confluence-note` / `chart` directives. Update the README
comment-directives section accordingly.

## Implementation

- `libmarkdown.py`:
  - Add `_shield_storage(md) -> (shielded_md, unshield_fn)` (or compute sentinels +
    a shield/unshield pair) implementing the sentinel scheme above.
  - In `md_to_confluence`, wrap the `gfm.convert()` call: shield → convert →
    unshield, before the existing transform sequence.
  - Remove `replace_layout_blocks` and its call.
- `README.md`: document raw storage-format support + the two conventions; remove the
  `ac:layout` directive bullet.
- `CLAUDE.md`: pipeline description — note shield/unshield around marko; drop
  `layout` from the directive list.
- Tests (`tests/`): pure, no network — sentinel growth on collision; global
  round-trip transparency for prose/links; a pasted macro passes through; a layout
  with blank-line-separated cell markdown converts; a code-fenced storage example
  stays escaped.

## Not doing

- No macro dialect, no comment-wrapped raw passthrough, no parser switch.
- No `<p>`-stripping cleanup transform (rely on the own-line convention).
