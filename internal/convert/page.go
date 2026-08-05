package convert

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
}

// Attachment is a local image the body references, to be uploaded to the page.
// Path is absolute. Filename is the attachment name, a bijective encoding of
// Source, so distinct images can never collide on one name. Source is the
// normalized page-relative path the image was written as, recorded on the
// attachment so a later read recovers it exactly rather than inferring it.
type Attachment struct {
	Filename string `json:"filename"`
	Path     string `json:"path"`
	Source   string `json:"source"`
}
