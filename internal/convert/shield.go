package convert

import "strings"

// shieldStorage renames raw Confluence storage prefixes (ac:/ri:) to colon-free
// sentinels so goldmark passes the tags through instead of escaping them (a colon
// isn't valid in an HTML tag name, so <ac:...> would otherwise be treated as
// literal text). It returns the shielded markdown and an unshield function that
// restores the tags in the rendered output.
//
// Sentinels are derived from the source so they can't collide with real content:
// each grows by appending 'F' until it no longer occurs in the input.
func shieldStorage(md string) (shielded string, unshield func(string) string) {
	ac := storageSentinel("MFAC", md)
	ri := storageSentinel("MFRI", md)

	shielded = strings.ReplaceAll(md, "ac:", ac)
	shielded = strings.ReplaceAll(shielded, "ri:", ri)

	unshield = func(s string) string {
		s = strings.ReplaceAll(s, ac, "ac:")
		s = strings.ReplaceAll(s, ri, "ri:")
		return s
	}
	return shielded, unshield
}

// storageSentinel returns a variant of base (growing by 'F') that does not occur
// in text, guaranteeing the shield round-trip can't corrupt real content.
func storageSentinel(base, text string) string {
	s := base
	for strings.Contains(text, s) {
		s += "F"
	}
	return s
}
