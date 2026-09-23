## markfluence attachment-download

Download the attachments of a Confluence page

### Synopsis

Download the attachments of a Confluence page to your machine.

PAGE is a page id, a Confluence page URL, or a Markdown file that names a
page_id in its frontmatter or in its pages: entry. Each NAME is an attachment
name, as attachment-list shows it. With no NAME, markfluence downloads every
attachment.

markfluence records the source path of each file that it publishes or uploads.
It writes such an attachment back to that path under --dest. Thus the
downloaded files are where the Markdown of the page expects them, and a local
preview works.

An attachment with no recorded path was uploaded by hand, or markfluence
published it before it recorded paths. markfluence writes it into a directory
named after the page title. An attachment name is unique on a
page, but not in a space, so two pages can each have a diagram.png. read and
export also point to this directory.

--flat writes every attachment directly into --dest, with its stored name.

markfluence refuses an attachment whose recorded path would go outside --dest.
The path comes from the attachment comment, and anybody who can edit the page
can change that comment.

markfluence skips a file that already exists, unless you give --force.

```
markfluence attachment-download PAGE [NAME...] [flags]
```

### Examples

```
  # Download every attachment to the paths it was published from
  markfluence attachment-download 1234567890 --dest ./out

  # Download one attachment, by its stored name
  markfluence attachment-download 1234567890 diagram.png --dest ./out

  # Ignore the recorded paths, and write everything into one directory
  markfluence attachment-download 1234567890 --dest ./out --flat

```

### Options

```
      --dest string   Directory to write the attachments into. (default ".")
      --dry-run       Show what attachment-download would write, and write no files.
      --flat          Write every attachment into --dest with its stored name, and ignore recorded paths.
      --force         Overwrite a file that already exists.
  -h, --help          help for attachment-download
```

### Options inherited from parent commands

```
  -d, --debug             Print debug details, such as each retry decision
      --env-file string   File to read credentials from, before the environment and your credentials file
      --json              Write JSON, and no human output. A result goes to stdout. A fatal error goes to stderr as a JSON error object
      --no-color          Print output with no color
      --root string       Documentation root for every file. The default is the nearest directory above each file that has a markfluence.yaml, or the directory of the file if there is none
```

### SEE ALSO

* [markfluence](markfluence.md)	 - Publish Markdown to Confluence

