# Spec: file organization and how it round-trips

How markdown on disk maps to pages and attachments in Confluence, and back.

A spec rather than an implementation plan. It states the problem in terms of the
guarantees in [docs/guarantees.md](../docs/guarantees.md), lists the layouts
markfluence should support, then works through the model and what it costs, and
ends with the work it implies.

Everything it raises is now decided. One thing is deliberately *not* fixed here
and says so where it comes up: **R1** (report-unresolved-references), which needs
a converter diagnostic rather than a path decision.

## The problem

**Eight guarantees are not true today.** Each is broken by a scenario a user
reaches without doing anything unusual.

| | label | guarantee | status | scenario |
|---|---|---|---|---|
| **L1** | `resolve-what-was-named` | a reference resolves to the file it names, or nothing | Aspirational | A |
| **C1** | `preview-compatible-resolution` | references resolve the way a Markdown preview does | Partial | A |
| **L2** | `invocation-independent` | resolution and naming depend only on the files on disk | Aspirational | B |
| **L3** | `identity-from-asset-location` | an attachment's identity depends only on the asset's location | Aspirational | C |
| **L5** | `roundtrip-from-confluence` | export, then publish back unedited, changes nothing | Partial | D |
| **L6** | `roundtrip-from-disk` | publish, then export, republishes the same page | Partial | D |
| **R1** | `report-unresolved-references` | every unresolved reference is reported | Aspirational | E |
| **S2** | `no-read-outside-root` | no file is read outside the root | Aspirational | F |

### Scenario A. A link to a file in a subdirectory

Breaks **L1** and **C1**. Given this tree:

```
docs/
  index.md          See the [setup overview](setup/overview.md) to get started.
  guide.md          Follow the [install steps](setup/install.md).
  overview.md       title: Product Overview      page_id: 999
  setup/
    overview.md     title: Setup Overview        page_id: 777
    install.md      title: Install Steps         page_id: 888
```

`index.md` links to `setup/overview.md`, which is page id **777**. Publishing it
produces a link to page id **999** instead:

```
index.md   <a href="https://wiki.example.net/wiki/spaces/ENG/pages/999/Product+Overview"
guide.md   <a href="setup/install.md"
```

No warning, no error. The reader clicks "setup overview" and lands on the
product overview.

The cause is the lookup key. markfluence takes the **last path segment of the
link destination** — `filepath.Base("setup/overview.md")` is `overview.md` — and
looks it up in a table of the `.md` files in the **linking file's own directory**,
which `buildPageMap` reads without descending. `docs/overview.md` is in that
table, so `overview.md` matches it. `docs/setup/` is never read, so page 777 is
never a candidate.

`guide.md` shows the same lookup failing to match anything: there is no
`install.md` beside it, so the destination is published unchanged as
`setup/install.md`, a relative href that is dead on Confluence.

Of the two, `index.md` is the serious one. The README promises an unresolvable
link is "left exactly as written" — which is what `guide.md` got — but
`index.md` was rewritten to point somewhere the author never named. A duplicate
basename across directories is all it takes, and `index.md`, `README.md` and
`overview.md` repeat across directories routinely.

Images do not have this problem: they resolve against the linking file's
directory as a *path*, the way a Markdown preview does, so
`![](setup/diagram.png)` finds `docs/setup/diagram.png`. That split between
images and links is why C1 is Partial rather than false.

### Scenario B. Running from a different directory

Breaks **L2**. Given this tree:

```
docs/
  assets/logo.png
  team/
    onboarding.md     ![company logo](../assets/logo.png)
```

The image sits above the page that references it, which is the layout the README
endorses for shared assets. Publishing the same file from two directories
produces two different pages:

```
$ cd docs && markfluence update team/onboarding.md
  → 1 attachment, named ..%2Fassets%2Flogo.png
  → <ac:image ac:alt="company logo"><ri:attachment ri:filename="..%2Fassets%2Flogo.png" /></ac:image>

$ cd docs/team && markfluence update onboarding.md
  → 0 attachments
  → <p>IMAGE BROKEN: ../assets/logo.png (outside the documentation root)</p>
```

The second run publishes the sentence `IMAGE BROKEN: ../assets/logo.png (outside
the documentation root)` into the page body, where readers see it. `update`
prints it as a warning and publishes anyway.

The cause is what bounds the publishable area. `MdToConfluence` calls
`os.Getwd()` and treats the working directory as the documentation root, then
refuses any image resolving above it. From `docs/`, the image is inside. From
`docs/team/`, the identical reference is outside.

Two consequences follow from the root being the working directory rather than a
property of the tree. Attachment names shift with it — the first run's
`..%2Fassets%2Flogo.png` would have been `assets%2Flogo.png` had the root been
declared at `docs/` — which is Scenario C from a different angle. And the same
tree reached through a symlink is rejected outright, because `withinRoot`
compares lexically after `filepath.Abs` without resolving either side, so
`/docs` and `/private/docs` do not match.

The rule is documented: the README says to run markfluence from the root of your
documentation tree. Nothing detects a violation, and the consequence is a
corrupted page rather than a refusal.

### Scenario C. One shared image, referenced from two depths

Breaks **L3**. This one happens at **publish** time, and nothing visibly breaks
when it does — it is the cause of Scenario D and of the churn in Use case 9,
which is why it is worth naming on its own.

Given this tree, published from `docs/`:

```
docs/
  assets/brand.png
  index.md            ![brand](assets/brand.png)       page_id: 100
  team/
    onboarding.md     ![brand](../assets/brand.png)    page_id: 200
```

Both pages reference **the same file on disk**. Publishing them:

```
$ cd docs && markfluence update index.md team/onboarding.md
```

markfluence uploads the image once per page. A Confluence attachment belongs to
a page rather than to a space, so each page gets its own copy even though the two
share one file on disk. That duplication is inherent, and it is not the problem.

The problem is what markfluence records on each copy. Two fields carry the path:

- the **attachment name**, which is Confluence's `title` for the attachment. It
  is what the page's attachment list shows, and what `ri:filename` in the page
  body points at.
- the **recorded source**, which markfluence writes into the attachment's
  *comment* field as `path=…` beside a checksum
  (`attachmentComment` builds `markfluence: sha256=<hex> path=<source>`). It is
  the original markdown destination, and it is what `read` and `export` use to
  put the image back where it came from.

What the two pages end up with:

```
page 100  (from index.md)
  attachment name     assets%2Fbrand.png
  attachment comment  markfluence: sha256=<hex> path=assets/brand.png
  body references     <ri:attachment ri:filename="assets%2Fbrand.png" />

page 200  (from team/onboarding.md)
  attachment name     ..%2Fassets%2Fbrand.png
  attachment comment  markfluence: sha256=<hex> path=../assets/brand.png
  body references     <ri:attachment ri:filename="..%2Fassets%2Fbrand.png" />
```

Both are the reference **as the author wrote it** — percent-encoded for the name,
verbatim for the comment. The two authors wrote different relative paths to reach
the same file, so the file has two identities in Confluence. Both pages render
correctly, which is why this stays invisible until something depends on the
name.

Two things depend on it.

**Export.** `export --dest DIR` writes the page's markdown to `DIR/<slug>.md`
and each attachment to `DIR` joined with its **recorded source**. `--dest`
defaults to `.`, so `--dest out` run from `~/work` means the destination
directory is `~/work/out`.

Joining the two recorded sources gives very different results:

```
page 100   path=assets/brand.png      → out/assets/brand.png     written
page 200   path=../assets/brand.png   → assets/brand.png         REFUSED
```

The second one is not inside `out` at all. `filepath.Join` cleans as it joins, so
`out` + `../assets/brand.png` collapses to `assets/brand.png` — a sibling of
`out`, in `~/work` itself. Writing there would drop a file outside the directory
the user named, so `attachfile.Resolve` refuses it:

```
attachment "..%2Fassets%2Fbrand.png" resolves to "assets/brand.png",
outside the destination directory
```

So the same image on disk is exportable from page 100 and not from page 200,
decided entirely by how the author of each page happened to spell the path. That
is Scenario D and we'll discuss it there.

**Moving a file.** Move `index.md` from `docs/` into `docs/guides/` and its
reference has to become `../assets/brand.png` to still resolve. Republish it —
same page, `page_id` 100 is still in the frontmatter:

```
$ cd docs && markfluence update guides/index.md
  → attachment name     ..%2Fassets%2Fbrand.png     (was assets%2Fbrand.png)
  → attachment comment  path=../assets/brand.png    (was path=assets/brand.png)
```

That uploads a *second* attachment under the new name and leaves the original
behind unreferenced, because markfluence never deletes. Nothing about the page or
the image changed; only the markdown file's position did.

The model fixes the *rename*: with the source recorded relative to the root,
moving a page does not change what its attachments are called, so there is
nothing to strand. It does not fix stranding in general, because an attachment's
identity does follow the **asset's** location by design (**L3**) — so moving or
renaming an asset still renames its attachment, as does removing an image from a
page. Cleaning those up is #99 (`attachment-prune`), which is a tool for the
residual rather than part of this model.

Note that `team/onboarding.md` and `guides/index.md` agree, because they sit at
the same depth. Two pages only disagree when they are at different depths, which
is why this is easy to miss in a shallow tree and unavoidable in a deep one.

`attachment-upload` diverges a third way: it records `filepath.Base(f)`, so
uploading `sub/img.png` by hand records `img.png`, while publishing a page that
references the same file records `sub/img.png`.

### Scenario D. Exporting a page whose assets sit above it

Breaks **L5** and **L6**. Publishing works; only the return trip fails.

Start from the layout the README endorses for shared assets, and the one
`regression/images-shared-parent` pins with a golden — a page in a subdirectory
using an asset directory above it:

```
docs/
  assets/logo.png
  team/
    onboarding.md     ![company logo](../assets/logo.png)
```

Publish it, from the root of the tree so the asset is inside the documentation
root:

```
$ cd docs && markfluence create team/onboarding.md --space ENG
```

That succeeds. The page now carries one attachment, named
`..%2Fassets%2Flogo.png`, whose comment records `path=../assets/logo.png` — the
reference as written, per Scenario C.

Now get it back. This is the direction where the original tree may not exist at
all: a colleague exporting a page they did not publish has only what Confluence
stores.

```
$ markfluence export https://.../pages/12345/Onboarding --dest out
```

`export` writes the markdown to `out/onboarding.md` and each attachment to `out`
joined with its recorded source. For this attachment that join is:

```
out + ../assets/logo.png  →  assets/logo.png
```

which is not inside `out` — `filepath.Join` cleans as it joins, so the result is
a *sibling* of `out`. `attachfile.Resolve` refuses to write outside the directory
the user named, so the export reports:

```
  ✓ written    out/onboarding.md
  ✗ failed     ..%2Fassets%2Flogo.png: attachment "..%2Fassets%2Flogo.png" resolves to
               "assets/logo.png", outside the destination directory
```

and exits **1**, because `report` counts failed attachments and any failure is a
non-zero exit. The markdown is written; the image it references is not. The
exported page renders with a broken image, and there is no flag that changes
this — `--flat` on `attachment-download` opts out of recorded paths, but `export`
has no equivalent.

So a layout markfluence documents, tests, and publishes correctly cannot be
round-tripped. That is why **L5** and **L6** are Partial rather than false: they
hold for every layout whose assets sit at or below the page, and fail entirely
for the one above it.

The refusal itself is correct — it is **S1** (no-write-outside-root) doing its
job. The defect is
upstream: Scenario C recorded a path that cannot be honoured under `--dest`.
Fixing this belongs in naming, not in export.

> **Verified in pieces, not end to end.** The converter recording
> `path=../assets/logo.png`, and `attachfile.Resolve` refusing exactly that
> string with exactly that message, are both measured. The export output above is
> assembled from `report` in `cmd/export/export.go` rather than observed on a
> live page, because the credential to hand lacks the scope to publish one.

### Scenario E. Any unresolved reference

Breaks **R1**. Images report what they could not resolve; links say nothing at
all. Same class of problem, two behaviours.

```
docs/
  guide.md      (below)
  draft.md      title: Draft, and no page_id -- not published yet
  notes.txt     a text file, not an image
```

`guide.md` contains four references, none of which can resolve:

```markdown
A [link to a missing file](nope.md).

A [link to the draft](draft.md), which exists but has no page_id.

An image that is missing: ![missing image](nope.png)

An image of the wrong type: ![bad type](notes.txt)
```

Publishing it with `markfluence update guide.md` produces this body:

```
<p>A <a href="nope.md">link to a missing file</a>.</p>
<p>A <a href="draft.md">link to the draft</a>, which exists but has no page_id.</p>
<p>An image that is missing: IMAGE BROKEN: nope.png (not found)</p>
<p>An image of the wrong type: IMAGE BROKEN: notes.txt (unsupported type)</p>
```

and these diagnostics:

```
broken   (2): IMAGE BROKEN: nope.png (not found)
              IMAGE BROKEN: notes.txt (unsupported type)
warnings (0):
```

**The images behave well.** Both failures are named in `broken`, so `update`
prints them, and both are visible in the page as `IMAGE BROKEN: …` text, so a
reader can see something is wrong. Loud in both directions.

**The links behave badly.** Both publish as `<a href="nope.md">` and
`<a href="draft.md">` — relative hrefs that are dead on Confluence, since nothing
resolves `nope.md` there. Neither appears in `broken` or in `warnings`. `update`
reports a successful publish, the page looks fine in a diff, and the links fail
only when somebody clicks one.

The two link cases fail for different reasons, and both are silent. `nope.md`
does not exist. `draft.md` does exist, but has no `page_id`, so it is not in the
page index — which is the ordinary state of every page in a tree that has not
been published yet, and is why Scenario E and Use case 7 are related.

The cause is one-sided reporting. `internal/convert/images.go` appends to
`r.broken` in three places and `r.warnings` in two. `internal/convert/links.go`
touches neither, ever.

This is documented behaviour rather than an oversight. The README:

> A link it cannot resolve — a target with no `page_id`, or a file that isn't
> there — is left exactly as written and published as-is, which on Confluence is
> a dead relative link. **There is no warning for this**, so check the targets
> when a link matters.

R1 is the decision to stop saying that. Note what it does *not* require: leaving
the link as written is a reasonable thing to publish, and R1 does not ask for it
to be an error. It asks for it to be **said out loud** — which is why R1 is a
reporting requirement rather than a law.

### Scenario F. Reading outside the root

Breaks **S2**, and it is the one scenario where this spec's model makes things
worse before better.

```
/work/
  outside/
    secret.md      title: Secret Outside The Root, and no page_id
    linked.md      title: Linked Outside, page_id: 555
  docs/                        ← the documentation root
    page.md        parent: ../outside/secret.md
    link.md        A [link outside the root](../outside/linked.md).
```

`withinRoot` is applied to images and to nothing else, so the two references
above are treated very differently — and neither is treated the way S2 asks.

#### A `parent:` path is read with no clamp

`page.md` names a parent above the root. Publishing it:

```
$ cd docs && markfluence create page.md --space ENG
```

`resolveParent` joins the path onto the file's directory, stats it, and parses
its frontmatter, with no bound of any kind. The read happens before a client is
ever touched, so it is provable without a network call at all — calling
`resolveParent` with a `nil` client returns:

```
parent not yet published (no page_id): ../outside/secret.md
```

That error is only reachable by opening `/work/outside/secret.md` and finding no
`page_id` in it. The file outside the root was read.

The exposure is genuinely small: only `page_id` and `title` are taken, and
nothing from that file is published. But S2 says no file is read outside the
root, and this reads one.

#### A link cannot traverse, purely by accident

`link.md` names a target above the root too, and nothing is read:

```
href: href="../outside/linked.md"
broken=[] warnings=[]
```

The reason is the lookup key, not a bound. `docKey("../outside/linked.md")` is
`"linked.md"`, which is looked up in an index of `docs/` alone. There is no
`linked.md` in `docs/`, so the link resolves to nothing and is published as
written. `page_id: 555` is never seen, because `/work/outside/` is never opened.

Flattening every destination to its basename makes traversal impossible. That is
the same flattening that produces Scenario A's wrong-page links — the property
protecting S2 here is a side effect of the bug there.

#### Which is why the model has to add a clamp

Making resolution path-aware, so `../team/onboarding.md` finds the page it names,
necessarily also makes `../../../../etc/anything.md` a real path lookup. The
accident goes away with the bug.

So S2 becomes work this spec creates rather than work it completes. What the model
does about it is in
[How S2 is enforced](#how-s2-no-read-outside-root-is-enforced): links need no
clamp at all once resolution is a lookup in an index built downward from the
root, and the two remaining reads — an image leaf and a `parent:` path — are
handled differently from each other.

### What is actually going wrong

Not one cause. Three kinds, and they are worth keeping apart, because choosing a
single convention fixes most of them and provably does not fix two.

Letters throughout this section refer to the scenarios above.

**Conventions that are wrong.** These work exactly as designed and still violate
a guarantee:

| scenario | convention | violates |
|---|---|---|
| A | a link destination is keyed by its **basename**, looked up in one directory | L1, C1 |
| B | the root is the **working directory** | L2 |
| C | an attachment's identity is the reference **as written** | L3 |

**Inconsistencies.** The same question answered differently in different places,
which is what happens when no single convention was chosen:

| scenario | inconsistency |
|---|---|
| A | images resolve a destination as a *path*; links flatten it to a basename |
| C | `attachment-upload` records a basename; `update` records the path |
| D | publishing accepts an asset above the page; exporting refuses it |
| F | `withinRoot` guards images, and not links or `parent:` paths |

**Bugs.** Behaviour contradicting its own stated contract:

| scenario | bug |
|---|---|
| A | a non-sibling link is **rewritten to the wrong page**, where the README promises it is "left exactly as written" |
| B | `withinRoot` rejects a tree reached through a **symlink**, because it compares lexically — `filepath.Abs` then `filepath.Rel`, with no `EvalSymlinks` anywhere in the tree |

The three groups are not independent. The inconsistencies exist because there was
no single answer to defer to, and two of the bugs are downstream of a convention:
basename keying is what produces the wrong page, and reference-as-written is what
produces a path export cannot honour.

**Two things are not downstream of any convention**, and one root will not fix
them:

- **The symlink comparison** (Scenario B). Choosing where the root comes from does not
  change that the comparison is lexical. The fix is to stop following symlinks at
  all — the index walk traverses none, a leaf read refuses one, and an `os.Root`
  catches an escape through an intermediate directory that a leaf check cannot
  see. See [docs/guarantees.md](../docs/guarantees.md#symlinks).
- **The reporting omission** (Scenario E). Links never say what they could not resolve.
  That gap exists under any convention, and closing it is a diagnostic rather
  than a path decision.

## The use cases

What markfluence should support. Mechanism comes later; this is the list to
measure a solution against.

### Use case 1. One markdown file, no images

```
notes.md
```

Publish it. Nothing to resolve, no configuration.

### Use case 2. Export a page, edit it, publish it back

Take a page that already exists in Confluence, get it onto disk with its images,
edit it, publish it back. Publishing back an unedited export changes nothing.

### Use case 3. A new page with its images in a subdirectory

```
project/
  page.md
  images/diagram.png
```

The subdirectory is for tidiness and should need no configuration.

### Use case 4. A directory of pages sharing one images subdirectory

```
project/
  a.md
  b.md
  c.md
  images/x.png
```

The pages share a Confluence parent, so they sit in one directory. They may link
to each other.

### Use case 5. A whole space: hierarchy, shared and page-specific images

```
docs/
  assets/brand.png       ← shared by many pages
  index.md
  team/
    onboarding.md
    images/flow.png      ← specific to one page
  ops/
    runbook.md
    images/graph.png
```

Around 100 files. Pages link to each other across directories. Brand images are
shared; graphs are page-specific. The Confluence hierarchy comes from
frontmatter, not from the directory layout.

### Use case 6. Links across the tree

```
docs/
  index.md           [team onboarding](team/onboarding.md)
                     [the runbook](ops/runbook.md)
  team/
    onboarding.md    [escalation](../ops/runbook.md#escalation)
                     [back to the index](../index.md)
  ops/
    runbook.md       ## Escalation
                     [onboarding](../team/onboarding.md)
```

Four directions, all of which should resolve to the right Confluence page:
**down** into a subdirectory, **across** between two of them, **up** to the root,
and **into a heading** in a page in another directory.

Implied by Use case 5, and stated separately because it is the requirement most
likely to be dropped. Note that every link here is spelled the way a Markdown
preview resolves it, so the tree renders correctly on GitHub before it is
published at all — which is the standard the links should be held to.

### Use case 7. Publishing a whole tree for the first time

Nothing has a `page_id` yet, and links point at pages that do not exist until
the run creates them — including pairs that link to each other.

The requirement is that one command publishes the tree with every link resolved,
rather than leaving the reader to know that a second pass is needed.

### Use case 8. Exporting a subtree or a whole space

The inverse of Use case 5. Does not exist today: `export` is single-page.

Three provenance variations have to work, because a real space contains all
three: pages markfluence published (their attachments carry recorded paths),
pages that originated in Confluence (their attachments carry none), and a subtree
mixing the two.

### Use case 9. Moving or renaming a markdown file

Reorganise the repository without churning Confluence. The page keeps its
identity, and its attachments keep theirs.

Two variants, because a page with images can be moved two ways:

```
(a) the markdown and its images move together
      docs/index.md        + docs/images/x.png        ![x](images/x.png)
    → docs/guides/index.md + docs/guides/images/x.png ![x](images/x.png)

(b) the markdown moves, the images stay, the links are updated
      docs/index.md        + docs/images/x.png        ![x](images/x.png)
    → docs/guides/index.md + docs/images/x.png        ![x](../images/x.png)
```

Variant (a) is the tidy per-page layout of Use cases 3 and 4 moved as a unit.
Variant (b) is what happens when the images are shared and stay put.

The requirement is that neither churns Confluence: no re-uploaded attachment
under a new name, no stranded original. **Only one of the two can be met** — see
below.

### Use case 10. Cloning the repository, or publishing from CI

Someone else checks out the repo, or CI does, and publishing produces the same
result — same attachment names, same links.

### Use case 11. Smaller cases, so they are deliberate

- **Non-image attachments** (PDF, CSV) via `attachment-upload`.
- **Attachments added in the Confluence UI** by someone else, which markfluence
  must leave alone.
- **A page whose title changes**, since an exported filename is slug-derived.

## The solution model

One root. Every recorded path is relative to it.

- **The root** is the directory holding `markfluence.yaml`, found by walking up
  from the working directory. With no config file, the root is the markdown
  file's own directory. `--root` overrides discovery.
- **The root is always reported**, in human output and in `--json`. It silently
  determines every attachment name and bounds what may be published, so leaving
  it invisible would make **L2** true but unauditable — and a stray config file in
  an ancestor directory undetectable.
- **Assets must live at or below the root.** Above it is `IMAGE BROKEN`, as
  today.
- **Markdown still writes page-relative paths**, so a Markdown preview renders
  them. `![](images/flow.png)` from `team/onboarding.md` is unchanged.
- **The recorded attachment source is root-relative.** That same reference
  records `team/images/flow.png`, named `team%2Fimages%2Fflow.png`.
- **Links resolve root-relative**, against an index of the tree below the root
  rather than by basename against one directory.
- **Symlinks are not followed** ([non-goal](../docs/guarantees.md#symlinks)). The
  index walk does not traverse them, a leaf read refuses one, and reads go through
  an `os.Root` on the root as the backstop — which also makes *how the root was
  reached* irrelevant.

There is no flat mode and no nested mode. For a project whose files all sit in
one directory, the root *is* that directory, and root-relative and page-relative
produce byte-identical results. "Flat" is not a mode; it is what this model does
when the tree is one level deep.

### The project file

#### The no-config default is stricter than today

Without `markfluence.yaml`, the root is the markdown file's own directory — so an
asset *above* the page is outside the root and becomes `IMAGE BROKEN`. That is
narrower than today, where the root is the working directory and running from the
tree root lets a page reach an asset above itself.

Scenario B's own tree is the example. `docs/team/onboarding.md` referencing
`../assets/logo.png` publishes today when run from `docs/`; with no project file
its root is `docs/team/` and the asset is out of bounds.

This is deliberate rather than an oversight — **assets above a page are exactly
the case that needs a declared root**, and Use case 5 is where they appear. But it
means the shared-parent layout the README endorses is repaired by this model
*only when a project file exists*, and it is why item 12 has to make the root
explicit in the regression suite: `regression/images-shared-parent/test.input` has
no root today, and its golden pins `source: "../assets/logo.png"`.



`markfluence.yaml`, in the root directory. **Its existence is its whole meaning:**
it marks the root, and nothing in it is read yet.

Visible rather than hidden, and named for the tool the way `go.mod`, `Cargo.toml`
and `pyproject.toml` are. The root silently decides every attachment name and
bounds what may be published, so being able to see where it is matters more here
than tidiness in a directory listing does.

It carries a comment rather than being literally empty, so a reader who finds it
learns what it does:

```yaml
# Marks the root of a markfluence project. Image and link paths are recorded
# relative to this directory. https://github.com/mozilla/markfluence
```

**YAML by name, with no parser yet.** Nothing in the file is read, so nothing
parses it and no dependency is needed today. The `.yaml` extension fixes the
intended format so adding keys later is not a migration.

When keys do arrive the format costs something: **there is no YAML library in this
module.** `go.yaml.in/yaml/v3` appears in `go.sum` only as a `/go.mod` hash — a
module-graph entry with no zip hash — so `go mod why` reports "main module does
not need package" and importing it fails on a missing `go.sum` entry. Adding it is
a new direct dependency, not a promotion. The alternative is a third minimal
parser, after `internal/frontmatter` and the `.env` reader; `frontmatter` itself
cannot be reused, since it requires `---` fences and returns an empty map without
them.

**Two precedence chains, and they must not be conflated.** Credentials resolve
**flag > environment > `.env`** and are about *who you are*. Anything the project
file grows later is about *what the content is*, and would resolve **flag >
frontmatter > project file** — a different chain, because a per-file answer
should beat a per-project one. A default `space` is the obvious first candidate,
since Use case 5 repeats `space: ENG` across a hundred files.

**`.env` is read from the root.** One walk, one anchor: it is read from wherever
`markfluence.yaml` was found, so running from a subdirectory works for
credentials as well as for assets. With no project file, the working directory, as
today. `--env-file` still overrides absolutely.

The two files stay separate — `markfluence.yaml` is committed and shared, `.env`
is gitignored and personal — and one footgun comes with the change: a stray `.env`
in an ancestor can hand a project credentials that are not its own. Reporting the
root is what makes that visible.

Unresolved and deferred to when keys exist: what markfluence does with a key it
does not recognise, and whether a malformed file is an error or still a valid
marker.

#### Hooks, and why discovery must not authorise them

markfluence today executes nothing, which is why the discovery risk in *What
changes* is framed as narrow. That may not hold: a git-style hook system — run a
named program at a point in the pipeline — is a plausible direction, because it
lets users add variable expansion, mermaid rendering, a "maintained at"
banner, and other things without markfluence carrying the maintenance for each
of them. The hook system does not need designing now and is out of scope for
this spec.

One constraint does need honouring now, because it is free today and a retrofit
later: **discovering a file by walking up must not, by itself, authorise anything
in it to run.** Git's hooks live inside the directory its discovery walk finds,
which is precisely what made CVE-2022-24765 reachable, and the answer was
`safe.directory` bolted on afterwards. Keeping the root marker separable from any
future execution declaration — distinct files, or an explicit consent step in the
shape of `direnv allow` — costs nothing while neither exists.

A milder form of the same applies to `.env`. Reading it from a discovered root
means a stray one in an ancestor can hand a project credentials that are not its
own, which is a footgun in your own tree and worse in a shared one.

### How S2 (no-read-outside-root) is enforced

Mostly it is not enforced, because it stops being enforceable-by-check and
becomes true by construction.

**Links and anchors need no clamp.** The index is built by walking *down* from the
root, so resolution is a map lookup rather than a file read. Nothing outside the
root can be in the index, and `filepath.WalkDir` does not descend a symlink to put
it there (verified — see the non-goal). So `[x](../../../../etc/passwd.md)` is not
refused; it is simply not found, falls through to "left exactly as written", and
is reported under **R1** (report-unresolved-references) like any other
unresolved link. It deserves its own
message — "target is outside the project root" beats "not found" — but the
behaviour is the same.

**`parent:` is a real read**, and the only one left besides an image leaf. It goes
through the root handle, and an escaping path is a **hard error** rather than an
unresolved-and-reported: a parent is load-bearing, and publishing under the wrong
parent is worse than not publishing. `create` already errors on a bad parent, so
this joins an existing path. It does mean `cmd/create` needs the root handle,
which it has no reason to hold today.

### The link index is built once

Built once per run and shared, rather than rebuilt per conversion. Measured, and
doing so is faster than what happens today.

| files | per-directory, per conversion (today) | tree-wide, per conversion | tree-wide, built once |
|---|---|---|---|
| 100 | 30ms | 162ms | 1.6ms |
| 200 | 115ms | 583ms | 3ms |
| 400 | 445ms | 2.24s | 5.5ms |

Doubling the file count roughly quadruples the first two columns and doubles the
third, so **today's per-directory index is already O(n²)**: each conversion
re-reads its whole directory, and 40 files in a directory means 40 reads for each
of 400 conversions. Tree-wide indexing does not introduce a new complexity class,
it multiplies the existing one by about five.

Built once, the index is O(n) and about **80× faster than today** at 400 files.
Extrapolated to 1000 files: today ≈ 2.8s, tree-wide per conversion ≈ 14s, shared
≈ 14ms. So the shared index is not a performance concession the model needs — it
repairs a cost that predates it.

### Publishing a tree in three phases

`create` reserves ids before converting anything.

1. **Preflight.** What phase 1 already does — validate `page_id`, space, parent
   and title for every file, and abort the whole run if any of them fails.
2. **Reserve.** Create a stub for every page: title, parent, no content. Capture
   each `page_id` and persist it unless `--no-persist`. No conversion happens
   here, and no attachments are uploaded.
3. **Publish.** Convert and update every page. Every `page_id` in the set now
   exists on disk, so every link resolves.

The reason this beats a second pass bolted onto the current design is
**determinism**. Today conversion happens per file inside the create loop, and
`buildPageMap` reads ids from disk, so a file converted later sees the ids of
files created earlier — links pointing "backwards" in parent-topological order
resolve and the rest do not, with link direction unrelated to parent order. A
cycle can never fully resolve. Reserving first removes the ordering question from
links entirely.

Three costs, accepted:

- **Every page's v1 is a stub, permanently.** Confluence assigns ids on creation,
  so there is no way to learn an id without making a page, and the empty first
  version stays in the history.
- **An interrupted run leaves stubs**, where today it leaves pages missing. More
  recoverable — every id exists and is persisted, so `markfluence update *.md`
  finishes the job — but uglier while it is broken.
- **Writes double**, even for a page nothing links to.

Topological ordering stays necessary in phase 2, because creating a page needs
its parent's id at creation time. Only *link* resolution stops depending on
order.

Under `--no-persist` phase 3 still works, since the ids are in memory for the
duration of the run; what is lost is the ability to re-run `update` afterwards,
which is already what that flag means.

### What it costs

**Longer attachment names.** A page-specific image becomes
`team%2Fsub%2Fimages%2Fgraph.png` rather than `images%2Fgraph.png`. The majority
case pays so the minority case — a shared image — works. Accepted: names are
identifiers, a reader sees the rendered image, and the name surfaces only in a
page's attachment list.

**A path longer than 165 characters cannot be recorded.** Both the attachment
name and its comment cap at 255 characters
([attachments.md](../docs/confluence/attachments.md#how-long-a-name-and-a-comment-may-be),
verified 2026-08-28), and the comment binds first because it carries 90
characters of fixed overhead — `markfluence: ` + `sha256=` + 64 hex + ` path=`.
Root-relative paths are longer than the page-relative ones recorded today, so
this ceiling gets closer rather than staying put; a 130-character path already
lands at 220.

Over the limit is an **HTTP 400**, not a truncation, which is the outcome worth having:
a truncated comment would disagree with the local source on every publish and
re-upload the attachment forever trying to correct itself. Shortening the comment
format buys headroom — see *What changes*.

**Which kind of move is free gets inverted.** Identity relative to the markdown
file makes Use case 9's variant (a) free and (b) churn; identity relative to the
root does the reverse. Measured, today:

```
(a) md + images/ moved together   source=images/x.png     unchanged
(b) md moved, asset left behind   source=../images/x.png  changed
```

Both cannot be free. The two variants differ in precisely which path moved, so a
path-based identity has to pick one, and only content-addressed names would make
both free — at the cost of readable names and of reconstructing a tree on export.

Root-relative is still the right side of that trade, for two reasons. Variant (a)
churning produces an **orphan**, which is cleanable (#99) and leaves the page
correct meanwhile; the shared-asset case it buys is **broken** today, with export
refusing outright and no workaround. And L2 is unreachable from a page-relative
identity at all.

### Multi-page export layout

The Confluence hierarchy is mirrored into directories. A page becomes
`<slug>.md`, and gains a `<slug>/` beside it if it has children or attachments of
its own. A Confluence folder becomes a directory with no markdown in it.

**Two placement rules for attachments, one per provenance.** This is the part the
markdown layout turns out to depend on:

| the attachment | goes to | because |
|---|---|---|
| has a recorded `path=` (markfluence published it) | `dest/<recorded path>` | the recorded path is authoritative, and reconstructing it is what makes **L5**/**L6** hold |
| has none (it originated in Confluence) | `dest/<page dir>/<name>` | attachment names are unique per *page*, so page-scoping cannot collide |

Mixed provenance needs no special handling: each attachment follows its own
rule, and the two land in different parts of the tree.

```
dest/
  home.md
  home/
    onboarding.md            ← markfluence-published
    onboarding/
      diagram.png            ← native attachment for onboarding.md, page-scoped
      escalation.md          ← child page
  assets/brand.png           ← recorded path=assets/brand.png, shared
  team/images/flow.png       ← recorded path=team/images/flow.png
```

The export is self-describing as a result: an asset under a page's directory came
from Confluence, and an asset in the shared tree came from markfluence — until the
first republish, after which an adopted asset has a recorded path and moves into
the shared tree.

**Recorded paths collide across pages, and benignly.** The model's success case is
one shared asset referenced from many pages, each carrying its own attachment
recording `path=assets/brand.png`. On a multi-page export all of them resolve to
`dest/assets/brand.png`. The bytes are identical, so the first write lands and the
rest skip under **S3** (no-overwrite-without-force) — the right outcome, reached
by accident. Worth stating rather than discovering: a *differing* checksum under
one recorded path means two pages disagree about what that path holds, and that
should be reported rather than skipped.

**Why page-scoping rather than flat.** A Confluence attachment name is unique
within its page, not within the space, so fifty native pages can each carry a
`diagram.png`. Today `attachfile.Resolve` falls back to the attachment name when
there is no recorded path, so all fifty resolve to `dest/diagram.png` — which
under **S3** (no-overwrite-without-force) is a refusal, or forty-nine skips.
Page-scoping removes that class of collision by construction rather than by
detecting it.

Two consequences to carry into implementation:

- **Placement and the emitted markdown have to move together.** `sourceFor`
  derives an unsourced attachment's markdown destination from its name, so a
  page-scoped directory needs `StorageToMarkdown` to carry a per-page prefix.
- **Single-page export adopts the same rule.** Otherwise the same native page
  exports as different markdown depending on how many pages were asked for.
  Nothing depends on today's flat-in-the-root behaviour.

  > **Correction (`_plans/029`, #59).** The last sentence is wrong. The
  > attachment *name* depended on it. A name was the percent-encoded
  > root-relative path, and the name is the attachment's identity, so
  > page-scoping an attachment moved its markdown path, moved its name, and made
  > republishing create a second attachment while orphaning the first. The rule
  > stands and the reasoning for it stands; what changed to make it payable is
  > that an attachment is now named by its base name, so a moved path keeps its
  > name. See `_plans/029` §"The thing 025 got wrong".

**Slug collisions are refused, naming both pages.** *(Superseded by
`_plans/029`, which disambiguates with a `-<id>` suffix instead. Refusing makes
a space unexportable over a punctuation variant, and `--space` exists for spaces
the caller cannot retitle; the L2 objection below does not reach an exported
filename, which is ergonomic because identity travels in `page_id` under L8.)*
`slugify` is lossy —
`Deploy: Prod`, `Deploy Prod` and `deploy-prod` all become `deploy-prod`, and
long titles truncate — so two pages can want one filename even though Confluence
enforces unique titles per space. Mirroring narrows this to siblings, and it does
not eliminate it. Refusing follows from the guarantees rather than from taste:
overwriting is out under **S3**, appending a page id would make the filename
depend on walk order (**L2** in the export direction), and skipping with a warning
is the quiet incompleteness this spec exists to remove.

**The adopt-an-existing-page flow falls out of this for free.** Export a native
page, and its attachments land under its directory with the markdown referencing
them there. Republish, and markfluence records
`path=home/onboarding/diagram.png`. The page becomes markfluence-managed, in a
layout someone would plausibly have written by hand, without anyone deciding
anything.

## How the guarantees are fixed

| | before | after |
|---|---|---|
| **L1** | `sub/dup.md` reaches `./dup.md` | resolution is by path, so a basename cannot match |
| **C1** | true for images, false for links | both resolve page-relative, as a preview does |
| **L2** | the root is the working directory | the root is found by walking up, so invocation does not matter |
| **L3** | identity depends on the referencing page's depth | identity is the asset's root-relative path |
| **L5** | fails for an asset above the page | nothing can escape `--dest`, because nothing is recorded as escaping |
| **L6** | same cause | same fix |

Two are **not** fixed here.

**R1** needs the converter to emit a diagnostic where it emits nothing today.
Separate work, tracked with the `check` command. What this spec contributes is a
model in which "could not resolve" is precisely statable.

**S2** (no-read-outside-root) is also fixed, though it gets more reachable before
it gets safer: path-aware links make traversal possible where basename flattening
made it impossible. The model settles the enforcement completely — links need no
clamp once resolution is an index lookup, and the two remaining reads are an
image leaf and a `parent:` path. Items 2 and 7 are the work.

## How the use cases work

| use case | today | under the model |
|---|---|---|
| 1. one file, no images | works | unchanged |
| 2. export, edit, publish back | works for assets at or below the page | works for any layout |
| 3. images in a subdirectory | works | byte-identical |
| 4. pages sharing an images directory | works | byte-identical; links resolve by path rather than by coincidence |
| 5. whole space | partly — see Scenarios A–D | works |
| 6. links across the tree | broken | works |
| 7. first publish of a tree | links resolve only if they point backwards in parent order; a cycle never resolves | every link resolves — `create` reserves ids in phase 2 before converting anything |
| 8. subtree export | does not exist | hierarchy mirrored into directories; attachments placed by provenance |
| 9. moving a file | (a) free, (b) renames and strands | **inverted**: (b) free, (a) renames and strands |
| 10. clone, or CI | depends on the working directory | same result anywhere |
| 11. `attachment-upload` | flattens to a basename | root-relative, like everything else |

Use cases 1, 3 and 4 are **byte-identical** to today. That is what the root
defaulting to the markdown file's own directory buys: the model changes behaviour
only for trees, which is where the problems are.

### Worked example: Use case 5

```
docs/
  <config>
  assets/brand.png
  index.md             ![](assets/brand.png)
  team/
    onboarding.md      ![](images/flow.png)  ![](../assets/brand.png)
    images/flow.png
```

| | today | under the model |
|---|---|---|
| `flow.png` source | `images/flow.png` | `team/images/flow.png` |
| `brand.png` from `index.md` | `assets%2Fbrand.png` | `assets%2Fbrand.png` |
| `brand.png` from `team/onboarding.md` | `..%2Fassets%2Fbrand.png` | `assets%2Fbrand.png` |
| `brand.png` after moving a page | renamed, originals stranded | unchanged |
| exporting either page | fails | writes the tree under `--dest` |
| `[x](../team/onboarding.md)` from `ops/` | dead, or the wrong page | resolves |
| running from `docs/team/` | `IMAGE BROKEN` for brand | works |

Page hierarchy is untouched throughout: `parent:` in frontmatter is the only
thing that decides it, per **L8** (no-layout-inference). Nothing infers a parent
from a directory.

### Worked example: Use case 2

`export --dest out/` on a page published from a nested tree writes:

```
out/
  onboarding.md          ← contains ![flow](team/images/flow.png)
  team/images/flow.png                ![brand](assets/brand.png)
  assets/brand.png
```

The markdown lands at the dest root carrying the recorded sources verbatim
(`sourceFor`), which resolve from there and render in a preview. Re-publishing it
— no config, so the root is `out/` — records the same two sources. Same names, no
orphans, so **L5** holds.

The page landed at `out/onboarding.md`, not at a path mirroring the disk layout it
was published from — `team/` is a source-tree directory, and export has no way to
know it. Multi-page export mirrors the *Confluence hierarchy* instead, which is a
different tree; for a single page neither matters.

## What changes

1. **Thread a root through the converter.** `MdToConfluence` takes it instead of
   calling `os.Getwd()`.
2. **Scope reads to an `os.Root`** on the documentation root, replacing
   `convert.withinRoot`'s lexical comparison. This is what makes S2 hold by
   construction and retires the symlinked-checkout failure without special-casing
   it. `os.Root` is the backstop for an escape through an intermediate symlinked
   directory; item 7 covers the leaf. Its `path escapes from parent` message wants
   wrapping the way `internal/attachfile` already wraps it. Reads through a root
   handle also mean the converter takes the handle rather than a root string.
3. **Record `Source` root-relative** in `images.go`.
4. **Root-relative link resolution.** `docKey` keeps the path, and the page and
   anchor indexes cover the tree below the root. The index is **built once and
   passed in**, not rebuilt per conversion — which means `MdToConfluence` takes
   it alongside the root. That is what keeps the walk O(n), and it happens to fix
   the quadratic cost the per-directory version already has.
5. **Config file discovery**: walk up from the working directory, stat a known
   filename at each level, stop at the first hit or at the filesystem root.
   Discovery stats a filename rather than listing a directory, so it needs only
   execute permission on each ancestor — which is guaranteed, since otherwise the
   working directory would be unreachable. Treat `EACCES` as "not here, keep
   walking" rather than fatal. Reaching the root without a hit is not an error.
6. **Review the security history of walk-up discovery before implementing it.**
   Walking up out of your own tree and trusting what you find there is the shape
   of [CVE-2022-24765](https://github.blog/2022-04-12-git-security-vulnerability-announced/),
   which git answered with `safe.directory` ownership checks. markfluence
   executes nothing from a config file, so the exposure is narrower — but the
   discovered root decides every attachment name and bounds what may be
   published. `.editorconfig` has the closest discovery model to what is proposed
   here, so its issues are the ones worth reading before this is built.
7. **Refuse symlinks at the two remaining reads** — an image leaf (`os.Lstat`,
   refuse anything not a regular file) and a frontmatter `parent:` path. Link and
   anchor resolution needs nothing, since the index is a lookup built by a walk
   that does not traverse symlinks. `cmd/create` gains the root handle so
   `resolveParent` can open through it.
8. **`attachment-upload`**: root-relative source rather than `filepath.Base`.
9. **`attachfile.Resolve`**: nothing can escape any more, so keep the clamp as a
   guard against server data rather than as a rule about layouts.
10. **Shorten the attachment comment format.** The comment caps at 255 characters
    and its overhead is what bounds a recorded path, so the format is a budget
    decision rather than a cosmetic one:

    | format | overhead | path budget |
    |---|---|---|
    | `markfluence: sha256=<64> path=` (today) | 90 | 165 |
    | `mf: s=<64> p=` | 73 | 182 |
    | `markfluence: sha256=<32> path=` | 58 | 197 |
    | `mf: s=<32> p=` | 41 | 214 |
    | `markfluence: s=<64> p=` (keys only) | 82 | 173 |

    **The checksum is the bigger lever**, at 64 of the 90 characters. It answers
    "did these bytes change" rather than resisting an adversary, so 128 bits is
    already more than the job needs — truncating it buys roughly twice what
    renaming the keys does.

    Shortening the keys alone buys 8 characters and costs nothing at all, which the
    table's last row shows. Against that, the prefix is not only overhead: it is
    the **ownership marker**
    `AttachmentMeta.Managed` tests, and therefore what **S5** (remove-only-ours)
    rests on. Someone
    browsing a page's attachments in Confluence can guess what `markfluence: `
    means and cannot guess `mf:`. Worth weighing 17 characters against a
    self-describing marker on data other people will read.
    `parseAttachmentComment` already tolerates a legacy form, so accepting both
    spellings costs little.

11. **Restructure `create` into three phases** — preflight, reserve, publish.
    Phase 2 creates a stub per page (title and parent, no content) and captures
    the ids; phase 3 converts and updates everything, so every link resolves
    regardless of order. Phase 2 keeps the topological ordering, since a page
    needs its parent's id at creation time. `--dry-run` must still create nothing.

12. **Regression suite**: the root becomes explicit in `test.input`, and the
    `images-shared-parent` golden changes — that case is the whole point.
13. **`--root`**, a persistent flag overriding discovery, with completion.
14. **Report the discovered root** in human output and in `--json`. The JSON half
    is not free: `schema/json-output/v1.json` has no `root` field, and the
    project's own rule is that every result field lives on a typed struct with
    `additionalProperties:false`, so this means a schema change plus conformance
    updates for every command. Nothing budgeted that until now.
15. **Read `.env` from the discovered root**, falling back to the working
    directory when there is no project file. `internal/client.Resolve` reads
    `dotenvPath` (`".env"`) against the working directory today, and it is the
    single place this is resolved.
16. **Multi-page export.** The whole of *Multi-page export layout*: mirror the
    hierarchy into directories, place attachments by provenance, teach
    `StorageToMarkdown` a per-page prefix for unsourced attachments, adopt the
    same rule in single-page export, and refuse slug collisions naming both
    pages. This is the largest single piece of work in the list and depends on
    `internal/pagetree` for the walk.
17. **Docs**: the README path rules, a `docs/` entry for the root model, and
    status updates in [docs/guarantees.md](../docs/guarantees.md).
