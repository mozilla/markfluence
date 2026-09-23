## markfluence credentials-init

Write your credentials file, after checking the credentials

### Synopsis

Ask for your Confluence site URL, your username, and your API token, check
them with Confluence, and write them to your credentials file,
~/.config/markfluence/credentials ($XDG_CONFIG_HOME/markfluence/credentials if
you set XDG_CONFIG_HOME to an absolute path). The token is read without
echo, so it does not show on the screen or go into your shell history.

For a site at atlassian.net, credentials-init also gets the cloud ID of the
site and saves it. A scoped API token needs the cloud ID, and a normal token
works with it too, so you do not have to know which kind of token you have.

Before it saves, credentials-init sends the request that markfluence
user-info sends, and shows the account that the credentials belong to. If
Confluence refuses the credentials, it saves nothing. If it cannot tell, for
example because it cannot reach the site, it asks you whether to save.

If you already have a credentials file, each question shows the current
value, and Enter keeps it. To change only the token, press Enter twice and
paste the new token. credentials-init writes the whole file again, so it
tells you first if your file has comments or other lines that it will not
keep. The file gets mode 0600.

credentials-init only writes the credentials file. It writes nothing in the
current directory.

credentials-init requires a terminal to run. When using markfluence in CI,
set the CONFLUENCE_* environment variables from the secrets of the CI system
instead.

```
markfluence credentials-init [flags]
```

### Examples

```
  # Set up this computer
  markfluence credentials-init

  # Then make sure that it works
  markfluence user-info
```

### Options

```
  -h, --help   help for credentials-init
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

