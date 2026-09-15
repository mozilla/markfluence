# 042 — `markfluence diff FILE`

Closes #154.

`update` can now tell "the page differs because I have edits" from "the page
differs because somebody published first" (#149, `_plans/041`). It refuses the
second case with `CodeConflict` and points the author at — nothing. There is no
way to see *how* the page moved. `read` prints the page, `export` writes a tree,
and neither compares either against the file you are holding.

`diff` is that remedy: fetch the page, render its body to markdown the way
`export` does, and diff that against the file on disk. Nothing is written to
disk and nothing is written to Confluence.

The motivation is in the issue and is not repeated. This plan records where the
design moved since the issue was written, then the design as it will be built.

## The output is two things, on two streams

The issue's shape is one unified diff over the whole document, frontmatter
included. That cannot be made appliable, and being appliable is worth more than
the uniformity:

```sh
markfluence diff docs/runbook.md > my.diff
patch -R -p1 < my.diff          # pull Confluence's body edits into the file
meld <(markfluence diff ...)    # or any tool that eats a unified diff
```

For `patch` to work, the `+++` side has to be the **bytes on disk**. A composed
frontmatter side never is: a pristine manifest-managed file (#139) has no
frontmatter block on disk at all while its rendered counterpart has a full one,
so `patch` finds no matching context and bails; and a file carrying a key
markfluence preserves but does not model (`reviewers:`) would lose it on apply,
since `RenderFrontmatter` has a fixed field list.

So the two halves are reported in the two registers they actually belong in:

| half | register | stream |
|---|---|---|
| body | a real unified diff, appliable by `patch -p1` | **stdout**, alone |
| frontmatter | a per-field report, with provenance | **stderr** |

**Nothing but the diff goes to stdout.** That is what makes `> my.diff` produce
a valid patch, and it is the existing `ui.Info`/`ui.Hint` rule rather than a new
one — `children --space` already puts its hint on stderr "so the table stays
pipeable", and this is the stronger case. On a terminal the two streams
interleave and a reader sees one report; redirected, they separate cleanly.

A frontmatter value difference is a one-line fact, not a hunk, so the report is
better output for a human anyway — and it is the only register that can carry
provenance, which a diff hunk cannot express at all.

### What it looks like

```
frontmatter differs:
  title     confluence "Deploy Runbook"
            local      "Deploy Runbook (2026)"   frontmatter
  labels    confluence [runbook]
            local      [runbook, oncall]         markfluence.yaml

--- confluence/docs/runbook.md
+++ local/docs/runbook.md
@@ -18,7 +18,7 @@
 Restart the worker pool:

-    kubectl rollout restart deploy/worker
+    kubectl rollout restart deploy/worker -n prod
```

## What changed since the issue

1. **The output is split**, as above. The issue's "the diff covers frontmatter
   and body" still holds; the registers differ.
2. **Exit codes follow `diff(1)`**: `0` identical, `1` differs, `2` any trouble.
   The issue did not settle this.
3. **The labels are paths**: `confluence/<path>` and `local/<path>`, the git
   `a/`/`b/` convention, so `patch -p1` strips to the real file and the output
   is self-identifying when pasted into an issue.
4. **A field the local side does not declare is not compared.** The issue
   accepted a list of frontmatter noise; most of it disappears under one rule,
   and the rule is not a trick — it is what makes the report mean "what
   publishing would change".
5. **A failed page-width or label fetch reports the field as uncomparable**
   rather than as a difference, because a fabricated difference is worse than a
   missing one.
6. **`pagemeta` gains per-field provenance.** The issue wanted a frontmatter
   difference to say where the local value came from; `Resolved` records only an
   aggregate `Source` today.

## Exit codes

```
0  identical
1  differs (either half)
2  trouble (bad flag, unresolvable file, no page_id, page not found,
   credentials rejected, fetch failed)
```

This is `diff(1)` and `git diff --exit-code`, and it is a **deliberate
departure** from `docs/json-output.md`'s contract, where `1` is "an operation
failed" and `2` is "fatal/pre-flight". The departure is the point: an exit code
is the only thing a shell script has, and

```sh
if markfluence diff docs/runbook.md >/dev/null; then echo "in sync"; fi
```

is the whole reason the command is worth having in CI. Making "differs" exit `0`
would force every caller through `--json | jq .results[0].differs`.

The cost is that `diff`'s *operational* failures exit `2` where `read`'s exit
`1`. That is stated in the command's `Long`, and `docs/json-output.md` and the
README's exit table grow a sentence naming `diff` as the one command whose codes
are the `diff(1)` ones. It is not ambiguous in practice — `2` is "markfluence
could not answer", `1` is "markfluence answered, and the answer is that they
differ" — and the typed error object on stderr still says what went wrong.

Note that **only frontmatter differing still exits `1` with empty stdout**. That
is correct and has to stay: `differs` is about the page and the file, not about
whether a patch was produced.

## The body diff

Two documents, each `frontmatter-block + body`, sharing the frontmatter block
**byte for byte**:

```go
prefix := mf.Content[:len(mf.Content)-len(mf.Body)]  // "" for a file with none
lead   := leadingNewlines(mf.Body)                   // the author's blank line

localDoc      := prefix + lead + mf.Body[len(lead):]         // == mf.Content
confluenceDoc := prefix + lead + trimLeadingNewlines(rendered)
```

Three things this buys, none of them incidental:

- **Hunk line numbers are file-relative.** Diffing the bodies alone would number
  hunks from the body's first line, and `patch` would apply them with a "Hunk #1
  succeeded at 23 (offset 18 lines)" fudge. Including the frontmatter as
  identical context makes the numbers simply correct.
- **No hunk can touch the frontmatter**, because it is identical on both sides.
  Frontmatter differences are reported separately, and a patch can never
  rewrite a `page_id`.
- **A file with no frontmatter block needs no special case**: `prefix` is empty
  and the numbers are already right.

`lead` — the newlines the author left between the delimiter and the first line
of prose — goes into the shared prefix rather than into either body, so the
joint is identical on both sides and a file with two blank lines there does not
diff against a rendered side with one.

The **local side is never normalized** — not even a missing final newline. That
was the plan's first answer and it is wrong: padding it would put a line in the
last hunk's context that the file does not have, and the patch would not apply.
Unified diff already has the `\ No newline at end of file` marker for exactly
this, and `patch` understands it. Only the rendered side, being synthetic, is
given one trailing newline.

### `--reverse`

`--- confluence` / `+++ local` reads as what publishing would do to the page,
which is the primary question (`update`'s companion: I am about to publish, what
changes?). But the file on disk *is* the `+++` side, so the patch applies
Confluence→file only in reverse — `patch -R -p1`.

`--reverse` swaps the two sides and their labels, so the other workflow needs no
`-R`:

```sh
markfluence diff --reverse docs/runbook.md | patch -p1
```

It affects **only the diff**. The frontmatter report names both sides on every
line, so it has no direction to reverse, and flipping its columns under a flag
would make two runs of the same command disagree about which column is which.

### Placement

`pagedoc.Placement` decides where a rendered page thinks it sits. `read` renders
as though at the top level of nothing; `export` renders for a position in a
tree. `diff` renders **as though exported to exactly where the file already
is**, which the existing type expresses without change:

| field | value | why |
|---|---|---|
| `Dir` | `path.Dir(key)`, `""` at the root | `PageDir` is the file's directory relative to the documentation root, which is what a recorded attachment `path=` is relative to. Get it wrong and every sourced image diffs. |
| `AttachmentDir` | left empty | Derives `Dir/<slug>`, matching where `attachment-download` puts an unrecorded attachment. One added in the editor therefore shows as an added image reference, which is a real difference. |
| `Attachments` | left nil | One page; `Options` fetches its own listing only if the body references one. |
| `Parent` | **unused** | Nothing renders frontmatter here, so there is nothing for it to override. The parent is compared as a value instead — see below. |

The body comes from `pagedoc.Options` plus `convert.StorageToMarkdown`, the way
`cmd/read` calls it. Deliberately **not** `pagedoc.Render`, whose
`pagedoc.Frontmatter` would fetch page width and labels a second time on top of
the reads the frontmatter report already makes.

## The frontmatter report

Field by field, comparing **values**, and only for fields the local side
declares.

### The rule that kills the noise

> **A field the local side does not declare is not compared.**

Not a convenience. The report says what publishing would change, and `update`
does not touch a field the file does not declare. That is the existing,
documented asymmetry of every declarative field:

- `labels` absent means untouched (`internal/labels`, **L9**); `labels: []`
  means remove them all, and *that* is a real difference worth reporting
- `page_width` absent means the default is not asserted
- `title` absent means the live page's title is kept (`update` honours this)
- `parent` and `space` absent mean the page is not moved

So a file declaring only `page_id` and `title` reports on its title and nothing
else, and the issue's list of frontmatter noise reduces to nothing. `page_id` is
identical by construction — it is how the page was found — so it is never a
difference; it names the page in the report's header line instead.

### Two declaration sites the plan missed

**`page_width` has a third one.** The project file's own `page_width:` setting
(#100) is a real declaration that `update` acts on — `resolveWidth` applies it
to a file that declares none — so a file with no `page_width` of its own still
has one asserted, and not comparing it would under-report. It is labelled
`markfluence.yaml (project default)` rather than plain `markfluence.yaml`,
because it is a different place in the same file than a `pages:` entry and the
label's whole job is to say where to go and edit. (`space` has the same chain in
`create`, but `update` never sends a space at all, so there is nothing to
inherit.)

The rule is therefore a third copy of a two-step that also lives in `update`'s
`resolveWidth` and `check`'s project-default lint. It cannot be shared from
`internal/pagewidth`, which cannot import `internal/project` (`pagewidth` →
`client` → `project`), and a new package for six lines would be worse.

**`space` and `parent` are compared but reconciled by nothing.** `UpdatePage`
sends neither a `spaceId` nor a `parentId`, so no verb moves a page today (#10).
They are still compared — the file and the page disagreeing about where the page
*is* is worth knowing, and the fix is by hand — which means "what publishing
would change" is the rule for *which fields to compare*, not a promise about
every row. Said once in `Long` and once per row as a `note`, rather than
silently dropping two fields a reader would expect.

### The local side goes through `pagemeta`

Not the file's raw frontmatter map:

```go
key, _ := pagemeta.KeyFor(root, abs)
meta, err := pagemeta.Resolve(key, mf, root)
```

This is what `internal/pagemeta` exists for. A pristine file whose metadata
lives in a `pages:` entry declares its title there, and a report reading only
`mf.Frontmatter` would say the title is undeclared and compare nothing. Going
through `Resolve` makes the report about **values, not location**.

`Resolve`'s errors and warnings are honoured as everywhere else: a coordinate
disagreement is a `2` (there is no single local answer to compare against), and
a soft disagreement is a warning.

### `parent` is compared as an id

A file that came out of an export tree spells its parent as a relative path to
the parent's own `.md`; the live page's parent is an id. Comparing the spellings
would report a difference on every such file.

So a local `parent:` ending in `.md` is resolved to a page id — `ParseFile` plus
`pagemeta.Resolve` against the same root, the resolution `create` already does
for a `.md` parent, with no API call — and *that* is compared against the page's
`ParentID`. The report shows the local value as written, with the resolved id
beside it, so a reader can see why it matched or did not. A path that resolves to
nothing is reported as such rather than as a plain difference.

### Provenance, and the `pagemeta` change

Each reported field names where its local value came from: `frontmatter`,
`markfluence.yaml`, or both. Without it, "the title differs" is ambiguous about
which file to edit — and when both locations agree, the answer is *both*, which
is exactly when it matters most.

`pagemeta.Resolved` records only an aggregate `Source`, so it gains a per-field
map:

```go
// Origin says which location supplied each field's effective value, keyed by
// field name across both Fields and Lists (a key is in exactly one map).
Origin map[string]Source
```

`Resolve` already knows this at each branch of its grading; the map is filled
where the decision is made, including in the no-entry fast path, where every
declared field is `FromFrontmatter`. `FromBoth` means both locations supplied the
same value.

Computing it in `cmd/diff` instead was the alternative and is the wrong call:
`internal/pagemeta` exists precisely because "a per-command copy is how two
commands come to publish one file to two different pages", and a second copy of
the precedence rules is a second copy whatever it is used for.

`MetadataSource()` is untouched, so nothing else changes.

### A failed fetch is uncomparable, not different

`pagewidth.Read` and `labels.Read` are best-effort, and `read`/`export` omit the
field when they fail — right for them, actively misleading here: the file
declares `labels: [runbook]`, the fetch failed, and the report says publishing
would *add* the label. It may already be there.

So a read failure reports the field as uncomparable, naming what could not be
fetched, and does **not** count toward `differs`. A missing comparison a reader
is told about beats a fabricated one they are not.

## The round-trip note

**L5** (`roundtrip-from-confluence`) and **L6** (`roundtrip-from-disk`) are both
**Partial**, so a page exported and republished unedited is not guaranteed to
produce byte-identical markdown. Without saying so, this command generates a
stream of "diff shows a change I did not make" reports.

Two places, deliberately both:

- the command's `Long`, at length, listing the known shapes
- one `ui.Hint` line on stderr **when the body diff is non-empty**, pointing at
  `markfluence diff --help`

Not on every run: a hint printed when the answer is "identical" is a line people
learn to scroll past, and then it is not there when it matters.

The shapes to expect, now only body ones — the issue's two frontmatter items are
gone, since frontmatter is no longer compared as text:

- table alignment: Confluence has no explicit left, so `:---` publishes bare and
  reads back as `---`
- a column's alignment is per-paragraph in storage and per-column in GFM, so
  `columnSeparators` takes the most common declared alignment and drops the rest
- `coalesceSplitMarks` hoists a mark the editor split, so a bold-containing link
  may come back spelled differently
- a soft break becomes a space
- a table cell colour outside the 21 named swatches comes back as a literal hex
- a macro markfluence does not map comes back as raw storage tags

This is also the reason `patch -R` is documented rather than recommended: it
applies the *rendering* of the page, round-trip losses included. Reviewing the
diff first is the point of the command.

## The diff library

`github.com/aymanbagabas/go-udiff` v0.4.1. Checked against the issue's criteria:

| | |
|---|---|
| unified output | `udiff.Unified(oldLabel, newLabel, old, new) string`, and `ToUnifiedDiff` for structured hunks |
| maintained | last pushed 2026-07-16 |
| dependencies | **zero**, direct or transitive |
| licence | BSD-3-Clause |
| size | one package, derived from Go's own `internal/diff` (the Myers implementation `gofmt` uses) |

The author (`aymanbagabas`) is already in the tree via `go-osc52`, which is not
a reason to pick it but does mean the name is not new. `gotextdiff` is a 2023
fork of the same lineage with no commits since; `go-difflib` is archived;
`go-diff` is character-oriented and larger than the job. This adds the project's
**sixth** direct dependency.

`ToUnified` — the canonical string — **not** `ToUnifiedDiff`'s structured hunks,
which was the plan's first answer and is the wrong one. Re-rendering hunks means
reimplementing the `@@ -a,b +c,d` arithmetic, which is precisely the surface a
library was chosen to avoid getting subtly wrong. So the library's own output is
what is printed, and a one-line classifier (`classify`) decides each line's
colour and whether it counts. What is counted cannot disagree with what is
printed, because it *is* what is printed.

The `---`/`+++` labels and `--reverse` need no string surgery either way, since
both are arguments to `ToUnified`. Colour goes through `internal/ui`'s new
`DiffAdded`/`DiffRemoved`/`DiffHunk`, so `NO_COLOR` and a non-tty stdout already
behave.

**`--json`'s `diff` string is uncoloured**, whatever the terminal is doing.

## `--json`

Everything goes in the payload, both streams' worth, since stderr under `--json`
is a schema-validated document with no room for a stray line.

```json
{
  "ok": true,
  "file": "docs/runbook.md",
  "page_id": "1234567890",
  "title": "Deploy Runbook",
  "url": "https://org.atlassian.net/wiki/spaces/ENG/pages/1234567890/...",
  "metadata_source": "manifest",
  "differs": true,
  "body_differs": true,
  "added": 3,
  "removed": 1,
  "diff": "--- confluence/docs/runbook.md\n+++ local/docs/runbook.md\n@@ ...",
  "frontmatter": [
    {
      "field": "title",
      "confluence": "Deploy Runbook",
      "local": "Deploy Runbook (2026)",
      "source": "markfluence.yaml",
      "comparable": true
    }
  ]
}
```

Per the schema discipline: every field on a typed struct, no `omitempty`, and
`frontmatter` **initialized to an empty slice** so it marshals as `[]` rather
than `null` when nothing differs. `differs` is `body_differs || len(frontmatter
where comparable) > 0`.

`confluence` and `local` are strings, a list rendered in flow form (`[a, b]`).
A consumer wanting the array of labels would want them typed; that waits for a
consumer to exist, per the `_plans` convention, and the strings are what the
human report shows.

`comparable: false` is the failed-fetch case, where `confluence` is `null` and
the field does not count toward `differs`.

Only differing fields are listed — an agreeing field is not a row — so
`frontmatter` is exactly the report.

New `$defs`: `diffResult` and `frontmatterDifference`. `summary` is
`basicSummary`, `total` always 1; no `differs` count is added there, since with
one file it would restate `results[0].differs`.

Trouble is a `results[0]` failure (`singleOpFailure`) for anything naming the
page and a stderr `errorObject` for anything before it, matching `read` — but
both exit `2` here.

## Files

```
cmd/diff/diff.go        the command: flags, resolution, orchestration
cmd/diff/frontmatter.go the per-field comparison
cmd/diff/body.go        the two documents, the diff, the line classifier
cmd/diff/report.go      human output: which half goes to which stream
cmd/diff/json.go        diffResult and the envelope builder
cmd/diff/*_test.go
internal/pagemeta       Resolved.Origin
internal/ui             DiffAdded / DiffRemoved / DiffHunk
```

No new `internal` package. Nothing here is shared with another command yet, and
`internal/pagedoc` already owns everything that is.

## The new-subcommand checklist

Each of these has a test that fails without it:

- `Long` **and** `Example` — `cmd`'s `TestSubcommandsDocumentThemselves`
- `ValidArgsFunction: completion.MarkdownFiles` — `TestSubcommandsCompleteArgs`
  (the argument is a FILE, like `check`, not `read`'s PAGE-or-file)
- registration in `cmd/root.go`
- `"diff"` in the schema's `command` enum **and** an `if`/`then` branch
  constraining `results.items` and `summary` — the enum entry alone leaves the
  command completely unvalidated (`internal/schematest/document.go`), and adding
  only the enum entry is exactly how a new command's conformance test goes green
- `TestCommandEnumMatchesRegisteredCommands` closes the same loop from the `cmd`
  side
- a conformance test building its document with the command's **own** builder
  (`failEnvelope`, `jsonResult`), not a hand-copied literal
- `make docs` for `docs/commands/markfluence_diff.md`
- `docs/json-output.md`: the `diff` entry, the two-stream split, and the
  exit-code departure
- `CLAUDE.md`: a `cmd/diff/` bullet, `diff` in the `cmd/{...}` list, and
  `Origin` on the `internal/pagemeta` bullet
- `README.md`: one row in the command table, `diff` in the completions command
  list, and a note on the exit-code table naming `diff` as the one command where
  `1` means "differs". It is a 50,000-foot view (#102), so nothing more.

## Guarantees

Nothing changes status. `diff` writes nothing, so no safety guarantee is in
play; it **reports** rather than resolves, so it does not move L5/L6 off
**Partial** — it makes their Partial-ness visible, which is the honest framing
and is why the round-trip note exists at all.

No new guarantee id is minted. "The diff is what publishing would change" is
tempting as one, but it is only true modulo L5/L6, and a guarantee that holds
modulo two Partial guarantees is not a guarantee.

## Tests

- **stdout holds nothing but the diff.** The test that protects the patch
  workflow: a file with both halves differing, captured streams, and stdout
  parsed as a unified diff.
- **`patch` applies it.** `patch -R -p1` against a copy of the tree reproduces
  the Confluence side's body exactly, with the frontmatter block untouched.
  A real `exec.Command("patch", ...)`, skipped when `patch` is absent.
- **hunk numbers are file-relative**, for a file with a five-line frontmatter
  block: the first hunk's `@@` names the line a reader would count to in an
  editor.
- **the manifest case**: a pristine file with a `pages:` entry and no
  frontmatter block reports **no** frontmatter difference when the entry agrees
  with the page, and reports `markfluence.yaml` as the source when it does not.
  This is the test that would have caught the naive implementation, and it is
  the point of the command being written at all.
- **the undeclared-field rule**: a file declaring only `page_id` never reports a
  `page_width`, `labels`, `space`, `parent` or `title` difference, whatever the
  page says.
- **`labels: []` is not "undeclared"**: it is a removal and is reported.
- **the failed fetch**: a stubbed page-width/label failure reports the field
  uncomparable and does not set `differs`.
- **provenance**: `frontmatter`, `markfluence.yaml` and both, the last being the
  agree-in-two-places case.
- **the parent rule**: a file whose `parent:` is `../index.md`, where
  `index.md`'s `page_id` is the live parent, reports no difference; the same file
  pointing at a different page does; one pointing at a file that does not
  resolve says so.
- **`Dir`**: a file in a subdirectory with a recorded `assets/brand.png`
  attachment renders `../assets/brand.png` and reports no difference.
- **`--reverse`** swaps the sides and the labels and leaves the frontmatter
  report's columns alone.
- **exit codes**: `0`/`1`/`2` for identical, differing, and each class of
  trouble — including frontmatter-only differing, which is `1` with empty
  stdout.
- **`--json`**: `differs`, the counts, `frontmatter` as `[]` when nothing
  differs, and that `diff` is uncoloured with a style active.
- **`pagemeta.Origin`**: its own tests in that package, covering the fast path.
- **every source label validates**: the report's four spellings are checked
  against the schema's `source` enum through the command's own `sourceLabel`,
  so adding a fifth fails here rather than in somebody's `--json` consumer.
- **the conformance test**, per the checklist.

Verified live as well, against the standing fixture page in the personal space
(2026-09-14): a page exported with `read` and diffed unedited reports **nothing
on either stream and exits 0** — so the round-trip noise on a real
markfluence-authored page with tables, macros, callouts and a TOC is zero in
practice, not merely bounded. With a title, a width, a label and two body lines
edited, the report and the patch are as designed, and `patch -R -p1` applied the
patch to the real file, reverting the body while leaving the frontmatter edits
untouched.

Tests stub the client the way `cmd/read` and `cmd/export`'s do; no live instance
is needed for any of the above.

## Not in scope

- **Multiple files.** One file, deliberately — the opposite of `update`/`check`.
  A glob would produce output nobody reads, and a patch spanning files would
  need `diff -r` semantics nobody asked for. `status` (#148) is the tree-wide
  view.
- **Three-way merge.** The action log knows what version this copy was derived
  from (#149), which is the base a real merge needs, and `patch`/`meld` on the
  two-way diff is the 80% answer. A `--merge` that writes conflict markers into
  the file is a writing verb and belongs with #148/#151 reasoning, not here.
- **`--body-only` / `--frontmatter-only`.** The split already separates them by
  stream, which covers most of what the flags were for: `2>/dev/null` and
  `>/dev/null` respectively.
- **Structured hunks in `--json`.** Waiting for a consumer; the `diff` string
  plus counts is the commitment.
- **Applying the diff from inside markfluence**, in either direction. Writing
  the page is `update`; writing the file is `export`; `patch` does the rest.
- **Attachment binary comparison.** A missing or differing attachment is a
  different report.
- **Reporting divergence alongside the diff.** `diff` could add "the page is at
  version 12; your copy was derived from 9" from the action log. Genuinely
  useful and the natural pairing, but it is a second question in one command's
  output and nobody has asked. #148's `status` is the likelier home.
- **A rendered visual preview.** #47, closed wontfix; a textual diff is a
  different thing and that reasoning does not apply here.
