package convert

// attachname.go owns the mapping between a markdown image's source path and the
// Confluence attachment name it is published under.
//
// "/" is not legal in an attachment name, so the path is flattened. The encoding
// escapes its own escape character, which makes it bijective: distinct source
// paths always produce distinct names, and every name markfluence produces
// decodes back to the exact path it came from. That is what lets `read`
// reconstruct an image's original location, and it is why two images can never
// silently collide on one attachment name.
//
// Confluence stores these names verbatim and matches ri:filename literally --
// verified against Cloud, where "a%2Fb.png" resolves and renders with the name
// re-escaped as "a%252Fb.png" in the image URL.
//
// The codec is exported because the attachment subcommands share it: upload
// encodes a --name path, and download decodes a stored name. It lives here
// rather than in its own package because the converter's image handling is what
// defines the mapping.

import (
	"path"
	"strings"
)

const (
	pctEscape = "%25" // a literal "%" in the source path
	pctSlash  = "%2F" // a "/" path separator
)

// AttachmentFilename derives the Confluence attachment name for a markdown image
// src. The src is normalized first so a name can never decode to an absolute
// path, then "%" is encoded before "/" -- in that order, so the escapes
// introduced for "/" are not themselves escaped.
func AttachmentFilename(src string) string {
	rel := normalizeSrc(src)
	rel = strings.ReplaceAll(rel, "%", pctEscape)
	return strings.ReplaceAll(rel, "/", pctSlash)
}

// AttachmentSource inverts AttachmentFilename, recovering the source path an
// attachment was published from. It reports false when the name could not have
// come from markfluence -- currently when it decodes to an empty or absolute
// path, which AttachmentFilename never produces -- so callers fall back to
// treating the attachment name as the path.
//
// A name markfluence did not create is decoded on a best-effort basis: there is
// no way to tell a hand-uploaded "a%2Fb.png" from one we published.
func AttachmentSource(filename string) (string, bool) {
	// Decode "%2F" first and "%25" last. Replacing "%2F" only ever removes text
	// and cannot spell a new "%25", while "%25" must be replaced last precisely
	// so its output is not rescanned -- that is how a literal "%2F" in the source
	// path (encoded "%252F") round-trips instead of collapsing to a separator.
	s := strings.ReplaceAll(filename, pctSlash, "/")
	s = strings.ReplaceAll(s, pctEscape, "%")
	if s == "" || path.IsAbs(s) {
		return "", false
	}
	return s, true
}

// normalizeSrc reduces a markdown image src to a clean relative path: "./a/x.png"
// and "a/./x.png" both become "a/x.png". A leading "/" is dropped because image
// resolution joins src onto the page's directory anyway, so an absolute-looking
// src was never actually absolute -- and dropping it keeps AttachmentFilename
// from ever producing a name that decodes to an absolute path.
//
// A ".." prefix is preserved: an image in a shared directory above the page
// ("../assets/logo.png") is a supported layout, the same as it would be viewing
// the file on GitHub.
func normalizeSrc(src string) string {
	s := strings.TrimPrefix(path.Clean(src), "/")
	if s == "." {
		return ""
	}
	return s
}
