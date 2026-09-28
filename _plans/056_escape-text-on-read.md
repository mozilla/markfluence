# 056: escape text that `read` writes

Answers #203, widened to cover inline syntax as well as block markers.

`read` and `export` write a text node's characters straight into the
Markdown. Wherever those characters spell Markdown syntax, the next publish
turns them into that syntax, and the page's content changes with nothing
reported. #203 filed the block half (`<p>1. not a list</p>` publishes as a
list). Surveying it showed the same gap inline: literal `*not emphasis*`
publishes as `<em>`, and literal `<b>x</b>` as real markup. Both halves come
from one missing mechanism, so they are fixed together. Fixing only the block
half would leave the same silent content change in place.

Markdown is not at fault. CommonMark lets any ASCII punctuation character be
escaped with a backslash, and a Markdown writer is expected to escape text
that would otherwise read as syntax. `read` writes no escapes at all.

Principles: **L5** (`roundtrip-from-confluence`), which this violates today.
**L6**'s Accepts covers the result, since an escape is a difference in
spelling: `\*` in an exported file publishes the same `*` the author typed.

## What was checked

Each row publishes a Markdown string through `MdToConfluence` (goldmark with
`extension.GFM`, the forward converter as shipped). All rows are Verified on
2026-09-28.

**Text that becomes syntax today:**

| text, as `read` writes it | publishes as |
|---|---|
| `1. x`, `1) x`, `10. x` | an ordered list (`start="10"` for the last) |
| `- x`, `+ x`, `* x` | a bulleted list |
| `# x` | `<h1>` |
| `>x`, `> x` | a blockquote (no space needed) |
| `***`, `- - -` | `<hr />` |
| `a` + hard break + `---` | `<h2>a</h2>` (setext) |
| `a` + hard break + `===` | `<h1>a</h1>` (setext) |
| `a \| b` + hard break + `--- \| ---` | a table |
| `a` + hard break + `# b` | `<p>a</p><h1>b</h1>`: a heading can interrupt a paragraph |
| `~~~` at a line start | an empty code macro |
| `2*3*4`, `_x_`, `a _b_ c` | `<em>` |
| `a ~b~ c`, `a ~~b~~ c` | `<del>`: goldmark accepts a single tilde |
| `` `x` `` | `<code>` |
| `<b>x</b>`, `<!-- c -->` | raw HTML (a comment is then stripped by Confluence) |
| `<https://x.com>`, `<foo@bar.com>` | a link |
| `&copy;`, `&#169;` | `©` |
| `[x]: y` at a line start | nothing: a link reference definition, consumed |
| `[x]` with a matching definition elsewhere | a link |
| `- [ ] x` | a task-list checkbox |
| `https://x.com`, `www.x.com`, `a@b.com` | a link (GFM autolinking) |
| `## Item #` | `<h2>Item</h2>`: a trailing ` #` is a closing sequence |

**Text that is already safe**, which the escaping must leave alone so that
exported Markdown stays readable:

| text | publishes as |
|---|---|
| `~5 min to ~10 min` | text: neither tilde can close |
| `a * b * c` | text: a `*` with space on both sides cannot open or close |
| `snake_case_word`, `foo_bar_` | text: an intraword `_` cannot open or close |
| `a < b`, `a<b`, `<3`, `a <= b` | text |
| `AT&T`, `a & b` | text |
| `[x]`, `[x][y]` with no definition | text |
| `C:\path`, `a\b` | text: a backslash before a non-punctuation character is literal |
| `#x`, `C#` | text: `#` needs a space to open a heading |

**Every escape the decisions rely on publishes as the literal character:**
`1\.`, `1\)`, `\#`, `\>`, `\-`, `\+`, `\*`, `\_`, `\~`, `` \` ``, `\<`,
`\&copy;`, `\[`, `\]` (inside link text), `\\`, `\=`, `\|`, `## Item \#`,
`https\://`, `www\.`, `a\@b.com`. `\<ac:link>` publishes as the text
`<ac:link>` too, so the shield (which renames raw `ac:`/`ri:` tags before
goldmark sees them) does not interfere with an escaped one.

**One place an escape does not work:** image alt text. `![a\]b](…)` publishes
`ac:alt="a\]b"`, backslash included, because the forward converter takes the
alt from the source bytes rather than from goldmark's unescaped text. See
Not in scope.

## Decisions

**D1. Two layers: inline escaping on each text node, and a line-start pass
over each paragraph.** Inline syntax can be judged from a text node and its
neighbours within the node, so it is escaped where the text enters the
Markdown: `renderInline`'s text case, and the loose text `blockStrings`
handles. Block syntax depends on where a line starts, which only the assembled
paragraph knows: a line starts at the paragraph's start and after every hard
break, and text nodes do not know either. So the block half is a pass over a
paragraph's rendered text, split at hard breaks. The pass is safe on rendered
Markdown because nothing markfluence emits inline starts a line with a block
marker (D4 has the argument). Both live in a new `internal/convert/escape.go`.

**D2. Inline escaping depends on the characters around it, and a node's edge
counts as "could be anything".** Escaping every punctuation character would be
correct and unreadable (`snake\_case`, `a \* b`, `\~5 min`). Each rule below
escapes only where the character could take effect. A text node's first and
last characters sit next to whatever the neighbouring node renders, which the
node cannot see, so at an edge the rule assumes the worst and escapes.
Escaping too much is safe, since every escape round-trips as the literal
character. Escaping too little is this bug. So when a rule is unsure, it
escapes.

| character | escaped when | why |
|---|---|---|
| `\` | followed by ASCII punctuation, or last in the node | only then is it an escape; at the edge the next node's first character is unknown |
| `` ` `` | always | any backtick can open a code span |
| `*` | unless whitespace on both sides | a `*` between spaces is neither left- nor right-flanking |
| `_` | unless letters or digits on both sides, or whitespace on both | intraword `_` cannot open or close; between spaces it is not flanking |
| `~` | preceded by non-whitespace (or the node's start), or next to another `~` | every strikethrough needs a closer, and a closer is preceded by non-whitespace; a run of two could pair with an emitted `~~`. This keeps `about ~5 min` readable |
| `[` | always | `[x]: y` is a definition, `[ ] x` in a list item a checkbox, and `[x]` a link wherever a definition exists |
| `]` | inside link text only (D5) | elsewhere a lone `]` is inert, and brackets are common in prose |
| `<` | followed by a letter, `/`, `!` or `?` | the starts of a tag, a comment, a declaration and an autolink; `a < b` and `<3` stay |
| `&` | followed by an entity (`&name;`, `&#nnn;`, `&#xhh;`) | `AT&T` stays |

**D3. The line-start pass.** For each line of a paragraph (its first line, and
each line after a hard break), after skipping leading spaces, the first
character is escaped when the line:

- starts with 1–6 `#` followed by a space or the end of the line (`\#`);
- starts with `>` (`\>`);
- starts with `-` or `+` followed by a space or the end of the line (`\-`,
  `\+`; `*` is already covered by D2's rule);
- starts with 1–9 digits then `.` or `)` followed by a space or the end of the
  line: the delimiter is escaped, as in `1\.` and `1\)`;
- is a thematic break (three or more `-`, `*` or `_`, alone or separated by
  spaces), a setext underline (only `=` or only `-`), or a table delimiter row
  (only `|`, `:`, `-` and spaces, holding at least one `|` and one `-`);
- starts with `~~~` or ```` ``` ```` (after D2 a text-origin fence is already
  escaped; the pass covers any other way to reach it).

A heading's text is not a paragraph, since ATX heading content cannot start a
block, so the pass does not apply to it. D6 handles headings.

**D4. Why the pass cannot damage markup markfluence emits.** An inline
construct markfluence writes starts with `*`, `**`, `~~`, `` ` ``, `[`, `![`,
`<` (raw storage, or a heading's `<br />`) or `\` (an escape from D2). None of
them matches D3:
- a mark is never followed by a space, because `renderMark` moves edge
  whitespace outside its delimiters (#204);
- a whitespace-only mark renders as its whitespace, so no `***` or `****` is
  emitted;
- nothing markfluence emits inline starts with a digit, `#`, `>`, `-`, `+`,
  `=`, `|` or `:`.

The pass splits only at hard breaks (`"  \n"`), never at every newline. So a
raw serialized inline element whose CDATA holds a newline is never split, and
its content never gains a backslash.

**D5. Link text escapes `]` too, and `escapeLinkText` becomes the full
escaper.** Today `escapeLinkText` escapes `\`, `[` and `]` and nothing else.
Its comment records that a title like `*Foo*` renders as emphasis and was
knowingly left alone. With escaping at the text node, link text needs only one
addition: `]`, which ends a link's text early. The renderer tracks whether it
is inside link text (a depth counter on `mdRenderer`, set by
`inlineTextForLink`). `inlineTextForLink`'s `onlyText` split goes away: it
existed so that already-rendered markup was not escaped, and text is now
escaped at its source, before any markup is added around it. The raw sources,
which are strings rather than text nodes, go through the same escaper with the
link rule on: a page title, a space key, an anchor, a CDATA link body, a
fallback, and a mention's display name.

`MentionMarkdown` therefore changes for a display name holding `*`, `_`, `` `
``, `<`, `&` or `~`. `user-find` prints the same function's output, so the
guarantee that the line an author pastes is byte-identical to what `read`
writes holds by construction. The line itself changes for those names.

**D6. A heading's closing sequence.** `## Item #` publishes as "Item". When a
heading's rendered text ends in whitespace followed by one or more `#`, the
first `#` of that run is escaped: `## Item \#`. `C#` has no space before the
`#`, so it is not a closing sequence and stays as it is.

**D7. Bare URLs and email addresses are escaped out of autolinking.** GFM
autolinks `http://`, `https://`, `ftp://`, `www.` and email addresses, so
plain text holding a URL publishes as a link. An escape suppresses it
(Verified): `https\://`, `www\.`, `a\@b.com`. This is the one rule whose cost
is visible, since `https\://example.com` reads badly in an exported file. It
is included anyway:
- storage holds a URL as plain text only when someone chose that; the editor
  turns a typed URL into a link;
- turning it into a link is a content change, of the kind this plan exists to
  stop;
- without it, a page holding one is not a fixed point after one read.

It escapes the `:` of `scheme://` for the three schemes goldmark links, the
`.` of a `www.` that starts a word, and the `@` of anything goldmark's email
pattern would match.

**D8. What is never escaped:**
- **code spans and code blocks:** their text is literal already;
- **link destinations:** they are URLs, encoded by `encodeDestination`;
- **raw storage:** `serialize` and `renderRawBlock`'s tags go through
  `xmlTextEscape` or pass through verbatim;
- **the TOC token, frontmatter and image alt text:** frontmatter is YAML with
  its own writer; alt text is covered in Not in scope.

Text inside a raw block's content container, such as a raw table cell or a
layout cell, is Markdown and is escaped like any other: those bodies go
through `blockStrings`.

**D9. Pipe-table cells get the inline layer and not the line-start pass.** A
GFM table cell is inline, so `1. x` in a cell is text. `renderCellLines` goes
through `renderInlineChildren`, which picks up D2 without any change.
`escapeCellPipe` still runs afterwards, and D2 escapes no `|`, so the two do
not compound.

## Implementation

1. `escape.go`: `escapeText(s string, inLink bool) string` (D2, D5, D7) and
   `escapeLineStarts(s string) string` (D3). Both are pure functions over
   strings, tested on their own.
2. `renderInline`'s text case and `blockStrings`' loose text call
   `escapeText`, passing `r.linkDepth > 0` as `inLink`.
3. A single helper, called wherever a paragraph's rendered text becomes a
   Markdown block, applies `escapeLineStarts`. The call sites:
   - `renderBlock`'s `p` case and its inline-element-at-block-level case;
   - `renderListItem`'s text segments;
   - `blockStrings`' loose text;
   - `rawCellBlocks`' paragraphs and loose runs.
   Callouts and layout cells reach it through `blockStrings`, and each call
   site gets a test.
4. Headings apply D6.
5. `inlineTextForLink` increments `linkDepth` and drops the `onlyText` split.
   `escapeLinkText` becomes `escapeText(s, true)`, and its comment is
   rewritten.
6. The table property generator drops its #203 restriction and gains words
   holding the characters above. `markText` stays as #204 left it.

## Tests

- **Unit tests** for `escapeText` and `escapeLineStarts`, one row for each
  rule and each "stays readable" row in What was checked. Each row checks the
  escaped form *and* that publishing it gives back the original text, so no
  rule can emit an escape that does not round-trip.
- **A `storage2md/escaping` case** with one paragraph per row of the first
  table, plus list items, a raw table cell, a layout cell, a callout, a link
  text, a heading ending in ` #`, and a mention whose name holds `*`. Being a
  storage2md case it also runs through `TestRoundTripMarkdownIsAFixedPoint`
  and the output-parses-as-Markdown check.
- **Existing goldens** that hold these characters in text change. Each change
  is reviewed and listed in the PR: every one should be a new backslash and
  nothing else.
- **The property test** with the restriction lifted. It must fail against
  `main`'s converter and pass with the fix, the way #204's did (checked by
  swapping in `main`'s `storage_to_md.go`, not by stashing).
- `user-find`'s test that its line matches `read`'s still passes unchanged.

## Documentation

- `docs/markdown-file.md`: a short section saying that exported Markdown
  escapes text that would otherwise read as Markdown, with the D7 cost stated,
  so an author who sees `\*` or `https\://` in an exported file knows why.
- `CLAUDE.md`: the converter bullet gains `escape.go` and the two layers. The
  regression-suite paragraph drops #203 from the gaps the generator avoids.
- `escapeLinkText`'s comment, which records the `*Foo*` gap as known, is
  rewritten.

## Not in scope

- **Image alt text.** The forward converter reads alt from the source bytes,
  so an escape there publishes the backslash. A `]` in alt text already breaks
  the image today. Fixing it means unescaping on the forward side first, a
  change to what `update` publishes for existing files, and it deserves its
  own issue.
- **A code span holding a backtick.** `renderInline` writes `` `x` `` with a
  single backtick whatever the content, so code holding a backtick breaks. It
  is a separate, older gap with a known fix (a longer fence), not escaping.
- **The mention marker.** A link to an Atlassian Home profile whose text
  starts with `@` publishes as a mention, by design (#91). Escaping that `@`
  would break the convention for real mentions.
- **#213** (two hard breaks in a row) and **#214** (an empty paragraph before a
  list in a cell) are left as they are, although both are in the same
  functions.

## Amended during implementation

- **Raw storage written inline is escaped (D8 was wrong about it).** D8 held
  that raw storage needs no escaping, which is true of a raw *block*: an HTML
  block's content is not parsed. It is not true inline. The text between
  inline HTML tags is Markdown, so `_x_` in a status macro's title, in a list
  in a pipe-table cell, or in a passed-through `<ac:link>` published as
  `<em>`. `serializeInline` escapes those text nodes (`escapeRawText`, which
  leaves `<` and `&` to `xmlTextEscape`).
- **Whitespace at a node's edge counts as unknown.** D2 treated only the
  node's edge as unknown. But `renderMark` moves a mark's edge whitespace
  outside it (#204), and publishing then stores that whitespace in the
  neighbouring node, so a decision that trusted it changed between one read
  and the next. The property test found it, with a backslash before a space
  the mark gave away going unescaped and then escaping the closing
  delimiter. For the same reason, adjacent text nodes (which
  `coalesceSplitMarks` leaves when it merges two runs of one mark) are
  merged before rendering.
- **D3's thematic break repeats one character.** A pattern allowing a mix
  matched `**---**`, bold markup markfluence emits, contradicting D4.
- **D6 covers a heading that is only `#`**: `### #` is an empty heading with a
  closing sequence.
- **From the code review:**
  - **Heading escapes reached anchor slugs.** `linkindex.extractHeadings`
    builds a heading's slug from its Markdown source line, so
    `## Setup \[beta]` made links point at `#Setup-\[beta]`, an anchor
    Confluence never creates. It now removes backslash escapes first, since
    Confluence builds the anchor from the published text.
  - **Transparent wrappers are flattened before escaping.** A `<span>`,
    `<u>` or `<sup>` renders as its children, so text it split was escaped
    in pieces: an address, URL or entity split by one was missed, and
    `<u>a_</u>b` escaped differently from the `a_b` read back next time.
  - **A tilde at a node's end is written as `&#126;`.** It may meet a
    del mark's `~~`, and goldmark counts `\~~~` as a run of three however
    the first is escaped, so the strikethrough was lost. A character
    reference is outside any run.
  - **Raw storage written inline encodes a newline as `&#10;`**, and
    escapes `]` inside link text. A real newline let goldmark start a
    block at the next line, or read the element as an HTML block whose
    escapes then published. A raw block's `hasLooseText` form is one line,
    so at the top it is inline too and is escaped; nested in a wrapper it
    is inside that wrapper's HTML block and is not.
- **#216** was filed for the punctuation half of CommonMark's flanking rule
  (`a**(b)**c`), which escaping cannot fix. The property test's generator
  keeps a mark's text from starting or ending with punctuation, and it
  keeps backticks out of code spans (the older gap listed in Not in scope).

## Commits

Each commit passes `make check` on its own. So `escape.go` arrives with its
first caller, because golangci-lint's `unused` check fails a function nothing
calls.

1. `docs(plans): 056 escape text that read writes`
2. `fix(convert): escape inline syntax in text on read` (`escape.go`'s
   `escapeText`, D2, D5, D9, unit tests, goldens)
3. `fix(convert): escape block markers at a line start on read` (D3, D6)
4. `fix(convert): keep bare URLs as text on read` (D7)
5. `test(convert): let the table property test write block markers`
6. `docs: what read escapes`
