package userfind

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/clienttest"
	"github.com/mozilla/markfluence/internal/ui"
	"github.com/spf13/cobra"
)

// stub serves the user-search route. rows(start) returns the results JSON for
// that offset; status, when non-zero, is served instead of any body.
type stub struct {
	rows   func(start int) string
	status int
	// calls counts requests, so a test can prove a pre-flight refusal made none.
	calls *int
}

func (s stub) handler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if s.calls != nil {
			*s.calls++
		}
		if s.status != 0 {
			w.WriteHeader(s.status)
			_, _ = fmt.Fprint(w, `{"message":"nope"}`)
			return
		}
		start, _ := strconv.Atoi(r.URL.Query().Get("start"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"results":%s,"start":%d,"limit":100,"totalSize":1}`, s.rows(start), start)
	}
}

// people builds a results array from name/id pairs.
func people(pairs ...[2]string) string {
	rows := make([]string, 0, len(pairs))
	for _, p := range pairs {
		rows = append(rows, fmt.Sprintf(
			`{"user":{"accountId":%q,"displayName":%q,"type":"known"}}`, p[1], p[0]))
	}
	return "[" + strings.Join(rows, ",") + "]"
}

func testCmd(t *testing.T, url string) *cobra.Command {
	t.Helper()
	t.Setenv("CONFLUENCE_TOKEN", "t")
	c := &cobra.Command{}
	c.Flags().String("url", url, "")
	c.Flags().String("username", "u", "")
	c.Flags().String("cloud-id", "", "")
	c.Flags().String("env-file", "", "")
	return c
}

// outcome is what one run produced on each stream, plus its exit code.
type outcome struct {
	stdout, stderr string
	exit           int
}

// runFind executes the command against a stub, capturing both streams.
//
// It runs from an empty directory so the repository's own .env cannot be read
// as this test's configuration.
func runFind(t *testing.T, s stub, limit string, args ...string) outcome {
	t.Helper()
	c := clienttest.New(t, s.handler(t))
	cmd := testCmd(t, c.SiteURL())

	old := limitOpt
	limitOpt = limit
	t.Cleanup(func() { limitOpt = old })

	outR, outW, _ := os.Pipe()
	errR, errW, _ := os.Pipe()
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}

	runErr := run(cmd, args)

	if err := os.Chdir(wd); err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = oldOut, oldErr
	_ = outW.Close()
	_ = errW.Close()

	out, _ := io.ReadAll(outR)
	errOut, _ := io.ReadAll(errR)

	exit := 0
	if runErr != nil {
		if !ui.IsSilent(runErr) {
			t.Fatalf("run returned a non-silent error: %v", runErr)
		}
		exit = ui.ExitCode(runErr)
	}
	return outcome{stdout: string(out), stderr: string(errOut), exit: exit}
}

// The whole point of the command: the second line is paste-ready markdown.
func TestTheMentionLineIsTheAnswer(t *testing.T) {
	o := runFind(t, stub{rows: func(int) string {
		return people([2]string{"William Kahn-Greene", "60c36d0718e9f60071326951"})
	}}, "10", "kahn")

	if o.exit != 0 {
		t.Fatalf("exit = %d, want 0; stderr:\n%s", o.exit, o.stderr)
	}
	want := "William Kahn-Greene  60c36d0718e9f60071326951\n" +
		"  [@William Kahn-Greene](https://home.atlassian.com/people/60c36d0718e9f60071326951)\n"
	if o.stdout != want {
		t.Errorf("stdout =\n%q\nwant\n%q", o.stdout, want)
	}
}

// A display name holding brackets must still produce a parseable link, or the
// line pastes as literal text rather than a mention.
func TestBracketsInANameAreEscaped(t *testing.T) {
	o := runFind(t, stub{rows: func(int) string {
		return people([2]string{"[TEMPLATE] ASCII art", "712020:abc"})
	}}, "10", "template")

	if !strings.Contains(o.stdout, `[@\[TEMPLATE\] ASCII art](https://home.atlassian.com/people/712020:abc)`) {
		t.Errorf("stdout did not escape the brackets in the link text:\n%s", o.stdout)
	}
}

// Blocks are separated by a blank line, since a hit is two lines and running
// them together makes the ids unreadable.
func TestHitsAreSeparatedByABlankLine(t *testing.T) {
	o := runFind(t, stub{rows: func(int) string {
		return people([2]string{"Ana", "id-a"}, [2]string{"Bo", "id-b"})
	}}, "10", "a")

	if !strings.Contains(o.stdout, "people/id-a)\n\nBo  id-b\n") {
		t.Errorf("hits are not separated by a blank line:\n%q", o.stdout)
	}
}

// Finding nobody is a success, matching find and search: an empty answer is one
// a caller acts on.
func TestNoMatchesIsExitZero(t *testing.T) {
	o := runFind(t, stub{rows: func(int) string { return "[]" }}, "10", "zzznobody")

	if o.exit != 0 {
		t.Fatalf("exit = %d, want 0", o.exit)
	}
	if !strings.Contains(o.stderr+o.stdout, "No users found.") {
		t.Errorf("did not report an empty result:\nstdout %q\nstderr %q", o.stdout, o.stderr)
	}
}

// The "more exist" notice goes to stderr via ui.Info's own stream discipline,
// and names the value that lifts the bound.
func TestMoreExistIsReported(t *testing.T) {
	o := runFind(t, stub{rows: func(int) string {
		return people([2]string{"Ana", "id-a"}, [2]string{"Bo", "id-b"}, [2]string{"Cy", "id-c"})
	}}, "2", "a")

	all := o.stdout + o.stderr
	if !strings.Contains(all, "more exist") || !strings.Contains(all, "--limit all") {
		t.Errorf("no usable more-exist notice:\nstdout %q\nstderr %q", o.stdout, o.stderr)
	}
	if strings.Contains(o.stdout, "id-c") {
		t.Errorf("a third hit was printed against --limit 2:\n%s", o.stdout)
	}
}

// An empty NAME is a usage error recognised locally. The route answers an empty
// match with a 500, which would otherwise surface as an operational failure
// with the wrong exit code -- so this also proves no request was made.
func TestEmptyNameIsRefusedWithoutARequest(t *testing.T) {
	calls := 0
	o := runFind(t, stub{rows: func(int) string { return "[]" }, calls: &calls}, "10", "   ")

	if o.exit != 2 {
		t.Errorf("exit = %d, want 2", o.exit)
	}
	if calls != 0 {
		t.Errorf("made %d requests for an empty name, want 0", calls)
	}
}

// A bad --limit is refused before any request too.
func TestBadLimitIsRefusedWithoutARequest(t *testing.T) {
	calls := 0
	o := runFind(t, stub{rows: func(int) string { return "[]" }, calls: &calls}, "0", "kahn")

	if o.exit != 2 {
		t.Errorf("exit = %d, want 2", o.exit)
	}
	if calls != 0 {
		t.Errorf("made %d requests for --limit 0, want 0", calls)
	}
}

// A failing lookup is the normal operational exit code, 1 -- diff's departure
// to diff(1)'s codes is diff's alone.
func TestLookupFailureExitsOne(t *testing.T) {
	o := runFind(t, stub{status: http.StatusInternalServerError}, "10", "kahn")

	if o.exit != 1 {
		t.Fatalf("exit = %d, want 1; stderr:\n%s", o.exit, o.stderr)
	}
	if o.stdout != "" {
		t.Errorf("a failed lookup wrote to stdout:\n%s", o.stdout)
	}
}

func TestParseLimit(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    int
		wantErr bool
	}{
		{"number", "25", 25, false},
		{"one", "1", 1, false},
		{"all", limitAll, 0, false},
		{"the default", defaultLimit, 10, false},
		{"zero", "0", 0, true},
		{"negative", "-1", 0, true},
		{"not a number", "lots", 0, true},
		{"empty", "", 0, true},
		{"float", "2.5", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseLimit(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseLimit(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
			if err != nil {
				if !strings.Contains(err.Error(), limitAll) {
					t.Errorf("error = %q, want it to name %q as the unlimited value", err, limitAll)
				}
				return
			}
			if got != tt.want {
				t.Errorf("parseLimit(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

// blocks is exercised end to end above; this pins that it adds no trailing
// blank line, which fmt.Println would turn into two.
func TestBlocksHasNoTrailingNewline(t *testing.T) {
	got := blocks([]client.UserMatch{{AccountID: "id-a", DisplayName: "Ana"}})
	if strings.HasSuffix(got, "\n") {
		t.Errorf("blocks = %q, want no trailing newline", got)
	}
}
