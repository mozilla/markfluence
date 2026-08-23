# Security Policy

## Reporting a vulnerability

**Please don't open a public issue for a security problem.**

Report it privately through GitHub:
[**Report a vulnerability**](https://github.com/mozilla/markfluence/security/advisories/new).
That opens a draft advisory only you and the maintainers can see.

Helpful things to include:

- the command you ran, and the version (`markfluence --version`)
- whether the site is Confluence Cloud or Data Center
- the smallest reproduction you can manage

> [!IMPORTANT]
> **Don't put a real API token, or a token fragment, in a report.** If you think
> a token was exposed while you were investigating, revoke it first — an
> Atlassian API token can't be rotated in place, so recovery means issuing a new
> one. A redacted site URL is fine (use example.com); say so if you've redacted it.

Reports are handled on a best-effort basis by a small number of maintainers.
There's no guaranteed response time. Once there's a fix, disclosure is
coordinated through a GitHub Security Advisory.

## Supported versions

markfluence has not had a release yet. Fixes land on `main`, which is the only
supported version until 1.0.0. This section will get a version table when there's
something to put in it.

## What's in scope

markfluence is an authenticated CLI: it holds a Confluence API token, reads local
files to publish, and writes local files from what a Confluence site returns.
That gives it three security boundaries worth attacking.

**Leaking the API token.** The token is read only from `CONFLUENCE_TOKEN` or a
`.env` file, and is deliberately never accepted as a command-line flag, so it
can't end up in shell history, `ps` output, or a CI job log. Anything that gets
it out anyway is a vulnerability: appearing in `--debug` output or an error
message, being written into a published page, or being sent to any host that
isn't your Confluence site or Atlassian's API gateway. A standing example of
the last one: attachment downloads follow redirects to Atlassian's media host,
which must never receive the site credentials.

**Writing files outside the destination.** `export` and `attachment-download`
turn server-supplied attachment metadata into local filesystem paths, clamped to
the destination directory. A path that escapes that clamp is an arbitrary file
write driven by a Confluence page you may not control.

**Reading files outside the documentation root.** When publishing, image paths
resolve relative to the markdown file and are bounded by the working directory.
A path that escapes that bound would publish a local file the author never meant
to expose.

Beyond those: anything that lets a Confluence page's content cause markfluence to
execute code, or that lets one page's content corrupt or overwrite an unrelated
page.

## What's not in scope

- **Anything that requires valid credentials to reach.** markfluence acts as the
  user whose token it's given, on purpose. "The tool can edit pages my token can
  edit" is the design.
- **Raw storage markup passing through to a page.** Pasting `<ac:…>` / `<ri:…>`
  markup into markdown and having it published verbatim is a documented feature.
  Publishing your own macro to your own page isn't injection.
- **Confluence's own permission, sharing, or authentication behavior.** Report
  that to Atlassian.
- **A CVE in a dependency with no reachable path through markfluence.**
  Dependabot already opens those weekly; a normal issue or PR is the right
  channel. If you can show a way to actually reach it, that's a vulnerability —
  please report it privately.
- **The permissions on your own `.env` file**, or credentials committed to your
  own repository.
