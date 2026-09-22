# _plans/

Design history. One numbered file per substantial change, written **before** the
change and committed ahead of the branch that implements it.

**These are not documentation.** A plan describes what was intended at the
moment it was written, and it is not revised to match what the code does now.
Current behaviour is `markfluence CMD --help`, [docs/](../docs/), and
[CLAUDE.md](../CLAUDE.md) — in that order. If a plan and the code disagree,
the code is right and the plan is a record of how it got there.

## Why they are in the repository

Because things point at them, and a pointer into a private notebook is a dead
end at exactly the moment somebody needs it.

- **12 Go files cite a plan by number** — `internal/project/project.go`,
  `internal/convert/attachname.go`, `cmd/create/create.go` and others. Those
  comments exist where the code looks arbitrary and the reasoning is long.
- **[docs/guarantees.md](../docs/guarantees.md) cites them 14 times**, and
  load-bearingly: a guarantee's *status* is often justified by the plan that
  set it ("`_plans/026` accepted it as the cost").
- **[docs/root-model.md](../docs/root-model.md) cites them 10 times**, and 22
  commit messages do too — those can never be fixed.

They are also versioned with the code they describe, which a notebook cannot
be: `_plans/039` sits at the commit where the `pages:` block landed.

## How a plan gets written

The workflow, in order:

1. **An issue** describes the problem. Discussion happens there.
2. **A plan** is written from that discussion, numbered `NNN_short-name.md`,
   and committed to `main` on its own — `docs: the space-info plan`. It names
   the issue it answers in its first lines.
3. **A branch** implements it. The plan is the thing the implementation is
   checked against, and it is normal for implementation to falsify part of a
   plan — when it does, the plan is amended in that branch and the commit
   message says what was wrong.
4. **`docs/`, `CLAUDE.md`, and `--help`** carry whatever a reader needs
   afterwards. The plan is not that.

Most plans are written by a coding agent from an interview with the
maintainer, which is why the recurring sections are what they are:

| section | what it holds |
|---|---|
| **What the probe established** | Confluence behaviour *measured*, not assumed — dated. The canonical home for these is [docs/confluence/](../docs/confluence/); a finding that lives only here should be promoted. |
| **Decisions** / **Decisions locked** | choices made, each with the reason, so the next person can tell a deliberate decision from an accident |
| **Implementation** | the intended shape: packages, signatures, call order |
| **Files** | a table of what changes, which doubles as a review checklist |
| **Tests** | what has to be true, especially the regressions a change invites |
| **Not in scope** | the ideas deliberately *not* built, and why. This is the most re-read section: it is the answer to "why doesn't it just…" |

## Reading them

Start from the citation that sent you here rather than from `001`. They are
chronological, not a narrative, and the early ones describe a Python tool this
one replaced.

Numbers are permanent and never reused. A plan whose feature was later removed
stays — `004_fix-subcommand.md` and `040_remove-fix.md` are both history, and
the pair is the useful part.
