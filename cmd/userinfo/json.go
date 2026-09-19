package userinfo

import (
	"fmt"
	"strings"
)

// jsonUserInfoResult is user-info's --json result shape. Keys are always
// present (the stable-schema rule); a read that failed or an answer of "none"
// surfaces as null, and the two are kept apart where they differ.
type jsonUserInfoResult struct {
	OK          bool   `json:"ok"`
	Self        bool   `json:"self"`
	AccountID   string `json:"account_id"`
	DisplayName string `json:"display_name"`
	PublicName  string `json:"public_name"`
	Email       string `json:"email"`
	// AccountType is "atlassian" for a person and "app" for a service account,
	// which is most of why permissions surprise people.
	AccountType   string `json:"account_type"`
	AccountStatus string `json:"account_status"`
	External      bool   `json:"external_collaborator"`
	Guest         bool   `json:"guest"`
	// PersonalSpace is null when the account has none -- a real answer, not a
	// failure: an app account has no personal space. Its key cannot be built
	// from the account id, since personal spaces keyed by email and by id are
	// both live on one instance.
	PersonalSpace *jsonPersonalSpace `json:"personal_space"`
	// Spaces is null without --spaces, and also when the survey failed.
	Spaces *jsonSpaces `json:"spaces"`
}

type jsonPersonalSpace struct {
	ID   string `json:"id"`
	Key  string `json:"key"`
	Name string `json:"name"`
	// URL is the space in a browser, taken from the response's own link rather
	// than built from the key -- an email-keyed personal space would need an
	// escaping rule this has no business inventing. "" when no link came back.
	URL string `json:"url"`
}

// jsonSpaces is the --spaces survey.
//
// write lists the spaces the account may **create pages in**. That is a space
// grant; permission to edit an existing page is not in it and Confluence
// decides that per page, so a consumer must not render this as "can edit
// anything here".
type jsonSpaces struct {
	Visible int      `json:"visible"`
	Write   []string `json:"write"`
	Admin   []string `json:"admin"`
}

func (r report) jsonResult() jsonUserInfoResult {
	res := jsonUserInfoResult{
		OK:            true,
		Self:          r.self,
		AccountID:     r.user.AccountID,
		DisplayName:   r.user.DisplayName,
		PublicName:    r.user.PublicName,
		Email:         r.user.Email,
		AccountType:   r.user.AccountType,
		AccountStatus: r.user.AccountStatus,
		External:      r.user.IsExternalCollaborator,
		Guest:         r.user.IsGuest,
	}
	if ps := r.user.PersonalSpace; ps != nil {
		res.PersonalSpace = &jsonPersonalSpace{ID: ps.ID, Key: ps.Key, Name: ps.Name, URL: ps.URL}
	}
	if r.spaces != nil {
		res.Spaces = &jsonSpaces{
			Visible: r.spaces.Visible,
			Write:   nonNil(r.spaces.Write),
			Admin:   nonNil(r.spaces.Admin),
		}
	}
	return res
}

// nonNil keeps an empty survey marshalling as [] rather than null: "may create
// pages nowhere" is a real answer, and null is reserved for "not surveyed".
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// human renders the label/value block, omitting an empty field -- page-info's
// rule, and the same shape.
func (r report) human() string {
	rows := [][2]string{
		{"account id", r.user.AccountID},
		{"name", r.user.DisplayName},
		{"email", r.user.Email},
		{"type", r.accountType()},
		{"status", r.user.AccountStatus},
		{"external", yesNo(r.user.IsExternalCollaborator)},
		{"guest", yesNo(r.user.IsGuest)},
		{"personal space", r.personalSpace()},
		{"visible spaces", r.visible()},
		{"write access", r.writeAccess()},
		{"admin access", r.adminAccess()},
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

// accountType spells out what "app" means rather than leaving a reader to
// guess: it is the field that explains most permission surprises.
func (r report) accountType() string {
	switch r.user.AccountType {
	case "":
		return ""
	case "app":
		return "app (a service account)"
	case "atlassian":
		return "atlassian (a person)"
	default:
		return r.user.AccountType
	}
}

// yesNo renders a flag. "no" rather than "" so the row is printed: for
// external and guest, a negative is information.
func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// personalSpace shows the key and its URL, or says the account has none.
//
// The URL rather than the name: the name is almost always the person's own
// display name, already on the line above, where the URL is the thing somebody
// reading this actually wants to click. Never built from the account id --
// personal space keys come in two live formats.
func (r report) personalSpace() string {
	ps := r.user.PersonalSpace
	if ps == nil {
		return "(none)"
	}
	if ps.URL == "" {
		return ps.Key
	}
	return ps.Key + "  " + ps.URL
}

func (r report) visible() string {
	if r.spaces == nil {
		return ""
	}
	return fmt.Sprint(r.spaces.Visible)
}

func (r report) writeAccess() string {
	if r.spaces == nil {
		return ""
	}
	return countAndKeys(r.spaces.Write)
}

func (r report) adminAccess() string {
	if r.spaces == nil {
		return ""
	}
	return countAndKeys(r.spaces.Admin)
}

// countAndKeys renders "N -- KEY, KEY", naming the spaces while the list is
// short enough to read and falling back to the count when it is not. An
// operator asking where they can publish wants the keys; one with 102 of them
// wants the number and a --json call.
//
// The long form says *why* it is not listing them. "(--json lists them)" alone
// read as though --json were the only place the keys existed, rather than as a
// line that ran out of room.
//
// The total is **not** repeated here. It is on its own "visible spaces" row
// above, and printing "N of M spaces" on both this line and the admin one put
// the same number on screen three times.
func countAndKeys(keys []string) string {
	if len(keys) == 0 {
		return "none"
	}
	const nameLimit = 12
	if len(keys) <= nameLimit {
		return fmt.Sprintf("%d -- %s", len(keys), strings.Join(keys, ", "))
	}
	return fmt.Sprintf("%d (too many to list; see --json output)", len(keys))
}
