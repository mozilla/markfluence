# 047 — Ship markfluence as a GitHub Action

Closes #29.

[docs/github-actions.md](../docs/github-actions.md) already documents the whole
CI arrangement — credentials, why `--force` is the correct configuration, the
`git diff` recipe that narrows a glob to the files that changed, and the
`concurrency` group. Every consumer has to copy all of it, and the parts that
are easy to get wrong (`--diff-filter=ACMRT`, the empty-list guard, the
all-zero `github.event.before`) are exactly the parts that fail quietly rather
than loudly. That copied block is what this replaces.

Two actions, because they answer different questions:

```yaml
# The whole recipe, for the common case.
- uses: mozilla/markfluence@v1
  with:
    files: 'docs/**/*.md'
    changed-only: true
  env:
    CONFLUENCE_URL: ${{ secrets.CONFLUENCE_URL }}
    CONFLUENCE_USERNAME: ${{ secrets.CONFLUENCE_USERNAME }}
    CONFLUENCE_TOKEN: ${{ secrets.CONFLUENCE_TOKEN }}
    CONFLUENCE_CLOUD_ID: ${{ vars.CONFLUENCE_CLOUD_ID }}

# Just the binary, for everything else.
- uses: mozilla/markfluence/setup@v1
  with: { version: v1.2.3 }
- run: markfluence diff docs/deploy-runbook.md
```

The split is what keeps the publish action from growing an input per command.
There are seventeen subcommands and only one of them belongs in a scheduled
publish; `check` in a PR gate, `diff` in a review comment and `find` before a
create are all plausible in CI and all better served by a binary on `PATH` than
by a `command:` enum that has to track the CLI.

## Depends on #175

**Nothing here can be built until a release exists.** There are no tags and no
releases; `.goreleaser.yaml` is fully configured and has never been invoked.
The composite action's entire premise is fetching
`markfluence_<version>_<os>_<arch>.tar.gz`, which does not exist yet.

#175 establishes the release process and cuts `v0.1.0`. This plan starts after
that, and the next section is what this plan *requires* of it — recorded here
because these are the constraints the action imposes on a release, and a
release process built without them is one this plan then has to change.

The four phases:

1. `setup/action.yml` — install the binary.
2. `action.yml` — the publish action.
3. An integration workflow, the only way any of this is testable.
4. The moving `v1` tag, and the Marketplace listing.

## What the probe established

### A composite action cannot `uses:` a sibling action in its own repo

The obvious composition — the publish action calling `uses: ./setup` — **does
not work**. A relative `uses:` path inside a composite action resolves against
`$GITHUB_WORKSPACE`, the *consumer's* checkout, not against the action's own
repository ([actions/runner#1348](https://github.com/actions/runner/issues/1348),
[#2101](https://github.com/actions/runner/issues/2101)). A consumer with no
`setup/` directory gets `Can't find 'action.yml'`, naming a path in their repo
that has nothing to do with them. The workarounds in circulation are copying
the action into the workspace or symlinking it, both of which put files in the
consumer's checkout.

Hardcoding `uses: mozilla/markfluence/setup@v1` is the other obvious move and
is worse: it pins the inner action to a ref the outer one may not be, so
`@v1.2.3` would silently run `v1`'s installer.

**So the two actions share a shell script, not an action.** `setup/install.sh`
holds the install logic; both `action.yml` files invoke it through
`$GITHUB_ACTION_PATH`, which points at the checkout of *this* repo:

| caller | invocation |
|---|---|
| `setup/action.yml` | `"$GITHUB_ACTION_PATH/install.sh"` |
| `action.yml` (root) | `"$GITHUB_ACTION_PATH/setup/install.sh"` |

One copy of the logic, no nesting, and the ref is whatever the consumer asked
for in both cases.

### Whether step-level `env:` reaches a composite action's steps is unresolved

#29 raises this and it is still the right thing to be suspicious of, but
neither the composite-action documentation nor
[ADR 0549](https://github.com/actions/runner/blob/main/docs/adrs/0549-composite-run-steps.md)
states it, and the runner has a cluster of adjacent bugs — `GITHUB_ENV` not
updating across repeated composite invocations
([#789](https://github.com/actions/runner/issues/789)), inputs not seeing
step-level `env`, `GITHUB_ACTION_REF` empty inside a composite action
([#2525](https://github.com/actions/runner/issues/2525)).

Rather than guess, the design **does not depend on the answer**, and the answer
gets measured. The token is never an input (see below), so it reaches the CLI
through the process environment either way; the integration workflow in phase 3
sets `CONFLUENCE_TOKEN` at step level on the `uses:` step and asserts the
publish worked, which settles it in the repo rather than in a comment. If it
turns out not to propagate, the fix is a documentation line saying job-level,
not a design change.

`GITHUB_ACTION_REF` being unreliable is why `version` does **not** default to
the action's own tag, which was the tidy answer.

## What this plan needs from #175

Five constraints, all of them things the *action* imposes on the release
rather than choices the release would make on its own: the ref/asset coupling,
the tag format, the trigger glob, the stripped `v`, and the platform set. They
are in #175's body too, and are restated here because if any of them changes,
this plan changes with it.

Start with two artifacts, from two different places:

| what | comes from |
|---|---|
| `action.yml`, `install.sh` | the **git ref** — `uses: mozilla/markfluence@v1` |
| the `markfluence` binary | a **Release asset** |

`uses:` resolves a git ref and nothing else — a tag, a branch, or a SHA — and
needs no Release at all. The Release requirement is one *this design* creates
by downloading a binary rather than building one. The consequence to hold on
to: **a tag whose release failed is a broken action.** `uses:
mozilla/markfluence@v1.2.3` would run an installer that 404s. Two things keep
that from happening: `version` defaults to `latest`, which resolves to the
last release that actually *succeeded* rather than to whatever tag exists, and
the moving `v1` tag (phase 4) moves only after assets upload. The second is a
step this plan adds to #175's `release.yml`, so #175 should leave room for it
rather than be surprised by it.

### The tag has to be `vX.Y.Z`

Git does not care and neither does `uses:`, but two things in this repo do:

- **Go modules.** `github.com/mozilla/markfluence` is a module, and
  `docs/github-actions.md` tells people to `go install …@latest`. The proxy
  only recognises semver tags **with the `v` prefix**; a `1.2.3` or
  `release-1` tag is invisible to it, so `@v1.2.3` does not resolve and
  `@latest` degrades to a `v0.0.0-2026…` pseudo-version.
- **goreleaser**, which parses the tag and fails with `not a valid semantic
  version` otherwise.

A prerelease suffix is fine for all of them: `v0.1.0-rc.1` is valid semver,
valid for Go, and goreleaser marks the release a prerelease on its own.

**And the release trigger must be `tags: ['v*.*.*']`, not `['v*']`** — the
obvious glob also matches the moving `v1` tag this plan adds, so every release
would push `v1`, re-enter the workflow, and fail the second run on a tag
goreleaser rejects as non-semver. Actions tag filters are globs rather than
regex, so `v*.*.*` is the available way to say "three components". This is the
one requirement #175 has to get right *before* `v0.1.0`, since the rest can be
fixed afterwards and a workflow that re-triggers itself cannot.

**The `v` is stripped in asset names.** goreleaser's `name_template` uses
`.Version`, so tag `v1.2.3` produces `markfluence_1.2.3_linux_amd64.tar.gz`.
`install.sh` has to strip it, in the same place it maps `$RUNNER_OS`/`$RUNNER_ARCH`.

### Which runners the release matrix covers

An action can only run where an asset exists, so the build matrix becomes a
support matrix the moment this ships. goreleaser builds darwin+linux ×
arm64+amd64 with `darwin/amd64` in `ignore`, so **three** assets exist.
Against GitHub-hosted runners:

| runner | arch | asset |
|---|---|---|
| `ubuntu-latest` | linux/amd64 | ✅ |
| `ubuntu-24.04-arm` | linux/arm64 | ✅ |
| `macos-latest` (14/15) | darwin/arm64 | ✅ |
| `macos-13` | darwin/amd64 | ❌ in `ignore` |
| `windows-latest` | windows/amd64 | ❌ not built |

**Decision: the matrix does not change.** The three supported runners are
`ubuntu-latest`, `ubuntu-24.04-arm` and `macos-latest`; `macos-13` and Windows
are added later if anyone needs them. Neither gap is free — Windows means
`.zip` archives, a `.exe` suffix and a shell that is not bash unless every
composite step says `shell: bash`, and `darwin/amd64` is a fourth build for a
runner image on its way out.

What that requires of *this* plan is that both gaps are **named errors** in
`install.sh`, not 404s. An action that does not support Windows is a scope
decision; one that fails on it confusingly is a bug report. The message names
the platforms that exist and points at this issue, so "we can add it later"
has somewhere to be asked for.

## Phase 1 — `setup/action.yml`

Inputs: `version` (default `latest`).

`install.sh`:

1. Resolve `latest` **without the API**: `curl -sIL -o /dev/null -w '%{url_effective}'`
   on `/releases/latest` and read the tag out of the redirect. The GitHub API
   would need a token to avoid a shared unauthenticated rate limit, and a
   `setup` action that demands a token to install a binary is a bad trade. An
   explicit `version` skips this entirely, which is what a consumer should pin.
2. Map `$RUNNER_OS`/`$RUNNER_ARCH` to goreleaser's `name_template` — `Linux`→
   `linux`, `macOS`→`darwin`, `X64`→`amd64` — and **strip the leading `v`**
   from the tag, since `.Version` does. **The two uncovered platforms must fail
   by name rather than on a 404**: Windows and Intel macOS, per the matrix
   table above.
3. Download the archive **and `checksums.txt`**, verify with `sha256sum -c`,
   and fail on mismatch. This is the "verifies it" checkbox in #29 and it is
   the only integrity check there is until the release emits provenance.
4. Extract to `$RUNNER_TEMP/markfluence`, `chmod +x`, append to `$GITHUB_PATH`.
5. `markfluence --version` as a smoke test, so a bad asset fails in the setup
   step rather than three steps later inside a publish.

Output: `version` (the resolved tag), because a workflow that pinned `latest`
should be able to say in its log what it actually ran.

No `actions/cache`. The archive is small, the download is one request, and a
cache key would be one more thing to invalidate.

## Phase 2 — `action.yml` (publish)

Inputs:

| input | default | why |
|---|---|---|
| `files` | `docs/**/*.md` | the glob, before narrowing |
| `changed-only` | `true` | the `git diff` recipe from the docs |
| `since` | `''` | explicit base ref, overriding `github.event.before` |
| `dry-run` | `false` | `--dry-run`, for a PR preview |
| `debug` | `false` | `--debug` |
| `version` | `latest` | passed to the installer |

Steps: install (the shared script), narrow the file list, publish, emit
outputs.

**`--force` is always on and is not an input.** The action *is* the CI
arrangement, and the arrangement is the one where the repository is the source
of truth. An action that can be configured into the other combination is a
trap: without `--force`, whether a page publishes depends on a divergence check
against an action log that a fresh checkout does not have, so pages get
reported `skipped` for a reason nobody in CI can act on.

**`changed-only` is load-bearing, and more so than #29's second comment
predicted.** That comment expected #149 to leave the idempotence check standing
under `--force`, so that `update --force` over a whole glob would publish only
what actually differs. **That is not what shipped**: `--force` now means always
`PUT`, and nothing may suppress the request — it overrides both the moved-page
refusal *and* the unchanged-body skip. So a glob really does republish
everything, every watcher really is emailed, and narrowing is the only thing
standing between one typo fix and two hundred notifications.

The narrowing step is the documented recipe verbatim, which is the point of
moving it here: `--diff-filter=ACMRT`, the empty-list guard that skips rather
than invoking `update` with no arguments, and the all-zero `github.event.before`
falling back to publishing everything rather than failing on an unknown ref.
`fetch-depth: 0` **cannot** be set by the action and stays a documented
requirement on the consumer's `checkout` step; the action should detect a
shallow clone (`git rev-parse --is-shallow-repository`) and say so by name,
since the failure is otherwise an unhelpful `bad object`.

Outputs, from `--json` rather than scraped text: `published`, `skipped`,
`failed`, and `results-json` (the path to the envelope, not its contents — a
multi-line JSON document in an output is a quoting hazard and the file is right
there). `--json` suppresses human output entirely, so the step renders a short
summary to the log from the same file and writes it to `$GITHUB_STEP_SUMMARY`.

### Security shape of both actions

The repo is under Mozilla's SSDLC zizmor gate and `ci.yml` already pins every
action by commit SHA. Two rules the new files have to follow:

- **No `${{ inputs.* }}` interpolated into a `run:` block.** That is template
  injection — `files` reaches a shell — and it is the finding zizmor exists to
  catch. Every input crosses into the script through `env:`.
- `set -euo pipefail` in every script, and the download path quoted throughout.

## Phase 3 — proving it works

A workflow in this repo that uses both actions against the live instance, on
`workflow_dispatch` rather than on every push, publishing to a fixture page in
the personal space. It is the only way to test a composite action, since
nothing about it is exercisable by `go test`, and it is what settles the
step-level `env` question above.

## Phase 4 — the moving `v1` tag, and the Marketplace

A `v1` tag that `release.yml` re-points on each `v1.x.y`, since `uses:
mozilla/markfluence@v1` is the convention consumers expect and goreleaser's
tags are exact. **It moves only after the assets upload**, for the reason in
the coupling section: a `v1` pointing at a tag whose release failed is an
action that cannot install anything. It is also why the trigger glob is
`v*.*.*` — `v1` must not re-enter the workflow.

Marketplace, verified against the docs rather than assumed:

- **The repository must be public** (it is) and hold **a single `action.yml`
  at the root**. Sub-folder actions work fine via `uses:` but are never
  listed — so `setup/` is documented rather than discoverable, and the
  listing is the publish action. That is an accepted cost of the two-action
  shape, not a problem to solve.
- **The `name:` must be globally unique** across Marketplace actions, and must
  not collide with a GitHub user/org or a Marketplace category. `markfluence`
  needs checking before the listing, not after.
- **Publishing is a checkbox on a release page and needs 2FA**, so the first
  publish is a manual UI step; goreleaser cannot tick it.
- `branding:` (icon + colour) is **not** a documented requirement, but the
  listing looks unfinished without it, so both `action.yml` files get one.

Optional and deliberately not phase 4: nothing here requires 1.0.0, and the
Marketplace listing can wait until the action has been used in anger once.

## Files

| file | change |
|---|---|
| `.github/workflows/release.yml` | **#175 creates it**; this plan adds the move-`v1` step |
| `setup/action.yml`, `setup/install.sh` | new — the thin action |
| `action.yml` | new — the publish action, at the repo root |
| `.github/workflows/action-integration.yml` | new — phase 3 |
| `docs/github-actions.md` | lead with the action; keep the hand-rolled recipe below it as what the action does |
| `docs/releasing.md` | **#175 creates it**; this plan adds the `v1` tag and the Marketplace step |
| `README.md` | a short section on the action, under Usage |
| `CLAUDE.md` | `action.yml`/`setup/` are new top-level artifacts, and the layout section says what lives outside `cmd`/`internal`/`schema` |

## Tests

There is no Go surface here, so "tests" means the integration workflow plus
things a reviewer can check by reading:

- The integration workflow exercises `setup` with a pinned `version` and with
  `latest`, and the publish action with `changed-only: true` and `false`.
- A `dry-run: true` run changes nothing on the fixture page (its version number
  is unchanged afterwards).
- An empty changed-file list skips the publish step and exits 0 rather than
  invoking `update` with no arguments.
- A checksum mismatch fails the setup step — forced by pointing the script at a
  doctored `checksums.txt` in a test fixture.
- Windows and Intel-macOS runners fail with the named message, not a 404.
- The three supported runners are each exercised by the integration matrix,
  since a mapping bug is per-platform and `ubuntu-latest` alone would not
  catch `macOS`→`darwin` or `ARM64`→`arm64`.
- `v1.2.3` finds `markfluence_1.2.3_…` — the `v`-stripping, which is the kind
  of thing that works on the tag it was written against and nowhere else.
- `make docs-check` still passes: nothing here touches `docs/commands/`, and a
  reviewer should confirm that rather than assume it.

## Not in scope

- **`create` in CI.** Creating a page has to commit a `page_id` back to the
  repository; [docs/github-actions.md](../docs/github-actions.md) already says
  creating stays a human act, and an action that writes to the repo is a
  different and much larger conversation.
- **Deleting pages.** `--diff-filter=ACMRT` excludes deletions deliberately.
- **A Docker container action.** Linux-only and a slower cold start, for no
  benefit over a static binary.
- **`update --changed-since REF` in the CLI.** Rejected in #29's second comment
  and still right: git knowledge in the tool is scope creep, and CI glue is
  what a composite action is for.
- **Reading, writing or caching the action log.** #149's log is per-checkout
  and deliberately not committed; a shared repository is by definition the
  arrangement that does not need it. Caching it between runs would be the
  obvious wrong optimisation.
- **Release provenance/attestation.** Worth having, and a separate change to
  the release pipeline rather than to the action. `checksums.txt` is the
  integrity check until then.
- **Windows and Intel-macOS runners.** Decided above: added later if anyone
  needs them, named errors until then.
- **The release pipeline itself.** #175: `release.yml`, `docs/releasing.md`,
  the `README.md`/`SECURITY.md` staleness, and `v0.1.0`. This plan states what
  it needs from that and builds on top.
- **Homebrew.** #1, settled: the repo is its own tap, so the cask needs no
  separate repository and no token beyond the release job's own. Nothing about
  the action touches it.
- **1.0.0.** `v0.1.0` is all the action needs. The 1.0.0 milestone holds this
  issue, not the other way round.

## Open questions

- **`timeout-minutes`.** A rate-limited request can legitimately take minutes —
  worst case roughly twelve on a single attachment (#81) — and a job that looks
  hung gets cancelled, which for `create` leaves pages behind. The action cannot
  set a job timeout, so this is either a documented recommendation or nothing.
  Documented, unless there is a better idea.
- **Whether `debug` should default to `true`.** A slow run is otherwise
  indistinguishable from a stuck one, and `--debug` prints the `X-RateLimit-*`
  values that are the actual diagnosis. Against: it is a lot of log for the
  common case.
- **Whether the publish action should run `check` first.** It would turn a
  converter-rejected file into a fast local failure instead of a partial
  publish. Against: `update` already validates, and this is a second opinion
  that can disagree.
