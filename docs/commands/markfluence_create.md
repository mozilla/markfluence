## markfluence create

Create new Confluence pages from Markdown files

### Synopsis

Create new Confluence pages from Markdown FILEs.

The title comes from the frontmatter, or from --title. --title overrides the
frontmatter, and it works with one FILE only.

The space comes from --space or the frontmatter. If they disagree, create
refuses the file. If neither gives a space, create uses the space: in
markfluence.yaml.

The page width comes from --page-width, then the frontmatter, then
markfluence.yaml. The first one that gives a value wins, and the default is
max.

The parent comes from --parent or the frontmatter. It can be a page or a Cloud
folder. Give the id of a folder in the same way as the id of a page.

A page_status: line sets the status of the new page, which is the colored
lozenge next to its title. Confluence decides which statuses a page can have,
for each page and each account. Only the new page can answer for a new page.
Thus create does a check of the name after the page exists. If the page does
not accept the name, the page stays created, with no status and a warning that
lists the names that it accepts. With no page_status:, the page has no status.

create does a check of every file first, and converts each one. If any file
would fail, create makes no page. A page_id that names no page is also a
failure. create does not make a second copy and overwrite an id that it cannot
explain. To make a new page, remove the page_id or correct it.

When every file passes, create reserves an empty page for each file, parents
first, before it publishes any of them. Thus a link between two files in the
same batch resolves in either direction, also when the two files link to each
other. create refuses a parent cycle among the files.

If a run stops after the reserve step, it leaves an empty page version, and not
no page. Unless you gave --no-persist, create already wrote every id back, so
a plain update completes the job.

To create a whole tree in one run, give each child a parent: that names the .md
file of its parent. create makes the parents first, and fills in the real ids.

create records the title, space, parent, page_id, and page_width of each new
page, unless you give --no-persist. It writes them to the frontmatter of the
file, or to its pages: entry in markfluence.yaml when the metadata of the file
is there. In a project that uses pages:, a file with no frontmatter gets an
entry, so the Markdown does not change. A file that has frontmatter keeps it.
A write to markfluence.yaml changes that shared file one time for each new
page.

--dry-run does the same checks as a real run. Thus it fails on the same
problems, and one file that cannot publish stops the preview of the whole batch.
To check files separately, use check.

```
markfluence create FILE... [flags]
```

### Examples

```
  # Create one page in a space
  markfluence create docs/new_page.md --space ENG

  # Create it under a parent page or folder that exists
  markfluence create docs/child.md --space ENG --parent 123456

  # Create a whole tree, with the parent: path of each file
  markfluence create docs/*.md --space ENG

  # Override the title and the width for one file
  markfluence create note.md --space ENG --title "Ad-hoc note" --page-width wide

  # Create the page, and do not write the page_id back into the file
  markfluence create note.md --space ENG --no-persist

  # Show what would happen, and write nothing
  markfluence create docs/*.md --space ENG --dry-run
```

### Options

```
      --dry-run             Show what create would do, and write nothing to Confluence or to files.
  -h, --help                help for create
      --no-persist          Record no metadata. Do not change the file or markfluence.yaml.
      --page-width string   Page width: narrow, wide, or max. Overrides the frontmatter.
      --parent string       Id of the parent page or folder for the new pages.
      --space string        Key of the target space.
      --title string        Page title. Overrides the frontmatter. Works with one FILE only.
```

### Options inherited from parent commands

```
      --cloud-id string   Atlassian cloud ID. Set it only for a scoped API token. If not set, markfluence uses $CONFLUENCE_CLOUD_ID, then .env
  -d, --debug             Print debug details, such as each retry decision
      --env-file string   Env file to read credentials from. The default is .env in the documentation root of the working directory, or in the working directory if there is no markfluence.yaml
      --json              Write JSON, and no human output. A result goes to stdout. A fatal error goes to stderr as a JSON error object
      --no-color          Print output with no color
      --root string       Documentation root for every file. The default is the nearest directory above each file that has a markfluence.yaml, or the directory of the file if there is none
      --url string        Confluence site URL. If not set, markfluence uses $CONFLUENCE_URL, then .env
      --username string   Confluence username (your email address). If not set, markfluence uses $CONFLUENCE_USERNAME, then .env
```

### SEE ALSO

* [markfluence](markfluence.md)	 - Publish Markdown to Confluence

