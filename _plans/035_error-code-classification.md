# Plan: classify an error code by the failure's origin

Make a `--json` failure's `code` distinguish a server failure from a local one,
in both directions: a preflight HTTP failure in `create` stops reporting
`VALIDATION`, and a local file that cannot be read on an attachment path stops
reporting `NETWORK`. Fixes #133.

## The bug

`create`'s preflight makes four kinds of server call -- `checkPageID`
(`GetPageOrNil`), `ResolveSpaceID`, `resolveParent` (`checkParentInSpace`), and
`checkTitleFree` (`SearchPagesByTitle`). Every one can fail with an
`*client.HTTPError`, and `newFailure` (create.go:192) stamps every phase-1 error
`jsonout.CodeValidation` -- the code that means "there is something wrong with
your file".

The worst case is the one `RejectedCredential` exists for. A revoked token
answers every v2 route with a 404 whose body names nothing, and `GetPageOrNil`
gates its nil-return on `notFound`, which excludes exactly that case
(client.go:248) -- so `checkPageID` returns the `*HTTPError`, and `--json`
reports `VALIDATION` against a file that is perfectly fine. `jsonout.CodeFor`
asks `RejectedCredential` *before* its status switch precisely so this reports
`AUTH`, and `create` throws that away. `create`'s own phase 3 (`publishOne`)
already uses `CodeFor`, so today one credential produces `AUTH` from publish and
`VALIDATION` from preflight. `cmd/fix` gets it right for the HTTP half via
`locateCode` (fix/json.go:142). Two commands classifying one failure
differently is the defect #133 names.

The same defect runs the other way on the attachment paths, and #127 walked
past it. `client.planAttachments` calls `fileChecksum(att.Path)`
(client.go:1033) -- a local `os.Open` -- and returns that error raw, so a
`SyncAttachments`/`PlanAttachments`/`ForceUploadAttachments` failure classified
through bare `CodeFor` reports **`NETWORK`** for an unreadable local asset. Four
sites do that. `create`'s `publishOne` comment names this exact condition as one
of S7's residuals ("an image that Lstat'd fine in preflight can still be
unreadable now"), and when it happens `--json` blames the network.

`CodeFor` alone cannot fix either direction: it answers `NETWORK` for any
non-`HTTPError`, so routing everything through it would report `no title given`
as a transport problem.

## Decisions

**The fallback rule is not enough on its own, because a transport failure is not
an `*HTTPError`.** #133 proposes lifting `fix`'s `locateCode` -- "is it an
`*HTTPError`? then `CodeFor`, else `VALIDATION`" -- into `internal/jsonout`.
That fixes the status half only. `doJSON` builds an `HTTPError` only once it has
a status (client.go:451); a dial failure, a TLS error, or a malformed response
body comes back as a plain error from `send`, so under a bare type check a
preflight failure with the VPN down still reports `VALIDATION` -- and would in
`fix` too, while `update` reports `NETWORK` for the same failure because it
calls `CodeFor` at the call site. The distinction matters more than the status
one: `NETWORK` vs `VALIDATION` is what a consumer branches on to decide whether
retrying is worth anything.

**So the client types its own request-path errors, and callers ask a predicate
rather than marking call sites.** The rejected alternative was to wrap each
client call's error in `create` and `fix` with a typed marker, the way
`convertFailure` and `attachmentupload`'s `badInput` already do. That works, but
the obligation lands on every future call site and fails *silently* when
forgotten: a new client call added to preflight without the wrapper reports
`VALIDATION`, which is the bug being fixed, reintroduced. Typing at the source
puts the guarantee in one function where a test can hold it, the same reasoning
that keeps the traversal clamp in `internal/attachfile` instead of in two
commands.

**The rule, stated so it stays true at 118 call sites:** an error a client
method returns because *the request* failed is typed; an error that came from
the caller's own data is not. So `send`'s transport returns, all of `doJSON`'s
own returns (`json.Marshal`, `http.NewRequest`, `json.Unmarshal` of the
response), and the two other request builders (`DownloadAttachment`, the
multipart upload) are typed --
while `DownloadAttachment`'s `w.Write`, `os.Open` in the upload path, and
`Resolve`'s config errors are left exactly as they are, for their callers to
classify as `IO`/`CONFIG`. There is deliberately **no** claim that every error
from `internal/client` is typed: `w.Write` writing the destination file is a
local disk failure, and calling it a request error would be the same lie in a
new place.

**`*HTTPError` and the new type are siblings, and the predicate lives in the
client.** `doJSON` returns one or the other. Rather than have `jsonout` run two
`errors.As` checks -- and grow a third the day the client grows a type --
`internal/client` answers for its own error types with `FromRequest(err) bool`.
`jsonout` already imports `client` for `CodeFor`, so this adds no dependency
edge.

**The new type is unexported; `FromRequest` is the whole new surface.** The only
sanctioned use is the predicate, and keeping the type unexported means nothing
outside `internal/client` can start branching on it and grow a second
classification rule -- which is how the disagreement in #133 arose. It is also
the reversible direction: unexported to exported is additive the day someone
genuinely needs `errors.As`, while the reverse breaks callers. `*HTTPError`
stays exported, because `notFound`, `RejectedCredential`, the hint logic, and
`CodeFor` all read its fields.

**`Error()` returns the inner text verbatim, and the wrapper carries nothing
else.** It is a classification tag, exactly like `convertFailure`, which also
carries no message of its own. This is what guarantees the change is invisible
to human output and to every existing test that asserts on an error string: the
only observable difference is the `code` field.

**A malformed 200 response classifies as `NETWORK`.** `CodeFor`'s existing
non-`HTTPError` branch answers `NETWORK`, which is right for a dial failure and
a stretch for a response body that failed to decode. Both mean "no usable
answer, and retrying is not obviously pointless", and adding a code to split
them would be a vocabulary change (`schema_version` territory) for a case whose
message already says what happened.

**The mirror sites are fixed here, not deferred.** `_plans/034` deferred #133
because it changed codes on failures that issue was not about, when there was no
shared rule to appeal to. The rule is now the thing being added, so applying it
everywhere it belongs *is* the change -- and landing a helper whose entire
purpose is "stop guessing the code" while four call sites nearby keep guessing
would read as though the guessing were deliberate. The accepted cost is a
`--json` code change on failures #133 does not mention: `NETWORK` -> `IO` for an
unreadable or missing local asset.

**No schema change.** `$defs/code` is one global enum and all eight codes are
valid on every result shape (v1.json:159). `README.md` lists the eight values
and never claims which one a given failure carries, so it needs no edit either.

**Exit codes do not move.** A preflight `AUTH` stays a per-file failure with
exit 1. The README's contract scopes exit 2 to "bad flags, credential
*resolution*" -- resolving a credential locally -- not the server rejecting one,
and `update` already reports a rejected credential per-file. Making `create`
fatal here would create a new inconsistency of exactly the kind #133 is about.

## Implementation

### `internal/client`

- `requestError` -- unexported, holds one error, `Error()` returns the inner
  text verbatim, plus `Unwrap()`.
- `FromRequest(err error) bool` -- true for `*HTTPError` or `*requestError`,
  false for anything else including nil. Its doc comment carries the rule above,
  including what is deliberately *not* typed and why.
- Wrap sites: `send`'s transport returns; every non-status return in `doJSON`;
  `DownloadAttachment`'s and the multipart upload's `http.NewRequest`.
  `json.Marshal` of a request body cannot fail for any body this code builds,
  but it is wrapped too, so the invariant is statable as "`doJSON` returns only
  typed errors" rather than "only typed errors except one". *(Amended during
  implementation: the pagination helpers have no wrap site. `resolveNext`
  swallows its own `url.Parse` failure and falls back to appending, so
  `listV1`/`listV2`/`searchCQL` return nothing but what `doJSON` handed them.)*

### `internal/jsonout`

- `CodeOr(err error, fallback Code) Code` -- `client.FromRequest(err)` then
  `CodeFor(err)`, else `fallback`. The fallback is a parameter rather than
  hardcoded `VALIDATION` so the call site says which default it is choosing;
  both directions of the bug are one call with a different fallback.

### `cmd/create`

- `newFailure` -> `jsonout.CodeOr(err, jsonout.CodeValidation)`, after the
  `convertFailure` check so `CONVERT` still wins. Local phase-1 errors (`no
  title given`, `pageIDFailure`, the duplicate-title messages, `no space
  given`, `space %q not found`, `resolveParent`'s conflicts) are neither client
  type, so they stay `VALIDATION` by construction.
- `publishOne`'s `SyncAttachments` -> `CodeOr(err, jsonout.CodeIO)`.

### `cmd/fix`

- `locateCode` deleted; `processFile` calls `jsonout.CodeOr(err,
  jsonout.CodeValidation)`. Its tests move to `internal/jsonout` with it.

### `cmd/update`

- `SyncAttachments` and `PlanAttachments` -> `CodeOr(err, jsonout.CodeIO)`.

### `cmd/attachmentupload`

- `plan(...)` -> `CodeOr(err, jsonout.CodeIO)`. This command already
  distinguishes `IO` from `VALIDATION` upstream via `localAttachmentsCode` and
  then loses the distinction one call later, which is the clearest single
  illustration of the bug.

### `internal/attachfile`

- `Write`'s `DownloadAttachment` -> `CodeOr(err, jsonout.CodeIO)`.

## Tests

- **`internal/client`** -- the new invariant, in the package that owns it: a 500
  is an `*HTTPError`; a closed server (dial refused) and a 200 with a malformed
  JSON body are `*requestError`; all three satisfy `FromRequest`.
  `FromRequest(errors.New("x"))` and `FromRequest(nil)` are false. `Unwrap`
  returns the inner error and `Error()` matches it exactly -- that last one is
  what pins "no message anywhere changes".
- **`internal/jsonout`** -- `CodeOr` table: a rejected-credential 404 is `AUTH`
  (not `NOT_FOUND`), 403 is `AUTH`, 500 is `API`, a `*requestError` is
  `NETWORK`, and a plain `errors.New("no title given")` is the fallback. The
  last row is #133's named hazard asserted directly.
- **`cmd/create`** -- through the existing `fakeConfluence`, with a per-test
  override answering one route 404 with `"title":"Not Found"`: a file carrying a
  `page_id` reports `code: AUTH` in the emitted envelope. Plus a 403 on
  `/wiki/api/v2/spaces` reporting `AUTH`, and a `no title given` file still
  reporting `VALIDATION` in the same batch.
- **`cmd/fix`** -- the same rejected-credential 404 reporting `AUTH`. This is
  the consistency claim #133 actually makes, so it is asserted from both sides
  rather than inferred from a shared helper.
- **Mirror-direction guard** -- a 403 from the attachment listing still reports
  `AUTH` through `CodeOr(err, CodeIO)`, so the flipped fallback cannot swallow a
  server failure.

**Not tested, deliberately.** The local-`IO` direction end to end *through the
converter*, which is to say in `create` and `update`. An asset that is missing
by upload time never reaches `fileChecksum` there -- the converter reports it
`IMAGE BROKEN` and adds no attachment -- so provoking it needs a file readable
at convert time and unreadable at upload time, which means either a `chmod 000`
that behaves differently as root or a hook in the client existing only for a
test.

*Amended during implementation: `attachment-upload` has no converter in the
way, so the local direction is testable there against a real missing file, and
is tested -- which also made the guard worth restructuring. Asserting
`CodeOr(err, CodeIO)` in the test would have re-derived the command's
expression beside it rather than exercising it (the "validates a copy" trap
`CLAUDE.md` names for conformance tests), so the decision is named `planCode`
in the command, matching `localAttachmentsCode` one function below it, and the
test calls that. The other three sites stay inline: the rule itself is pinned
by the `jsonout` table, and one representative call site exercised end to end
is what the guard is for.*

## Docs

- `CLAUDE.md`, the `internal/client` bullet: the package returns two error
  types on the request path and answers `FromRequest` about them, with the
  request-vs-local rule in a clause and the note that file handling on the
  attachment paths is deliberately outside it. The existing `CodeFor`
  /`RejectedCredential` sentence there gains the `CodeOr` counterpart.
- No `README.md` change (it lists the eight codes and claims nothing per-code),
  no schema change, no `docs/confluence/` change (no new API knowledge).

## Out of scope (deliberately)

- **A new guarantee in `docs/guarantees.md`.** An `error-code-names-the-cause`
  R3 was considered and dropped. Worded checkably it would say "no error from a
  request reports a local code, and no local error reports
  `NETWORK`/`AUTH`/`NOT_FOUND`/`API`" -- but it would have to land as Partial,
  since it rests on 118 code-assignment sites across `cmd`/`internal` being
  individually right and nothing mechanically stops a new site from passing a
  wrong fallback. Auditing all 118 to claim Holds is a large review surface for
  a small yield, and a wrong call in that sweep would be a silent status lie.
  This change cites #133 and nothing else.
- **`frontmatter.ParseFile`'s unreadable-file error staying `VALIDATION`.** It
  is a local read failure reported as a file defect, which looks like a
  near-miss of the same bug but is the documented house answer: `checkResult`'s
  schema description commits to it in writing ("status=failed ... (unreadable,
  unterminated frontmatter, ...) -- code is VALIDATION in that case",
  v1.json:440). Changing it in `create` alone would make `create` and `check`
  disagree, which is the shape of defect this change exists to remove. Same for
  `roots.Resolve` and `linkindex` build failures, which are tree walks reported
  against the file that triggered them.
- **`DownloadAttachment`'s "no download link" error.** It is returned before any
  request is made -- Confluence handed back an attachment record with an empty
  `_links.download` -- so it is neither request nor local, and the `IO` fallback
  reports it as a disk problem. Left as a named residual rather than given a
  third error type or a `CodeOr` variant: it is one error on one path, its
  message says precisely what happened, and the reader's next move (look at the
  attachment record) is not changed by the code.
- **A `page_id` that resolves to nothing being classified two ways.** Found
  while writing `fix`'s test. All three commands phrase it identically through
  `pageref.NotFoundMessage`, and it is a local error by origin -- `GetPageOrNil`
  reports the page absent, so no `HTTPError` survives for `CodeOr` to read --
  yet `update` reports `NOT_FOUND` (update.go:180) while `fix` and `create` take
  the `VALIDATION` fallback. It is #133's complaint about a different failure,
  but it is not the request-vs-local rule: it is a judgment about whether "the
  id in your file points at nothing" is a defect in the file or a missing
  target, and both readings have a case (`create` must not answer `NOT_FOUND`
  for a stale `page_id`, which reads as "the page you asked for is gone"; for
  `update` the page *is* the target). Aligning three commands on a judgment call
  belongs in its own issue.
- **A client-wide "every error is typed" invariant.** Ruled out above:
  `DownloadAttachment`'s `w.Write` and the upload's `os.Open` are local
  failures, and `Resolve` is config. The invariant is scoped to the request
  path and says so.
- **Splitting `NETWORK` for a malformed response.** Would need a ninth code and
  a `schema_version` conversation.

## Commits

1. `docs: plan for classifying an error code by the failure's origin`
2. `feat(client): type every request-path error, and FromRequest to ask`
3. `feat(jsonout): add CodeOr, classifying by whether a request failed`
4. `fix(create): report a preflight HTTP failure by its status, not VALIDATION`
5. `refactor(fix): classify a locate failure through jsonout.CodeOr`
6. `fix: report a local attachment failure as IO, not NETWORK`
7. `docs: record the two client error types and CodeOr`

Commit 6 is the four mirror sites in one commit: it is one rule applied to four
call sites, and splitting it would produce four commits whose messages differ
only by package name.
