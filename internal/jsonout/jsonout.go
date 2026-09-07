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
	// Roots is every distinct documentation root the command resolved,
	// sorted -- empty for a command with no per-file root concept (find,
	// search, schema, ...), and for create/update/attachment-upload's
	// pre-flight failure paths that never reached root resolution. Not
	// omitted: every envelope carries this key, [] when there is nothing to
	// report, the same convention Results already follows.
	Roots []string `json:"roots"`
	// Warnings is about the invocation rather than about any page or file --
	// currently only the .env permission warning. It is filled from the
	// package-level collector by NewEnvelope, not by the caller: the warning is
	// raised deep inside credential resolution, before any command knows
	// whether it will emit an envelope at all, and twelve commands would
	// otherwise each have to remember to pass it through. Not omitted: every
	// envelope carries the key, [] when empty, as Roots and Results do.
	Warnings []string `json:"warnings"`
	Results  []any    `json:"results"`
	Summary  any      `json:"summary"`
}

// ErrorObject is the stderr document for a fatal/pre-flight failure in --json
// mode (bad flags, credential resolution). No stdout payload accompanies it.
type ErrorObject struct {
	SchemaVersion int    `json:"schema_version"`
	Command       string `json:"command"`
	Error         string `json:"error"`
	Code          Code   `json:"code"`
	// Warnings is the envelope's Warnings, carried here for the same reason it
	// exists there -- and it matters more here: a fatal failure emits no
	// envelope, and a credential-resolution failure is exactly the run where a
	// warning about the .env holding the token is worth reading.
	Warnings []string `json:"warnings"`
}

// warnings collects invocation-level warnings raised before any document is
// emitted. Package-level for the same reason client.SetRetryLogger is: the
// warning comes out of credential resolution, far below the command that will
// emit the document, and a value threaded through twelve commands is a value
// the thirteenth forgets.
//
// It is deliberately not an output channel. internal/ui prints the warning for
// a human; this is how the same text reaches the JSON documents, where stderr
// is a schema-validated document and a stray line would break it.
var warnings []string

// AddWarning records an invocation-level warning for both output documents.
func AddWarning(msg string) { warnings = append(warnings, msg) }

// ResetWarnings clears the collector. For tests; a process emits one document.
func ResetWarnings() { warnings = nil }

// collectedWarnings returns the warnings as a non-nil slice, so the key
// marshals as [] rather than null.
func collectedWarnings() []string {
	if warnings == nil {
		return []string{}
	}
	return warnings
}

// NewEnvelope builds an envelope for a command, stamping the schema and build
// version. results is emitted as [] (never null) when empty; so is Roots,
// which callers with a root to report set afterward -- most commands have
// none, so making it a constructor parameter would force all of them to pass
// nil for a concept they don't have.
func NewEnvelope(command string, results []any, summary any) Envelope {
	if results == nil {
		results = []any{}
	}
	return Envelope{
		SchemaVersion:      SchemaVersion,
		MarkfluenceVersion: buildinfo.Version,
		Command:            command,
		Roots:              []string{},
		Warnings:           collectedWarnings(),
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
		Warnings:      collectedWarnings(),
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
		// Asked before the status switch: a rejected credential arrives as a 404
		// on every v2 route, and reporting that as not_found tells a consumer to
		// go and check an id that was never the problem.
		if he.RejectedCredential() {
			return CodeAuth
		}
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

// CodeOr classifies err the way CodeFor does when it came from a Confluence
// request, and returns fallback when it did not.
//
// This is what most failure sites want, and CodeFor alone is not: CodeFor
// answers NETWORK for any non-nil error that is not an *HTTPError, so a site
// that mixes local and server failures -- create's preflight, fix's page
// location, every attachment path -- would report "no title given" as a
// network problem. Passing everything to a constant is the other half of the
// same mistake, and is what reported a rejected credential as VALIDATION
// against a file that was fine (#133).
//
// fallback is a parameter rather than a hardcoded VALIDATION so the call site
// says which local meaning it is choosing: VALIDATION where a local failure
// means the file is wrong, IO where it means the file could not be read.
func CodeOr(err error, fallback Code) Code {
	if client.FromRequest(err) {
		return CodeFor(err)
	}
	return fallback
}
