// Package update implements the `markfluence update` command: publish markdown
// files to existing Confluence pages.
package update

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mozilla/markfluence/internal/buildinfo"
	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/convert"
	"github.com/mozilla/markfluence/internal/frontmatter"
	"github.com/mozilla/markfluence/internal/pagewidth"
	"github.com/mozilla/markfluence/internal/ui"
	"github.com/spf13/cobra"
)

var (
	message       string
	force         bool
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
	Args: cobra.MinimumNArgs(1),
	RunE: run,
}

func init() {
	Cmd.Flags().StringVar(&message, "message", "Updated via markfluence", "Version message.")
	Cmd.Flags().BoolVar(&force, "force", false, "Skip the file-mtime check and always update the page.")
	Cmd.Flags().StringVar(&titleFlag, "title", "",
		"Override the page title (requires a single FILE).")
	Cmd.Flags().StringVar(&pageIDFlag, "page-id", "",
		"Override the target page id (requires a single FILE).")
	Cmd.Flags().StringVar(&pageWidthFlag, "page-width", "",
		"Override the page width: narrow, wide, or max.")
}

func run(cmd *cobra.Command, args []string) error {
	if overrideNeedsSingleFile(titleFlag, pageIDFlag, len(args)) {
		ui.Error("--title/--page-id apply to a single page; pass exactly one FILE")
		return ui.ErrSilent
	}

	url, _ := cmd.Flags().GetString("url")
	username, _ := cmd.Flags().GetString("username")
	envFile, _ := cmd.Flags().GetString("env-file")
	c, err := client.Resolve(url, username, envFile)
	if err != nil {
		ui.Error(err.Error())
		return ui.ErrSilent
	}

	failures := 0
	for _, filename := range args {
		if !processFile(filename, c) {
			failures++
		}
	}
	if failures > 0 {
		ui.Error(fmt.Sprintf("%d of %d file(s) failed.", failures, len(args)))
		return ui.ErrSilent
	}
	return nil
}

func processFile(filename string, c *client.ConfluenceClient) bool {
	prefix := "[" + filename + "]"
	mf, err := frontmatter.ParseFile(filename)
	if err != nil {
		ui.Error(prefix + " " + err.Error())
		return false
	}

	title, pageID := resolveTitlePageID(titleFlag, pageIDFlag, mf)
	if pageID == "" {
		ui.Error(prefix + " no page id: set page_id in frontmatter or pass --page-id")
		return false
	}
	width, applyWidth, err := resolveWidth(pageWidthFlag, mf)
	if err != nil {
		ui.Error(prefix + " " + err.Error())
		return false
	}

	page, err := c.GetPage(pageID)
	if err != nil {
		ui.Error(prefix + " " + err.Error())
		return false
	}
	spaceKey := client.SpaceKeyFromWebUI(page.Links.WebUI)
	if title == "" {
		title = page.Title // fall back to the live page's title
	}

	if !force && page.Version.CreatedAt != "" {
		if pageUpdated, err := time.Parse(time.RFC3339, page.Version.CreatedAt); err == nil {
			if info, err := os.Stat(filename); err == nil && !info.ModTime().After(pageUpdated) {
				ui.Info(prefix + " Skipping -- no changes")
				return true
			}
		}
	}

	pageContent, err := convert.MdToConfluence(mf, c.BaseURL(), spaceKey, buildinfo.Stamp())
	if err != nil {
		ui.Error(prefix + " " + err.Error())
		return false
	}
	for _, msg := range append(append([]string{}, pageContent.Broken...), pageContent.Warnings...) {
		ui.Warn(prefix + " " + msg)
	}

	actions, err := c.SyncAttachments(pageID, toLocalAttachments(pageContent.Attachments))
	if err != nil {
		ui.Error(prefix + " " + err.Error())
		return false
	}
	for _, a := range actions {
		ui.Info(fmt.Sprintf("%s attachment %s: %s", prefix, a.Action, a.Filename))
	}

	next := page.Version.Number + 1
	ui.Info(fmt.Sprintf("%s Updating '%s' (v%d -> v%d)...", prefix, title, page.Version.Number, next))
	result, err := c.UpdatePage(pageID, title, pageContent.HTML, next, message)
	if err != nil {
		ui.Error(prefix + " " + err.Error())
		return false
	}

	// Assert the page width (a separate content-property call) only when set;
	// non-fatal.
	if applyWidth {
		if acts, err := pagewidth.Apply(c, pageID, width); err != nil {
			ui.Warn(prefix + " could not set page width: " + err.Error())
		} else {
			for _, a := range acts {
				if a.Action == "set" {
					ui.Info(prefix + " page width: " + string(width))
					break
				}
			}
		}
	}

	ui.Success(fmt.Sprintf("%s Published v%d: %s", prefix, next, pageURL(c, result, pageID)))
	return true
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
		return fmt.Sprintf("%s/wiki/pages/viewpage.action?pageId=%s", c.BaseURL(), pageID)
	}
	base := page.Links.Base
	if base == "" {
		base = c.BaseURL() + "/wiki"
	}
	return base + page.Links.WebUI
}

func toLocalAttachments(atts []convert.Attachment) []client.LocalAttachment {
	out := make([]client.LocalAttachment, len(atts))
	for i, a := range atts {
		out[i] = client.LocalAttachment{Path: a.Path, Filename: a.Filename}
	}
	return out
}
