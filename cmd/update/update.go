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
	c, err := client.Resolve(client.Options{
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

	title, pageID := resolveTitlePageID(titleFlag, pageIDFlag, mf)
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
	r.url = pageURL(c, page, pageID)

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
		actions, err := c.PlanAttachments(pageID, toLocalAttachments(pageContent.Attachments))
		if err != nil {
			return r.fail(err, jsonout.CodeFor(err))
		}
		for _, a := range actions {
			r.attachments = append(r.attachments, jsonout.Attachment{Action: a.Action, Filename: a.Filename})
		}
		r.versionNew = next
		r.previewWidth(c, pageID, width, applyWidth)
		r.ok = true
		r.status = statusPublished
		return r
	}

	actions, err := c.SyncAttachments(pageID, toLocalAttachments(pageContent.Attachments))
	if err != nil {
		return r.fail(err, jsonout.CodeFor(err))
	}
	for _, a := range actions {
		r.attachments = append(r.attachments, jsonout.Attachment{Action: a.Action, Filename: a.Filename})
	}

	result, err := c.UpdatePage(pageID, title, pageContent.HTML, next, message)
	if err != nil {
		return r.fail(err, jsonout.CodeFor(err))
	}
	r.versionNew = next
	r.url = pageURL(c, result, pageID)

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
// flags override the file's frontmatter. Either may be "" (an empty title falls
// back to the live page title later; an empty page id is an error).
func resolveTitlePageID(cliTitle, cliPageID string, mf *frontmatter.MarkdownFile) (title, pageID string) {
	title = cliTitle
	if title == "" {
		title = mf.Title()
	}
	pageID = cliPageID
	if pageID == "" {
		pageID = mf.PageID()
	}
	return title, pageID
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

func pageURL(c *client.ConfluenceClient, page *client.Page, pageID string) string {
	if page.Links.WebUI == "" {
		return fmt.Sprintf("%s/wiki/pages/viewpage.action?pageId=%s", c.SiteURL(), pageID)
	}
	base := page.Links.Base
	if base == "" {
		base = c.SiteURL() + "/wiki"
	}
	return base + page.Links.WebUI
}

func toLocalAttachments(atts []convert.Attachment) []client.LocalAttachment {
	out := make([]client.LocalAttachment, len(atts))
	for i, a := range atts {
		out[i] = client.LocalAttachment{Path: a.Path, Filename: a.Filename, Source: a.Source}
	}
	return out
}
