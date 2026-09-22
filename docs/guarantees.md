# What markfluence guarantees

These are the properties that markfluence holds itself to. You cite them. A
change that breaks one needs an argument. If a new feature cannot hold one, the
problem is probably in the design of the feature, and not in the guarantee.

The [confluence/](confluence/) directory records what we know about
*Confluence*. This document is its counterpart: it gives claims about
**markfluence**.

Each guarantee is about one thing on purpose, so you can argue about it
separately.

## How to read an entry

Every guarantee has a status. Every entry in [confluence/](confluence/) gives
its provenance for the same reason: "we promise this" and "we intend this" need
different levels of trust.

- **Holds**: it is true now. The entry names the thing that enforces it.
- **Partial**: it is true on some paths. The entry names the paths where it
  fails.
- **Aspirational**: it is not true yet. The entry names what would make it true.
- **Vacuous**: nothing uses it yet. It is not tested, so it is not proved.

## Rules for changes to this document

**Identifiers are permanent.** Never change the number of a guarantee, and never
use a number again. Each guarantee also has a **label**, such as
`no-read-outside-root`. The label helps you to read and cite the guarantee. In
a heading, "S2 (no-read-outside-root)" gives enough information, and you do not
need a full sentence to explain it.

Labels are also permanent, for the same reason as the ids. Persons cite them,
and a changed label makes an old reference wrong with no warning. If the id and
the label ever disagree, the id is correct. If a guarantee stops being useful,
mark it as retired and give the reason. Do not delete it. Plans and pull
requests cite guarantees by id, and an id used again makes an old reference
wrong with no warning.

**A change cannot lower a status without a statement.** A change from Holds to
Partial is a decision. Write it in the commit message and in this file. Do not
let it be a side effect that somebody finds later.

*Retired: "Nothing in Confluence is deleted." This was a fact about the code,
and not a principle. It would become false on the day that the first prune
feature shipped. S4, S5, and S6 replace it. They stay true with that feature,
and they control how it works.*

## Safety

If a safety guarantee fails, the result is damage, and not only a wrong answer.

| | label | guarantee | status |
|---|---|---|---|
| **S1** | `no-write-outside-root` | markfluence writes no file outside the root. | Holds |
| **S2** | `no-read-outside-root` | markfluence reads no file outside the root. | Holds |
| **S3** | `no-overwrite-without-force` | markfluence does not overwrite a file that exists, unless you give `--force`. | Holds |
| **S4** | `no-removal-as-side-effect` | markfluence removes nothing as a side effect. A removal occurs only when it is the stated purpose of a command. | Vacuous |
| **S5** | `remove-only-ours` | markfluence removes only what markfluence created. | Vacuous |
| **S6** | `removal-is-previewable` | A command that removes things says what it will remove before it removes them, and it obeys `--dry-run`. | Vacuous |
| **S7** | `no-partial-create` | If `create` cannot publish a file, it leaves no page behind. | Partial |
| **S8** | `no-overwrite-of-a-moved-page` | markfluence does not overwrite a page that has a newer version than the base of the local copy, unless you give `--force`. | Partial |

**S1**: `attachfile.Resolve` enforces it. It refuses a path that goes outside
the root. It does not clip the path.

**S2** now holds for all 3 reads that `_plans/025` names.

`root.FS` enforces it for the image leaf (`internal/convert/images.go`).
`root.FS` is an `os.Root` that the documentation root bounds. markfluence
refuses a path that escapes by its text before it asks `os.Root`. `os.Root`
also refuses an escape that only it can see: an intermediate directory that is
a symlink. The lexical comparison of `withinRoot` did not find that case. The
same leaf also refuses every symlink with `os.Lstat`, also a symlink that
resolves inside the root.

Link and anchor resolution needs no clamp at all. `internal/linkindex.Build`
goes *down* from the root one time. Thus nothing outside the root can be in the
index, and markfluence never opens a file outside the root for this purpose.
The guarantee holds by construction, and not by a check, as `_plans/025`
describes. See [Non-goals](#symlinks).

markfluence reads a `parent:` path in frontmatter in the same way as the image
leaf (`cmd/create.resolveParent`, through `root.FS`). But the failure is
different on purpose. A parent that escapes, or that is a symlink, is a **hard
error**. It is not an unresolved case that markfluence reports, as a link is.
The parent is important: a publish under the wrong parent, or under no parent
with no message, is worse than no publish (`_plans/026` commit 6).

**S3**: `export` enforces it. It does a check of the destination. It skips the
Markdown file and each attachment unless you give `--force`.

### S8 is about the page, and S3 is about the file

**S3** (`no-overwrite-without-force`) protects a *file* on disk, so it is about
the filesystem. Nothing protected a *page* that exists, and that is the side
where the work of another person is. #149 was about that gap. Thus S8 is a new
guarantee, and not a wider S3.

S8 uses a **merge base**: the version that this copy came from. The base is
different for each copy, because the page cannot know where a local copy came
from. Thus `create`, `update`, and `export` record it locally
(`internal/actionlog`). `update` refuses when the live version is newer than
the base.

S8 is **Partial**. The gap is that protection starts at a later time, and it is
not a defect. A file with no recorded base has nothing to compare. It publishes
with no message, as markfluence did before S8 existed. A file gets protection
the first time that you publish or export it. Thus you need no setup step and
no adopt command.

But the first run after S8 shipped gave no protection to any file that already
existed. A new clone also starts with no base, because the log is local to each
checkout and is not committed. `update` reports the count of files with no
check one time in each run.

`--force` overrides S8, and that is the purpose of the flag. It is not a hole.
When a repository is the source of truth, you publish with `--force`. Then an
edit in the Confluence UI is drift that you overwrite, and not work that you
protect.

### An overwrite and a removal are not the same risk

S3 covers overwrites, and S4 to S6 cover removals, because the two risks have
different shapes.

An overwrite is **in scope** of the operation. You told `export` to write to
that path. Thus the risk is one file, at a path that you named, and a flag
gives your consent.

A removal is **out of scope**. "Publish this file" or "export this page" does
not include a removal. Thus the risk has no limit: which things, and how many.
There is also no defined way to give consent for it. "Not without `--force`" is
the wrong shape for a removal. The correct shape is that a removal does not
occur unless you asked for a removal.

### Why S4 to S6 exist before any command removes things

No command removes a local file (there is no `os.Remove` in the tree). No
command deletes anything in Confluence. Thus all 3 guarantees are vacuous and
not tested. We wrote them anyway, for two reasons. Without them, a guarantee
would stop being true one day with no warning. Also, we can already see two
places where a removal will be necessary:

- **Orphaned attachments.** The identity of an attachment comes from the file
  name of its asset (L3). Thus a rename of an asset leaves the old attachment
  behind on the page. #99 tracks a future `attachment-prune` command to remove
  those attachments.
- **`export --clean`.** Subtree export will need it. Then a new export does not
  leave stale files for pages that somebody deleted in Confluence.

**S5 is the guarantee with force, and the mechanism for it exists.**
`client.AttachmentMeta.Managed` is true when an attachment has the
`markfluence: ` prefix in its comment. It is false for an attachment that a
person uploaded by hand. Now, only `attachment-list` reports it. It lets a
prune remove markfluence attachments that nothing references, and never touch
a file that a person attached by hand.

### S7 and the stub that create leaves behind

**S7** is **Partial**. The exact limit is important, because the gap is not
the part that looks dangerous.

`create` has 3 phases:

1. Preflight does a check of every file.
2. Reserve creates a page with no content for each file, and writes its
   `page_id` to the file.
3. Publish converts each file and fills in its page.

If preflight refuses a file, the batch stops and creates nothing. Since #127,
this includes **every defect that the converter can find in the files on
disk**. Preflight converts each file and keeps the error. Thus a document that
the converter refuses never gets to the reserve phase. An example is two assets
that need the same attachment name.

Before #127, such a document got to the reserve phase, and that is why this
guarantee was necessary. The author got a page with no content and a `page_id`
that they did not ask for. A second run then refused, because a page already
had that id.

3 gaps remain, and only the first one is fully remote:

- **A server failure or a network failure during publish.** The stub stays, and
  its `page_id` is already in the frontmatter. Thus a plain `markfluence update`
  completes the publish.
- **An attachment that markfluence cannot read.** `client.SyncAttachments`
  opens every asset to calculate its checksum and to upload it. The converter
  never opens an asset: it only calls `Lstat`. Thus an unreadable image fails
  in the publish phase, locally, after the stub exists. Examples are an image
  with mode `000`, or a file that somebody replaced between the two steps.
  Preflight does not do this check, on purpose. It would copy the read that the
  upload does anyway. It would also race the filesystem, so the check can pass
  and the upload can still fail.
- **A frontmatter file that markfluence cannot write.** `reserveOne` calls
  `os.WriteFile` *after* `CreatePage`. Thus a read-only `.md` file leaves a
  stub whose id is **not** in the file. This is the one case where
  `markfluence update` cannot continue the work, because nothing on disk names
  the page. For this reason, `failKeepingPage` keeps the id and the URL in the
  result. The output of the run is the only remaining record.

The first gap is a decision, and not a thing that we forgot. `_plans/026`
accepted it as the cost of a reservation of every id before any conversion.
That reservation stopped link resolution from depending on the sequence of
creation. In all 3 cases, a page exists that the command reported as failed.
Thus the guarantee does not hold as written.

To remove the gap, markfluence would have to delete the stub. This guarantee
cannot approve that change alone. The change would make **S4** and **S5**
not vacuous. S4 says that a removal occurs only when it is the stated purpose
of a command.

## Laws

The laws are algebraic properties of the 3 mappings that markfluence does:

- **Resolve**: a Markdown reference to a local file.
- **Name**: a local file to a Confluence identity.
- **Place**: a Confluence attachment to a local file.

We state each law so that a property test can generate trees and assert it.

| | label | guarantee | status |
|---|---|---|---|
| **L1** | `resolve-what-was-named` | A reference resolves to the file that it names, or to nothing. | Holds |
| **L2** | `invocation-independent` | How a reference resolves, and the name of an attachment, depend only on the files on disk. They do not depend on the working directory, or on the other files in the same command. | Holds |
| **L3** | `identity-from-asset-location` | The identity of an attachment depends only on the file name of the asset. A move of the asset keeps the same attachment. | Holds |
| **L4** | `publish-is-idempotent` | A publish of a file that did not change makes no change in Confluence. | Holds |
| **L5** | `roundtrip-from-confluence` | If you export a page and then publish it again with no edits, the page does not change. | Partial |
| **L6** | `roundtrip-from-disk` | If you publish a file and then export it, the result is Markdown that publishes to the same page. | Partial |
| **L7** | `output-is-valid-markdown` | Everything that markfluence writes to disk is Markdown that renders. | Holds |
| **L8** | `no-layout-inference` | markfluence never gets the identity or the hierarchy of a page from the layout on disk. | Holds |
| **L9** | `declared-metadata-is-asserted` | markfluence makes a metadata field that a file declares true of the page. A field that the file does not declare leaves the page alone. | Partial |

**L1** is about correctness, and not about how many files match. A lookup by
base name used to resolve to exactly one file, but not always the file that the
reference named. That is how a link to `sub/dup.md` got to `./dup.md`.
`internal/linkindex` now resolves by path. Thus a base name cannot match the
wrong file (`_plans/026` commit 5).

**L2** is narrow on purpose. A flag such as the `--title` flag of `create`
changes what markfluence publishes, and that is its purpose. Thus the law
controls resolution and naming only.

In that scope, the law does not let the root come from the working directory. It
also does not let the root come from the *set* of arguments. Otherwise, the same
file would get a different name for each batch. `internal/project` finds the
root when it goes up from the directory of each file. The working directory and
the other files in the command do not change the result (`_plans/026` commits 1
to 4).

A `space:` or `page_width:` default for the whole project in `markfluence.yaml`
(#100) is in that scope, and it helps L2. A committed file on disk declares the
value. markfluence finds that file by the same walk, which does not depend on
the working directory. Thus two persons in different directories resolve it the
same way. It is better for L2 than the `--space` flag that it replaces, because
a flag is invocation state by definition. The status does not change.

A `pages:` entry (#139) takes the same argument further. That is why the path
keys are **lexical and relative to the root**, and markfluence does not resolve
them. If markfluence resolved a symlink, the meaning of a key would depend on
the layout of a checkout. Then the same repository could publish to different
pages on two machines, and L2 forbids exactly that.

It is also why `update` lost `--title`, `--page-id`, and `--page-width`. Page
metadata now lives only in files on disk. Thus the page that a file publishes
to does not depend on how you ran the command. The exception that L2 gives to
flags is narrower than it was.

One thing is outside L2, and it is better to say so now than to find it later.
The action log (#149) is **local to each checkout and not committed**. Thus two
persons who run the same command on the same tree can get different
*behavior*. One of them has a base for a file and gets a refusal for a moved
page. The other has no base and publishes it.

That is not L2 as written. L2 controls how a reference resolves and the name of
an attachment, and neither of those changes. But it is against the purpose of
the law. It is also the reason that markfluence never keeps
`pagedoc.UserCache` on disk.

The difference cannot be avoided. A merge base is different for each copy by
definition. The alternative is to commit the log, and that would help only the
one arrangement that does not need it. The published result does not change:
with the same files and the same page, a run that publishes sends the same
bytes.

**L3** is why a move of a page or of an asset costs nothing. Two places in the
code enforce it:

- `convert.AttachmentFilename` (`internal/convert/attachname.go`) gives an
  attachment the base name of its file, and nothing else.
- `planAttachments` (`internal/client/client.go`) matches each local file to
  an attachment on the page by that name. When the name agrees but the recorded
  `Source` path does not, it updates the attachment to record the new path. It
  does not create a second attachment.

Thus a move keeps the attachment, and a rename of the file creates a new
attachment. The old attachment stays on the page, because markfluence never
deletes an attachment (see S4 to S6).

The statement of L3 was corrected. It said that the identity depends on the
*location* of the asset. That was true when the name was the whole path,
encoded (`_plans/026` commit 4). It became false when the name became the base
name (`_plans/029`). The label is permanent (see *Rules for changes to this
document*), so `identity-from-asset-location` does not change, although the
guarantee is now about the file name. The status does not change.

A history note: an earlier version of this entry said that a move of an asset
changes its identity, and that a fix would need names from the content of the
file. Neither is true now. The name never had to build the tree again on
export, because the attachment comment holds the path.

**L4** had the status Holds when it was not true. It is better to record the
correction than to change it with no statement. The skip in `update` used the
mtime of the file, and git does not keep mtimes. Thus a clone, pull, checkout,
`touch`, file copy, or backup restore looked the same as an edit. Each one made
markfluence publish a file that did not change.

On a new CI checkout, the mtime of every file is the time of the clone. Thus
the whole tree was published again on every run. That is a counterexample, and
not an edge case. The status should have been **Partial** from the first day.

L4 holds now, by construction and not by approximation. `update` calculates a
sha of exactly what the body `PUT` would send: the resolved title and the
rendered body. It compares that sha with the sha that the last publish
recorded. When they agree, it sends no request (#149, `internal/actionlog`).
Nothing reads a clock.

The scope of L4 is the **body**. markfluence still uploads an attachment whose
bytes changed. It still asserts a width or a label that the file declares,
because each one has its own pass. That is why the result is "no change", and
not "no request at all".

A publish of a body that did not change would give the page a new version. It
would send a notification to every watcher. It would also fill the history of
the page with versions that are all the same.

**L5** and **L6** stay **Partial**. This file said that #59 would decide them.
#59 showed that they cannot have the status Holds as they are written.

Multi-page export now exists, and the earlier note said that it was missing.
It has attachment placement from provenance, it mirrors directories, and it
writes a tree whose `parent:` paths let it publish into new pages
(`_plans/029`).

The attachment part of the round trip is also correct now, because of a
different change than we expected. An attachment gets the base name of its
file. Thus when export places the images of a page, their attachments do not
move (`_plans/029` §"The thing 025 got wrong").

The problem is the text of the law: *"publish it again with no edits, the page
does not change"*. We measured it on 2026-09-05 with a live page. An export and
a new publish change the stored storage format in two ways that have no relation
to the content:

- The editor of Confluence writes `<li><p>text</p></li>`. The converter writes
  `<li>text</li>`.
- A TOC macro has `ac:local-id`, `ac:macro-id`, and `data-layout` attributes.
  The canonical form of the converter does not have them.

Both forms render the same way, and neither form loses anything. That is the
stated design target of the converter: equivalence of meaning, and not
byte-for-byte equivalence. Thus the law as written asks for a thing that
markfluence does not do, on purpose. The honest reading is that L5 and L6 have
the wrong shape, and not that markfluence fails them. A change to their text is
a separate decision, and this document does not make it.

A property test now proves one thing (`TestRoundTripMarkdownIsAFixedPoint`,
over every `storage2md` case and not a list that a person keeps). **Markdown is
a fixed point.** Export a page, publish that Markdown again, and export again.
The Markdown is the same. After a page goes through markfluence one time, it
stops changing.

That test found real drift on its first run: a hard break got one more leading
space on every cycle. That is the argument for the test. It also answers the
note that this file added when #125 showed that L5 had no property test at all.

We expect two exceptions to the fixed point, and both become stable on the
second cycle. A native Confluence attachment is not managed, so the first new
publish writes a new comment on it. Also, a mention from the editor of
Confluence has an `ri:local-id` that markfluence does not write (#91). Thus the
first new publish removes it. We found that this does no harm. A mention with
only the account id resolves to the same person
([links-and-anchors.md](confluence/links-and-anchors.md)).

**#91 makes the gap in L5 and L6 smaller, but it does not remove it.** A
mention was the most frequent thing that an `<ac:link>` could be: 80% of all
use. Before #91, a mention stayed correct through the round trip only as raw
storage format. It now converts in both directions. Thus the *readable* part of
the round trip covers the case that most real pages have. The laws stay
**Partial** for the reasons above: the text asks for byte-for-byte
equivalence, and the converter does not have that target, on purpose.

**L9** is what makes a new frontmatter field safe. Without it, each new field
has two bad defaults. Assume that markfluence always asserts the field. Then a
run that never mentioned the field reverts a page that a person set up by hand,
with no message. If markfluence never asserts the field, you cannot use it to
manage anything. The declaration of the field is the signal.

L9 is **Partial**, and the gap is `page_width`, not `labels`. `update` obeys
the law exactly. It asserts a `page_width` line, and an absent line leaves the
live width alone. But `create` asserts a *default* width of `max` for a file
that declares no width. Thus in `create`, an absent field does not leave the
page alone.

`labels` holds in both verbs. If the field is absent, markfluence makes no
label request at all. That is stronger than "no write", and a test in
`cmd/update` pins it.

`page_status` (#168) holds in both verbs in the same way as `labels`, with the
same "no request at all" test. It has one difference from `labels`, and it is
better to write it down than to let a reader guess it. A file can set and
change a status, but a file **cannot clear** a status.

`labels: []` can mean "remove them all", because an empty sequence has its own
spelling, and that spelling is different from a null scalar. A scalar field has
no such spelling. `page_status:`, `page_status: ~`, and `page_status: null` all
read as `""`, and that is exactly what an unfinished edit looks like. Thus
markfluence refuses an empty value. That is a missing *declaration*, and not a
declaration that markfluence does not assert, so the law is not weaker. To
clear a status, use the UI, until somebody asks for a spelling.

The law now has **no exception**. It had one until `fix` was removed (#151).
`fix` changed the *file* to agree with the page. Thus in `fix`, the page was
the authority, and markfluence filled in an absent field.

To `update`, an absent `labels` key meant "leave the page alone". To `fix`, it
meant "use the labels that the page has". A reader had to remember that
difference. Now every verb that writes goes in one direction, and the law
describes all of them.

Thus you adopt a page that a person labeled or resized by hand with a manual
step. Look at the page, and then edit the file. `page-info` shows the labels,
the width, and the page status. `read` and `export` write all three. #154
(`markfluence diff`) is the correct way to see what is different. Nothing
writes the file for you, on purpose.

The status does not change. The default width of `create` makes L9 partial,
and the removal of `fix` did not change that.

## Conformance

| | label | guarantee | status |
|---|---|---|---|
| **C1** | `preview-compatible-resolution` | A reference resolves the same way that a Markdown preview resolves it, the GitHub preview included. | Holds |
| **C2** | `frontmatter-is-valid-yaml` | The frontmatter that markfluence writes parses as YAML, and it reads back as the values that markfluence wrote. A value is a single-line scalar, or a sequence in either YAML style whose elements are all single-line scalars. | Holds |

**C1** is not an internal property. It is agreement with an external
specification. It always held for images, which resolve relative to the page.
Links now resolve the same way. Inside markfluence they are relative to the
root, but markfluence composes them from the directory of the page that
references them, as a preview does. They do not resolve by base name in one
directory (`_plans/026` commit 5).

C1 is separate from L1 because we could, in principle, give up C1.
markfluence could use its own resolution rules and document them. We could not
give up L1.

**C2** was false until #130, and only an outside tool could see that it was
false. `internal/frontmatter` was a flat-key parser that we wrote by hand. It
split each line at the first `:`. Thus it read its own
`title: Deploy Runbook: Part 2` correctly, and every real YAML parser refused
it. The colon was one member of a class. markfluence wrote booleans, numbers,
nulls, flow collections, and the reserved indicators with no quotes. It read
them back as the wrong type, or not at all.

C2 holds now for two reasons. `goccy/go-yaml` parses and writes the block. The
writer also **does a check of its own output**, and it does not trust it. It
writes with the style that goccy chooses, and it reads the result again. When
the two disagree, it writes a double-quoted scalar instead.

That fallback is necessary, and not only an extra safety. goccy drops a tab. It
writes a value that starts with `? ` as a document that it then cannot parse.
It writes `.inf` and `.nan` with no quotes, and every conforming reader then
sees a float.

A predicate that lists those shapes would be incomplete, because we found them
only by experiment. A check is better than a prediction, and the check stays
correct if goccy gets worse.

The check compares the **node kind** that it reads again, and also the text.
That is what makes the `.inf` case work. An earlier version compared only the
text, and it passed. The reader changes every scalar to its token. Thus
markfluence read `.inf` back as `.inf`, and the round trip looked correct. But
the file said "float" to every other tool. A comparison of text compares the
wrong thing.

markfluence enforces the single-line rule when it **reads**. It refuses a
scalar whose source is on more than one line. It must do this, because
markfluence writes a key that it did not change from the node that the parser
made. When goccy writes a parsed node again, the result is not always the same
as the source.

We measured this with the pinned version of goccy. goccy writes a `|` block or
a `>` block again as a block, and then the node-kind whitelist of the reader
refuses it. Thus a write would make a file that markfluence cannot read. In
`create`, that occurs only after markfluence made the page.

A plain scalar on more than one line, and a quoted scalar on more than one
line, are less dangerous. goccy writes both again on one line. The result
parses, but markfluence changes the file of the author with no message. That
must not occur while markfluence sets a different field.

Sequences made this rule larger, but not weaker. A value can now be a list, in
either YAML style. The rule is now: "a *sequence* can be on more than one line,
but every element must be a single-line scalar". Thus a block list is correct,
and a flow list on more than one line is correct. An element that continues on
the next line is not correct.

The element check trims its origin at both ends. A leading newline in the
origin of an element is structure (the item started on a new line), and not
content.

The self-check of the writer had to get a second context. The reason for that
is the nearest thing to a repeat of #130. The scalar check tests a value as a
*mapping* value, and two shapes pass it and then break a list. In a flow
sequence with no quotes, `x,y` becomes two elements, and `has]bracket` ends
the sequence.

Block style has a different trap. There, `? q` is the explicit-key indicator
of YAML, and it parses as a mapping. Thus the check runs in the style that
markfluence will write. Flow is the stricter context, so a check in flow style
is always *safe*. The style parameter gives the minimum of quotes. It protects
against one fault: a check in block style for a write in flow style.

markfluence now writes both styles, because a rewrite keeps the style that it
found. Some sets are large enough to be a block list. The flow spelling of
such a set is a single line that nobody can read.

markfluence keeps the style, but not the format. It writes block items at its
own indent, and not at the indent of the author. Valid YAML in the correct
style is the guarantee. Byte-for-byte preservation is not.

One result is important, because a code comment used the old rule as its
reason. `Normalize` removes blank lines with a **textual** filter. The comment
said that this was safe "because nothing this package emits spans more than one
line". A block list that markfluence passes through makes that false.

The correct statement is that no value that markfluence can write has a
*meaningful* blank line. A blank line between two list items has no effect in
YAML. The shape where a blank line is content is a `|` block, and markfluence
refuses that shape when it reads.

C2 has two limits, and we state them. The quotes come from goccy, but **the
types come from markfluence**. markfluence writes `page_id` and `parent` as
YAML integers and nulls, because `page_id: "123"` is valid YAML that says the
wrong thing. That is a rule that we wrote by hand, and it applies only to two
keys whose sets of values are closed.

Also, the check is a *self*-check. It proves that goccy can read again what
goccy wrote. It does not prove that a different implementation can. There is
no second YAML library in `go.mod` to decide that, on purpose. If a real tool
reports a difference, that is an issue to correct, and the frontmatter that
fails is the evidence.

C2 is separate from **L7** (`output-is-valid-markdown`), because each one has
a different external specification. A file with bad frontmatter still renders
as Markdown. GitHub shows it, and the YAML extension of VSCode was the tool
that found the fault. If L7 included C2, its status would not tell you which
specification failed.

## Reporting

These are not invariants. A publish of a dead link can be acceptable. A publish
of a dead link **with no message** is not acceptable. The obligation is to tell
the user. Thus R1 can be false while nothing calculates a wrong answer.

| | label | guarantee | status |
|---|---|---|---|
| **R1** | `report-unresolved-references` | markfluence reports every reference that it could not resolve. | Holds |
| **R2** | `report-unplaceable-attachments` | markfluence reports every attachment that it could not name or place. | Holds |

**R1** was false by design, and a document said so. The README said that an
unresolved link was "published as-is, which on Confluence is a dead relative
link. There is no warning for this."

An unresolved image already used the `warnings` list. A same-tree `.md` link
that does not resolve was the next thing to go into it (`_plans/026` commit 5).
The status was Partial, and not Holds. That was a minimum warning with a
mechanism that already existed. It was not the dedicated diagnostic that
`_plans/025` described and left for later. That diagnostic tells you *why* a
reference failed, and it lets you audit a tree with no publish.

That dedicated diagnostic now exists (#42). A doc-link target that is missing,
or that resolves outside the documentation root, is Broken. It replaces the
published element, as a broken image already does. Some targets exist but
have no `page_id` yet, or have a `#fragment` that matches no heading. Such a
target gives a warning, and markfluence does not publish it with no message.

Every message gives the source line that it came from, when markfluence can
find it. A link or image with no visible text has no `*ast.Text` to go to.
Thus markfluence reports the message with no line prefix, and not with a wrong
line (the documented `ok=false` case of `nodeLine`). `check` adds the audit of
a tree with no publish. It gives the same diagnostics, and it never touches
Confluence. It only reads the filesystem.

R1 covers the two types of reference that markfluence tries to resolve:
doc-links (`.md` siblings) and images. A relative link to a local file that is
not `.md` and not an image, such as a PDF, gets no existence check at all. That
was true before #42 and after it.

`rewriteDocLink` tries to resolve only an href that ends in `.md`, so
markfluence never tries to resolve that case, by design. markfluence uploads
only images, and a relative href to any other file would be dead in all cases.
That case is outside the claim of R1, and not a part of R1 that fails. Thus it
does not stop the status Holds.

**R2** covers naming and placement. The note is wider, but the label is not.
Labels are permanent (see *Rules for changes to this document*). Thus
`report-unplaceable-attachments` does not change, although the guarantee now
starts one step earlier in the pipeline.

The step is new. Since `_plans/029`, an attachment gets the base name of its
file. Thus two assets in one document can need the same name. There is no
correct way to publish that, because a name is unique on a page. The converter
refuses the file, and it names both paths and both lines.

`attachment-upload` refuses the same collision in a batch. `check` reports it
offline as a Broken entry, in the same list as a dead link. In both cases, the
fix is to rename a file. An attachment that markfluence cannot name
is as unusable as an attachment that it cannot place. The obligation to report
it is the same.

## Non-goals

These are decisions about what markfluence will not do. They are in this
document because they control future work in the same way as a guarantee. A
request that needs a reversal of one is a design discussion, and not a bug
report.

### Symlinks

**markfluence does not follow symlinks.** This includes a symlink that goes
outside the project, and a symlink that stays inside it. There is one rule,
because two rules made a system where the same symlink worked for an image and
failed for a link.

There are 3 points of enforcement, from the most work to the least work:

| where | mechanism | cost |
|---|---|---|
| the link and anchor index | `filepath.WalkDir`, which reports a symlinked directory and does not descend it | free |
| the read of a leaf, such as an image | `os.Lstat`, and a refusal of anything that is not a regular file | one call |
| an escape through an intermediate symlinked directory | `os.Root` bounded to the root, which refuses it | `internal/attachfile` already uses this pattern |

**Verified 2026-08-28.** `WalkDir` from `docs/` over a tree that has
`docs/escape → ../outside`:

```
dir                      docs/
SYMLINK (not descended)  docs/escape       ← outside/out.md never enumerated
dir                      docs/sub/
file                     docs/sub/in.md
```

This is `os.Root` bounded to `docs/`, for the escape case that an `Lstat` on a
leaf cannot see:

| path | `os.Root` |
|---|---|
| no symlink | allowed |
| relative symlink that stays inside the root | allowed |
| relative symlink that escapes the root | refused: `path escapes from parent` |
| absolute symlink, any target | refused |

This rule has two good results.

**The origin of the root stops being important.** markfluence addresses paths
relative to the open root handle. Thus a tree in a checkout that is a symlink
works. On macOS, `/tmp` is `/private/tmp`, and home directories are frequently
links. None of that needs a special case.

**markfluence does not support an asset directory that you share with a
symlink**, on purpose. Persons use a symlink to let many pages use one asset
directory. markfluence records attachment sources relative to the root, and
that gives the same result directly. Also, git symlinks do not work on Windows
without `core.symlinks` and developer mode. Thus a repository that uses them
cannot be cloned everywhere.

This is how **S2** stopped being lexical. The converter does a check of each
image with `root.FS.Lstat` (`internal/convert/images.go`). `root.FS` is an
`os.Root` bounded to the documentation root (`internal/project/project.go`).
It refuses an escape through a symlinked directory, and the `Lstat` refuses a
symlinked leaf. The lexical `convert.withinRoot` clamp, which could not see a
symlinked directory, no longer exists.

One limit remains. The check goes through `os.Root`, but the upload does not.
The converter gives the upload an ordinary path, `filepath.Join(root.Dir,
rootRel)`, and `fileChecksum` and the upload call `os.Open` on it
(`internal/client/client.go`). If somebody replaces a directory with a symlink
between the check and the upload, the uploaded bytes can come from outside the
root. That is a race, and a static layout of files cannot cause it. The S7
note already accepts a file that somebody replaces between the two steps, for
a different reason.

## When markfluence cannot hold a guarantee

markfluence does its best. If that is not possible, it gives an error that
names the problem and a safe next step. It never gives a partial success with
no message.

This is a policy, and not a guarantee. A property test cannot prove it, and it
applies to all of the guarantees above. It is also the reason that S1 refuses
an attachment path that goes outside the root, and does not clip it. A clip
would write *something*, with a name that nobody chose.

## How we do a check of each kind

| kind | how we do the check |
|---|---|
| Safety | adversarial tests: tries to go outside the root, files that already exist, and a preflight that fails and must create nothing |
| Laws | property tests: generate trees and assert the equation |
| Conformance | C1: fixtures, compared with what a Markdown preview renders. C2: the writer does a check of its own output when it runs, and there is a round-trip test and a fuzz target. Agreement with *other* YAML implementations is review judgement |
| Reporting | example tests that assert that a specific message appears |
| Policy | review judgement |
