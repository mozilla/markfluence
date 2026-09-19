package spaceinfo

import (
	"fmt"
	"strings"
	"time"
)

// jsonSpaceInfoResult is space-info's --json result shape. Keys are always
// present (the stable-schema rule); a read that failed surfaces as null.
type jsonSpaceInfoResult struct {
	OK          bool     `json:"ok"`
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	ID          string   `json:"id"`
	Type        string   `json:"type"`
	Status      string   `json:"status"`
	Description string   `json:"description"`
	Labels      []string `json:"labels"`
	// HomepageID is "" for a space with no homepage; HomepageTitle is null when
	// it could not be fetched, which is separate from having none.
	HomepageID    string  `json:"homepage_id"`
	HomepageTitle *string `json:"homepage_title"`
	// Access is null when the operations expansion did not come back, which is
	// "could not be determined" rather than "may do nothing".
	Access *jsonAccess `json:"access"`
	// PageStatuses is always an object, so a consumer can tell the three cases
	// apart without string-matching a human sentence.
	PageStatuses jsonPageStatuses `json:"page_statuses"`
	// Pages and Recent are null together when the walk failed: partial counts
	// are worse than none in a command whose output is mostly counts.
	Pages  *jsonPages  `json:"pages"`
	Recent *jsonRecent `json:"recent"`
}

// jsonAccess is what the space grants this account.
//
// CreatePages, never "write": editing an existing page is not a space grant
// and Confluence decides it per page (#168). A consumer rendering this as
// "write access" promises something the API does not.
type jsonAccess struct {
	Read        bool `json:"read"`
	CreatePages bool `json:"create_pages"`
	Admin       bool `json:"admin"`
}

// jsonPageStatuses says what a page_status: line here may say, and where the
// answer came from.
//
// Source is "space" for the space's configured list (admin only), "page" for
// what this caller may set on ProbePageID, and null when neither could be read.
// The distinction is the field's whole value: a "page" answer is not the
// space's list, because Confluence decides the list per (caller, page), so
// another page in the same space may allow more or fewer.
type jsonPageStatuses struct {
	Source      *string   `json:"source"`
	ProbePageID *string   `json:"probe_page_id"`
	Names       *[]string `json:"names"`
}

type jsonPages struct {
	Current  int `json:"current"`
	Archived int `json:"archived"`
	Roots    int `json:"roots"`
}

// jsonRecent is activity in the --since window.
//
// PagesCreated and PagesTouched count **pages, not edits**, and they overlap:
// a page created inside the window has its first version inside it too, so
// PagesCreatedAndTouched reports the intersection rather than leaving a
// consumer to add the two and get a number that means nothing.
type jsonRecent struct {
	Days                   int     `json:"days"`
	PagesCreated           int     `json:"pages_created"`
	PagesTouched           int     `json:"pages_touched"`
	PagesCreatedAndTouched int     `json:"pages_created_and_touched"`
	LastActivity           *string `json:"last_activity"`
}

func (r report) jsonResult() jsonSpaceInfoResult {
	res := jsonSpaceInfoResult{
		OK:            true,
		Key:           r.space.Key,
		Name:          r.space.Name,
		ID:            r.space.ID,
		Type:          r.space.Type,
		Status:        r.space.Status,
		Description:   r.space.Description,
		Labels:        nonNil(r.space.Labels),
		HomepageID:    r.space.HomepageID,
		HomepageTitle: strOrNil(r.space.HomepageTitle),
		PageStatuses:  r.jsonStatuses(),
	}
	if read, known := r.space.CanRead(); known {
		create, _ := r.space.CanCreatePages()
		admin, _ := r.space.IsAdmin()
		res.Access = &jsonAccess{Read: read, CreatePages: create, Admin: admin}
	}
	if r.counts != nil {
		res.Pages = &jsonPages{
			Current: r.counts.Current, Archived: r.counts.Archived, Roots: r.counts.Roots,
		}
		res.Recent = &jsonRecent{
			Days:         r.sinceDays,
			PagesCreated: r.counts.Created,
			PagesTouched: r.counts.Touched,
			// Reported even when zero: the field exists so the other two are
			// never added together, and omitting it when they do not overlap
			// would make that arithmetic look safe.
			PagesCreatedAndTouched: r.counts.CreatedTouched,
			LastActivity:           strOrNil(r.counts.LastActivity),
		}
	}
	return res
}

func (r report) jsonStatuses() jsonPageStatuses {
	if r.statusSource == sourceNone {
		return jsonPageStatuses{}
	}
	source := string(r.statusSource)
	names := nonNil(r.statuses)
	out := jsonPageStatuses{Source: &source, Names: &names}
	out.ProbePageID = strOrNil(r.statusProbe)
	return out
}

// strOrNil renders an empty string as null: a field nobody could read and a
// field that is genuinely blank are different answers.
func strOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// nonNil keeps a slice field marshalling as [] rather than null, since for
// these two an empty list is a real answer.
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// human renders the label/value block, in the order the command's three uses
// put them: what this is, what you may do, what values it wants, then size.
// An empty field is omitted rather than printed blank, matching page-info.
func (r report) human() string {
	rows := [][2]string{
		{"key", r.space.Key},
		{"name", r.space.Name},
		{"id", r.space.ID},
		{"type", r.spaceType()},
		{"homepage", r.homepage()},
		{"description", r.space.Description},
		{"labels", strings.Join(r.space.Labels, ", ")},
		{"your access", r.access()},
		{r.statusLabel(), r.statusValue()},
		{"pages", r.pages()},
		{"root pages", r.roots()},
		{"last activity", r.lastActivity()},
		{"pages created", r.created()},
		{"pages touched", r.touched()},
	}
	width := 0
	for _, row := range rows {
		if len(row[0]) > width {
			width = len(row[0])
		}
	}
	width++ // room for the ':'

	var b strings.Builder
	for _, row := range rows {
		if row[1] == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "%-*s %s", width, row[0]+":", row[1])
	}
	return b.String()
}

func (r report) spaceType() string {
	switch {
	case r.space.Type == "" && r.space.Status == "":
		return ""
	case r.space.Status == "":
		return r.space.Type
	case r.space.Type == "":
		return r.space.Status
	}
	// The status in parentheses rather than its own row: an *archived space* is
	// the context that explains every count below it, so it belongs where a
	// reader meets the space rather than further down.
	return r.space.Type + " (" + r.space.Status + ")"
}

func (r report) homepage() string {
	if r.space.HomepageID == "" {
		return ""
	}
	if r.space.HomepageTitle == "" {
		return r.space.HomepageID
	}
	return r.space.HomepageID + "  " + r.space.HomepageTitle
}

// access renders what the space grants. "create pages", never "write": see
// jsonAccess.
func (r report) access() string {
	read, known := r.space.CanRead()
	if !known {
		return "(not available: the space's permissions could not be read)"
	}
	var granted []string
	if read {
		granted = append(granted, "read")
	}
	if create, _ := r.space.CanCreatePages(); create {
		granted = append(granted, "create pages")
	}
	if admin, _ := r.space.IsAdmin(); admin {
		granted = append(granted, "administer")
	}
	if len(granted) == 0 {
		return "none"
	}
	return strings.Join(granted, ", ")
}

// statusLabel names the source in the label itself, so the value cannot be read
// as the space's list when it is not.
func (r report) statusLabel() string {
	if r.statusSource == sourcePage {
		return "page statuses (yours, on the homepage)"
	}
	return "page statuses"
}

func (r report) statusValue() string {
	switch {
	case r.statusSource == sourceNone:
		return "(not available: not a space admin, and the homepage's own list could not be read" +
			" -- markfluence page-info PAGE shows what a page you can edit allows)"
	case len(r.statuses) == 0:
		return "(none)"
	default:
		return strings.Join(r.statuses, ", ")
	}
}

func (r report) pages() string {
	if r.counts == nil {
		return "(not available: the space's pages could not be walked)"
	}
	return fmt.Sprintf("%d current, %d archived", r.counts.Current, r.counts.Archived)
}

func (r report) roots() string {
	if r.counts == nil {
		return ""
	}
	return fmt.Sprint(r.counts.Roots)
}

// lastActivity shows the date plus how long ago, because the raw timestamp
// answers "when" and the reader's question is "is this space alive".
func (r report) lastActivity() string {
	if r.counts == nil || r.counts.LastActivity == "" {
		return ""
	}
	when, err := time.Parse(time.RFC3339, r.counts.LastActivity)
	if err != nil {
		return r.counts.LastActivity
	}
	return when.Format("2006-01-02") + " (" + ago(when) + ")"
}

func ago(when time.Time) string {
	days := int(time.Since(when).Hours() / 24)
	switch {
	case days <= 0:
		return "today"
	case days == 1:
		return "1 day ago"
	default:
		return fmt.Sprintf("%d days ago", days)
	}
}

func (r report) created() string {
	if r.counts == nil {
		return ""
	}
	return fmt.Sprintf("%d %s", r.counts.Created, window(r.sinceDays))
}

// touched names the overlap inline, so the two counts are never added up.
func (r report) touched() string {
	if r.counts == nil {
		return ""
	}
	out := fmt.Sprintf("%d %s", r.counts.Touched, window(r.sinceDays))
	if r.counts.CreatedTouched > 0 {
		out += fmt.Sprintf(" (%d of them also created then)", r.counts.CreatedTouched)
	}
	return out
}

// window names the --since window as the counts actually measure it: from
// midnight UTC that many days ago. Zero is "today", not "the last day", which
// is the phrase a 24-hour window should produce and would make a zero count
// for today indistinguishable from one for yesterday-and-today.
func window(days int) string {
	switch days {
	case 0:
		return "today"
	case 1:
		return "since yesterday"
	default:
		return fmt.Sprintf("in the last %d days", days)
	}
}
