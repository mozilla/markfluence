// Package update implements the `markfluence update` command: publish markdown
// files to existing Confluence pages.
package update

import (
	"fmt"
	"os"
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
	message string
	force   bool
)

// Cmd is the update command.
var Cmd = &cobra.Command{
	Use:   "update FILE...",
	Short: "Publish one or more markdown files to Confluence pages",
	Long: "Publish one or more markdown FILEs to Confluence pages.\n\n" +
		"Title and page id are read from each file's YAML frontmatter. Each file is\n" +
		"processed independently; the command exits non-zero if any file failed.",
	Args: cobra.MinimumNArgs(1),
	RunE: run,
}

func init() {
	Cmd.Flags().StringVar(&message, "message", "Updated via markfluence", "Version message.")
	Cmd.Flags().BoolVar(&force, "force", false, "Skip the file-mtime check and always update the page.")
}

func run(cmd *cobra.Command, args []string) error {
	url, _ := cmd.Flags().GetString("url")
	username, _ := cmd.Flags().GetString("username")
	c, err := client.Resolve(url, username)
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

	title := mf.Title()
	if title == "" {
		ui.Error(prefix + " no 'title' field found in frontmatter; add a 'title:' line")
		return false
	}
	width, err := pagewidth.Declared(mf.Frontmatter)
	if err != nil {
		ui.Error(prefix + " " + err.Error())
		return false
	}

	pageID := mf.PageID()
	if pageID == "" {
		pageID = findByTitle(c, prefix, title)
		if pageID == "" {
			return false
		}
		ui.Info(fmt.Sprintf("%s Found page id %s; writing to frontmatter", prefix, pageID))
		if err := os.WriteFile(filename,
			[]byte(frontmatter.UpdateField(mf.Content, "page_id", pageID, "")), 0o644); err != nil {
			ui.Error(prefix + " " + err.Error())
			return false
		}
	}

	page, err := c.GetPage(pageID)
	if err != nil {
		ui.Error(prefix + " " + err.Error())
		return false
	}
	spaceKey := client.SpaceKeyFromWebUI(page.Links.WebUI)

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

	// Assert the page width (a separate content-property call); non-fatal.
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

	ui.Success(fmt.Sprintf("%s Published v%d: %s", prefix, next, pageURL(c, result, pageID)))
	return true
}

// findByTitle resolves a page id by exact title, printing an error and returning
// "" on zero or multiple matches.
func findByTitle(c *client.ConfluenceClient, prefix, title string) string {
	ui.Info(fmt.Sprintf("%s Searching for page titled '%s'...", prefix, title))
	matches, err := c.SearchPagesByTitle(title, "")
	if err != nil {
		ui.Error(prefix + " " + err.Error())
		return ""
	}
	switch len(matches) {
	case 0:
		ui.Error(fmt.Sprintf("%s no Confluence page found with title '%s'", prefix, title))
		return ""
	case 1:
		return matches[0].ID
	default:
		ui.Error(fmt.Sprintf("%s found %d pages with title '%s':", prefix, len(matches), title))
		for _, m := range matches {
			ui.Error(fmt.Sprintf("%s   - %s: %s (%s/wiki/pages/viewpage.action?pageId=%s)",
				prefix, m.ID, m.Title, c.BaseURL(), m.ID))
		}
		return ""
	}
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
