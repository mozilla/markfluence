// Package create implements the `markfluence create` command: create new
// Confluence pages from markdown files. Creation is two-phase: every file is
// validated first, and only if all pass are the pages created (parents first).
package create

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mozilla/markfluence/internal/buildinfo"
	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/convert"
	"github.com/mozilla/markfluence/internal/frontmatter"
	"github.com/mozilla/markfluence/internal/pagewidth"
	"github.com/mozilla/markfluence/internal/ui"
	"github.com/spf13/cobra"
)

var (
	spaceOpt  string
	parentOpt string
)

// Cmd is the create command.
var Cmd = &cobra.Command{
	Use:   "create FILE...",
	Short: "Create new Confluence pages from markdown files",
	Long: "Create new Confluence pages from markdown FILEs.\n\n" +
		"All files are validated first; if any would fail, nothing is created.\n" +
		"Otherwise pages are created parents-first and their page_id/space/parent\n" +
		"are written back into the frontmatter.",
	Args: cobra.MinimumNArgs(1),
	RunE: run,
}

func init() {
	Cmd.Flags().StringVar(&spaceOpt, "space", "", "Target space key.")
	Cmd.Flags().StringVar(&parentOpt, "parent", "", "Parent page id for the new page(s).")
}

// parentInfo describes a resolved parent. kind is top|inset|published|external.
type parentInfo struct {
	kind    string
	id      string // page id (published/external)
	abs     string // absolute path of an in-set parent
	display string // original .md path when the parent was a file reference
}

// record is a validated file ready for creation.
type record struct {
	filename string
	absPath  string
	mdfile   *frontmatter.MarkdownFile
	title    string
	spaceKey string
	spaceID  string
	parent   parentInfo
	width    pagewidth.Width
}

func run(cmd *cobra.Command, args []string) error {
	url, _ := cmd.Flags().GetString("url")
	username, _ := cmd.Flags().GetString("username")
	c, err := client.Resolve(url, username)
	if err != nil {
		ui.Error(err.Error())
		return ui.ErrSilent
	}

	inSetAbs := map[string]bool{}
	for _, f := range args {
		if abs, err := filepath.Abs(f); err == nil {
			inSetAbs[abs] = true
		}
	}
	spaceCache := map[string]string{}

	// Phase 1: validate every file, create nothing.
	type failure struct{ filename, message string }
	var records []record
	var errs []failure
	for _, filename := range args {
		r, err := resolveFile(filename, c, inSetAbs, spaceCache)
		if err != nil {
			errs = append(errs, failure{filename, err.Error()})
			continue
		}
		records = append(records, r)
	}

	var ordered []record
	if len(errs) == 0 {
		byAbs := map[string]record{}
		for _, r := range records {
			byAbs[r.absPath] = r
		}
		for _, r := range records {
			if r.parent.kind == "inset" && byAbs[r.parent.abs].spaceID != r.spaceID {
				errs = append(errs, failure{r.filename, "parent page is not in the target space"})
			}
		}
		if len(errs) == 0 {
			ordered, err = topoSort(records, byAbs)
			if err != nil {
				errs = append(errs, failure{"(hierarchy)", err.Error()})
			}
		}
	}

	if len(errs) > 0 {
		for _, e := range errs {
			ui.Error(fmt.Sprintf("[%s] %s", e.filename, e.message))
		}
		ui.Error(fmt.Sprintf("Aborting: %d file(s) failed validation; nothing was created.", len(errs)))
		return ui.ErrSilent
	}

	// Phase 2: create in topological order.
	created := map[string]string{}
	failures := 0
	for _, r := range ordered {
		prefix := "[" + r.filename + "]"
		parentID := r.parent.id
		if r.parent.kind == "inset" {
			parentID = created[r.parent.abs]
			if parentID == "" {
				ui.Error(prefix + " parent page was not created; skipping")
				failures++
				continue
			}
		}
		newID, url, err := createOne(r, parentID, c)
		if err != nil {
			ui.Error(prefix + " " + err.Error())
			failures++
			continue
		}
		created[r.absPath] = newID
		ui.Success(fmt.Sprintf("%s Created page %s: %s", prefix, newID, url))
	}

	if failures > 0 {
		ui.Error(fmt.Sprintf("%d of %d file(s) failed.", failures, len(ordered)))
		return ui.ErrSilent
	}
	return nil
}

func resolveFile(
	filename string, c *client.ConfluenceClient, inSetAbs map[string]bool, spaceCache map[string]string,
) (record, error) {
	mf, err := frontmatter.ParseFile(filename)
	if err != nil {
		return record{}, err
	}
	title := mf.Title()
	if title == "" {
		return record{}, errors.New("no 'title' field found in frontmatter")
	}
	width, err := pagewidth.Declared(mf.Frontmatter)
	if err != nil {
		return record{}, err
	}

	// Space: --space or frontmatter 'space'; both set and differing is an error.
	fmSpace := mf.Frontmatter["space"]
	if spaceOpt != "" && fmSpace != "" && spaceOpt != fmSpace {
		return record{}, fmt.Errorf("--space %q conflicts with frontmatter space %q", spaceOpt, fmSpace)
	}
	spaceKey := spaceOpt
	if spaceKey == "" {
		spaceKey = fmSpace
	}
	if spaceKey == "" {
		return record{}, errors.New("no space given (pass --space or add a 'space:' frontmatter field)")
	}
	spaceID, ok := spaceCache[spaceKey]
	if !ok {
		spaceID, err = c.ResolveSpaceID(spaceKey)
		if err != nil {
			return record{}, err
		}
		spaceCache[spaceKey] = spaceID
	}
	if spaceID == "" {
		return record{}, fmt.Errorf("space %q not found", spaceKey)
	}

	parent, err := resolveParent(filename, mf.Frontmatter, inSetAbs, c, spaceID)
	if err != nil {
		return record{}, err
	}

	if mf.PageID() != "" {
		exists, err := c.PageExists(mf.PageID())
		if err != nil {
			return record{}, err
		}
		if exists {
			return record{}, errors.New("a page already exists at this page_id")
		}
	}
	dupes, err := c.SearchPagesByTitle(title, spaceID)
	if err != nil {
		return record{}, err
	}
	if len(dupes) > 0 {
		return record{}, fmt.Errorf("a page titled %q already exists in space %s", title, spaceKey)
	}

	abs, _ := filepath.Abs(filename)
	return record{filename, abs, mf, title, spaceKey, spaceID, parent, width}, nil
}

func resolveParent(
	filename string, fm map[string]string, inSetAbs map[string]bool, c *client.ConfluenceClient, spaceID string,
) (parentInfo, error) {
	fmParent := fm["parent"]
	fmParentSet := fmParent != "" && fmParent != "null"
	if parentOpt != "" && fmParentSet {
		return parentInfo{}, errors.New("both --parent and a frontmatter 'parent' are set; use only one")
	}
	parentValue := fmParent
	if parentOpt != "" {
		parentValue = parentOpt
	}
	if parentValue == "" || parentValue == "null" {
		return parentInfo{kind: "top"}, nil
	}

	if strings.HasSuffix(parentValue, ".md") {
		parentPath := filepath.Join(filepath.Dir(filename), parentValue)
		if info, err := os.Stat(parentPath); err != nil || info.IsDir() {
			return parentInfo{}, fmt.Errorf("parent file not found: %s", parentValue)
		}
		parentAbs, _ := filepath.Abs(parentPath)
		if inSetAbs[parentAbs] {
			return parentInfo{kind: "inset", abs: parentAbs, display: parentValue}, nil
		}
		pmf, err := frontmatter.ParseFile(parentPath)
		if err != nil {
			return parentInfo{}, err
		}
		pID := pmf.PageID()
		if pID == "" {
			return parentInfo{}, fmt.Errorf("parent not yet published (no page_id): %s", parentValue)
		}
		if err := checkParentInSpace(c, pID, spaceID); err != nil {
			return parentInfo{}, err
		}
		return parentInfo{kind: "published", id: pID, display: parentValue}, nil
	}

	if err := checkParentInSpace(c, parentValue, spaceID); err != nil {
		return parentInfo{}, err
	}
	return parentInfo{kind: "external", id: parentValue}, nil
}

func checkParentInSpace(c *client.ConfluenceClient, parentID, spaceID string) error {
	p, err := c.GetPageOrNil(parentID)
	if err != nil {
		return err
	}
	if p == nil {
		return fmt.Errorf("parent page %s not found", parentID)
	}
	if p.SpaceID != spaceID {
		return fmt.Errorf("parent page %s is not in the target space", parentID)
	}
	return nil
}

// topoSort orders records parents-before-children, seeding the queue in input
// order for determinism. It errors on a cycle among in-set parents.
func topoSort(records []record, byAbs map[string]record) ([]record, error) {
	indeg := map[string]int{}
	children := map[string][]string{}
	for _, r := range records {
		indeg[r.absPath] = 0
	}
	for _, r := range records {
		if r.parent.kind == "inset" {
			children[r.parent.abs] = append(children[r.parent.abs], r.absPath)
			indeg[r.absPath]++
		}
	}

	var queue, order []string
	for _, r := range records {
		if indeg[r.absPath] == 0 {
			queue = append(queue, r.absPath)
		}
	}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		order = append(order, cur)
		for _, ch := range children[cur] {
			indeg[ch]--
			if indeg[ch] == 0 {
				queue = append(queue, ch)
			}
		}
	}
	if len(order) != len(records) {
		return nil, errors.New("parent cycle detected among the given files")
	}
	out := make([]record, len(order))
	for i, a := range order {
		out[i] = byAbs[a]
	}
	return out, nil
}

// parentField builds the (value, comment) for the frontmatter parent line.
func parentField(p parentInfo, parentID string) (value, comment string) {
	if p.kind == "top" {
		return "null", ""
	}
	return parentID, p.display
}

func createOne(r record, parentID string, c *client.ConfluenceClient) (newID, url string, err error) {
	prefix := "[" + r.filename + "]"
	pageContent, err := convert.MdToConfluence(r.mdfile, c.BaseURL(), r.spaceKey, buildinfo.Stamp())
	if err != nil {
		return "", "", err
	}
	for _, msg := range append(append([]string{}, pageContent.Broken...), pageContent.Warnings...) {
		ui.Warn(prefix + " " + msg)
	}

	result, err := c.CreatePage(r.spaceID, r.title, pageContent.HTML, parentID)
	if err != nil {
		return "", "", err
	}
	newID = result.ID

	actions, err := c.SyncAttachments(newID, toLocalAttachments(pageContent.Attachments))
	if err != nil {
		return "", "", err
	}
	for _, a := range actions {
		ui.Info(fmt.Sprintf("%s attachment %s: %s", prefix, a.Action, a.Filename))
	}

	if acts, err := pagewidth.Apply(c, newID, r.width); err != nil {
		ui.Warn(prefix + " could not set page width: " + err.Error())
	} else {
		for _, a := range acts {
			if a.Action == "set" {
				ui.Info(prefix + " page width: " + string(r.width))
				break
			}
		}
	}

	parentValue, parentComment := parentField(r.parent, parentID)
	content := r.mdfile.Content
	content = frontmatter.UpdateField(content, "page_id", newID, "")
	content = frontmatter.UpdateField(content, "space", r.spaceKey, "")
	content = frontmatter.UpdateField(content, "parent", parentValue, parentComment)
	if err := os.WriteFile(r.filename, []byte(content), 0o644); err != nil {
		return "", "", err
	}
	return newID, pageURL(c, result, newID), nil
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
