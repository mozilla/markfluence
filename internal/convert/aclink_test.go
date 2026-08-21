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
