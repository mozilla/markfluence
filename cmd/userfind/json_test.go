package userfind

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/jsonout"
	"github.com/mozilla/markfluence/internal/schematest"
	"github.com/mozilla/markfluence/internal/ui"
)

// envelopeFor builds the document the command really emits, through the same
// builders run() uses. Hand-copying a literal here would validate a copy while
// the real output drifted.
func envelopeFor(matches []client.UserMatch, more bool) jsonout.Envelope {
	results := make([]any, 0, len(matches))
	for _, m := range matches {
		results = append(results, buildResult(m))
	}
	return jsonout.NewEnvelope(command, results, buildSummary(matches, more))
}

func TestSchemaConformance(t *testing.T) {
	matches := []client.UserMatch{
		{AccountID: "60c36d0718e9f60071326951", DisplayName: "William Kahn-Greene"},
		// The other live account-id shape on the same instance, which is why
		// nothing parses one.
		{AccountID: "712020:75e6f4e8-1ad1-42a9-9d5c-5f867110c36a", DisplayName: "Brittany Reid"},
		// A name needing escaping in the mention line.
		{AccountID: "712020:95564662-741f-43a4-8818-67e2eb2ddde8", DisplayName: "[TEMPLATE] ASCII art"},
	}
	var buf bytes.Buffer
	if err := jsonout.Emit(&buf, envelopeFor(matches, true)); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	schematest.ValidateEnvelope(t, buf.Bytes())
}

// The no-match case is a success with zero results rather than a failure, so
// the schema has to accept an empty array.
func TestEmptyEnvelopeConforms(t *testing.T) {
	var buf bytes.Buffer
	if err := jsonout.Emit(&buf, envelopeFor(nil, false)); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	schematest.ValidateEnvelope(t, buf.Bytes())
}

// A failed lookup is an error object on stderr with no envelope, which find and
// search share and which the schema publishes as #/$defs/errorObject.
func TestErrorObjectConforms(t *testing.T) {
	var buf bytes.Buffer
	if err := jsonout.EmitError(&buf, command, "500 Internal Server Error", jsonout.CodeAPI); err != nil {
		t.Fatalf("EmitError: %v", err)
	}
	schematest.ValidateError(t, buf.Bytes())
}

// End to end under --json: a real run emits a document the schema accepts, the
// mention travels in the payload, and nothing goes to stderr -- under --json
// there is no second stream for a human line.
func TestJSONRunValidates(t *testing.T) {
	ui.SetJSON(true)
	defer ui.SetJSON(false)

	o := runFind(t, stub{rows: func(int) string {
		return people([2]string{"William Kahn-Greene", "60c36d0718e9f60071326951"})
	}}, "10", "kahn")

	if o.exit != 0 {
		t.Fatalf("exit = %d, want 0; stderr:\n%s", o.exit, o.stderr)
	}
	schematest.ValidateEnvelope(t, []byte(o.stdout))
	if o.stderr != "" {
		t.Errorf("--json wrote to stderr:\n%s", o.stderr)
	}

	var env struct {
		Results []jsonUserResult `json:"results"`
		Summary jsonUserSummary  `json:"summary"`
	}
	if err := json.Unmarshal([]byte(o.stdout), &env); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(env.Results) != 1 {
		t.Fatalf("results = %d, want 1", len(env.Results))
	}
	got := env.Results[0]
	if got.AccountID != "60c36d0718e9f60071326951" || got.DisplayName != "William Kahn-Greene" {
		t.Errorf("result = %#v", got)
	}
	want := "[@William Kahn-Greene](https://home.atlassian.com/people/60c36d0718e9f60071326951)"
	if got.Mention != want {
		t.Errorf("mention = %q, want %q", got.Mention, want)
	}
	if env.Summary.Truncated {
		t.Error("truncated = true on a satisfied bound")
	}
}

// truncated is the only way a consumer learns the bound was hit, since this
// route's totalSize describes the page rather than the result set.
func TestTruncatedIsReported(t *testing.T) {
	ui.SetJSON(true)
	defer ui.SetJSON(false)

	o := runFind(t, stub{rows: func(int) string {
		return people([2]string{"Ana", "id-a"}, [2]string{"Bo", "id-b"})
	}}, "1", "a")

	if !strings.Contains(o.stdout, `"truncated": true`) {
		t.Errorf("summary did not report truncation:\n%s", o.stdout)
	}
}
