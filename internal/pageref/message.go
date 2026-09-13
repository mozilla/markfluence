package pageref

import "fmt"

// The two ways a frontmatter page_id is wrong, phrased once.
//
// create and update report both conditions and check reports the non-numeric
// one, and each wants a reader to recognize the same problem seen from a
// different command -- so the sentence lives here rather than in literals that
// drift apart. Only the remedy differs, since what to do about a dead id depends
// on what the command was trying to do, and that is the caller's to supply.
//
// These return strings, not errors: create wraps the text in a typed error that
// also carries the fields its --json result reports, while update and check want
// a plain error. A shared error type would serve neither.

// NotFoundMessage describes a page_id that resolves to nothing -- the id 404s.
// remedy completes the sentence -- e.g. "remove it to create a new page, or
// correct it".
//
// "Trashed" is deliberately *not* offered as a cause, though it is the first
// thing a reader will suspect. A trashed page answers GET /wiki/api/v2/pages/{id}
// with 200 and status "trashed" (docs/confluence/folders.md), as does an
// archived page with status "archived", so neither reaches this message at all --
// they slip past the nil check that calls it and on into the operation. Naming
// trashing here would tell someone debugging exactly that case that they had
// found their answer. Detecting those two states is #17, and needs a status
// check rather than a 404.
func NotFoundMessage(pageID, remedy string) string {
	return fmt.Sprintf("page_id %s not found (deleted or wrong); %s", pageID, remedy)
}

// NotNumericMessage describes a page_id that is not a page id at all -- a pasted
// URL, a leftover placeholder. Worth catching before any request: the API answers
// such an id with a 400 whose raw body says nothing a reader can act on.
func NotNumericMessage(pageID string) string {
	return fmt.Sprintf("page_id %q is not a numeric page id; correct it or remove it", pageID)
}
