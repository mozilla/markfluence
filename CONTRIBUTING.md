# Contributing to markfluence

Thanks for your interest in markfluence. Bug reports, feature requests, and
pull requests are all welcome.

## Code of conduct

This project is governed by Mozilla's
[Community Participation Guidelines](CODE_OF_CONDUCT.md). By participating, you
agree to abide by them.

## Reporting bugs and requesting features

File an issue at
[github.com/mozilla/markfluence/issues](https://github.com/mozilla/markfluence/issues).
For a bug, include the command you ran, what you expected, what happened, and
the output of `markfluence --version`. Re-running with `--debug` often shows the
request that failed. Redact your site URL, username, and token if you'd rather
not share them.

## Development setup

Requires Go 1.25+.

```sh
git clone https://github.com/mozilla/markfluence
cd markfluence
make build     # produces ./bin/markfluence
make test
```

Run `make` with no target for the annotated list of rules. `make check` is the
one to remember — see below.

To exercise the binary against a real Confluence site, put a `.env` in the
working directory — see [`.env.example`](.env.example) and the
[Configure](README.md#configure) section of the README.

## Before you open a pull request

Run:

```sh
make check
```

That's vet, fmt-check, test, build, and lint, in CI's order. CI runs this exact
target and nothing else, so the two can't drift. Don't substitute the individual
pieces: `fmt-check` is the one that gets forgotten, and `golangci-lint` doesn't
enable `gofmt`, so `make lint` passes on a file CI rejects.

The converter's behavior is pinned by a golden-file regression suite under
`internal/convert/testdata/regression/`, one directory per case. If a change
intentionally alters converter output, regenerate the goldens with
`make regen-regressions` and review the diff — it's the record of what your
change actually did.

`--json` output is locked to the schema in
[`schema/json-output/v1.json`](schema/json-output/v1.json). Changing a command's
result fields means updating the schema too, or the conformance tests fail.

## Commit messages

Commits follow [Conventional Commits 1.0.0](https://www.conventionalcommits.org/en/v1.0.0/):
`type(optional-scope): description`. For example:

```
feat(convert): rewrite anchor links
fix(client): handle 404 on missing page
docs(confluence): record what <ac:link> looks like
```

Common types: `feat`, `fix`, `docs`, `refactor`, `test`, `chore`, `build`. The
scope is usually the package. Fine-grained commits that each do one thing are
preferred over one large squashed change.

## Changing anything that talks to Confluence

Read [docs/confluence/](docs/confluence/) first. Atlassian documents little of
the storage format and describes parts of the REST API loosely enough that
markfluence's behavior rests on things established by experiment; that directory
is where those findings live. It also lists the traps that have already produced
confident, wrong conclusions.

If you establish something new about Confluence's behavior, write it down there
with how you verified it. A claim the next person can't reproduce from the note
is a claim they have to establish from scratch.

## Architecture

[CLAUDE.md](CLAUDE.md) is the working map of the codebase: what each package
owns and why it's shaped that way. It's written for coding agents, but it's the
most complete orientation available for humans too.

## License

markfluence is licensed under the [Mozilla Public License 2.0](LICENSE).
Contributions are accepted under the same license.

## Cutting a release

For maintainers. Releases are driven by a git tag: you push the tag, a workflow
does the rest, and one step at the end is deliberately manual.

> [!IMPORTANT]
> **Not usable yet.** `.github/workflows/release.yml` does not exist —
> `.goreleaser.yaml` is fully configured and has never been invoked. Building
> that workflow and cutting `v0.1.0` is
> [#175](https://github.com/mozilla/markfluence/issues/175). Until it lands,
> everything below describes the intended process rather than a path you can
> follow.

### The tag must be `vX.Y.Z`

Not a convention — two things enforce it:

- **Go modules.** The proxy only recognises semver tags carrying the `v`
  prefix. A `1.2.3` or `release-1` tag is invisible to it, so
  `go install github.com/mozilla/markfluence@v1.2.3` won't resolve and
  `@latest` silently degrades to a `v0.0.0-<date>-<sha>` pseudo-version.
- **goreleaser**, which parses the tag and fails outright on anything that
  isn't semver.

A prerelease suffix is fine for both, and is how you rehearse: `v1.2.3-rc.1`
is valid semver, goreleaser marks the release a prerelease on its own, and the
Homebrew cask is skipped for it (`skip_upload: "auto"`), so an RC can't reach
`brew install` users.

One consequence worth knowing before you read a URL: **goreleaser strips the
`v`** for artifact names. Tag `v1.2.3` produces
`markfluence_1.2.3_darwin_arm64.tar.gz`.

### Steps

1. **Land everything you want in the release.** `main` should be green.

2. **Check the release notes will read well.** They're generated from commit
   subjects (`changelog: {use: github}` — there is no `CHANGELOG.md`), so the
   Conventional Commits discipline in [Commit messages](#commit-messages) is
   what makes them legible. Skim `git log --oneline <last-tag>..main`.

3. **Rehearse locally.** This builds everything and publishes nothing:

   ```sh
   goreleaser release --snapshot --clean --skip=publish
   ```

   Check `dist/`: four archives (`darwin_arm64`, `darwin_amd64`, `linux_arm64`,
   `linux_amd64`), a `checksums.txt`, and `dist/homebrew/Casks/markfluence.rb`.
   `goreleaser check` validates the config but proves nothing about what it
   produces, so do this rather than that.

4. **Tag and push.**

   ```sh
   git tag -a v1.2.3 -m 'v1.2.3'
   git push origin v1.2.3
   ```

   The workflow triggers on `v*.*.*` — three components, deliberately, so the
   moving `v1` tag doesn't re-enter it.

5. **Watch the run.** It cross-compiles, archives, writes `checksums.txt`,
   creates the GitHub Release with its assets, and pushes the generated
   Homebrew cask to a `goreleaser/cask-v1.2.3` branch.

6. **Open the cask PR by hand.**

   ```sh
   gh pr create --head goreleaser/cask-v1.2.3 \
     --title 'chore: bump markfluence cask to v1.2.3'
   ```

   This step is manual for a reason, and the reason is not caution — see
   [Why the cask PR is manual](#why-the-cask-pr-is-manual) below. Merge it
   once `ci` is green. Until it merges, `brew install markfluence` still
   serves the previous version.

7. **Verify what shipped.** Download one archive and check the stamp matches
   the tag:

   ```sh
   markfluence --version
   ```

   Then, after the cask PR merges:

   ```sh
   brew update && brew upgrade markfluence
   ```

### Why the cask PR is manual

This repository is its own Homebrew tap
([#1](https://github.com/mozilla/markfluence/issues/1)): the cask lives in
`Casks/` here rather than in a separate `mozilla/homebrew-markfluence`. That's what lets the release job use its own
`GITHUB_TOKEN`, since GitHub's automatic token can't write to another
repository.

It also means the release can't finish the job, because two things collide:

- `main`'s ruleset requires the **`ci`** status check, with no bypass actors
  and `current_user_can_bypass: never`. Nobody can force a merge, repo admins
  included.
- GitHub **doesn't start workflow runs for events triggered by the automatic
  `GITHUB_TOKEN`**. A PR goreleaser opened would never run `ci`, so it could
  never satisfy the check.

A cask PR opened by the release would therefore sit at "waiting for status to
be reported" forever, and a direct push to `main` is refused by the same
ruleset. (This is measured, not assumed: `mozilla/mozcloud` does open its
formula PRs this way and they *do* merge — because its ruleset requires no
status check. Ours does.)

The ruleset only protects the default branch, so pushing
`goreleaser/cask-<tag>` is fine. Hence: the release pushes the branch, you open
the PR, and `ci` runs because a human triggered it.

**Don't "fix" this** by setting `pull_request: {enabled: true}` in
`.goreleaser.yaml`. It produces a pull request that looks correct and can never
land.

### If a release fails partway

The order matters: goreleaser creates the GitHub Release **before** uploading
assets. So a run that dies mid-upload leaves a published release whose archives
404 — and `/releases/latest` will happily point at it, since that endpoint
doesn't care whether assets are complete.

Recovery is to delete the release *and* the tag, fix the problem, and re-tag:

```sh
gh release delete v1.2.3 --yes
git push --delete origin v1.2.3
git tag -d v1.2.3
```

Pre-1.0 this is cheap. Once anything depends on a tag, prefer cutting
`v1.2.4` over reusing a tag someone may already have fetched.
