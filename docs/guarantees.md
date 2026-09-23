# Design principles

These are the principles that guide design decisions in markfluence. Cite them
by id. A change that breaks one needs an argument. If a new feature cannot hold
one, the problem is probably in the design of the feature, and not in the
principle.

The [confluence/](confluence/) directory records what we know about
*Confluence*. This document is its counterpart: it states what **markfluence**
aims to do.

This document does not say whether the code holds each principle today. A gap
between a principle and the code is a GitHub issue that names the principle by
id. Thus this document changes when a principle changes, and at no other time.

## How to read an entry

Each entry has an id, a label, and a statement, and then these parts:

- **Why:** the reason for the principle.
- **Rules out:** the designs that the principle forbids.
- **Accepts:** a trade-off that we chose on purpose. Only some entries have
  one. An Accepts records a decision. It never records a bug.

## Rules for changes to this document

**Ids and labels are permanent.** Never change the id or the label of a
principle, and never use an id again. Plans, pull requests, and code comments
cite them, and a changed or reused id makes an old reference wrong with no
warning. If the id and the label ever disagree, the id is correct.

You can reword a statement when the principle itself changes. If a principle
stops being useful, mark it as retired and give the reason. Do not delete it.

*Retired: "Nothing in Confluence is deleted." This was a fact about the code,
and not a principle. It would become false on the day that the first prune
feature shipped. S4, S5, and S6 replace it.*

## Safety

If a safety principle fails, the result is damage, and not only a wrong answer.

### S1 `no-write-outside-root`

markfluence writes no file outside the destination of a command. For `export`
and `attachment-download`, that is `--dest`. For everything else, it is the
documentation root.

**Why:** Data from the server decides where some files go. An attachment
records the path of its source, and `..` is correct in a source path. Thus a
recorded path can point outside the destination.

**Rules out:** A clip of a path that escapes. A clip writes *something*, with a
name that nobody chose. markfluence refuses the path.

### S2 `no-read-outside-root`

markfluence reads no file outside the root.

**Why:** A publish sends what it reads to Confluence. A reference in a
Markdown file must not publish a file from outside the project.

**Rules out:**

- A check of the text of a path only. A directory in the path that is a
  symlink can escape while the text stays inside the root.
- A `parent:` path outside the root that publishes as though it had no parent.
  This is an error. A publish under the wrong parent, or under no parent, is
  worse than no publish.

### S3 `no-overwrite-without-force`

markfluence does not overwrite a file that exists, unless you give `--force`.

The scope is the files that markfluence writes from a page: the Markdown files
and attachments that `export` and `attachment-download` write. The
metadata that `create` writes back to a file or to `markfluence.yaml` is not an
overwrite. It changes only the fields that it records. The action log only
appends.

**Why:** A file on disk can have edits that exist nowhere else.

**Rules out:** An export that replaces a file that you edited, because you ran
it again.

### An overwrite and a removal are different risks

S3 covers overwrites, and S4 to S6 cover removals, because the two risks have
different shapes.

An overwrite is **in scope** of the operation. You told `export` to write to
that path. Thus the risk is one file, at a path that you named, and a flag
gives your consent.

A removal is **out of scope**. "Publish this file" or "export this page" does
not include a removal. Thus the risk has no limit: which things, and how many.
"Not without `--force`" is the wrong shape for a removal. The correct shape is
that a removal does not occur unless you asked for a removal.

### S4 `no-removal-as-side-effect`

markfluence removes nothing as a side effect. A removal occurs only when it is
the stated purpose of a command.

**Why:** See above. Nobody who asks for a publish or an export expects to lose
anything.

**Rules out:** A publish that deletes the attachments that no file references
now. An export that deletes the files of pages that are gone. Those removals
come as their own commands: #99 and #129.

### S5 `remove-only-ours`

markfluence removes only what markfluence created.

**Why:** A person can attach a file to a page by hand, or put a file in a
directory. markfluence cannot know why it is there.

**Rules out:** A prune that removes an attachment that markfluence did not
upload.

### S6 `removal-is-previewable`

A command that removes things says what it will remove before it removes them,
and it obeys `--dry-run`.

**Why:** markfluence cannot undo a removal.

**Rules out:** A command that removes things and has no `--dry-run`.

### S7 `no-partial-create`

If `create` cannot publish a file, it leaves no page behind.

**Why:** An empty page, and a `page_id` that the author did not ask for, must
be undone by hand. A second run refuses, because the id is already in use.

**Rules out:** A check that finds a defect in a file on disk after `create`
made a page. Every defect that the converter can find must stop the batch
before the first page exists.

**Accepts:**

- `create` reserves every page before it publishes any, so that link
  resolution does not depend on the sequence of creation. Thus some failures
  during publish leave an empty page: a server or network failure, or an asset
  that markfluence cannot read when it uploads. The `page_id` of that page is
  already on disk, in the file or in its `pages:` entry, so
  `markfluence update` completes the work. A delete of the empty page would be
  a removal as a side effect, which S4 forbids.
- With `--no-persist`, nothing on disk records the `page_id`. Then the output
  of the run is the only record of an empty page.
- `create` checks a `page_status` only after the page exists. The statuses
  that a page can have depend on the page, and the page does not exist before.
  Thus a bad status is a warning on a page that `create` made.

### S8 `no-overwrite-of-a-moved-page`

markfluence does not overwrite a page that has a newer version than the base of
the local copy, unless you give `--force`.

**Why:** S3 protects a *file*. S8 protects a *page*, and the page is where the
work of other persons is. A page that changed after your copy came from it has
edits that your copy does not have.

**Rules out:**

- An `update` that publishes over a page that somebody edited after your last
  publish or export.
- A base that the page records. The base is different for each copy, and the
  page cannot know where a copy came from.

**Accepts:**

- Protection needs a base, and the base is local. markfluence records it in the
  action log under a `markfluence.yaml`, when you publish or export a file. A
  file with no base publishes with no check. This includes every file in a new
  clone, because the log is not committed.
- Without a `markfluence.yaml`, there is no action log and thus no protection.
  markfluence does not create a project file for you (#139).
- `--force` overrides S8, and that is the purpose of the flag. When a
  repository is the source of truth, an edit in the Confluence UI is drift that
  you overwrite, and not work that you protect.

## Laws

The laws are algebraic properties of the 3 mappings that markfluence does:

- **Resolve**: a Markdown reference to a local file.
- **Name**: a local file to a Confluence identity.
- **Place**: a Confluence attachment to a local file.

We state each law so that a property test can generate trees and assert it.

### L1 `resolve-what-was-named`

A reference resolves to the file that it names, or to nothing.

**Why:** A near match publishes a link to the wrong page, and nothing reports
it.

**Rules out:** A lookup by base name. That is how a link to `sub/dup.md` once
got to `./dup.md`.

### L2 `invocation-independent`

How a reference resolves, and the name of an attachment, depend only on the
files on disk. They do not depend on the working directory, or on the other
files in the same command.

**Why:** Two persons, or a person and CI, publish the same tree. They must get
the same result.

**Rules out:**

- A root that comes from the working directory, or from the set of arguments.
  Otherwise the same file gets a different name in each batch.
- A `pages:` key that markfluence resolves through a symlink. Then the meaning
  of a key depends on the layout of a checkout.
- Page metadata from flags in `update`. A file that `update` publishes names
  its page and its metadata on disk. That is why `update` has no `--title`,
  `--page-id`, or `--page-width`.
- A cache on disk that changes the bytes that markfluence publishes.

The law controls resolution and naming only. The flags of `create`, such as
`--title` and `--parent`, change what markfluence publishes, and that is their
purpose.

**Accepts:** Two checkouts of the same tree can *behave* differently, because
the action log is local and not committed. The log decides *whether* `update`
sends a body: one person has a base and gets a refusal for a moved page, or a
skip for a body that did not change. The other has no base and publishes. A
merge base is different for each copy by definition. The bytes do not differ:
with the same files and the same page, a run that publishes sends the same
bytes.

### L3 `identity-from-asset-location`

The identity of an attachment depends only on the file name of the asset. A
move of the asset keeps the same attachment.

The label is older than the statement. The identity once came from the
location, and the label is permanent.

**Why:** A move of a page or an asset in the tree is a usual operation. If the
name moved with the file, each move would upload a new attachment and leave
the old one on the page.

**Rules out:** An attachment name that comes from the path of the asset.

**Accepts:**

- Two assets in one document with the same file name cannot both publish. R2
  reports it.
- A rename of an asset leaves the old attachment on the page, because
  markfluence removes nothing as a side effect (S4). #99 is where a prune will
  arrive.

### L4 `publish-is-idempotent`

A publish of a file that did not change makes no change in Confluence.

**Why:** A publish of a body that did not change gives the page a new version.
It sends a notification to every watcher, and it fills the history of the page
with versions that are all the same.

**Rules out:** A decision about "changed" from the mtime of the file. git does
not keep mtimes, so a clone, a checkout, or a CI run looks the same as an edit.
The decision must come from the content that markfluence would send.

The scope is the **body**. markfluence still uploads an attachment whose bytes
changed, and it still asserts a width or labels that the file declares. Thus
the result is "no change", and not "no request at all".

**Accepts:** The same dependency as S8. The comparison needs a record of the
last publish. That record exists only under a `markfluence.yaml`, and only
after the first publish or export of the file. Without it, `update` publishes
the body again.

### L5 `roundtrip-from-confluence`

If you export a page and then publish it again with no edits, the page keeps
its meaning. A second export gives the same Markdown as the first.

**Why:** Persons edit exported files and publish them. A round trip that loses
content makes export a trap.

**Rules out:** A conversion that drops a construct that it cannot map to
Markdown. markfluence keeps the storage format as raw markup instead, and
publishes it again unchanged.

**Accepts:** The stored storage format can change. The Confluence editor writes
some forms that the converter does not, and both forms render the same way
([storage-format.md](confluence/storage-format.md)). The design target of the
converter is equivalence of meaning, and not byte-for-byte equivalence.

### L6 `roundtrip-from-disk`

If you publish a file and then export it, the result is Markdown that publishes
to the same page.

**Why:** An export of a page that markfluence published must not be a
different document.

**Rules out:** A published form that the reverse conversion cannot read back.
If the converter writes a construct, the reverse conversion must recognize it.

**Accepts:** The same as L5. The exported Markdown can differ in spelling from
the file that you published, but it publishes to the same page.

### L7 `output-is-valid-markdown`

Everything that markfluence writes to disk is Markdown that renders.

**Why:** Persons and tools read these files, and markfluence publishes them
again.

**Rules out:** Output that only markfluence can read, such as a link
destination that is not encoded and thus does not parse.

L7 is separate from C2 because each one has a different external
specification. A file with bad frontmatter can still render as Markdown.

### L8 `no-layout-inference`

markfluence never gets the identity or the hierarchy of a page from the layout
on disk.

**Why:** A move or a rename in the tree is a usual operation. It must not
change which page a file publishes to, or where that page is. `page_id` gives
the identity, and `parent:` gives the hierarchy.

**Rules out:** A directory that becomes a parent page, or a file name that
selects a page. The file names that `export` writes are for persons only.

### L9 `declared-metadata-is-asserted`

markfluence makes a metadata field that a file declares true of the page. A
field that the file does not declare leaves the page alone.

**Why:** Without this rule, each new field has two bad defaults. If
markfluence always asserts the field, a run that never mentioned it reverts a
page that a person set up by hand, with no message. If markfluence never
asserts it, you cannot use the field to manage anything. The declaration is the
signal.

**Rules out:**

- A request about a field that the file does not declare. That includes a
  read.
- A verb that writes the file from the page. Every verb that writes goes in one
  direction: from the file to the page. To adopt a page that a person set up by
  hand, look at the page (`page-info`, `read`, `diff`), and then edit the file.

**Accepts:** A file cannot clear a scalar field such as `page_status`. An empty
value looks exactly like an unfinished edit, so markfluence refuses it. Use
the Confluence UI to clear a status. `labels: []` can mean "remove them all",
because an empty sequence has its own spelling.

## Conformance

### C1 `preview-compatible-resolution`

A reference resolves the same way that a Markdown preview resolves it, the
GitHub preview included.

**Why:** Authors check their documents in a preview. If markfluence resolves a
reference differently, the preview shows the wrong thing.

**Rules out:** A link that resolves by base name in one directory, or relative
to anything other than the file that contains it.

C1 is separate from L1. We could give up C1, use our own resolution rules, and
document them. We could not give up L1.

### C2 `frontmatter-is-valid-yaml`

The frontmatter that markfluence writes parses as YAML, and it reads back as
the values that markfluence wrote. A value is a single-line scalar, or a
sequence in either YAML style whose elements are all single-line scalars.

**Why:** Other tools read these files: editors, GitHub, and YAML linters. A
file that only markfluence reads correctly is broken.

**Rules out:**

- A parser that is not a YAML parser, such as one that splits each line at the
  first `:`.
- A value that a conforming reader gets as a different type, such as `.inf` as
  a float or `page_id: "123"` as a string.
- A write of one field that changes the text of another field.

**Accepts:**

- markfluence checks its output with its own YAML library only. That proves
  that the library can read what it wrote, and not that every implementation
  can. A difference that a real tool reports is an issue to fix.
- A rewrite keeps the YAML style of a list, but not its indent.

## Reporting

These are not invariants. A publish of a dead link can be acceptable. A publish
of a dead link **with no message** is not acceptable. The obligation is to tell
the user.

### R1 `report-unresolved-references`

markfluence reports every reference that it could not resolve.

**Why:** Otherwise a reader finds the dead link on the published page, too
late.

**Rules out:** A publish of an unresolved link or image with no message.

The scope is the references that markfluence tries to resolve: links to `.md`
files and images. markfluence uploads only images, so it does not check a
relative link to any other file, such as a PDF.

### R2 `report-unplaceable-attachments`

markfluence reports every attachment that it could not name or place.

The label is older than "name". It is permanent.

**Why:** An attachment that markfluence cannot name is as unusable as one that
it cannot place.

**Rules out:** A publish that drops one of two assets that need the same name.
A name is unique on a page, so markfluence refuses the file and names both
paths.

## Non-goals

These are decisions about what markfluence will not do. A request that needs a
reversal of one is a design discussion, and not a bug report.

### Symlinks

**markfluence does not follow symlinks.** This includes a symlink that goes
outside the project, and a symlink that stays inside it. There is one rule,
because two rules made a system where the same symlink worked for an image and
failed for a link.

This rule has two good results.

**The origin of the root stops being important.** markfluence addresses paths
relative to the open root. Thus a tree in a checkout that is a symlink works.
On macOS, `/tmp` is `/private/tmp`, and home directories are frequently links.
None of that needs a special case.

**markfluence does not support an asset directory that you share with a
symlink**, on purpose. Persons use a symlink to let many pages use one asset
directory. markfluence records attachment sources relative to the root, and
that gives the same result directly. Also, git symlinks do not work on Windows
without `core.symlinks` and developer mode. Thus a repository that uses them
cannot be cloned everywhere.

## When markfluence cannot hold a principle

markfluence does its best. If that is not possible, it gives an error that
names the problem and a safe next step. It never gives a partial success with
no message.

This is a policy, and not a principle. A property test cannot prove it, and it
applies to all of the principles above. It is also the reason that S1 refuses
a path, and does not clip it.
