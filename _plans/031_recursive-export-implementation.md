# Plan: implementing 029 — sequence, checkpoints, reviews

`_plans/029` is the design: what recursive export does, why an attachment is
named by its base name, and what each decision costs. This file owns only the
order the work lands in, where it is reviewed, and what has already diverged
from the plan while building it. When the two disagree about *what* something
does, 029 wins; when they disagree about *when*, this one does.

## The standing rule

**Every commit passes `make check` on its own.** Not the branch tip -- each
commit, so the history can be bisected and each step reviewed as a working
state. This has already forced one reordering (see Divergences), and it will
force more: when a commit breaks something a later commit was scheduled to fix,
the fix moves earlier rather than the suite going red.

## Sequence

### Naming (commits 1-10)

The widest blast radius on the branch: it changes the attachment name on every
page markfluence has ever published.

| # | commit | notes |
|---|---|---|
| 1 | `feat(convert)`: name an attachment by its base name | **done** |
| 2 | `feat(convert)`: refuse two same-base-name assets in one file | compares `rootRel`, not the raw src; includes the raw-storage `ri:filename` scan |
| 3 | `feat(check)`: report a base-name collision offline | no network needed to see it |
| 4 | `refactor(attachmentupload)`: refuse a collision across a batch of FILEs | shrunk; see Divergences |
| 5 | `refactor(convert)`: stop interpreting a stored name | deletes `AttachmentSource`, rewrites `TestRoundTripEncodedImageSources`, fixes `client.go:1062` |
| 6 | `test`: hand-authored fixtures and case renames | shrunk; see Divergences |
| 7 | `docs(confluence)`: `attachments.md`'s framing | the Verified probes stay |
| 8 | `docs`: L3's note, R2's note, `root-model.md:114` | R2's **label** is unchanged |
| 9 | `docs`: README and CLAUDE.md | including the now-void `--attachments-dir` rationale |
| 10 | `docs(plans)`: a correction note on 025 | |

**Checkpoint A** — `/code-review` over commits 1-10, plus an amendment to this
file recording anything else that diverged.

### Placement (commits 11-15)

Where **L5** and the traversal clamp both live.

| # | commit |
|---|---|
| 11 | `refactor`: `slugify` moves out of `cmd/export` |
| 12 | `feat(pagedoc)`: the page's position through `Options`/`StorageOptions` |
| 13 | `feat(attachfile)`: page-scope an attachment with no recorded path; `--flat` opts out |
| 14 | `feat(read)`: page-scoped positions |
| 15 | `feat(attachment-download)`: page-scoped by default, with the `GetPageOrNil` a slug needs |

**Commit 12 is reviewed on its own**, mid-stretch, because its failure mode is
silent: a wrong position compiles, passes the suite, and breaks L5 in a way only
a live round-trip shows. Every other commit here fails loudly.

**Checkpoint B** — `/code-review` over commits 11-15.

### Export (commits 16-22)

| # | commit |
|---|---|
| 16 | `feat(export)`: `--depth`, the walk, the mirrored layout, `parent:` paths, the `--file` refusal, `Args`, completion, `fix`'s help |
| 17 | `feat(export)`: `--space` |
| 18 | `feat(export)`: `markfluence.yaml` at `dest`, and `roots` |
| 19 | `feat(export)`: destination-conflict detection across pages |
| 20 | `feat(export)`: the slug pre-flight and its `-<id>` suffixing |
| 21 | `feat(export)`: skip a rendered page whose file already exists |
| 22 | `feat(export)`: the multi-page summary, `parent_file`, the schema |

**Checkpoint C** — `/code-review` over commits 16-22, then **`/security-review`
over the whole branch**.

Security waits until here rather than running earlier, because the thing worth
reviewing does not exist until then: this feature turns **page titles into
directory names** and attachment names into files beneath them, and that span
crosses commits 11-22. `slugify` drops `/` so a title cannot traverse, and
`attachfile` has both the lexical clamp and `os.Root` -- but every line of that
reasoning was written against attachment *comments* as the untrusted input, and
a title has never been audited as one.

**Live verification** follows immediately, against the personal space
(76646426): the standing three-level fixture 029 §Verification describes. Before
the closing commits, not after, because a finding here changes code rather than
prose.

### Close (commits 23-24)

| # | commit |
|---|---|
| 23 | `test(convert)`: the L5 property test |
| 24 | `docs`: the tree feature, L5/L6 to Holds, README, CLAUDE.md, `pagedoc`'s package comment |

**These are 029's commits 23 and 24 in the opposite order**, deliberately. 029
has the docs commit flipping L5/L6 to Holds and the property test after it,
"gating" it -- a gate that can only be enforced by going back and editing the
previous commit. Landing the test first means the docs commit cites something
that already passes, and if the test cannot be made to pass, the docs commit
simply says Partial and no history needs rewriting.

**Checkpoint D** — whole-branch `/code-review`, then the PR.

## Review policy, and why not more

Six reviews across twenty-four commits: four checkpoints, one solo review of
commit 12, one security pass.

Reviewing every commit was considered and rejected. Most of these commits are
mechanical -- regenerated goldens, a moved function, prose -- and a review finds
little in them. What has actually been productive on this work is reviewing a
*whole coherent artifact*: two reviews of 029 each found a design flaw that no
per-commit reading would have surfaced, because both were about how pieces
interact. The checkpoints are placed where a complete piece of behaviour first
exists.

## Divergences from 029

Recorded as they happen, so 029 stays the design and this stays the log.

1. **Commit 1 absorbed the five regenerable goldens** that 029 scheduled for
   commit 6. Changing the naming scheme changes them immediately, and the
   standing rule does not allow five commits of red suite in between. Commit 6
   keeps the hand-authored `storage2md` inputs and the `images-encoded-src`
   renames.
2. **Commit 1 absorbed `localAttachments`' source fix** from commit 4. It
   round-tripped `Source` through the attachment name, so a base name silently
   recorded `path=x.png` for an asset at `docs/assets/x.png` -- destroying the
   one copy of the path the whole scheme now depends on. 029 did not notice this
   call site. Commit 4 keeps the in-batch refusal.
3. **`decodeName` was deleted in commit 1**, not commit 5: it wrapped
   `AttachmentSource` for a test asserting the invariant that inverted, and an
   unused function fails `lint`, which the standing rule does not permit.
4. **Commits 23 and 24 are swapped**, per the Close section above.
