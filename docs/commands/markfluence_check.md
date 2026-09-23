## markfluence check

Check Markdown files for problems, with no network access

### Synopsis

Check one or more Markdown FILEs for problems before you publish them.
check makes no network request and needs no credentials, so it is fast and
safe to run in CI or from an agent.

check reports:

  - conversion warnings
  - broken images and broken links
  - frontmatter that does not parse
  - a page_width that is not valid
  - a page_id that is not a number
  - a title or page_status that is present but empty
  - a markfluence.yaml that markfluence cannot load

check does each file separately. It exits with a code that is not zero if any
file is broken or failed. A warning alone does not fail.

check reads the metadata of a file from its frontmatter and from its pages:
entry in markfluence.yaml. It reports an entry only when that file is one of
the FILEs. Thus one bad entry does not stop the check of the other files. Two
locations that name different pages are an error. A file with its own
frontmatter in a project that uses pages: gets a warning, because both work.

check cannot tell whether a page can have the status that page_status names.
Confluence decides that for each page and each account, and check makes no
request. Thus check reports an empty page_status, but not a misspelled one.
update and create do a check of the name.

"link not resolved: TARGET" means that TARGET is a .md file under the
documentation root that has no page_id yet. That is the usual state of a tree
that nobody published yet, and it is not a defect.

"same-page anchor not resolved: #heading" is the same for an anchor in the
current file. The heading exists, but markfluence cannot make its URL until
this file has a page_id. The first publish of the file resolves it.

```
markfluence check FILE... [flags]
```

### Examples

```
  # Check a set of files
  markfluence check docs/*.md

  # Show the storage HTML that a publish would send
  markfluence check --show-html docs/one-page.md

```

### Options

```
  -h, --help        help for check
      --show-html   Also print the converted storage HTML and the list of attachments, for debugging.
```

### Options inherited from parent commands

```
      --cloud-id string   Atlassian cloud ID. Set it only for a scoped API token. If not set, markfluence uses $CONFLUENCE_CLOUD_ID, then .env
  -d, --debug             Print debug output, such as each request and each retry
      --env-file string   Env file to read credentials from. The default is .env in the documentation root of the working directory, or in the working directory if there is no markfluence.yaml
      --json              Write one JSON document to stdout, and no human output
      --no-color          Print output with no color
      --root string       Documentation root for every file. The default is the nearest directory above each file that has a markfluence.yaml, or the directory of the file if there is none
      --url string        Confluence site URL. If not set, markfluence uses $CONFLUENCE_URL, then .env
      --username string   Confluence username (your email address). If not set, markfluence uses $CONFLUENCE_USERNAME, then .env
```

### SEE ALSO

* [markfluence](markfluence.md)	 - Publish markdown to Confluence

