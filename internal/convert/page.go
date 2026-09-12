package convert

import "github.com/mozilla/markfluence/internal/attachref"

// ConfluencePage is the result of converting a markdown body to Confluence
// storage format: the storage-format HTML plus the local images the body
// references. Fields are ordered so the JSON encoding reads with sorted keys.
type ConfluencePage struct {
	// Attachments are local images to upload, deduped by filename.
	Attachments []Attachment `json:"attachments"`
	// Broken holds human-readable "IMAGE BROKEN: ..." messages.
	Broken []string `json:"broken"`
	// HTML is the storage-format body ready to publish.
	HTML string `json:"html"`
	// Warnings holds image-property warnings (e.g. a bad width/align).
	Warnings []string `json:"warnings"`
	// Mentions are the account ids the body publishes as user mentions.
	//
	// Reported rather than validated here, because validating means a lookup
	// and this package holds no client -- the Attachments arrangement exactly.
	// The caller resolves them and warns about one that does not exist, which
	// it has to do because nothing else will: Confluence accepts any account id
	// and renders it as "@Unlicensed user" rather than failing.
	Mentions []string `json:"mentions"`
}

// Attachment is a local image the body references, to be uploaded to the page.
// It's the same shape internal/client uploads from -- see attachref.LocalAttachment.
type Attachment = attachref.LocalAttachment
