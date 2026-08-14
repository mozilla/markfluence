package client

import (
	"net/http"
	"testing"
	"time"
)

// captureRetries installs a recorder for the duration of a test and restores
// whatever was there before.
func captureRetries(t *testing.T) *[]RetryEvent {
	t.Helper()
	var got []RetryEvent
	prev := retryLogger
	SetRetryLogger(func(ev RetryEvent) { got = append(got, ev) })
	t.Cleanup(func() { SetRetryLogger(prev) })
	return &got
}

func TestRetryLoggerReportsARetry(t *testing.T) {
	events := captureRetries(t)
	c, _ := countingServer(t, func(w http.ResponseWriter, req int32) {
		if req == 1 {
			w.Header().Set("Retry-After", "2")
			w.Header().Set("X-RateLimit-Limit", "1000")
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", "2026-08-14T12:00:00Z")
			w.Header().Set("RateLimit-Reason", "user")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"id":"1"}`))
	})
	if _, err := c.GetPage("1"); err != nil {
		t.Fatalf("GetPage: %v", err)
	}

	if len(*events) != 1 {
		t.Fatalf("got %d events, want 1: %+v", len(*events), *events)
	}
	ev := (*events)[0]
	if !ev.Retrying {
		t.Error("Retrying = false, want true")
	}
	if ev.Status != http.StatusTooManyRequests {
		t.Errorf("Status = %d, want 429", ev.Status)
	}
	if ev.Attempt != 0 {
		t.Errorf("Attempt = %d, want 0 (the original request)", ev.Attempt)
	}
	if ev.Delay != 2*time.Second {
		t.Errorf("Delay = %v, want the advertised 2s", ev.Delay)
	}
	if ev.Method != http.MethodGet {
		t.Errorf("Method = %q, want GET", ev.Method)
	}
	// The rate-limit headers are the reason the event carries a struct: they are
	// what makes a 429 diagnosable after the fact.
	if ev.RateLimit.Limit != "1000" || ev.RateLimit.Remaining != "0" || ev.RateLimit.Reason != "user" {
		t.Errorf("RateLimit = %+v, want the headers captured", ev.RateLimit)
	}
}

// TestRetryLoggerReportsANonRetry is the half that is easy to leave out: the
// new 500 rule is invisible unless a refusal is reported too.
func TestRetryLoggerReportsANonRetry(t *testing.T) {
	events := captureRetries(t)
	c, _ := countingServer(t, func(w http.ResponseWriter, _ int32) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	if _, err := c.GetPage("1"); err == nil {
		t.Fatal("GetPage: want an error")
	}

	if len(*events) != 1 {
		t.Fatalf("got %d events, want 1: %+v", len(*events), *events)
	}
	ev := (*events)[0]
	if ev.Retrying {
		t.Error("Retrying = true, want false for a bare 500")
	}
	if ev.Delay != 0 {
		t.Errorf("Delay = %v, want 0 when not retrying", ev.Delay)
	}
	if ev.RateLimit.Empty() != true {
		t.Errorf("RateLimit = %+v, want empty", ev.RateLimit)
	}
}

// TestRetryLoggerQuietOnSuccessAndOn4xx: an ordinary 404 is the command layer's
// to report, and reporting it here would turn --debug into a request log.
func TestRetryLoggerQuietOnSuccessAndOn4xx(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"success", 200, `{"id":"1"}`},
		{"not found", 404, `{}`},
		{"forbidden", 403, `{}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			events := captureRetries(t)
			c, _ := countingServer(t, func(w http.ResponseWriter, _ int32) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			_, _ = c.GetPage("1")
			if len(*events) != 0 {
				t.Errorf("got %d events, want none: %+v", len(*events), *events)
			}
		})
	}
}

// TestRetryLoggerReportsExhaustion: the last failure gets an event too, marked
// as not retrying, so a run does not end mid-sentence.
func TestRetryLoggerReportsExhaustion(t *testing.T) {
	events := captureRetries(t)
	c, _ := countingServer(t, func(w http.ResponseWriter, _ int32) {
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	if _, err := c.GetPage("1"); err == nil {
		t.Fatal("GetPage: want an error after exhausting retries")
	}
	if len(*events) != maxRetries+1 {
		t.Fatalf("got %d events, want %d (one per attempt)", len(*events), maxRetries+1)
	}
	last := (*events)[len(*events)-1]
	if last.Retrying {
		t.Error("the final event says Retrying, want false")
	}
	for i, ev := range (*events)[:maxRetries] {
		if !ev.Retrying {
			t.Errorf("event %d says not retrying, want true", i)
		}
	}
}

func TestRetryLoggerReportsATransportFailure(t *testing.T) {
	events := captureRetries(t)
	// A server that is not there: the request fails before any status.
	c := New(Config{SiteURL: "http://127.0.0.1:1", Username: "u", Token: "t"})
	if _, err := c.GetPage("1"); err == nil {
		t.Fatal("GetPage: want a transport error")
	}
	if len(*events) == 0 {
		t.Fatal("got no events, want one per attempt")
	}
	ev := (*events)[0]
	if ev.Err == nil {
		t.Error("Err = nil, want the transport failure")
	}
	if ev.Status != 0 {
		t.Errorf("Status = %d, want 0 when there was no response", ev.Status)
	}
}

// TestSetRetryLoggerNilIsSafe: the logger is unset in every command's tests and
// in library use.
func TestSetRetryLoggerNilIsSafe(t *testing.T) {
	prev := retryLogger
	SetRetryLogger(nil)
	t.Cleanup(func() { SetRetryLogger(prev) })

	c, _ := countingServer(t, func(w http.ResponseWriter, _ int32) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	if _, err := c.GetPage("1"); err == nil {
		t.Fatal("GetPage: want an error")
	}
}
