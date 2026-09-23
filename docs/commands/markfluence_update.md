## markfluence update

Publish one or more Markdown files to Confluence pages

### Synopsis

Publish one or more Markdown FILEs to their Confluence pages.

The page id and title of each file come from its YAML frontmatter, or from its
pages: entry in markfluence.yaml. With an entry, the file can have no
frontmatter at all. Both locations are legal, and update says nothing when they
agree. If they disagree about page_id, space, or parent, the file fails. If
they disagree about title, page_width, page_status, or labels, the frontmatter
wins, with a warning. With no title, update keeps the live title of the page.

update skips a file that neither location mentions, and does not fail it. A
repository can correctly hold Markdown that nobody publishes. Thus a glob over
a docs tree does not fail because somebody added a draft. A file that IS
registered but has no page id fails. Something claimed it, and nobody created
the page yet.

There are no flags for the metadata of one page. The metadata is in the file
or in its entry, and that is what lets one command publish 'docs/**/*.md'. A
flag would have to name one file. To give a whole tree one width, set
page_width: in markfluence.yaml.

update asserts the page width only when something declares it: the file, its
entry, or the default of the project. Otherwise it does not touch the live
width. Labels work in the same way. update asserts a labels: line exactly, and
removes a label on the page that the file does not list. With no labels: line,
update does not touch the labels, and does not even read them.

A page_status: line asserts the status of the page, which is the colored
lozenge next to its title. With no page_status: line, update does not touch the
status, and does not even read it. Confluence decides which statuses a page can
have, for each page and each account, so update asks the page that it publishes
to. A name that does not agree fails that file, and the error lists the names
that would work. page-info shows them too. A status write gives the page a new
version, so update does not send a status that already agrees.

update never writes to the file or to markfluence.yaml. Thus you can safely
correct a wrong page_id. A page_id that names no page fails that file, and the
error tells you what to do. A page_id that is not a number fails with no
request to Confluence.

Two checks protect the page. Both compare with what an earlier create, update,
or export recorded locally for that file, in the log next to markfluence.yaml:

  - If the page changed after you made your copy, update refuses the file, and
    does not overwrite the page. Export the page again, or use --force.
  - If the rendered body already agrees with the page, update skips the body.
    It still applies attachments, width, and labels. Thus a new version of an
    image publishes, and the page gets no new version for the body.

With no markfluence.yaml, there is no log, so neither check runs. update then
publishes every file, and each publish makes a new page version.

update publishes a file with no record yet with no check and no warning of its
own. The run reports how many such files there were. Protection starts with the
first publish or export of a file.

--force means "always publish". It overrides both checks. A CI workflow needs
this when the repository is the source of truth.

update does each file separately. It exits with a code that is not zero if any
file failed, also a refused page.

--dry-run shows the new version, the attachment uploads, and any change to the
width or the labels, and writes nothing to Confluence. It does the same two
checks as a real run, so its preview agrees with the real run.

```
markfluence update FILE... [flags]
```

### Examples

```
  # Publish a file. The page id comes from its frontmatter or its entry
  markfluence update docs/managing_an_incident.md

  # Publish a whole tree, as CI does. The metadata comes from the files
  # and from markfluence.yaml, so you give nothing for each file
  markfluence update docs/**/*.md

  # Publish a set of files with a version message
  markfluence update docs/*.md --message "Bulk update"

  # Publish, also if the page changed after you made your copy
  markfluence update docs/foo.md --force

  # Show what would happen, and write nothing
  markfluence update docs/*.md --dry-run

  # Show which location gave each file its metadata
  markfluence update docs/*.md --json | jq -r '.results[] | "\(.file) \(.metadata_source)"'
```

### Options

```
      --dry-run          Show what update would publish, and write nothing to Confluence.
      --force            Always publish. Overrides the check for a changed page and the check for an unchanged body.
  -h, --help             help for update
      --message string   Message for the new page version. (default "Updated via markfluence")
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

