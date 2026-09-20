# Releasing markfluence

For maintainers. Releases are driven by a git tag: you push the tag, a workflow
does the rest, and one step at the end is deliberately manual.

> [!IMPORTANT]
> **Not usable yet.** `.github/workflows/release.yml` does not exist —
> `.goreleaser.yaml` is fully configured and has never been invoked. Building
> that workflow and cutting `v0.1.0` is
> [#175](https://github.com/mozilla/markfluence/issues/175). Until it lands,
> everything below describes the intended process rather than a path you can
> follow.

## Versioning

The tag must be `vX.Y.Z` — required by both **Go modules** and
**goreleaser**.

A prerelease suffix is fine for both, and is how you rehearse: `v1.2.3-rc.1`
is valid semver, goreleaser marks the release a prerelease on its own, and the
Homebrew cask is skipped for it (`skip_upload: "auto"`), so an RC can't reach
`brew install` users.

**goreleaser strips the `v`** for artifact names. Tag `v1.2.3` produces
`markfluence_1.2.3_darwin_arm64.tar.gz`.

## Where this runs

goreleaser runs in **two places, for two different jobs**, and the steps below
say which each time:

| | machine | what it does |
|---|---|---|
| the rehearsal | **your laptop** | builds everything, publishes nothing |
| the real release | **GitHub Actions** | builds, publishes, pushes the cask branch |

## Steps

1. (Laptop) **Land everything you want in the release.** `main` should be green.

2. (Laptop) **Rehearse.** On your laptop. This builds everything and publishes
   nothing, so it is safe to run at any time:

   ```sh
   goreleaser release --snapshot --clean --skip=publish
   ```

   Check `dist/`: four archives (`darwin_arm64`, `darwin_amd64`, `linux_arm64`,
   `linux_amd64`), a `checksums.txt`, and `dist/homebrew/Casks/markfluence.rb`.
   `goreleaser check` validates the config but proves nothing about what it
   produces, so do this rather than that.

   Then `rm -rf dist` — it's gitignored, but a stale `dist/` is confusing.

3. (Laptop) **Tag and push.**

   ```sh
   git tag -a v1.2.3 -m 'v1.2.3'
   git push origin v1.2.3
   ```

   The workflow triggers on `v*.*.*` — three components, deliberately, so the
   moving `v1` tag doesn't re-enter it.

4. (GHA) **Watch the run.** This is where the real goreleaser invocation happens,
   in GitHub Actions, from a clean checkout of the tag. It cross-compiles,
   archives, writes `checksums.txt`, creates the GitHub Release with its
   assets, and pushes the generated Homebrew cask to a
   `goreleaser/cask-v1.2.3` branch.

   ```sh
   gh run watch
   ```

5. (Laptop) **Create the cask PR by hand.**

   ```sh
   gh pr create --head goreleaser/cask-v1.2.3 \
     --title 'chore: bump markfluence cask to v1.2.3'
   ```

   This step is manual for a reason — see
   [Why the cask PR is manual](#why-the-cask-pr-is-manual) below.

   Merge it once `ci` is green. Until it merges, `brew install markfluence`
   still serves the previous version.

6. (Laptop) **Verify what shipped.** Download one archive and check the stamp matches
   the tag:

   ```sh
   markfluence --version
   ```

   Then, after the cask PR merges:

   ```sh
   brew update && brew upgrade markfluence
   ```

7. (Laptop) **Read the published release notes, and fix them if they're bad.**

   ```sh
   gh release view v1.2.3
   # if it reads badly:
   gh release view v1.2.3 --json body --jq .body > notes.md
   # ...edit notes.md...
   gh release edit v1.2.3 --notes-file notes.md
   ```

   There are no consequences to changing the release notes text - only
   humans read it.

## Why the cask PR is manual

This repository is its own Homebrew tap
([#1](https://github.com/mozilla/markfluence/issues/1)): the cask lives in
`Casks/` here rather than in a separate `mozilla/homebrew-markfluence`. That's
what lets the release job use its own `GITHUB_TOKEN`, since GitHub's automatic
token can't write to another repository.

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

## If a release fails partway

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
