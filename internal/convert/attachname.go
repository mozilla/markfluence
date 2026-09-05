package convert

// attachname.go owns the mapping between a markdown image's source path and the
// Confluence attachment name it is published under: the name is the path's base
// name.
//
// "/" is not legal in an attachment name, so a path cannot be a name and
// something has to give. This used to flatten the whole path by percent-encoding
// it, which was bijective -- distinct paths always produced distinct names, and
// a name decoded back to the path it came from. Two things paid for that, and
// both were worth more than it was.
//
// The name is the attachment's identity: Confluence matches ri:filename
// literally, and planAttachments decides create-vs-update by name. So an
// encoded name moves whenever the path moves, and a moved name is a new
// attachment with the old one orphaned. Exporting a page positions its images,
// which moves paths; so does reorganising a repository. Neither should churn
// what is stored on the page.
//
// The path is not lost by dropping the encoding, because it is recorded in the
// attachment's comment (client.attachmentComment, since 026 commit 7). That is
// where `read` and `export` recover an image's original location from, and it
// is authoritative where a decoded name was only ever a guess about a name
// markfluence might not have written.
//
// What the encoding did buy is collision-freedom, and that is now an explicit
// refusal: two assets under one page whose base names agree are reported by
// images.go rather than silently given one name. See _plans/029.
//
// Exported because the attachment subcommands share it -- upload derives a name
// from a --name path. It lives here rather than in its own package because the
// converter's image handling is what defines the mapping.

import (
	"path"
	"strings"
)

const (
	pctEscape = "%25" // a literal "%" in the source path
	pctSlash  = "%2F" // a "/" path separator
)

// AttachmentFilename derives the Confluence attachment name for a markdown image
// src: the base name of the file, and nothing else. The src is normalized first
// so that "a/./x.png" and "a/x.png" agree, and so a name is never derived from a
// path that was absolute.
//
// The mapping is deliberately lossy, and the loss is what makes it usable. Two
// assets under one page can want the same name -- "arch/diagram.png" and
// "deploy/diagram.png" -- and that is refused where it is detected, in
// images.go, rather than encoded around here.
func AttachmentFilename(src string) string {
	rel := normalizeSrc(src)
	if rel == "" {
		// path.Base would answer "." for this; an empty src has no name.
		return ""
	}
	return path.Base(rel)
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
