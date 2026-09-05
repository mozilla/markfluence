package export

import (
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/client"
)

func attWithSum(title, path, sum string) client.Attachment {
	a := client.Attachment{Title: title}
	a.Metadata.Comment = "markfluence: sha256=" + sum + " path=" + path
	return a
}

// TestClaimsSharedAssetIsQuiet is the model's success case: one asset referenced
// from several pages, each carrying its own attachment recording the same path.
// They all resolve to one file, the bytes match, and the second write skips.
func TestClaimsSharedAssetIsQuiet(t *testing.T) {
	c := newClaims()
	a := attWithSum("brand.png", "assets/brand.png", "abc123")
	if err := c.claim("/out/assets/brand.png", &client.Page{ID: "1"}, a); err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if err := c.claim("/out/assets/brand.png", &client.Page{ID: "2"}, a); err != nil {
		t.Errorf("identical bytes must not conflict: %v", err)
	}
}

// TestClaimsDifferingContentConflicts is the case worth reporting: two pages
// disagreeing about what one path holds. Skipping would leave whichever page
// was walked first deciding the contents.
func TestClaimsDifferingContentConflicts(t *testing.T) {
	c := newClaims()
	if err := c.claim("/out/assets/brand.png", &client.Page{ID: "1"},
		attWithSum("brand.png", "assets/brand.png", "aaa")); err != nil {
		t.Fatal(err)
	}
	err := c.claim("/out/assets/brand.png", &client.Page{ID: "2"},
		attWithSum("brand.png", "assets/brand.png", "bbb"))
	if err == nil {
		t.Fatal("want a conflict when two pages record one path with different content")
	}
	for _, want := range []string{"page 1", "brand.png"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// TestClaimsWithoutAChecksumAreNotConflicts keeps the rule to what it can
// decide. An attachment with no markfluence comment has no checksum, and it is
// also page-scoped -- so a meeting like this is the same shape as an attachment
// landing on a page file, which takes the S3 skip.
func TestClaimsWithoutAChecksumAreNotConflicts(t *testing.T) {
	c := newClaims()
	native := client.Attachment{Title: "diagram.png"}
	if err := c.claim("/out/home/diagram.png", &client.Page{ID: "1"}, native); err != nil {
		t.Fatal(err)
	}
	if err := c.claim("/out/home/diagram.png", &client.Page{ID: "2"}, native); err != nil {
		t.Errorf("an unmeasurable meeting must not be reported as a conflict: %v", err)
	}
}

// TestClaimsDistinctDestinationsAreIndependent is the ordinary case, and the
// one the slug suffix pass guarantees for unsourced attachments.
func TestClaimsDistinctDestinationsAreIndependent(t *testing.T) {
	c := newClaims()
	for _, dest := range []string{"/out/home/diagram.png", "/out/onboarding/diagram.png"} {
		if err := c.claim(dest, &client.Page{ID: "1"}, client.Attachment{Title: "diagram.png"}); err != nil {
			t.Errorf("%s: %v", dest, err)
		}
	}
}

// TestClaimsRefusesAnAttachmentLandingOnAPageFile is the security case: a
// recorded path is a server-side string that can name any file under dest,
// including one an exported page occupies. A parent's attachments are written
// before its children are exported, so without this the attachment lands first
// and the child page is reported "skipped (exists)" -- silently, and counted as
// a success, leaving attacker-chosen bytes in a file the reader believes is a
// page they exported.
func TestClaimsRefusesAnAttachmentLandingOnAPageFile(t *testing.T) {
	c := newClaims()
	c.reservePage("/out/handbook/onboarding.md", "999")

	err := c.claim("/out/handbook/onboarding.md", &client.Page{ID: "12345"},
		attWithSum("evil.md", "handbook/onboarding.md", "aaa"))
	if err == nil {
		t.Fatal("an attachment must not be written over a page's own file")
	}
	for _, want := range []string{"evil.md", "999"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// TestClaimsAllowsAnAttachmentBesideAPageFile keeps the reservation to exact
// paths: a page directory holds its own attachments by design.
func TestClaimsAllowsAnAttachmentBesideAPageFile(t *testing.T) {
	c := newClaims()
	c.reservePage("/out/handbook/onboarding.md", "999")
	if err := c.claim("/out/handbook/onboarding/diagram.png", &client.Page{ID: "999"},
		client.Attachment{Title: "diagram.png"}); err != nil {
		t.Errorf("an attachment under a page's own directory must be fine: %v", err)
	}
}
