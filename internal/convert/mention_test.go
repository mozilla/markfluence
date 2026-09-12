package convert_test

import (
	"strings"
	"testing"
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
