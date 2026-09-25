// Package update implements the `markfluence update` command: publish Markdown
// files to existing Confluence pages.
package update

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mozilla/markfluence/internal/actionlog"
	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/completion"
	"github.com/mozilla/markfluence/internal/convert"
	"github.com/mozilla/markfluence/internal/frontmatter"
	"github.com/mozilla/markfluence/internal/jsonout"
	"github.com/mozilla/markfluence/internal/labels"
	"github.com/mozilla/markfluence/internal/linkindex"
	"github.com/mozilla/markfluence/internal/pagedoc"
	"github.com/mozilla/markfluence/internal/pagemeta"
	"github.com/mozilla/markfluence/internal/pageref"
	"github.com/mozilla/markfluence/internal/pagestatus"
	"github.com/mozilla/markfluence/internal/pagewidth"
	"github.com/mozilla/markfluence/internal/project"
	"github.com/mozilla/markfluence/internal/ui"
	"github.com/spf13/cobra"
)

var (
	message string
	force   bool
	dryRun  bool
)

// Cmd is the update command.
var Cmd = &cobra.Command{
	Use:   "update FILE...",
	Short: "Publish one or more Markdown files to Confluence pages",
	Long: "Publish one or more Markdown FILEs to their Confluence pages.\n\n" +
		"The page id and title of each file come from its YAML frontmatter, or from its\n" +
		"pages: entry in markfluence.yaml. With an entry, the file can have no\n" +
		"frontmatter at all. Both locations are legal, and update says nothing when they\n" +
		"agree. If they disagree about page_id, space, or parent, the file fails. If\n" +
		"they disagree about title, page_width, page_status, or labels, the frontmatter\n" +
		"wins, with a warning. With no title, update keeps the live title of the page.\n\n" +
		"update skips a file that neither location mentions, and does not fail it. A\n" +
		"repository can correctly hold Markdown that nobody publishes. Thus a glob over\n" +
		"a docs tree does not fail because somebody added a draft. A file that IS\n" +
		"registered but has no page id fails. Something claimed it, and nobody created\n" +
		"the page yet.\n\n" +
		"There are no flags for the metadata of one page. The metadata is in the file\n" +
		"or in its entry, and that is what lets one command publish 'docs/**/*.md'. A\n" +
		"flag would have to name one file. To give a whole tree one width, set\n" +
		"page_width: in markfluence.yaml.\n\n" +
		"update asserts the page width only when something declares it: the file, its\n" +
		"entry, or the default of the project. Otherwise it does not touch the live\n" +
		"width. Labels work in the same way. update asserts a labels: line exactly, and\n" +
		"removes a label on the page that the file does not list. With no labels: line,\n" +
		"update does not touch the labels, and does not even read them.\n\n" +
		"A page_status: line asserts the status of the page, which is the colored\n" +
		"lozenge next to its title. With no page_status: line, update does not touch the\n" +
		"status, and does not even read it. Confluence decides which statuses a page can\n" +
		"have, for each page and each account, so update asks the page that it publishes\n" +
		"to. A name that does not agree fails that file, and the error lists the names\n" +
		"that would work. page-info shows them too. A status write gives the page a new\n" +
		"version, so update does not send a status that already agrees.\n\n" +
		"update also puts the page where the file says. A parent: line moves the page\n" +
		"under that page or folder, and parent: null moves it to the top of its space.\n" +
		"With no parent: line, update does not move the page. A moved page goes last\n" +
		"among its new siblings. markfluence never changes the order of siblings, so\n" +
		"reorder them in Confluence. A move does not give the page a new version.\n\n" +
		"update does not move a page to a different space. If the file or its entry\n" +
		"declares a space, or markfluence.yaml has a space: default, and the page is in a\n" +
		"different space, update fails that file. Move the page in Confluence, or\n" +
		"correct the space.\n\n" +
		"update never writes to the file or to markfluence.yaml. Thus you can safely\n" +
		"correct a wrong page_id. A page_id that names no page fails that file, and the\n" +
		"error tells you what to do. A page_id that is not a number fails with no\n" +
		"request to Confluence.\n\n" +
		"Two checks protect the page. Both compare with what an earlier create, update,\n" +
		"or export recorded locally for that file, in the log next to markfluence.yaml:\n\n" +
		"  - If the page changed after you made your copy, update refuses the file, and\n" +
		"    does not overwrite the page. Export the page again, or use --force.\n" +
		"  - If the rendered body already agrees with the page, update skips the body.\n" +
		"    It still applies attachments, width, labels, and page status. Thus a new\n" +
		"    version of an image publishes, and the page gets no new version for the\n" +
		"    body. A status change still gives the page a new version.\n\n" +
		"With no markfluence.yaml, there is no log, so neither check runs. update then\n" +
		"publishes every file, and each publish makes a new page version.\n\n" +
		"update publishes a file with no record yet with no check and no warning of its\n" +
		"own. The run reports how many such files there were. Protection starts with the\n" +
		"first publish or export of a file.\n\n" +
		"--force means \"always publish\". It overrides both checks. A CI workflow needs\n" +
		"this when the repository is the source of truth.\n\n" +
		"update does each file separately. It exits with a code that is not zero if any\n" +
		"file failed, also a refused page.\n\n" +
		"--dry-run shows the new version, the attachment uploads, and any change to the\n" +
		"parent, the width, the labels, or the page status. It writes nothing to\n" +
		"Confluence. It does the same two checks as a real run, so its preview agrees\n" +
		"with the real run.",
	Example: "  # Publish a file. The page id comes from its frontmatter or its entry\n" +
		"  markfluence update docs/managing_an_incident.md\n\n" +
		"  # Publish a whole tree, as CI does. The metadata comes from the files\n" +
		"  # and from markfluence.yaml, so you give nothing for each file\n" +
		"  markfluence update docs/**/*.md\n\n" +
		"  # Publish a set of files with a version message\n" +
		"  markfluence update docs/*.md --message \"Bulk update\"\n\n" +
		"  # Publish, also if the page changed after you made your copy\n" +
		"  markfluence update docs/foo.md --force\n\n" +
		"  # Show what would happen, and write nothing\n" +
		"  markfluence update docs/*.md --dry-run\n\n" +
		"  # Show which location gave each file its metadata\n" +
		"  markfluence update docs/*.md --json | jq -r '.results[] | \"\\(.file) \\(.metadata_source)\"'",
	Args:              cobra.MinimumNArgs(1),
	ValidArgsFunction: completion.MarkdownFiles,
	RunE:              run,
}

func init() {
	Cmd.Flags().StringVar(&message, "message", "Updated via markfluence", "Message for the new page version.")
	Cmd.Flags().BoolVar(&force, "force", false,
		"Always publish. Overrides the check for a changed page and the check for an "+
			"unchanged body.")
	Cmd.Flags().BoolVar(&dryRun, "dry-run", false,
		"Show what update would publish, and write nothing to Confluence.")
}

func run(cmd *cobra.Command, args []string) error {
	envFile, _ := cmd.Flags().GetString("env-file")
	rootOverride, _ := cmd.Flags().GetString("root")
	roots := project.NewCache(rootOverride)
	defer roots.Close()
	c, err := client.Resolve(envFile)
	if err != nil {
		if ui.IsJSON() {
			_ = jsonout.EmitError(os.Stderr, "update", err.Error(), jsonout.CodeConfig)
		} else {
			ui.Error(err.Error())
		}
		return ui.SilentExit(2)
	}

	if dryRun {
		ui.Warn("DRY RUN — no changes will be written.")
	}
	indexes := linkindex.NewCache()
	// One log per root, read and appended through a cache for the same reason
	// the index is: a batch under one project must not re-read its own
	// publishing history once per file.
	logs := actionlog.NewCache()
	// One cache for the batch: a batch of files mentioning the same on-call
	// rotation resolves each person once, not once per file.
	users := pagedoc.NewUserCache()
	failures := 0
	results := make([]*updateResult, 0, len(args))
	for _, filename := range args {
		r := processFile(filename, c, roots, indexes, users, logs)
		recordAction(logs, r)
		results = append(results, r)
		if !ui.IsJSON() {
			r.renderHuman()
		}
		if !r.ok {
			failures++
		}
	}
	reportBaseGaps(results, logs)
	for _, dir := range roots.Roots() {
		ui.Info("root: " + dir)
	}
	// Under --debug only, and beside the root it belongs to: a project-wide
	// default takes effect for a file that says nothing about it, so it has no
	// answer anywhere in the file a reader would open.
	project.ReportSettings(roots)

	if ui.IsJSON() {
		items := make([]any, len(results))
		for i, r := range results {
			items[i] = r.jsonResult()
		}
		env := jsonout.NewEnvelope("update", items, summarize(results))
		env.Roots = roots.Roots()
		if err := jsonout.Emit(os.Stdout, env); err != nil {
			return err
		}
		if failures > 0 {
			return ui.SilentExit(1)
		}
		return nil
	}

	if failures > 0 {
		ui.Error(fmt.Sprintf("%d of %d file(s) failed.", failures, len(args)))
		return ui.ErrSilent
	}
	return nil
}

// processFile publishes one file and returns a result describing the outcome. It
// performs no output itself; the caller renders the result (human lines or JSON).
func processFile(
	filename string, c *client.ConfluenceClient, roots *project.Cache, indexes *linkindex.Cache,
	users *pagedoc.UserCache, logs *actionlog.Cache,
) *updateResult {
	r := &updateResult{file: filename, dryRun: dryRun}
	mf, err := frontmatter.ParseFile(filename)
	if err != nil {
		return r.fail(err, jsonout.CodeValidation)
	}

	// The root comes first now: a file's metadata may live in the project
	// file's pages: block rather than in the file, so nothing local can be
	// checked until the root is known, and the root also decides which action
	// log the base is read from. The walk is cached, so asking early costs
	// nothing.
	//
	// Building the link *index* no longer sits below a cheap skip: the
	// idempotence check compares a sha of the render, so a file skipped as
	// unchanged has been rendered by then. The two cases that still return
	// before it are an unmanaged file and a refused one -- divergence is
	// decided from the version alone, precisely so a refusal pays for
	// nothing. The index is cached per root, so the cost is one per batch
	// rather than one per file, against files already read.
	abs, err := filepath.Abs(filename)
	if err != nil {
		return r.fail(err, jsonout.CodeIO)
	}
	root, err := roots.Resolve(filepath.Dir(abs))
	if err != nil {
		return r.fail(project.RootError(err), rootErrorCode(err))
	}
	key, keyed := pagemeta.KeyFor(root, abs)
	r.root = root
	if keyed {
		r.logKey = key
	}
	meta, err := pagemeta.Resolve(key, mf, root)
	if err != nil {
		// A coordinate disagreement: the two locations name different pages, and
		// publishing to either would be a guess about which one the author
		// means. Only this file fails; the rest of the batch proceeds.
		return r.fail(err, jsonout.CodeValidation)
	}
	r.warnings = append(r.warnings, meta.Warnings...)

	// Nothing anywhere claims this file, so there is nothing to publish and
	// nothing wrong (#139). Repositories legitimately hold Markdown that is not
	// published to Confluence, drafts are a normal state, and a glob-driven CI
	// run must not go red because somebody added a file. A file that *is*
	// claimed but has no page_id still fails below: somebody registered it and
	// create has not run.
	if !meta.Managed() {
		r.ok = true
		r.status = statusSkipped
		r.unmanaged = true
		// metadata_source stays null, deliberately, even for a file whose
		// frontmatter holds a known field: the question it answers is "which
		// location supplied the metadata this page was published from", and
		// nothing was published. Setting it before this check reported
		// "frontmatter" on a skip, contradicting the schema and leaving
		// --json unable to tell an unmanaged skip from an unchanged one.
		return r
	}
	r.metadataSource = string(meta.MetadataSource())

	title, titlePresent, pageID := resolveTitlePageID(meta.Fields)
	// Before the request, like the page-id check below: an empty title is a
	// local defect, and paying for a round trip to discover it is waste. Only a
	// title that is *present* and empty is wrong -- an absent title means the
	// file does not manage the page's title, which is honoured further down.
	if title == "" && titlePresent {
		return r.fail(errors.New(
			"the title is present but empty; give it a value, or remove it to keep the "+
				"live page title"), jsonout.CodeValidation)
	}
	if pageID == "" {
		return r.fail(errors.New(
			"no page id: set page_id in this file's frontmatter or in its "+
				project.Filename+" entry, or create the page first"),
			jsonout.CodeValidation)
	}
	r.pageID = pageID
	// Before the request: an id that is not digits earns a 400 whose raw body is
	// the least useful thing markfluence can show a reader.
	if !pageref.IsDigits(pageID) {
		return r.fail(errors.New(pageref.NotNumericMessage(pageID)), jsonout.CodeValidation)
	}
	width, applyWidth, err := resolveWidth(meta.Fields, root)
	if err != nil {
		return r.fail(err, jsonout.CodeValidation)
	}
	// Before any request, and fatal: an invalid label is a local defect, and
	// the one it exists to catch is not recoverable afterwards. A name holding
	// a space publishes *successfully* as several labels that read back as none
	// of what the file says, so there is no later run that can clean it up --
	// see docs/confluence/labels.md.
	labelSet, err := labels.Declared(meta.Lists, meta.Fields)
	if err != nil {
		return r.fail(err, jsonout.CodeValidation)
	}
	r.warnings = append(r.warnings, labelSet.Warnings...)
	// Offline half only: a present-but-empty page_status is a defect in the
	// file. The *name* cannot be checked until the page is known, since the
	// vocabulary is a property of its space.
	statusName, statusDeclared, err := pagestatus.Declared(meta.Fields)
	if err != nil {
		return r.fail(err, jsonout.CodeValidation)
	}

	// GetPageOrNil, not GetPage: a 404 here means the page_id is wrong, which is
	// worth saying in words. Every other transport failure still reports itself.
	page, err := c.GetPageOrNil(pageID)
	if err != nil {
		return r.fail(err, jsonout.CodeFor(err))
	}
	if page == nil {
		// update cannot conjure the page: the remedy is a right id, or create.
		return r.fail(errors.New(pageref.NotFoundMessage(pageID,
			"correct it, or remove it and use create instead")), jsonout.CodeNotFound)
	}
	r.space = client.SpaceKeyFromWebUI(page.Links.WebUI)
	// Before the divergence check: a page in the wrong space is the more basic
	// error, and a refusal pays for nothing further.
	if err := checkSpace(meta, root, r.space); err != nil {
		return r.fail(err, jsonout.CodeValidation)
	}
	if title == "" {
		title = page.Title // fall back to the live page's title
	}
	r.title = title
	r.versionPrev = page.Version.Number
	r.url = c.PageURL(page, pageID)

	// The merge base: what this copy was derived from (#149). Read before the
	// render, because the divergence check below needs no render and a refused
	// file should pay for nothing.
	//
	// The mtime comparison that used to stand here is gone, with no fallback
	// for a file that has no base. Its failure modes are precisely the ones
	// this check exists to fix -- git does not preserve mtimes, so a clone,
	// pull, checkout or touch all look exactly like an edit, and it compared
	// the local filesystem's clock against Atlassian's -- so keeping it would
	// have preserved every one of them for exactly the population with no
	// base, which at first is everyone.
	base, haveBase := usableBase(logs.Get(root), r.logKey, pageID)
	if haveBase {
		r.base = &base
	}

	// Divergence: the page has moved past this copy. Refused rather than
	// warned, because a warning that publishes anyway is the silent data loss
	// this exists to prevent with extra text on top. Deliberately not
	// qualified by the sha: a page that moved must not be overwritten whether
	// or not I also have local edits, and the case where I have none is the
	// worse one -- publishing would replace their work with the very bytes
	// they started from.
	if !force && haveBase && base.PageVersion != 0 && page.Version.Number != base.PageVersion {
		// "export --force" rather than "re-export": export skips a page whose
		// file is already there (S3) and records nothing for a skip, so a
		// plain re-export leaves this refusal in place and repeats it
		// verbatim on the next run.
		return r.fail(fmt.Errorf(
			"the page has changed since your copy (v%d -> v%d); replace your copy with "+
				"`export --force` before publishing, or `update --force` to publish over it",
			base.PageVersion, page.Version.Number),
			jsonout.CodeConflict)
	}

	// Whether the page moves, and whether it can: every read the move needs,
	// before any write, so a parent that cannot be resolved or would make a
	// loop fails the file with nothing written -- under --dry-run too. The
	// move itself comes first among the writes below (_plans/053 D5).
	mv, err := planMove(c, meta, root, filepath.Dir(abs), page, r.space)
	if err != nil {
		return r.fail(err, jsonout.CodeOr(err, jsonout.CodeValidation))
	}

	// The name half of page_status, resolved to the status to write. Before any
	// request that could change the page -- the file names a status and the wire
	// format is an id, and a name Confluence does not recognise would *create*
	// a status no API route can delete (docs/confluence/page-status.md) -- and
	// after the divergence check, which is a request a refused file must not
	// pay for, the same rule the merge-base read above follows. A name matching
	// nothing is a local failure carrying what the page does offer, which is
	// the main way an author learns the vocabulary at all.
	//
	// Asked of this page rather than of its space, and not cached across a
	// batch: the vocabulary is per (caller, page), so another page's answer is
	// a guess -- see pagestatus.Resolve.
	var status client.ContentState
	if statusDeclared {
		status, err = pagestatus.Resolve(c, pageID, statusName)
		if err != nil {
			return r.fail(err, jsonout.CodeOr(err, jsonout.CodeValidation))
		}
	}

	index, err := indexes.Get(root)
	if err != nil {
		return r.fail(fmt.Errorf("building the link index: %w", err), jsonout.CodeIO)
	}

	// SiteURL, not BaseURL: rewritten links are published into the page, so they
	// must point at the site even when requests go through the gateway.
	pageContent, err := convert.MdToConfluence(mf, root, index, c.SiteURL(), r.space)
	if err != nil {
		return r.fail(err, jsonout.CodeConvert)
	}
	// What the body PUT would send, hashed: the resolved title and the body.
	// Taken from the same two values handed to UpdatePage below, so the
	// recorded value and the published one cannot drift apart.
	r.publishSHA = actionlog.Sum(title, pageContent.HTML)

	// Idempotence: the page already holds what this file renders to, so the
	// body PUT would change nothing. --force does not override it and must
	// not: --force means "always PUT", and this is the check that decides
	// whether there is a PUT to make at all -- so it only ever runs when
	// --force is absent, and a forced run publishes unconditionally.
	//
	// It skips the *body* alone. The width, label and attachment passes below
	// still run, which is the fix for a live bug: the old mtime skip returned
	// before all three, so redrawing an image without touching the Markdown
	// never uploaded it and the page kept serving the old diagram.
	// Both halves of the base are required, not just the sha. The sha attests
	// what markfluence last *published*; only the version agreeing with the
	// live page attests that the page still holds it. With a version the
	// divergence check above has already established that agreement, so this
	// is about the line that carries a sha and no version -- an export line
	// written during the walk, or a hand-edited log. There, concluding
	// "unchanged" would report a skip while somebody's UI edit stood and this
	// file's content was never published. A missing version means "cannot
	// conclude", which means publish.
	bodyChanged := true
	if !force && haveBase && base.PublishSHA256 != "" &&
		base.PageVersion != 0 && page.Version.Number == base.PageVersion {
		bodyChanged = base.PublishSHA256 != r.publishSHA
		r.bodyChanged = &bodyChanged
	}
	r.broken = append(r.broken, pageContent.Broken...)
	r.warnings = append(r.warnings, pageContent.Warnings...)
	// Publishing needs no display names -- the account id is already in the
	// Markdown -- so this lookup exists only to catch an id that names nobody,
	// which Confluence will not: it renders any id as "@Unlicensed user". The
	// cache makes it one request per distinct person across the whole batch.
	r.warnings = append(r.warnings, pagedoc.MentionWarnings(c, users, pageContent.Mentions)...)

	next := page.Version.Number + 1

	// --dry-run: preview attachments (read-only) and the width change, but make
	// no writes. The version bump and page URL are the same values a real run
	// would produce, so the human output lines are identical.
	if dryRun {
		actions, err := c.PlanAttachments(pageID, pageContent.Attachments)
		if err != nil {
			return r.fail(err, jsonout.CodeOr(err, jsonout.CodeIO))
		}
		for _, a := range actions {
			r.attachments = append(r.attachments, jsonout.Attachment{Action: a.Action, Filename: a.Filename})
		}
		r.versionNew = next
		if !bodyChanged {
			r.versionNew = page.Version.Number
		}
		// Reported only here and once the move has happened, never when it was
		// merely planned: a failure in between must not claim a move.
		r.move = mv
		r.previewWidth(c, pageID, width, applyWidth)
		r.previewLabels(c, pageID, labelSet)
		r.previewStatus(c, pageID, status, statusDeclared)
		r.ok = true
		r.status = statusFor(r, bodyChanged)
		return r
	}

	// The move first, and fatal, unlike the width and label passes below: those
	// run once the page is published, so failing would report a publish that
	// happened, while nothing has been written when this runs. A v1 move
	// leaves the page version alone, so nothing after it needs to know.
	if mv != nil {
		if err := c.MovePage(pageID, mv.position, mv.target); err != nil {
			return r.fail(err, jsonout.CodeFor(err))
		}
		r.move = mv
	}

	// CodeOr, not CodeFor: planning an upload checksums every local asset, so
	// a file that cannot be read fails here and is an IO failure, not a
	// network one. Same for PlanAttachments in the dry-run above.
	actions, err := c.SyncAttachments(pageID, pageContent.Attachments)
	if err != nil {
		return r.fail(err, jsonout.CodeOr(err, jsonout.CodeIO))
	}
	for _, a := range actions {
		r.attachments = append(r.attachments, jsonout.Attachment{Action: a.Action, Filename: a.Filename})
	}

	r.versionNew = page.Version.Number
	if bodyChanged {
		result, err := c.UpdatePage(pageID, title, pageContent.HTML, next, message)
		if err != nil {
			return r.fail(err, jsonout.CodeFor(err))
		}
		r.versionNew = next
		r.url = c.PageURL(result, pageID)
	}

	// Assert the page width (a separate content-property call) only when set;
	// non-fatal (a failure is a warning, not an error).
	if applyWidth {
		r.width = &jsonout.PageWidth{Value: string(width), Default: false}
		if acts, err := pagewidth.Apply(c, pageID, width); err != nil {
			r.width = nil
			r.warnings = append(r.warnings, "could not set page width: "+err.Error())
		} else {
			for _, a := range acts {
				if a.Action == "set" {
					r.widthSet = true
					break
				}
			}
		}
	}

	// Labels last, and non-fatal for the same reason the width is: the page is
	// published by the time this runs, so failing the result would report that
	// the publish did not happen. A declared-but-unapplied set leaves labels
	// null rather than claiming a set that is not there.
	r.applyLabels(c, pageID, labelSet)
	r.applyStatus(c, pageID, status, statusDeclared)
	// After every write, and only when the status pass actually wrote: that is
	// the one pass that bumps the page version, so the base has to name where
	// the page ended up rather than where the body PUT left it.
	if setAStatus(r) {
		version, err := pagestatus.VersionAfter(c, pageID, r.versionNew)
		r.versionFinal = version
		if err != nil {
			// Warned rather than swallowed: the recorded base is now one
			// version behind the page, so the next run of this file will
			// refuse it as diverged, and this line is the only thing that
			// explains a conflict markfluence caused itself.
			r.warnings = append(r.warnings,
				"page status set, but the page's new version could not be read ("+err.Error()+
					"); the next update of this file may report a conflict -- re-run this one, "+
					"or use --force")
		}
	}

	r.ok = true
	r.status = statusFor(r, bodyChanged)
	return r
}

// finalVersion is where the page actually ended up: what the base records and
// what --json reports. See updateResult.versionFinal.
func (r *updateResult) finalVersion() int {
	if r.versionFinal != 0 {
		return r.versionFinal
	}
	return r.versionNew
}

// usableBase returns the merge base recorded for this file, discarding one that
// describes a different page.
//
// The page_id check is the case that would produce a *wrong* answer rather than
// no answer: retarget a file at another page and the old entry's version is
// about something else entirely, so comparing it would invent "the page moved
// 40 versions" out of an edit to one line of frontmatter. Discarding leaves the
// file unknown, which means publish -- the same answer it would get on a fresh
// clone.
func usableBase(log *actionlog.Log, key, pageID string) (actionlog.Entry, bool) {
	e, ok := log.Base(key)
	if !ok || e.PageID != pageID {
		return actionlog.Entry{}, false
	}
	return e, true
}

// statusFor reports whether this run wrote anything to the page.
//
// "published" means the page was written to -- the body, a width, a label or
// an attachment -- and "skipped" means nothing was. That keeps the status enum
// as it is while letting the body be skipped on its own: a run that uploaded a
// redrawn diagram and republished no prose is a publish, and shows it with
// version.previous == version.new.
//
// "Skipping -- no changes" is finally true when it is printed: it now means
// the render matched the recorded base and nothing else was found to do,
// rather than that two timestamps happened to line up.
func statusFor(r *updateResult, bodyChanged bool) string {
	if bodyChanged || r.move != nil || r.widthSet || wroteAnAttachment(r) || changedALabel(r) || setAStatus(r) {
		return statusPublished
	}
	return statusSkipped
}

// wroteAnAttachment reports whether any attachment was uploaded. "skipped" is
// what SyncAttachments reports for one whose checksum already matches.
func wroteAnAttachment(r *updateResult) bool {
	for _, a := range r.attachments {
		if a.Action != "skipped" {
			return true
		}
	}
	return false
}

// setAStatus reports whether the page status was written this run. "unchanged"
// is not a change, and the distinction matters more here than for a label: a
// status write bumps the page version, so reporting "published" for an
// unchanged one would claim a version bump that deliberately did not happen.
func setAStatus(r *updateResult) bool {
	return r.pageStatus != nil && r.pageStatus.Action == "set"
}

// changedALabel reports whether any label was added or removed.
//
// "kept" is not a change, and reading it as one was a bug: it marks a surplus
// label that could *not* be removed because an unmanaged label shares its
// name, so it is the opposite of a write. A page carrying such a label reports
// it on every run, which would have made the result "published" with nothing
// written, forever -- and a consumer watching for a tree to settle into
// "skipped" would never see it.
func changedALabel(r *updateResult) bool {
	for _, l := range r.labels {
		if l.Action != labels.ActionUnchanged && l.Action != labels.ActionKept {
			return true
		}
	}
	return false
}

// previewWidth reports the width change a dry-run update would make. It reads the
// live width (read-only) and marks a change only when it differs from the intended
// width — matching the real run's "page width:" line only when there is
// something to change. A read failure is a warning, not fatal.
func (r *updateResult) previewWidth(
	c *client.ConfluenceClient, pageID string, width pagewidth.Width, applyWidth bool,
) {
	if !applyWidth {
		return
	}
	live, _, err := pagewidth.Read(c, pageID)
	if err != nil {
		r.warnings = append(r.warnings, "could not read page width: "+err.Error())
		return
	}
	if live == width {
		return
	}
	r.width = &jsonout.PageWidth{Value: string(width), Default: false}
	r.widthSet = true
}

// resolveTitlePageID reads the effective title and page id out of a file's
// resolved metadata, which may have come from its frontmatter or from a pages:
// entry (#139).
//
// An empty page id is an error; an empty title is an error only when the key is
// *present*, which is what titlePresent reports -- an absent title means the
// file does not manage the page's title and falls back to the live one further
// down. There is no flag to consider any more: update consumes metadata and has
// no way to invent any (#139's "flags describe the run; files describe the
// page").
func resolveTitlePageID(fields map[string]string) (
	title string, titlePresent bool, pageID string) {
	title, titlePresent = fields["title"]
	return strings.TrimSpace(title), titlePresent, strings.TrimSpace(fields["page_id"])
}

// resolveWidth resolves the page width to assert: the file's own page_width --
// from its frontmatter or its pages: entry, whichever supplied it -- then the
// project file's project-wide default. It returns apply=false when neither is
// set, meaning the live page's width is left untouched.
//
// --page-width is gone with the other page-metadata flags (#139): a uniform
// width across a batch is what the project-wide default is for, and it says so
// permanently rather than per invocation.
func resolveWidth(fields map[string]string, root *project.Root) (pagewidth.Width, bool, error) {
	if raw, ok := fields["page_width"]; ok && strings.TrimSpace(raw) != "" {
		w, err := pagewidth.Declared(fields)
		return w, err == nil, err
	}
	// The project file's default, and the one level of the chain that changes
	// update's behavior: declaring page_width there makes update assert a width
	// on a file that declares none, where before it left the live width alone.
	// That is deliberate -- it is what "declared means asserted" (L9) means one
	// level up -- and a project that wants the live width untouched omits the
	// key. Absent still means no width request at all.
	if root != nil && root.Config.PageWidth != "" {
		w, err := pagewidth.Declared(map[string]string{"page_width": root.Config.PageWidth})
		if err != nil {
			return "", false, fmt.Errorf("%s: %w", root.File, err)
		}
		return w, true, nil
	}
	return "", false, nil
}

// rootErrorCode classifies a root-resolution failure. A malformed project file
// is a local defect in a file the author can open and fix, so reporting it as
// I/O would send the reader looking for a disk fault.
func rootErrorCode(err error) jsonout.Code {
	if project.IsConfigError(err) {
		return jsonout.CodeValidation
	}
	return jsonout.CodeIO
}

// applyLabels asserts the declared label set, recording the per-label actions.
//
// A file that declares no labels key makes no request at all -- not merely no
// write. That is what makes "absent means untouched" a property rather than an
// implementation detail, and it is why the check is here rather than inside
// labels.Apply, which refuses an undeclared set outright.
//
// A failure is a warning on a successful result, matching pagewidth.Apply: the
// body is already published, and reporting the file as failed would say
// otherwise. The labels field stays nil so nothing claims a set that was not
// asserted.
func (r *updateResult) applyLabels(c *client.ConfluenceClient, pageID string, s labels.Set) {
	if !s.Declared {
		return
	}
	actions, warnings, err := labels.Apply(c, pageID, s)
	r.warnings = append(r.warnings, warnings...)
	if err != nil {
		r.warnings = append(r.warnings, "could not set labels: "+err.Error())
		return
	}
	r.labels = toJSONLabels(actions)
}

// previewLabels reports the label changes a dry run would make, read-only. A
// read failure is a warning, not fatal -- mirroring previewWidth.
func (r *updateResult) previewLabels(c *client.ConfluenceClient, pageID string, s labels.Set) {
	if !s.Declared {
		return
	}
	actions, warnings, err := labels.Plan(c, pageID, s)
	r.warnings = append(r.warnings, warnings...)
	if err != nil {
		r.warnings = append(r.warnings, "could not read labels: "+err.Error())
		return
	}
	r.labels = toJSONLabels(actions)
}

// applyStatus asserts the declared page status, recording what it did.
//
// Non-fatal, for the reason pagewidth.Apply and labels.Apply are: the page is
// published by the time this runs, so failing the result would report a publish
// that happened as one that did not. A declared-but-unapplied status leaves the
// field null rather than claiming a lozenge that is not there.
//
// An undeclared status makes no request at all -- not even the read Apply does
// to avoid a needless version bump -- which is the property that keeps this free
// for every tree not using the field.
func (r *updateResult) applyStatus(
	c *client.ConfluenceClient, pageID string, status client.ContentState, declared bool,
) {
	if !declared {
		return
	}
	action, err := pagestatus.Apply(c, pageID, status)
	if err != nil {
		r.warnings = append(r.warnings, "could not set page status: "+err.Error())
		return
	}
	r.pageStatus = &jsonout.PageStatus{Name: action.Name, Action: action.Action}
}

// previewStatus reports the status change a dry run would make, read-only.
// A read failure is a warning, not fatal -- mirroring previewWidth.
func (r *updateResult) previewStatus(
	c *client.ConfluenceClient, pageID string, status client.ContentState, declared bool,
) {
	if !declared {
		return
	}
	live, err := pagestatus.Read(c, pageID)
	if err != nil {
		r.warnings = append(r.warnings, "could not read page status: "+err.Error())
		return
	}
	action := "set"
	if live != nil && live.ID == status.ID {
		action = "unchanged"
	}
	r.pageStatus = &jsonout.PageStatus{Name: status.Name, Action: action}
}

// toJSONLabels converts label actions to the reported shape, always non-nil so
// a declared-but-empty set renders as [] rather than null.
func toJSONLabels(actions []labels.Action) []jsonout.Label {
	out := make([]jsonout.Label, 0, len(actions))
	for _, a := range actions {
		out = append(out, jsonout.Label{Action: a.Action, Name: a.Name})
	}
	return out
}

// recordAction appends this file's line to its root's action log (#149), so a
// later run can tell "I changed this" from "they changed it".
//
// Called from the batch loop as each file completes, not once at the end: #139
// D10's reasoning applies unchanged, since a run that dies partway must leave
// every already-published page recorded.
//
// A failure to write is a warning and never a failure. The page is published by
// the time this runs, so failing the result would report that it was not -- the
// non-fatal shape pagewidth.Apply and labels.Apply already have. The cost lands
// on the next run, which sees a base trailing the live page by this publish and
// reports a divergence that --force resolves.
//
// **A body-unchanged skip records too**, and that is load-bearing rather than
// tidy: once the sha does the skipping most runs skip, so a publish-only log
// would never accrue a base in a tree that is already published. A skip is a
// perfectly good observation of the base -- the render matched the page at
// version N -- which is exactly what the mtime skip could never claim, and why
// that one recorded nothing.
//
// What records nothing:
//
//   - --dry-run, because a preview that logged would claim a publish happened.
//   - A file nothing claims, because there is no page to have a base against.
//   - A file with no manifest key, because a key is what a line is looked up by.
//   - Anything that never reached the render, which is the divergence refusal
//     and every earlier failure: those record a *failed* line when a page id is
//     known, kept as history and never read as a base.
func recordAction(logs *actionlog.Cache, r *updateResult) {
	if dryRun || r.logKey == "" || r.pageID == "" {
		return
	}
	if r.ok && r.publishSHA == "" {
		return
	}
	log := logs.Get(r.root)
	if log == nil {
		return
	}
	entry := actionlog.Entry{
		Action:        actionlog.ActionUpdate,
		Status:        actionlog.StatusOK,
		File:          r.logKey,
		PageID:        r.pageID,
		PageVersion:   r.finalVersion(),
		PublishSHA256: r.publishSHA,
	}
	if !r.ok {
		// Recorded for the history, never read as a base. The version is the
		// one seen rather than the one intended: nothing was published.
		entry.Status = actionlog.StatusFailed
		entry.PageVersion = r.versionPrev
	}
	if err := log.Append(entry); err != nil {
		r.warnings = append(r.warnings, "could not record this publish in "+log.Path()+": "+err.Error())
	}
}

// reportBaseGaps says, once per run, what the moved-page check could not cover.
//
// Once per run and not once per file, which is the whole design of it. Right
// after this lands every file is unknown, so a per-file line would fire on 200
// of 200 and say the same thing 200 times -- carrying no per-file information
// and training people to scroll past it. One line extinguishes itself as bases
// accrue, and a steady-state "3 of 200" is worth reading: those three came
// from another machine or were never published from here.
//
// Silent under --force, where neither check ran and so there is no gap to
// report, and silent under --json, where every ui helper is a no-op and the
// per-file "base": null says it better anyway.
//
// The count and the two warnings below are deliberately different things. A
// missing base is **transient** -- publishing records one and the line stops
// -- while no project file and an unreadable log never self-heal, so each of
// those names a remedy instead of being counted.
func reportBaseGaps(results []*updateResult, logs *actionlog.Cache) {
	if force {
		return
	}
	checked, unknown := 0, 0
	rootless := map[string]bool{}
	for _, r := range results {
		if r.root != nil && r.root.File == "" && r.pageID != "" {
			rootless[r.root.Dir] = true
			// Counted *or* warned, never both. A rootless project can never
			// accrue a base, so including it would make a line that claims to
			// extinguish itself repeat forever beside the warning explaining
			// why -- which is the distinction this function's own comment
			// draws.
			continue
		}
		// Only a file this run actually published or verified: a non-zero sha
		// means the render happened, which is where both checks finish. A
		// file that failed -- a 404, a convert error, a refused page -- must
		// not be counted, or the sentence says it was published without the
		// check when it was not published at all.
		if !r.ok || r.publishSHA == "" {
			continue
		}
		checked++
		if r.base == nil {
			unknown++
		}
	}
	if unknown > 0 {
		ui.Hint(fmt.Sprintf(
			"no publish base for %d of %d file(s); published without the moved-page check",
			unknown, checked))
	}
	for _, dir := range sortedKeys(rootless) {
		ui.Warn("no " + project.Filename + " above " + dir +
			": nothing can record what these files were published from, so a page somebody " +
			"edited elsewhere is overwritten without warning. An empty " + project.Filename +
			" there is enough.")
	}
	for _, l := range sortedLogs(logs) {
		if err := l.ReadError(); err != nil {
			ui.Warn("could not read " + l.Path() + ": " + err.Error() +
				". No file under that root can be checked for a moved page.")
		}
	}
}

// sortedKeys returns a map's keys in order, so a multi-root batch reports the
// same lines in the same order every run.
func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// sortedLogs returns the batch's logs ordered by path, for the same reason.
func sortedLogs(logs *actionlog.Cache) []*actionlog.Log {
	out := logs.Logs()
	sort.Slice(out, func(i, j int) bool { return out[i].Path() < out[j].Path() })
	return out
}
