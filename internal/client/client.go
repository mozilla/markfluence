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
)

// An uploaded attachment carries markfluence bookkeeping in its comment: the
// checksum a later run compares to tell whether the local file changed, and the
// markdown image path it was published from, so reading the page back recovers
// the image's original location exactly instead of inferring it from the
// attachment name.
const (
	// attachmentCommentPrefix marks an attachment as markfluence-managed.
	attachmentCommentPrefix = "markfluence: "
	// legacyChecksumPrefix is the older checksum-only comment form. It is still
	// parsed -- comparing the parsed checksum rather than the whole comment is
	// what lets the format change without re-uploading every attachment.
	legacyChecksumPrefix = "mzcld:checksum: "
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

// HTTPError is returned when the API responds with a >= 400 status.
type HTTPError struct {
	StatusCode int
	Method     string
	URL        string
	Body       string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("%s %s: HTTP %d: %s", e.Method, e.URL, e.StatusCode, e.Body)
}

// --- API types ---------------------------------------------------------------

// Page is a Confluence page (v2). Fields not needed by the CLI are omitted.
type Page struct {
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	Status    string  `json:"status"`
	SpaceID   string  `json:"spaceId"`
	ParentID  string  `json:"parentId"`
	AuthorID  string  `json:"authorId"`
	OwnerID   string  `json:"ownerId"`
	CreatedAt string  `json:"createdAt"`
	Version   Version `json:"version"`
	Body      Body    `json:"body"`
	Links     Links   `json:"_links"`
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
// empty, in which case only a checksum is recorded.
type LocalAttachment struct {
	Path     string
	Filename string
	Source   string
}

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

// parseAttachmentComment reads both the current form ("markfluence: sha256=<hex>
// path=<path>") and the legacy checksum-only form, so an attachment written by
// an older markfluence is still recognized as unchanged.
func parseAttachmentComment(comment string) AttachmentMeta {
	if sum, ok := strings.CutPrefix(comment, legacyChecksumPrefix); ok {
		return AttachmentMeta{SHA256: strings.TrimSpace(sum), Managed: true}
	}
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
// request was rejected before processing); 502/503/504 and network errors are
// retried only for idempotent methods, so a non-idempotent POST is retried solely
// on 429.
func (c *ConfluenceClient) send(req *http.Request, timeout time.Duration) (int, []byte, error) {
	req.SetBasicAuth(c.username, c.token)
	req.Header.Set("Accept", "application/json")

	for attempt := 0; ; attempt++ {
		status, body, retryAfter, err := c.attempt(req, timeout)
		switch {
		case err != nil:
			if attempt < maxRetries && isIdempotent(req.Method) {
				sleep(backoff(attempt, 0))
				continue
			}
			return 0, nil, err
		case attempt < maxRetries && retryableStatus(status, req.Method):
			sleep(backoff(attempt, retryAfter))
			continue
		default:
			return status, body, nil
		}
	}
}

// attempt performs a single HTTP round trip under a fresh timeout context,
// returning the status, body, and any Retry-After delay the response advertised.
func (c *ConfluenceClient) attempt(req *http.Request, timeout time.Duration) (int, []byte, time.Duration, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	r := req.Clone(ctx)
	if req.GetBody != nil { // rebuild the body so retries don't send an empty one
		body, err := req.GetBody()
		if err != nil {
			return 0, nil, 0, err
		}
		r.Body = body
	}

	resp, err := c.http.Do(r)
	if err != nil {
		return 0, nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, 0, err
	}
	return resp.StatusCode, data, parseRetryAfter(resp.Header.Get("Retry-After")), nil
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
func retryableStatus(status int, method string) bool {
	switch status {
	case http.StatusTooManyRequests:
		return true
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return isIdempotent(method)
	default:
		return false
	}
}

// backoff returns the delay before a retry: the server's Retry-After when given,
// otherwise exponential (baseBackoff * 2^attempt), both capped at maxBackoff.
func backoff(attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		if retryAfter > maxBackoff {
			return maxBackoff
		}
		return retryAfter
	}
	d := baseBackoff << attempt
	if d <= 0 || d > maxBackoff { // d <= 0 guards a shift overflow
		return maxBackoff
	}
	return d
}

// parseRetryAfter parses a Retry-After header (delta-seconds or an HTTP date),
// returning 0 when absent or unparseable.
func parseRetryAfter(v string) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
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
		var he *HTTPError
		if errors.As(err, &he) && he.StatusCode == http.StatusNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}

// GetPageBodyOrNil fetches a page including its storage-format body, returning
// nil (no error) on HTTP 404. The plain GetPage/GetPageOrNil stay bodyless so
// metadata-only callers don't pay to transfer the body.
func (c *ConfluenceClient) GetPageBodyOrNil(pageID string) (*Page, error) {
	var p Page
	err := c.doJSON(http.MethodGet, c.baseURL+"/wiki/api/v2/pages/"+pageID,
		url.Values{"body-format": {"storage"}}, nil, &p, timeoutRead)
	if err != nil {
		var he *HTTPError
		if errors.As(err, &he) && he.StatusCode == http.StatusNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}

// PageExists reports whether a page with this id currently exists.
func (c *ConfluenceClient) PageExists(pageID string) (bool, error) {
	p, err := c.GetPageOrNil(pageID)
	return p != nil, err
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

// SearchPagesByTitle returns current pages matching title exactly, optionally
// restricted to spaceID (pass "" for no restriction).
func (c *ConfluenceClient) SearchPagesByTitle(title, spaceID string) ([]Page, error) {
	params := url.Values{"title": {title}, "status": {"current"}}
	if spaceID != "" {
		params.Set("space-id", spaceID)
	}
	var out struct {
		Results []Page `json:"results"`
	}
	err := c.doJSON(http.MethodGet, c.baseURL+"/wiki/api/v2/pages", params, nil, &out, timeoutRead)
	if err != nil {
		return nil, err
	}
	return out.Results, nil
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
	if err != nil {
		return nil, err
	}
	return &p, nil
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

// attachmentPageSize is the per-request page size for ListAttachments.
const attachmentPageSize = 250

// ListAttachments lists all of a page's attachments, with the comment, version,
// and extensions expanded.
//
// Pagination is by start/limit offset rather than _links.next: a v1 collection
// omits next when the results fit one page, and its next is relative to the
// /wiki context rather than the v2 paths resolveNext is written for.
func (c *ConfluenceClient) ListAttachments(pageID string) ([]Attachment, error) {
	var all []Attachment
	for start := 0; ; start += attachmentPageSize {
		var out struct {
			Results []Attachment `json:"results"`
		}
		err := c.doJSON(http.MethodGet,
			c.baseURL+"/wiki/rest/api/content/"+pageID+"/child/attachment",
			url.Values{
				"expand": {"metadata.comment,version,extensions"},
				"limit":  {strconv.Itoa(attachmentPageSize)},
				"start":  {strconv.Itoa(start)},
			}, nil, &out, timeoutRead)
		if err != nil {
			return nil, err
		}
		all = append(all, out.Results...)
		if len(out.Results) < attachmentPageSize {
			return all, nil
		}
	}
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
		comment := attachmentComment(sum, att.Source)
		contentType := mime.TypeByExtension(filepath.Ext(att.Filename))
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		p := attachmentPlan{att: att, comment: comment, contentType: contentType}
		cur, ok := remote[att.Filename]
		switch {
		case !ok:
			p.action = "created"
		case cur.Meta().SHA256 == sum:
			// Compare the checksum, not the whole comment: an attachment stamped
			// by an older markfluence is unchanged and must not be re-uploaded
			// merely because the comment format has moved on.
			p.action = "skipped"
		default:
			p.action = "updated"
			p.existingID = cur.ID
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
	plans, err := c.planAttachments(pageID, attachments)
	if err != nil {
		return nil, err
	}
	var actions []SyncAction
	for _, p := range plans {
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

// uploadAttachment posts a multipart attachment upload to rawURL.
func (c *ConfluenceClient) uploadAttachment(rawURL, filename, comment, filePath, contentType string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("comment", comment); err != nil {
		return err
	}
	if err := mw.WriteField("minorEdit", "true"); err != nil {
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

// ListContentProperties returns all of a page's content properties, following
// pagination.
func (c *ConfluenceClient) ListContentProperties(pageID string) ([]Property, error) {
	var results []Property
	rawURL := c.baseURL + "/wiki/api/v2/pages/" + pageID + "/properties"
	params := url.Values{"limit": {"250"}}
	for rawURL != "" {
		var out struct {
			Results []Property `json:"results"`
			Links   struct {
				Next string `json:"next"`
			} `json:"_links"`
		}
		if err := c.doJSON(http.MethodGet, rawURL, params, nil, &out, timeoutRead); err != nil {
			return nil, err
		}
		results = append(results, out.Results...)
		// The next link already carries the cursor and limit as query params.
		rawURL = resolveNext(c.baseURL, out.Links.Next)
		params = nil
	}
	return results, nil
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
