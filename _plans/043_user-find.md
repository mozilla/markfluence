# 043 — `markfluence user-find NAME`

Closes #143.

#91 added support for user mentions, but there was no good way to determine
the Atlassian account id for a user when writing Markdown leaving the author to
figure it out from Confluence. Issue 143 adds another command to the finder
family: `find` resolves a title to page ids, `search` resolves full text to
pages, `user-find` resolves a name to an account id.

**The paste-ready markdown line is the deliverable, not the account id.** The id
is what the *tool* needs. The line is what the author needs, and building it by
hand means knowing two things nothing tells you: that the host is
`home.atlassian.com` and not the site, and that the `@` on the link text is what
makes it a mention rather than a plain profile link (#91,
[links-and-anchors.md](../docs/confluence/links-and-anchors.md)).

## The command

```
markfluence user-find NAME
```

Exactly one argument, free text, no `--space` (the route has no space field and
a Confluence account is not space-scoped). Nothing writes; no credentials beyond
the usual resolution.

Output is a **block per hit** rather than a table, for `search`'s reason: the
paste-ready line is far too long for a column, and it is the line the author
came for.

Example looking for an existing user:

```shell
$ markfluence user-find kahn
William Kahn-Greene  60c36d0718e9f60071326951
  [@William Kahn-Greene](https://home.atlassian.com/people/60c36d0718e9f60071326951)
$ echo $?
0
$ markfluence user-find reid
Ashley Roybal-Reid  63ab265b7cde7bff9d7876ce
  [@Ashley Roybal-Reid](https://home.atlassian.com/people/63ab265b7cde7bff9d7876ce)

Brittany Reid  712020:75e6f4e8-1ad1-42a9-9d5c-5f867110c36a
  [@Brittany Reid](https://home.atlassian.com/people/712020:75e6f4e8-1ad1-42a9-9d5c-5f867110c36a)

Kathy Reid  712020:f1e7dc96-f235-4f1c-bfd5-142ccddc9d74
  [@Kathy Reid](https://home.atlassian.com/people/712020:f1e7dc96-f235-4f1c-bfd5-142ccddc9d74)
$ echo $?
0
```

Two shapes of account id are live on one instance — a 24-character hex string
and a `712020:`-prefixed UUID — which is why #91 refused to pattern-validate one
and why nothing here parses one either.

The markdown line is built by `convert.MentionURL`, already exported for
precisely this reason ("the forward direction has to recognise what this
direction emits, and a second copy of the path would be a second thing to keep
in step"). The display name goes through `convert`'s link-text escaping, or a
person whose name contains `[` or `]` gets a line that does not parse as a link.

Finding nothing is a success — `No users found.` and exit 0 — matching `find`
and `search`, since an empty answer is one a caller acts on.

```shell
$ markfluence user-find nonuser
No users found.
$ echo $?
0
```

## What the live probe changed — verified 2026-09-15

Probed against mozilla-hub with an unscoped personal token. Six findings, and
the first two move the design rather than confirming it.

### Reusing `listV1` would silently truncate every result set at 100

This is the sharp one. The route pages by `start`/`limit` offset, exactly like
the v1 child collections `listV1` serves — but it **caps a page at 100 rows
while echoing whatever limit you asked for**:

| requested `limit` | echoed `limit` | rows returned |
|---|---|---|
| 100 | 100 | 100 |
| 101 | 101 | **100** |
| 250 | 250 | **100** |
| 500 | 500 | **100** |

`listV1` uses `v1PageSize = 250` and terminates when a page comes back shorter
than what it asked for. Pointed at this route it would request 250, receive 100,
conclude that it had reached the end, and return a truncated set with no error
on a query matching more. So this route needs its own page size, and the
constant needs the reason written beside it. Walked at 100, the pages are
honest: `user.fullname~"a"` gives 100, 100, 100, 4, then 0 — 304 people.

`totalSize` is useless here, in a way worth stating precisely because it is *not*
`/search`'s way: it reports the number of rows **on the current page**, so
`limit=3` answers `3` and `limit=500` answers `100`, with more rows beyond
either. Nothing may branch on it. There is never a `_links.next`; the only
`_links` keys are `base` and `context`.

### Deactivated accounts cannot be included, so the exclusion is documented

The issue left this open — either document it or see whether
`sitePermissionTypeFilter` can lift it. It cannot:

| `sitePermissionTypeFilter` | `user.fullname~"reid"` |
|---|---|
| *(absent)* | Ashley Roybal-Reid, Brittany Reid, Kathy Reid |
| `none` | the same three |
| `all` | the same three |
| `externalCollaborator` | zero rows |

`Mark Reid (Deactivated)` appears under none of them, and
`user.fullname~"lonnen"` finds nothing though `. Lonnen (Deactivated)` exists
and is mentioned on real pages. So the exclusion is a property of the route's
index, it goes in the command's `Long`, and the asymmetry with #91 stands:
markfluence can *render* a departed colleague's name via
`GET /user?accountId=` but cannot *find* them by one.

### `~` is word-prefix and order-sensitive, not substring

| query | result |
|---|---|
| `user.fullname~"kahn"` | William Kahn-Greene |
| `user.fullname~"Kahn-Greene"` | William Kahn-Greene |
| `user.fullname~"ahn"` | **nothing** |
| `user.fullname~"ahn-greene"` | **nothing** |
| `user.fullname~"william kahn"` | William Kahn-Greene |
| `user.fullname~"kahn william"` | **nothing** |
| `user.fullname~"kahn*"` | William Kahn-Greene |
| `user.fullname~"*"` | everybody |

A fragment starting mid-token matches nothing, and multiple words must be in
order. That belongs in the help text: the failure mode is a confident empty
result, and an author who typed `ahn` will conclude the person has no account.

### A non-user CQL field is accepted and answers zero rows with a 200

`title~"kahn"` returns `totalSize: 0` and a 200 — not a 400. This is the trap
family [confluence/README.md](../docs/confluence/README.md) opens with: a clause
the server did not honour looks exactly like a query with no matches. It only
bites a future maintainer adding a clause, since the command builds the whole
query itself, but it is why no flag here may append one without a test that the
narrowed query returns *fewer* rows rather than merely returning.

### An empty query is a 500

`user.fullname~""` answers `500 UndeclaredThrowableException`. Same shape as
`/search`'s blank query ([search.md](../docs/confluence/search.md)), same
remedy: refuse an empty NAME locally, as a usage error, before any request.

### Results include non-human accounts, and `type` does not identify them

`user.fullname~"a"` returns `[TEMPLATE] ASCII art generator` and
`Workato Automation` alongside people, and every row — those included — carries
`user.type: "known"`. So the field discriminates nothing and **is deliberately
not carried into `--json`**: a field named `type` whose only observed value is
`known` invites a consumer to filter on it and get nothing for the effort. Rows
are reported as the route returns them.

CQL injection is live here as everywhere (`user.fullname~"kahn" or type=user`
parses), and the parser accepts backslash escapes, so the existing unexported
`escapeCQL` in `internal/client/search.go` is correct for this route too.

## The scope question is unresolved, and cannot be resolved here

The route's only `Current` scope is `read:content-details:confluence`, which is
**granular**; markfluence's required union holds the *classic*
`read:confluence-content.summary`, and [api.md](../docs/confluence/api.md#scopes)
is explicit that neither vocabulary implies the other. A 200 through the
`api.atlassian.com` gateway proves nothing about this, because the token that
produced it is an unscoped personal one carrying full user permissions — which
is exactly the confident wrong conclusion that section was written about.

So: implement, and record the scope as **Unverified** in the new
`docs/confluence/users.md` with the reason, rather than adding it to the
README's copy-pasteable list on a guess. Adding a scope to that list that a
token does not need is cheap; omitting one it does need is a 401 in CI with a
misleading message. The README list gets the scope only once someone has run the
command with a scoped token — noted as the one follow-up this leaves open.

### `--limit`, and why it is not optional

`user.fullname~"a"` matches 304 people on this instance, which as blocks is
about 900 lines of scrollback. So `--limit` is a **string** vocabulary — a
positive number or `all`, default `10`, refusing `0` — exactly as
`search --limit` and `children --depth` are, for the same reason `search` keeps
its default low: a hit is a block, not a row.

The pager asks for one row more than the bound and reports the surplus as
"more exist", never a count, because `totalSize` cannot supply one. Under `all`
it walks at a page size of 100.

## The client

A new `internal/client/users.go`, beside `find.go` and modelled on it:

```go
// UserMatch is one person the user directory matched.
type UserMatch struct {
    AccountID   string
    DisplayName string
}

func (c *ConfluenceClient) SearchUsers(query string, max int) ([]UserMatch, bool, error)
```

`max <= 0` means unbounded; the `bool` is "more exist". A dedicated file rather
than another method on `client.go` because the route's paging rules are its own
and belong next to the type they produce — and because `userPageSize = 100`
needs its reason written where a reader will hit it before reaching for
`listV1`.

`LookupUser`/`GetUser` stay exactly as they are. This is the other direction and
shares nothing with them but a noun.

## `--json`

A new `command` enum entry, a result shape, and an `if`/`then` branch
constraining both `results.items` and `summary` — `internal/schematest`'s rules,
and the enum entry alone is how a new command's conformance test goes green
while validating nothing.

```json
{
  "ok": true,
  "account_id": "60c36d0718e9f60071326951",
  "display_name": "William Kahn-Greene",
  "mention": "[@William Kahn-Greene](https://home.atlassian.com/people/60c36d0718e9f60071326951)"
}
```

One object per match, so `.results[].account_id` works directly and
`summary.total` is the match count. `mention` is carried rather than left for
the consumer to assemble, for the same reason it is the point of the human
output: assembling it means knowing the host and the `@` convention.

No `type` field (measured above). No `url` field either — the profile URL is
inside `mention` already, and a second copy differing by the `@` is a thing to
get wrong.

An operational failure is an `errorObject` on **stderr** with exit 1, not a
`results[0]` failure, which this shares with `find`, `search` and
`children --space` and nothing else: those name the page they were asked about,
and there is no id here to name.

## Files

| file | change |
|---|---|
| `internal/client/users.go` | new: `UserMatch`, `SearchUsers`, `userPageSize` |
| `internal/client/users_test.go` | new: paging, the 100-cap, bound + `more`, escaping |
| `cmd/userfind/userfind.go` | new: the command |
| `cmd/userfind/json.go` | new: the result shape |
| `cmd/userfind/*_test.go` | new |
| `cmd/root.go` | register it |
| `schema/json-output/v1.json` | enum entry, `if`/`then` branch, `$defs` |
| `docs/confluence/users.md` | new: the route, the traps, the scope as Unverified |
| `docs/confluence/README.md` | index line for it |
| `docs/json-output.md` | the per-command section |
| `README.md` | the finder table, and the feature list's search bullet |
| `CLAUDE.md` | the `cmd/userfind/` bullet |
| `docs/commands/markfluence_user-find.md` | generated by `make docs` |

## The new-subcommand checklist

Every one of these has been the thing that was forgotten once:

- `Long` carries the reasoning and `Example` a worked invocation, or
  `cmd`'s `TestSubcommandsDocumentThemselves` fails.
- `ValidArgsFunction` is set, or `TestSubcommandsCompleteArgs` fails. Here it is
  `cobra.NoFileCompletions`: a name is free text, and `internal/completion` may
  not call Confluence.
- The `command` enum entry **and** the `if`/`then` branch.
- `cmd`'s `TestCommandEnumMatchesRegisteredCommands` closes the loop.
- `make docs` regenerates `docs/commands/`; never hand-edit it.
- Exit codes are the normal contract — 1 operational, 2 fatal. `diff`'s
  departure is `diff`'s alone.

## Tests

- **The 100-cap does not truncate.** A stub serving 100 rows for a requested
  page and 4 for the next must yield 104. This is the regression for the
  `listV1` trap and the one test that would have caught it.
- Nothing reads `totalSize`: a stub reporting `totalSize: 3` with 100 rows still
  pages on.
- `--limit N` returns N and reports `more`; `--limit all` walks; `--limit 0` is
  refused; a non-numeric limit is refused.
- An empty NAME is refused with no request made (a stub that fails the test if
  called).
- A name containing `"` is escaped into the CQL literal.
- A display name containing `[`/`]` produces a parseable markdown link.
- Zero matches is exit 0 with `No users found.` and an empty `results` array.
- `TestSchemaConformance` built from the command's own builder, per
  `internal/schematest`.

## Not in scope

- **An account-id argument or `--id` flag.** The probe found that
  `user.accountId="…"` is a valid CQL field here, and #91's
  `GET /user?accountId=` already resolves a *deactivated* account, returning
  `Mark Reid (Deactivated)`. So the "who is this id I found on an old page"
  case — the one the deactivated exclusion rules out — is a cheap follow-up
  whenever somebody wants it. Recorded here rather than built, and deliberately
  not shape-sniffed: two id shapes are live on one instance, so telling an id
  from a name by pattern is the thing #91 refused to do.
- **A `user-list` or group listing.** No demonstrated need.
- **Filtering out non-human accounts.** `user.type` cannot do it, and a name
  heuristic over `[TEMPLATE] …` would be a guess about somebody else's naming.
- **Completion of names.** Completion runs on every keystroke and may not call
  Confluence.
