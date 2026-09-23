package credentialsinit

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/ui"
	"github.com/spf13/cobra"
)

// script is a prompter that answers from a list, then reports end of input.
type script struct {
	answers []string
	prompts []string
}

func (s *script) next(prompt string) (string, error) {
	s.prompts = append(s.prompts, prompt)
	if len(s.answers) == 0 {
		return "", io.EOF
	}
	a := s.answers[0]
	s.answers = s.answers[1:]
	return a, nil
}

func (s *script) Line(prompt string) (string, error)   { return s.next(prompt) }
func (s *script) Secret(prompt string) (string, error) { return s.next(prompt) }

// harness is a run of runInit with everything outside it stubbed.
type harness struct {
	path     string
	env      map[string]string
	cloudID  string
	cloudErr error
	user     *client.User
	userErr  error

	fetched  []string
	verified []client.Config
	messages []string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	return &harness{
		path:    filepath.Join(t.TempDir(), "markfluence", "credentials"),
		env:     map[string]string{},
		cloudID: "cloud-1",
		user:    &client.User{AccountID: "acc-1", DisplayName: "Bot"},
	}
}

func (h *harness) run(answers ...string) error {
	d := deps{
		path: h.path,
		fetchCloudID: func(site string) (string, error) {
			h.fetched = append(h.fetched, site)
			return h.cloudID, h.cloudErr
		},
		verify: func(cfg client.Config) (*client.User, error) {
			h.verified = append(h.verified, cfg)
			return h.user, h.userErr
		},
		getenv:  func(k string) string { return h.env[k] },
		info:    func(m string) { h.messages = append(h.messages, "info: "+m) },
		success: func(m string) { h.messages = append(h.messages, "ok: "+m) },
		warn:    func(m string) { h.messages = append(h.messages, "warn: "+m) },
	}
	return runInit(&script{answers: answers}, d)
}

func (h *harness) said(sub string) bool {
	for _, m := range h.messages {
		if strings.Contains(m, sub) {
			return true
		}
	}
	return false
}

func (h *harness) file(t *testing.T) map[string]string {
	t.Helper()
	got, err := client.ReadCredentials(h.path)
	if err != nil || got == nil {
		t.Fatalf("ReadCredentials = %v, %v", got, err)
	}
	return got.Values
}

func (h *harness) seed(t *testing.T, body string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(h.path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(h.path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(h.path, mode); err != nil {
		t.Fatal(err)
	}
}

const seeded = "# mine\nCONFLUENCE_URL=https://old.atlassian.net\nCONFLUENCE_USERNAME=old@example.com\n" +
	"CONFLUENCE_TOKEN=old-token\nCONFLUENCE_CLOUD_ID=old-cloud\n"

func TestANewFile(t *testing.T) {
	h := newHarness(t)
	if err := h.run("example.atlassian.net", "me@example.com", " tok-en \n"); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"CONFLUENCE_URL":      "https://example.atlassian.net",
		"CONFLUENCE_USERNAME": "me@example.com",
		"CONFLUENCE_TOKEN":    "tok-en", // a pasted space and newline are trimmed
		"CONFLUENCE_CLOUD_ID": "cloud-1",
	}
	got := h.file(t)
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	if len(h.verified) != 1 || h.verified[0].CloudID != "cloud-1" || h.verified[0].Token != "tok-en" {
		t.Errorf("verified = %+v, want one check with the typed values and the cloud ID", h.verified)
	}
	if !h.said("Bot (acc-1)") {
		t.Errorf("messages = %q, want the account reported", h.messages)
	}
}

func TestNormalizeURL(t *testing.T) {
	for in, want := range map[string]string{
		"example.atlassian.net":                                 "https://example.atlassian.net",
		"https://example.atlassian.net/":                        "https://example.atlassian.net",
		"https://example.atlassian.net/wiki":                    "https://example.atlassian.net",
		"https://example.atlassian.net/wiki/spaces/ENG/pages/1": "https://example.atlassian.net",
	} {
		got, _, err := normalizeURL(in)
		if err != nil || got != want {
			t.Errorf("normalizeURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, note, _ := normalizeURL("https://example.atlassian.net/wiki/x"); note == "" {
		t.Error("a dropped path gets no note")
	}
	if _, note, _ := normalizeURL("https://example.atlassian.net"); note != "" {
		t.Errorf("a bare site gets a note: %q", note)
	}
	for _, bad := range []string{"http://example.atlassian.net", "https://"} {
		if _, _, err := normalizeURL(bad); err == nil {
			t.Errorf("normalizeURL(%q) accepted", bad)
		}
	}
}

// TestHTTPIsAskedAgain: a refused URL is a question asked again, not a failure.
func TestHTTPIsAskedAgain(t *testing.T) {
	h := newHarness(t)
	if err := h.run("http://example.atlassian.net", "example.atlassian.net", "me", "tok"); err != nil {
		t.Fatal(err)
	}
	if !h.said("https") || h.file(t)["CONFLUENCE_URL"] != "https://example.atlassian.net" {
		t.Errorf("messages = %q, file = %v", h.messages, h.file(t))
	}
}

// TestPrefillKeepsEveryValue: Enter at every prompt keeps the file, except the
// cloud ID, which is fetched again because the URL may have changed.
func TestPrefillKeepsEveryValue(t *testing.T) {
	h := newHarness(t)
	h.seed(t, seeded, 0o600)
	if err := h.run("", "", ""); err != nil {
		t.Fatal(err)
	}
	got := h.file(t)
	if got["CONFLUENCE_URL"] != "https://old.atlassian.net" || got["CONFLUENCE_USERNAME"] != "old@example.com" ||
		got["CONFLUENCE_TOKEN"] != "old-token" {
		t.Errorf("file = %v, want the old values kept", got)
	}
	if got["CONFLUENCE_CLOUD_ID"] != "cloud-1" || len(h.fetched) != 1 {
		t.Errorf("cloud ID = %q (fetched %v), want it fetched again", got["CONFLUENCE_CLOUD_ID"], h.fetched)
	}
	if !h.said("This will drop comments from the file. Ctrl-C to exit.") ||
		!h.said("Press Enter to keep a current value.") {
		t.Errorf("messages = %q, want the dropped comment reported", h.messages)
	}
}

// TestPrefillNeverShowsTheToken: the token's prompt says a current one exists,
// and nothing more.
func TestPrefillNeverShowsTheToken(t *testing.T) {
	h := newHarness(t)
	h.seed(t, seeded, 0o600)
	s := &script{answers: []string{"", "", ""}}
	d := deps{path: h.path, fetchCloudID: func(string) (string, error) { return "c", nil },
		verify: func(client.Config) (*client.User, error) { return h.user, nil },
		getenv: func(string) string { return "" }, info: func(string) {}, success: func(string) {},
		warn: func(string) {}}
	if err := runInit(s, d); err != nil {
		t.Fatal(err)
	}
	for _, p := range s.prompts {
		if strings.Contains(p, "old-token") {
			t.Errorf("prompt %q shows the token", p)
		}
	}
	if !strings.Contains(strings.Join(s.prompts, "\n"), "Enter keeps the current token") {
		t.Errorf("prompts = %q", s.prompts)
	}
}

// TestALooseFileIsReportedAndTightened: no chmod advice during the read, a
// note that the rewrite fixes it, and a 0600 file afterwards.
func TestALooseFileIsReportedAndTightened(t *testing.T) {
	var warned []string
	client.SetSecurityWarner(func(m string) { warned = append(warned, m) })
	t.Cleanup(func() { client.SetSecurityWarner(nil) })

	h := newHarness(t)
	h.seed(t, seeded, 0o644)
	if err := h.run("", "", ""); err != nil {
		t.Fatal(err)
	}
	if len(warned) != 0 {
		t.Errorf("permission warnings = %q, want none", warned)
	}
	if !h.said("will be 0600") {
		t.Errorf("messages = %q", h.messages)
	}
	fi, err := os.Stat(h.path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %#o, want 0600", fi.Mode().Perm())
	}
}

func TestNoCloudIDFromTenantInfo(t *testing.T) {
	h := newHarness(t)
	h.cloudErr = errors.New("no cloudId")
	if err := h.run("example.atlassian.net", "me", "tok"); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.file(t)["CONFLUENCE_CLOUD_ID"]; ok || !h.said("Could not get the cloud ID") {
		t.Errorf("file = %v, messages = %q", h.file(t), h.messages)
	}
}

// TestNoCloudIDOffAtlassianNet: the gateway is api.atlassian.com, so a site
// elsewhere gets no cloud ID and no tenant_info request.
func TestNoCloudIDOffAtlassianNet(t *testing.T) {
	h := newHarness(t)
	if err := h.run("wiki.example.gov", "me", "tok"); err != nil {
		t.Fatal(err)
	}
	if len(h.fetched) != 0 {
		t.Errorf("fetched %v, want no tenant_info request", h.fetched)
	}
	if _, ok := h.file(t)["CONFLUENCE_CLOUD_ID"]; ok || !h.said("only for a site at atlassian.net") {
		t.Errorf("file = %v, messages = %q", h.file(t), h.messages)
	}
}

// TestEndOfInputAborts: Ctrl-D at any prompt stops, writes nothing, and does
// not loop.
func TestEndOfInputAborts(t *testing.T) {
	for n := 0; n < 3; n++ {
		h := newHarness(t)
		answers := []string{"example.atlassian.net", "me", "tok"}[:n]
		if err := h.run(answers...); !errors.Is(err, errAborted) {
			t.Errorf("EOF after %d answers: err = %v, want errAborted", n, err)
		}
		if _, err := os.Stat(h.path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("EOF after %d answers: a file was written", n)
		}
	}
}

func TestBlankAnswersAreAskedAgain(t *testing.T) {
	h := newHarness(t)
	if err := h.run("", "example.atlassian.net", "  ", "me", "", "tok"); err != nil {
		t.Fatal(err)
	}
	if h.file(t)["CONFLUENCE_TOKEN"] != "tok" {
		t.Errorf("file = %v", h.file(t))
	}
}

func TestAnUnreadableFileFailsBeforeAnyPrompt(t *testing.T) {
	h := newHarness(t)
	if err := os.MkdirAll(h.path, 0o700); err != nil { // a directory where the file goes
		t.Fatal(err)
	}
	s := &script{}
	d := deps{path: h.path}
	if err := runInit(s, d); err == nil || len(s.prompts) != 0 {
		t.Errorf("err = %v, prompts = %q; want an error and no prompt", err, s.prompts)
	}
}

func httpError(status int, url, body string) error {
	return &client.HTTPError{StatusCode: status, Method: "GET", URL: url, Body: body}
}

const (
	gatewayURL = "https://api.atlassian.com/ex/confluence/cloud-1/wiki/rest/api/user/current"
	siteURL    = "https://example.atlassian.net/wiki/rest/api/user/current"
)

// TestRefusals: each shape that means the credentials do not work writes
// nothing and leaves an existing file byte-identical.
func TestRefusals(t *testing.T) {
	tests := []struct {
		name string
		err  error
		user *client.User
		want string
	}{
		{"v1 rejected credential", httpError(403, gatewayURL,
			`{"message":"Request rejected because caller cannot access Confluence"}`), nil, "refused"},
		{"site HTML 401", httpError(401, siteURL, `<html>401</html>`), nil, "scoped token"},
		{"other 401", httpError(401, gatewayURL, `{"code":401,"message":"Unauthorized"}`), nil, "refused"},
		{"other 403", httpError(403, gatewayURL, `{"message":"no"}`), nil, "refused"},
		{"404", httpError(404, siteURL, `nope`), nil, "does not look like a Confluence site"},
		{"anonymous", nil, &client.User{DisplayName: "Anonymous"}, "without accepting"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			h.seed(t, seeded, 0o600)
			h.userErr, h.user = tt.err, tt.user
			err := h.run("example.atlassian.net", "me", "tok")
			if err == nil || !strings.Contains(err.Error(), tt.want) ||
				!strings.Contains(err.Error(), "nothing was written") {
				t.Errorf("err = %v, want %q and nothing written", err, tt.want)
			}
			if b, _ := os.ReadFile(h.path); string(b) != seeded {
				t.Errorf("the existing file changed to %q", b)
			}
		})
	}
}

// TestInconclusiveChecksAsk: when the answer cannot say the credentials are
// wrong, a "no" writes nothing and a "yes" writes and says it was unchecked.
func TestInconclusiveChecksAsk(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"server error", httpError(503, gatewayURL, `busy`)},
		{"no response", errors.New("dial tcp: no such host")},
	}
	for _, tt := range tests {
		t.Run(tt.name+" no", func(t *testing.T) {
			h := newHarness(t)
			h.userErr, h.user = tt.err, nil
			if err := h.run("example.atlassian.net", "me", "tok", "n"); err == nil {
				t.Error("want an error for a no")
			}
			if _, err := os.Stat(h.path); !errors.Is(err, os.ErrNotExist) {
				t.Error("a file was written")
			}
		})
		t.Run(tt.name+" yes", func(t *testing.T) {
			h := newHarness(t)
			h.userErr, h.user = tt.err, nil
			if err := h.run("example.atlassian.net", "me", "tok", "y"); err != nil {
				t.Fatal(err)
			}
			if h.file(t)["CONFLUENCE_TOKEN"] != "tok" || !h.said("without checking") {
				t.Errorf("file = %v, messages = %q", h.file(t), h.messages)
			}
		})
	}
}

// TestTheEnvironmentIsReported: one case per way an exported variable changes
// what the file does.
func TestTheEnvironmentIsReported(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string // "" means no warning about the environment
	}{
		{"nothing", nil, ""},
		{"all three", map[string]string{client.URLVar: "u", client.UsernameVar: "n", client.TokenVar: "t"}, "will not read"},
		{"URL and token", map[string]string{client.URLVar: "u", client.TokenVar: "t"}, "used instead of the ones"},
		{"URL alone", map[string]string{client.URLVar: "u"}, "same place"},
		{"token alone", map[string]string{client.TokenVar: "t"}, "same place"},
		{"username", map[string]string{client.UsernameVar: "n"}, "CONFLUENCE_USERNAME is set"},
		{"cloud ID", map[string]string{client.CloudIDVar: "c"}, "will ignore it"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			for k, v := range tt.env {
				h.env[k] = v
			}
			if err := h.run("example.atlassian.net", "me", "tok"); err != nil {
				t.Fatal(err)
			}
			if tt.want == "" {
				if h.said("environment") {
					t.Errorf("messages = %q, want no environment warning", h.messages)
				}
				return
			}
			if !h.said(tt.want) {
				t.Errorf("messages = %q, want %q", h.messages, tt.want)
			}
		})
	}
}

func TestReadLine(t *testing.T) {
	r := strings.NewReader("first\nsecond")
	if got, err := readLine(r); got != "first" || err != nil {
		t.Errorf("readLine = %q, %v", got, err)
	}
	if got, err := readLine(r); got != "second" || err != nil {
		t.Errorf("readLine at a partial last line = %q, %v", got, err)
	}
	if _, err := readLine(r); !errors.Is(err, io.EOF) {
		t.Errorf("readLine at the end = %v, want io.EOF", err)
	}
}

// refusalCommand stands in for the root: run reads --json and --env-file,
// which the root defines.
func refusalCommand(t *testing.T, args ...string) *cobra.Command {
	t.Helper()
	c := &cobra.Command{}
	c.Flags().Bool("json", false, "")
	c.Flags().String("env-file", "", "")
	if err := c.Flags().Parse(args); err != nil {
		t.Fatal(err)
	}
	return c
}

// TestRefusalsBeforeAnyPrompt: each refusal is a plain error, which Execute
// prints (as an errorObject under --json) and exits 2 for -- not a silent exit,
// which under --json would print nothing at all.
func TestRefusalsBeforeAnyPrompt(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, tt := range []struct {
		args []string
		want string
	}{
		{[]string{"--json"}, "without --json"},
		{[]string{"--env-file", "x.env"}, "without --env-file"},
	} {
		err := run(refusalCommand(t, tt.args...), nil)
		if err == nil || ui.IsSilent(err) || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%v: err = %v, want a plain error with %q", tt.args, err, tt.want)
		}
	}

	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	if err := run(refusalCommand(t), nil); err == nil || ui.IsSilent(err) ||
		!strings.Contains(err.Error(), "nowhere to write") {
		t.Errorf("no home: err = %v", err)
	}
}

func TestNotATerminal(t *testing.T) {
	if isTerminal(os.Stdin) && isTerminal(os.Stderr) {
		t.Skip("the test is running at a terminal")
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	err := run(refusalCommand(t), nil)
	if err == nil || ui.IsSilent(err) || !strings.Contains(err.Error(), "needs a terminal") {
		t.Errorf("err = %v", err)
	}
	if _, serr := os.Stat(client.CredentialsPath()); !errors.Is(serr, os.ErrNotExist) {
		t.Error("a file was written")
	}
}

// TestAScopeMismatchCountsAsChecked: the token authenticated, so it is saved
// as checked, with a warning, and no question is asked.
func TestAScopeMismatchCountsAsChecked(t *testing.T) {
	h := newHarness(t)
	h.userErr, h.user = httpError(401, gatewayURL, `{"code":401,"message":"Unauthorized; scope does not match"}`), nil
	if err := h.run("example.atlassian.net", "me", "tok"); err != nil {
		t.Fatal(err)
	}
	if !h.said("read:confluence-user") || !h.said("ok: Wrote") || h.said("without checking") {
		t.Errorf("messages = %q, want a checked write with the scope warning", h.messages)
	}
}

// TestANewSiteDoesNotKeepTheToken: a token belongs to its site, so a changed
// URL gets no "Enter keeps the current token", and no kept cloud ID.
func TestANewSiteDoesNotKeepTheToken(t *testing.T) {
	h := newHarness(t)
	h.seed(t, seeded, 0o600)
	h.cloudErr = errors.New("offline")
	// A blank token answer is asked again rather than keeping old-token.
	if err := h.run("new.atlassian.net", "", "", "new-token"); err != nil {
		t.Fatal(err)
	}
	got := h.file(t)
	if got[client.TokenVar] != "new-token" || h.verified[0].Token != "new-token" {
		t.Errorf("token = %q, checked with %q; want new-token for both", got[client.TokenVar], h.verified[0].Token)
	}
	if _, ok := got[client.CloudIDVar]; ok {
		t.Errorf("cloud ID = %q, want the old site's not kept", got[client.CloudIDVar])
	}
	if !h.said("site changed") {
		t.Errorf("messages = %q", h.messages)
	}
}

// TestTheSameSiteKeepsItsCloudID: rotating a token must not lose a cloud ID
// that cannot be fetched again right now, or that was set by hand for a host
// markfluence does not fetch one for. A trailing slash in the old URL is
// still the same site.
func TestTheSameSiteKeepsItsCloudID(t *testing.T) {
	h := newHarness(t)
	h.seed(t, strings.Replace(seeded, "old.atlassian.net", "old.atlassian.net/", 1), 0o600)
	h.cloudErr = errors.New("offline")
	if err := h.run("", "", "rotated"); err != nil {
		t.Fatal(err)
	}
	if got := h.file(t); got[client.CloudIDVar] != "old-cloud" || got[client.TokenVar] != "rotated" {
		t.Errorf("file = %v, want old-cloud kept and the token rotated", got)
	}

	h = newHarness(t)
	h.seed(t, strings.Replace(seeded, "old.atlassian.net", "wiki.example.gov", 1), 0o600)
	if err := h.run("", "", ""); err != nil {
		t.Fatal(err)
	}
	if got := h.file(t); got[client.CloudIDVar] != "old-cloud" || len(h.fetched) != 0 {
		t.Errorf("file = %v, fetched %v; want the hand-set cloud ID kept, no fetch", got, h.fetched)
	}
}

// TestTheHostIsLowercased: a capitalized atlassian.net host is still one.
func TestTheHostIsLowercased(t *testing.T) {
	h := newHarness(t)
	if err := h.run("Example.Atlassian.NET", "me", "tok"); err != nil {
		t.Fatal(err)
	}
	if got := h.file(t); got[client.URLVar] != "https://example.atlassian.net" || got[client.CloudIDVar] != "cloud-1" {
		t.Errorf("file = %v", got)
	}
}

// TestTheEnvironmentIsReportedFirst: before any question, so nobody answers
// everything to learn the file will not be read.
func TestTheEnvironmentIsReportedFirst(t *testing.T) {
	h := newHarness(t)
	h.env[client.URLVar] = "u"
	if err := h.run(); !errors.Is(err, errAborted) {
		t.Fatalf("err = %v", err)
	}
	if !h.said("same place") {
		t.Errorf("messages = %q, want the environment reported before the first prompt", h.messages)
	}
}

func TestNormalizeURLNotes(t *testing.T) {
	if _, note, _ := normalizeURL("https://me:secret@example.atlassian.net"); !strings.Contains(note, "not saved") {
		t.Errorf("userinfo note = %q", note)
	}
	if _, _, err := normalizeURL("foo bar"); err == nil || !strings.Contains(err.Error(), `"foo bar"`) ||
		strings.Contains(err.Error(), "https://") {
		t.Errorf("err = %v, want it to name what was typed", err)
	}
}
