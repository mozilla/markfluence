# Releasing markfluence

**For maintainers.**

Summary: Releases are driven by a git tag: you push the tag, a workflow does
the rest, and one step at the end is deliberately manual.

## Versioning

The tag must be `vX.Y.Z` — required by both **Go modules** and
**goreleaser**.

**This project does not cut prereleases.** Step 2 below is the rehearsal, and
it is a local build that publishes nothing, which is a better test than a real
tag anybody can see.

The config handles an RC correctly anyway, as insurance rather than as a
practice: `v1.2.3-rc.1` is valid semver, `release.prerelease: auto` keeps it
out of `/releases/latest`, and `skip_upload: "auto"` keeps the Homebrew cask
from bumping. Neither path has ever run.

> [!NOTE]
> **goreleaser strips the `v`** for artifact names. Tag `v1.2.3` produces
> `markfluence_1.2.3_darwin_arm64.tar.gz`.

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

   Check `dist/`: three archives (`darwin_arm64`, `linux_arm64`,
   `linux_amd64` — there is no Intel macOS build), a `checksums.txt`, and
   `dist/homebrew/Casks/markfluence.rb`.
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

4. (GHA) **Watch the run.** GitHub Actions runs goreleaser from a clean
   checkout of the tag. It runs `make check` against the tagged commit,
   cross-compiles, archives, writes `checksums.txt`, creates the GitHub Release
   **as a draft** and uploads the assets into it, pushes the generated Homebrew
   cask to a `goreleaser/cask-v1.2.3` branch, and only then publishes the
   draft.

   ```sh
   gh run watch
   ```

   > [!NOTE]
   > goreleaser creates a release before it uploads anything, and
   > `/releases/latest` is the most recent *published* non-prerelease whether or
   > not its assets are complete. Creating a draft and then publishing it as the
   > last step means a run that dies partway is never the release `latest`
   > resolves to.

5. (Laptop) **Create the cask PR by hand.**

   ```sh
   gh pr create --head goreleaser/cask-v1.2.3 \
     --title 'chore: bump markfluence cask to v1.2.3'
   ```

   See [Why the cask PR is manual](#why-the-cask-pr-is-manual) below.

   Merge the PR once `ci` is green. Until it merges, `brew install markfluence`
   still serves the previous version.

6. (Laptop) **Verify what shipped.** Download a real archive and run the binary
   out of it. Run this from the root of the markfluence git repository
   (macOS-centric):

   ```sh
   # create a temp dir, download the release, untar it, check the version
   mkdir tmp
   pushd tmp
   gh release download v1.2.3 -p 'markfluence_*_darwin_arm64.tar.gz' -p checksums.txt &&
   shasum -a 256 --check --ignore-missing checksums.txt &&
   tar -xzf markfluence_*_darwin_arm64.tar.gz markfluence &&
   ./markfluence --version

   # --- verify the version ---

   # clean up
   popd
   rm -rf tmp
   ```

   It must print the tag. **Not `markfluence --version`** — that runs whatever
   is on your `PATH`, which for a maintainer is the `make install` build
   stamped `dev`, so it would pass no matter what shipped.

   Then, after the cask PR merges:

   ```sh
   brew update && brew upgrade markfluence
   markfluence --version
   ```

7. (Laptop) **Read the published release notes, and fix them if they're bad.**

   ```sh
   gh release view v1.2.3
   # if it reads badly:
   gh release view v1.2.3 --json body --jq .body > notes.md
   # ...edit notes.md...
   gh release edit v1.2.3 --notes-file notes.md
   ```

   GitHub Releases supports full GFM, but newlines are line breaks. Don't
   wrap paragraphs.

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

You get a **draft** release, not a broken published one — that is what
`release.draft` plus the publish-last step in the workflow buy. Nobody
downstream sees it, and `latest` still points at the previous release.

Recovery is to delete the draft *and* the tag, fix the problem, and re-tag.
The tag has to go too, or re-pushing it does nothing and the workflow will
not re-run:

```sh
gh release delete v1.2.3 --yes
git push --delete origin goreleaser/cask-v1.2.3   # if the run got that far
git push --delete origin v1.2.3
git tag -d v1.2.3
```

The cask branch matters: goreleaser pushes it *after* creating the release, so
a run that died at the publish step has already left one. Re-tagging the same
version commits a second cask on top of the stale one, and the PR you
eventually open carries both.

Pre-1.0 this is cheap. Once anything depends on a tag, prefer cutting
`v1.2.4` over reusing a tag someone may already have fetched.
