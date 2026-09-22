# Release markfluence

**For maintainers.**

Summary: a git tag starts a release. You push the tag, and a workflow does the
other work. One step at the end is manual, on purpose.

## Versioning

The tag must be `vX.Y.Z`. **Go modules** and **goreleaser** both need this
form.

**This project does not make prereleases.** Step 2 below is the rehearsal. It
is a local build that publishes nothing. That is a better test than a real tag
that anybody can see.

The configuration handles a release candidate (RC) correctly anyway, as
insurance and not as a practice. `v1.2.3-rc.1` is valid semver.
`release.prerelease: auto` keeps it out of `/releases/latest`, and
`skip_upload: "auto"` stops the push of a new Homebrew cask. Neither path has
ever run.

> [!NOTE]
> **goreleaser removes the `v`** from artifact names. The tag `v1.2.3` makes
> `markfluence_1.2.3_darwin_arm64.tar.gz`.

## Where this runs

goreleaser runs in **two places, for two different jobs**. Each step below
tells you which place:

| | machine | what it does |
|---|---|---|
| the rehearsal | **your laptop** | builds everything, publishes nothing |
| the real release | **GitHub Actions** | builds, uploads to a draft release, pushes the cask branch. A separate workflow step then publishes the draft |

## Steps

1. (Laptop) **Merge everything that you want in the release.** `main` must be
   green.

2. (Laptop) **Do the rehearsal** on your laptop. It builds everything and
   publishes nothing. But its hooks run `go mod tidy`, which can change
   `go.mod` and `go.sum` in your checkout, and `make completions`, which writes
   `completions/`:

   ```sh
   goreleaser release --snapshot --clean --skip=publish
   ```

   Look in `dist/`. It must have 3 archives (`darwin_arm64`, `linux_arm64`, and
   `linux_amd64`), a `checksums.txt`, and `dist/homebrew/Casks/markfluence.rb`.
   There is no Intel macOS build.

   `goreleaser check` does a check of the configuration, but it proves nothing
   about what the configuration makes. Thus do the rehearsal, and not that
   check.

   Then run `rm -rf dist completions`. git ignores both, but stale copies are
   confusing. Run `git status` too. If `go mod tidy` changed `go.mod` or
   `go.sum`, the tree was not tidy: commit that change in a normal pull
   request, and do not release until it merges.

3. (Laptop) **Make the tag and push it.**

   ```sh
   git tag -a v1.2.3 -m 'v1.2.3'
   git push origin v1.2.3
   ```

   The workflow starts on `v*.*.*`. The 3 components are deliberate, because
   goreleaser refuses anything that is not semver, and `v1` and `v1.2` are not
   semver. A bare `v*` would start a run that could only fail.

4. (GHA) **Watch the run.** The release workflow runs from a clean checkout
   of the tag. It does these steps, in this sequence:

   1. The workflow runs `make check` on the tagged commit.
   2. goreleaser cross-compiles, makes the archives, and writes
      `checksums.txt`.
   3. goreleaser creates the GitHub Release **as a draft** and uploads the
      assets into it.
   4. goreleaser pushes the generated Homebrew cask to a
      `goreleaser/cask-v1.2.3` branch.
   5. A separate workflow step publishes the draft
      (`gh release edit --draft=false`), and only at this point.

   ```sh
   gh run watch
   ```

   > [!NOTE]
   > goreleaser creates a release before it uploads anything.
   > `/releases/latest` is the most recent *published* release that is not a
   > prerelease, and it does not look at whether the assets are complete. The
   > workflow creates a draft and publishes it as the last step. Thus a run
   > that stops partway is never the release that `latest` points to.

5. (Laptop) **Create the cask PR by hand.**

   ```sh
   gh pr create --head goreleaser/cask-v1.2.3 \
     --title 'chore: bump markfluence cask to v1.2.3'
   ```

   See [Why the cask PR is manual](#why-the-cask-pr-is-manual) below.

   Merge the PR when `ci` is green. Until it merges, `brew install markfluence`
   still gives the earlier version.

6. (Laptop) **Make sure that the correct thing shipped.** Download a real
   archive and run the binary from it. Run this from the root of the
   markfluence git repository. These commands are for macOS:

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

   It must print the version of the tag with no leading `v`, for example
   `markfluence 1.2.3 (...)`. Run `./markfluence`, and not `markfluence`. The
   plain `markfluence` runs the binary on your `PATH`. For a maintainer, that
   is usually the `make install` build with the stamp `dev`, so it does not
   test the release at all.

   Then, after the cask PR merges, do a check of the Homebrew binary by its full
   path, for the same reason:

   ```sh
   brew update && brew upgrade markfluence
   "$(brew --prefix)/bin/markfluence" --version
   ```

7. (Laptop) **Read the published release notes, and correct them if they are
   bad.**

   ```sh
   gh release view v1.2.3
   # if it reads badly:
   gh release view v1.2.3 --json body --jq .body > notes.md
   # ...edit notes.md...
   gh release edit v1.2.3 --notes-file notes.md
   ```

   GitHub Releases supports full GFM, but a newline is a line break. Do not
   wrap paragraphs.

   A change to the text of the release notes has no effect on anything. Only
   persons read it.

## Why the cask PR is manual

This repository is its own Homebrew tap
([#1](https://github.com/mozilla/markfluence/issues/1)). The cask is in
`Casks/` in this repository, and not in a separate
`mozilla/homebrew-markfluence`. Thus the release job can use its own
`GITHUB_TOKEN`. The automatic token of GitHub cannot write to a different
repository.

But the release cannot complete the job, because of two rules:

- The ruleset of `main` needs the **`ci`** status check. It has no bypass
  actors and `current_user_can_bypass: never`. Nobody can force a merge. This
  includes repository admins.
- GitHub **does not start workflow runs for events from the automatic
  `GITHUB_TOKEN`**. A PR that goreleaser opened would never run `ci`. Thus it
  could never pass the check.

Thus a cask PR that the release opened would show "waiting for status to be
reported" with no end. The same ruleset refuses a direct push to `main`.

We measured this, and we did not assume it. `mozilla/mozcloud` opens its
formula PRs in this way, and they *do* merge. But its ruleset needs no status
check, and ours does.

The ruleset protects only the default branch, so the release job can push
`goreleaser/cask-<tag>`. Thus the release pushes the branch, you open the PR,
and `ci` runs because a person started it.

**Do not "fix" this** with `pull_request: {enabled: true}` in
`.goreleaser.yaml`. That makes a pull request that looks correct and can never
merge.

## If a release fails partway

If the run fails after goreleaser creates the release, you get a **draft**
release, and not a broken published release. That is the result of
`release.draft` and the publish step at the end of the workflow. Nobody
downstream sees the draft, and `latest` still points to the earlier release.

If the run fails earlier, for example in `make check` or in the build, there is
no draft. There is only the tag.

If only the publish step failed, the draft is complete. Run
`gh release edit v1.2.3 --draft=false` again, and you do not need to do
anything else.

Otherwise, to recover, delete the draft, the cask branch, and the tag. Then
correct the problem and make the tag again. You must delete the tag, because a
new push of a tag that exists does nothing, and the workflow does not run
again. Delete only what the run made:

```sh
gh release delete v1.2.3 --yes
git push --delete origin goreleaser/cask-v1.2.3   # if the run got that far
git push --delete origin v1.2.3
git tag -d v1.2.3
```

The cask branch is important. goreleaser pushes it *after* it creates the
release. Thus a run that stopped at the publish step already left one. If you
make the same tag again, goreleaser commits a second cask on top of the stale
one. The PR that you open later has both.

Before 1.0, this costs little. After anything depends on a tag, make `v1.2.4`,
and do not use a tag again that somebody possibly already fetched.
