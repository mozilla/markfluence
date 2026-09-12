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
	"github.com/mozilla/markfluence/internal/labels"
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
		"Populates/refreshes page_id, space, parent, page_width and labels (and\n" +
		"fills a missing title) from the live page. The page is located by page_id,\n" +
		"or by searching for the title when page_id is absent. fix never creates,\n" +
		"updates or moves pages -- it is read-only on the server. Each file is\n" +
		"processed independently; the command exits non-zero if any file failed.\n\n" +
		"It writes a file when a field changed, and also when the frontmatter keys\n" +
		"are out of canonical order (title, space, parent, page_id, then the rest\n" +
		"alphabetically), which is reported separately as reordered. --dry-run\n" +
		"reports both without writing.\n\n" +
		"Labels are reconciled even for a file with no labels: line, which is how\n" +
		"you adopt a page somebody labeled in the UI. That is the one place fix\n" +
		"fills in a field update would have left alone, because fix reconciles the\n" +
		"file to the page rather than the page to the file.\n\n" +
		"parent is written as the live page's parent id. In a tree written by\n" +
		"`export --depth`, where parent points at the parent's own .md file,\n" +
		"fix therefore replaces that path with an id -- consistent with\n" +
		"reconciling to the live page, and worth knowing before running it over\n" +
		"an exported tree.",
	Example: "  # Reconcile a batch of files to their live pages\n" +
		"  markfluence fix docs/*.md\n\n" +
		"  # Report what would change, write nothing\n" +
		"  markfluence fix docs/foo.md --dry-run",
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
//
// newList, when non-nil, makes this a list-valued field: newValue is then the
// display rendering ("[a, b]") that both the human line and --json's `new`
// string carry, and newList is what actually gets written. Keeping the display
// a string means the reported shape of a change does not vary by field, so the
// schema needs no new case for one list field.
type change struct {
	field, oldDisplay, newValue string
	newList                     []string
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
		// locatePage mixes server failures (GetPageOrNil, SearchPagesByTitle)
		// with local ones (no page_id or title, an ambiguous title), so the code
		// comes from the error's origin. This was fix's own locateCode, lifted
		// into jsonout when create needed the identical rule (#133); the
		// transport case is what a bare type check got wrong here too.
		return r.fail(err, jsonout.CodeOr(err, jsonout.CodeValidation))
	}
	r.pageID = page.ID

	// Read the live width to reconcile page_width; a read failure is non-fatal.
	liveWidth := ""
	if w, _, err := pagewidth.Read(c, page.ID); err != nil {
		r.warnings = append(r.warnings, "could not read page width: "+err.Error())
	} else {
		liveWidth = string(w)
	}

	// The live labels, likewise best-effort. nil means "not known", which is
	// distinct from an empty slice: a page with no labels and a page whose
	// labels could not be read must not plan the same change, or a failed read
	// would write `labels: []` and silently propose stripping the page.
	var liveLabels []string
	if live, err := labels.Read(c, page.ID); err != nil {
		r.warnings = append(r.warnings, "could not read labels: "+err.Error())
	} else {
		liveLabels = labels.Global(live)
		if liveLabels == nil {
			liveLabels = []string{}
		}
	}

	r.changes = plannedChanges(mf, page, liveWidth, liveLabels)
	// Field order is reconciled too, and counts as a change: reporting a
	// jumbled file "consistent" would mean running fix, being told there is
	// nothing to do, and still having a jumbled file. Computed before any edit,
	// which is stable because a surgical UpdateField never moves an existing key
	// and inserting before the first key that sorts after it cannot flip
	// canonicity either way.
	_, reordered, err := frontmatter.Normalize(mf.Content)
	if err != nil {
		return r.fail(err, jsonout.CodeValidation)
	}
	r.reordered = reordered
	if len(r.changes) == 0 && !r.reordered {
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
		var err error
		if ch.newList != nil {
			content, err = frontmatter.UpdateListField(content, ch.field, ch.newList)
		} else {
			content, err = frontmatter.UpdateField(content, ch.field, ch.newValue, "")
		}
		if err != nil {
			return r.fail(err, jsonout.CodeValidation)
		}
	}
	// Last, so a key inserted above lands in canonical position rather than
	// wherever the surgical insert put it.
	content, _, err = frontmatter.Normalize(content)
	if err != nil {
		return r.fail(err, jsonout.CodeValidation)
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

// plannedChanges computes the field edits needed to reconcile mf to page. Only
// fields that actually differ are returned.
func plannedChanges(
	mf *frontmatter.MarkdownFile, page *client.Page, liveWidth string, liveLabels []string,
) []change {
	fm := mf.Frontmatter
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
		case !present:
			changes = append(changes, change{field: lv.field, oldDisplay: "(none)", newValue: lv.value})
		case norm(current) != norm(lv.value):
			// A present-but-blank value goes through norm, not straight to
			// "(none)": every null spelling now parses to "", so a top-level
			// page's `parent: null` reads as "" and norm makes it equal to the
			// orNull("null") the live side reports. Short-circuiting on blank
			// would plan `parent: (none) -> null` on every run, write it, read
			// "" again, and never converge.
			changes = append(changes, change{field: lv.field, oldDisplay: orNone(current), newValue: lv.value})
		}
	}

	if strings.TrimSpace(fm["title"]) == "" {
		changes = append(changes, change{field: "title", oldDisplay: "(none)", newValue: page.Title})
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
			changes = append(changes, change{field: "page_width", oldDisplay: old, newValue: liveWidth})
		}
	}

	if ch, ok := labelChange(mf, liveLabels); ok {
		changes = append(changes, ch)
	}
	return changes
}

// labelChange plans the labels edit, if one is needed.
//
// This is the only way to adopt a page somebody labeled by hand, so it runs
// even for a file with no labels key at all -- unlike update and create, where
// an absent key means "leave the page alone". The directions are not symmetric
// and should not be: fix reconciles the *file* to the page, so the page is the
// authority here in exactly the way the file is there.
//
// Compared as sets, so a file whose list is merely reordered or holds a
// duplicate is left alone and keeps the author's own ordering. When a write is
// needed the list is emitted sorted and deduplicated, since neither label GET
// returns a useful order and anything else would be unstable across runs.
//
// A file whose labels are invalid is reconciled rather than refused: the live
// set is what is about to be written, and it came from the server, so it is
// valid by construction. That is the one place fix repairs a file check would
// have failed.
func labelChange(mf *frontmatter.MarkdownFile, liveLabels []string) (change, bool) {
	// nil means the read failed. Planning nothing is right: a change here would
	// propose the file's own labels be replaced by a set nobody could see.
	if liveLabels == nil {
		return change{}, false
	}
	declared, present := mf.Lists[labels.Field]
	// A scalar labels: value is a file every other verb refuses, so fix has to
	// offer a way out of it whatever the page's labels are -- including none,
	// where the repair is "labels: []". Reading it as absent meant fix reported
	// "already consistent" for a file check, update and create all reject, and
	// only repaired it when the page happened to carry labels.
	scalar := !present && hasKey(mf.Frontmatter, labels.Field)

	if present {
		// Compared raw, not normalized. Normalizing first made a case mismatch
		// invisible here, so `labels: [Runbook]` against a page carrying
		// `runbook` reported "already consistent" while update and check warned
		// "Update the file to match" on every run -- the command whose job is
		// to make the file match the page refusing to fix the thing the warning
		// names.
		if equalSets(declared, liveLabels) {
			return change{}, false
		}
	} else if !scalar && len(liveLabels) == 0 {
		// No key and no labels: nothing to adopt, and writing "labels: []"
		// would add a field that says nothing to every file fix touches.
		return change{}, false
	}

	old := noneDisplay
	switch {
	case present:
		old = renderLabelList(declared)
	case scalar:
		old = strings.TrimSpace(mf.Frontmatter[labels.Field])
		if old == "" {
			old = noneDisplay
		}
	}
	return change{
		field:      labels.Field,
		oldDisplay: old,
		newValue:   renderLabelList(liveLabels),
		newList:    liveLabels,
	}, true
}

// renderLabelList renders a label list the way the frontmatter writes it, for
// the human line and --json's `new` string.
func renderLabelList(names []string) string {
	return "[" + strings.Join(names, ", ") + "]"
}

// hasKey reports whether a frontmatter key is present at all, blank included.
func hasKey(fm map[string]string, key string) bool {
	_, ok := fm[key]
	return ok
}

// equalSets reports whether two label lists name the same set, exactly as
// Confluence would see them: order and duplicates carry no meaning, so a file
// that merely reorders its list needs no write. Case does not get normalized
// away first, because a file spelling a label differently from the page is
// precisely what fix exists to correct.
func equalSets(declared, live []string) bool {
	if len(live) != len(uniq(declared)) {
		return false
	}
	inLive := make(map[string]bool, len(live))
	for _, l := range live {
		inLive[l] = true
	}
	for _, d := range declared {
		if !inLive[d] {
			return false
		}
	}
	return true
}

func uniq(names []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(names))
	for _, n := range names {
		if seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out
}

// norm treats "", whitespace-only, and the literal "null" all as no value.
func norm(value string) string {
	t := strings.TrimSpace(value)
	if t == "" || t == "null" {
		return ""
	}
	return t
}

// orNone renders a frontmatter value for the "old" column, naming a blank as
// "(none)" the way an absent field is named.
func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(none)"
	}
	return s
}

func orNull(s string) string {
	if s == "" {
		return "null"
	}
	return s
}
