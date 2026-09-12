## markfluence update

Publish one or more markdown files to Confluence pages

### Synopsis

Publish one or more markdown FILEs to Confluence pages.

Each file's title and page id come from its own YAML frontmatter, or from
a 'pages:' entry for it in markfluence.yaml -- a file can stay pristine and
keep its metadata there instead. Both places are legal and agreement is
silent; where they disagree about page_id, space or parent the file fails,
and where they disagree about title, page_width or labels the frontmatter
wins with a warning.

A file that neither place mentions is skipped, not failed: a repository
legitimately holds markdown that is not published, so a glob over a docs
tree does not go red because somebody added a draft. A file that IS
registered but has no page id fails -- something claimed it and the page
has not been created yet.

There are no per-page flags. Page metadata lives in the file or its entry,
which is what lets one invocation publish 'docs/**/*.md'; a flag would have
to name a single file. A project-wide 'page_width:' in markfluence.yaml is
how a whole tree gets one width.

Page width is asserted only when something declares it -- the file, its
entry, or the project-wide default -- otherwise the live page's width is
left untouched. Labels work the same way: a labels: line is asserted
exactly (anything on the page the file does not list is removed), and no
labels: line means the page's labels are left alone, not even read.

update never writes back to the file or to markfluence.yaml, so fixing a
wrong page_id is always safe: nothing is as you left it by accident. A
page_id that no longer resolves fails that file and says what to do about
it; one that is not a numeric id at all is reported without asking
Confluence.

A file that has not changed since the page's last version is skipped,
compared by mtime, unless --force is given. Each file is processed
independently; the command exits non-zero if any file failed.

--dry-run previews the version bump, attachment uploads and any width or
label change without writing to Confluence. It honours the mtime skip and
--force exactly as a real run does, so its forecast matches.

```
markfluence update FILE... [flags]
```

### Examples

```
  # Publish a file, taking the page id from its frontmatter or its entry
  markfluence update docs/managing_an_incident.md

  # Publish a whole tree -- the CI shape: metadata comes from the files
  # and from markfluence.yaml, so nothing has to be passed per file
  markfluence update docs/**/*.md

  # Publish a batch with a version message
  markfluence update docs/*.md --message "Bulk update"

  # Republish even though the file has not changed
  markfluence update docs/foo.md --force

  # Preview, write nothing
  markfluence update docs/*.md --dry-run

  # See which location supplied each file's metadata
  markfluence update docs/*.md --json | jq -r '.results[] | "\(.file) \(.metadata_source)"'
```

### Options

```
      --dry-run          Preview what would be published without writing to Confluence.
      --force            Skip the file-mtime check and always update the page.
  -h, --help             help for update
      --message string   Version message. (default "Updated via markfluence")
```

### Options inherited from parent commands

```
      --cloud-id string   Atlassian cloud ID; set to use a scoped API token via the api.atlassian.com gateway (falls back to $CONFLUENCE_CLOUD_ID, then .env)
  -d, --debug             Enable verbose debug output
      --env-file string   Path to an env file to read (default: .env at the discovered project root, or the working directory if none)
      --json              Emit machine-readable JSON to stdout instead of human output
      --no-color          Disable colored output
      --root string       Documentation root, overriding discovery (default: the directory holding markfluence.yaml, found by walking up from each file, or the file's own directory if none)
      --url string        Confluence base URL (falls back to $CONFLUENCE_URL, then .env)
      --username string   Confluence username/email (falls back to $CONFLUENCE_USERNAME, then .env)
```

### SEE ALSO

* [markfluence](markfluence.md)	 - Publish markdown to Confluence

