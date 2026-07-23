// Package jsonout builds markfluence's machine-readable --json output: the
// stable envelope wrapping every command's results, the typed error object for
// fatal failures, and the error-code vocabulary shared by both.
//
// The stdout payload is a single Envelope; fatal/pre-flight failures are an
// ErrorObject on stderr. "Stable schema" is per-command: each command always
// emits the same keys with the same shapes, and SchemaVersion is bumped on any
// breaking change.
package jsonout

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/mozilla/markfluence/internal/buildinfo"
	"github.com/mozilla/markfluence/internal/client"
)

// SchemaVersion is the version of the JSON output schema. Bump it on any
// breaking change to the envelope or a per-command result shape.
const SchemaVersion = 1

// Code is a typed, machine-branchable error category.
type Code string

// Error codes, tailored to markfluence's failure sites.
const (
	CodeConfig     Code = "CONFIG"     // credential/config resolution, usage
	CodeAuth       Code = "AUTH"       // 401/403
	CodeNotFound   Code = "NOT_FOUND"  // 404
	CodeValidation Code = "VALIDATION" // bad frontmatter/args, duplicate title
	CodeConvert    Code = "CONVERT"    // MdToConfluence / StorageToMarkdown failure
	CodeIO         Code = "IO"         // local file read/write
	CodeNetwork    Code = "NETWORK"    // transport failure (no HTTP status)
	CodeAPI        Code = "API"        // other HTTP >= 400
)

// Envelope is the top-level stdout document for every command in --json mode.
type Envelope struct {
	SchemaVersion      int    `json:"schema_version"`
	MarkfluenceVersion string `json:"markfluence_version"`
	Command            string `json:"command"`
	Results            []any  `json:"results"`
	Summary            any    `json:"summary"`
}

// ErrorObject is the stderr document for a fatal/pre-flight failure in --json
// mode (bad flags, credential resolution). No stdout payload accompanies it.
type ErrorObject struct {
	SchemaVersion int    `json:"schema_version"`
	Command       string `json:"command"`
	Error         string `json:"error"`
	Code          Code   `json:"code"`
}

// NewEnvelope builds an envelope for a command, stamping the schema and build
// version. results is emitted as [] (never null) when empty.
func NewEnvelope(command string, results []any, summary any) Envelope {
	if results == nil {
		results = []any{}
	}
	return Envelope{
		SchemaVersion:      SchemaVersion,
		MarkfluenceVersion: buildinfo.Version,
		Command:            command,
		Results:            results,
		Summary:            summary,
	}
}

// Emit writes the envelope as pretty-printed (2-space) JSON with a trailing
// newline. It is the sole writer of stdout in --json mode.
func Emit(w io.Writer, env Envelope) error {
	return encode(w, env)
}

// EmitError writes a typed error object (pretty-printed, trailing newline),
// intended for stderr on a fatal/pre-flight failure.
func EmitError(w io.Writer, command, msg string, code Code) error {
	return encode(w, ErrorObject{
		SchemaVersion: SchemaVersion,
		Command:       command,
		Error:         msg,
		Code:          code,
	})
}

func encode(w io.Writer, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = w.Write(b)
	return err
}

// CodeFor classifies an error into a Code. An *HTTPError maps by status
// (401/403 -> AUTH, 404 -> NOT_FOUND, else API); any other non-nil error is
// treated as a transport/NETWORK failure. Callers with more context (a bad
// frontmatter parse, a local file error) should pass an explicit Code instead.
func CodeFor(err error) Code {
	var he *client.HTTPError
	if errors.As(err, &he) {
		switch he.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return CodeAuth
		case http.StatusNotFound:
			return CodeNotFound
		default:
			return CodeAPI
		}
	}
	if err != nil {
		return CodeNetwork
	}
	return CodeAPI
}
