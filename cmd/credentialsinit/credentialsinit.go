// Package credentialsinit implements the `markfluence credentials-init`
// command: prompt for the Confluence credentials, check them, and write the
// user's credentials file (#193, _plans/051).
package credentialsinit

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/completion"
	"github.com/mozilla/markfluence/internal/ui"
	"github.com/spf13/cobra"
)

// Cmd is the credentials-init command.
var Cmd = &cobra.Command{
	Use:   "credentials-init",
	Short: "Write your credentials file, after checking the credentials",
	Long: "Ask for your Confluence site URL, your username, and your API token, check\n" +
		"them with Confluence, and write them to your credentials file,\n" +
		"~/.config/markfluence/credentials ($XDG_CONFIG_HOME/markfluence/credentials if\n" +
		"you set XDG_CONFIG_HOME to an absolute path). The token is read without\n" +
		"echo, so it does not show on the screen or go into your shell history.\n\n" +
		"For a site at atlassian.net, credentials-init also gets the cloud ID of the\n" +
		"site and saves it. A scoped API token needs the cloud ID, and a normal token\n" +
		"works with it too, so you do not have to know which kind of token you have.\n\n" +
		"Before it saves, credentials-init sends the request that markfluence\n" +
		"user-info sends, and shows the account that the credentials belong to. If\n" +
		"Confluence refuses the credentials, it saves nothing. If it cannot tell, for\n" +
		"example because it cannot reach the site, it asks you whether to save.\n\n" +
		"If you already have a credentials file, each question shows the current\n" +
		"value, and Enter keeps it. To change only the token, press Enter twice and\n" +
		"paste the new token. credentials-init writes the whole file again, so it\n" +
		"tells you first if your file has comments or other lines that it will not\n" +
		"keep. The file gets mode 0600.\n\n" +
		"credentials-init writes only the credentials file. It writes nothing in the\n" +
		"current directory. It needs a terminal: in CI, set the CONFLUENCE_*\n" +
		"environment variables from the secrets of the CI system instead.",
	Example: "  # Set up this computer\n" +
		"  markfluence credentials-init\n\n" +
		"  # Then make sure that it works\n" +
		"  markfluence user-info",
	Args:              cobra.NoArgs,
	ValidArgsFunction: completion.Values(),
	RunE:              run,
}

func run(cmd *cobra.Command, _ []string) error {
	// Each refusal here is a plain error, which Execute prints -- as an
	// errorObject under --json, where every ui helper prints nothing -- and
	// exits 2 for.
	if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
		return errors.New("credentials-init asks questions at a terminal and writes no JSON: run it without --json")
	}
	if cmd.Flags().Changed("env-file") {
		return errors.New("credentials-init writes only your credentials file, and --env-file does not choose " +
			"where: run it without --env-file")
	}
	path := client.CredentialsPath()
	if path == "" {
		return errors.New("there is nowhere to write a credentials file: set HOME, or XDG_CONFIG_HOME, to an " +
			"absolute path")
	}
	if !isTerminal(os.Stdin) || !isTerminal(os.Stderr) {
		return fmt.Errorf("credentials-init needs a terminal to ask its questions. In CI, set the CONFLUENCE_* "+
			"environment variables instead. See %s", client.CredentialsDoc)
	}

	err := runInit(newTerminal(os.Stdin, os.Stderr), deps{
		path:         path,
		fetchCloudID: client.FetchCloudID,
		verify:       verify,
		getenv:       os.Getenv,
		// All of it to stderr, beside the prompts: see ui.InfoStderr.
		info:    ui.InfoStderr,
		success: ui.SuccessStderr,
		warn:    ui.Warn,
	})
	if err != nil {
		ui.Error(err.Error())
		return ui.SilentExit(1)
	}
	return nil
}

// verify is the check before saving: the request user-info makes.
func verify(cfg client.Config) (*client.User, error) {
	return client.New(cfg).CurrentUser()
}

// prompter asks the questions. The terminal is the only implementation that
// touches a tty, so everything else is testable with a scripted one. Both
// methods return io.EOF when input ends.
type prompter interface {
	// Line asks for one line, shown with prompt, and returns it as typed.
	Line(prompt string) (string, error)
	// Secret asks for one line without echo.
	Secret(prompt string) (string, error)
}

// deps is what runInit reaches outside itself for.
type deps struct {
	path         string
	fetchCloudID func(siteURL string) (string, error)
	verify       func(client.Config) (*client.User, error)
	getenv       func(string) string
	info         func(string)
	success      func(string)
	warn         func(string)
}

// errAborted is end of input at a prompt.
var errAborted = errors.New("input ended; nothing was written")

func runInit(p prompter, d deps) error {
	shown := client.DisplayPath(d.path)
	existing, err := client.ReadCredentials(d.path)
	if err != nil {
		return err
	}
	current := map[string]string{}
	if existing != nil {
		current = existing.Values
		d.info(updatingNotice(shown, existing))
		if client.LooseMode(existing.Mode) {
			d.info(fmt.Sprintf("%s is mode %#o now; the new file will be 0600.", shown, existing.Mode))
		}
		d.info("Press Enter to keep a current value.")
	}
	// Before any question: an exported variable can make the file pointless,
	// and finding that out after answering everything is too late.
	warnEnvironment(d)

	siteURL, err := askURL(p, d, current[client.URLVar])
	if err != nil {
		return err
	}
	// The current token and cloud ID belong to the current site. Offering
	// them for a different one would pair one site's token with another's URL,
	// which is what Resolve's same-source rule exists to stop.
	sameSite := false
	if old, _, err := normalizeURL(current[client.URLVar]); err == nil && old == siteURL {
		sameSite = true
	} else if current[client.TokenVar] != "" {
		d.info("The site changed, so the current token is not kept.")
	}
	keptCloudID := ""
	if sameSite {
		keptCloudID = current[client.CloudIDVar]
	}
	cloudID := findCloudID(d, siteURL, keptCloudID)
	username, err := askLine(p, d, "Username (your email address)", current[client.UsernameVar])
	if err != nil {
		return err
	}
	hasToken := sameSite && current[client.TokenVar] != ""
	token, err := askToken(p, d, hasToken)
	if err != nil {
		return err
	}
	if token == "" {
		token = current[client.TokenVar]
	}

	cfg := client.Config{SiteURL: siteURL, CloudID: cloudID, Username: username, Token: token}
	checked, err := check(p, d, cfg)
	if err != nil {
		return err
	}

	values := map[string]string{
		client.URLVar: siteURL, client.UsernameVar: username, client.TokenVar: token, client.CloudIDVar: cloudID,
	}
	if err := client.WriteCredentials(d.path, values); err != nil {
		return err
	}
	if checked {
		d.success("Wrote " + shown + ".")
	} else {
		d.warn("Wrote " + shown + " without checking the credentials.")
	}
	return nil
}

// updatingNotice says, before any question, that the file is rewritten and
// what of it will not survive, while Ctrl-C still costs nothing.
func updatingNotice(shown string, f *client.CredentialsFile) string {
	var drop string
	switch {
	case f.Comments > 0 && f.Others > 0:
		drop = " This will drop comments, and lines that are not CONFLUENCE_* settings, from the file. " +
			"Ctrl-C to exit."
	case f.Comments > 0:
		drop = " This will drop comments from the file. Ctrl-C to exit."
	case f.Others > 0:
		drop = " This will drop lines that are not CONFLUENCE_* settings from the file. Ctrl-C to exit."
	}
	return "Updating " + shown + "." + drop
}

// askURL asks for the site URL until it gets one it can use.
func askURL(p prompter, d deps, current string) (string, error) {
	for {
		v, err := askLine(p, d, "Confluence site URL", current)
		if err != nil {
			return "", err
		}
		u, note, err := normalizeURL(v)
		if err != nil {
			d.warn(err.Error())
			continue
		}
		if note != "" {
			d.info(note)
		}
		return u, nil
	}
}

// askLine asks until it gets a non-blank answer, or the current value when
// there is one and the answer is blank.
func askLine(p prompter, d deps, prompt, current string) (string, error) {
	shown := prompt
	if current != "" {
		shown += " [" + current + "]"
	}
	for {
		v, err := p.Line(shown + ": ")
		if errors.Is(err, io.EOF) {
			return "", errAborted
		}
		if err != nil {
			return "", err
		}
		if v = strings.TrimSpace(v); v != "" {
			return v, nil
		}
		if current != "" {
			return current, nil
		}
		d.warn("An answer is needed.")
	}
}

// askToken asks for the token without echo. A blank answer keeps the current
// token when there is one, reported as "".
func askToken(p prompter, d deps, hasCurrent bool) (string, error) {
	prompt := "API token: "
	if hasCurrent {
		prompt = "API token (Enter keeps the current token): "
	}
	for {
		v, err := p.Secret(prompt)
		if errors.Is(err, io.EOF) {
			return "", errAborted
		}
		if err != nil {
			return "", err
		}
		if v = strings.TrimSpace(v); v != "" || hasCurrent {
			return v, nil
		}
		d.warn("An answer is needed.")
	}
}

// normalizeURL reduces what was typed to a site URL, scheme and lowercased
// host. Every request appends /wiki/... to the site URL, so a pasted page URL,
// or a /wiki suffix, would otherwise break every request. Only https is
// accepted: the token goes out as basic auth.
func normalizeURL(typed string) (site, note string, err error) {
	v := strings.TrimSpace(typed)
	if !strings.Contains(v, "://") {
		v = "https://" + v
	}
	u, err := url.Parse(v)
	if err != nil || u.Host == "" {
		return "", "", fmt.Errorf("%q is not a URL", typed)
	}
	if u.Scheme != "https" {
		return "", "", fmt.Errorf("the URL must use https, because the token is sent with each request: %s", typed)
	}
	site = "https://" + strings.ToLower(u.Host)
	switch {
	case u.User != nil:
		note = "Using " + site + ". The user name and password in that URL are not saved."
	case strings.TrimRight(u.Path, "/") != "" || u.RawQuery != "" || u.Fragment != "":
		note = "Using " + site + ", the site part of that URL."
	}
	return site, note, nil
}

// findCloudID gets the site's cloud ID, or keeps the current one (kept is ""
// unless the site is unchanged), or returns "" with a message saying why there
// is none.
//
// It fetches only for atlassian.net: the gateway markfluence routes a cloud ID
// through is api.atlassian.com, which Government and isolated Cloud sites do
// not use. A cloud ID that is already in the file for another host was put
// there by hand, and is kept rather than second-guessed.
func findCloudID(d deps, siteURL, kept string) string {
	u, err := url.Parse(siteURL)
	if err != nil || !strings.HasSuffix(u.Hostname(), ".atlassian.net") {
		if kept != "" {
			d.info("Keeping the current cloud ID, " + kept + ".")
			return kept
		}
		d.info("Saving no cloud ID: markfluence gets one only for a site at atlassian.net. A scoped " +
			"API token needs one; set " + client.CloudIDVar + " in the file by hand.")
		return ""
	}
	id, err := d.fetchCloudID(siteURL)
	switch {
	case err == nil:
		d.info("Cloud ID: " + id)
		return id
	case kept != "":
		d.warn(fmt.Sprintf("Could not get the cloud ID of %s, so the current one, %s, is kept. (%v)",
			siteURL, kept, err))
		return kept
	default:
		d.warn(fmt.Sprintf("Could not get the cloud ID of %s, so none is saved. A normal API token "+
			"works without one; for a scoped token, run credentials-init again later. (%v)", siteURL, err))
		return ""
	}
}

// verdict is what a failed check means for the file.
type verdict int

const (
	refuse verdict = iota // the credentials do not work: write nothing
	ask                   // cannot tell: ask whether to save
	accept                // they work, with a caveat worth a warning
)

// check sends the request user-info makes. It reports whether the
// credentials were checked, or an error when nothing is to be written: a
// refusal, or a "no" to saving unchecked credentials.
func check(p prompter, d deps, cfg client.Config) (bool, error) {
	d.info("Checking the credentials...")
	user, err := d.verify(cfg)
	if err == nil {
		if user == nil || user.AccountID == "" {
			// Anonymous: the site answered without authenticating anyone.
			return false, fmt.Errorf("%s answered without accepting the credentials for %s; nothing "+
				"was written", cfg.SiteURL, cfg.Username)
		}
		d.success(fmt.Sprintf("The credentials belong to %s (%s).", user.DisplayName, user.AccountID))
		return true, nil
	}

	v, why := classify(err, cfg)
	switch v {
	case refuse:
		return false, fmt.Errorf("%s; nothing was written. (%v)", why, err)
	case accept:
		d.warn(why)
		return true, nil
	}
	d.warn(why)
	answer, perr := p.Line("Save anyway? [y/N]: ")
	if perr != nil && !errors.Is(perr, io.EOF) {
		return false, perr
	}
	if a := strings.ToLower(strings.TrimSpace(answer)); a == "y" || a == "yes" {
		return false, nil
	}
	return false, errors.New("nothing was written")
}

// classify decides what a failed check means, by the shape of the response
// rather than its status alone: the same status arrives for unrelated reasons
// (docs/confluence/api.md). The shapes are the ones HTTPError's hint matches,
// so the two cannot disagree.
func classify(err error, cfg client.Config) (verdict, string) {
	var he *client.HTTPError
	if !errors.As(err, &he) {
		return ask, fmt.Sprintf("Could not check the credentials: %v", err)
	}
	who := cfg.Username + " at " + cfg.SiteURL
	switch {
	case he.RejectedCredential():
		return refuse, "Confluence refused the credentials for " + who
	case he.ScopeMismatch():
		// The token authenticated: this is a checked answer, not an unknown one.
		return accept, "The credentials work, but the token has no scope for the user-info request " +
			"(read:confluence-user), so markfluence user-info will not work with it."
	case he.SiteRejectedAuth():
		return refuse, "The site refused the credentials for " + who + " before they reached the API. " +
			"That is what a scoped token gets without a cloud ID"
	case he.StatusCode == http.StatusUnauthorized || he.StatusCode == http.StatusForbidden:
		return refuse, "Confluence refused the credentials for " + who
	case he.StatusCode == http.StatusNotFound:
		return refuse, cfg.SiteURL + " does not look like a Confluence site"
	default:
		return ask, fmt.Sprintf("Could not check the credentials: %v", err)
	}
}

// warnEnvironment reports what exported CONFLUENCE_* variables will do to the
// file, following Resolve's rules rather than a blanket "overrides".
func warnEnvironment(d deps) {
	set := func(k string) bool { return d.getenv(k) != "" }
	siteURL, username, token := set(client.URLVar), set(client.UsernameVar), set(client.TokenVar)
	switch {
	case siteURL && username && token:
		d.warn("CONFLUENCE_URL, CONFLUENCE_USERNAME, and CONFLUENCE_TOKEN are set in the environment, " +
			"so markfluence will not read the credentials file at all until you unset them.")
		return
	case siteURL && token:
		d.warn("CONFLUENCE_URL and CONFLUENCE_TOKEN are set in the environment, and will be used instead " +
			"of the ones in the file until you unset them.")
		return
	case siteURL || token:
		d.warn("CONFLUENCE_URL or CONFLUENCE_TOKEN is set in the environment. The URL and the token " +
			"must come from the same place, so every command will fail until you unset it.")
	}
	if username {
		d.warn("CONFLUENCE_USERNAME is set in the environment, and will be used instead of the one in " +
			"the file.")
	}
	// With the URL exported, the cloud ID follows it, and that case is
	// reported above already.
	if !siteURL && set(client.CloudIDVar) {
		d.warn("CONFLUENCE_CLOUD_ID is set in the environment. markfluence will ignore it, and warn on " +
			"every run, because the URL comes from the file. Unset it.")
	}
}
