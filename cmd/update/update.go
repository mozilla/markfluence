// Package update implements the `markfluence update` command: publish markdown
// files to existing Confluence pages.
package update

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mozilla/markfluence/internal/buildinfo"
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
	Short: "Publish one or more markdown files to Confluence pages",
	Long: "Publish one or more markdown FILEs to Confluence pages.\n\n" +
		"Each file's title and page id come from its own YAML frontmatter, or from\n" +
		"a 'pages:' entry for it in markfluence.yaml -- a file can stay pristine and\n" +
		"keep its metadata there instead. Both places are legal and agreement is\n" +
		"silent; where they disagree about page_id, space or parent the file fails,\n" +
		"and where they disagree about title, page_width or labels the frontmatter\n" +
		"wins with a warning.\n\n" +
		"A file that neither place mentions is skipped, not failed: a repository\n" +
		"legitimately holds markdown that is not published, so a glob over a docs\n" +
		"tree does not go red because somebody added a draft. A file that IS\n" +
		"registered but has no page id fails -- something claimed it and the page\n" +
		"has not been created yet.\n\n" +
		"There are no per-page flags. Page metadata lives in the file or its entry,\n" +
		"which is what lets one invocation publish 'docs/**/*.md'; a flag would have\n" +
		"to name a single file. A project-wide 'page_width:' in markfluence.yaml is\n" +
		"how a whole tree gets one width.\n\n" +
		"Page width is asserted only when something declares it -- the file, its\n" +
		"entry, or the project-wide default -- otherwise the live page's width is\n" +
		"left untouched. Labels work the same way: a labels: line is asserted\n" +
		"exactly (anything on the page the file does not list is removed), and no\n" +
		"labels: line means the page's labels are left alone, not even read.\n\n" +
		"update never writes back to the file or to markfluence.yaml, so fixing a\n" +
		"wrong page_id is always safe: nothing is as you left it by accident. A\n" +
		"page_id that no longer resolves fails that file and says what to do about\n" +
		"it; one that is not a numeric id at all is reported without asking\n" +
		"Confluence.\n\n" +
		"A file that has not changed since the page's last version is skipped,\n" +
		"compared by mtime, unless --force is given. Each file is processed\n" +
		"independently; the command exits non-zero if any file failed.\n\n" +
		"--dry-run previews the version bump, attachment uploads and any width or\n" +
		"label change without writing to Confluence. It honours the mtime skip and\n" +
		"--force exactly as a real run does, so its forecast matches.",
	Example: "  # Publish a file, taking the page id from its frontmatter or its entry\n" +
		"  markfluence update docs/managing_an_incident.md\n\n" +
		"  # Publish a whole tree -- the CI shape: metadata comes from the files\n" +
		"  # and from markfluence.yaml, so nothing has to be passed per file\n" +
		"  markfluence update docs/**/*.md\n\n" +
		"  # Publish a batch with a version message\n" +
		"  markfluence update docs/*.md --message \"Bulk update\"\n\n" +
		"  # Republish even though the file has not changed\n" +
		"  markfluence update docs/foo.md --force\n\n" +
		"  # Preview, write nothing\n" +
		"  markfluence update docs/*.md --dry-run\n\n" +
		"  # See which location supplied each file's metadata\n" +
		"  markfluence update docs/*.md --json | jq -r '.results[] | \"\\(.file) \\(.metadata_source)\"'",
	Args:              cobra.MinimumNArgs(1),
	ValidArgsFunction: completion.MarkdownFiles,
	RunE:              run,
}

func init() {
	Cmd.Flags().StringVar(&message, "message", "Updated via markfluence", "Version message.")
	Cmd.Flags().BoolVar(&force, "force", false, "Skip the file-mtime check and always update the page.")
	Cmd.Flags().BoolVar(&dryRun, "dry-run", false,
		"Preview what would be published without writing to Confluence.")
}

func run(cmd *cobra.Command, args []string) error {
	url, _ := cmd.Flags().GetString("url")
	username, _ := cmd.Flags().GetString("username")
	cloudID, _ := cmd.Flags().GetString("cloud-id")
	envFile, _ := cmd.Flags().GetString("env-file")
	rootOverride, _ := cmd.Flags().GetString("root")
	roots := project.NewCache(rootOverride)
	defer roots.Close()
	c, err := client.Resolve(client.ResolveOptions{
		URL: url, Username: username, CloudID: cloudID, EnvFile: envFile, Roots: roots,
	})
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
	// One cache for the batch: a batch of files mentioning the same on-call
	// rotation resolves each person once, not once per file.
	users := pagedoc.NewUserCache()

	failures := 0
	results := make([]*updateResult, 0, len(args))
	for _, filename := range args {
		r := processFile(filename, c, roots, indexes, users)
		results = append(results, r)
		if !ui.IsJSON() {
			r.renderHuman()
		}
		if !r.ok {
			failures++
		}
	}
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
	users *pagedoc.UserCache,
) *updateResult {
	r := &updateResult{file: filename, dryRun: dryRun}
	mf, err := frontmatter.ParseFile(filename)
	if err != nil {
		return r.fail(err, jsonout.CodeValidation)
	}

	// The root comes first now: a file's metadata may live in the project
	// file's pages: block rather than in the file, so nothing local can be
	// checked until the root is known. The walk is cached, so asking early
	// costs nothing; building the link *index* is still below the mtime check,
	// so a file that is skipped never pays for one.
	abs, err := filepath.Abs(filename)
	if err != nil {
		return r.fail(err, jsonout.CodeIO)
	}
	root, err := roots.Resolve(filepath.Dir(abs))
	if err != nil {
		return r.fail(project.RootError(err), rootErrorCode(err))
	}
	key, _ := pagemeta.KeyFor(root, abs)
	meta, err := pagemeta.Resolve(key, mf, root)
	if err != nil {
		// A coordinate disagreement: the two locations name different pages, and
		// publishing to either would be a guess about which one the author
		// means. Only this file fails; the rest of the batch proceeds.
		return r.fail(err, jsonout.CodeValidation)
	}
	r.warnings = append(r.warnings, meta.Warnings...)

	// Nothing anywhere claims this file, so there is nothing to publish and
	// nothing wrong (#139). Repositories legitimately hold markdown that is not
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
	if title == "" {
		title = page.Title // fall back to the live page's title
	}
	r.title = title
	r.versionPrev = page.Version.Number
	r.url = c.PageURL(page, pageID)

	if !force && page.Version.CreatedAt != "" {
		if pageUpdated, err := time.Parse(time.RFC3339, page.Version.CreatedAt); err == nil {
			if info, err := os.Stat(filename); err == nil && !info.ModTime().After(pageUpdated) {
				r.ok = true
				r.status = statusSkipped
				r.versionNew = page.Version.Number
				return r
			}
		}
	}

	index, err := indexes.Get(root)
	if err != nil {
		return r.fail(fmt.Errorf("building the link index: %w", err), jsonout.CodeIO)
	}

	// SiteURL, not BaseURL: rewritten links are published into the page, so they
	// must point at the site even when requests go through the gateway.
	pageContent, err := convert.MdToConfluence(mf, root, index, c.SiteURL(), r.space, buildinfo.Stamp())
	if err != nil {
		return r.fail(err, jsonout.CodeConvert)
	}
	r.broken = append(r.broken, pageContent.Broken...)
	r.warnings = append(r.warnings, pageContent.Warnings...)
	// Publishing needs no display names -- the account id is already in the
	// markdown -- so this lookup exists only to catch an id that names nobody,
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
		r.previewWidth(c, pageID, width, applyWidth)
		r.previewLabels(c, pageID, labelSet)
		r.ok = true
		r.status = statusPublished
		return r
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

	result, err := c.UpdatePage(pageID, title, pageContent.HTML, next, message)
	if err != nil {
		return r.fail(err, jsonout.CodeFor(err))
	}
	r.versionNew = next
	r.url = c.PageURL(result, pageID)

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

	r.ok = true
	r.status = statusPublished
	return r
}

// previewWidth reports the width change a dry-run update would make. It reads the
// live width (read-only) and marks a change only when it differs from the intended
// width — mirroring fix's dry-run, and matching the real run's "page width:" line
// only when there is something to change. A read failure is a warning, not fatal.
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

// toJSONLabels converts label actions to the reported shape, always non-nil so
// a declared-but-empty set renders as [] rather than null.
func toJSONLabels(actions []labels.Action) []jsonout.Label {
	out := make([]jsonout.Label, 0, len(actions))
	for _, a := range actions {
		out = append(out, jsonout.Label{Action: a.Action, Name: a.Name})
	}
	return out
}
