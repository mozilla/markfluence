package pagedoc

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/clienttest"
)

func TestRenderFrontmatter(t *testing.T) {
	// Fields come out in the canonical order (title, space, parent, page_id, then
	// the rest) regardless of the order renderFrontmatter writes them.
	got := RenderFrontmatter("My Page", "ENG", "456", "123456", "max", nil)
	want := "---\ntitle: My Page\nspace: ENG\nparent: 456\npage_id: 123456\npage_width: max\n---\n"
	if got != want {
		t.Errorf("RenderFrontmatter =\n%q\nwant\n%q", got, want)
	}
}

func TestRenderFrontmatterTopLevelParent(t *testing.T) {
	// A top-level page carries parent: null.
	got := RenderFrontmatter("T", "ENG", "null", "1", "max", nil)
	want := "---\ntitle: T\nspace: ENG\nparent: null\npage_id: 1\npage_width: max\n---\n"
	if got != want {
		t.Errorf("RenderFrontmatter =\n%q\nwant\n%q", got, want)
	}
}

func TestRenderFrontmatterOmitsEmptyFields(t *testing.T) {
	got := RenderFrontmatter("T", "", "", "1", "", nil)
	want := "---\ntitle: T\npage_id: 1\n---\n"
	if got != want {
		t.Errorf("RenderFrontmatter =\n%q\nwant\n%q", got, want)
	}
}

func TestRenderFrontmatterQuotesWhenNeeded(t *testing.T) {
	// A title with a leading '#' would be read as a comment unless quoted.
	got := RenderFrontmatter("# Sharp", "", "", "1", "", nil)
	want := "---\ntitle: \"# Sharp\"\npage_id: 1\n---\n"
	if got != want {
		t.Errorf("RenderFrontmatter =\n%q\nwant\n%q", got, want)
	}
}

// --- Sources -----------------------------------------------------------------

func TestSourcesFrom(t *testing.T) {
	managed := client.Attachment{Title: "x.png"}
	managed.Metadata.Comment = "markfluence: sha256=abc path=assets/x.png"
	legacy := client.Attachment{Title: "assets_x.png"}
	legacy.Metadata.Comment = "mzcld:checksum: abc"
	hand := client.Attachment{Title: "notes.pdf"}

	got := SourcesFrom([]client.Attachment{managed, legacy, hand})
	if len(got) != 1 {
		t.Fatalf("got %d sources, want 1: %v", len(got), got)
	}
	if got["x.png"] != "assets/x.png" {
		t.Errorf("sources = %v", got)
	}
	// An attachment with no recorded source contributes nothing, so the converter
	// falls back to decoding its name rather than being handed a wrong answer.
	for _, absent := range []string{"assets_x.png", "notes.pdf"} {
		if _, ok := got[absent]; ok {
			t.Errorf("%s should not have a recorded source", absent)
		}
	}
}

// TestSourcesSkipsLookupWithoutReferences pins the optimization: a page with no
// attachment references must not trigger an API call. The nil client would
// panic if one were attempted.
func TestSourcesSkipsLookupWithoutReferences(t *testing.T) {
	page := &client.Page{ID: "1"}
	page.Body.Storage.Value = "<p>no attachments here</p>"
	if got := Sources(nil, page); got != nil {
		t.Errorf("Sources = %v, want nil without any ri:attachment", got)
	}
}

func TestDocString(t *testing.T) {
	d := Doc{Frontmatter: "---\ntitle: T\n---\n", Body: "# T\n"}
	if want := "---\ntitle: T\n---\n\n# T\n"; d.String() != want {
		t.Errorf("String() = %q, want %q", d.String(), want)
	}
}

// TestRenderFrontmatterEmitsLabels pins the field's place in the block: labels
// is not in fieldOrder, so it sorts alphabetically among the trailing keys and
// lands after page_id but before page_width.
func TestRenderFrontmatterEmitsLabels(t *testing.T) {
	got := RenderFrontmatter("T", "ENG", "null", "1", "max", []string{"ci/cd", "runbook"})
	want := "---\ntitle: T\nspace: ENG\nparent: null\npage_id: 1\nlabels: [ci/cd, runbook]\npage_width: max\n---\n"
	if got != want {
		t.Errorf("RenderFrontmatter =\n%q\nwant\n%q", got, want)
	}
}

// TestRenderFrontmatterOmitsEmptyLabels: a page with no labels, and a page
// whose labels could not be read, both get no labels: key.
//
// Not "labels: []", which is what update reads as "remove every label". A read
// of a page that has none must not assert that on the author's behalf -- the
// round trip would then strip any label added in the UI in between.
func TestRenderFrontmatterOmitsEmptyLabels(t *testing.T) {
	for name, given := range map[string][]string{
		"fetch failed": nil,
		"none on page": {},
	} {
		got := RenderFrontmatter("T", "", "", "1", "", given)
		if strings.Contains(got, "labels") {
			t.Errorf("%s: RenderFrontmatter = %q, want no labels key", name, got)
		}
	}
}

// TestRenderFrontmatterQuotesALabelThatNeedsIt: labels go through the same
// verified writer as every other value, so a name YAML would misread is
// quoted. No valid Confluence label needs this -- every character that would
// break a flow sequence is in the server's reject set -- which is exactly why
// it is worth pinning that the general path is still being used.
func TestRenderFrontmatterQuotesALabelThatNeedsIt(t *testing.T) {
	got := RenderFrontmatter("T", "", "", "1", "", []string{"a,b"})
	if !strings.Contains(got, `"a,b"`) {
		t.Errorf("RenderFrontmatter = %q, want the comma-bearing label quoted", got)
	}
}

// --- user mentions ------------------------------------------------------------

const (
	mentionA = "712020:0e5f8a21-3c4d-4e5f-a6b7-c8d9e0f1a2b3"
	mentionB = "60c36d0718e9f60071326951"
)

// userServer answers the v1 user lookup and counts how many times each id was
// asked about.
func userServer(t *testing.T, names map[string]string) (*client.ConfluenceClient, map[string]int) {
	t.Helper()
	asked := map[string]int{}
	c := clienttest.New(t, func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("accountId")
		asked[id]++
		name, ok := names[id]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"No user found with key : null"}`))
			return
		}
		_, _ = fmt.Fprintf(w, `{"accountId":%q,"displayName":%q}`, id, name)
	})
	return c, asked
}

func mentionPage(id string, ids ...string) *client.Page {
	p := &client.Page{ID: id}
	body := ""
	for _, a := range ids {
		body += `<p><ac:link><ri:user ri:account-id="` + a + `" /></ac:link></p>`
	}
	p.Body.Storage.Value = body
	return p
}

// TestUserCacheAsksOncePerIDAcrossPages is the test the whole cache exists for,
// and the one nothing in the output would reveal. PageLinks builds its space-id
// map per page and Options is constructed per page, so a user map written the
// same way would re-resolve the same people on every page of a walk -- twelve
// names costing 2400 requests across 200 pages. Asserted on a request count,
// since the rendered markdown is identical either way.
func TestUserCacheAsksOncePerIDAcrossPages(t *testing.T) {
	c, asked := userServer(t, map[string]string{mentionA: "Ada Lovelace", mentionB: "Bo Peep"})
	users := NewUserCache()

	for i := range 5 {
		page := mentionPage(fmt.Sprint(i), mentionA, mentionB)
		opts := Options(c, page, Placement{}, users)
		if opts.UserNames[mentionA] != "Ada Lovelace" {
			t.Fatalf("page %d: names = %v", i, opts.UserNames)
		}
	}
	if asked[mentionA] != 1 || asked[mentionB] != 1 {
		t.Errorf("lookups = %v, want exactly one per id across five pages", asked)
	}
}

// TestUserCacheDoesNotRetryAMiss: an id that does not resolve is cached as the
// miss it is, or a page mentioning deactivated people costs a request each,
// every page, to learn the same failures.
func TestUserCacheDoesNotRetryAMiss(t *testing.T) {
	c, asked := userServer(t, map[string]string{})
	users := NewUserCache()

	for i := range 4 {
		page := mentionPage(fmt.Sprint(i), mentionA)
		names := Options(c, page, Placement{}, users).UserNames
		// Present with an empty value: a *confirmed* absence, which is a
		// settled answer and renders a placeholder. Distinct from absent,
		// which means the lookup could not be made.
		name, settled := names[mentionA]
		if !settled || name != "" {
			t.Fatalf("page %d: names = %v, want a confirmed absence", i, names)
		}
	}
	if asked[mentionA] != 1 {
		t.Errorf("lookups = %v, want the miss asked once and remembered", asked)
	}
}

// TestUserCacheMakesNoRequestWithoutAMention is the guard every other lookup in
// this package has: a page with nothing to resolve costs nothing.
func TestUserCacheMakesNoRequestWithoutAMention(t *testing.T) {
	c, asked := userServer(t, map[string]string{mentionA: "Ada Lovelace"})
	page := &client.Page{ID: "1"}
	page.Body.Storage.Value = "<p>Nobody is mentioned here.</p>"

	if names := Options(c, page, Placement{}, NewUserCache()).UserNames; names != nil {
		t.Errorf("UserNames = %v, want nil", names)
	}
	if len(asked) != 0 {
		t.Errorf("lookups = %v, want none", asked)
	}
}

// TestNilUserCacheResolvesNothing: a nil cache renders every mention as
// passthrough rather than panicking, so a caller that has no use for names
// degrades instead of failing.
func TestNilUserCacheResolvesNothing(t *testing.T) {
	c, asked := userServer(t, map[string]string{mentionA: "Ada Lovelace"})
	page := mentionPage("1", mentionA)
	if names := Options(c, page, Placement{}, nil).UserNames; names != nil {
		t.Errorf("UserNames = %v, want nil", names)
	}
	if len(asked) != 0 {
		t.Errorf("lookups = %v, want none", asked)
	}
}

// TestMentionWarningsNamesAnUnresolvableID is the only signal that a mention
// reaches nobody. Confluence accepts any account id and renders it as
// "@Unlicensed user", and the profile URL 200s for a real id and a nonsense one
// alike -- both verified against the live instance -- so if markfluence stays
// quiet, nothing downstream ever speaks up.
func TestMentionWarningsNamesAnUnresolvableID(t *testing.T) {
	c, _ := userServer(t, map[string]string{mentionB: "Bo Peep"})
	got := MentionWarnings(c, NewUserCache(), []string{mentionA, mentionB})

	if len(got) != 1 {
		t.Fatalf("warnings = %q, want one (only the unresolvable id)", got)
	}
	if !strings.Contains(got[0], mentionA) {
		t.Errorf("warning = %q, want it to name the id", got[0])
	}
	if !strings.Contains(got[0], "Unlicensed user") {
		t.Errorf("warning = %q, want it to say what the reader will see", got[0])
	}
}

// TestMentionWarningsDedupe: a rotation table mentioning one person in twelve
// rows should say so once. resolve dedupes the *requests* via the cache, which
// is a different thing from deduping the warnings.
func TestMentionWarningsDedupe(t *testing.T) {
	c, asked := userServer(t, map[string]string{})
	ids := []string{mentionA, mentionA, mentionA}
	if got := MentionWarnings(c, NewUserCache(), ids); len(got) != 1 {
		t.Errorf("warnings = %q, want one for three mentions of one person", got)
	}
	if asked[mentionA] != 1 {
		t.Errorf("lookups = %v, want one", asked)
	}
}

// TestMentionWarningsSilentWhenEverythingResolves, and silent with nothing to
// check -- a document with no mention must not cost a request.
func TestMentionWarningsSilentWhenEverythingResolves(t *testing.T) {
	c, asked := userServer(t, map[string]string{mentionA: "Ada Lovelace"})
	if got := MentionWarnings(c, NewUserCache(), []string{mentionA}); got != nil {
		t.Errorf("warnings = %q, want none", got)
	}
	if got := MentionWarnings(c, NewUserCache(), nil); got != nil {
		t.Errorf("warnings = %q, want none for a document with no mention", got)
	}
	if asked[mentionA] != 1 {
		t.Errorf("lookups = %v, want exactly one", asked)
	}
}

// TestMentionWarningsShareTheCacheWithRendering is what makes the warning
// affordable at all: publishing needs no display names, so this lookup exists
// purely for the warning, and one cache across a batch means one request per
// person rather than one per file.
func TestMentionWarningsShareTheCacheWithRendering(t *testing.T) {
	c, asked := userServer(t, map[string]string{})
	users := NewUserCache()
	for range 6 {
		MentionWarnings(c, users, []string{mentionA})
	}
	if asked[mentionA] != 1 {
		t.Errorf("lookups = %v, want one across six files", asked)
	}
}
