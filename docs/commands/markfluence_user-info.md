## markfluence user-info

Print who the credentials belong to, or who an account id names

### Synopsis

With no argument, print the account the configured credentials belong
to. This is the question to ask first when a publish lands somewhere
unexpected, a token is refused, or a page's history names an account you
do not recognise: markfluence otherwise cannot tell you who it is.

With an ACCOUNT_ID, print that account instead. An id, never a name --
resolving a name is markfluence user-find. The two see different things,
which is why both exist: user-find searches a directory that cannot see
deactivated accounts at all, while this route resolves one, so an id from
an old page names its person here and nowhere else.

'type' tells a person (atlassian) from a service account (app), which is
most of why permissions surprise people, and 'external'/'guest' name a
restricted account directly.

With no argument it also surveys every space those credentials can see,
reporting where they may create pages and which they administer. That
survey is a walk of the space directory rather than one request, so the
no-argument form takes a few seconds.

It is absent from the ACCOUNT_ID form, and not by omission: the route
answers for the authenticated account and takes no account id, so
reporting it beside somebody else's name would attribute your access to
them. Asking where another person may publish means reading every
space's permission grants and resolving them against their group
memberships -- over a thousand requests, and a local reimplementation of
Confluence's permission rules.

'write access' means creating pages in a space: permission to edit an
existing page is not a space grant at all, so a space listed here may
still refuse a particular page.

Read-only. Nothing is written to Confluence or to disk.

```
markfluence user-info [ACCOUNT_ID] [flags]
```

### Examples

```
  # Who am I, and where can these credentials publish?
  markfluence user-info

  # Who is this account id on an old page?
  markfluence user-info 60c36d0718e9f60071326951

  # Just the spaces, as data
  markfluence user-info --json | jq '.results[0].spaces'
```

### Options

```
  -h, --help   help for user-info
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

