package attachmentdownload

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/mozilla/markfluence/internal/jsonout"
	"github.com/mozilla/markfluence/internal/schematest"
)

func TestSchemaConformance(t *testing.T) {
	results := []any{
		buildResult(outcome{name: "assets%2Fx.png", destPath: "out/assets/x.png", status: statusDownloaded}),
		buildResult(outcome{name: "notes.pdf", destPath: "out/notes.pdf", status: statusSkipped}),
		buildResult(outcome{
			name: "evil.png", status: statusFailed,
			err: errors.New("outside the destination directory"), code: jsonout.CodeValidation,
		}),
	}
	env := jsonout.NewEnvelope(command, results,
		map[string]int{"total": 3, "succeeded": 2, "failed": 1, "skipped": 1})
	var buf bytes.Buffer
	if err := jsonout.Emit(&buf, env); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	schematest.ValidateEnvelope(t, buf.Bytes())

	failRes := map[string]any{"ok": false, "page_id": "9", "error": "page 9 not found", "code": jsonout.CodeNotFound}
	failEnv := jsonout.NewEnvelope(command, []any{failRes},
		map[string]int{"total": 1, "succeeded": 0, "failed": 1, "skipped": 0})
	buf.Reset()
	if err := jsonout.Emit(&buf, failEnv); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	schematest.ValidateEnvelope(t, buf.Bytes())
}

func TestBuildResultFailureCarriesErrorAndNullDest(t *testing.T) {
	res := buildResult(outcome{
		name: "evil.png", status: statusFailed,
		err: errors.New("boom"), code: jsonout.CodeValidation,
	})
	if res.OK {
		t.Error("ok = true, want false for a failure")
	}
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	var round map[string]any
	if err := json.Unmarshal(b, &round); err != nil {
		t.Fatal(err)
	}
	if round["dest_path"] != nil {
		t.Errorf("dest_path = %v, want null when path resolution failed", round["dest_path"])
	}
	if round["error"] != "boom" || round["code"] != "VALIDATION" {
		t.Errorf("error/code = %v/%v", round["error"], round["code"])
	}
}

func TestJSONDownloadResultMarshal(t *testing.T) {
	b, err := json.MarshalIndent(
		buildResult(outcome{name: "assets%2Fx.png", destPath: "out/assets/x.png", status: statusDownloaded}),
		"", "  ")
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "ok": true,
  "status": "downloaded",
  "dry_run": false,
  "filename": "assets%2Fx.png",
  "dest_path": "out/assets/x.png",
  "error": null,
  "code": null
}`
	if string(b) != want {
		t.Errorf("download result mismatch:\n got:\n%s\n want:\n%s", b, want)
	}
}
