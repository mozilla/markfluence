package export

// Two pages writing one file.

import (
	"fmt"

	"github.com/mozilla/markfluence/internal/client"
)

// destClaims records which page wrote each destination in this run, so that two
// pages wanting one file with different content are reported rather than
// silently resolved by whichever was walked first.
//
// One shared asset referenced from many pages is the model's success case: each
// page carries its own attachment recording the same path, all of them resolve
// to one file, the bytes match, and the second write skips under S3. That is
// the right outcome and stays quiet. A *differing* checksum under one path is
// two pages disagreeing about what that path holds, which is not.
type destClaims struct {
	by map[string]claim
	// pages is every path this run will write a page's markdown to, reserved
	// before anything is written. A recorded attachment path is a server-side
	// string that can name any file under dest, including one of these, and the
	// parent's attachments are written before its children are exported -- so
	// without this the attachment lands first and the page it shadowed is
	// reported "skipped (exists)" and counted as a success.
	pages map[string]string
}

type claim struct {
	pageID   string
	checksum string
}

func newClaims() *destClaims {
	return &destClaims{by: map[string]claim{}, pages: map[string]string{}}
}

// reservePage records that pageID's markdown will be written to dest.
func (d *destClaims) reservePage(dest, pageID string) { d.pages[dest] = pageID }

// claim records that page is about to write dest for attachment a, and reports
// a conflict when another page already wrote it with different content.
//
// The comparison is deliberately narrow: it compares the checksums recorded in
// the two attachments' comments, and a recorded path implies a managed
// attachment, so both sides of a shared-path collision always have one. Every
// other way two writes can meet takes the S3 exists-skip instead:
//
//   - two unsourced attachments cannot collide at all, because each is scoped
//     into its own page's directory and the slug pass makes those unique;
//   - an unsourced attachment meeting a recorded path needs a recorded path
//     equal to another page's slug directory, which is the same shape as an
//     attachment colliding with a page file and gets the same answer.
//
// So a missing checksum means the case is not one this rule covers, not that
// the rule failed to decide.
func (d *destClaims) claim(dest string, page *client.Page, a client.Attachment) error {
	if owner, taken := d.pages[dest]; taken {
		return fmt.Errorf(
			"attachment %q of page %s resolves to %s, which is page %s's own file",
			a.Title, page.ID, dest, owner)
	}
	sum := a.Meta().SHA256
	prev, seen := d.by[dest]
	if !seen {
		d.by[dest] = claim{pageID: page.ID, checksum: sum}
		return nil
	}
	if prev.checksum == "" || sum == "" || prev.checksum == sum {
		return nil
	}
	return fmt.Errorf(
		"page %s already wrote %s with different content; both pages record this path "+
			"for attachment %q", prev.pageID, dest, a.Title)
}
