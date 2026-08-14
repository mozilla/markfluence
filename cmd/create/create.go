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
	"github.com/mozilla/markfluence/internal/completion"
	"github.com/mozilla/markfluence/internal/convert"
	"github.com/mozilla/markfluence/internal/frontmatter"
	"github.com/mozilla/markfluence/internal/jsonout"
	"github.com/mozilla/markfluence/internal/pageref"
	"github.com/mozilla/markfluence/internal/pagewidth"
	"github.com/mozilla/markfluence/internal/ui"
	"github.com/spf13/cobra"
)

var (
	spaceOpt     string
	parentOpt    string
	titleOpt     string
	pageWidthOpt string
	persistOpt   bool
	noPersistOpt bool
	dryRunOpt    bool
)

// Cmd is the create command.
var Cmd = &cobra.Command{
	Use:   "create FILE...",
	Short: "Create new Confluence pages from markdown files",
	Long: "Create new Confluence pages from markdown FILEs.\n\n" +
		"All files are validated first; if any would fail, nothing is created.\n" +
		"Otherwise pages are created parents-first. --title and --page-width override\n" +
		"the frontmatter (--title requires a single FILE). Unless --no-persist is\n" +
		"given, each created page's title/space/parent/page_id/page_width are written\n" +
		"back into the frontmatter.",
	Args:              cobra.MinimumNArgs(1),
	ValidArgsFunction: completion.MarkdownFiles,
	RunE:              run,
}

func init() {
	Cmd.Flags().StringVar(&spaceOpt, "space", "", "Target space key.")
	Cmd.Flags().StringVar(&parentOpt, "parent", "", "Parent page id for the new page(s).")
	Cmd.Flags().StringVar(&titleOpt, "title", "",
		"Override the page title (requires a single FILE).")
	Cmd.Flags().StringVar(&pageWidthOpt, "page-width", "",
		"Override the page width: narrow, wide, or max.")
	Cmd.Flags().BoolVar(&persistOpt, "persist", true,
		"Write title/space/parent/page_id/page_width back into the frontmatter.")
	Cmd.Flags().BoolVar(&noPersistOpt, "no-persist", false,
		"Do not write anything back into the frontmatter.")
	Cmd.Flags().BoolVar(&dryRunOpt, "dry-run", false,
		"Preview what would be created without writing to Confluence or files.")

	completion.RegisterFlag(Cmd, "page-width", completion.Values(pagewidth.Vocabulary()...))
}

// parentInfo describes a resolved parent. kind is top|inset|published|external.
type parentInfo struct {
	kind string
	id   string // page id (published/external)
	abs  string // absolute path of an in-set parent
	// parentType is what the parent *is* on the server: "page" or "folder". It
	// does not vary the behavior of create — Confluence accepts either as a
	// parentId — it exists so --json can report which one was used. Empty for a
	// top-level page.
	parentType string
	display    string // original .md path when the parent was a file reference
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

// failure is a phase-1 validation error against a file (or "(hierarchy)").
// pageID and url are set only for a frontmatter page_id failure, which is the one
// validation error that can name a page: see pageIDFailure.
type failure struct {
	filename, message string
	pageID, url       string
}

// pageIDFailure is a phase-1 failure about a file's frontmatter page_id. create
// needs that id to point at nothing, and there are three ways it can fail: the id
// is not a page id at all, it resolves to nothing (so publishing would silently
// create a second page and overwrite the id), or a page is already there.
//
// It is a typed error rather than a plain one so the --json result can report
// page_id (always) and url (when a page is really there) as fields instead of
// leaving a consumer to parse them out of the message.
type pageIDFailure struct {
	pageID  string
	title   string // the live page's title, when one exists
	url     string // the live page's URL, when one exists
	message string
}

func (e *pageIDFailure) Error() string { return e.message }

// newFailure records a phase-1 error against a file, carrying over the fields of
// a page_id failure so abort() can report them without re-fetching anything.
func newFailure(filename string, err error) failure {
	f := failure{filename: filename, message: err.Error()}
	var pf *pageIDFailure
	if errors.As(err, &pf) {
		f.pageID, f.url = pf.pageID, pf.url
	}
	return f
}

// checkPageID validates a file's frontmatter page_id for creation. An empty id is
// what create expects, and is no error.
//
// The numeric check comes first because a page_id that is not digits (a pasted
// URL, a leftover "TODO") makes the API answer 400, whose raw body was what the
// user used to see.
func checkPageID(c *client.ConfluenceClient, pageID string) error {
	if pageID == "" {
		return nil
	}
	if !pageref.IsDigits(pageID) {
		return &pageIDFailure{pageID: pageID, message: pageref.NotNumericMessage(pageID)}
	}
	page, err := c.GetPageOrNil(pageID)
	if err != nil {
		return err
	}
	return pageIDFailureFor(c, pageID, page)
}

// pageIDFailureFor turns a resolved page_id into its failure, or nil when the id
// is free to create against. page is nil when the id resolves to nothing.
func pageIDFailureFor(c *client.ConfluenceClient, pageID string, page *client.Page) error {
	if page == nil {
		// Wording mirrors fix's locatePage, which reports the same condition; only
		// the remedy differs, since removing the id here means "create a new page".
		return &pageIDFailure{
			pageID:  pageID,
			message: pageref.NotFoundMessage(pageID, "remove it to create a new page, or correct it"),
		}
	}
	url := pageURL(c, page, pageID)
	return &pageIDFailure{
		pageID: pageID,
		title:  page.Title,
		url:    url,
		message: fmt.Sprintf("a page already exists at page_id %s (%q): %s",
			pageID, page.Title, url),
	}
}

func run(cmd *cobra.Command, args []string) error {
	if overrideNeedsSingleFile(titleOpt, len(args)) {
		return fatalFail("--title applies to a single page; pass exactly one FILE", jsonout.CodeConfig)
	}
	doPersist := wantPersist(persistOpt, noPersistOpt)

	url, _ := cmd.Flags().GetString("url")
	username, _ := cmd.Flags().GetString("username")
	cloudID, _ := cmd.Flags().GetString("cloud-id")
	envFile, _ := cmd.Flags().GetString("env-file")
	c, err := client.Resolve(client.Options{
		URL: url, Username: username, CloudID: cloudID, EnvFile: envFile,
	})
	if err != nil {
		return fatalFail(err.Error(), jsonout.CodeConfig)
	}

	if dryRunOpt {
		ui.Warn("DRY RUN — no changes will be written.")
	}

	inSetAbs := map[string]bool{}
	for _, f := range args {
		if abs, err := filepath.Abs(f); err == nil {
			inSetAbs[abs] = true
		}
	}
	spaceCache := map[string]string{}

	// Phase 1: validate every file, create nothing.
	var records []record
	var errs []failure
	for _, filename := range args {
		r, err := resolveFile(filename, c, inSetAbs, spaceCache)
		if err != nil {
			errs = append(errs, newFailure(filename, err))
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
				errs = append(errs, failure{filename: r.filename, message: "parent page is not in the target space"})
			}
		}
		if len(errs) == 0 {
			ordered, err = topoSort(records, byAbs)
			if err != nil {
				errs = append(errs, failure{filename: "(hierarchy)", message: err.Error()})
			}
		}
	}

	if len(errs) > 0 {
		return abort(args, errs)
	}

	// Phase 2: create in topological order.
	created := map[string]string{}
	failures := 0
	results := make([]*createResult, 0, len(ordered))
	for _, r := range ordered {
		res := createInOrder(r, created, c, doPersist)
		if res.ok {
			created[r.absPath] = res.pageID
		} else {
			failures++
		}
		results = append(results, res)
		if !ui.IsJSON() {
			res.renderHuman()
		}
	}

	if ui.IsJSON() {
		items := make([]any, len(results))
		for i, r := range results {
			items[i] = r.jsonResult()
		}
		env := jsonout.NewEnvelope("create", items, summarize(results))
		if err := jsonout.Emit(os.Stdout, env); err != nil {
			return err
		}
		if failures > 0 {
			return ui.SilentExit(1)
		}
		return nil
	}

	if failures > 0 {
		ui.Error(fmt.Sprintf("%d of %d file(s) failed.", failures, len(ordered)))
		return ui.ErrSilent
	}
	return nil
}

// createInOrder resolves the effective parent id for a record and creates it,
// returning a result. A missing in-set parent (its creation failed earlier) is a
// failed result rather than a create attempt.
func createInOrder(
	r record, created map[string]string, c *client.ConfluenceClient, doPersist bool,
) *createResult {
	parentID := r.parent.id
	if r.parent.kind == "inset" {
		parentID = created[r.parent.abs]
		// In a dry-run nothing is created, so an in-set parent has no id yet;
		// that is not a failure (the parent would have been created first). The
		// relationship is still reported via parent_file.
		if parentID == "" && !dryRunOpt {
			res := newResult(r)
			return res.fail(errors.New("parent page was not created; skipping"), jsonout.CodeValidation)
		}
	}
	return createOne(r, parentID, c, doPersist)
}

func resolveFile(
	filename string, c *client.ConfluenceClient, inSetAbs map[string]bool, spaceCache map[string]string,
) (record, error) {
	mf, err := frontmatter.ParseFile(filename)
	if err != nil {
		return record{}, err
	}
	title := resolveTitle(titleOpt, mf)
	if title == "" {
		return record{}, errors.New("no title given (pass --title or add a 'title:' frontmatter field)")
	}
	width, err := resolveWidth(pageWidthOpt, mf.Frontmatter)
	if err != nil {
		return record{}, err
	}

	// Before the space, parent, and duplicate-title lookups: a page_id that is
	// already taken or already broken is the most specific thing wrong with the
	// file, and reporting it first also spares three API calls the file cannot use.
	if err := checkPageID(c, mf.PageID()); err != nil {
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

	dupes, err := c.SearchPagesByTitle(title, spaceID)
	if err != nil {
		return record{}, err
	}
	if len(dupes) > 0 {
		// Link the page in the way of the title, as the page_id conflict above does:
		// the fix is usually to look at it and pick a different title.
		return record{}, fmt.Errorf("a page titled %q already exists in space %s: %s",
			title, spaceKey, pageURL(c, &dupes[0], dupes[0].ID))
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
			// An in-set parent is a page this run creates, so its kind is known
			// without asking the server.
			return parentInfo{kind: "inset", abs: parentAbs, parentType: "page", display: parentValue}, nil
		}
		pmf, err := frontmatter.ParseFile(parentPath)
		if err != nil {
			return parentInfo{}, err
		}
		pID := pmf.PageID()
		if pID == "" {
			return parentInfo{}, fmt.Errorf("parent not yet published (no page_id): %s", parentValue)
		}
		parentType, err := checkParentInSpace(c, pID, spaceID)
		if err != nil {
			return parentInfo{}, err
		}
		return parentInfo{kind: "published", id: pID, parentType: parentType, display: parentValue}, nil
	}

	parentType, err := checkParentInSpace(c, parentValue, spaceID)
	if err != nil {
		return parentInfo{}, err
	}
	return parentInfo{kind: "external", id: parentValue, parentType: parentType}, nil
}

// checkParentInSpace verifies that parentID names something in spaceID that can
// hold a page, and reports what it is ("page" or "folder").
//
// A parent may be either, and the two live in separate v2 route families: a
// folder id answers every page route with 404, so finding nothing as a page
// proves nothing until the folder route has also been asked. Publishing into a
// folder needs no other accommodation — Confluence accepts a folder as parentId
// — so the only thing that ever blocked it was refusing it here
// (docs/confluence/folders.md).
func checkParentInSpace(c *client.ConfluenceClient, parentID, spaceID string) (string, error) {
	p, err := c.GetPageOrNil(parentID)
	if err != nil {
		return "", err
	}
	if p != nil {
		if p.SpaceID != spaceID {
			return "", fmt.Errorf("parent page %s is not in the target space", parentID)
		}
		return "page", nil
	}

	f, err := c.GetFolderOrNil(parentID)
	if err != nil {
		return "", err
	}
	if f != nil {
		if f.SpaceID != spaceID {
			return "", fmt.Errorf("parent folder %s is not in the target space", parentID)
		}
		return "folder", nil
	}

	// Neither kind, so "page" would be the wrong noun in the error.
	return "", fmt.Errorf("parent %s not found: no page or folder has that id", parentID)
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

// createOne creates one page and returns a result. It performs no output; the
// caller renders the result.
func createOne(r record, parentID string, c *client.ConfluenceClient, persist bool) *createResult {
	res := newResult(r)
	res.parent = nullableStr(parentID)
	// parent_type tracks parent: both null for a top-level page, and both null in
	// a dry-run whose parent is an in-set page that has no id yet.
	if parentID != "" {
		res.parentType = nullableStr(r.parent.parentType)
	}

	// SiteURL, not BaseURL: rewritten links are published into the page, so they
	// must point at the site even when requests go through the gateway.
	pageContent, err := convert.MdToConfluence(r.mdfile, c.SiteURL(), r.spaceKey, buildinfo.Stamp())
	if err != nil {
		return res.fail(err, jsonout.CodeConvert)
	}
	res.broken = append(res.broken, pageContent.Broken...)
	res.warnings = append(res.warnings, pageContent.Warnings...)

	// --dry-run: preview without creating. The page has no id/URL yet (they stay
	// null); every attachment would be a fresh upload, and a new page always has
	// its width set. persisted reflects intent — dry_run signals nothing was
	// actually written.
	if dryRunOpt {
		for _, a := range pageContent.Attachments {
			res.attachments = append(res.attachments, jsonout.Attachment{Action: "created", Filename: a.Filename})
		}
		res.width = &jsonout.PageWidth{Value: string(r.width), Default: false}
		res.widthSet = true
		res.persisted = persist
		res.ok = true
		res.status = statusCreated
		return res
	}

	result, err := c.CreatePage(r.spaceID, r.title, pageContent.HTML, parentID)
	if err != nil {
		return res.fail(err, jsonout.CodeFor(err))
	}
	newID := result.ID
	res.pageID = newID
	res.url = pageURL(c, result, newID)

	actions, err := c.SyncAttachments(newID, toLocalAttachments(pageContent.Attachments))
	if err != nil {
		return res.fail(err, jsonout.CodeFor(err))
	}
	for _, a := range actions {
		res.attachments = append(res.attachments, jsonout.Attachment{Action: a.Action, Filename: a.Filename})
	}

	res.width = &jsonout.PageWidth{Value: string(r.width), Default: false}
	if acts, err := pagewidth.Apply(c, newID, r.width); err != nil {
		res.width = nil
		res.warnings = append(res.warnings, "could not set page width: "+err.Error())
	} else {
		for _, a := range acts {
			if a.Action == "set" {
				res.widthSet = true
				break
			}
		}
	}

	if persist {
		parentValue, parentComment := parentField(r.parent, parentID)
		content := r.mdfile.Content
		content = frontmatter.UpdateField(content, "title", r.title, "")
		content = frontmatter.UpdateField(content, "space", r.spaceKey, "")
		content = frontmatter.UpdateField(content, "parent", parentValue, parentComment)
		content = frontmatter.UpdateField(content, "page_id", newID, "")
		content = frontmatter.UpdateField(content, "page_width", string(r.width), "")
		if err := os.WriteFile(r.filename, []byte(content), 0o644); err != nil {
			return res.fail(err, jsonout.CodeIO)
		}
		res.persisted = true
	}

	res.ok = true
	res.status = statusCreated
	return res
}

// wantPersist resolves the --persist/--no-persist pair; --no-persist wins.
func wantPersist(persist, noPersist bool) bool { return persist && !noPersist }

// overrideNeedsSingleFile reports whether --title was given with anything other
// than exactly one FILE. --page-width and the persist toggle are batch-ok.
func overrideNeedsSingleFile(cliTitle string, nFiles int) bool {
	return cliTitle != "" && nFiles != 1
}

// resolveTitle returns the effective title: --title overrides the frontmatter.
func resolveTitle(cliTitle string, mf *frontmatter.MarkdownFile) string {
	if cliTitle != "" {
		return cliTitle
	}
	return mf.Title()
}

// resolveWidth returns the effective page width: --page-width overrides the
// frontmatter page_width, which defaults to max when unset.
func resolveWidth(cliPageWidth string, fm map[string]string) (pagewidth.Width, error) {
	if cliPageWidth != "" {
		return pagewidth.Declared(map[string]string{"page_width": cliPageWidth})
	}
	return pagewidth.Declared(fm)
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
