package client

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// userRows builds a results array of n synthetic people, numbered from base so
// two pages hold different accounts.
func userRows(base, n int) string {
	rows := make([]string, 0, n)
	for i := base; i < base+n; i++ {
		rows = append(rows, fmt.Sprintf(
			`{"user":{"accountId":"id-%d","displayName":"Person %d","type":"known"}}`, i, i))
	}
	return "[" + strings.Join(rows, ",") + "]"
}

// userServer serves the user-search route, recording every request's query.
// page(start) returns the rows JSON for that offset.
func userServer(t *testing.T, page func(start int) string) (*ConfluenceClient, *[]string) {
	t.Helper()
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != userSearchPath {
			t.Errorf("unexpected path %q", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		queries = append(queries, r.URL.RawQuery)
		start, _ := strconv.Atoi(r.URL.Query().Get("start"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"results":%s,"start":%d,"limit":100,"totalSize":3}`, page(start), start)
	}))
	t.Cleanup(srv.Close)
	return New(Config{SiteURL: srv.URL, Username: "u", Token: "t"}), &queries
}

// The regression for the trap that shaped this whole file: the route caps a
// page at 100 rows while echoing the requested limit, so a pager that reads a
// short page as the end truncates silently. Pointed through listV1 (250) this
// returns 100 and claims it is done; it must return all 104.
func TestPageCapDoesNotTruncate(t *testing.T) {
	c, queries := userServer(t, func(start int) string {
		switch start {
		case 0:
			return userRows(0, 100)
		case 100:
			return userRows(100, 4)
		default:
			return "[]"
		}
	})

	got, more, err := c.SearchUsers("person", 0)
	if err != nil {
		t.Fatalf("SearchUsers: %v", err)
	}
	if len(got) != 104 {
		t.Fatalf("got %d matches, want 104 (the 100-row page cap truncated the walk)", len(got))
	}
	if more {
		t.Error("more = true on an unbounded walk that reached the end")
	}
	if len(*queries) != 2 {
		t.Fatalf("made %d requests, want 2: %v", len(*queries), *queries)
	}
	if !strings.Contains((*queries)[0], "limit=100") {
		t.Errorf("first request did not ask for the page size: %s", (*queries)[0])
	}
	if !strings.Contains((*queries)[1], "start=100") {
		t.Errorf("second request did not advance the offset: %s", (*queries)[1])
	}
}

// totalSize reports the rows on the current page, not the result set, so a
// pager that believes it stops one page early. The stub says 3 throughout.
func TestTotalSizeIsNotConsulted(t *testing.T) {
	c, _ := userServer(t, func(start int) string {
		if start == 0 {
			return userRows(0, 100)
		}
		return userRows(100, 2)
	})

	got, _, err := c.SearchUsers("person", 0)
	if err != nil {
		t.Fatalf("SearchUsers: %v", err)
	}
	if len(got) != 102 {
		t.Errorf("got %d matches, want 102; totalSize was read as a total", len(got))
	}
}

// A bound asks for one row more than it needs and reports the surplus as
// "more", since totalSize cannot supply a count.
func TestBoundReportsMoreWithoutCounting(t *testing.T) {
	c, queries := userServer(t, func(int) string { return userRows(0, 3) })

	got, more, err := c.SearchUsers("person", 2)
	if err != nil {
		t.Fatalf("SearchUsers: %v", err)
	}
	if len(got) != 2 || !more {
		t.Fatalf("got %d matches, more = %v; want 2 and true", len(got), more)
	}
	if !strings.Contains((*queries)[0], "limit=3") {
		t.Errorf("a bound of 2 asked for %s, want limit=3", (*queries)[0])
	}
	if len(*queries) != 1 {
		t.Errorf("made %d requests for a satisfied bound, want 1", len(*queries))
	}
}

// Exactly as many matches as the bound is not "more exist".
func TestBoundMetExactlyIsNotMore(t *testing.T) {
	c, _ := userServer(t, func(int) string { return userRows(0, 2) })

	got, more, err := c.SearchUsers("person", 2)
	if err != nil {
		t.Fatalf("SearchUsers: %v", err)
	}
	if len(got) != 2 || more {
		t.Errorf("got %d matches, more = %v; want 2 and false", len(got), more)
	}
}

// A quote in a name would otherwise end the CQL literal and turn the rest of
// the value into query syntax.
func TestQueryIsEscapedIntoTheCQLLiteral(t *testing.T) {
	c, queries := userServer(t, func(int) string { return "[]" })

	if _, _, err := c.SearchUsers(`a" or type=user`, 0); err != nil {
		t.Fatalf("SearchUsers: %v", err)
	}
	cql := queryParam(t, (*queries)[0], "cql")
	if cql != `user.fullname~"a\" or type=user"` {
		t.Errorf("cql = %q, want the quote backslash-escaped inside the literal", cql)
	}
}

// A row carrying no account id cannot be mentioned, so it is dropped -- but it
// still filled a page slot, so the walk must not read the short *kept* list as
// the end of the results.
func TestRowWithNoAccountIDIsSkippedButStillFillsThePage(t *testing.T) {
	c, _ := userServer(t, func(start int) string {
		if start == 0 {
			rows := userRows(0, 99)
			return rows[:len(rows)-1] + `,{"user":{"accountId":"","displayName":"Ghost"}}]`
		}
		return userRows(100, 1)
	})

	got, _, err := c.SearchUsers("person", 0)
	if err != nil {
		t.Fatalf("SearchUsers: %v", err)
	}
	if len(got) != 100 {
		t.Fatalf("got %d matches, want 100 (99 kept from a full page, plus 1)", len(got))
	}
	for _, m := range got {
		if m.AccountID == "" {
			t.Fatal("a match with no account id was reported")
		}
	}
}

// An unbounded walk over an empty result set is one request and no error.
func TestNoMatchesIsNotAnError(t *testing.T) {
	c, queries := userServer(t, func(int) string { return "[]" })

	got, more, err := c.SearchUsers("zzznobody", 0)
	if err != nil {
		t.Fatalf("SearchUsers: %v", err)
	}
	if len(got) != 0 || more {
		t.Errorf("got %d matches, more = %v; want 0 and false", len(got), more)
	}
	if len(*queries) != 1 {
		t.Errorf("made %d requests, want 1", len(*queries))
	}
}

func TestUserRequestSize(t *testing.T) {
	for _, tt := range []struct{ max, want int }{
		{0, userPageSize}, {-1, userPageSize}, {1, 2}, {99, 100},
		{userPageSize, userPageSize}, {500, userPageSize},
	} {
		if got := userRequestSize(tt.max); got != tt.want {
			t.Errorf("userRequestSize(%d) = %d, want %d", tt.max, got, tt.want)
		}
	}
}

// queryParam pulls one decoded parameter out of a recorded raw query.
func queryParam(t *testing.T, rawQuery, key string) string {
	t.Helper()
	vals, err := url.ParseQuery(rawQuery)
	if err != nil {
		t.Fatalf("parsing %q: %v", rawQuery, err)
	}
	if !vals.Has(key) {
		t.Fatalf("no %q in %q", key, rawQuery)
	}
	return vals.Get(key)
}

// A short page is the only end-of-results signal this route gives, so a server
// that stopped honouring start -- clamping it, or ignoring it the way
// /wiki/rest/api/search ignores it outright -- would hand back full pages
// forever. Without the page bound an unbounded walk never returns and collects
// rows until it runs out of memory.
func TestAWalkThatNeverEndsIsRefused(t *testing.T) {
	c, queries := userServer(t, func(int) string { return userRows(0, 100) })

	_, _, err := c.SearchUsers("everybody", 0)
	if err == nil {
		t.Fatal("SearchUsers = nil error against a server that never ends a page")
	}
	if !strings.Contains(err.Error(), "did not terminate") {
		t.Errorf("error = %q, want it to say the walk did not terminate", err)
	}
	// Typed as a request failure, so a caller classifying by origin blames the
	// server rather than the query.
	if !FromRequest(err) {
		t.Error("the error is not a request error, so --json would misreport its code")
	}
	if len(*queries) != maxUserPages {
		t.Errorf("made %d requests, want the bound of %d", len(*queries), maxUserPages)
	}
}
