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
not share them — but say which Confluence flavor you're on (Cloud or Data
Center), since the API differs.

## Development setup

Requires Go 1.25+.

```sh
git clone https://github.com/mozilla/markfluence
cd markfluence
make build     # produces ./bin/markfluence
make test
```

Run `make` with no target for the annotated list of rules.

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
