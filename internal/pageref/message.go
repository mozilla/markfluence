package pageref

import "fmt"

// The two ways a frontmatter page_id is wrong, phrased once.
//
// create, update, and fix all report both conditions, and all three want a reader
// to recognize them as the same problem seen from different commands -- so the
// sentence lives here rather than in three literals that drift apart. Only the
// remedy differs, since what to do about a dead id depends on what the command was
// trying to do, and that is the caller's to supply.
//
// These return strings, not errors: create wraps the text in a typed error that
// also carries the fields its --json result reports, while update and fix want a
// plain error. A shared error type would serve neither.

// NotFoundMessage describes a page_id that resolves to nothing. remedy completes
// the sentence -- e.g. "remove it to create a new page, or correct it".
//
// The parenthetical lists deletion and trashing as causes because both look
// identical from here: the API answers 404 either way, and markfluence has no way
// to tell which happened.
func NotFoundMessage(pageID, remedy string) string {
	return fmt.Sprintf("page_id %s not found (deleted, trashed, or wrong); %s", pageID, remedy)
}

// NotNumericMessage describes a page_id that is not a page id at all -- a pasted
// URL, a leftover placeholder. Worth catching before any request: the API answers
// such an id with a 400 whose raw body says nothing a reader can act on.
func NotNumericMessage(pageID string) string {
	return fmt.Sprintf("page_id %q is not a numeric page id; correct it or remove it", pageID)
}
