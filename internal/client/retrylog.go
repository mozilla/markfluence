package client

import (
	"net/http"
	"time"
)

// RateLimitInfo holds the rate-limit headers Atlassian sends alongside a 429
// (and sometimes otherwise). They are reported rather than acted on: the retry
// decision needs only Retry-After, but "why is this slow" is unanswerable
// without them, and capturing a real 429's headers during ordinary use is how
// docs/confluence/api.md's transcribed entry could become a verified one without
// generating artificial load.
type RateLimitInfo struct {
	Limit     string // X-RateLimit-Limit
	Remaining string // X-RateLimit-Remaining
	Reset     string // X-RateLimit-Reset
	NearLimit string // X-RateLimit-NearLimit
	Reason    string // RateLimit-Reason
}

// Empty reports whether the response carried no rate-limit headers at all.
func (r RateLimitInfo) Empty() bool {
	return r == RateLimitInfo{}
}

func rateLimitFrom(h http.Header) RateLimitInfo {
	return RateLimitInfo{
		Limit:     h.Get("X-RateLimit-Limit"),
		Remaining: h.Get("X-RateLimit-Remaining"),
		Reset:     h.Get("X-RateLimit-Reset"),
		NearLimit: h.Get("X-RateLimit-NearLimit"),
		Reason:    h.Get("RateLimit-Reason"),
	}
}

// RetryEvent describes one failed attempt and what was decided about it.
//
// It is a struct rather than a set of arguments so fields can be added without
// touching every caller. URL is safe to log: the token travels in the
// Authorization header, never the query string.
type RetryEvent struct {
	Method string
	URL    string
	// Attempt is 0-based: 0 is the original request.
	Attempt int
	// Status is the response status, or 0 when the request never got one.
	Status int
	// Err is set only for a transport failure.
	Err error
	// Delay is how long the retry will wait; 0 when Retrying is false.
	Delay time.Duration
	// Retrying says which way the decision went. Both directions are reported:
	// "500, not retrying, no Retry-After" is the answer to a question someone
	// will have, and it is invisible otherwise.
	Retrying  bool
	RateLimit RateLimitInfo
}

// retryLogger receives every retry decision. It is package-level and set once
// from the command layer, rather than a field on Config, because ten commands
// build a client through Resolve with an identical literal: a new command that
// forgot to pass it would silently lose retry visibility, and no test would
// catch that -- retry logging is invisible until something is retrying.
//
// internal/client deliberately produces no output and imports no ui. A hook
// keeps it that way.
var retryLogger func(RetryEvent)

// SetRetryLogger installs fn as the retry reporter, replacing any previous one.
// Pass nil to silence it.
func SetRetryLogger(fn func(RetryEvent)) { retryLogger = fn }

// logRetry reports a decision, if anyone is listening.
func logRetry(ev RetryEvent) {
	if retryLogger != nil {
		retryLogger(ev)
	}
}
