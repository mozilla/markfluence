package client

import (
	"errors"
	"net/http"
	"testing"
)

func TestMovePageRoute(t *testing.T) {
	for _, pos := range []string{MoveAppend, MoveAfter} {
		var method, path string
		c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			method, path = r.Method, r.URL.Path
			_, _ = w.Write([]byte(`{"pageId":"10"}`))
		})
		if err := c.MovePage("10", pos, "20"); err != nil {
			t.Fatalf("%s: %v", pos, err)
		}
		if want := "/wiki/rest/api/content/10/move/" + pos + "/20"; method != http.MethodPut || path != want {
			t.Errorf("%s: got %s %s, want PUT %s", pos, method, path, want)
		}
	}
}

func TestMovePageRefusesOtherPositions(t *testing.T) {
	called := false
	c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) { called = true })
	err := c.MovePage("10", "before", "20")
	if err == nil || called {
		t.Fatalf("err=%v called=%v; want a local refusal and no request", err, called)
	}
}

func TestMovePageReportsLoop(t *testing.T) {
	c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"statusCode":400,` +
			`"message":"Cannot move the content here as it creates a parent-child loop."}`))
	})
	err := c.MovePage("10", MoveAppend, "20")
	var he *HTTPError
	if !errors.As(err, &he) || he.StatusCode != http.StatusBadRequest {
		t.Fatalf("got %v, want the 400", err)
	}
}
