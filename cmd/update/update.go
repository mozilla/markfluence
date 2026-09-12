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
	"github.com/mozilla/markfluence/internal/pageref"
	"github.com/mozilla/markfluence/internal/pagewidth"
	"github.com/mozilla/markfluence/internal/project"
	"github.com/mozilla/markfluence/internal/ui"
	"github.com/spf13/cobra"
)

var (
	message       string
	force         bool
	dryRun        bool
	titleFlag     string
	pageIDFlag    string
	pageWidthFlag string
)

// Cmd is the update command.
var Cmd = &cobra.Command{
	Use:   "update FILE...",
	Short: "Publish one or more markdown files to Confluence pages",
	Long: "Publish one or more markdown FILEs to Confluence pages.\n\n" +
		"Title and page id are read from each file's YAML frontmatter; --title and\n" +
		"--page-id override the frontmatter (and require a single FILE). A page id is\n" +
		"required (from --page-id or frontmatter). Page width is asserted only when\n" +
		"set via --page-width or a page_width frontmatter line. Each file is processed\n" +
		"independently; the command exits non-zero if any file failed.",
	Args:              cobra.MinimumNArgs(1),
	ValidArgsFunction: completion.MarkdownFiles,
	RunE:              run,
}

func init() {
	Cmd.Flags().StringVar(&message, "message", "Updated via markfluence", "Version message.")
	Cmd.Flags().BoolVar(&force, "force", false, "Skip the file-mtime check and always update the page.")
	Cmd.Flags().BoolVar(&dryRun, "dry-run", false,
		"Preview what would be published without writing to Confluence.")
	Cmd.Flags().StringVar(&titleFlag, "title", "",
		"Override the page title (requires a single FILE).")
	Cmd.Flags().StringVar(&pageIDFlag, "page-id", "",
		"Override the target page id (requires a single FILE).")
	Cmd.Flags().StringVar(&pageWidthFlag, "page-width", "",
		"Override the page width: narrow, wide, or max.")

	completion.RegisterFlag(Cmd, "page-width", completion.Values(pagewidth.Vocabulary()...))
}

func run(cmd *cobra.Command, args []string) error {
	if overrideNeedsSingleFile(titleFlag, pageIDFlag, len(args)) {
		ui.Error("--title/--page-id apply to a single page; pass exactly one FILE")
		return ui.ErrSilent
	}

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

	failures := 0
	results := make([]*updateResult, 0, len(args))
	for _, filename := range args {
		r := processFile(filename, c, roots, indexes)
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
) *updateResult {
	r := &updateResult{file: filename, dryRun: dryRun}
	mf, err := frontmatter.ParseFile(filename)
	if err != nil {
		return r.fail(err, jsonout.CodeValidation)
	}

	title, titlePresent, pageID := resolveTitlePageID(titleFlag, pageIDFlag, mf)
	// Before the request, like the page-id check below: an empty title is a
	// local defect, and paying for a round trip to discover it is waste. Only a
	// title that is *present* and empty is wrong -- an absent title means the
	// file does not manage the page's title, which is honoured further down.
	if title == "" && titlePresent {
		return r.fail(errors.New(
			"frontmatter has an empty 'title:'; give it a value, remove it to keep the "+
				"live page title, or pass --title"), jsonout.CodeValidation)
	}
	if pageID == "" {
		return r.fail(errors.New("no page id: set page_id in frontmatter or pass --page-id"),
			jsonout.CodeValidation)
	}
	r.pageID = pageID
	// Before the request: an id that is not digits earns a 400 whose raw body is
	// the least useful thing markfluence can show a reader.
	if !pageref.IsDigits(pageID) {
		return r.fail(errors.New(pageref.NotNumericMessage(pageID)), jsonout.CodeValidation)
	}
	width, applyWidth, err := resolveWidth(pageWidthFlag, mf)
	if err != nil {
		return r.fail(err, jsonout.CodeValidation)
	}
	// Before any request, and fatal: an invalid label is a local defect, and
	// the one it exists to catch is not recoverable afterwards. A name holding
	// a space publishes *successfully* as several labels that read back as none
	// of what the file says, so there is no later run that can clean it up --
	// see docs/confluence/labels.md.
	labelSet, err := labels.Declared(mf.Lists, mf.Frontmatter)
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

	abs, err := filepath.Abs(filename)
	if err != nil {
		return r.fail(err, jsonout.CodeIO)
	}
	root, err := roots.Resolve(filepath.Dir(abs))
	if err != nil {
		return r.fail(fmt.Errorf("resolving the documentation root: %w", err), jsonout.CodeIO)
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

// overrideNeedsSingleFile reports whether a per-page override (--title/--page-id)
// was given with anything other than exactly one FILE. --page-width is exempt (a
// uniform width change across a batch is sensible).
func overrideNeedsSingleFile(cliTitle, cliPageID string, nFiles int) bool {
	return (cliTitle != "" || cliPageID != "") && nFiles != 1
}

// resolveTitlePageID resolves the effective title and page id, letting the CLI
// flags override the file's frontmatter. An empty page id is an error; an empty
// title is an error only when the frontmatter key is present, which is what
// titlePresent reports. An absent title falls back to the live page title later.
//
// --title wins over both, as every other override does, so it satisfies a
// present-but-empty frontmatter title rather than tripping over it.
func resolveTitlePageID(cliTitle, cliPageID string, mf *frontmatter.MarkdownFile) (
	title string, titlePresent bool, pageID string) {
	title = cliTitle
	if title == "" {
		title, titlePresent = mf.TitleField()
	}
	pageID = cliPageID
	if pageID == "" {
		pageID = mf.PageID()
	}
	return title, titlePresent, pageID
}

// resolveWidth resolves the page width to assert. It returns apply=false when
// neither --page-width nor a frontmatter page_width is set, meaning the live
// page's width should be left untouched.
func resolveWidth(cliPageWidth string, mf *frontmatter.MarkdownFile) (pagewidth.Width, bool, error) {
	if cliPageWidth != "" {
		w, err := pagewidth.Declared(map[string]string{"page_width": cliPageWidth})
		return w, err == nil, err
	}
	if raw, ok := mf.Frontmatter["page_width"]; ok && strings.TrimSpace(raw) != "" {
		w, err := pagewidth.Declared(mf.Frontmatter)
		return w, err == nil, err
	}
	return "", false, nil
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
