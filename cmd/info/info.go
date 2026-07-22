// Package info implements the `markfluence info` command: print a page's metadata.
package info

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/frontmatter"
	"github.com/mozilla/markfluence/internal/pagewidth"
	"github.com/mozilla/markfluence/internal/ui"
	"github.com/spf13/cobra"
)

// valueMax is the length at which a content-property value is truncated.
const valueMax = 100

var showProperties bool

// Cmd is the info command.
var Cmd = &cobra.Command{
	Use:   "info ARG",
	Short: "Print metadata about a Confluence page",
	Long: "Print metadata about a Confluence page.\n\n" +
		"ARG is a numeric page id or a markdown file whose frontmatter has a page_id.",
	Args: cobra.ExactArgs(1),
	RunE: run,
}

func init() {
	Cmd.Flags().BoolVar(&showProperties, "properties", false,
		"Also list all of the page's content properties.")
}

func run(cmd *cobra.Command, args []string) error {
	url, _ := cmd.Flags().GetString("url")
	username, _ := cmd.Flags().GetString("username")
	c, err := client.Resolve(url, username)
	if err != nil {
		ui.Error(err.Error())
		return ui.ErrSilent
	}

	pageID, err := resolvePageID(args[0])
	if err != nil {
		ui.Error(err.Error())
		return ui.ErrSilent
	}
	page, err := c.GetPageOrNil(pageID)
	if err != nil {
		ui.Error(err.Error())
		return ui.ErrSilent
	}
	if page == nil {
		ui.Error(fmt.Sprintf("page %s not found", pageID))
		return ui.ErrSilent
	}
	fmt.Println(formatPage(page, c, showProperties))
	return nil
}

// resolvePageID resolves the CLI argument to a page id: a markdown file's
// frontmatter page_id, or a bare numeric id.
func resolvePageID(arg string) (string, error) {
	if info, err := os.Stat(arg); err == nil && !info.IsDir() {
		mf, err := frontmatter.ParseFile(arg)
		if err != nil {
			return "", err
		}
		if mf.PageID() == "" {
			return "", fmt.Errorf("no page_id in frontmatter of %s", arg)
		}
		return mf.PageID(), nil
	}
	if isDigits(arg) {
		return arg, nil
	}
	return "", fmt.Errorf("%s is not a file or a numeric page id", arg)
}

// formatPage builds the aligned "label: value" report for a page.
func formatPage(page *client.Page, c *client.ConfluenceClient, withProps bool) string {
	spaceKey := client.SpaceKeyFromWebUI(page.Links.WebUI)
	parent := page.ParentID
	if parent == "" {
		parent = "none (top-level)"
	}

	url := page.Links.Base + page.Links.WebUI
	if page.Links.WebUI == "" {
		url = fmt.Sprintf("%s/wiki/pages/viewpage.action?pageId=%s", c.BaseURL(), page.ID)
	} else if page.Links.Base == "" {
		url = c.BaseURL() + "/wiki" + page.Links.WebUI
	}

	cache := map[string]string{}
	creator := authorName(c, page.AuthorID, cache)
	editor := authorName(c, page.Version.AuthorID, cache)

	pageWidth, properties, propsErr := resolveWidth(c, page.ID, withProps)

	rows := [][2]string{
		{"id", page.ID},
		{"title", page.Title},
		{"status", page.Status},
		{"space", spaceKey},
		{"parent", parent},
		{"version", versionNumber(page.Version.Number)},
		{"page_width", pageWidth},
		{"created", withAuthor(page.CreatedAt, creator)},
		{"updated", withAuthor(page.Version.CreatedAt, editor)},
		{"message", page.Version.Message},
		{"url", url},
	}
	labelWidth := 0
	for _, r := range rows {
		if len(r[0]) > labelWidth {
			labelWidth = len(r[0])
		}
	}
	labelWidth++ // room for the ':'

	var b strings.Builder
	for _, r := range rows {
		if r[1] == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "%-*s %s", labelWidth, r[0]+":", r[1])
	}
	if withProps {
		b.WriteByte('\n')
		b.WriteString(propertiesSection(properties, propsErr))
	}
	return b.String()
}

// resolveWidth derives the page_width display string and, when withProps is set,
// the full property list. A fetch failure is tolerated (width "unknown").
func resolveWidth(c *client.ConfluenceClient, pageID string, withProps bool) (string, []client.Property, error) {
	var (
		props    []client.Property
		width    pagewidth.Width
		explicit bool
		err      error
	)
	if withProps {
		props, err = c.ListContentProperties(pageID)
		if err == nil {
			width, explicit = pagewidth.WidthFromProperties(props)
		}
	} else {
		width, explicit, err = pagewidth.Read(c, pageID)
	}
	if err != nil {
		return "unknown", nil, err
	}
	if explicit {
		return string(width), props, nil
	}
	return string(width) + " (Confluence default)", props, nil
}

func propertiesSection(properties []client.Property, err error) string {
	if err != nil {
		return fmt.Sprintf("content properties: (could not fetch: %s)", err)
	}
	if len(properties) == 0 {
		return "content properties: (none)"
	}
	sorted := append([]client.Property(nil), properties...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Key < sorted[j].Key })
	lines := []string{"content properties:"}
	for _, p := range sorted {
		lines = append(lines, fmt.Sprintf("  %s: %s", p.Key, renderValue(p.Value)))
	}
	return strings.Join(lines, "\n")
}

// authorName resolves an account id to a display name, caching lookups and
// falling back to the raw id.
func authorName(c *client.ConfluenceClient, accountID string, cache map[string]string) string {
	if accountID == "" {
		return ""
	}
	if name, ok := cache[accountID]; ok {
		return name
	}
	name := c.GetUser(accountID)
	if name == "" {
		name = accountID
	}
	cache[accountID] = name
	return name
}

func renderValue(v any) string {
	text, ok := v.(string)
	if !ok {
		b, _ := json.Marshal(v)
		text = string(b)
	}
	r := []rune(text)
	if len(r) > valueMax {
		text = string(r[:valueMax-1]) + "…"
	}
	return text
}

func withAuthor(when, who string) string {
	if when == "" {
		return ""
	}
	if who == "" {
		return when
	}
	return when + " by " + who
}

func versionNumber(n int) string {
	if n == 0 {
		return ""
	}
	return strconv.Itoa(n)
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
