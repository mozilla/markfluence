package convert

// imgsrc.go owns the mapping between a markdown image destination and the
// filesystem path it names.
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
// it into bytes. markfluence has no such layer beneath it -- it opens the file
// itself to upload it as an attachment -- so it has to do that job: decode on
// the way in, encode on the way back out.

import (
	"net/url"
	"strings"
)

// parenEscaper escapes what url.PathEscape leaves alone but markdown cannot
// hold: an unbalanced parenthesis ends a destination early.
var parenEscaper = strings.NewReplacer("(", "%28", ")", "%29")

// decodeImageSrc turns a markdown image destination into a filesystem path.
//
// An invalid escape is not an error. "100%.png" is a legal filename that nobody
// percent-encoded, so a destination that cannot be decoded is one that was never
// encoded, and is used as written.
func decodeImageSrc(dest string) string {
	if p, err := url.PathUnescape(dest); err == nil {
		return p
	}
	return dest
}

// encodeImageSrc turns a filesystem path into a markdown image destination, the
// inverse of decodeImageSrc. Segments are escaped individually so "/" survives
// as a separator rather than becoming "%2F".
func encodeImageSrc(p string) string {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		segs[i] = parenEscaper.Replace(url.PathEscape(s))
	}
	return strings.Join(segs, "/")
}
