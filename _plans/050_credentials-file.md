# 050: a user-scoped credentials file

Answers #188. Credentials come only from sources the user chose. A
user-scoped credentials file replaces the per-project `.env`, and the
`--url`, `--username`, and `--cloud-id` flags go away.

This plan adds the implementation detail, the order of the work, and how to
check it.

## Decisions

**D1. Three sources, highest precedence first.** Each setting resolves key by
key from:

1. the file named by `--env-file PATH`,
2. the environment (`CONFLUENCE_URL`, `CONFLUENCE_USERNAME`,
   `CONFLUENCE_TOKEN`, `CONFLUENCE_CLOUD_ID`),
3. the credentials file.

An empty value counts as unset, as it does now (`resolveValue`), so
`CONFLUENCE_TOKEN=` in a higher source lets a lower source supply the token.
The explicit file ranks above the environment on purpose: a token exported in
a shell profile must not reach the URL in a file named for another instance.

**D2. Where the credentials file is.** `$XDG_CONFIG_HOME/markfluence/credentials`
when `XDG_CONFIG_HOME` is set **and absolute**, else
`$HOME/.config/markfluence/credentials`, on every platform. The XDG spec says to
ignore a relative value, and a relative one would make the file depend on the
working directory, which is the problem #188 removes. The same holds for
`HOME`: `os.UserHomeDir` returns it as set, so a relative or empty `HOME` (or
an `os.UserHomeDir` failure, as in some CI containers) means there is no
credentials file. That is not an error.

**D3. The credentials file is read only when it is needed, and a missing one is
fine.** It is read only if `--env-file` and the environment leave a key unset,
so a broken or loose file does not fail or warn on a run that never uses it.
When it is read, `fs.ErrNotExist` and `ENOTDIR` mean there is no file. Any
other failure (a directory at the path, no read permission) means the user
made one and markfluence cannot use it, so the command fails and names the
path. Silently falling through would make a token rotation look like it had
no effect.

**D4. The URL and the token must come from the same source.** If both are set
and their sources differ, `Resolve` fails before it builds a client:

> CONFLUENCE_URL comes from the environment, but CONFLUENCE_TOKEN comes from
> ~/.config/markfluence/credentials. Set both in the same place.

A source is named as `--env-file PATH`, `the environment`, or the credentials
file's path with the home directory shown as `~`. The error names the source
that *supplied* each value, so an empty key in a higher source is not blamed.
The rule applies whether or not a cloud ID is set. With a cloud ID the token
goes to Atlassian's gateway and not to the URL, so the rule protects less
there, but one rule is easier to state and to test than two. All 15 callers
already map a `Resolve` error to `CodeConfig`, so the error exits 2 and is an
`errorObject` under `--json` with no extra work.

So setting `CONFLUENCE_URL` alone for one command fails when the token lives
in the credentials file. The one-off forms are `--env-file`, or the URL and
the token together in the environment.

The cloud ID is part of the credentials: it names one site, as the URL does. It
is read only from the source that supplied the URL, and a cloud ID in any other
source is ignored. It is ignored rather than an error because a higher source
cannot say "no cloud ID": an empty value counts as unset. The username may come
from any source, since a wrong one fails with a 401.

**D5. `Resolve` takes only the env-file path.** The signature becomes
`client.Resolve(envFile string)`. `ResolveOptions` goes: three of its fields
were the removed flags, and `Roots` existed only so the `.env` walk could reuse
a command's `project.Cache`. With the walk gone, `loadEnvFile`, `dotenvDir`,
and `dotenvPath` go too, and `internal/client` no longer imports
`internal/project` (`config.go` is its only importer).

**D6. A malformed `markfluence.yaml` above the working directory no longer
fails every command.** Today `Resolve` discovers the root from the working
directory, so a project file it cannot parse fails even `read 123` (#100's
"abort immediately", in `loadEnvFile`'s comment in
`internal/client/config.go`). After this change nothing discovers a root from
the working directory. A command that reads a project file still reports it:
the per-file commands, `pageref` on a `.md` argument, and `export --dest`.
`read 123` and `search` never needed the file, so they stop failing on it.
This is intended.

**D7. `--root` stops affecting credentials, and `roots` loses the working
directory's root.** `--root` moved the `.env` lookup for `create`, `update`,
`diff`, and `attachment-upload`. Now it bounds per-file roots only, which is
all its name says.

A second effect is visible in output. Passing `Roots` to `Resolve` put the
working directory's root into the command's cache. So `create`, `update`, and
`attachment-upload` printed it as `root:`, listed it in `--json`'s `roots`, and
reported its settings (`ReportSettings`), even when no file argument lives
under it. After this change they report only the roots of the files they were
given. That is what `roots` claims to mean. `docs/root-model.md` gets
checked for text that describes the old reporting.

**D8. The credential errors name every source and link to
`docs/credentials.md`.** The "missing" error lists the places markfluence
looked, says "set them in one place" when more than one setting is missing,
and ends with a link to `docs/credentials.md` on GitHub. So does the
same-source error. For example: `missing Confluence URL (CONFLUENCE_URL), token
(CONFLUENCE_TOKEN): set them in one place: the environment,
~/.config/markfluence/credentials, or a file named by --env-file. See
https://github.com/mozilla/markfluence/blob/main/docs/credentials.md`.

`docs/credentials.md` is the reference for credentials: the sources, the file
and its setup, the two rules, one-off use of another site, the permission
warning, and what each error means. The README's Configure section keeps the
setup steps and points to it, and the root `--help` summarises and links to
it. A link, rather than a `markfluence help credentials` topic, keeps one
reference that the README and the errors share. It describes `main`, which a
released binary may trail, so the file must not be renamed.

markfluence does nothing about an existing `.env` in the working directory: no
hint and no migration.

**D9. The permission warning (#136) covers the credentials file.** It already
runs inside `loadDotenv`, which every source file goes through, so this needs
no new code. With D3 it fires only when the file is actually read. Its wording,
and the docs that say "your `.env`", become "the file that holds your API
token".

**D10. Tests cannot see the developer's credentials file.** After this change
a real `~/.config/markfluence/credentials` would feed every test that reaches
`Resolve`. It would fill in settings a test left unset, turn a "missing token"
test into a D4 error, and a real cloud ID would send a test's requests to the
gateway instead of its `httptest` server.

Isolating tests one by one is fragile, because the next test added forgets it.
So each affected package gets a `TestMain` that sets `XDG_CONFIG_HOME` and
`HOME` to an empty temp directory and unsets the four `CONFLUENCE_*` variables
before any test runs. A test then `t.Setenv`s only what it needs. No test in
the repo uses `t.Parallel`, so this is safe.

`internal/clienttest` provides the helper for the `cmd` packages. It cannot
serve `internal/client`: those tests are `package client`, and `clienttest`
imports `client`, which would be a cycle. So `internal/client` has its own
`TestMain` in `config_test.go`.

## Work, in commit order

1. **`test: isolate credential resolution from the home directory`.** Add the
   `clienttest` helper and a `TestMain` to every package whose tests reach
   `Resolve`: `cmd` (root_test.go), `cmd/pageinfo`, `read`, `diff`, `userinfo`,
   `find`, `children`, `create`, `userfind`, `spaceinfo`, and
   `internal/client`. With no `markfluence.yaml`, `.env` is read from the
   working directory, which in a test is the package directory, and no package
   directory has a `.env`. So no test reads one today, and this commit changes
   no behaviour. It lands first
   so the next commit's diff is only the change itself.
2. **`feat(client)!: read credentials from a user-scoped file`.**
   - `internal/client/config.go`: D1 to D5, D8, and `validateCloudID`'s
     message, which names `--cloud-id`.
   - `cmd/root.go`: remove the three flags. Rewrite `Long` and the
     `--env-file` usage for the new sources.
   - The 15 commands that call `Resolve`: drop the three `GetString` calls and
     pass `envFile`.
   - Tests: `config_test.go`, plus `client_test.go`'s `TestResolve` and
     `TestResolveValuePrecedence`, which test `resolveValue`'s flag argument.
     The per-package `testCmd` helpers stop registering `url`, `username`, and
     `cloud-id` and set `CONFLUENCE_URL` instead.
   - `make docs`.
3. **`docs: document the credentials file`.**
   - A new `docs/credentials.md` (D8), and a row for it in the README's
     documentation table.
   - `README.md` Configure section, and the line near 675 about where `.env`
     is read.
   - `CONTRIBUTING.md`: it tells contributors to put a `.env` in the working
     directory.
   - `.env.example`: becomes the template for the credentials file (or for an
     `--env-file`). It keeps its name, because `.gitignore` still ignores
     `.env` for anyone who uses one with `--env-file` or direnv.
   - `SECURITY.md`.
   - `docs/root-model.md`: remove "Where markfluence reads `.env`". Fix the
     NOTE near line 128 that gives the precedence as "flag, environment,
     `.env`". Rewrite the paragraph that says markfluence reports the root "and
     thus where it read `.env` from". Check the `roots` description against
     D7.
   - `docs/json-output.md`, the `warnings` comments in
     `internal/jsonout/jsonout.go`, and the `warnings` descriptions in
     `schema/json-output/v1.json`. Only descriptions change, so
     `schema_version` stays.
   - `docs/confluence/README.md` and `docs/confluence/api.md`.
   - `internal/project`: the comment in `project.go` that gives "locate `.env`"
     as a reason `Discover` runs from the working directory, and `config.go`'s
     statement of the credential precedence.
   - `CLAUDE.md`: the Configuration section, the `cmd/root.go` bullet, the
     `internal/project` bullet ("called from two starting points for two
     reasons" is no longer true), and the `internal/client` bullet's `.env`
     warning text.
4. **Code review** of the branch, verify each finding, then fix them in one
   commit.
5. **Open a PR** that fixes #188.

## Tests for step 2

In `internal/client/config_test.go`:

- each precedence pair: `--env-file` beats the environment, the environment
  beats the credentials file, and the credentials file is used when nothing
  else sets a key;
- `XDG_CONFIG_HOME` absolute is used, relative is ignored, and unset falls back
  to `$HOME/.config`; a relative or empty `HOME` means no file;
- a missing credentials file is fine; `ENOTDIR` is fine; a directory at the
  path fails and names the path;
- the credentials file is not read, and does not warn or fail, when higher
  sources set every key;
- same-source: URL from the environment with the token from the file fails,
  URL from `--env-file` with the token from the environment fails, and both
  from one source passes; a username from another source passes;
- the cloud ID comes only from the URL's source: a cloud ID in the credentials
  file is ignored when the URL comes from `--env-file` or the environment, and
  is used when the URL comes from the credentials file too;
- an empty `CONFLUENCE_TOKEN=` in the `--env-file`, with the token in the
  environment, names the environment as the token's source;
- a `.env` in the working directory, and one at a discovered project root, are
  **not** read (these replace the tests that asserted they were);
- a malformed `markfluence.yaml` above the working directory does not fail
  `Resolve` (this replaces `TestResolveFailsOnAMalformedProjectFile`);
- the permission warning fires for a loose credentials file;
- the "missing" error names the credentials file's path.

`cmd/root_test.go`'s flag list loses `url`, and a test pins that `--url`,
`--username`, and `--cloud-id` are unknown flags.

## Checks before the PR

- `make check` passes.
- `grep -rn -- '--url\|--username\|--cloud-id' --exclude-dir=_plans .` finds
  nothing but unrelated hits.
- Every remaining mention of `.env` outside `_plans/` describes a file named
  with `--env-file`, direnv, or the `.gitignore` entry.
- The GitHub Action and the demo keep working. Both pass all four settings as
  environment variables and use none of the removed flags (checked
  2026-09-23), so neither needs a change.

## Interaction with other work

`_plans/049` (help text in STE100, #165) rewrites `cmd/root.go`'s `Long` and
every flag usage, including the three this removes. This change should land
first, so 049 rewrites text that still exists.

Outside the repository:

- The maintainer's checkout has a `.env` that the live probes use. It moves to
  `~/.config/markfluence/credentials`.
- `~/.claude/skills/markfluence/SKILL.md` tells users to put a `.env` in the
  current directory. It needs the same update after this lands.

## Not in scope

- The OS keychain and named profiles, for the issue's reasons.
- Anything about an existing `.env` in the working directory (D8).
- A `--debug` line that reports which source supplied each setting. The
  same-source error names sources where it matters, and `user-info` answers
  which account a token belongs to.
- A command that writes the credentials file (#193). People create it by hand,
  so the README and `docs/credentials.md` show its whole contents inline, since
  a Homebrew install ships no `.env.example`, and give the `mkdir` and
  `chmod 600` steps.
- A user settings file (the issue's "separate config file").
