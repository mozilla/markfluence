package convert

// destination.go owns the mapping between a markdown link destination and the
// filesystem path it names. It serves both kinds of destination markfluence
// resolves against the disk: an image's `src`, and a link to a sibling `.md`.
//
// A destination is a URL, not a path. CommonMark resolves backslash escapes and
// entities and then uses the result as an href, percent-encoding what a URL
// cannot hold literally -- so "![a](my%20file.png)" and "![a](<my file.png>)"
// are two spellings of one image, and renderers converge them on the same
// src="my%20file.png". (A bare "![a](my file.png)" is a third thing entirely:
// an unbracketed destination cannot contain a space, so it is not an image at
// all and renders as literal text.)
//
// An ordinary renderer stops at that href and lets a browser or web server turn
// it into bytes. markfluence has no such layer beneath it -- it opens image
// files to upload them, and matches link targets against sibling filenames on
// disk -- so it has to do that job: decode on the way in, encode on the way
// back out.
//
// Fragments go through decodeDestination too. A heading anchor is matched
// against githubSlug output, which is Unicode-aware, so "#caf%C3%A9-section"
// has to become "#café-section" before it will match anything.

import (
	"net/url"
	"strings"
)

// parenEscaper escapes what url.PathEscape leaves alone but markdown cannot
// hold: an unbalanced parenthesis ends a destination early.
var parenEscaper = strings.NewReplacer("(", "%28", ")", "%29")

// decodeDestination turns a markdown link destination into a filesystem path.
//
// An invalid escape is not an error. "100%.png" is a legal filename that nobody
// percent-encoded, so a destination that cannot be decoded is one that was never
// encoded, and is used as written.
func decodeDestination(dest string) string {
	if p, err := url.PathUnescape(dest); err == nil {
		return p
	}
	return dest
}

// encodeDestination turns a filesystem path into a markdown link destination,
// the inverse of decodeDestination. Segments are escaped individually so "/"
// survives as a separator rather than becoming "%2F".
func encodeDestination(p string) string {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		segs[i] = parenEscaper.Replace(url.PathEscape(s))
	}
	return strings.Join(segs, "/")
}
