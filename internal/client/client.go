// Package client is an HTTP client for the Confluence REST API. It wraps
// net/http with basic auth and the handful of calls markfluence needs.
//
// Requests are built as absolute URLs off the base URL. Pages and content
// properties use the Confluence v2 API; attachment writes and the user lookup
// use v1 (/wiki/rest/api/...) since v2 doesn't cover them.
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
	"strings"
	"time"
)

// attachmentChecksumPrefix is stored in an attachment's comment so a later run
// can tell whether the local file changed.
const attachmentChecksumPrefix = "mzcld:checksum: "

const (
	timeoutRead   = 30 * time.Second
	timeoutWrite  = 60 * time.Second
	timeoutUpload = 120 * time.Second
)

// retrySleep is the pause before retrying a content-property write. It is a
// package variable so tests can shorten it.
var retrySleep = time.Second

// ConfluenceClient talks to the Confluence REST API as a single authenticated user.
type ConfluenceClient struct {
	baseURL  string
	username string
	token    string
	http     *http.Client
}

// New builds a client for baseURL authenticating as username:token.
func New(baseURL, username, token string) *ConfluenceClient {
	return &ConfluenceClient{
		baseURL:  strings.TrimRight(baseURL, "/"),
		username: username,
		token:    token,
		http:     &http.Client{},
	}
}

// BaseURL returns the client's base URL (trailing slash trimmed).
func (c *ConfluenceClient) BaseURL() string { return c.baseURL }

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

// Attachment is a page attachment (v1), with the checksum comment expanded.
type Attachment struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Metadata struct {
		Comment string `json:"comment"`
	} `json:"metadata"`
}

// Property is a page content property (v2). Value is decoded as-is (page
// appearance values are strings).
type Property struct {
	ID      string  `json:"id"`
	Key     string  `json:"key"`
	Value   any     `json:"value"`
	Version Version `json:"version"`
}

// LocalAttachment is a local image to sync to a page.
type LocalAttachment struct {
	Path     string
	Filename string
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
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

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
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return err
	}
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	status, respBody, err := c.send(req)
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

// send sets common headers, executes the request, and returns the status and body.
func (c *ConfluenceClient) send(req *http.Request) (int, []byte, error) {
	req.SetBasicAuth(c.username, c.token)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	return resp.StatusCode, data, err
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

// ListAttachments lists a page's attachments with the checksum comment expanded.
func (c *ConfluenceClient) ListAttachments(pageID string) ([]Attachment, error) {
	var out struct {
		Results []Attachment `json:"results"`
	}
	err := c.doJSON(http.MethodGet,
		c.baseURL+"/wiki/rest/api/content/"+pageID+"/child/attachment",
		url.Values{"expand": {"metadata.comment"}, "limit": {"250"}}, nil, &out, timeoutRead)
	if err != nil {
		return nil, err
	}
	return out.Results, nil
}

// SyncAttachments creates, updates, or skips attachments so the page matches the
// local files, using a SHA-256 stored in each attachment's comment to detect
// changes. Returns one action per file.
func (c *ConfluenceClient) SyncAttachments(pageID string, attachments []LocalAttachment) ([]SyncAction, error) {
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

	var actions []SyncAction
	for _, att := range attachments {
		sum, err := fileChecksum(att.Path)
		if err != nil {
			return nil, err
		}
		comment := attachmentChecksumPrefix + sum
		contentType := mime.TypeByExtension(filepath.Ext(att.Filename))
		if contentType == "" {
			contentType = "application/octet-stream"
		}

		cur, ok := remote[att.Filename]
		switch {
		case !ok:
			if err := c.uploadAttachment(
				c.baseURL+"/wiki/rest/api/content/"+pageID+"/child/attachment",
				att.Filename, comment, att.Path, contentType); err != nil {
				return nil, err
			}
			actions = append(actions, SyncAction{att.Filename, "created"})
		case cur.Metadata.Comment == comment:
			actions = append(actions, SyncAction{att.Filename, "skipped"})
		default:
			if err := c.uploadAttachment(
				c.baseURL+"/wiki/rest/api/content/"+pageID+"/child/attachment/"+cur.ID+"/data",
				att.Filename, comment, att.Path, contentType); err != nil {
				return nil, err
			}
			actions = append(actions, SyncAction{att.Filename, "updated"})
		}
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

	ctx, cancel := context.WithTimeout(context.Background(), timeoutUpload)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-Atlassian-Token", "nocheck")
	status, respBody, err := c.send(req)
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
		if out.Links.Next != "" {
			rawURL = c.baseURL + out.Links.Next
		} else {
			rawURL = ""
		}
		params = nil
	}
	return results, nil
}

// SetContentProperty idempotently sets a content property, returning "set" or
// "unchanged". It creates the property if absent, updates it (version-bumped) if
// it differs, and does nothing if it already equals value. On an error it pauses
// and retries once (the retry re-reads first, so an already-applied write
// resolves to "unchanged").
func (c *ConfluenceClient) SetContentProperty(pageID, key, value string) (string, error) {
	var lastErr error
	for attempt := 1; attempt <= 2; attempt++ {
		action, err := c.trySetContentProperty(pageID, key, value)
		if err == nil {
			return action, nil
		}
		lastErr = err
		if attempt == 1 {
			time.Sleep(retrySleep)
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
