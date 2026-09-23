## markfluence

Publish markdown to Confluence

### Synopsis

markfluence publishes Markdown files to Confluence pages, and gets pages back as
Markdown.

It needs a site URL, a username, and an API token. It reads each one from a flag
first, then from an environment variable, then from a .env file:

  site URL   --url        CONFLUENCE_URL
  username   --username   CONFLUENCE_USERNAME
  API token  (no flag)    CONFLUENCE_TOKEN
  cloud ID   --cloud-id   CONFLUENCE_CLOUD_ID

The API token is never a flag, so it cannot get into your shell history.

Set the cloud ID only for a scoped API token, such as the token of a service
account. Confluence refuses a scoped token at your site URL, so markfluence
sends it through the api.atlassian.com gateway, which needs the cloud ID. Do
not set it for a personal token. To find your cloud ID, open
https://YOUR-SITE.atlassian.net/_edge/tenant_info. The cloud ID is not a secret.

```
markfluence [flags]
```

### Options

```
      --cloud-id string   Atlassian cloud ID. Set it only for a scoped API token. If not set, markfluence uses $CONFLUENCE_CLOUD_ID, then .env
  -d, --debug             Print debug output, such as each request and each retry
      --env-file string   Env file to read credentials from. The default is .env in the documentation root of the working directory, or in the working directory if there is no markfluence.yaml
  -h, --help              help for markfluence
      --json              Write one JSON document to stdout, and no human output
      --no-color          Print output with no color
      --root string       Documentation root for every file. The default is the nearest directory above each file that has a markfluence.yaml, or the directory of the file if there is none
      --url string        Confluence site URL. If not set, markfluence uses $CONFLUENCE_URL, then .env
      --username string   Confluence username (your email address). If not set, markfluence uses $CONFLUENCE_USERNAME, then .env
```

### SEE ALSO

* [markfluence attachment-download](markfluence_attachment-download.md)	 - Download the attachments of a Confluence page
* [markfluence attachment-list](markfluence_attachment-list.md)	 - List a Confluence page's attachments
* [markfluence attachment-upload](markfluence_attachment-upload.md)	 - Upload or replace attachments on a Confluence page
* [markfluence check](markfluence_check.md)	 - Validate markdown files against the converter and frontmatter rules, offline
* [markfluence children](markfluence_children.md)	 - List the pages and folders under a Confluence page, folder, or space
* [markfluence create](markfluence_create.md)	 - Create new Confluence pages from markdown files
* [markfluence diff](markfluence_diff.md)	 - Show what differs between a page and its local markdown file
* [markfluence export](markfluence_export.md)	 - Write a Confluence page and its attachments to a directory
* [markfluence find](markfluence_find.md)	 - Find Confluence pages and folders by exact title
* [markfluence page-info](markfluence_page-info.md)	 - Print metadata about a Confluence page
* [markfluence read](markfluence_read.md)	 - Fetch a Confluence page and print its body
* [markfluence schema](markfluence_schema.md)	 - Print the JSON Schema for --json output
* [markfluence search](markfluence_search.md)	 - Find Confluence pages by full-text search
* [markfluence space-info](markfluence_space-info.md)	 - Print metadata about a Confluence space
* [markfluence update](markfluence_update.md)	 - Publish one or more markdown files to Confluence pages
* [markfluence user-find](markfluence_user-find.md)	 - Find a Confluence user's account id and mention markdown
* [markfluence user-info](markfluence_user-info.md)	 - Print who the credentials belong to, or who an account id names

