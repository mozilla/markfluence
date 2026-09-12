package info

import (
	"sort"

	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/jsonout"
	"github.com/mozilla/markfluence/internal/labels"
)

// jsonInfoResult is info's --json result shape. Keys are always present (per the
// stable-schema rule); optional fetches that failed and top-level pages surface
// as null. page_status is Confluence's page status, renamed to avoid colliding
// with the action commands' result-status concept.
type jsonInfoResult struct {
	OK         bool                 `json:"ok"`
	PageID     string               `json:"page_id"`
	Title      string               `json:"title"`
	PageStatus string               `json:"page_status"`
	Space      string               `json:"space"`
	Parent     *string              `json:"parent"`
	ParentType *string              `json:"parent_type"`
	Version    jsonVersion          `json:"version"`
	PageWidth  *jsonout.PageWidth   `json:"page_width"`
	Labels     *[]jsonout.LabelInfo `json:"labels"`
	Created    *jsonout.Stamp       `json:"created"`
	Updated    *jsonout.Stamp       `json:"updated"`
	Message    string               `json:"message"`
	URL        string               `json:"url"`
	Properties []jsonProperty       `json:"properties"`
}

type jsonVersion struct {
	Number int `json:"number"`
}

type jsonProperty struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

// jsonResult renders the report as info's JSON result.
func (r report) jsonResult() jsonInfoResult {
	res := jsonInfoResult{
		OK:         true,
		PageID:     r.id,
		Title:      r.title,
		PageStatus: r.status,
		Space:      r.space,
		Parent:     nullable(r.parentID),
		ParentType: nullable(r.parentType),
		Version:    jsonVersion{Number: r.versionNum},
		Created:    stamp(r.createdAt, r.creatorID, r.creator),
		Updated:    stamp(r.updatedAt, r.editorID, r.editor),
		Message:    r.message,
		URL:        r.url,
	}
	if r.widthKnown {
		w := r.width
		res.PageWidth = &w
	}
	// null when the fetch failed, [] when the page genuinely has none. A
	// consumer should not have to know the prefix rule to reproduce the
	// managed/unmanaged split, so managed is reported per label rather than
	// left to be derived.
	if r.labelsKnown {
		list := make([]jsonout.LabelInfo, 0, len(r.labels))
		for _, l := range r.labels {
			list = append(list, jsonout.LabelInfo{
				Name: l.Name, Prefix: l.Prefix, Managed: l.Prefix == labels.ManagedPrefix,
			})
		}
		sort.Slice(list, func(i, j int) bool {
			if list[i].Prefix != list[j].Prefix {
				return list[i].Prefix < list[j].Prefix
			}
			return list[i].Name < list[j].Name
		})
		res.Labels = &list
	}
	// properties stays null unless --properties was given and the fetch succeeded;
	// then it is a (possibly empty) sorted array.
	if r.withProps && r.propsErr == nil {
		res.Properties = propertyList(r.properties)
	}
	return res
}

// nullable maps an empty string to a JSON null, else a pointer to the value.
func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// stamp builds a Stamp, returning nil when there is no timestamp. The author is
// nil when no account id is known.
func stamp(at, accountID, name string) *jsonout.Stamp {
	if at == "" {
		return nil
	}
	s := &jsonout.Stamp{At: at}
	if accountID != "" {
		s.By = &jsonout.Author{AccountID: accountID, Name: name}
	}
	return s
}

// propertyList converts properties to sorted JSON entries. A non-nil (possibly
// empty) slice marshals as [], signalling "fetched, here they are".
func propertyList(props []client.Property) []jsonProperty {
	out := make([]jsonProperty, len(props))
	for i, p := range props {
		out[i] = jsonProperty{Key: p.Key, Value: p.Value}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}
