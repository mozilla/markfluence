# Contributing to markfluence

Thank you for your interest in markfluence. Bug reports, feature requests, and
pull requests are all welcome.

## Code of conduct

Mozilla's [Community Participation Guidelines](CODE_OF_CONDUCT.md) apply to
this project. When you participate, you agree to obey them.

## Report a bug or request a feature

File an issue at
[github.com/mozilla/markfluence/issues](https://github.com/mozilla/markfluence/issues).
For a bug, give the command that you ran, what you expected, and what occurred.
Also give the output of `markfluence --version`. If you run the command again
with `--debug`, the output often shows the request that failed. Remove your
site URL, username, and token from the output if you do not want to share them.

## Development setup

You need Go 1.25 or a later version.

```sh
git clone https://github.com/mozilla/markfluence
cd markfluence
make build     # makes ./bin/markfluence
make test
```

Run `make` with no target to see the list of rules and their descriptions.
Remember `make check`. See the next section.

To use the binary with a real Confluence site, put a `.env` file in the working
directory. See [`.env.example`](.env.example) and the
[Configure](README.md#configure) section of the README.

## Before you open a pull request

Run:

```sh
make check
```

This target runs vet, fmt-check, docs-check, test, build, and lint, in the same
sequence as CI. CI runs this target and nothing else, so the two cannot become
different. Do not run the individual parts instead. Persons often forget
`fmt-check`. Also, `golangci-lint` does not enable `gofmt`. Thus `make lint`
can pass on a file that CI refuses.

A regression suite of golden files pins the behavior of the converter. The
suite is in `internal/convert/testdata/regression/`, with one directory for
each case. If you change the output of the converter on purpose, run
`make regen-regressions` to make the golden files again. Then do a check of
the diff. The diff shows what your change did.

The schema in [`schema/json-output/v1.json`](schema/json-output/v1.json)
controls the `--json` output. If you change the result fields of a command,
change the schema too. If you do not, the conformance tests fail.

## Commit messages

Commit messages obey
[Conventional Commits 1.0.0](https://www.conventionalcommits.org/en/v1.0.0/):
`type(optional-scope): description`. For example:

```
feat(convert): rewrite anchor links
fix(client): handle 404 on missing page
docs(confluence): record what <ac:link> looks like
```

The usual types are `feat`, `fix`, `docs`, `refactor`, `test`, `chore`, and
`build`. The scope is usually the package. We prefer small commits that each do
one thing. We do not prefer one large squashed commit.

## Before you change code that talks to Confluence

Read [docs/confluence/](docs/confluence/) first. Atlassian gives little
documentation for the storage format. Some parts of its REST API documentation
are not exact. Thus markfluence uses facts that we found by experiment, and
that directory records them. It also lists the traps that already gave persons
confident, wrong conclusions.

If you find a new fact about the behavior of Confluence, write it in that
directory. Also write how you found it. A claim the next person cannot
reproduce from the note is a claim they have to establish from scratch.

## Architecture

[CLAUDE.md](CLAUDE.md) is the working map of the codebase. It tells what each
package owns and why it has that shape. We wrote it for coding agents. But it
is also the most complete guide for persons.

## License

markfluence uses the [Mozilla Public License 2.0](LICENSE). We accept
contributions under the same license.

## Make a release

For maintainers: [docs/releasing.md](docs/releasing.md).
