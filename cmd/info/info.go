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
	"github.com/mozilla/markfluence/internal/jsonout"
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
	envFile, _ := cmd.Flags().GetString("env-file")
	c, err := client.Resolve(url, username, envFile)
	if err != nil {
		return fatalFail(err.Error(), jsonout.CodeConfig)
	}

	pageID, err := resolvePageID(args[0])
	if err != nil {
		return fatalFail(err.Error(), jsonout.CodeValidation)
	}
	page, err := c.GetPageOrNil(pageID)
	if err != nil {
		return operationalFail(pageID, err, jsonout.CodeFor(err))
	}
	if page == nil {
		return operationalFail(pageID, fmt.Errorf("page %s not found", pageID), jsonout.CodeNotFound)
	}

	rep := buildReport(page, c, showProperties)
	if ui.IsJSON() {
		env := jsonout.NewEnvelope("info", []any{rep.jsonResult()},
			map[string]int{"total": 1, "succeeded": 1, "failed": 0})
		if err := jsonout.Emit(os.Stdout, env); err != nil {
			return err
		}
		return nil
	}
	fmt.Println(rep.human())
	return nil
}

// fatalFail reports a config/usage/pre-flight failure: a JSON error object on
// stderr under --json, else a human error line, exiting 2.
func fatalFail(msg string, code jsonout.Code) error {
	if ui.IsJSON() {
		_ = jsonout.EmitError(os.Stderr, "info", msg, code)
	} else {
		ui.Error(msg)
	}
	return ui.SilentExit(2)
}

// operationalFail reports an operational failure for the single target: under
// --json a results[0] entry {ok:false,error,code}, else a human error line,
// exiting 1.
func operationalFail(pageID string, err error, code jsonout.Code) error {
	if ui.IsJSON() {
		res := map[string]any{"ok": false, "page_id": pageID, "error": err.Error(), "code": code}
		env := jsonout.NewEnvelope("info", []any{res},
			map[string]int{"total": 1, "succeeded": 0, "failed": 1})
		_ = jsonout.Emit(os.Stdout, env)
	} else {
		ui.Error(err.Error())
	}
	return ui.SilentExit(1)
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

// report is the resolved metadata for a page, feeding both the human "label:
// value" renderer and the JSON result. Fields are captured raw (empty when
// absent); each renderer decides how to present or omit them.
type report struct {
	id, title, status, space string
	parentID                 string // "" for a top-level page
	versionNum               int
	widthKnown               bool
	width                    jsonout.PageWidth
	createdAt, creator       string
	creatorID                string
	updatedAt, editor        string
	editorID                 string
	message, url             string
	withProps                bool
	properties               []client.Property
	propsErr                 error
}

// buildReport resolves a page (and, when withProps is set, its content
// properties) into a report. Author names and page width are fetched here; a
// width-fetch failure is tolerated (widthKnown stays false).
func buildReport(page *client.Page, c *client.ConfluenceClient, withProps bool) report {
	url := page.Links.Base + page.Links.WebUI
	if page.Links.WebUI == "" {
		url = fmt.Sprintf("%s/wiki/pages/viewpage.action?pageId=%s", c.BaseURL(), page.ID)
	} else if page.Links.Base == "" {
		url = c.BaseURL() + "/wiki" + page.Links.WebUI
	}

	cache := map[string]string{}
	r := report{
		id:         page.ID,
		title:      page.Title,
		status:     page.Status,
		space:      client.SpaceKeyFromWebUI(page.Links.WebUI),
		parentID:   page.ParentID,
		versionNum: page.Version.Number,
		createdAt:  page.CreatedAt,
		creator:    authorName(c, page.AuthorID, cache),
		creatorID:  page.AuthorID,
		updatedAt:  page.Version.CreatedAt,
		editor:     authorName(c, page.Version.AuthorID, cache),
		editorID:   page.Version.AuthorID,
		message:    page.Version.Message,
		url:        url,
		withProps:  withProps,
	}

	var (
		width    pagewidth.Width
		explicit bool
		err      error
	)
	if withProps {
		r.properties, err = c.ListContentProperties(page.ID)
		r.propsErr = err
		if err == nil {
			width, explicit = pagewidth.WidthFromProperties(r.properties)
		}
	} else {
		width, explicit, err = pagewidth.Read(c, page.ID)
	}
	if err == nil {
		r.widthKnown = true
		r.width = jsonout.PageWidth{Value: string(width), Default: !explicit}
	}
	return r
}

// human builds the aligned "label: value" report (empty fields omitted).
func (r report) human() string {
	widthDisplay := "unknown"
	if r.widthKnown {
		widthDisplay = r.width.Value
		if r.width.Default {
			widthDisplay += " (Confluence default)"
		}
	}
	parent := r.parentID
	if parent == "" {
		parent = "none (top-level)"
	}

	rows := [][2]string{
		{"id", r.id},
		{"title", r.title},
		{"status", r.status},
		{"space", r.space},
		{"parent", parent},
		{"version", versionNumber(r.versionNum)},
		{"page_width", widthDisplay},
		{"created", withAuthor(r.createdAt, r.creator)},
		{"updated", withAuthor(r.updatedAt, r.editor)},
		{"message", r.message},
		{"url", r.url},
	}
	labelWidth := 0
	for _, row := range rows {
		if len(row[0]) > labelWidth {
			labelWidth = len(row[0])
		}
	}
	labelWidth++ // room for the ':'

	var b strings.Builder
	for _, row := range rows {
		if row[1] == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "%-*s %s", labelWidth, row[0]+":", row[1])
	}
	if r.withProps {
		b.WriteByte('\n')
		b.WriteString(propertiesSection(r.properties, r.propsErr))
	}
	return b.String()
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
