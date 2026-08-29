package attachmentupload

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/jsonout"
	"github.com/mozilla/markfluence/internal/project"
	"github.com/mozilla/markfluence/internal/schematest"
)

func TestSchemaConformance(t *testing.T) {
	results := []any{
		buildResult(client.SyncAction{Filename: "a.png", Action: "created"}),
		buildResult(client.SyncAction{Filename: "b.png", Action: "updated"}),
		buildResult(client.SyncAction{Filename: "c.png", Action: "skipped"}),
	}
	env := jsonout.NewEnvelope(command, results,
		map[string]int{"total": 3, "succeeded": 3, "failed": 0, "skipped": 1})
	var buf bytes.Buffer
	if err := jsonout.Emit(&buf, env); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	schematest.ValidateEnvelope(t, buf.Bytes())

	// Built by the command, not restated here: a renamed key or a changed summary
	// in failEnvelope has to reach the schema through this test.
	failEnv := failEnvelope("9", errors.New("page 9 not found"), jsonout.CodeNotFound, project.NewCache(""))
	buf.Reset()
	if err := jsonout.Emit(&buf, failEnv); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	schematest.ValidateEnvelope(t, buf.Bytes())
}

func TestSchemaConformanceDryRun(t *testing.T) {
	dryRun = true
	t.Cleanup(func() { dryRun = false })

	res := buildResult(client.SyncAction{Filename: "a.png", Action: "created"})
	if !res.DryRun {
		t.Error("dry_run = false, want true")
	}
	env := jsonout.NewEnvelope(command, []any{res},
		map[string]int{"total": 1, "succeeded": 1, "failed": 0, "skipped": 0})
	var buf bytes.Buffer
	if err := jsonout.Emit(&buf, env); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	schematest.ValidateEnvelope(t, buf.Bytes())
}

func TestJSONUploadResultMarshal(t *testing.T) {
	b, err := json.MarshalIndent(buildResult(client.SyncAction{Filename: "a.png", Action: "created"}), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "ok": true,
  "status": "created",
  "dry_run": false,
  "filename": "a.png",
  "dest_path": null,
  "error": null,
  "code": null
}`
	if string(b) != want {
		t.Errorf("upload result mismatch:\n got:\n%s\n want:\n%s", b, want)
	}
}
