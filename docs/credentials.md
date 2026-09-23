# Credentials

markfluence needs a Confluence site URL, a username, and an API token. For a
scoped API token, it also needs a cloud ID.

| Setting | Name |
| --- | --- |
| Site URL | `CONFLUENCE_URL` |
| Username (your email address) | `CONFLUENCE_USERNAME` |
| API token | `CONFLUENCE_TOKEN` |
| Cloud ID, optional | `CONFLUENCE_CLOUD_ID` |

There is no command-line flag for any of these settings, so your API token
cannot get into your shell history.

## Set up your credentials file

Do this one time on each computer.

1. Make the directory:

   ```
   mkdir -p ~/.config/markfluence
   ```

2. Create `~/.config/markfluence/credentials` with these lines:

   ```
   CONFLUENCE_URL=https://your-org.atlassian.net
   CONFLUENCE_USERNAME=you@example.com
   CONFLUENCE_TOKEN=your-api-token
   # Optional. Set this only for a *scoped* API token.
   # CONFLUENCE_CLOUD_ID=
   ```

3. Restrict the file, because it holds your API token:

   ```
   chmod 600 ~/.config/markfluence/credentials
   ```

4. Make sure that it works. This command shows the account that the
   credentials belong to:

   ```
   markfluence user-info
   ```

To get an API token, see Atlassian's
[Manage API tokens for your Atlassian account](https://support.atlassian.com/atlassian-account/docs/manage-api-tokens-for-your-atlassian-account/).
For a scoped token and the cloud ID, see
[Scoped tokens and service accounts](../README.md#scoped-tokens-and-service-accounts).

## Where markfluence looks

markfluence reads each setting from these places, and uses the first one that
has it:

1. the file that you name with `--env-file PATH`
2. the environment variable
3. your credentials file

The credentials file is `~/.config/markfluence/credentials` on Linux and on
macOS. If you set `XDG_CONFIG_HOME` to an absolute path, the file is
`$XDG_CONFIG_HOME/markfluence/credentials`.

An empty value counts as not set. Thus `CONFLUENCE_TOKEN=` in a file lets a
place lower in the list give the token.

markfluence does not look for credentials in the working directory or in a
project. A repository that you clone cannot give markfluence a URL to send your
token to.

A file for `--env-file` has the same format as the credentials file: one
`KEY=value` on each line. Blank lines and lines that start with `#` are
ignored. You can put `export ` before a key, and quotes around a value.
markfluence does not expand variables in a value.

## Two rules that protect your token

**The URL and the token must come from the same place.** Each setting comes
from the first place that has it, so without this rule the URL could come from
one place and the token from another. markfluence would then send your token
to a site that you did not pair it with. If the two come from different
places, markfluence stops and tells you where each one came from:

```
✗ CONFLUENCE_URL comes from the environment, but CONFLUENCE_TOKEN comes from ~/.config/markfluence/credentials. Set both in the same place. See https://github.com/mozilla/markfluence/blob/main/docs/credentials.md
```

To correct it, set the URL and the token together in one place.

**markfluence reads the cloud ID only from the place that gives the URL.** A
cloud ID names one site, as the URL does. markfluence ignores a cloud ID that
comes from a different place. It does not stop, because there is no way to
say "no cloud ID" in a place higher in the list: an empty value counts as not
set.

If you set a cloud ID in a place higher in the list than the URL, markfluence
gives a warning that it ignored it. For example, the URL is in your
credentials file and you export `CONFLUENCE_CLOUD_ID`. Without the cloud ID,
a scoped token gets a 401 from your site, so put the cloud ID in the same
place as the URL. A cloud ID lower in the list than the URL gets no warning:
that is the usual case of a credentials file for one site while `--env-file`
names another.

The username can come from any place.

## Use a different site for one command

Put the settings of the other site in a file, and name the file with
`--env-file`:

```
markfluence --env-file ~/other-site.env read 123
```

You can also set the URL and the token together in the environment:

```
CONFLUENCE_URL=https://other.atlassian.net CONFLUENCE_TOKEN=... markfluence read 123
```

If you set only `CONFLUENCE_URL`, markfluence stops, because the token then
comes from a different place.

For different credentials in each project, name a file with `--env-file`, or
use a tool such as [direnv](https://direnv.net/), which sets environment
variables when you enter a directory. If you keep such a file in a
repository, make sure that git ignores it.

## CI

In CI, set the four settings as environment variables from the secrets of the
CI system. The
[markfluence GitHub Action](https://github.com/mozilla/markfluence-action)
works this way.

## The permission warning

markfluence gives a warning when two conditions are both true:

- The mode of the credentials file, or of the file that `--env-file` names,
  gives a permission to a person who is not you: read, write, or execute.
- The file holds `CONFLUENCE_TOKEN`.

The warning gives the name of the file, the fault in its mode, and the `chmod`
command that corrects it. markfluence gives the warning only for a regular file
that it reads. It gives no warning for a pipe, such as
`--env-file <(pass show confluence)`. It does not read the credentials file
when the other places give every setting.

If you run markfluence with `--json`, markfluence does not print the warning.
It puts the warning in the `warnings` array of the output document, and in the
error object on stderr. stderr is a JSON document in that mode.

## Errors

**`missing Confluence …`**: no place gives that setting. The error lists the
places that markfluence looked in. Set the setting there. If more than one
setting is missing, set them in one place, because of the rule above. If only
one of the URL and the token is set, the error names its place, because the
other must go there too.

**`… comes from …, but … comes from …`**: the URL and the token come from
different places. See [Two rules that protect your token](#two-rules-that-protect-your-token).

**`reading credentials file …`**: the credentials file exists, but markfluence
cannot read it. For example, it is a directory, it is a symbolic link to a file
that does not exist, or you do not have permission to read it. markfluence
stops, and does not ignore the file, because then a new token in that file
would appear to have no effect.

**`reading env file …`**: markfluence cannot read the file that `--env-file`
names.

**`invalid Confluence cloud ID …`**: the cloud ID looks like a URL or a path.
Give only the identifier, such as `d8febd08-5555-5555-5555-db37c2369ce5`.
