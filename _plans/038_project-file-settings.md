# Plan: read settings from `markfluence.yaml`

Give the project file its first keys: a project-wide default `space` and
`page_width`, resolved **flag > frontmatter > project file**, plus the loader
and the malformed-file handling that every later key rests on. Implements #100,
and unblocks #139, which builds `pages:` on top of this loader.

Today `markfluence.yaml` is a bare root marker: `internal/project` stats the
filename and nothing reads its contents (`project.go:12-16`). Use case 5 in
`_plans/025` is a hundred files in one space, each repeating `space: ENG`. That
is the duplication the file exists to absorb.

## Two halves, and the smaller one is the keys

The keys are almost trivial: `space` feeds one call site in `create`,
`page_width` feeds `resolveWidth` in `create` and `update`. Everything
interesting is in the loader.

**The loader decides whether the project's boundary is known.** The root
silently decides every attachment name, bounds every read, and anchors the link
index. So a `markfluence.yaml` that cannot be understood is not a marker for a
root markfluence merely knows less about — it means the boundary is unknown, and
#100 settles that as: abort immediately, naming what is wrong, and do **not**
keep walking upward for a better marker, and do **not** fall back to the
markdown file's own directory.

**Refusing a file from the future is the point, not a cost.** A
`markfluence.yaml` written for a newer markfluence holds keys an older binary
would ignore — and ignoring a project-wide default means publishing with the
wrong space or the wrong width, silently, everywhere at once. So the file
carries no schema version, unknown keys are fatal, and that must not be loosened
later. What should be good is the *message*: an unknown key most likely means
the binary is older than the project, and the error should say so.

## What the code says — read 2026-09-12

Five facts from the existing code that shape the design.

1. **`open()` is the single place a `Root` is built from a marker hit.** Three
   call sites reach it — `Discover` (`project.go:88`), `Cache.walkAndCache`
   (`cache.go:78`) and `FromPath` (`project.go:144`) — so loading inside `open()`
   gives exactly one copy of the rule, and discovery, the cache, and `--root`
   cannot disagree about it.

2. **`internal/project` cannot import `internal/pagewidth`.** `pagewidth`
   imports `client` (`pagewidth.go:26`) and `client.ResolveOptions.Roots` is a
   `*project.Cache` (`internal/client/config.go:44`), so `project → pagewidth →
   client → project` is a cycle. This is what decides where a *vocabulary* check
   can run (D5).

3. **`loadEnvFile` swallows a discovery failure.** `internal/client/config.go:151`
   is `if root, err := project.Discover(cwd); err == nil`, falling back to the
   working directory on any error. Once discovery can fail on a malformed
   project file, that silently resolves `.env` from somewhere else.

4. **`update` resolves width before it resolves the root.** `resolveWidth` is
   `update.go:198`; `roots.Resolve` is `update.go:247`. `create` is already in the
   right order (`root` at `create.go:683`, width at `696`, space at `722`).

5. **`update` does not read `space` from the file at all.** `r.space` comes from
   the live page (`update.go:224`). So a project-wide `space:` affects only
   `create` until #10 makes `update` enforce and move pages.

## Decisions

**D1 — The keys are `space` and `page_width`.** Both already have a frontmatter
field, a resolution path, and (for width) a validator, so neither invents a
concept. Deliberately not shipped:

- **`message`** — `--message` describes the *run*, not the page. #139's rule
  ("flags describe the run; files describe the page") puts it on the flag side.
- **`parent`** — varies per file by definition (#100).
- **`url` / `username` / `cloud_id`** — not because a site URL is secret (it
  is not), but because a committed, walked-up file naming a host decides where
  the token is sent. Settled below, and the reason belongs on #100.

The whitelist stores an expected **kind** per key (scalar / sequence / mapping),
not just a name, so #139 adds `"pages": mapping` rather than reworking the
reader.

**D2 — An unknown top-level key is fatal, and the message names the likely
cause.** One line, because these strings land verbatim in `check --json`:

```
/repo/markfluence.yaml:3: unknown setting "spce" (known: page_width, space) --
an unrecognized setting may mean this project needs a newer markfluence
```

This is the check the whole feature is for: `spce: ENG` silently ignored is
wrong for every file in the project at once.

**D3 — A malformed project file is not a valid marker.** Loading happens in
`open()` (fact 1), so `Discover`, `Cache` and `FromPath` all abort identically.
Discovery does not continue upward and does not fall back. The error is a typed
`*project.ConfigError` so a caller can report it without the misleading
`resolving the documentation root:` prefix that `create.go:685` and
`update.go:249` currently add to everything.

**D4 — An empty or comment-only file stays valid.** That is exactly what ships
today, what the README documents, and what `export` writes
(`cmd/export/projectfile.go:16`). It loads to an empty `Config`.

**D5 — Load validates structure; a value's vocabulary is validated where it is
consumed, plus an offline `check` lint.** Structure means: parseable YAML, a
flat mapping, a known key, the right kind, and a single-line scalar. `space` has
no offline vocabulary to check at all (a space key is opaque until the API sees
it). `page_width` does, but `internal/project` cannot ask (fact 2) — so
`pagewidth.Declared` catches an invalid value where it already runs, with the
error naming `markfluence.yaml` rather than looking like a frontmatter problem,
and **`check` gains a project-file lint** so there is one offline way to find it
without publishing.

The alternative is to break the cycle by making `client.ResolveOptions.Roots` an
interface, which would let the loader validate every value at load. That is a
better end state and a wider change than #100 needs — twelve `client.Resolve`
call sites plus the `roots == nil` fallback. Deferred, noted as a follow-up.

**D6 — The project file is a default, never a conflict.** It is consulted only
when the flag and the frontmatter are both silent. This is `flag > frontmatter >
project file` with the useful property that it needs no new disagreement rules:
`create`'s existing `--space` vs frontmatter conflict error (`create.go:724`)
is untouched, because a project default never participates in a conflict.

**D7 — `page_width` in the project file makes `update` assert the width on a
file that declares none.** `resolveWidth` returns `apply=false` today when
neither flag nor frontmatter declares (`update.go:389`); a project default makes
it `true`. That is a behavior change and it is deliberate — it is what
"declared means asserted" (**L9**) means one level up, and a project that wants
the live width left alone simply omits the key. Flag and frontmatter still win.
`create` already defaults to `max`, so there a project default only changes
*which* default.

**D8 — Settings are per-root, so a multi-root batch gets per-file defaults.**
`Config` hangs off `Root`, which is already resolved per file and cached, so two
files under two different projects get their own defaults with no special case —
the same way `docs/root-model.md`'s "Multi-root batches are allowed" falls out
of per-file discovery.

**D9 — `--root DIR` loads `DIR/markfluence.yaml` when there is one.** `--root`
overrides *discovery*, not the file's contents; `FromPath` already notes the
file (`project.go:137-142`), and it would be strange for the flag that declares
the root to also discard the root's settings. A malformed file there is the same
error as anywhere else.

**D10 — No `--json` or schema change.** These are inputs, not results. The
resolved settings are reported through `ui.Debug`, beside the root. #139 adds
`metadata_source` because it needs to answer "why *that* page"; a default space
and width need no such forensics.

**D11 — The YAML dialect gets one copy, in `internal/frontmatter`** (settled
2026-09-12). The rules
worth not duplicating are already there and were all found by probing: the
scalar node-kind whitelist (`plainScalar`), the single-line rule (`spansLines`),
every null spelling reading as `""`, and the flat-mapping refusal. The project
file is a second *use* of markfluence's YAML dialect, not a second dialect, so
`frontmatter` exposes a fence-free reader and `project` calls it. This is the
"third minimal parser" cost #100 warned about, declined.

The alternative — extract the node primitives into a new `internal/yamlmap` that
both import — is cleaner on the name and worse on timing: #100 needs read-only
flat scalars, so the package would be designed against a guess at what #139's
write side and nested `pages:` need. Extract it when #139 makes those concrete.

## Implementation

### `internal/frontmatter`

Split the fence handling from the dialect so the latter is callable on a whole
document:

- `ReadMapping(text string) (map[string]string, map[string][]string, error)` —
  parse `text` as a single flat YAML mapping and read it through the existing
  whitelist. This is `parseBlock` + `toMaps` with the `---` position shift and
  the `scalarFields` whitelist left out, both being frontmatter's own.
- `parseBlock`'s error formatting splits into the shared one-lining and
  frontmatter's `shiftLeadingPosition`, which exists only because the block text
  excludes the `---` opener. A whole file needs no shift.
- `scalarFields` stays frontmatter's; `project` passes its own key/kind table.

Pure refactor, no behavior change, pinned by the existing 800-line test file.
The package doc comment gains a sentence: `internal/frontmatter` owns
markfluence's YAML *dialect*, and the fenced block is one use of it.

### `internal/project` (`config.go`, new)

- `Config` — `Space string`, `PageWidth string`. Raw strings: the vocabulary is
  validated by the consumer (D5), and a raw field keeps the loader free of
  `pagewidth`.
- `settings` — the key/kind whitelist (D1).
- `loadConfig(path string) (Config, error)` — read the file, `ReadMapping`,
  refuse an unknown key or a wrong kind, return a `*ConfigError` carrying the
  path.
- `ConfigError` — typed, so callers skip the `resolving the documentation root:`
  prefix (D3).
- `Root` gains `Config`. `open()` loads it when `file != ""` (D3), so `Discover`,
  `Cache.walkAndCache` and `FromPath` all get it from one place.
- An unreadable file (permissions) is an error, not an empty config: the walk
  treats an unstattable *ancestor* as "not here" (`probeMarker`), but a file it
  found and cannot read is a boundary it cannot establish.

### `internal/client`

`loadEnvFile` stops swallowing the discovery error (fact 3). Without this, a
malformed project file leaves `.env` silently resolved from the working
directory, and a command with no per-file root (`read`, `search`, `info`) never
aborts at all.

### `cmd/create`

`resolveWidth` and the space block already sit after `roots.Resolve` (fact 4),
so both take `root.Config` as the last fallback. `space` becomes: `--space`, then
frontmatter, then `root.Config.Space`, then the existing "no space given" error
— whose message gains the project file as a third remedy.

### `cmd/update`

Move `resolveWidth` (line 198) below `roots.Resolve` (line 247) so the config is
available, and thread `root.Config.PageWidth` in as the third level. Nothing
between the two depends on the ordering; the label validation that currently
follows width stays before any request, which is the property that matters
(`update.go:202-206`).

### `cmd/check`

A project-file lint, scoped like everything else in `check`: for each root the
run resolves, validate `Config.PageWidth` against `pagewidth.Declared` and
report an invalid value. A malformed file already fails the file through
discovery. This is the offline way to catch what D5 leaves to the consumer.

### `cmd/export`

`projectFileBody`'s comment and `writeProjectFile`'s doc comment both say
nothing in the file is parsed. Update both. The body stays comment-only, and the
comment itself stays accurate: the file the export plants really does carry no
settings. Why it should not carry a `space:` is settled below.

## Tests

- `internal/frontmatter`: `ReadMapping` on a whole document — flat mapping,
  comment-only, empty, a nested value, a sequence, a `|` block, a duplicate key,
  a tab indent, an anchor/alias/tag (the whitelist), a multi-line scalar, a
  `...` second document. These mirror the fenced tests and must agree with them,
  which is the point of sharing the reader.
- `internal/project`: a valid file; comment-only and empty (D4); unknown key,
  with the message asserted (D2); wrong kind per key; a malformed file aborting
  `Discover` **without** continuing to an ancestor that has a good one (D3);
  the same through `Cache` (including that a second `Resolve` under the same
  root does not re-read the file); the same through `FromPath` (D9); an
  unreadable file; `ConfigError` identified by `errors.As`.
- `internal/client`: a malformed project file makes `Resolve` fail rather than
  reading `.env` from the working directory.
- `cmd/create`: project `space` used when flag and frontmatter are silent; flag
  wins; frontmatter wins; the flag-vs-frontmatter conflict error unchanged; an
  invalid project `page_width` names `markfluence.yaml`.
- `cmd/update`: project `page_width` asserted on a file declaring none (D7);
  frontmatter and flag each win; no key means no width request at all.
- Multi-root: one invocation, two projects, two different defaults (D8).
- `cmd/check`: an invalid project `page_width` reported offline; a malformed
  file fails the file.

## Docs

- `docs/root-model.md` — "Its existence is its whole meaning. Nothing in it is
  parsed or read" is now false. Rewrite that section: the settings, the
  precedence chain, why an unknown key is fatal, and that the file is still
  never *executed* (the CVE-2022-24765 framing below it stands, and matters
  more now that the file is read).
- `docs/markdown_file.md` — the precedence chain beside the field table, and
  which fields have a project-wide default.
- `README.md` — the `markfluence.yaml` block (line ~482) gains the keys; one
  line, not a reference.
- `CLAUDE.md` — `internal/project` has **no bullet in the Layout list** today.
  Add one, covering discovery, the `Root`/`Cache` split, and now the loader,
  the abort rule, and why the vocabulary check lives outside the package. This
  is the rule about a change that adds a parser belonging in CLAUDE.md.
- `docs/guarantees.md` — a note under **L2** (`invocation-independent`): a
  declared, on-disk default *strengthens* L2, since a `--space` flag is
  invocation state and a project file is not. No status change.
- `_plans/038` (this file).

## Commits

1. `refactor(frontmatter): expose the YAML dialect reader for whole documents`
2. `feat(project): read markfluence.yaml, refusing a file it cannot understand`
3. `fix(client): stop swallowing a root-discovery failure when locating .env`
4. `feat(create): default space and page_width from markfluence.yaml`
5. `feat(update): default page_width from markfluence.yaml`
6. `feat(check): validate markfluence.yaml offline`
7. `docs(export): the marker file is read now, not merely present`
8. `docs: record the project-file settings and the precedence chain`

## The two questions, answered — 2026-09-12

Both were left open when this plan was written and both are now settled as
**no**, with reasons better than the ones the plan first gave.

### No `url:` (or `username:`)

#100's stated reason is that credentials resolve **flag > env > `.env`** and the
project file answers a different question. That reason is weak on its own terms
and invites relitigating, because a site URL genuinely is not a secret — it is
why `--cloud-id` is allowed to be a flag.

The real reason is sharper: **markfluence sends basic auth on every request to
whatever host the resolved URL names.** A `url:` in a committed, walked-up
project file therefore decides where `CONFLUENCE_TOKEN` is sent, so a pull
request adding one line to `markfluence.yaml` redirects a CI run's token to a
host the author chose. That is credential exfiltration by PR, and it is a
strictly worse version of a hole the project already documents and knowingly
leaves open for `.env` (#136: a writable `.env` "rewriting `CONFLUENCE_URL`
... would redirect the token") — worse because `markfluence.yaml` is committed,
shared, walked up to from a subdirectory, and reviewed as content rather than as
configuration.

The asymmetry against the keys this plan does add is the whole argument. A wrong
`space:` publishes to the wrong place in *your own* instance: visible, and
recoverable with `fix`. A wrong `url:` hands out the token: neither.

`username:` fails a duller test — it is per-person, not per-project, so it has no
business in a committed file even setting the redirect risk aside.

### No `space:` in the tree `export` plants

This one is moot by construction, which the plan did not notice: **every
exported file already carries `space:` in its own frontmatter.**
`pagedoc.Frontmatter` fills it from `client.SpaceKeyFromWebUI(page.Links.WebUI)`
and `RenderFrontmatter` emits it (`internal/pagedoc/pagedoc.go:387-441`), as it
does `page_width`. Frontmatter beats the project file (D6), so a `space:` in the
planted marker would be a setting that every file in the tree overrides.

It would matter only for a file somebody *adds* to an exported tree later — and
even there inconsistently, since `writeProjectFile` never overwrites an existing
marker (S3), so the setting would be present or absent depending on whether one
was already there.

### `create --persist` still writes `space:` into the file — settled 2026-09-12

Found while testing live: `create` persists the space it resolved, so a file
`create` makes carries `space: ENG` even when the project file already said it.
The dedup this issue exists for therefore pays off only for files that are
hand-written and only ever `update`d.

Left alone, deliberately. **The target workflow is CI updating existing pages,
not creating them** — a person creates the page and wires the repo up so a GHA
workflow can `update` it afterwards, which is the same division #139 records as
"CI updates; humans create" (creating in CI would mean a workflow committing a
new `page_id` back to the repo). So `create` is the verb a human runs once, with
their own flags, and the write-back recording what it actually did is a feature
there rather than duplication.

The alternative — not persisting a field whose value came from the project file
— would make a file's frontmatter silently depend on where it sits, and #139
supersedes the problem properly by having `create` write a `pages:` entry
instead.

## Out of scope

- **`pages:`** — #139, blocked on this.
- **`message:`** — D1; a run descriptor, not page metadata.
- **Credentials in the project file** — D1, and settled above: a `url:` would
  decide where the token goes.
- **`markfluence init`** — #5. The file is still created by hand.
- **`export` writing a `space:`** — settled above: redundant by construction,
  since every exported file already carries its own `space:`.
- **Breaking `client → project`** to allow load-time vocabulary validation — D5,
  a follow-up.
- **A `url:` cross-check** — the safe direction of the key settled above: the
  project file *declares* the site and markfluence refuses to publish when the
  resolved URL disagrees, fencing the token instead of directing it. It would
  catch "published our docs to the wrong Confluence." Noted rather than filed;
  it waits for someone to want it.

## Follow-ups

- Make `client.ResolveOptions.Roots` an interface, then move `page_width`'s
  vocabulary check into the loader and drop `check`'s lint (D5).
- A test that every key in `settings` appears in `docs/root-model.md`, the way
  `TestSubcommandsDocumentThemselves` pins `--help`. A silently undocumented
  project-wide setting is the same class of problem as a silently ignored one.
