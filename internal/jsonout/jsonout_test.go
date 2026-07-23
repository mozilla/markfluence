package jsonout

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/client"
)

func TestEmitEnvelope(t *testing.T) {
	var buf bytes.Buffer
	env := NewEnvelope("info", []any{map[string]any{"ok": true}}, map[string]any{"total": 1})
	if err := Emit(&buf, env); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	out := buf.String()
	if !strings.HasSuffix(out, "\n") {
		t.Errorf("output not newline-terminated: %q", out)
	}
	if !strings.Contains(out, "\n  \"command\": \"info\"") {
		t.Errorf("output not 2-space indented:\n%s", out)
	}
	for _, want := range []string{`"schema_version": 1`, `"command": "info"`, `"results"`, `"summary"`} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestNewEnvelopeEmptyResultsIsArray(t *testing.T) {
	var buf bytes.Buffer
	if err := Emit(&buf, NewEnvelope("fix", nil, nil)); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if !strings.Contains(buf.String(), `"results": []`) {
		t.Errorf("empty results should marshal as [], got:\n%s", buf.String())
	}
}

func TestEmitError(t *testing.T) {
	var buf bytes.Buffer
	if err := EmitError(&buf, "update", "could not resolve credentials", CodeConfig); err != nil {
		t.Fatalf("EmitError: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		`"schema_version": 1`,
		`"command": "update"`,
		`"error": "could not resolve credentials"`,
		`"code": "CONFIG"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("error output missing %q:\n%s", want, out)
		}
	}
}

func TestCodeFor(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want Code
	}{
		{"401", &client.HTTPError{StatusCode: 401}, CodeAuth},
		{"403", &client.HTTPError{StatusCode: 403}, CodeAuth},
		{"404", &client.HTTPError{StatusCode: 404}, CodeNotFound},
		{"500", &client.HTTPError{StatusCode: 500}, CodeAPI},
		{"wrapped 404", errWrap(&client.HTTPError{StatusCode: 404}), CodeNotFound},
		{"transport", errors.New("dial tcp: connection refused"), CodeNetwork},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CodeFor(tt.err); got != tt.want {
				t.Errorf("CodeFor(%v) = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
}

func errWrap(err error) error { return errors.Join(errors.New("context"), err) }
