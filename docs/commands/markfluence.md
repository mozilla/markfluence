## markfluence

Publish markdown to Confluence

### Synopsis

markfluence publishes and manipulates Confluence pages from markdown files.

Configuration resolves with the precedence flag > environment variable >
.env file. The site URL (--url / CONFLUENCE_URL), username (--username /
CONFLUENCE_USERNAME), and cloud ID (--cloud-id / CONFLUENCE_CLOUD_ID) may be
set any of those ways; the API token (CONFLUENCE_TOKEN) comes only from the
environment or .env, never a flag.

Set the cloud ID to authenticate with a scoped API token, such as one issued
to a service account: those tokens are rejected against the site domain and
must go through Atlassian's api.atlassian.com gateway. Leave it unset for an
unscoped personal token. Find yours at
https://YOUR-SITE.atlassian.net/_edge/tenant_info -- it isn't a secret.

```
markfluence [flags]
```

### Options

```
      --cloud-id string   Atlassian cloud ID; set to use a scoped API token via the api.atlassian.com gateway (falls back to $CONFLUENCE_CLOUD_ID, then .env)
  -d, --debug             Enable verbose debug output
      --env-file string   Path to an env file to read (default: .env at the discovered project root, or the working directory if none)
  -h, --help              help for markfluence
      --json              Emit machine-readable JSON to stdout instead of human output
      --no-color          Disable colored output
      --root string       Documentation root, overriding discovery (default: the directory holding markfluence.yaml, found by walking up from each file, or the file's own directory if none)
      --url string        Confluence base URL (falls back to $CONFLUENCE_URL, then .env)
      --username string   Confluence username/email (falls back to $CONFLUENCE_USERNAME, then .env)
```

### SEE ALSO

* [markfluence attachment-download](markfluence_attachment-download.md)	 - Download a Confluence page's attachments
* [markfluence attachment-list](markfluence_attachment-list.md)	 - List a Confluence page's attachments
* [markfluence attachment-upload](markfluence_attachment-upload.md)	 - Upload or replace attachments on a Confluence page
* [markfluence check](markfluence_check.md)	 - Validate markdown files against the converter and frontmatter rules, offline
* [markfluence children](markfluence_children.md)	 - List the pages and folders under a Confluence page, folder, or space
* [markfluence create](markfluence_create.md)	 - Create new Confluence pages from markdown files
* [markfluence export](markfluence_export.md)	 - Write a Confluence page and its attachments to a directory
* [markfluence find](markfluence_find.md)	 - Find Confluence pages and folders by exact title
* [markfluence fix](markfluence_fix.md)	 - Reconcile each markdown file's frontmatter to its live Confluence page
* [markfluence info](markfluence_info.md)	 - Print metadata about a Confluence page
* [markfluence read](markfluence_read.md)	 - Fetch a Confluence page and print its body
* [markfluence schema](markfluence_schema.md)	 - Print the JSON Schema for --json output
* [markfluence search](markfluence_search.md)	 - Find Confluence pages by full-text search
* [markfluence update](markfluence_update.md)	 - Publish one or more markdown files to Confluence pages

