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

--spaces additionally surveys every space **the credentials you are
running as** can see, and reports where they may create pages and which
they administer. It describes the authenticated account and nothing
else, so it cannot be combined with an ACCOUNT_ID: Confluence has no
route that answers "where may this other person publish". That is a
walk of the space directory rather than one request, which is why it is
opt-in.

'write access' means creating pages in a space: permission to edit an
existing page is not a space grant at all, so a space listed here may
still refuse a particular page.

Read-only. Nothing is written to Confluence or to disk.

```
markfluence user-info [ACCOUNT_ID] [flags]
```

### Examples

```
  # Who am I, and can this token do anything?
  markfluence user-info

  # Who is this account id on an old page?
  markfluence user-info 60c36d0718e9f60071326951

  # Where can these credentials publish?
  markfluence user-info --spaces
```

### Options

```
  -h, --help     help for user-info
      --spaces   Also survey which spaces the account may create pages in and administer.
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

