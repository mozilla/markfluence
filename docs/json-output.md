# `--json` output, in detail

This file tells you why the JSON shape is the way it is. It also gives
details for each command that script authors will want to know.

The [README](../README.md#--json-output) shows how to use `--json`,
shows an example, and documents the exit codes.

The JSON Schema at [`schema/json-output/v1.json`](../schema/json-output/v1.json)
is the authoritative contract for each field. The `markfluence schema` command
also prints it.

## Compatibility

`schema_version` is `1`. It increases only for a change that can break a
consumer.

These changes are compatible, and `schema_version` stays the same:

- a new key on the envelope, on a result, on a summary, or on the error
  object;
- a constraint that is made looser;

- a new command, which adds a value to the `command` enum. Only the new
  command's own output carries that value, so no consumer of an existing
  command sees it;
- a new value of `field` in a `diff` result. It is a string rather than an
  enum for this reason: a frontmatter field that markfluence learns later is
  reported there too.

These changes break compatibility, and `schema_version` increases:

- a key that is removed or renamed;
- a key whose type or meaning changes;
- a key that can be `null` and could not before;
- a new value in any other enum, such as `code` or a result's `status`. A
  consumer can reasonably handle each value of those, and a new one would
  reach a consumer of an existing command.

Thus a consumer must ignore a key that it does not know. The published schema
is open for this reason: an object can have keys that the schema does not list.
If you validate the output, validate against the schema that
`markfluence schema` prints, which is the schema of the binary that you run.
A copy from an older release does not list the newer keys, but it still
accepts them.

markfluence's own tests validate against a closed copy of the schema, which
refuses any key that the schema does not list. Thus a key cannot get into the
output before it gets into the schema.

## Notes on the schema

- **Stable for each command.** Each command always writes the same keys in the
  same shapes. An empty value is `null` or `[]`. The *set* of keys is different
  for each command. Any change that breaks compatibility increases
  `schema_version`. See [Compatibility](#compatibility) for which changes
  those are.
- **`roots`** lists each different
  [documentation root](../README.md#the-documentation-root) that the command
  resolved, in sorted order. It is `[]` for a command that has no per-file root,
  such as `find` or `search`. It is also `[]` for a preflight failure that
  stopped before root resolution. `schema` writes no envelope at all, so it has
  no `roots` key.
- **`warnings`** holds warnings about the *invocation*, and not about a page or
  a file. Now, both such warnings come from reading credentials: the
  permission warning for a file that holds your API token, and the warning
  about a cloud ID that markfluence ignored (see
  [credentials.md](credentials.md)).
  The warnings of a result are on that result. This field is for a warning that
  belongs to no result. It is `[]` when there is nothing to report. It is also
  on the error object on stderr. A fatal failure writes no envelope, and a
  credential failure is exactly the run where a warning about that file is
  important.
- **Status verbs** are different for each command:
  - `update`: `published`, `skipped`
  - `create`: `created`, `not_created`
  - `check`: `clean`, `warnings`, `broken`
  - `attachment-upload`: `created`, `updated`, `skipped`
  - `attachment-download`: `downloaded`, `skipped`
  - `export`: `wrote`, `skipped`, or `""` for a page that failed. Each
    attachment in its `attachments` array has `downloaded`, `skipped`,
    `skipped_unreferenced`, or `failed`.

  Every command in the list, except `export`, also has `failed`.

  The results of the other commands hold data only, and they have no status
  verb. `children` has a `status` field, but that is the content status of
  Confluence (`current`, `archived`), and not a status verb.
- **There is one result for each target**, and the target is different for each
  command:
  - `page-info`, `read`: the page. There is always one.
  - `export`: the page. There is one result for each page that it exported,
    so `--depth` and `--space` give more than one.
  - `update`, `create`, `check`: the file.
  - the three `attachment-*` commands: the attachment. Thus
    `.results[] | .filename` works, and `summary.total` is the count of
    attachments. If markfluence cannot find the page, the result is a single
    failure that has a `page_id` and no `filename`.

  `export` puts the files that it wrote in an `attachments` array on its page
  result, as `update` and `create` do.
- **`metadata_source`** on `update` and `create` tells you which location
  supplied the page metadata of that file. The values are `"frontmatter"`,
  `"manifest"` (a `pages:` entry in `markfluence.yaml`), or `null` when nothing
  claimed the file. When both locations supply metadata, it reports
  `"frontmatter"`, because frontmatter wins every field that it can win.

  This field exists because otherwise, "why did this publish to *that* page?"
  has only one answer: do the resolution again by hand. That is difficult from
  a CI log. On `update`, a `null` source comes with `status: skipped`. Nothing
  claims the file, and that is not a failure, because a repository can
  correctly hold Markdown that nobody publishes.

  `diff` also reports it. `diff` also gives a **per-field** `source` on each
  frontmatter difference. That field *does* show the case where both locations
  supply the value (`"frontmatter and markfluence.yaml"`). To correct a field
  that two locations supply, you must edit two files. If markfluence told you
  only one of them, the value would come back on the next run.
- **`base` and `body_changed`** on `update` report the data that the moved-page
  check and the unchanged-body check had (#149).

  `base` is the merge base that an earlier `create`, `update`, or `export`
  recorded locally for that file. It is `null` when markfluence could not use a
  base: there was no log, no line for the file, or a line for a different page.
  That `null` is the signal that a consumer needs, and a person does not. It
  means that the checks could not run, so markfluence published the file with
  no check.

  `body_changed: false` means that the page already held what the file renders
  to. Thus markfluence skipped the body `PUT`, and `version.previous` is equal
  to `version.new`. The attachment, width, and label passes still ran. Thus the
  result can be `published` with no new version. `body_changed` is `null` when
  the check did not run: there was no base, the base had no sha, or you gave
  `--force`. `--force` always publishes and uses neither check.
- **`code: "CONFLICT"`** is the refusal to overwrite a page that has a newer
  version than your copy. It is not `VALIDATION`, because nothing in the file
  is wrong. To correct it, export the page again or give `--force`. The run
  exits with a code that is not zero, and the other files in the batch are not
  affected.
- **The `broken` status of `check` is `ok: false` with no `error` and no
  `code`.** For every other failure, those fields hold an operational error.
  For `broken`, the `broken` and `warnings` arrays already give all the
  information, so there is no operational error to attach. Only the `failed`
  status of `check` sets `error` and `code`, as every other command does. A
  `failed` file never got to the converter at all.

  `check --show-html` adds a `debug: { html, attachments } | null` field. It is
  filled in only for a file that got to the converter. `html` is exactly what
  the converter made, with no indents, because it must agree with what
  `update` and `create` would publish.
- **`diff` gives its answer in two parts**, and under `--json` both parts are in
  the payload. `diff` is the unified diff of the body as one string. It never
  has color, and it is `""` when the bodies agree. `frontmatter` is an array of
  the fields that are different. It is `[]`, never `null`.

  `differs` answers the question that the exit code answers. `body_differs` is
  a narrower question: "is there a patch?" For a file where only the title
  changed, `body_differs` is `false` and `differs` is `true`.

  A row with `comparable: false` has a `null` `confluence` value, because the
  read of the page failed. markfluence reports that row, but the row does
  **not** set `differs`. Nobody asked the page, so nothing can say that the
  page disagrees. markfluence compares only the fields that the file declares.
- **A compound value is an object**, and never a display string. Examples are
  `version`, `page_width`, and the `created` and `updated` author stamps on
  `page-info`.
- **Two fields use the word "status", and they are different things.**
  `content_status` on `page-info` is the content status of Confluence:
  `current`, `archived`, or `trashed`. `page_status` is the colored lozenge
  next to the page title. Its shape depends on what the command can say about
  it:
  - `update` and `create` report `{name, action}`. `action` is `set` or
    `unchanged`. `unchanged` is useful because a status write gives the page a
    new version. Thus `unchanged` is the evidence that a run that changed
    nothing added no version.
  - `page-info` and `read` report only the name.

  On `update` and `create`, `page_status` is `null` when the file declares no
  status, or when markfluence could not assert it. The second case is a
  warning to act on. On `page-info` and `read`, it is `null` when the page has
  no status, or when the read failed.

  `page-info` also has `page_status_available`. This is the list of statuses
  that the account that ran the command can give to **that page**. It is `[]`
  when the page can be given no status, and `null` when that read failed. It is
  *not* a property of the space. Confluence makes the list for each page and
  each account. Thus another page in the same space can have more statuses or
  fewer. Confluence refuses a write of a status that is not in the list.
  The list also never includes a custom status of your own, because nobody
  else can use it.
- **The preflight abort of `create`** occurs when any file fails, and then
  markfluence creates nothing. The result lists every input file. The failed
  files have an `error`, and the other files are `not_created`. The summary
  sets `summary.aborted: true`.
- **Warnings and notices about broken images and links** are data in the
  `warnings` and `broken` arrays on each result. They are not log lines on
  stderr.
- **`space-info` can fail in 3 independent places, and each failure is
  visible.**
  - `access` is `null` when markfluence could not read the permissions of the
    space. That means "not known", and never "can do nothing". Its field
    `create_pages` is not called `write`, on purpose, because permission to
    edit a page that exists is not a space grant at all.
  - `pages` and `recent` are both `null` when the page walk failed. They are
    never partial, because a wrong count is worse than no count.
  - `page_statuses` is always an object with a `source` field. `"space"` is the
    list that the space has configured, which only space admins can read.
    `"page"` is the list that the running account can set on `probe_page_id`,
    and it is **not** the list of the space. `null` means that markfluence
    could read neither list.

  The counts in `recent` are counts of **pages, not edits**. `pages_created`
  and `pages_touched` overlap. `pages_created_and_touched` reports the
  intersection, so that nobody adds the two together.
- **`user-info` reports which question you asked.** `self` is `true` for the
  form with no argument, which shows the account that owns the credentials. It
  is `false` when you gave an account id. Thus a consumer never has to guess.

  `personal_space` is `null` when the account has no personal space. That is a
  real answer, because a service account has none. You cannot make its `key`
  from `account_id`, because personal spaces with an email key and with an
  account id key are both in use.

  `spaces` is present only for the form with no argument. Otherwise it is
  `null`, and it is also `null` when the survey failed. It describes the
  **authenticated** credentials, and the API route takes no account id. If
  markfluence reported it next to a different account, it would give that
  account the access of the caller. Its `write` field lists the spaces where
  those credentials can **create pages**. That is a space grant, and not
  permission to edit a page that exists.
- **The discovery commands list what they found.** `results` has one object for
  each match (`find`, `search`, `user-find`) or for each node (`children`).
  `summary.total` is that count.

  The summary of `search` has two more fields. `truncated` means that the
  search got to `--limit` and more matches were available. `skipped` counts
  index rows that had no page id to report. You can get those rows only with
  `--cql` or `--type all`. Neither field is a count of matches that you could
  get if you asked for more.

  The summary of `user-find` has `truncated` for the same reason, and for a
  stronger one. The `totalSize` of the user route gives the count of rows on
  the page that it just fetched. It does not give the size of the result set.
  Thus markfluence cannot count the other matches, even in principle.
- **`user-find` gives the Markdown for a mention as a field.** A consumer does
  not have to build it from `account_id`. It is the same string that the
  converter writes when it renders a mention *out* of storage format. Thus if
  you paste `.results[0].mention` into a body and publish it, it round-trips
  exactly.

  To build it by hand, you must know two things. The profile host is Atlassian
  Home, and not your site. The leading `@` is what makes the link a mention,
  and not a link to the profile of a person. There is no `type` field,
  although the API gives one. Every account gives `known`, also automation
  accounts and page-template accounts, so the field shows no difference.

Errors and exit codes:

- **An operational failure for one file** is in `results` as
  `{ "ok": false, "error": "…", "code": "…" }`. The command exits with `1` if
  any file failed.
- **`find`, `search`, `user-find`, `space-info`, `user-info`, and
  `children --space` have no failed result.** They name no page, so there is
  no id to attach a failure to. For an operational failure,
  they print the same typed error object to **stderr** and exit with `1`. There
  is no envelope on stdout. An empty `results` array would be worse than no
  output, because "no matches" is a real answer that a caller acts on.
- **`diff` uses the exit codes of `diff(1)` instead**, and it is the only
  command that does. `0` means the same, `1` means different, and `2` means any
  trouble. `2` includes the operational failures that every other command
  reports as `1`. A shell script has only the exit code
  (`if markfluence diff FILE >/dev/null 2>&1; then …`). Thus `1` gives the
  answer, and not a failure. Trouble that names the page is still a
  `results[0]` failure on stdout. Trouble before that point is still an error
  object on stderr. Only the code is different.
- **A fatal or preflight failure** prints a typed error object to **stderr**
  and exits with `2`. Examples are a bad flag, or a credential that does not
  resolve. For a bad flag, markfluence has not parsed the command yet, so
  `command` can be `""`:

  ```json
  { "schema_version": 1, "command": "update", "error": "…", "code": "CONFIG", "warnings": [] }
  ```

- These are the error `code` values: `CONFIG`, `AUTH`, `NOT_FOUND`,
  `VALIDATION`, `CONVERT`, `IO`, `NETWORK`, `API`, `CONFLICT`.
