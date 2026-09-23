# 051: `credentials-init`, a command that writes the credentials file

Answers #193. After #188 (plan 050), people create
`~/.config/markfluence/credentials` by hand. This plan adds
`markfluence credentials-init`, which prompts for the settings, checks them
against Confluence, and writes the file with the right permissions.

It writes only the credentials file. Project setup (`markfluence.yaml`, a
GitHub Actions workflow) stays with #5's `init`: the credentials file belongs
to one machine and `markfluence.yaml` to one project, and a person fixing auth
from `~` must not end up with a `~/markfluence.yaml` that becomes the
documentation root for every Markdown file under their home directory.

## Decisions

**D1. The name is `credentials-init`.** Multi-word commands put the noun first
(`attachment-list`, `user-info`), and markfluence has no command groups. `init`
is #5's. `login` and `auth-login` describe a session that does not exist:
nothing logs in, and there would be no `logout` to pair with it.

**D2. It writes only the default credentials file** (`credentialsPath`, D2 of
plan 050). No flag for another path: a file for a second site is a copy and an
edit, and a target flag could be pointed into a repository by mistake. Other
targets are in Not in scope until someone needs one.

`--env-file` is refused (`cmd.Flags().Changed("env-file")`). It cannot name
the target (above), and it means nothing else here.

When there is no credentials path (`credentialsPath` returns "" for a relative
or empty `HOME`, or an `os.UserHomeDir` failure), the command refuses with
exit 2 and says why: there is nowhere to write.

**D3. The prompts.** In order:

1. **Site URL.** Normalized to scheme and host: surrounding space is trimmed,
   `https://` is added when there is no scheme, and a path is dropped, with a
   note when one was, so a pasted page URL works. Every request appends
   `/wiki/...` to the site URL, so a kept `/wiki` would double. A scheme other
   than `https` is refused: the token goes out as basic auth.
2. **Username.**
3. **API token**, read without echo (D6).

Every answer is trimmed (`strings.TrimSpace`), so a pasted trailing space or
newline never reaches the file. A blank answer is refused and asked again,
except where there is a current value to keep (D4). End of input (Ctrl-D) at
any prompt aborts: exit 1, nothing written. Without that, a blank-answer loop
reading a closed stdin would spin forever, and `ReadPassword` reports an
empty line at EOF as `io.EOF`. The cloud ID is not a prompt (D5).

Prompts go to stderr, and results to stdout through `internal/ui`.

**D4. An existing file prefills the prompts, and is rewritten.** Each prompt
shows the current value as its default and Enter keeps it; the token's default
is shown as `(Enter keeps the current token)`, never any part of the token.
Running it again to rotate a token is then one paste. The file is rewritten
whole, so if the existing one holds a comment or a key other than the four
`CONFLUENCE_*` settings, the command says, before it asks anything, that those
lines will not be kept.

The existing file is read with the same rules as `Resolve` (`loadCredentials`):
missing is fine, a dangling symbolic link or an unreadable file is an error and
nothing is written.

Reading a loose existing file goes through `loadDotenv`, which would raise the
#136 "run chmod 600" warning just before the command rewrites the file `0600`
anyway. The command silences the warner for that read (`SetSecurityWarner(nil)`
around it, then reinstating the previous one, which needs a getter or a
returned restore function) and instead says the rewrite will make the file
`0600`.

Values are never prefilled from the environment or `--env-file`. The file
holds what was typed.

**D5. The cloud ID is fetched and saved for every `*.atlassian.net` site.**
Only there: `gatewayPrefix` is hardcoded to `api.atlassian.com`, which
Government and isolated Cloud sites do not use, and `tenant_info` is verified
only on `*.atlassian.net`. Those sites are in scope though untested
(CLAUDE.md), and a saved cloud ID would send every request to the wrong
gateway, where the check would then refuse credentials that work. For any
other host the command saves no cloud ID and says why.

After the URL prompt, the command asks `GET {site}/_edge/tenant_info`, which answers `{"cloudId": "…"}`
with no credentials (docs/confluence/api.md, verified 2026-08-07), and shows
the result. There is no question about whether the token is scoped: an
unscoped personal token returns 200 through the gateway (docs/confluence/api.md,
verified 2026-08-07, and every write measurement since went through it with a
personal token), so saving a cloud ID costs nothing, and nobody has to know
what one is.

The request is sent **without** an `Authorization` header: it does not need
one, and at that point no username or token has been typed.

If the site answers but gives no usable cloud ID (a non-200, a body without
`cloudId`, or a value `validateCloudID` refuses), the command warns and goes on
without one: an unscoped token still works, and a scoped one can be fixed by
running the command again. If the site cannot be reached, it says so and goes
on without one; the check (D7) will fail the same way and ask.

A prefilled cloud ID is never kept: it is fetched again, because the URL may
have changed.

**D6. The token is read with `charmbracelet/x/term`.** `ReadPassword` and
`IsTerminal` from `github.com/charmbracelet/x/term` v0.2.1, which lipgloss
already brings into the build, so no new code ships in the binary. It becomes
the seventh direct dependency in `go.mod`. It is pre-1.0 and lipgloss decides
its version in practice; `golang.org/x/term` was the alternative, and would add
a module to the build.

`ReadPassword` turns echo off and restores it in a `defer`, but it leaves
`ISIG` set, so Ctrl-C kills the process before the `defer` runs and leaves the
terminal with echo off. The command saves the terminal state with `GetState`
before reading the token and, for `SIGINT`, `SIGTERM`, and `SIGHUP` during the
read, restores it with `Restore`, prints a newline, and exits 130. It calls
`signal.Stop` after the read, so the rest of the run keeps default signal
handling.

Plain lines are read from stdin a byte at a time, not through a `bufio.Reader`:
a buffered reader would swallow typed-ahead input that `ReadPassword`, which
reads the file descriptor directly, then never sees.

**D7. The check before saving.** The command prints `Checking the
credentials…`, builds a client from the typed values and the fetched cloud ID,
and calls `CurrentUser`, the request `user-info` makes. On success it reports
the account's display name and account id. The line first, because `send`
retries an unreachable host for about 2¾ minutes (5 attempts at the 30-second
read timeout, plus backoff), and without it that is a silent hang.

A failure is classified by the response's shape, never by its status alone,
because the same status arrives for unrelated reasons
(docs/confluence/api.md, "A scope failure is a 401, not a 403"). The markers
are the ones `HTTPError.hint` already matches (client.go), so the check and
the hint cannot disagree:

| response | meaning | action |
|---|---|---|
| `HTTPError.RejectedCredential()` | wrong or revoked username or token | refuse |
| 401 from the site domain, not JSON | a scoped token sent without a cloud ID (only reachable when D5 saved none) | refuse, and say the token looks scoped, and why no cloud ID was saved |
| 401 with `scope does not match` | the token **authenticated**, but lacks `read:confluence-user` | ask `Save anyway? [y/N]`, saying the credentials are good but `user-info` will not work with this token |
| any other 401 or 403 | refused, in a shape not yet measured | refuse |
| 404 that is not `RejectedCredential` | the URL is not a Confluence site | refuse |
| no response, 5xx or 429 after retries, or a body that would not decode | unknown: the values may well be right | ask `Save anyway? [y/N]` |

A 200 whose account has an empty `accountId`, or is `type: anonymous`, is
refused: the site answered without authenticating anyone.

**Refuse** writes nothing (an existing file stays byte-identical), names the
URL and the username, and exits 1: a mistyped token never reaches the disk.
**Ask**: yes writes the file and says it was not checked, no writes nothing
and exits 1.

The shape check is in `internal/client`, as `HTTPError` methods beside
`RejectedCredential` (for example `ScopeMismatch()` and `SiteRejectedAuth()`),
refactoring `hint` to use them, so the markers exist once. The check decides
with `var he *client.HTTPError; errors.As(err, &he)`. What the gateway answers
for a wrong token on `/user/current` is not measured yet, which is why "any
other 401 or 403" has a row (see Checks).

**D8. How the file is written.** `client.WriteCredentials(path, values)`:

- The directory is created `0700` if missing; an existing directory's mode is
  left alone.
- The file is written to a temporary file in the same directory
  (`os.CreateTemp`, which creates it `0600`), synced, and renamed over the
  target, so an interrupted write never leaves a half file, and a loose
  existing file comes out `0600`. The temporary file is removed on every
  failure path.
- When the target is a symbolic link (a dotfiles repository), the write goes
  to the link's target, with the temporary file beside it, so the link
  survives. It resolves the path with `filepath.EvalSymlinks` only when
  `os.Lstat` shows a link, since `EvalSymlinks` fails on a path that does not
  exist yet.
- The keys are written in the order URL, username, token, cloud ID, below a
  one-line comment naming the command and `docs/credentials.md`. The quoting
  rule is defined as a round trip rather than as a list of characters: a value
  `v` is written bare when `unquote(strings.TrimSpace(v)) == v`, and
  double-quoted otherwise, which `unquote` strips exactly. `TrimSpace` strips
  all Unicode space (U+00A0 included), and a list would miss some. A value
  holding `\n` or `\r` is refused.
- It then reads the file back through the parser `Resolve` uses, with the
  warner silenced (the file is `0600`, but a verification read must never
  warn), and compares, the way the frontmatter writer verifies its own
  output. A mismatch is an error.

It lives in `internal/client` beside `loadDotenv` so the writer and the reader
of the format are in one package.

**D9. After saving, it warns about what the environment does to the file.**
The warnings follow `Resolve`'s actual rules, not a blanket "overrides":

- URL, username, and token all exported: the file is not read at all, so none
  of it takes effect until they are unset.
- `CONFLUENCE_URL` or `CONFLUENCE_TOKEN` exported (but not both with the
  username): every command fails the same-source rule until it is unset.
- `CONFLUENCE_USERNAME` exported: it is used instead of the file's.
- `CONFLUENCE_CLOUD_ID` exported: it is ignored, since the URL comes from the
  file, and `Resolve` will warn about it on every run.

It does not refuse: an exported username alone is harmless.

**D10. It refuses to run without a terminal, and refuses `--json`.** If stdin
or stderr is not a terminal, it fails at once (exit 2, before any prompt) with
a message pointing to `docs/credentials.md` and noting that CI uses
environment variables. Stdin, because reading answers from a pipe would bring
back the shell history problem (`echo $TOKEN |`) and make the prompt order an
interface. Stderr, because the prompts go there, and with `2>log` the person
would be answering prompts they cannot see.

`--json` is refused (exit 2) and the command goes on `cmd`'s `noJSONEnvelope`
list: a person answering prompts is the only consumer, so a schema branch would
be an interface with no user.

Every refusal in D2 and D10 is a plain returned error, not `ui.Error` plus a
silent exit. `Execute` already turns a plain error into exit 2, printed as a
human line or, under `--json`, as an `errorObject` on stderr. Under `--json`
every `ui` helper is a no-op, so the `ui.Error` route would refuse `--json`
while printing nothing at all.

**D11. The credential errors name the command.** When the credentials file
applies (`credPath != ""`), the "missing Confluence …" error from `Resolve`
adds `run markfluence credentials-init, or` before the places list. The error
that sends a person to the docs is the main way they discover the command. Not
in the two half-pair forms (only the URL or only the token set), which name
the one place the other setting can go, and where the command would write a
file that then fails the same-source rule. The same-source error does not
change either: running the command does not fix it. In CI the credentials path
is usually set too, so the suggestion appears there, which is harmless: the
command refuses without a terminal and says what to do instead.

## Implementation

`internal/client`:

- `CredentialsPath() string`: `credentialsPath`, exported.
- `ReadCredentials(path string) (map[string]string, error)`: `loadCredentials`,
  exported. It stays the one reading of the file for both callers.
- `WriteCredentials(path string, values map[string]string) error` (D8), in a
  new `credentials.go` with the path and read functions moved beside it.
- `FetchCloudID(siteURL string) (string, error)`: the unauthenticated
  `tenant_info` request (D5), through its own `http.Client` with `Timeout`
  set to the read timeout. Not through `send`, which always sets basic auth;
  losing `send`'s retries and retry logging is acceptable because D5 degrades
  to "no cloud ID". It returns the `requestError` or `*HTTPError` shapes
  `FromRequest` knows.
- `HTTPError.ScopeMismatch()` and `HTTPError.SiteRejectedAuth()` (D7), with
  `hint` rewritten to use them.
- A way to silence the warner around a read and restore it (D4, D8): for
  example `SetSecurityWarner` returning the previous function.

`cmd/credentialsinit/` exports `Cmd`. `run` is a thin shell over
`runInit(io prompter, deps)`, where `prompter` is `Line(prompt, def string)`
and `Secret(prompt string, hasCurrent bool)`, and `deps` carries
`fetchCloudID`, `verify(client.Config) (*client.User, error)`, the path, and
`getenv`. The terminal `prompter` is the only code that touches the tty, so
every decision above is testable with a scripted one.

`cmd/root.go` registers it, and its `Long` mentions it next to the credentials
file.

## Files

| file | change |
|---|---|
| `cmd/credentialsinit/credentialsinit.go` | new command |
| `cmd/credentialsinit/credentialsinit_test.go`, `main_test.go` | tests; `TestMain` calls `testenv.RunIsolated` |
| `cmd/root.go` | register; `Long` names the command |
| `cmd/root_test.go` | `noJSONEnvelope` entry |
| `internal/client/credentials.go` | new: path, read, write, `FetchCloudID` |
| `internal/client/config.go` | move the path/read functions out; D11's wording; `Resolve`'s closing comment ("Without a cloud ID … an unscoped personal token needs") |
| `internal/client/client.go` | `ScopeMismatch`/`SiteRejectedAuth`; `hint` uses them, and the site-rejected hint names `credentials-init`; the `Config.CloudID` comment says leaving it empty is for Data Center, not required by a personal token |
| `internal/client/*_test.go` | tests below; `config_test.go` pins the exact "missing" wording and changes with D11 |
| `docs/confluence/api.md` | the gateway's answer for a wrong token on `/user/current` (Checks) |
| `go.mod`, `go.sum` | `charmbracelet/x/term` direct |
| `docs/credentials.md` | setup starts with the command; manual steps stay as the alternative; cloud ID wording (D5) |
| `README.md` | Configure names the command; the scoped-token section says the command fills the cloud ID |
| `.env.example` | the cloud ID comment no longer says to leave it out for a personal token |
| `docs/commands/` | `make docs` |
| `CLAUDE.md` | Configuration (the command; "leave it unset for an unscoped token" becomes "harmless for one"), a Layout bullet, the four exports, the seventh dependency |

## Tests

`internal/client`:

- `WriteCredentials`: file `0600`, new directory `0700`, existing directory
  mode untouched; a loose existing file becomes `0600`; a symbolic link is
  written through and survives; round-trip of values with a leading space, a
  trailing U+00A0, a leading quote, a value both starting and ending with a
  quote, `#`, `=`, and `export `; `\n` and `\r` are refused and the old file is
  untouched; no temporary file is left behind on either path; writing raises
  no permission warning.
- `ScopeMismatch`/`SiteRejectedAuth`/`RejectedCredential` against the measured
  bodies, and `hint`'s output unchanged for the existing cases.
- `FetchCloudID`: the id from a 200; no `Authorization` header on the request;
  a 404, a body without `cloudId`, and a value with `/` are errors;
  an unreachable server is a `FromRequest` error that is not an `HTTPError`.
- The "missing" error names `credentials-init` when there is a credentials
  path, and does not when there is none.

`cmd/credentialsinit` (scripted prompter, stub deps):

- Stdin not a terminal, or stderr not a terminal: exit 2, no prompt, nothing
  written. `--json` (the refusal arrives as an `errorObject` on stderr),
  `--env-file`, and no credentials path: exit 2, nothing written.
- EOF at each prompt: exit 1, nothing written, no loop.
- A pasted token with a trailing space is saved trimmed.
- A loose existing file: no chmod warning during the prefill read, and the
  rewrite leaves it `0600`.
- A new file: the values typed plus the fetched cloud ID are written.
- URL normalization: a bare host, a trailing slash, and a page URL all become
  `https://host`; `http://` is refused and asked again.
- Prefill: Enter at every prompt keeps the URL, username, and token, and the
  cloud ID is refetched, not kept; an existing comment triggers the notice.
- `tenant_info` with no id: warns and writes no cloud ID.
- A host not under `.atlassian.net`: no `tenant_info` request, no cloud ID,
  and a note saying why.
- The check, one case per row of D7's table: each refusal writes nothing and
  leaves an existing file byte-identical; each "ask" with "n" writes nothing
  and with "y" writes and says unchecked; an anonymous account is refused.
- The environment, one case per D9 bullet, and none gives no warning.
- An unreadable existing file fails before any prompt.

`cmd`: `TestSubcommandsDocumentThemselves` and `TestSubcommandsCompleteArgs`
cover `Long`, `Example`, and completion (no arguments, so
`completion.Values()`).

## Checks

- `make check`.
- **Live:** run `./bin/markfluence credentials-init` against the Mozilla
  instance with `XDG_CONFIG_HOME` pointed at a temp directory: a good token
  saves and reports the account; a wrong token through the gateway, to measure
  what `/user/current` answers there (D7), recorded in docs/confluence/api.md;
  then `user-info` with that file. One request per case, no loops, on the
  shared instance.
- Ctrl-C at the token prompt leaves echo on (D6), by hand.

## Not in scope

- **Writing anywhere but the default path** (D2), including honoring
  `--env-file` as a target.
- **Non-interactive input**: flags or piped answers (D10).
- **The OS keychain and named profiles**, for #188's reasons.
- **Project setup** (`markfluence.yaml`, a workflow): #5.
- **Checking that the username matches the account** `CurrentUser` returns:
  Atlassian hides the email address by privacy setting, so the comparison
  would often have nothing to compare. The command shows the account instead.
