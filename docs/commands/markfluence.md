## markfluence

Publish Markdown to Confluence

### Synopsis

markfluence publishes and manipulates Confluence pages from Markdown files.

It needs a site URL, a username, and an API token, and for a scoped token a
cloud ID:

  CONFLUENCE_URL        the site, such as https://YOUR-SITE.atlassian.net
  CONFLUENCE_USERNAME   your email address
  CONFLUENCE_TOKEN      your API token
  CONFLUENCE_CLOUD_ID   optional; only for a scoped API token

markfluence reads each one from these places, and uses the first it finds:

  1. the file that --env-file names
  2. the environment variable
  3. your credentials file, ~/.config/markfluence/credentials
     ($XDG_CONFIG_HOME/markfluence/credentials if XDG_CONFIG_HOME is an
     absolute path)

The two files hold KEY=value lines. Put the URL and the token in the same
place: markfluence refuses to send a token to a URL from a different place.
It reads the cloud ID only from the place that gives the URL.

There is no flag for any of these, so the token cannot get into your shell
history. To use a different site for one command, name a file with
--env-file.

Set the cloud ID only for a scoped API token, such as the token of a service
account. Confluence refuses a scoped token at your site URL, so markfluence
sends it through the api.atlassian.com gateway, which needs the cloud ID. To
find your cloud ID, open https://YOUR-SITE.atlassian.net/_edge/tenant_info .
The cloud ID is not a secret.

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

