# Plan: GitHub Actions CI workflow

Add `.github/workflows/ci.yml` running the build/test/lint gate on pushes to
`main` and on pull requests. Implements issue #4. Continuous integration only —
the goreleaser release workflow (CD) is deliberately out of scope.

## Decisions locked (from the interview)

### Drive everything through `make`

Per issue #4, CI calls the existing `make` targets so local and CI run the
identical commands and tool versions (notably the pinned golangci-lint v2.6.0 the
`lint` target installs). No duplicated tool versions in the workflow.

- `make vet` — `go vet ./...`
- `make fmt-check` — **new** non-mutating format gate (see below)
- `make test` — `go test ./...` (also exercises the converter golden suite, which
  fails on stale goldens, so no separate goldens check is needed)
- `make build` — builds the binary; transitively compiles every `internal/*`
  package, so it covers `go build ./...` for reachable code
- `make lint` — golangci-lint v2.6.0 (its default set includes govet)

### `make fmt-check` (new Makefile target)

`make fmt` runs `go fmt ./...`, which *rewrites* files and can't gate CI. Added a
sibling `fmt-check` that lists misformatted files and fails if any:

```make
fmt-check:  ## Check formatting without modifying files (fails if any file needs gofmt)
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then \
	  echo "These files are not gofmt'd:"; echo "$$out"; exit 1; fi
```

Registered in `.PHONY`. Devs and CI run the same check.

### Single job, sequential steps

One `ubuntu-latest` job. ubuntu-only (the code is cross-platform and release
builds are cross-compiled; no macOS runner). Go version pinned via
`setup-go`'s `go-version-file: go.mod` (currently 1.25). Module/build caching via
`setup-go`'s built-in `cache: true` (also speeds the golangci-lint reinstall in
`make lint`).

Step order: checkout → setup-go → vet → fmt-check → test → build → lint.

### Triggers — one run per change

- `push` restricted to `main` (direct pushes and post-merge).
- `pull_request` for anything under review.

This avoids the double run that `push` (all branches) + `pull_request` would
produce on PR branches.

### Hardening

- `permissions: contents: read` — minimal token scope.
- `concurrency` group by `workflow + ref` with `cancel-in-progress: true` — a new
  push cancels the superseded run.
- **Actions SHA-pinned** to a full commit with the semver in a trailing comment
  (Dependabot-compatible), not just a mutable tag:
  - `actions/checkout@11d5960a326750d5838078e36cf38b85af677262 # v4.4.0`
  - `actions/setup-go@40f1582b2485089dde7abd97c1529aa768e1baff # v5.6.0`

## Out of scope (follow-up)

- `release.yml` wiring goreleaser on tag push (needs a tap token / the
  `homebrew-markfluence` repo to exist).
