// Package fix implements the `markfluence fix` command: reconcile a file's
// frontmatter coordinates to its live Confluence page. It never writes the
// server; it writes a file only when a field actually changed.
package fix

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/completion"
	"github.com/mozilla/markfluence/internal/frontmatter"
	"github.com/mozilla/markfluence/internal/jsonout"
	"github.com/mozilla/markfluence/internal/pageref"
	"github.com/mozilla/markfluence/internal/pagewidth"
	"github.com/mozilla/markfluence/internal/ui"
	"github.com/spf13/cobra"
)

var dryRun bool

// Cmd is the fix command.
var Cmd = &cobra.Command{
	Use:   "fix FILE...",
	Short: "Reconcile each markdown file's frontmatter to its live Confluence page",
	Long: "Reconcile each markdown file's frontmatter to its live Confluence page.\n\n" +
		"Populates/refreshes page_id, space, parent, and page_width (and fills a\n" +
		"missing title) from the live page. Each file is processed independently;\n" +
		"the command exits non-zero if any file failed.\n\n" +
		"parent is written as the live page's parent id. In a tree written by\n" +
		"`export --depth`, where parent points at the parent's own .md file,\n" +
		"fix therefore replaces that path with an id -- consistent with\n" +
		"reconciling to the live page, and worth knowing before running it over\n" +
		"an exported tree.",
	Args:              cobra.MinimumNArgs(1),
	ValidArgsFunction: completion.MarkdownFiles,
	RunE:              run,
}

func init() {
	Cmd.Flags().BoolVar(&dryRun, "dry-run", false,
		"Report the changes fix would make without writing any files.")
}

func run(cmd *cobra.Command, args []string) error {
	url, _ := cmd.Flags().GetString("url")
	username, _ := cmd.Flags().GetString("username")
	cloudID, _ := cmd.Flags().GetString("cloud-id")
	envFile, _ := cmd.Flags().GetString("env-file")
	c, err := client.Resolve(client.ResolveOptions{
		URL: url, Username: username, CloudID: cloudID, EnvFile: envFile,
	})
	if err != nil {
		if ui.IsJSON() {
			_ = jsonout.EmitError(os.Stderr, "fix", err.Error(), jsonout.CodeConfig)
		} else {
			ui.Error(err.Error())
		}
		return ui.SilentExit(2)
	}

	if dryRun {
		ui.Warn("DRY RUN — no changes will be written.")
	}

	failures := 0
	results := make([]*fixResult, 0, len(args))
	for _, filename := range args {
		r := processFile(filename, c)
		results = append(results, r)
		if !ui.IsJSON() {
			r.renderHuman()
		}
		if !r.ok {
			failures++
		}
	}

	if ui.IsJSON() {
		items := make([]any, len(results))
		for i, r := range results {
			items[i] = r.jsonResult()
		}
		env := jsonout.NewEnvelope("fix", items, summarize(results))
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

// change is a planned frontmatter edit.
type change struct {
	field, oldDisplay, newValue string
}

// processFile reconciles one file and returns a result. It performs no output;
// the caller renders the result.
func processFile(filename string, c *client.ConfluenceClient) *fixResult {
	r := &fixResult{file: filename, dryRun: dryRun}
	mf, err := frontmatter.ParseFile(filename)
	if err != nil {
		return r.fail(err, jsonout.CodeValidation)
	}
	page, err := locatePage(mf.Frontmatter, c)
	if err != nil {
		return r.fail(err, locateCode(err))
	}
	r.pageID = page.ID

	// Read the live width to reconcile page_width; a read failure is non-fatal.
	liveWidth := ""
	if w, _, err := pagewidth.Read(c, page.ID); err != nil {
		r.warnings = append(r.warnings, "could not read page width: "+err.Error())
	} else {
		liveWidth = string(w)
	}

	r.changes = plannedChanges(mf.Frontmatter, page, liveWidth)
	if len(r.changes) == 0 {
		r.ok = true
		r.status = statusConsistent
		return r
	}
	if dryRun {
		r.ok = true
		r.status = statusChanged
		return r
	}

	content := mf.Content
	for _, ch := range r.changes {
		content = frontmatter.UpdateField(content, ch.field, ch.newValue, "")
	}
	if err := os.WriteFile(filename, []byte(content), 0o644); err != nil {
		return r.fail(err, jsonout.CodeIO)
	}
	r.ok = true
	r.status = statusChanged
	return r
}

// locatePage finds the live page for a file: by page_id if present, else by
// searching for the frontmatter title.
func locatePage(fm map[string]string, c *client.ConfluenceClient) (*client.Page, error) {
	if pageID := fm["page_id"]; norm(pageID) != "" {
		page, err := c.GetPageOrNil(pageID)
		if err != nil {
			return nil, err
		}
		if page == nil {
			return nil, errors.New(
				pageref.NotFoundMessage(pageID, "remove it to search by title, or correct it"))
		}
		return page, nil
	}

	title := fm["title"]
	if title == "" {
		return nil, errors.New("no page_id or title in frontmatter; add one so the page can be located")
	}
	matches, err := c.SearchPagesByTitle(title, "")
	if err != nil {
		return nil, err
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("no Confluence page found with title %q", title)
	case 1:
		return c.GetPage(matches[0].ID)
	default:
		var b strings.Builder
		fmt.Fprintf(&b, "found %d pages with title %q:", len(matches), title)
		for _, m := range matches {
			fmt.Fprintf(&b, "\n  - %s: %s (%s/wiki/pages/viewpage.action?pageId=%s)",
				m.ID, m.Title, c.SiteURL(), m.ID)
		}
		b.WriteString("\nadd a page_id to the frontmatter to disambiguate")
		return nil, errors.New(b.String())
	}
}

// plannedChanges computes the field edits needed to reconcile fm to page. Only
// fields that actually differ are returned.
func plannedChanges(fm map[string]string, page *client.Page, liveWidth string) []change {
	live := []struct{ field, value string }{
		{"page_id", page.ID},
		{"space", client.SpaceKeyFromWebUI(page.Links.WebUI)},
		{"parent", orNull(norm(page.ParentID))},
	}

	var changes []change
	for _, lv := range live {
		if lv.value == "" {
			continue // e.g. space key couldn't be derived
		}
		current, present := fm[lv.field]
		switch {
		case !present || strings.TrimSpace(current) == "":
			changes = append(changes, change{lv.field, "(none)", lv.value})
		case norm(current) != norm(lv.value):
			changes = append(changes, change{lv.field, current, lv.value})
		}
	}

	if strings.TrimSpace(fm["title"]) == "" {
		changes = append(changes, change{"title", "(none)", page.Title})
	}

	if liveWidth != "" {
		raw, present := fm["page_width"]
		declared := "max"
		if s := strings.ToLower(strings.TrimSpace(raw)); s != "" {
			declared = s
		}
		if declared != liveWidth {
			old := "(none)"
			if present && strings.TrimSpace(raw) != "" {
				old = raw
			}
			changes = append(changes, change{"page_width", old, liveWidth})
		}
	}
	return changes
}

// norm treats "", whitespace-only, and the literal "null" all as no value.
func norm(value string) string {
	t := strings.TrimSpace(value)
	if t == "" || t == "null" {
		return ""
	}
	return t
}

func orNull(s string) string {
	if s == "" {
		return "null"
	}
	return s
}
