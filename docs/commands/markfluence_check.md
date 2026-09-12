## markfluence check

Validate markdown files against the converter and frontmatter rules, offline

### Synopsis

Validate one or more markdown FILEs against the converter and frontmatter
rules, with no network access and no credentials -- fast, safe, and
CI/agent-friendly. Reports conversion warnings and broken image/link
references, and metadata sanity (parseable, page_width valid, page_id
numeric when present). Each file is processed independently; the command
exits non-zero if any file is broken or failed outright. Warnings alone do
not fail.

A file's metadata is checked wherever it lives -- its own frontmatter or a
'pages:' entry for it in markfluence.yaml -- and an entry is reported only
when its file is one of the FILEs given, so one bad entry never blocks
checking the rest of a repository. Two locations naming different pages is
an error; a file keeping its own keys in a project that uses 'pages:' is a
warning, since both work.

"link not resolved: TARGET" means TARGET is a sibling .md file that exists
under the documentation root but has no page_id yet -- the normal state of
a tree that hasn't been published, not a defect. "same-page anchor not
resolved: #heading" is the same situation for a same-page anchor: it
resolves to a real heading in the current file, but can't be turned into
an absolute URL until this file itself has a page_id -- resolved by this
file's own first publish, nothing to fix.

```
markfluence check FILE... [flags]
```

### Examples

```
  # Validate a batch of files
  markfluence check docs/*.md

  # Show the storage HTML a publish would send
  markfluence check --show-html docs/one-page.md

```

### Options

```
  -h, --help        help for check
      --show-html   Also print the converted storage HTML and attachment list, for debugging.
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

