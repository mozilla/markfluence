// Package client is an HTTP client for the Confluence REST API. It wraps
// net/http with basic auth and the handful of calls markfluence needs.
//
// Requests are built as absolute URLs off the base URL. Pages and content
// properties use the Confluence v2 API; attachment writes and the user lookup
// use v1 (/wiki/rest/api/...) since v2 doesn't cover them.
//
// A client carries two bases. baseURL is where requests go; siteURL is the
// human-facing site the pages live on. They differ only when a cloud ID selects
// the platform API gateway (see Config): a scoped API token -- the kind a service
// account gets -- is rejected against the site domain and must go through
// https://api.atlassian.com/ex/confluence/{cloudId} instead. The path suffixes are
// identical under the gateway, so every call below is written against baseURL
// unchanged. siteURL exists because the gateway host must never reach a reader:
// it's wrong in printed URLs and, worse, would be written into published page
// content by the converter's link rewriting.
package client

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mozilla/markfluence/internal/attachref"
)

// An uploaded attachment carries markfluence bookkeeping in its comment: the
// checksum a later run compares to tell whether the local file changed, and the
// markdown image path it was published from, so reading the page back recovers
// the image's original location exactly instead of inferring it from the
// attachment name.
const (
	// attachmentCommentPrefix marks an attachment as markfluence-managed. This
	// is the only comment form markfluence writes or recognizes -- no older
	// format is parsed. An attachment stamped by a markfluence predating a
	// comment-format change (the Python tool's "mzcld:checksum:" prefix; this
	// tool's own 64-hex checksum before it was truncated) reads as unmanaged
	// and is re-uploaded once, the same as any other hand-uploaded file.
	attachmentCommentPrefix = "markfluence: "
	// checksumHexLen truncates a file's SHA-256 hex digest to 128 bits before
	// it goes into a comment. The comment only needs to detect that a file's
	// bytes changed, not resist an adversary, so this is the bigger and
	// cheaper of the two levers on the 255-character comment ceiling --
	// truncating it buys roughly twice what shortening the "markfluence: "
	// prefix would (#101), and the prefix is worth keeping full-length: it is
	// the ownership marker S5 rests on, readable by a human browsing a page's
	// attachments in the Confluence UI.
	checksumHexLen = 32
)

const (
	timeoutRead   = 30 * time.Second
	timeoutWrite  = 60 * time.Second
	timeoutUpload = 120 * time.Second
	// timeoutDownload matches timeoutUpload: 30s is thin for a large attachment.
	timeoutDownload = 120 * time.Second
)

const (
	// maxRetries is the number of retries (so up to maxRetries+1 attempts) for
	// transient failures.
	maxRetries = 4
	// baseBackoff is the first retry delay; it doubles each attempt up to maxBackoff.
	baseBackoff = time.Second
	maxBackoff  = 30 * time.Second
)

// sleep is the backoff pause primitive; a package variable so tests can stub it.
var sleep = time.Sleep

// gatewayPrefix is the platform API gateway a scoped token must use, joined with
// the cloud ID to form the request base.
const gatewayPrefix = "https://api.atlassian.com/ex/confluence/"

// ConfluenceClient talks to the Confluence REST API as a single authenticated user.
type ConfluenceClient struct {
	baseURL  string // where requests go: the gateway when a cloud ID is set, else the site
	siteURL  string // always the Confluence site, for URLs a reader will see
	username string
	token    string
	http     *http.Client
}

// Config holds what a client needs to reach Confluence. The fields are named
// rather than positional because SiteURL and CloudID both address the same site
// and Token is a secret; transposing them would be easy and the failure obscure.
type Config struct {
	// SiteURL is the Confluence site, e.g. https://your-org.atlassian.net.
	SiteURL string
	// CloudID, when set, routes requests through the platform API gateway. Leave
	// it empty for site-domain requests, which is what an unscoped personal token
	// and any Data Center site need.
	CloudID string
	// Username is the account the token belongs to (basic auth).
	Username string
	// Token is the API token.
	Token string
}

// New builds a client from cfg.
func New(cfg Config) *ConfluenceClient {
	site := strings.TrimRight(cfg.SiteURL, "/")
	base := site
	if cfg.CloudID != "" {
		base = gatewayPrefix + cfg.CloudID
	}
	return &ConfluenceClient{
		baseURL:  base,
		siteURL:  site,
		username: cfg.Username,
		token:    cfg.Token,
		http:     &http.Client{},
	}
}

// BaseURL returns the base requests are built off (trailing slash trimmed). This
// is the gateway when a cloud ID is configured, so it must not be shown to a
// reader or written into page content -- use SiteURL for that.
func (c *ConfluenceClient) BaseURL() string { return c.baseURL }

// SiteURL returns the Confluence site (trailing slash trimmed), regardless of
// whether requests are routed through the gateway.
func (c *ConfluenceClient) SiteURL() string { return c.siteURL }

// PageURL builds a page's human-facing URL from its own links, falling back to
// the legacy ?pageId= form when Links.WebUI is empty (a page fetched by a route
// that doesn't populate it). Always built off SiteURL, never BaseURL: a reader
// must never see the gateway host.
func (c *ConfluenceClient) PageURL(page *Page, pageID string) string {
	if page.Links.WebUI == "" {
		return fmt.Sprintf("%s/wiki/pages/viewpage.action?pageId=%s", c.SiteURL(), pageID)
	}
	base := page.Links.Base
	if base == "" {
		base = c.SiteURL() + "/wiki"
	}
	return base + page.Links.WebUI
}

// HTTPError is returned when the API responds with a >= 400 status.
type HTTPError struct {
	StatusCode int
	Method     string
	URL        string
	Body       string
}

// Response-body markers for the auth failures whose status code alone points a
// reader at the wrong thing. Matched as substrings of the body, never inferred
// from the status: the same 401 arrives for two unrelated reasons, and a
// rejected credential shows up as 403 on v1 and *404* on v2.
//
// Measured 2026-08-21 against the live API -- see docs/confluence/api.md. If
// Atlassian rewords any of them the hint stops appearing, which is the right
// way for this to fail: the status and body are printed either way.
const (
	bodyScopeMismatch = "scope does not match"
	// The two v1 phrasings for a rejected credential. /user/current and /search
	// disagree, and neither is a substring of the other.
	bodyNoAccessV1  = "caller cannot access Confluence"
	bodyNotPermitV1 = "not permitted to use Confluence"
	// A v2 404 whose title names nothing. Every genuine v2 404 names what it
	// could not find -- "Cannot find a page with id [...]", "Content with id:
	// [...] not found", "Could not find page with id [...]" -- so the bare title
	// is the authentication failure, not a missing page.
	bodyBareNotFound = `"title":"Not Found"`

	hintIndent = "\n    " // aligns under ui.Error's "  ✗ " glyph column
)

func (e *HTTPError) Error() string {
	msg := fmt.Sprintf("%s %s: HTTP %d: %s", e.Method, e.URL, e.StatusCode, e.Body)
	if h := e.hint(); h != "" {
		return msg + hintIndent + h
	}
	return msg
}

// hint explains a failure the status code describes badly, or returns "" when
// there is nothing trustworthy to say.
//
// It is *appended* to the status and body rather than replacing them. Each case
// is a measured response shape rather than a deduction, but a shape can still
// arrive for a reason not listed here, and a reader being misdirected needs
// everything they had before the hint existed.
//
// Deliberately silent on a 403 that is not one of the credential phrasings:
// that is the shape a genuine permission denial takes, and "check your token"
// would send someone to reissue a credential that is working fine.
func (e *HTTPError) hint() string {
	switch {
	case e.StatusCode == http.StatusUnauthorized && strings.Contains(e.Body, bodyScopeMismatch):
		return "hint: the API token is valid but carries no scope for this call. Scopes are fixed " +
			"when a token is issued, so this needs a new token rather than an edit to this one -- " +
			"the list markfluence needs is in README.md."
	case e.StatusCode == http.StatusUnauthorized && !e.viaGateway() && !jsonBody(e.Body):
		return "hint: the site domain rejected this before it reached the API. A scoped " +
			"(service-account) token has to go through the platform gateway -- set " +
			"CONFLUENCE_CLOUD_ID."
	case e.RejectedCredential():
		return "hint: the credentials were rejected. Check CONFLUENCE_USERNAME and " +
			"CONFLUENCE_TOKEN -- this is what a wrong or revoked token returns, and on a v2 route " +
			"it arrives as a 404 rather than an auth status."
	}
	return ""
}

// RejectedCredential reports whether the response is the API refusing the
// credentials outright, which it does with three different statuses depending
// on the route: 403 on v1, and 404 with an unnamed target on v2.
//
// The v2 case is the one worth catching. Without it a revoked token makes every
// page read answer "page not found", and the obvious next move -- go and check
// the page id -- is wrong for every id.
func (e *HTTPError) RejectedCredential() bool {
	switch e.StatusCode {
	case http.StatusForbidden:
		return strings.Contains(e.Body, bodyNoAccessV1) || strings.Contains(e.Body, bodyNotPermitV1)
	case http.StatusNotFound:
		return strings.Contains(e.Body, bodyBareNotFound)
	}
	return false
}

// notFound reports whether err means the thing is genuinely absent, as opposed
// to the API refusing the credentials -- which v2 also answers with a 404.
//
// The distinction is the whole reason this helper exists rather than an inline
// status check. Reading a credential rejection as "absent" is what made a
// revoked token report "page not found" for every id, sending the reader off to
// check page ids that were all correct.
func notFound(err error) bool {
	var he *HTTPError
	return errors.As(err, &he) && he.StatusCode == http.StatusNotFound && !he.RejectedCredential()
}

// viaGateway reports whether the request went to the platform API gateway. The
// URL already carries the answer, so the error needs no extra field and no
// construction site has to change.
func (e *HTTPError) viaGateway() bool { return strings.HasPrefix(e.URL, gatewayPrefix) }

// jsonBody reports whether a body is the API's JSON error rather than a servlet
// container's HTML page. The distinction is the whole signal for a site-domain
// 401: reaching the API and being refused looks different from not reaching it.
func jsonBody(s string) bool { return strings.HasPrefix(strings.TrimSpace(s), "{") }

// --- API types ---------------------------------------------------------------

// Page is a Confluence page (v2). Fields not needed by the CLI are omitted.
type Page struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	SpaceID  string `json:"spaceId"`
	ParentID string `json:"parentId"`
	// ParentType is what kind of thing ParentID names: "page" or "folder" (a
	// Cloud-only content type). It is the only way to tell the two apart without
	// a second request, and a folder is a legitimate parent — see
	// docs/confluence/folders.md.
	ParentType string  `json:"parentType"`
	AuthorID   string  `json:"authorId"`
	OwnerID    string  `json:"ownerId"`
	CreatedAt  string  `json:"createdAt"`
	Version    Version `json:"version"`
	Body       Body    `json:"body"`
	Links      Links   `json:"_links"`
}

// Folder is a Confluence Cloud folder: a content type that holds pages and can
// be a page's parent, but is not itself a page. Every v2 page route answers a
// folder id with 404, which is why it needs its own lookup. Data Center has no
// folder type. See docs/confluence/folders.md.
type Folder struct {
	ID         string  `json:"id"`
	Type       string  `json:"type"`
	Title      string  `json:"title"`
	Status     string  `json:"status"`
	SpaceID    string  `json:"spaceId"`
	ParentID   string  `json:"parentId"`
	ParentType string  `json:"parentType"`
	Version    Version `json:"version"`
	Links      Links   `json:"_links"`
}

// Body holds a page's body in the representations we request. Only the format
// asked for via body-format is populated; the rest are zero.
type Body struct {
	Storage BodyRepresentation `json:"storage"`
}

// BodyRepresentation is one body representation (value plus its format name).
type BodyRepresentation struct {
	Value          string `json:"value"`
	Representation string `json:"representation"`
}

// Version is a page or property version.
type Version struct {
	Number    int    `json:"number"`
	Message   string `json:"message"`
	CreatedAt string `json:"createdAt"`
	AuthorID  string `json:"authorId"`
}

// Links holds the API-relative link fields the CLI reads.
type Links struct {
	WebUI string `json:"webui"`
	Base  string `json:"base"`
	Next  string `json:"next"`
}

// Attachment is a page attachment (v1), with the comment, version, and
// extensions expanded. Title is the name Confluence stores, which for an
// attachment markfluence published is the encoded source path (see
// convert.AttachmentFilename).
type Attachment struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Metadata struct {
		Comment string `json:"comment"`
	} `json:"metadata"`
	Version struct {
		Number int    `json:"number"`
		When   string `json:"when"`
	} `json:"version"`
	Extensions struct {
		MediaType string `json:"mediaType"`
		FileSize  int64  `json:"fileSize"`
	} `json:"extensions"`
	Links struct {
		// Download is context-relative ("/rest/api/content/{page}/child/
		// attachment/{id}/download"), not the /download/attachments/... UI path.
		// Being an API path, it also works through the gateway.
		Download string `json:"download"`
	} `json:"_links"`
}

// Property is a page content property (v2). Value is decoded as-is (page
// appearance values are strings).
type Property struct {
	ID      string  `json:"id"`
	Key     string  `json:"key"`
	Value   any     `json:"value"`
	Version Version `json:"version"`
}

// LocalAttachment is a local image to sync to a page. Source is the markdown
// image path it was written as, recorded in the attachment's comment; it may be
// empty, in which case only a checksum is recorded. It's the same shape
// internal/convert discovers images as -- see attachref.LocalAttachment.
type LocalAttachment = attachref.LocalAttachment

// AttachmentMeta is the markfluence bookkeeping parsed out of an attachment's
// comment. A hand-uploaded attachment has none, leaving Managed false.
type AttachmentMeta struct {
	SHA256  string
	Source  string
	Managed bool
}

// Meta parses this attachment's comment into markfluence's bookkeeping.
func (a Attachment) Meta() AttachmentMeta { return parseAttachmentComment(a.Metadata.Comment) }

// attachmentComment builds the comment stored on an uploaded attachment. source
// is written last and unquoted so it may contain spaces.
func attachmentComment(sum, source string) string {
	c := attachmentCommentPrefix + "sha256=" + sum
	if source != "" {
		c += " path=" + source
	}
	return c
}

// parseAttachmentComment reads the one form markfluence writes:
// "markfluence: sha256=<hex> path=<path>". Anything else -- a hand-uploaded
// attachment, or one stamped by a markfluence predating this format -- comes
// back unmanaged.
func parseAttachmentComment(comment string) AttachmentMeta {
	rest, ok := strings.CutPrefix(comment, attachmentCommentPrefix)
	if !ok {
		return AttachmentMeta{}
	}
	m := AttachmentMeta{Managed: true}
	if sum, after, found := strings.Cut(strings.TrimPrefix(rest, "sha256="), " "); found {
		m.SHA256, rest = sum, after
	} else {
		m.SHA256, rest = sum, ""
	}
	// path= is last, so its value is the remainder verbatim.
	if src, ok := strings.CutPrefix(rest, "path="); ok {
		m.Source = src
	}
	return m
}

// SyncAction reports what sync_attachments did for one file.
type SyncAction struct {
	Filename string
	Action   string // "created", "updated", or "skipped"
}

// --- request helpers ---------------------------------------------------------

// doJSON performs a request with an optional JSON body and decodes a JSON
// response into out (when non-nil and the status is < 400). It returns an
// *HTTPError for status >= 400.
func (c *ConfluenceClient) doJSON(
	method, rawURL string, params url.Values, reqBody, out any, timeout time.Duration,
) error {
	var body io.Reader
	if reqBody != nil {
		b, err := json.Marshal(reqBody)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	if len(params) > 0 {
		rawURL += "?" + params.Encode()
	}
	// http.NewRequest sets GetBody for a bytes.Reader body, so send can rebuild
	// the body on a retry.
	req, err := http.NewRequest(method, rawURL, body)
	if err != nil {
		return err
	}
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	status, respBody, err := c.send(req, timeout)
	if err != nil {
		return err
	}
	if status >= 400 {
		return &HTTPError{StatusCode: status, Method: method, URL: rawURL, Body: string(respBody)}
	}
	if out != nil && len(respBody) > 0 {
		return json.Unmarshal(respBody, out)
	}
	return nil
}

// send sets common headers and executes req, retrying transient failures with
// exponential backoff (honoring Retry-After). Each attempt runs under a fresh
// context with the given per-attempt timeout. 429 is retried for any method (the
// request was rejected before processing); 502/503/504, any other 5xx that
// carries Retry-After, and network errors are retried only for idempotent
// methods, so a non-idempotent POST is retried solely on 429. See
// retryableStatus and docs/confluence/api.md.
func (c *ConfluenceClient) send(req *http.Request, timeout time.Duration) (int, []byte, error) {
	req.SetBasicAuth(c.username, c.token)
	req.Header.Set("Accept", "application/json")

	for attempt := 0; ; attempt++ {
		res, err := c.attempt(req, timeout)
		ev := RetryEvent{
			Method: req.Method, URL: req.URL.String(), Attempt: attempt,
			Status: res.status, Err: err, RateLimit: res.rateLimit,
		}
		switch {
		case err != nil:
			ev.Retrying = attempt < maxRetries && isIdempotent(req.Method)
			if !ev.Retrying {
				logRetry(ev)
				return 0, nil, err
			}
			ev.Delay = backoff(attempt, 0)
			logRetry(ev)
			sleep(ev.Delay)
			continue
		case retryableStatus(res.status, req.Method, res.hasRetryAfter):
			ev.Retrying = attempt < maxRetries
			if !ev.Retrying { // retries exhausted
				logRetry(ev)
				return res.status, res.body, nil
			}
			ev.Delay = backoff(attempt, res.retryAfter)
			logRetry(ev)
			sleep(ev.Delay)
			continue
		default:
			// Report a failure that was eligible for a retry and did not get
			// one, so the rule that refused it is visible. A success, or an
			// ordinary 4xx the command layer will report itself, is not news.
			if res.status >= 500 || res.status == http.StatusTooManyRequests {
				logRetry(ev)
			}
			return res.status, res.body, nil
		}
	}
}

// attemptResult is what one round trip reported. It is a struct rather than a
// pile of return values because the retry decision, the delay, and what gets
// logged each need a different part of it.
type attemptResult struct {
	status int
	body   []byte
	// retryAfter is the delay the response advertised; hasRetryAfter says
	// whether it advertised one at all, which is not the same thing -- see
	// parseRetryAfter.
	retryAfter    time.Duration
	hasRetryAfter bool
	rateLimit     RateLimitInfo
}

// attempt performs a single HTTP round trip under a fresh timeout context.
func (c *ConfluenceClient) attempt(req *http.Request, timeout time.Duration) (attemptResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	r := req.Clone(ctx)
	if req.GetBody != nil { // rebuild the body so retries don't send an empty one
		body, err := req.GetBody()
		if err != nil {
			return attemptResult{}, err
		}
		r.Body = body
	}

	resp, err := c.http.Do(r)
	if err != nil {
		return attemptResult{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return attemptResult{}, err
	}
	delay, ok := parseRetryAfter(resp.Header.Get("Retry-After"))
	return attemptResult{
		status: resp.StatusCode, body: data,
		retryAfter: delay, hasRetryAfter: ok,
		rateLimit: rateLimitFrom(resp.Header),
	}, nil
}

// isIdempotent reports whether retrying a method can't cause a duplicate write.
func isIdempotent(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPut, http.MethodDelete, http.MethodOptions:
		return true
	default:
		return false
	}
}

// retryableStatus reports whether a response status warrants a retry.
//
// hasRetryAfter is what makes a 500 retryable: Atlassian's guidance is to retry
// an idempotent request when the response asks to be called back, and it draws
// no line between 500 and 503 (docs/confluence/api.md). That answers "is this
// 500 transient?" without guessing -- a bare 500 is usually a deterministic
// rejection of that particular request, so retrying only buys backoff on
// something that will not succeed.
//
// It is the header's presence that matters, not the delay: "Retry-After: 0" is
// a request to retry immediately, not the absence of one.
//
// 502/503/504 stay unconditional rather than also requiring the header.
// Conforming strictly would stop retrying a bare 502 from a proxy, which is
// transient in practice.
func retryableStatus(status int, method string, hasRetryAfter bool) bool {
	switch {
	case status == http.StatusTooManyRequests:
		return true
	case status == http.StatusBadGateway,
		status == http.StatusServiceUnavailable,
		status == http.StatusGatewayTimeout:
		return isIdempotent(method)
	case status >= 500 && hasRetryAfter:
		return isIdempotent(method)
	default:
		return false
	}
}

// backoff returns the delay before a retry: the server's Retry-After when given,
// otherwise exponential (baseBackoff * 2^attempt) with jitter, both capped at
// maxBackoff.
//
// Only the exponential path is jittered. A Retry-After is an instruction rather
// than a guess, and spreading it risks coming back before the server said to.
// The exponential path has no such authority behind it, and Atlassian asks for
// jitter explicitly: without it, every client that hit the same limit retries in
// lockstep at 1s, 2s, 4s, 8s. That is not hypothetical for markfluence -- see
// the GitHub Action, where parallel jobs share one rate limit.
func backoff(attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		if retryAfter > maxBackoff {
			return maxBackoff
		}
		return retryAfter
	}
	d := baseBackoff << attempt
	if d <= 0 { // guards a shift overflow
		return maxBackoff
	}
	// Jitter first, then cap, so the cap stays a real ceiling rather than
	// something jitter can push past.
	if d = jitter(d); d > maxBackoff {
		return maxBackoff
	}
	return d
}

// The range Atlassian suggests spreading a delay over.
const (
	jitterLow  = 0.7
	jitterHigh = 1.3
)

// jitter is the spreading primitive; a package variable for the same reason
// sleep is: the backoff tests assert exact durations, and a random factor would
// make them flap. Tests stub it to the identity and exercise jitterDelay
// directly.
var jitter = jitterDelay

// jitterDelay spreads d over [jitterLow, jitterHigh] of its nominal value.
func jitterDelay(d time.Duration) time.Duration {
	return time.Duration(float64(d) * (jitterLow + rand.Float64()*(jitterHigh-jitterLow)))
}

// parseRetryAfter parses a Retry-After header (delta-seconds or an HTTP date),
// reporting whether one was present and understood.
//
// Presence is reported separately from the delay because the two mean different
// things: "0", and a date already in the past, both parse to a zero delay and
// still mean "retry, immediately". Since a 5xx is retryable precisely when the
// server asked to be called back, collapsing those into "no header" would refuse
// to retry a response that explicitly requested one. An unparseable value is not
// an instruction, so it reports absent.
func parseRetryAfter(v string) (time.Duration, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}
	if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second, true
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d, true
		}
		return 0, true
	}
	return 0, false
}

// --- pages -------------------------------------------------------------------

// GetPage fetches a page's metadata.
func (c *ConfluenceClient) GetPage(pageID string) (*Page, error) {
	var p Page
	err := c.doJSON(http.MethodGet, c.baseURL+"/wiki/api/v2/pages/"+pageID, nil, nil, &p, timeoutRead)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// GetPageOrNil is like GetPage but returns nil (no error) on HTTP 404.
func (c *ConfluenceClient) GetPageOrNil(pageID string) (*Page, error) {
	var p Page
	err := c.doJSON(http.MethodGet, c.baseURL+"/wiki/api/v2/pages/"+pageID, nil, nil, &p, timeoutRead)
	if err != nil {
		if notFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}

// GetFolderOrNil fetches a folder's metadata, returning nil (no error) on HTTP
// 404. It exists so a caller handed an id can find out whether it names a folder
// after the page lookup comes back empty: the two live in separate route
// families and a folder 404s as a page. The response carries spaceId, so a
// parent-in-space check works on a folder exactly as it does on a page.
func (c *ConfluenceClient) GetFolderOrNil(folderID string) (*Folder, error) {
	var f Folder
	err := c.doJSON(http.MethodGet, c.baseURL+"/wiki/api/v2/folders/"+folderID, nil, nil, &f, timeoutRead)
	if err != nil {
		if notFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return &f, nil
}

// ChildNode is one row of a v1 child collection: a page or a folder directly
// under some parent. Type says which.
//
// Child listing is v1 because v2 cannot do it: every v2 page route refuses a
// folder id, so there is no way to see inside a folder, and
// /pages/{id}/children silently omits folders from a page's children — a wrong
// answer rather than a partial one (docs/confluence/folders.md).
//
// No expand is needed. A bare v1 child row already carries webui (so a URL and,
// via SpaceKeyFromWebUI, a space key), status, and extensions.position.
type ChildNode struct {
	ID     string `json:"id"`
	Type   string `json:"type"` // "page" or "folder"
	Title  string `json:"title"`
	Status string `json:"status"`
	// Position orders siblings the way Confluence displays them. Pages and
	// folders come from separate requests, so merging the two by this value is
	// the only way to reproduce the order a reader sees.
	Extensions struct {
		Position int64 `json:"position"`
	} `json:"extensions"`
	Links Links `json:"_links"`
}

// ListChildPages lists the pages directly under pageID, which may name a page or
// a folder.
func (c *ConfluenceClient) ListChildPages(id string) ([]ChildNode, error) {
	return listV1[ChildNode](c, "/wiki/rest/api/content/"+id+"/child/page", nil)
}

// ListChildFolders lists the folders directly under id.
//
// Folders nest, so this is not a leaf query: a folder may hold folders that hold
// the only pages in a subtree. Listing pages without also descending folders can
// report an empty result for a subtree that has pages in it.
func (c *ConfluenceClient) ListChildFolders(id string) ([]ChildNode, error) {
	return listV1[ChildNode](c, "/wiki/rest/api/content/"+id+"/child/folder", nil)
}

// GetPageBodyOrNil fetches a page including its storage-format body, returning
// nil (no error) on HTTP 404. The plain GetPage/GetPageOrNil stay bodyless so
// metadata-only callers don't pay to transfer the body.
func (c *ConfluenceClient) GetPageBodyOrNil(pageID string) (*Page, error) {
	var p Page
	err := c.doJSON(http.MethodGet, c.baseURL+"/wiki/api/v2/pages/"+pageID,
		url.Values{"body-format": {"storage"}}, nil, &p, timeoutRead)
	if err != nil {
		if notFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}

// ResolveSpaceID resolves a space key to its numeric space id, or "" if unknown.
func (c *ConfluenceClient) ResolveSpaceID(spaceKey string) (string, error) {
	var out struct {
		Results []struct {
			ID string `json:"id"`
		} `json:"results"`
	}
	err := c.doJSON(http.MethodGet, c.baseURL+"/wiki/api/v2/spaces",
		url.Values{"keys": {spaceKey}}, nil, &out, timeoutRead)
	if err != nil {
		return "", err
	}
	if len(out.Results) == 0 {
		return "", nil
	}
	return out.Results[0].ID, nil
}

// Page statuses accepted by the v2 pages route's status filter. The parameter
// repeats, so several may be asked for at once.
const (
	StatusCurrent  = "current"
	StatusArchived = "archived"
)

// SearchPagesByTitle returns pages whose title matches exactly, optionally
// restricted to spaceID (pass "" for no restriction) and to the given statuses
// (none means current only).
//
// The match is exact and case-insensitive -- not a prefix or substring search.
// Passing no statuses is deliberately *not* the same as sending no status
// parameter: the route's own default includes archived pages, so an omitted
// filter would quietly widen every caller (docs/confluence/search.md).
func (c *ConfluenceClient) SearchPagesByTitle(title, spaceID string, statuses ...string) ([]Page, error) {
	if len(statuses) == 0 {
		statuses = []string{StatusCurrent}
	}
	params := url.Values{"title": {title}, "status": statuses}
	if spaceID != "" {
		params.Set("space-id", spaceID)
	}
	return listV2[Page](c, "/wiki/api/v2/pages", params)
}

// CreatePage creates a page with storage-format body. Pass parentID "" for a
// top-level page.
func (c *ConfluenceClient) CreatePage(spaceID, title, body, parentID string) (*Page, error) {
	payload := map[string]any{
		"spaceId": spaceID,
		"status":  "current",
		"title":   title,
		"body":    map[string]string{"representation": "storage", "value": body},
	}
	if parentID != "" {
		payload["parentId"] = parentID
	}
	var p Page
	err := c.doJSON(http.MethodPost, c.baseURL+"/wiki/api/v2/pages", nil, payload, &p, timeoutWrite)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// UpdatePage updates a page with new storage-format body at the given version.
//
// On failure it checks whether the update landed anyway, because a versioned
// PUT is not as idempotent as its method suggests. send retries a PUT on a
// transport failure or a 502/503/504; if the first one reached Confluence and
// only its response was lost, the retry re-sends version N, which is refused
// because N now exists -- so a successful update surfaces as an error. Compare
// SetContentProperty, which has always re-read first for the same reason.
func (c *ConfluenceClient) UpdatePage(pageID, title, body string, version int, message string) (*Page, error) {
	payload := map[string]any{
		"id":      pageID,
		"status":  "current",
		"title":   title,
		"body":    map[string]string{"representation": "storage", "value": body},
		"version": map[string]any{"number": version, "message": message},
	}
	var p Page
	err := c.doJSON(http.MethodPut, c.baseURL+"/wiki/api/v2/pages/"+pageID, nil, payload, &p, timeoutWrite)
	if err == nil {
		return &p, nil
	}
	if landed := c.updateLanded(pageID, title, body, version); landed != nil {
		logRetry(RetryEvent{
			Method: http.MethodPut,
			URL:    c.baseURL + "/wiki/api/v2/pages/" + pageID,
			Status: statusOf(err),
			Err:    err,
			Note:   "the update had already been applied; reporting success",
		})
		return landed, nil
	}
	return nil, err
}

// updateLanded reports the page when a failed update turns out to have been
// applied, and nil otherwise.
//
// All three of version, title, and body must match what was sent. The version
// number alone is not proof: a concurrent human edit could have produced it,
// and claiming success over someone else's content is far worse than reporting
// a failure that actually succeeded. body.storage is the right field to compare
// precisely because it reports what was *stored* rather than what renders.
//
// Anything unexpected -- a differing body, a differing version, a re-read that
// itself fails -- returns nil, leaving the caller with the original error. If
// Confluence ever normalizes stored bytes the comparison simply stops matching
// and this stops firing, which is the safe direction.
//
// It runs after *any* error rather than after a particular conflict status,
// because what a stale-version PUT returns has not been observed. Guessing a
// status and getting it wrong would leave this silently never firing -- failing
// exactly the way it did before it existed.
func (c *ConfluenceClient) updateLanded(pageID, title, body string, version int) *Page {
	live, err := c.GetPageBodyOrNil(pageID)
	if err != nil || live == nil {
		return nil
	}
	if live.Version.Number != version || live.Title != title || live.Body.Storage.Value != body {
		return nil
	}
	return live
}

// statusOf extracts the HTTP status from an error, or 0 if it carries none.
func statusOf(err error) int {
	var he *HTTPError
	if errors.As(err, &he) {
		return he.StatusCode
	}
	return 0
}

// --- users -------------------------------------------------------------------

// GetUser returns a user's display name (best-effort: "" on any failure).
func (c *ConfluenceClient) GetUser(accountID string) string {
	if accountID == "" {
		return ""
	}
	var out struct {
		DisplayName string `json:"displayName"`
	}
	err := c.doJSON(http.MethodGet, c.baseURL+"/wiki/rest/api/user",
		url.Values{"accountId": {accountID}}, nil, &out, timeoutRead)
	if err != nil {
		return ""
	}
	return out.DisplayName
}

// --- attachments (v1) --------------------------------------------------------

// v1PageSize is the per-request page size for a v1 collection.
const v1PageSize = 250

// listV1 collects every row of a v1 collection, paging by start/limit offset.
//
// Offset, not _links.next: a v1 collection omits next when the results fit one
// page, so its absence cannot terminate the loop, and when present it is
// relative to the /wiki context rather than the v2 paths resolveNext handles
// (see docs/confluence/api.md). Termination is a short page instead.
func listV1[T any](c *ConfluenceClient, path string, params url.Values) ([]T, error) {
	var all []T
	for start := 0; ; start += v1PageSize {
		q := url.Values{"limit": {strconv.Itoa(v1PageSize)}, "start": {strconv.Itoa(start)}}
		for k, vs := range params {
			q[k] = vs
		}
		var out struct {
			Results []T `json:"results"`
		}
		if err := c.doJSON(http.MethodGet, c.baseURL+path, q, nil, &out, timeoutRead); err != nil {
			return nil, err
		}
		all = append(all, out.Results...)
		if len(out.Results) < v1PageSize {
			return all, nil
		}
	}
}

// ListAttachments lists all of a page's attachments, with the comment, version,
// and extensions expanded.
func (c *ConfluenceClient) ListAttachments(pageID string) ([]Attachment, error) {
	return listV1[Attachment](c, "/wiki/rest/api/content/"+pageID+"/child/attachment",
		url.Values{"expand": {"metadata.comment,version,extensions"}})
}

// DownloadAttachment fetches an attachment's bytes and writes them to w.
//
// The download endpoint 302s to Atlassian's media host with its own short-lived
// token in the query string. Go's default redirect policy drops the
// Authorization header on a cross-host hop, so the site credentials are never
// sent to that host -- which does not want them. Do not install a CheckRedirect
// that forwards headers: it would leak the API token to a third-party host.
//
// The body is buffered in memory, like every other response send handles, in
// exchange for its retry/backoff and typed errors.
func (c *ConfluenceClient) DownloadAttachment(att Attachment, w io.Writer) error {
	if att.Links.Download == "" {
		return fmt.Errorf("attachment %s (%s) has no download link", att.Title, att.ID)
	}
	rawURL := c.baseURL + "/wiki" + att.Links.Download
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	status, body, err := c.send(req, timeoutDownload)
	if err != nil {
		return err
	}
	if status >= 400 {
		return &HTTPError{StatusCode: status, Method: http.MethodGet, URL: rawURL, Body: string(body)}
	}
	_, err = w.Write(body)
	return err
}

// attachmentPlan is the decision for one local attachment: the action to take
// plus the parameters an upload would need. It is what planAttachments computes
// and both SyncAttachments (executes) and PlanAttachments (reports) consume.
type attachmentPlan struct {
	att         LocalAttachment
	action      string // "created", "updated", or "skipped"
	comment     string
	contentType string
	existingID  string // the current attachment id, for an "updated" upload
}

// planAttachments decides, per file, whether it would be created, updated, or
// skipped — reading the page's existing attachments and each local file's
// checksum, but performing no uploads. Preserves input order.
func (c *ConfluenceClient) planAttachments(pageID string, attachments []LocalAttachment) ([]attachmentPlan, error) {
	if len(attachments) == 0 {
		return nil, nil
	}
	existing, err := c.ListAttachments(pageID)
	if err != nil {
		return nil, err
	}
	remote := map[string]Attachment{}
	for _, a := range existing {
		remote[a.Title] = a
	}

	plans := make([]attachmentPlan, 0, len(attachments))
	for _, att := range attachments {
		sum, err := fileChecksum(att.Path)
		if err != nil {
			return nil, err
		}
		sum = sum[:checksumHexLen]
		comment := attachmentComment(sum, att.Source)
		contentType := mime.TypeByExtension(filepath.Ext(att.Filename))
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		p := attachmentPlan{att: att, comment: comment, contentType: contentType}
		cur, ok := remote[att.Filename]
		if ok {
			// Recorded even for a skip, so a forced upload can replace in place
			// rather than having to re-derive the id.
			p.existingID = cur.ID
		}
		meta := cur.Meta()
		switch {
		case !ok:
			p.action = "created"
		case meta.SHA256 != sum:
			// A stored checksum in a format markfluence no longer writes (a
			// different length, or unmanaged entirely) never equals sum, so
			// this also re-uploads once whatever predates the current format.
			p.action = "updated"
		case meta.Source != "" && meta.Source != att.Source:
			// The bytes are unchanged but the recorded path is wrong, so re-upload
			// to restamp the comment -- otherwise a path mangled in transit would
			// survive every later publish. The name is the encoding of the path, so
			// the two move together: a disagreement under the same name means the
			// stored comment does not say what we wrote. An empty Source is not a
			// disagreement -- a comment with no source recorded at all is a normal
			// case (see attachmentComment), not something to treat as mangled.
			p.action = "updated"
		default:
			p.action = "skipped"
		}
		plans = append(plans, p)
	}
	return plans, nil
}

// PlanAttachments reports what SyncAttachments would do — created/updated/skipped
// per file — without uploading anything. Used by --dry-run.
func (c *ConfluenceClient) PlanAttachments(pageID string, attachments []LocalAttachment) ([]SyncAction, error) {
	plans, err := c.planAttachments(pageID, attachments)
	if err != nil {
		return nil, err
	}
	actions := make([]SyncAction, 0, len(plans))
	for _, p := range plans {
		actions = append(actions, SyncAction{p.att.Filename, p.action})
	}
	return actions, nil
}

// SyncAttachments creates, updates, or skips attachments so the page matches the
// local files, using a SHA-256 stored in each attachment's comment to detect
// changes. Returns one action per file.
func (c *ConfluenceClient) SyncAttachments(pageID string, attachments []LocalAttachment) ([]SyncAction, error) {
	return c.syncAttachments(pageID, attachments, false)
}

// ForceUploadAttachments uploads every file regardless of its checksum,
// bumping each attachment's version. It exists for `attachment-upload --force`,
// which is how a user repairs an attachment whose stored bytes drifted while
// its recorded checksum still matches.
func (c *ConfluenceClient) ForceUploadAttachments(pageID string, attachments []LocalAttachment) (
	[]SyncAction, error,
) {
	return c.syncAttachments(pageID, attachments, true)
}

// syncAttachments executes a plan. When force is set, a file the checksum says
// is unchanged is uploaded anyway and reported as updated.
func (c *ConfluenceClient) syncAttachments(pageID string, attachments []LocalAttachment, force bool) (
	[]SyncAction, error,
) {
	plans, err := c.planAttachments(pageID, attachments)
	if err != nil {
		return nil, err
	}
	var actions []SyncAction
	for _, p := range plans {
		if force && p.action == "skipped" {
			p.action = "updated"
		}
		switch p.action {
		case "created":
			if err := c.uploadAttachment(
				c.baseURL+"/wiki/rest/api/content/"+pageID+"/child/attachment",
				p.att.Filename, p.comment, p.att.Path, p.contentType); err != nil {
				return nil, err
			}
		case "updated":
			if err := c.uploadAttachment(
				c.baseURL+"/wiki/rest/api/content/"+pageID+"/child/attachment/"+p.existingID+"/data",
				p.att.Filename, p.comment, p.att.Path, p.contentType); err != nil {
				return nil, err
			}
		}
		actions = append(actions, SyncAction{p.att.Filename, p.action})
	}
	return actions, nil
}

// writeTextField writes a multipart form field with an explicit UTF-8 charset.
// multipart.Writer.WriteField emits the part with no Content-Type, and
// Confluence's servlet stack decodes an unlabeled text part as ISO-8859-1 --
// which double-encodes every non-ASCII byte of the path recorded in an
// attachment's comment. The file part never had the problem: its name rides in
// Content-Disposition, which the server reads as UTF-8. See
// docs/confluence/attachments.md.
func writeTextField(mw *multipart.Writer, name, value string) error {
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q`, name))
	h.Set("Content-Type", "text/plain; charset=UTF-8")
	part, err := mw.CreatePart(h)
	if err != nil {
		return err
	}
	_, err = io.WriteString(part, value)
	return err
}

// uploadAttachment posts a multipart attachment upload to rawURL.
func (c *ConfluenceClient) uploadAttachment(rawURL, filename, comment, filePath, contentType string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	// _charset_ first: the servlet reads it as it parses, and it is the
	// conventional way to tell a Java stack how to decode unlabeled parts.
	if err := writeTextField(mw, "_charset_", "UTF-8"); err != nil {
		return err
	}
	if err := writeTextField(mw, "comment", comment); err != nil {
		return err
	}
	if err := writeTextField(mw, "minorEdit", "true"); err != nil {
		return err
	}
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename=%q`, filename))
	h.Set("Content-Type", contentType)
	part, err := mw.CreatePart(h)
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, f); err != nil {
		return err
	}
	if err := mw.Close(); err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, rawURL, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-Atlassian-Token", "nocheck")
	status, respBody, err := c.send(req, timeoutUpload)
	if err != nil {
		return err
	}
	if status >= 400 {
		return &HTTPError{StatusCode: status, Method: http.MethodPost, URL: rawURL, Body: string(respBody)}
	}
	return nil
}

func fileChecksum(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// --- content properties ------------------------------------------------------

// GetContentProperty returns a page's content property matching key, or nil.
func (c *ConfluenceClient) GetContentProperty(pageID, key string) (*Property, error) {
	var out struct {
		Results []Property `json:"results"`
	}
	err := c.doJSON(http.MethodGet, c.baseURL+"/wiki/api/v2/pages/"+pageID+"/properties",
		url.Values{"key": {key}}, nil, &out, timeoutRead)
	if err != nil {
		return nil, err
	}
	if len(out.Results) == 0 {
		return nil, nil
	}
	return &out.Results[0], nil
}

// resolveNext turns a paginated response's _links.next into an absolute URL
// against base, returning "" when there is no next page.
//
// next is normally a site-relative absolute path ("/wiki/api/v2/..."), so it is
// appended to base -- which is what preserves the gateway's /ex/confluence/{cloudId}
// segment. url.ResolveReference would be wrong here: an absolute-path reference
// replaces the whole path and would silently drop that prefix. The middle branch
// guards the converse, should the gateway ever echo next with the prefix already
// applied, which plain appending would double.
func resolveNext(base, next string) string {
	switch {
	case next == "":
		return ""
	case strings.HasPrefix(next, "http://"), strings.HasPrefix(next, "https://"):
		return next
	}
	if u, err := url.Parse(base); err == nil && u.Path != "" && strings.HasPrefix(next, u.Path) {
		u.Path, u.RawQuery, u.Fragment = "", "", ""
		return strings.TrimRight(u.String(), "/") + next
	}
	return base + next
}

// v2PageSize is the per-request page size for a v2 collection.
const v2PageSize = 250

// listV2 collects every row of a v2 collection, following the cursor in
// _links.next.
//
// Termination is a missing next link, which is reliable here in a way it is not
// for v1 (see listV1): a v2 collection reports next whenever more remains. The
// link is a /wiki-prefixed absolute path, the form resolveNext appends to the
// base unchanged, so the gateway's /ex/confluence/{cloudId} segment survives.
// It already carries the cursor and limit, which is why params go out only on
// the first request.
//
// This is not the helper for /wiki/rest/api/search. That endpoint ignores
// start, needs the /wiki prefix added to its next link, and reports short pages
// mid-collection -- see searchCQL and docs/confluence/search.md.
func listV2[T any](c *ConfluenceClient, path string, params url.Values) ([]T, error) {
	q := url.Values{"limit": {strconv.Itoa(v2PageSize)}}
	for k, vs := range params {
		q[k] = vs
	}
	var all []T
	rawURL := c.baseURL + path
	for rawURL != "" {
		var out struct {
			Results []T `json:"results"`
			Links   struct {
				Next string `json:"next"`
			} `json:"_links"`
		}
		if err := c.doJSON(http.MethodGet, rawURL, q, nil, &out, timeoutRead); err != nil {
			return nil, err
		}
		all = append(all, out.Results...)
		rawURL = resolveNext(c.baseURL, out.Links.Next)
		q = nil
	}
	return all, nil
}

// ListContentProperties returns all of a page's content properties, following
// pagination.
func (c *ConfluenceClient) ListContentProperties(pageID string) ([]Property, error) {
	return listV2[Property](c, "/wiki/api/v2/pages/"+pageID+"/properties", nil)
}

// SetContentProperty idempotently sets a content property, returning "set" or
// "unchanged". It creates the property if absent, updates it (version-bumped) if
// it differs, and does nothing if it already equals value. On an error it pauses
// and retries once (the retry re-reads first, so an already-applied write
// resolves to "unchanged"). This sits above send's transport retry: it recovers
// the case where the non-idempotent create POST succeeded but its response was
// lost, which the transport layer won't retry.
func (c *ConfluenceClient) SetContentProperty(pageID, key, value string) (string, error) {
	var lastErr error
	for attempt := 1; attempt <= 2; attempt++ {
		action, err := c.trySetContentProperty(pageID, key, value)
		if err == nil {
			return action, nil
		}
		lastErr = err
		if attempt == 1 {
			sleep(baseBackoff)
		}
	}
	return "", lastErr
}

func (c *ConfluenceClient) trySetContentProperty(pageID, key, value string) (string, error) {
	existing, err := c.GetContentProperty(pageID, key)
	if err != nil {
		return "", err
	}
	base := c.baseURL + "/wiki/api/v2/pages/" + pageID + "/properties"
	switch {
	case existing != nil && existing.Value == value:
		return "unchanged", nil
	case existing != nil:
		payload := map[string]any{
			"key":     key,
			"value":   value,
			"version": map[string]int{"number": existing.Version.Number + 1},
		}
		if err := c.doJSON(http.MethodPut, base+"/"+existing.ID, nil, payload, nil, timeoutWrite); err != nil {
			return "", err
		}
	default:
		payload := map[string]any{"key": key, "value": value}
		if err := c.doJSON(http.MethodPost, base, nil, payload, nil, timeoutWrite); err != nil {
			return "", err
		}
	}
	return "set", nil
}
