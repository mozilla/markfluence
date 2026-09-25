# 052: upload an attachment through the root that checked it

Answers #186. The converter checks an image through the documentation root's
`os.Root`, but the client computes its checksum and uploads it through a plain
path. So the check and the two reads are three separate lookups, and only the
first is bounded. A directory on the path replaced by a symbolic link between
the check and the reads makes markfluence upload a file from outside the root,
which **S2** (`no-read-outside-root`) rules out. It is a race, not a static
hole: no layout of files on disk triggers it by itself.

The same shape has a second, milder consequence, which this plan fixes too:
the checksum and the upload are two opens, so the bytes uploaded can differ
from the bytes whose checksum is recorded in the attachment's comment.

## Decisions

**D1. A `LocalAttachment` carries the root, and opens through it.** It gains
`Root *os.Root` (tagged `json:"-"`, so `check --show-html --json` and the
schema are unchanged) and an `Open() (*os.File, error)` method:

- with a `Root`, it opens `Source`, the root-relative path the converter
  checked, through `Root.Open`, which refuses an escape through a symbolic
  link anywhere on the path;
- without one, it opens `Path` with `os.Open`.

`Path` stays: it is what error messages and `check --show-html` show.

**D2. The converter sets `Root`.** `images.go` already holds `r.root.FS`; it
sets it on every attachment it records.

**D3. `attachment-upload` sets no `Root`.** Its files are named on the command
line by the person running it, not by a reference in a Markdown file, so S2
does not govern them, and a file given with `--name` need not be under any
root at all. It still gets D4, which removes its own check-then-open race
(`os.Stat` and a later open).

**D4. Whatever is opened must be a regular file, checked on the handle.**
`Open` calls `Stat` on the opened file and refuses anything that is not
regular. The converter's `Lstat` and `attachment-upload`'s `os.Stat` checked a
path; this checks the thing actually read, so a FIFO swapped in cannot hang
the upload and a directory cannot be "uploaded".

**D5. The upload verifies the checksum it recorded.** `planAttachments`
computes the checksum through `Open`; `uploadAttachment` hashes the bytes as
it copies them into the request, and before sending compares the result with
the planned checksum. On a mismatch it sends nothing and fails with an error
naming the file, `changed while publishing`. The upload already reads the whole
file into memory, so this costs one hash and no extra read. The error is local
(not a `requestError`), so `--json` reports it as a local failure, not
`NETWORK`.

## Files

| file | change |
|---|---|
| `internal/attachref/attachref.go` | `Root` field, `Open` method |
| `internal/convert/images.go` | set `Root` |
| `internal/client/client.go` | `planAttachments` and `uploadAttachment` open through `Open`; `uploadAttachment` takes the planned checksum and verifies it; `fileChecksum` takes the attachment |
| `cmd/attachmentupload/attachmentupload.go` | nothing required; the `os.Stat` check stays for its early, friendly error |
| `CLAUDE.md` | `internal/attachref` has no bullet; the client bullet notes that attachments open through the root |

## Tests

- `attachref`: `Open` with a root opens the file; refuses an escape through a
  symlinked directory (built after the root is opened, which is the race);
  refuses a non-regular file (a FIFO, a directory); without a root opens
  `Path`.
- `client`: `SyncAttachments` with an attachment whose root-relative path
  escapes through a symlinked directory fails and uploads nothing; a file
  whose content changes between plan and upload fails with `changed while
  publishing` and uploads nothing (a test hook between the two, or a plan
  built by hand with a wrong checksum).
- `convert`: an image attachment carries the root.

## Not in scope

- **Holding one file handle from plan to upload.** It would make D5
  unnecessary, but a page with many images would hold many descriptors open
  across network calls. Reopening through the root and verifying the checksum
  gives the same guarantee.
- **Bounding `attachment-upload` by a root** (D3).

## Amended after code review

- **D4: a symbolic link inside the root is refused too.** `os.Root` refuses
  an escape but follows a link that stays inside the root, and markfluence
  follows none (docs/design-principles.md, Symlinks). A link swapped in at an
  image's own name since the converter's `Lstat` would publish another file in
  the project, a `.env` say, under the image's name. After opening through the
  root, `Open` re-`Lstat`s the name, refuses a link, and requires `os.SameFile`
  between the name and the handle.
- **D4: an escape reads as "outside the documentation root"**, the converter's
  wording, not `os.Root`'s bare `path escapes from parent` under a generic
  "opening" wrapper, which read as a disk failure.
- **D5 is replaced.** Refusing a file that changed between planning and
  upload made an autosave or a build step fail a whole publish, half done.
  Instead `uploadAttachment` reads the file whole and takes the comment's
  checksum from the bytes it sends, which keeps the comment honest with no new
  way to fail; the upload buffered the whole form already. The planned
  checksum and comment no longer ride on the plan.
- **Not changed:** a batch that stops partway still reports none of the
  uploads that landed before the failure. That predates this plan (any upload
  error does it) and is left for its own change.
