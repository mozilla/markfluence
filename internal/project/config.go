package project

// The settings a markfluence.yaml declares, and why a file that cannot be
// understood stops the command rather than being treated as a bare marker.

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/mozilla/markfluence/internal/frontmatter"
)

// Config is what a project file declares. Every field is project-*wide*, and
// every one is a default the file being published overrides: the chain is
// flag > frontmatter > project file, which is not the credentials chain
// (flag > environment > .env) and must not be conflated with it. A setting
// here answers "what is this content", not "who are you" (#100).
//
// Values are raw strings. A vocabulary is validated where it is consumed --
// internal/pagewidth cannot be imported here, since it imports internal/client
// and internal/client holds a *Cache -- so this package checks that a file is
// structurally sound and says nothing about whether "huge" is a page width.
type Config struct {
	// Space is the default space key, used by create when neither --space nor
	// a frontmatter space: is given.
	Space string
	// PageWidth is the default page_width. Declaring it means update asserts
	// a width on a file that declares none, the same way frontmatter does.
	PageWidth string
}

// kind is the shape a setting's value must have. Only scalars exist today.
type kind int

const kindScalar kind = iota + 1

// settings is the whitelist of recognized top-level keys, mapped to the shape
// each one's value must have.
//
// A table of shapes rather than a set of names, because what a later setting
// needs checked is not "is this key known" but "is its value the right shape":
// #139's pages: is a mapping where both of today's settings are scalars, and it
// should be able to join this table instead of reworking the reader.
var settings = map[string]kind{
	"page_width": kindScalar,
	"space":      kindScalar,
}

// dialect is markfluence's YAML dialect, worded for this file. The rules -- the
// scalar node-kind whitelist, the single-line rule, every null spelling reading
// as "" -- are internal/frontmatter's, deliberately: they were established by
// probing goccy, and a project file read by a second copy of them would be a
// second set of the same bugs (#100 called this "a third minimal parser" and
// declined it).
var dialect = frontmatter.Dialect{Doc: "a project file", Item: "setting"}

// ConfigError is a markfluence.yaml that could not be understood. Typed so a
// caller can report it as itself rather than under the "resolving the
// documentation root" heading every other discovery failure earns -- the root
// was found; it is the file in it that is wrong.
type ConfigError struct {
	// File is the project file's absolute path.
	File string
	// Line is a 1-based line within it, or 0 when the problem is the document
	// as a whole rather than one setting.
	Line int
	Err  error
}

func (e *ConfigError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("%s:%d: %s", e.File, e.Line, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.File, e.Err)
}

func (e *ConfigError) Unwrap() error { return e.Err }

// loadConfig reads and validates a project file.
//
// An unrecognized key is fatal, and that is the point of validating at all
// rather than a cost of it (#100). A silently ignored `spce: ENG` is wrong for
// every file in the project at once, and a project file written for a newer
// markfluence holds keys this binary would ignore -- so publishing with the
// wrong space or the wrong width, silently and everywhere, is exactly what
// refusing prevents. The file therefore carries no schema version and this
// must not be loosened later; what should improve is the message, which says
// that an older binary is the likely cause.
//
// An empty file and a comment-only one are not errors: a comment and nothing
// else is what ships today, what the README documents, and what export plants.
func loadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		// A file found and not readable is different from an ancestor that
		// could not be stat'd (probeMarker treats that as "not here" and keeps
		// walking). This one is a boundary that exists and cannot be
		// established, which is the case that must not be guessed at.
		return Config{}, &ConfigError{File: path, Err: errors.New(readFailure(err))}
	}
	items, err := dialect.ReadMapping(string(data))
	if err != nil {
		return Config{}, &ConfigError{File: path, Err: err}
	}

	cfg := Config{}
	for _, it := range items {
		want, ok := settings[it.Key]
		if !ok {
			return Config{}, &ConfigError{File: path, Line: it.Line, Err: unknownSetting(it.Key)}
		}
		if want == kindScalar && it.List != nil {
			return Config{}, &ConfigError{File: path, Line: it.Line, Err: fmt.Errorf(
				"setting %q must be a single value, not a list", it.Key)}
		}
		// A declared-but-empty setting is unset rather than an error, matching
		// how frontmatter reads its own fields: `space:` with nothing after it
		// says no more than an absent key does.
		value := strings.TrimSpace(it.Value)
		switch it.Key {
		case "space":
			cfg.Space = value
		case "page_width":
			cfg.PageWidth = value
		}
	}
	return cfg, nil
}

// unknownSetting is the message #100 exists for. One line, because it lands
// verbatim in check --json.
func unknownSetting(key string) error {
	return fmt.Errorf(
		"unknown setting %q (known: %s) -- an unrecognized setting may mean "+
			"this project needs a newer markfluence", key, strings.Join(knownSettings(), ", "))
}

// knownSettings lists the recognized keys, sorted so the message is stable.
func knownSettings() []string {
	out := make([]string, 0, len(settings))
	for key := range settings {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// readFailure strips the os.ReadFile message's own copy of the path, which
// ConfigError already supplies.
func readFailure(err error) string {
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		return pathErr.Err.Error()
	}
	return err.Error()
}
