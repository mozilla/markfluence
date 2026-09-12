package convert_test

import (
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/convert"
)

const mentionID = "712020:0e5f8a21-3c4d-4e5f-a6b7-c8d9e0f1a2b3"

// mentioned converts body and returns the storage plus the mentions reported,
// through roundtrip_test.go's publishPage so there is one conversion helper
// rather than two that can drift.
func mentioned(t *testing.T, body string) (string, []string) {
	t.Helper()
	page := publishPage(t, body)
	return page.HTML, page.Mentions
}

// TestMentionPublishesAsRiUser is the forward half of #91. Without it the
// feature is a lossy rendering, and #88's rule says leave it as passthrough --
// so this is what makes converting a mention legitimate at all.
func TestMentionPublishesAsRiUser(t *testing.T) {
	html, mentions := mentioned(t, "Ping [@Ada Lovelace](https://home.atlassian.com/people/"+mentionID+") now.\n")
	want := `<ac:link><ri:user ri:account-id="` + mentionID + `" /></ac:link>`
	if !strings.Contains(html, want) {
		t.Errorf("html = %q, want it to contain %q", html, want)
	}
	if strings.Contains(html, "<a href") {
		t.Errorf("html = %q, want no <a> element for a mention", html)
	}
	if len(mentions) != 1 || mentions[0] != mentionID {
		t.Errorf("Mentions = %q, want the id reported for the caller to validate", mentions)
	}
}

// TestMentionNeedsTheAtMarker is the decision the "@" exists for. The URL
// cannot tell "mention this person" from "link to this person's profile" --
// both point at the same place -- so without the marker anyone who deliberately
// wrote the second would silently get the first.
func TestMentionNeedsTheAtMarker(t *testing.T) {
	html, mentions := mentioned(t,
		"See [Ada's profile](https://home.atlassian.com/people/"+mentionID+") for details.\n")
	if strings.Contains(html, "ri:user") {
		t.Errorf("html = %q, want a plain link without the @ marker", html)
	}
	if !strings.Contains(html, "<a href") {
		t.Errorf("html = %q, want an <a> element", html)
	}
	if len(mentions) != 0 {
		t.Errorf("Mentions = %q, want none", mentions)
	}
}

// TestMentionIgnoresHostAndQuery is the host-agnostic decision, stated as the
// test. Every row here is a spelling of the same target that somebody will
// paste into a file: what markfluence emits, what Confluence's person modal
// copies, the redirect target, what Confluence's own renderer emits, the legacy
// form, and the root-relative ones.
func TestMentionIgnoresHostAndQuery(t *testing.T) {
	dests := []string{
		"https://home.atlassian.com/people/" + mentionID,
		"https://home.atlassian.com/people/" + mentionID + "?cloudId=d8febd08",
		"https://home.atlassian.com/o/b6dckb26/people/" + mentionID + "?ref=confluence&cloudId=d8feb",
		"https://mozilla-hub.atlassian.net/wiki/people/" + mentionID,
		"https://mozilla-hub.atlassian.net/wiki/display/~" + mentionID,
		"/people/" + mentionID,
		"/wiki/people/" + mentionID,
	}
	for _, dest := range dests {
		html, mentions := mentioned(t, "Ping [@Ada]("+dest+") now.\n")
		if len(mentions) != 1 || mentions[0] != mentionID {
			t.Errorf("dest %q: Mentions = %q, want the id", dest, mentions)
			continue
		}
		if !strings.Contains(html, `ri:account-id="`+mentionID+`"`) {
			t.Errorf("dest %q: html = %q", dest, html)
		}
	}
}

// TestMentionAcceptsBothIDShapes: two are live on one instance, so a pattern
// tight enough to describe one rejects the other. The id is whatever the last
// path segment is.
func TestMentionAcceptsBothIDShapes(t *testing.T) {
	for _, id := range []string{"60c36d0718e9f60071326951", mentionID} {
		_, mentions := mentioned(t, "Ping [@A](https://home.atlassian.com/people/"+id+") now.\n")
		if len(mentions) != 1 || mentions[0] != id {
			t.Errorf("id %q: Mentions = %q", id, mentions)
		}
	}
}

// TestMentionEmitsNoLocalID: ri:local-id is a per-instance server id, and a
// mention carrying only the account id resolves to the same person -- verified
// against the live API. Emitting one would mean inventing it.
func TestMentionEmitsNoLocalID(t *testing.T) {
	html, _ := mentioned(t, "Ping [@Ada](https://home.atlassian.com/people/"+mentionID+") now.\n")
	if strings.Contains(html, "local-id") {
		t.Errorf("html = %q, want no ri:local-id", html)
	}
}

// TestNonProfileLinkIsUntouched: an "@"-led link text must not turn any old URL
// into a mention.
func TestNonProfileLinkIsUntouched(t *testing.T) {
	for _, dest := range []string{
		"https://example.com/team",
		"https://home.atlassian.com/people/",
		"https://home.atlassian.com/people/a/b",
	} {
		html, mentions := mentioned(t, "Ping [@Ada]("+dest+") now.\n")
		if len(mentions) != 0 {
			t.Errorf("dest %q: Mentions = %q, want none", dest, mentions)
		}
		if !strings.Contains(html, "<a href") {
			t.Errorf("dest %q: html = %q, want a plain link", dest, html)
		}
	}
}

// TestMentionMarkerInsideAChildNodeStillCounts: reading only the link's first
// child would miss an "@" nested inside emphasis or code and publish a plain
// link instead. Both spellings are things an author would plausibly write.
//
// Note what is *not* here: "[*@*Ada](…)". That does not form emphasis at all
// under CommonMark's flanking rules -- the text is literally "*@*Ada", so it
// correctly does not publish as a mention. It was the first case written here,
// and it was testing goldmark's parser rather than this code.
func TestMentionMarkerInsideAChildNodeStillCounts(t *testing.T) {
	for _, text := range []string{"**@Ada**", "`@Ada`"} {
		_, mentions := mentioned(t,
			"Ping ["+text+"](https://home.atlassian.com/people/"+mentionID+") now.\n")
		if len(mentions) != 1 {
			t.Errorf("text %q: Mentions = %q, want the marker found in a child node", text, mentions)
		}
	}
}

// TestMentionRoundTripIsAFixedPoint is the guarantee that makes this a round
// trip rather than two independent conversions: once a page has been through
// markfluence, it stops moving.
//
// The first cycle *does* change the storage, and that is expected and
// converges: Confluence's editor writes ri:local-id and markfluence does not,
// so the republished storage is shorter. The same shape as the documented
// attachment exception in docs/guarantees.md -- a native attachment gets
// restamped once. What must hold is that cycle two onward changes nothing.
func TestMentionRoundTripIsAFixedPoint(t *testing.T) {
	names := map[string]string{mentionID: "Ada Lovelace"}
	editor := `<p>Ping <ac:link><ri:user ri:account-id="` + mentionID +
		`" ri:local-id="4df0b1cc-1111-2222-3333-444455556666" /></ac:link> about it.</p>`

	toMD := func(storage string) string {
		t.Helper()
		md, err := convert.StorageToMarkdown(storage, convert.StorageOptions{UserNames: names})
		if err != nil {
			t.Fatal(err)
		}
		return md
	}

	firstMD := toMD(editor)
	firstStorage, _ := mentioned(t, firstMD+"\n")
	secondMD := toMD(firstStorage)
	if firstMD != secondMD {
		t.Errorf("markdown moved between cycles:\n first: %q\nsecond: %q", firstMD, secondMD)
	}
	secondStorage, _ := mentioned(t, secondMD+"\n")
	if firstStorage != secondStorage {
		t.Errorf("storage moved between cycles:\n first: %q\nsecond: %q", firstStorage, secondStorage)
	}
	// The one-time convergence, asserted rather than assumed: the editor's
	// local-id is gone after the first republish and never comes back.
	if !strings.Contains(editor, "local-id") || strings.Contains(firstStorage, "local-id") {
		t.Errorf("expected the local-id dropped exactly once; got %q", firstStorage)
	}
}

// TestMentionRoundTripSurvivesAnUnresolvedName: when the name cannot be
// resolved the mention stays raw storage, and *that* has to be a fixed point
// too -- it is the common case for a deactivated account, and a page full of
// them must not churn on every export.
func TestMentionRoundTripSurvivesAnUnresolvedName(t *testing.T) {
	editor := `<p>Ping <ac:link><ri:user ri:account-id="` + mentionID + `" /></ac:link> about it.</p>`
	first, err := convert.StorageToMarkdown(editor, convert.StorageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	republished, _ := mentioned(t, first+"\n")
	second, err := convert.StorageToMarkdown(republished, convert.StorageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Errorf("unresolved mention moved:\n first: %q\nsecond: %q", first, second)
	}
	if !strings.Contains(first, "ri:user") {
		t.Errorf("markdown = %q, want the storage passed through", first)
	}
}
