package convert

// Internals the package's own black-box tests need. Kept to one file so the
// list of what is reachable from convert_test is visible at a glance.

// DecodeDestinationForTest exposes decodeDestination, so a test materializing
// the files a converted document references decodes a Markdown destination the
// same way renderImage does rather than reimplementing the codec.
func DecodeDestinationForTest(dest string) string { return decodeDestination(dest) }

// EscapeLinkTextForTest exposes escapeLinkText, so that a backslash before a
// bracket is escaped (or it escapes the bracket's escape) can be pinned
// directly rather than inferred from a rendered document.
func EscapeLinkTextForTest(s string) string { return escapeLinkText(s) }

// EscapeTextForTest exposes escapeText, so each of its rules can be pinned
// alongside a check that its output publishes back as the original text.
func EscapeTextForTest(s string, inLink bool) string { return escapeText(s, inLink) }
