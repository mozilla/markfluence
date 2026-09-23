# 049: rewrite the help text in STE100

Finishes #165 ("polish the prose"). The README, CONTRIBUTING, and every
written file in `docs/` got an ASD-STE100 pass in #187. This does the same for
the help text, which is the command reference (#102).

## Scope

For each command: `Short`, `Long`, `Example`, and the usage string of every
flag it registers. The root command counts as one, and its persistent flags are
part of it.

Not in scope: `Use` lines, flag names, defaults, or any behavior. If the review
finds that the text is right and the code is wrong, that is an issue, not an
edit here.

## Commands, in commit order

The root command first, because its persistent flags appear in every generated
page, so its commit regenerates all of `docs/commands/`. Then alphabetical.

`root`, `attachment-download`, `attachment-list`, `attachment-upload`, `check`,
`children`, `create`, `diff`, `export`, `find`, `page-info`, `read`, `schema`,
`search`, `space-info`, `update`, `user-find`, `user-info`.

## Steps for each command

1. **Rewrite in STE100.** Short sentences, active voice, imperatives for
   instructions, no gerunds as nouns, no em dashes, approved words.
2. **Review for the user.** Read it as someone who runs the command:
   - **Clarity:** does `Long` start with what the command does, and when to use
     it?
   - **Usefulness:** does each sentence change what a user does or expects?
     Keep reasoning that does (why `--depth all` needs `--space`). Cut
     implementation detail that does not (which API route answers). CLAUDE.md
     and code comments keep that.
   - **Correctness:** check each claim against the command's code.
   - **Conciseness:** remove repetition between `Long`, `Example`, and flag
     usage.
3. **Regenerate** `docs/commands/` with `make docs`.
4. **Pass the gates** below.
5. **Commit** that command alone: `docs(help): rework <command> with STE100`.

## Quality gates, for every commit

| gate | check |
|---|---|
| G1 content | Every flag, value, exit code, path, and command name in the old text is still there, or its removal is deliberate and the commit message says why. |
| G2 correctness | Every claim agrees with the code: flag names and defaults as registered, values accepted, behavior described. |
| G3 STE100 | No sentence over 25 words (20 in instructions), no word from the do-not-use list, no em dashes, no slash outside flag and path syntax. Checked by script. |
| G4 tests | `TestSubcommandsDocumentThemselves` (a `Long` of 120+ characters and an `Example`) and `TestHelpMentionsWhatSearchCannotFind` still pass. |
| G5 build | `make check` passes: vet, fmt-check, docs-check, test, build, lint (`lll` at 120). |
| G6 scope | The commit touches only that command's Go file and its `docs/commands/` page, except the root commit, which regenerates every page. |

## After all commands

1. **Code review** of the whole branch against `main`, looking for:
   - claims that disagree with the code,
   - details a user needs that the rewrite dropped or changed,
   - text that disagrees between commands (for example, two descriptions of
     `--json`).
2. **Verify** each finding against the code before fixing it.
3. **Fix** in one commit: `docs(help): fix what the review found`. Gates G2,
   G3, G4, and G5 apply.
4. **Open a PR** that fixes #165.

## Not in scope

- Changing CLAUDE.md's rule that `Long` carries the reasoning (#102). The
  review cuts reasoning that does not help a user, and does not change the
  rule.
- Help text for cobra's own `help` and `completion` commands.
