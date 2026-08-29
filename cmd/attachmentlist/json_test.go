package attachmentlist

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/jsonout"
	"github.com/mozilla/markfluence/internal/schematest"
)

func TestSchemaConformance(t *testing.T) {
	managed := client.Attachment{ID: "att1", Title: "assets%2Fx.png"}
	managed.Metadata.Comment = "markfluence: sha256=abc path=assets/x.png"
	managed.Version.Number = 3
	managed.Extensions.MediaType = "image/png"
	managed.Extensions.FileSize = 171

	hand := client.Attachment{ID: "att2", Title: "notes.pdf"}
	hand.Extensions.MediaType = "application/pdf"
	hand.Extensions.FileSize = 2048
	hand.Version.Number = 1

	env := jsonout.NewEnvelope(command,
		[]any{buildResult(managed), buildResult(hand)},
		map[string]int{"total": 2, "succeeded": 2, "failed": 0})
	var buf bytes.Buffer
	if err := jsonout.Emit(&buf, env); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	schematest.ValidateEnvelope(t, buf.Bytes())

	// Built by the command, not restated here: a renamed key or a changed summary
	// in failEnvelope has to reach the schema through this test.
	failEnv := failEnvelope("9", errors.New("page 9 not found"), jsonout.CodeNotFound)
	buf.Reset()
	if err := jsonout.Emit(&buf, failEnv); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	schematest.ValidateEnvelope(t, buf.Bytes())

	// A page with no attachments still has to conform.
	emptyEnv := jsonout.NewEnvelope(command, nil,
		map[string]int{"total": 0, "succeeded": 0, "failed": 0})
	buf.Reset()
	if err := jsonout.Emit(&buf, emptyEnv); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	schematest.ValidateEnvelope(t, buf.Bytes())
}

func TestBuildResultManaged(t *testing.T) {
	a := client.Attachment{ID: "att1", Title: "assets%2Fx.png"}
	a.Metadata.Comment = "markfluence: sha256=abc path=assets/x.png"
	res := buildResult(a)
	if !res.Managed {
		t.Error("managed = false, want true")
	}
	if res.Source == nil || *res.Source != "assets/x.png" {
		t.Errorf("source = %v, want assets/x.png", res.Source)
	}
	if res.SHA256 == nil || *res.SHA256 != "abc" {
		t.Errorf("sha256 = %v, want abc", res.SHA256)
	}
	// The stored name is reported as-is; it is what identifies the attachment.
	if res.Filename != "assets%2Fx.png" {
		t.Errorf("filename = %q, want the stored name", res.Filename)
	}
}

// TestBuildResultHandUploadedNullsMetadata is the signal attachment-list exists
// to give: an attachment publishing will not touch.
func TestBuildResultHandUploadedNullsMetadata(t *testing.T) {
	a := client.Attachment{ID: "att2", Title: "notes.pdf"}
	res := buildResult(a)
	if res.Managed {
		t.Error("managed = true, want false")
	}
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	var round map[string]any
	if err := json.Unmarshal(b, &round); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"sha256", "source"} {
		if round[k] != nil {
			t.Errorf("%s = %v, want null", k, round[k])
		}
	}
}
