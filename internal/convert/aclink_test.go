package convert_test

import (
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/convert"
)

// The storage fragments here are real, taken from the survey of the 500
// most-recently-modified pages on mozilla-hub recorded in
// docs/confluence/links-and-anchors.md. Invented fragments would not have shown
// that an anchor is percent-encoded or that a card carries two extra attributes.

const pageURL = "https://mozilla-hub.atlassian.net/wiki/spaces/SRE/pages/2820571155/Support+runbook"

// TestACLinkResolvedPage covers every page-link shape that resolves to a URL.
func TestACLinkResolvedPage(t *testing.T) {
	links := map[convert.PageLinkTarget]string{
		{Title: "IT 2026 Roadmap"}:                              pageURL,
		{Title: "Felt Privacy Workstream", SpaceKey: "FIREFOX"}: pageURL,
	}

	for _, tc := range []struct {
		name    string
		storage string
		want    string
	}{
		{
			name: "plain",
			storage: `<p><ac:link><ri:page ri:content-title="IT 2026 Roadmap" ri:version-at-save="158" />` +
				`<ac:link-body>IT 2026 Roadmap</ac:link-body></ac:link></p>`,
			want: "[IT 2026 Roadmap](" + pageURL + ")",
		},
		{
			// An inline card loses its chip rendering and becomes a plain link.
			// The target is unchanged, which is what the mapping rule turns on.
			name: "inline card",
			storage: `<p><ac:link ac:local-id="8655d2740f7e" ac:card-appearance="inline">` +
				`<ri:page ri:content-title="IT 2026 Roadmap" /><ac:link-body>IT 2026 Roadmap` +
				`</ac:link-body></ac:link></p>`,
			want: "[IT 2026 Roadmap](" + pageURL + ")",
		},
		{
			// The anchor stays percent-encoded: it is going into a URL, and this
			// is the spelling Confluence itself writes.
			name: "cross-page anchor in another space",
			storage: `<p><ac:link ac:anchor="Workstream-2%3A-Cross-functional-%E2%80%9CHow-to-Felt-Privacy%E2%80%9D-guidance">` +
				`<ri:page ri:space-key="FIREFOX" ri:content-title="Felt Privacy Workstream" ri:version-at-save="34" />` +
				`<ac:link-body>guidance</ac:link-body></ac:link></p>`,
			want: "[guidance](" + pageURL +
				"#Workstream-2%3A-Cross-functional-%E2%80%9CHow-to-Felt-Privacy%E2%80%9D-guidance)",
		},
		{
			// Confluence displays the target's own title for a bodyless link.
			name:    "no body falls back to the title",
			storage: `<p><ac:link><ri:page ri:content-title="IT 2026 Roadmap" /></ac:link></p>`,
			want:    "[IT 2026 Roadmap](" + pageURL + ")",
		},
		{
			name: "CDATA body",
			storage: `<p><ac:link><ri:page ri:content-title="IT 2026 Roadmap" />` +
				`<ac:plain-text-link-body><![CDATA[the roadmap]]></ac:plain-text-link-body></ac:link></p>`,
			want: "[the roadmap](" + pageURL + ")",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := convert.StorageToMarkdown(tc.storage, convert.StorageOptions{PageLinks: links})
			if err != nil {
				t.Fatalf("StorageToMarkdown: %v", err)
			}
			if strings.TrimSpace(got) != tc.want {
				t.Errorf("got %q, want %q", strings.TrimSpace(got), tc.want)
			}
		})
	}
}

// TestACLinkCoalescesSplitBoldMark covers the same editor-induced mark split
// coalesceSplitMarks repairs for <a> (storage_to_md_test.go), but for an
// internal <ac:link>: bold text followed by a bold internal page link comes
// back from Confluence's editor as two adjacent runs sharing the mark instead
// of one nested element -- <strong>text </strong><ac:link>...<ac:link-body>
// <strong>y</strong></ac:link-body></ac:link> -- because the link's visible
// text lives inside ac:link-body, one level deeper than <a>'s. Verified live
// 2026-08-30 the same way as the <a> case: a direct atlas_doc_format PUT with
// the link mark's href pointing at another Confluence page.
func TestACLinkCoalescesSplitBoldMark(t *testing.T) {
	storage := `<p><strong>some text </strong><ac:link><ri:page ri:content-title="IT 2026 Roadmap" />` +
		`<ac:link-body><strong>y</strong></ac:link-body></ac:link></p>`
	want := "**some text [y](" + pageURL + ")**"

	got, err := convert.StorageToMarkdown(storage, convert.StorageOptions{
		PageLinks: map[convert.PageLinkTarget]string{{Title: "IT 2026 Roadmap"}: pageURL},
	})
	if err != nil {
		t.Fatalf("StorageToMarkdown: %v", err)
	}
	if strings.TrimSpace(got) != want {
		t.Errorf("got %q, want %q", strings.TrimSpace(got), want)
	}
}

// TestACLinkUnresolvedPageIsPassedThrough is the fallback that keeps a failed or
// skipped lookup from silently deleting a link. A markdown link with no
// destination would be worse than the storage, which still works.
func TestACLinkUnresolvedPageIsPassedThrough(t *testing.T) {
	const storage = `<p><ac:link><ri:page ri:content-title="Nowhere" />` +
		`<ac:link-body>x</ac:link-body></ac:link></p>`

	// A resolved map that simply does not hold this target -- the shape of both
	// a lookup that found nothing and a lookup that failed.
	got, err := convert.StorageToMarkdown(storage, convert.StorageOptions{
		PageLinks: map[convert.PageLinkTarget]string{{Title: "Somewhere Else"}: pageURL},
	})
	if err != nil {
		t.Fatalf("StorageToMarkdown: %v", err)
	}
	if !strings.Contains(got, "<ac:link>") {
		t.Errorf("expected raw storage passthrough, got:\n%s", got)
	}
}

// TestACLinkSpaceKeyDistinguishesTargets: two pages can share a title in
// different spaces, so the space key has to be part of the lookup key. If it
// were dropped, a link into another space would resolve to the wrong page --
// a wrong answer, which is worse than the passthrough a miss produces.
func TestACLinkSpaceKeyDistinguishesTargets(t *testing.T) {
	const storage = `<p><ac:link><ri:page ri:space-key="OTHER" ri:content-title="Runbook" />` +
		`<ac:link-body>x</ac:link-body></ac:link></p>`

	got, err := convert.StorageToMarkdown(storage, convert.StorageOptions{
		PageLinks: map[convert.PageLinkTarget]string{{Title: "Runbook"}: pageURL},
	})
	if err != nil {
		t.Fatalf("StorageToMarkdown: %v", err)
	}
	if strings.Contains(got, pageURL) {
		t.Errorf("a same-titled page in another space was used as the target:\n%s", got)
	}
}

// TestACLinkSpace: a space link needs no lookup, only the site base.
func TestACLinkSpace(t *testing.T) {
	const storage = `<p><ac:link><ri:space ri:space-key="SRE" />` +
		`<ac:link-body>SRE team page</ac:link-body></ac:link></p>`
	const site = "https://mozilla-hub.atlassian.net"

	got, err := convert.StorageToMarkdown(storage, convert.StorageOptions{SiteURL: site})
	if err != nil {
		t.Fatalf("StorageToMarkdown: %v", err)
	}
	if want := "[SRE team page](" + site + "/wiki/spaces/SRE)"; strings.TrimSpace(got) != want {
		t.Errorf("got %q, want %q", strings.TrimSpace(got), want)
	}

	// Without a site base there is no URL to write, so it passes through.
	got, err = convert.StorageToMarkdown(storage, convert.StorageOptions{})
	if err != nil {
		t.Fatalf("StorageToMarkdown: %v", err)
	}
	if !strings.Contains(got, "<ri:space") {
		t.Errorf("expected passthrough without a site URL, got:\n%s", got)
	}
}

// TestACLinkSamePageAnchorDecodesBeforeMatching is the case that proves the
// order: the anchor is percent-encoded and the heading is not, so matching
// without decoding first would miss every anchor containing punctuation and
// silently pass the link through.
func TestACLinkSamePageAnchorDecodesBeforeMatching(t *testing.T) {
	const storage = `<h2>Workstream 2: Cross-functional &ldquo;How to Felt Privacy&rdquo; guidance</h2>` +
		`<p><ac:link ac:anchor="Workstream-2%3A-Cross-functional-%E2%80%9CHow-to-Felt-Privacy%E2%80%9D-guidance">` +
		`<ac:link-body>see above</ac:link-body></ac:link></p>`

	got, err := convert.StorageToMarkdown(storage, convert.StorageOptions{})
	if err != nil {
		t.Fatalf("StorageToMarkdown: %v", err)
	}
	want := "[see above](#workstream-2-cross-functional-how-to-felt-privacy-guidance)"
	if !strings.Contains(got, want) {
		t.Errorf("got:\n%s\nwant it to contain %q", got, want)
	}
}

// TestPageLinkTargets is the contract between the converter and the caller doing
// the lookups: everything the renderer might resolve has to be reported, or the
// link passes through for want of a URL nobody asked for.
func TestPageLinkTargets(t *testing.T) {
	const storage = `<p><ac:link><ri:page ri:content-title="A" /></ac:link>` +
		`<ac:link><ri:page ri:space-key="ENG" ri:content-title="A" /></ac:link>` +
		`<ac:link><ri:page ri:content-title="A" /></ac:link>` +
		`<ac:link><ri:user ri:account-id="x" /></ac:link>` +
		`<ac:structured-macro ac:name="pagetree"><ac:parameter ac:name="root">` +
		`<ac:link><ri:page ri:content-title="B" /></ac:link></ac:parameter></ac:structured-macro></p>`

	got := convert.PageLinkTargets(storage)
	want := []convert.PageLinkTarget{
		{Title: "A"},
		{SpaceKey: "ENG", Title: "A"},
		// Inside a macro, so the renderer will serialize rather than convert it.
		// Reported anyway: one wasted lookup beats two copies of the macro rules.
		{Title: "B"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("target %d: got %v, want %v", i, got[i], want[i])
		}
	}
}

// TestPageLinkTargetsIgnoresBodiesWithNone keeps the caller's cheap path honest:
// a body with no page link must ask for nothing, since the lookup costs a
// request per target.
func TestPageLinkTargetsIgnoresBodiesWithNone(t *testing.T) {
	if got := convert.PageLinkTargets(`<p>plain <strong>text</strong></p>`); len(got) != 0 {
		t.Errorf("got %v, want none", got)
	}
}

// TestLinkTextEscapesBracketsInRawText covers a bug older than mentions: mdLink
// was a bare Sprintf, so any page title holding a "]" exported as a broken
// link -- the "]" ended the link text early and the rest of the line became
// literal junk. Mentions turn it from theoretical into likely, since display
// names carry brackets.
func TestLinkTextEscapesBracketsInRawText(t *testing.T) {
	storage := `<p>See <ac:link><ri:page ri:content-title="Deploy [staging] Runbook" ` +
		`ri:space-key="ENG" /></ac:link>.</p>`
	got, err := convert.StorageToMarkdown(storage, convert.StorageOptions{
		SiteURL: "https://wiki.example.net",
		PageLinks: map[convert.PageLinkTarget]string{
			{SpaceKey: "ENG", Title: "Deploy [staging] Runbook"}: "https://wiki.example.net/wiki/x",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `[Deploy \[staging\] Runbook](https://wiki.example.net/wiki/x)`) {
		t.Errorf("markdown = %q, want the brackets escaped in the link text", got)
	}
}

// TestLinkTextDoesNotEscapeARenderedBody is the other half, and the reason the
// escaping sits on the raw sources rather than in mdLink. An ac:link-body has
// already been rendered to markdown, so escaping it would turn a bold body into
// literal asterisks.
func TestLinkTextDoesNotEscapeARenderedBody(t *testing.T) {
	storage := `<p>See <ac:link><ri:page ri:content-title="Runbook" ri:space-key="ENG" />` +
		`<ac:link-body><strong>the runbook</strong></ac:link-body></ac:link>.</p>`
	got, err := convert.StorageToMarkdown(storage, convert.StorageOptions{
		SiteURL: "https://wiki.example.net",
		PageLinks: map[convert.PageLinkTarget]string{
			{SpaceKey: "ENG", Title: "Runbook"}: "https://wiki.example.net/wiki/x",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `[**the runbook**](https://wiki.example.net/wiki/x)`) {
		t.Errorf("markdown = %q, want the rendered body left alone", got)
	}
	if strings.Contains(got, `\*`) {
		t.Errorf("markdown = %q, want no escaped asterisks", got)
	}
}

// TestLinkTextEscapesABackslashFirst: the backslash pass has to run before the
// bracket passes, or it would escape the escapes they add.
func TestLinkTextEscapesABackslashFirst(t *testing.T) {
	if got := convert.EscapeLinkTextForTest(`a\b]c`); got != `a\\b\]c` {
		t.Errorf("EscapeLinkText = %q, want %q", got, `a\\b\]c`)
	}
}

// --- mentions -----------------------------------------------------------------

const probeID = "712020:0e5f8a21-3c4d-4e5f-a6b7-c8d9e0f1a2b3"

func mentionStorage(extra string) string {
	return `<p>Ping <ac:link><ri:user ri:account-id="` + probeID + `"` + extra +
		` /></ac:link> about it.</p>`
}

// TestMentionRendersAsAProfileLink is the point of #91: a mention exported as
// raw storage tells a reader everything except the one thing they want, which
// is who.
func TestMentionRendersAsAProfileLink(t *testing.T) {
	got, err := convert.StorageToMarkdown(mentionStorage(""), convert.StorageOptions{
		UserNames: map[string]string{probeID: "Ada Lovelace"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "Ping [@Ada Lovelace](https://home.atlassian.com/people/" + probeID + ") about it."
	if !strings.Contains(got, want) {
		t.Errorf("markdown = %q, want it to contain %q", got, want)
	}
}

// TestMentionURLNamesNoSite pins the property the whole spelling rests on: the
// profile URL lives on Atlassian Home, so a mention in markdown carries no
// site, no cloud id, and nothing about which instance it came from. That is
// what makes the output identical everywhere and what lets check recognise a
// mention with no client.
func TestMentionURLNamesNoSite(t *testing.T) {
	got, err := convert.StorageToMarkdown(mentionStorage(""), convert.StorageOptions{
		SiteURL:   "https://wiki.example.net",
		UserNames: map[string]string{probeID: "Ada Lovelace"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "wiki.example.net") {
		t.Errorf("markdown = %q, want no site in a mention URL", got)
	}
	if !strings.Contains(got, "https://home.atlassian.com/people/") {
		t.Errorf("markdown = %q, want the Atlassian Home profile URL", got)
	}
}

// TestMentionThreeStates pins the distinction that keeps a fabricated name out
// of somebody's file.
//
// A *confirmed* absence renders a placeholder, because the id is still the
// useful part and a reader should not have to read XML to find it. A lookup
// that could not be made renders nothing new at all -- passthrough -- because
// writing "Unlicensed user" over a real name the moment a VPN drops would put
// it across every page of an export, in a file that then looks authoritative.
func TestMentionThreeStates(t *testing.T) {
	tests := []struct {
		name  string
		names map[string]string
		want  string
	}{
		{"resolved", map[string]string{probeID: "Ada Lovelace"}, "[@Ada Lovelace]("},
		{"confirmed absent", map[string]string{probeID: ""}, "[@Unlicensed user]("},
		{"lookup not made", nil, "<ri:user"},
		{"other ids only", map[string]string{"someone-else": "Bo"}, "<ri:user"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := convert.StorageToMarkdown(mentionStorage(""),
				convert.StorageOptions{UserNames: tt.names})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(got, tt.want) {
				t.Errorf("markdown = %q, want it to contain %q", got, tt.want)
			}
		})
	}
}

// TestDeactivatedAccountKeepsItsName is the measurement that shaped the
// placeholder. Surveying every mention on a real page -- 18 of them, six
// departed -- the user lookup answered 200 for all 18, returning names like
// "Mark Reid (Deactivated)": Confluence appends the suffix itself. So a
// departed colleague keeps their name and never reaches the placeholder, which
// is why the placeholder mirrors Confluence's wording for the case that does.
func TestDeactivatedAccountKeepsItsName(t *testing.T) {
	got, err := convert.StorageToMarkdown(mentionStorage(""), convert.StorageOptions{
		UserNames: map[string]string{probeID: "Mark Reid (Deactivated)"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "[@Mark Reid (Deactivated)](") {
		t.Errorf("markdown = %q, want the deactivated name kept verbatim", got)
	}
}

// TestMentionDropsTheLocalID: ri:local-id is a per-instance server id, and a
// mention published with only the account id resolves to the same person --
// verified against the live API (docs/confluence/links-and-anchors.md). So it
// does not survive into the markdown, and nothing is lost.
func TestMentionDropsTheLocalID(t *testing.T) {
	storage := mentionStorage(` ri:local-id="4df0b1cc-1111-2222-3333-444455556666"`)
	got, err := convert.StorageToMarkdown(storage, convert.StorageOptions{
		UserNames: map[string]string{probeID: "Ada Lovelace"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "local-id") || strings.Contains(got, "4df0b1cc") {
		t.Errorf("markdown = %q, want the local-id gone", got)
	}
}

// TestMentionEscapesTheDisplayName: a name is plain text off the server, and
// people's names carry brackets.
func TestMentionEscapesTheDisplayName(t *testing.T) {
	got, err := convert.StorageToMarkdown(mentionStorage(""), convert.StorageOptions{
		UserNames: map[string]string{probeID: "Ada [contractor] Lovelace"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `[@Ada \[contractor\] Lovelace](`) {
		t.Errorf("markdown = %q, want the brackets escaped", got)
	}
}

// TestMentionTargetsDedupes pins the gather half: one lookup per distinct
// person, not per mention, which on a rotation table is the difference between
// a dozen requests and a hundred.
func TestMentionTargetsDedupes(t *testing.T) {
	other := "60c36d0718e9f60071326951"
	storage := mentionStorage("") + mentionStorage("") +
		`<p><ac:link><ri:user ri:account-id="` + other + `" /></ac:link></p>` +
		`<p><ac:link><ri:page ri:content-title="Not a user" /></ac:link></p>`
	got := convert.MentionTargets(storage)
	if len(got) != 2 {
		t.Fatalf("MentionTargets = %q, want two distinct ids", got)
	}
	if got[0] != probeID || got[1] != other {
		t.Errorf("MentionTargets = %q, want document order", got)
	}
}

func TestMentionTargetsIgnoresStorageWithNoMention(t *testing.T) {
	if got := convert.MentionTargets(`<p>Nothing here.</p>`); got != nil {
		t.Errorf("MentionTargets = %q, want nil so the caller makes no request", got)
	}
}
