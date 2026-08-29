// Package create implements the `markfluence create` command: create new
// Confluence pages from markdown files. Creation is three-phase: every file is
// validated first (preflight); if all pass, a content-less stub is created
// for each, parents first, capturing every id (reserve); only then is every
// page converted and given real content (publish). Reserving every id before
// converting anything is what makes link resolution stop depending on
// creation order -- a link pointing "forward" in the batch resolves exactly
// like one pointing "backward," and a cycle between two pages in the same
// batch resolves too.
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
	"github.com/mozilla/markfluence/internal/linkindex"
	"github.com/mozilla/markfluence/internal/pageref"
	"github.com/mozilla/markfluence/internal/pagewidth"
	"github.com/mozilla/markfluence/internal/project"
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
		"Otherwise a content-less stub is reserved for each, parents-first, before\n" +
		"any of them is converted -- so a link between two files in the same batch\n" +
		"resolves regardless of which direction it points, or whether they form a\n" +
		"cycle. --title and --page-width override the frontmatter (--title requires\n" +
		"a single FILE). Unless --no-persist is given, each created page's\n" +
		"title/space/parent/page_id/page_width are written back into the frontmatter.",
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
	// root bounds this file's image/parent reads and is what its attachments'
	// names and recorded Source are relative to. Discovered from the file's own
	// directory, cached across the batch by internal/project.Cache.
	root *project.Root
	// index is the tree-wide link/anchor index for root, shared by every file
	// under the same root (internal/linkindex.Cache).
	index *linkindex.Index
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

// checkTitleFree reports an error when the title is already taken in the space.
//
// It asks for archived pages as well as current ones. An archived page is
// absent from the page tree but still reserves its title -- Confluence rejects
// the POST with "A page already exists with the same TITLE in this space"
// (docs/confluence/search.md). Checking only current pages let validation pass
// and the create fail, and in a batch that failure lands after earlier pages
// have already been created, which is exactly what the two-phase design exists
// to prevent.
//
// Folders are deliberately not checked. A folder does not reserve a title:
// creating a page with a folder's exact name in the same space succeeds.
func checkTitleFree(c *client.ConfluenceClient, title, spaceKey, spaceID string) error {
	dupes, err := c.SearchPagesByTitle(title, spaceID, client.StatusCurrent, client.StatusArchived)
	if err != nil {
		return err
	}
	if len(dupes) == 0 {
		return nil
	}
	// Link the page in the way of the title, as the page_id conflict does: the
	// fix is usually to look at it and pick a different title.
	d := dupes[0]
	if d.Status == client.StatusArchived {
		// Say it is archived, or the author goes looking in the page tree for a
		// page that is not there and concludes markfluence is wrong.
		return fmt.Errorf("an archived page titled %q already exists in space %s and still holds "+
			"that title; restore it, rename it, or pick another title: %s",
			title, spaceKey, pageURL(c, &d, d.ID))
	}
	return fmt.Errorf("a page titled %q already exists in space %s: %s",
		title, spaceKey, pageURL(c, &d, d.ID))
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
	rootOverride, _ := cmd.Flags().GetString("root")
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
	roots := project.NewCache(rootOverride)
	defer roots.Close()
	indexes := linkindex.NewCache()

	// Phase 1: validate every file, create nothing.
	var records []record
	var errs []failure
	for _, filename := range args {
		r, err := resolveFile(filename, c, inSetAbs, spaceCache, roots, indexes)
		if err != nil {
			errs = append(errs, newFailure(filename, err))
			continue
		}
		records = append(records, r)
	}
	for _, dir := range roots.Roots() {
		ui.Info("root: " + dir)
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

	results := createAll(ordered, c, doPersist)
	failures := 0
	for _, res := range results {
		if !res.ok {
			failures++
		}
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

// pendingPublish is what phase 3 needs for a record whose reservation
// succeeded: the in-progress result to finish, and the stub's id/version.
type pendingPublish struct {
	res     *createResult
	pageID  string
	version int
}

// createAll runs phases 2 and 3 over ordered (already topologically sorted):
// reserve a content-less stub for every file, then convert and publish every
// one that reserved successfully. Splitting these into two full passes -- not
// reserve-then-publish per file -- is the point: every id any file in the set
// might link to already exists by the time phase 3 converts anything, so link
// resolution stops depending on topological order the way it used to.
//
// Results come back in ordered's order, one per record, regardless of which
// phase produced the final outcome -- a reserve failure and a publish failure
// look the same to the caller.
func createAll(ordered []record, c *client.ConfluenceClient, doPersist bool) []*createResult {
	// Phase 2: reserve, in topological order (a page needs its parent's id at
	// creation time).
	created := map[string]string{} // absPath -> pageID, for resolving an in-set parent
	pending := map[string]pendingPublish{}
	final := map[string]*createResult{}

	for _, r := range ordered {
		parentID := r.parent.id
		if r.parent.kind == "inset" {
			parentID = created[r.parent.abs]
			// In a dry-run nothing is created, so an in-set parent has no id
			// yet; that is not a failure (the parent would have been created
			// first). The relationship is still reported via parent_file.
			if parentID == "" && !dryRunOpt {
				res := newResult(r)
				final[r.absPath] = res.fail(errors.New("parent page was not created; skipping"), jsonout.CodeValidation)
				continue
			}
		}

		res, pageID, version, ok := reserveOne(r, parentID, c, doPersist)
		if !ok {
			final[r.absPath] = res
			continue
		}
		created[r.absPath] = pageID
		if pageID != "" {
			// Seed the shared link index immediately, using the identical key
			// MdToConfluence would compute for this file -- so phase 3 sees
			// this id regardless of link direction or a cycle among the files
			// being created, and regardless of --no-persist (which skips the
			// frontmatter write but not this).
			r.index.SetPage(convert.DocKeyFor(r.root, r.filename), linkindex.PageEntry{PageID: pageID, Title: r.title})
		}
		pending[r.absPath] = pendingPublish{res: res, pageID: pageID, version: version}
	}

	// Phase 3: convert and publish every reserved page, now that every id any
	// of them might link to already exists.
	for _, r := range ordered {
		p, ok := pending[r.absPath]
		if !ok {
			continue
		}
		final[r.absPath] = publishOne(r, p.res, p.pageID, p.version, c)
	}

	results := make([]*createResult, len(ordered))
	for i, r := range ordered {
		results[i] = final[r.absPath]
	}
	return results
}

// reserveOne creates a content-less stub for r (title and parent, no body) and
// persists its frontmatter fields immediately unless persist is false -- so a
// run interrupted after this point has already recorded a page_id a later
// `update` can finish publishing against, rather than leaving the file
// unpublished with no trace. Under --dry-run nothing is created; ok is still
// true, since phase 3 has a preview to run even though there is no id.
//
// ok distinguishes "reservation failed outright" (the terminal result is res;
// phase 3 must not run) from "proceed to phase 3" -- which is not the same as
// res.ok, since res is not finished until publishOne finalizes it.
func reserveOne(
	r record, parentID string, c *client.ConfluenceClient, persist bool,
) (res *createResult, pageID string, version int, ok bool) {
	res = newResult(r)
	res.parent = nullableStr(parentID)
	// parent_type tracks parent: both null for a top-level page, and both null in
	// a dry-run whose parent is an in-set page that has no id yet.
	if parentID != "" {
		res.parentType = nullableStr(r.parent.parentType)
	}

	if dryRunOpt {
		res.persisted = persist
		return res, "", 0, true
	}

	result, err := c.CreatePage(r.spaceID, r.title, "", parentID)
	if err != nil {
		return res.fail(err, jsonout.CodeFor(err)), "", 0, false
	}
	pageID = result.ID
	res.pageID = pageID
	res.url = pageURL(c, result, pageID)

	if persist {
		parentValue, parentComment := parentField(r.parent, parentID)
		content := r.mdfile.Content
		content = frontmatter.UpdateField(content, "title", r.title, "")
		content = frontmatter.UpdateField(content, "space", r.spaceKey, "")
		content = frontmatter.UpdateField(content, "parent", parentValue, parentComment)
		content = frontmatter.UpdateField(content, "page_id", pageID, "")
		content = frontmatter.UpdateField(content, "page_width", string(r.width), "")
		if err := os.WriteFile(r.filename, []byte(content), 0o644); err != nil {
			return res.fail(err, jsonout.CodeIO), "", 0, false
		}
		res.persisted = true
	}

	return res, pageID, result.Version.Number, true
}

// publishOne converts r's body -- now against a fully-seeded link index -- and
// gives the page reserveOne created its real content: attachments and page
// width. It always finalizes res.ok/res.status, on both success and failure;
// a failure here leaves a permanent content-less stub behind, which
// _plans/026 accepts as the cost of removing the ordering dependency (an
// interrupted run leaves stubs where the old single-pass create left pages
// missing entirely -- uglier, but every id is already persisted, so a plain
// `markfluence update` finishes the job).
func publishOne(r record, res *createResult, pageID string, version int, c *client.ConfluenceClient) *createResult {
	// SiteURL, not BaseURL: rewritten links are published into the page, so they
	// must point at the site even when requests go through the gateway.
	pageContent, err := convert.MdToConfluence(r.mdfile, r.root, r.index, c.SiteURL(), r.spaceKey, buildinfo.Stamp())
	if err != nil {
		return res.fail(err, jsonout.CodeConvert)
	}
	res.broken = append(res.broken, pageContent.Broken...)
	res.warnings = append(res.warnings, pageContent.Warnings...)

	// --dry-run: preview without creating. The page has no id/URL (reserveOne
	// never created one); every attachment would be a fresh upload, and a new
	// page always has its width set.
	if dryRunOpt {
		for _, a := range pageContent.Attachments {
			res.attachments = append(res.attachments, jsonout.Attachment{Action: "created", Filename: a.Filename})
		}
		res.width = &jsonout.PageWidth{Value: string(r.width), Default: false}
		res.widthSet = true
		res.ok = true
		res.status = statusCreated
		return res
	}

	result, err := c.UpdatePage(pageID, r.title, pageContent.HTML, version+1, "Initial publish via markfluence")
	if err != nil {
		return res.fail(err, jsonout.CodeFor(err))
	}
	res.url = pageURL(c, result, pageID)

	actions, err := c.SyncAttachments(pageID, toLocalAttachments(pageContent.Attachments))
	if err != nil {
		return res.fail(err, jsonout.CodeFor(err))
	}
	for _, a := range actions {
		res.attachments = append(res.attachments, jsonout.Attachment{Action: a.Action, Filename: a.Filename})
	}

	res.width = &jsonout.PageWidth{Value: string(r.width), Default: false}
	if acts, err := pagewidth.Apply(c, pageID, r.width); err != nil {
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

	res.ok = true
	res.status = statusCreated
	return res
}

func resolveFile(
	filename string, c *client.ConfluenceClient, inSetAbs map[string]bool, spaceCache map[string]string,
	roots *project.Cache, indexes *linkindex.Cache,
) (record, error) {
	mf, err := frontmatter.ParseFile(filename)
	if err != nil {
		return record{}, err
	}

	abs, err := filepath.Abs(filename)
	if err != nil {
		return record{}, err
	}
	root, err := roots.Resolve(filepath.Dir(abs))
	if err != nil {
		return record{}, fmt.Errorf("resolving the documentation root: %w", err)
	}
	index, err := indexes.Get(root)
	if err != nil {
		return record{}, fmt.Errorf("building the link index: %w", err)
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

	parent, err := resolveParent(filename, mf.Frontmatter, inSetAbs, c, spaceID, root)
	if err != nil {
		return record{}, err
	}

	if err := checkTitleFree(c, title, spaceKey, spaceID); err != nil {
		return record{}, err
	}

	return record{filename, abs, mf, title, spaceKey, spaceID, parent, width, root, index}, nil
}

// resolveParent resolves a file's parent: reference. A ".md" reference is read
// through root's os.Root -- root.FS -- rather than the bare filesystem: a
// parent escaping root is a hard error (S2), not an unresolved-and-reported
// case the way a link is, because a parent is load-bearing. Publishing under
// the wrong parent -- or under none, silently -- is worse than not publishing
// at all. A symlinked parent target is refused the same way a symlinked image
// leaf is.
func resolveParent(
	filename string, fm map[string]string, inSetAbs map[string]bool, c *client.ConfluenceClient, spaceID string,
	root *project.Root,
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
		parentAbs, err := filepath.Abs(parentPath)
		if err != nil {
			return parentInfo{}, err
		}
		rel, err := filepath.Rel(root.Dir, parentAbs)
		if err != nil {
			return parentInfo{}, err
		}
		rel = filepath.ToSlash(rel)
		if rel == ".." || strings.HasPrefix(rel, "../") {
			return parentInfo{}, fmt.Errorf(
				"parent %s resolves outside the documentation root (%s); a parent must be within it",
				parentValue, root.Dir)
		}

		info, statErr := root.FS.Lstat(rel)
		if statErr != nil || info.IsDir() {
			return parentInfo{}, fmt.Errorf("parent file not found: %s", parentValue)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return parentInfo{}, fmt.Errorf("parent file is a symlink, not a regular file: %s", parentValue)
		}

		if inSetAbs[parentAbs] {
			// An in-set parent is a page this run creates, so its kind is known
			// without asking the server.
			return parentInfo{kind: "inset", abs: parentAbs, parentType: "page", display: parentValue}, nil
		}
		data, err := root.FS.ReadFile(rel)
		if err != nil {
			return parentInfo{}, err
		}
		pmf := frontmatter.Parse(parentPath, string(data))
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
