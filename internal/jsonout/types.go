package jsonout

// Shared value objects used across command result shapes. Compound values are
// always structured objects, never human display strings. Per-command result
// structs live in their own packages; only the genuinely shared pieces are here.

// PageWidth is the structured page_width value: the effective width plus whether
// it is the Confluence default (i.e. not explicitly set on the page).
type PageWidth struct {
	Value   string `json:"value"`
	Default bool   `json:"default"`
}

// Author identifies a Confluence user by account id and resolved display name.
// Name may be empty when the lookup fails.
type Author struct {
	AccountID string `json:"account_id"`
	Name      string `json:"name"`
}

// Stamp is a timestamped authorship record (created/updated). By is nil when no
// author is known.
type Stamp struct {
	At string  `json:"at"`
	By *Author `json:"by"`
}

// Attachment is a synced attachment action: "created" or "updated" for a file.
type Attachment struct {
	Action   string `json:"action"`
	Filename string `json:"filename"`
}

// SingleOpFailure is the results[0] entry a single-target command emits when its
// one operation against the page fails (not found, fetch error) --
// #/$defs/singleOpFailure in the schema.
//
// It is a struct rather than the map each command used to build inline because
// every field then marshals unconditionally, which is what lets the schema's
// additionalProperties:false and required catch a renamed or added key. A map
// only carries the keys the caller remembered to set, so drift in one showed up
// nowhere.
type SingleOpFailure struct {
	OK     bool   `json:"ok"`
	PageID string `json:"page_id"`
	Error  string `json:"error"`
	Code   Code   `json:"code"`
}

// NewSingleOpFailure builds the failure result for a page. OK is always false --
// the schema pins it to that constant, since this shape exists only to report a
// failure.
func NewSingleOpFailure(pageID string, err error, code Code) SingleOpFailure {
	return SingleOpFailure{OK: false, PageID: pageID, Error: err.Error(), Code: code}
}
