package pageref

import (
	"strings"
	"testing"
)

// TestNotFoundMessage pins the sentence create, update, and fix all report, since
// the whole reason it lives here is that the three must not drift apart.
func TestNotFoundMessage(t *testing.T) {
	got := NotFoundMessage("999", "remove it to create a new page, or correct it")
	want := "page_id 999 not found (deleted, trashed, or wrong); " +
		"remove it to create a new page, or correct it"
	if got != want {
		t.Errorf("NotFoundMessage =\n %q\nwant\n %q", got, want)
	}
}

// TestNotNumericMessage checks the id is quoted. An unquoted one reads as prose
// when it is something like `page_id: my page`, and a pasted URL runs into the
// rest of the sentence.
func TestNotNumericMessage(t *testing.T) {
	got := NotNumericMessage("TODO")
	if !strings.Contains(got, `"TODO"`) {
		t.Errorf("NotNumericMessage = %q, want the id quoted", got)
	}
	if !strings.Contains(got, "not a numeric page id") {
		t.Errorf("NotNumericMessage = %q, want it to say what is wrong", got)
	}
}
