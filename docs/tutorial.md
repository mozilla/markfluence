# Tutorial

This tutorial takes you from no markfluence to a page that you published to
your personal space and exported back to a file.

We'll be using a Mac with [Homebrew](https://brew.sh/). For Linux, or to
build from source, see [Install](../README.md#install) in the README.

You'll need an [Atlassian API token][token] for your Confluence account on a Confluence Cloud site,
such as `your-org.atlassian.net`.

[token]: https://id.atlassian.com/manage-profile/security/api-tokens

The output in this tutorial is what markfluence prints. The ids, the names, and
the site are examples. Yours will be different.

## 1. Install markfluence

This repository is its own Homebrew tap, so `brew tap` needs the URL:

```sh
brew tap mozilla/markfluence https://github.com/mozilla/markfluence
brew trust mozilla/markfluence
brew install markfluence
```

Make sure that it works:

```sh
markfluence --version
```

## 2. Set up your credentials

markfluence needs your site URL, your username (the email address of your
Atlassian account), and your API token. `credentials-init` asks for each one,
does a check of them with Confluence, and writes them to
`~/.config/markfluence/credentials`:

```sh
markfluence credentials-init
```

When you're tying your API token, it'll be hidden.

Once you're done filling in information, markfluence will test your credentials
and let you know if it works. If Confluence refuses the credentials, then
markfluence will save nothing and you can run it again.

If you ever have to rotate your API token, you can run this command again
and markfluence will prompt you with your existing answers so you don't
have to type the ones that aren't changing again.

Now you can see your account information:

```console
$ markfluence user-info
account id:     5b10ac8d82e05b22cc7d4ef5  (these credentials)
name:           Ada Lovelace
email:          ada@example.com
type:           atlassian (a person)
status:         active
external:       no
guest:          no
personal space: ~5b10ac8d82e05b22cc7d4ef5  https://your-org.atlassian.net/wiki/spaces/~5b10ac8d82e05b22cc7d4ef5
visible spaces: 533
write access:   102 (too many to list; see --json output)
admin access:   2 -- ENG, ~5b10ac8d82e05b22cc7d4ef5
```

This command takes a few seconds, because it does a survey of every space that
you can see.

The line to note is `personal space`. Its key starts with `~`, and you use it in
the next steps. If there is no `personal space` line, your account has no
personal space. Create one in Confluence, or use the key of a space where you
have write access.

[docs/credentials.md](credentials.md) tells you more about credentials, and
what each credentials error means.

## 3. Look at your personal space

`space-info` shows what a space is and what you can do in it. Put the key in
quotes, because some shells expand a `~` at the start of a word:

```console
$ markfluence space-info '~5b10ac8d82e05b22cc7d4ef5'
key:           ~5b10ac8d82e05b22cc7d4ef5
name:          Ada Lovelace
id:            76646426
type:          personal (current)
homepage:      76646878  Things
labels:        favourite
your access:   read, create pages, administer
page statuses: Rough draft, In progress, Ready for review, Verified
pages:         40 current, 1 archived
root pages:    1
last activity: 2026-09-28 (9 days ago)
pages created: 0 in the last 7 days
pages touched: 0 in the last 7 days
```

`your access` must include `create pages`, or the next step fails.

## 4. Write a Markdown file

Each Markdown file is one Confluence page. The frontmatter at the top of the
file tells markfluence the title of the page, the space to put it in, and other
settings, such as the width of the page.

Make a directory for the tutorial, and write `hello.md` in it:

```sh
mkdir markfluence-tutorial
cd markfluence-tutorial
```

```markdown
---
title: Hello from markfluence
space: ~5b10ac8d82e05b22cc7d4ef5
page_width: medium
---

This page started as a Markdown file.

## A list

- one
- two

> [!NOTE]
> This is a callout. In Confluence it is an info panel.
```

Use your own personal space key in the `space:` line.

`markfluence check` finds problems in a file before you publish it. It makes no
network request and needs no credentials. Let's run it on our file:

```console
$ markfluence check hello.md
  ✗ [hello.md] invalid page_width "medium"; expected narrow, wide, or max
    root: /Users/ada/markfluence-tutorial
  ✗ 1 of 1 file(s) failed.
```

Oops! There's an error with the file. 

`check` exits with `1`, and the message tells you the values that are correct.
Change the `page_width:` line in `hello.md` to:

```yaml
page_width: wide
```

Then run `check` again:

```console
$ markfluence check hello.md
    [hello.md] clean
    root: /Users/ada/markfluence-tutorial
```

`check` also finds broken links, images that do not exist, and frontmatter that
does not parse. A message about the body gives the line number in the file.

[docs/markdown-file.md](markdown-file.md) tells you about every frontmatter
field and every Markdown construct.

## 5. Create the page

`markfluence create` creates the page in Confluence. To see what `create` will
do, and change nothing, use `--dry-run`:

```console
$ markfluence create --dry-run hello.md
  ! DRY RUN — no changes will be written.
    root: /Users/ada/markfluence-tutorial
    [hello.md] page width: wide
  ✓ [hello.md] Would create page 'Hello from markfluence' in ~5b10ac8d82e05b22cc7d4ef5
```

That looks good, so let's create the page:

```console
$ markfluence create hello.md
    root: /Users/ada/markfluence-tutorial
    [hello.md] page width: wide
  ✓ [hello.md] Created page 1234567890: https://your-org.atlassian.net/wiki/spaces/~5b10ac8d82e05b22cc7d4ef5/pages/1234567890
```

Open the URL to see the page in Confluence.

`create` also updates the information in the frontmatter of `hello.md`:

```markdown
---
title: Hello from markfluence
space: ~5b10ac8d82e05b22cc7d4ef5
parent: null
page_id: 1234567890
page_width: wide
---
```

The `page_id` connects the file to the page allowing you to update it as you
make changes. `parent: null` means that the page is at the top level of the
space. To publish changes made to the file, use `update`.

```sh
markfluence update hello.md
```

## 6. Show the pages in your personal space

`markfluence children --space` lists the pages and folders in a space. The new
page is at the top level, next to the homepage:

```console
$ markfluence children --space '~5b10ac8d82e05b22cc7d4ef5'
TYPE  ID          TITLE
page  76646878    Things
page  1234567890  Hello from markfluence

    Showing the space's top level. Use --depth 2, or --depth all for the whole tree.
```

To see the whole tree, add `--depth all`.

## 7. Export the page

`markfluence export` exports a Confluence page to a Markdown file on your file
system, with its frontmatter and the attachments that it uses. You can give the
page as an id, as a URL, or as a Markdown file that has a `page_id`:

```console
$ markfluence export hello.md --dest exported
  ✓ wrote      /Users/ada/markfluence-tutorial/exported/hello-from-markfluence.md
```

The exported file has the same frontmatter as `hello.md`, and the body is the
page converted back to Markdown. It is semantically the same as `hello.md`, but
it is not always byte-for-byte the same.

To compare your file with the page in Confluence, use `diff`. It exits with
`0` when they are the same:

```sh
markfluence diff hello.md
```

## Next steps

- `markfluence COMMAND --help` is the reference for each command. The same
  text is in [docs/commands/](commands/).
- The [Common workflows](../README.md#common-workflows) section of the README
  shows how to edit a page that exists and how to export a tree of pages.
- [docs/root-model.md](root-model.md) tells you how to keep many files in one
  project with a `markfluence.yaml`.
- To publish from a GitHub repository, use
  [markfluence-action](https://github.com/mozilla/markfluence-action).
- To remove the tutorial page, delete it in Confluence. markfluence does not
  delete pages.
