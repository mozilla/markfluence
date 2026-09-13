# Using with GitHub Actions

Run markfluence in CI to keep Confluence pages in sync with the markdown in
your repo: on a push to your default branch, publish the docs that changed.

Configuration in general — including how credentials resolve and what a scoped
token needs — is in the [README](../README.md#configure).

You will need to know the Confluence `page_id` for each page you want to
update.

## Credentials

Store environment variables as [encrypted secret][secrets] (never commit them).
markfluence reads them straight from the environment — no `.env` in CI.

- `CONFLUENCE_TOKEN`
- `CONFLUENCE_URL`
- `CONFLUENCE_USERNAME`

Prefer a [service account][svcacct] over a personal token here, so published pages
aren't authored by an individual and publishing doesn't break when that person
rotates their token or moves on. That means a **scoped** token, which also needs
`CONFLUENCE_CLOUD_ID` (see [Scoped tokens and service
accounts](../README.md#scoped-tokens-and-service-accounts)). The cloud ID is not sensitive, so
make it a repository **variable** rather than a secret.

[secrets]: https://docs.github.com/en/actions/security-guides/using-secrets-in-github-actions

## The source of truth is external to Confluence

A CI workflow only makes sense when **the repository is the source of truth**
and the Confluence page is a published copy of it. If someone makes changes in
the Confluence UI, they will get stomped on when the CI workflow pushes a new
change.

### Use `--force`

```yaml
        run: markfluence update --force docs/**/*.md
```

`update --force` prevents updates from failing in CI because someone
inadvertently edited the page in the Confluence UI. All edits are in the
Confluence history, so they can be recovered and applied to the repository
correctly.

### Say so on the page

Since UI edits are going to be overwritten, the page should tell readers where
they can make edits. Put a callout at the top of the markdown — markfluence
converts a GitHub alert into a Confluence panel, so it renders as one:

```markdown
> [!NOTE]
> This page is published from [docs/deploy-runbook.md](https://github.com/ORG/REPO/blob/main/docs/deploy-runbook.md).
> Edits made here are overwritten on the next push. Open a pull request instead.
```

`NOTE`, `TIP`, `IMPORTANT`, `WARNING` and `CAUTION` are all supported and keep
GitHub's colours. Linking the source file gives a reader somewhere to go, which
is what turns "do not edit" into something actionable.

Consider restricting page permissions to the publishing account as well, if the
space allows it. A banner is a convention; permissions are a mechanism.

### If Confluence is the source of truth, do not run this workflow

If Confluence is the source of truth, you shouldn't be using a workflow to
update Confluence. markfluence has no way to discover changes that have been
made in the Confluence UI and has no mechanism for reconciling them.

## Workflow

```yaml
name: Publish docs to Confluence

on:
  push:
    branches: [main]
    paths: ['docs/**.md']        # only when docs change

# Avoid overlapping publishes racing on the same pages.
concurrency:
  group: confluence-publish
  cancel-in-progress: false

jobs:
  publish:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: '1.25'

      # No release binaries are published yet, so install from source. Pin a tag
      # (…@v1.2.3) once releases exist, rather than @latest, for reproducibility.
      - name: Install markfluence
        run: go install github.com/mozilla/markfluence@latest

      - name: Publish
        env:
          CONFLUENCE_URL: ${{ secrets.CONFLUENCE_URL }}
          CONFLUENCE_USERNAME: ${{ secrets.CONFLUENCE_USERNAME }}
          CONFLUENCE_TOKEN: ${{ secrets.CONFLUENCE_TOKEN }}
          # A variable, not a secret: the cloud ID is public. Omit it if you're
          # using an unscoped personal token.
          CONFLUENCE_CLOUD_ID: ${{ vars.CONFLUENCE_CLOUD_ID }}
        # --force because the repository is the source of truth here; see above.
        run: markfluence update --force docs/**/*.md
```

That step takes no per-file inputs, and that is the point: each file's page id
and title come from its own frontmatter or from a `pages:` entry in
`markfluence.yaml`, so adding a page is a repository change rather than a
workflow change. There are deliberately no `--page-id`/`--title`/`--page-width`
flags — they would each have to name a single file, which is what made
`docs/**/*.md` inexpressible before.

If your markdown must stay pristine — a README, or a docs tree with other
readers — put every page's metadata in `markfluence.yaml`:

```yaml
space: ENG

pages:
  docs/deploy-runbook.md:
    title: Deploy Runbook
    page_id: 12346
```

See [the project file](root-model.md#pages--page-metadata-for-a-pristine-file).

Notes:

- **Exit codes.** `update` exits non-zero if any file fails, so the job fails
  loudly. Add `--json` to get machine-readable per-file results on stdout (see
  [`--json` output](../README.md#--json-output)) if a later step needs to parse them.
- **A file nothing claims is skipped, not failed**, so a glob over a docs tree
  does not turn the job red when somebody adds a draft. `metadata_source` in
  `--json` says which location supplied each published page's metadata, which is
  what to look at when a page lands somewhere unexpected.
- **Creating pages stays a human act.** A workflow creating one would have to
  commit the new `page_id` back to the repository. Create locally, commit the
  entry, and let CI update from then on.

A reusable composite/Docker action wrapping this is tracked in
[#29](https://github.com/mozilla/markfluence/issues/29).
