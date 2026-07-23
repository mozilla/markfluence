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
