package convert

// Internals the package's own black-box tests need. Kept to one file so the
// list of what is reachable from convert_test is visible at a glance.

// DecodeDestinationForTest exposes decodeDestination, so a test materializing
// the files a converted document references decodes a markdown destination the
// same way renderImage does rather than reimplementing the codec.
func DecodeDestinationForTest(dest string) string { return decodeDestination(dest) }

// EscapeLinkTextForTest exposes escapeLinkText, so the ordering of its passes
// (backslash before brackets, or it escapes its own escapes) can be pinned
// directly rather than inferred from a rendered document.
func EscapeLinkTextForTest(s string) string { return escapeLinkText(s) }
