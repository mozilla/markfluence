## markfluence

Publish Markdown to Confluence

### Synopsis

markfluence publishes and manipulates Confluence pages from Markdown files.

It needs a site URL, a username, and an API token, and for a scoped token a
cloud ID: CONFLUENCE_URL, CONFLUENCE_USERNAME, CONFLUENCE_TOKEN, and
CONFLUENCE_CLOUD_ID. markfluence reads each one from the first of these places
that has it:

  1. the file that --env-file names
  2. the environment variable
  3. your credentials file, ~/.config/markfluence/credentials

The URL and the token must come from the same place, the cloud ID is read only
from the place that gives the URL, and there is no flag for any of these. To
set up the credentials file, see
https://github.com/mozilla/markfluence/blob/main/docs/credentials.md

```
markfluence [flags]
```

### Options

```
  -d, --debug             Print debug details, such as each retry decision
      --env-file string   File to read credentials from, before the environment and your credentials file
  -h, --help              help for markfluence
      --json              Write JSON, and no human output. A result goes to stdout. A fatal error goes to stderr as a JSON error object
      --no-color          Print output with no color
      --root string       Documentation root for every file. The default is the nearest directory above each file that has a markfluence.yaml, or the directory of the file if there is none
```

### SEE ALSO

* [markfluence attachment-download](markfluence_attachment-download.md)	 - Download the attachments of a Confluence page
* [markfluence attachment-list](markfluence_attachment-list.md)	 - List the attachments of a Confluence page
* [markfluence attachment-upload](markfluence_attachment-upload.md)	 - Upload or replace attachments on a Confluence page
* [markfluence check](markfluence_check.md)	 - Check Markdown files for problems, with no network access
* [markfluence children](markfluence_children.md)	 - List the pages and folders under a Confluence page, folder, or space
* [markfluence create](markfluence_create.md)	 - Create new Confluence pages from Markdown files
* [markfluence diff](markfluence_diff.md)	 - Show what is different between a page and its local Markdown file
* [markfluence export](markfluence_export.md)	 - Write a Confluence page and its attachments to a directory
* [markfluence find](markfluence_find.md)	 - Find Confluence pages and folders by their exact title
* [markfluence page-info](markfluence_page-info.md)	 - Show the metadata of a Confluence page
* [markfluence read](markfluence_read.md)	 - Get a Confluence page and print its body
* [markfluence schema](markfluence_schema.md)	 - Print the JSON Schema of the --json output
* [markfluence search](markfluence_search.md)	 - Find Confluence pages by a search of their text
* [markfluence space-info](markfluence_space-info.md)	 - Show the metadata of a Confluence space
* [markfluence update](markfluence_update.md)	 - Publish one or more Markdown files to Confluence pages
* [markfluence user-find](markfluence_user-find.md)	 - Find a user, and the Markdown that mentions them
* [markfluence user-info](markfluence_user-info.md)	 - Show who the credentials belong to, or who an account id names

