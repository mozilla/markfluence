# Plan: the retry policy

Settle whether HTTP 500 is retryable, and close the gaps that surfaced while
answering it. Closes #81.

## What the issue asked, and what it turned out to be

#81 asked one question — should a plain 500 join 502/503/504 in
`retryableStatus`? — and noted that [api.md](../docs/confluence/api.md) marks the
whole retry policy **Unverified**.

Reading Atlassian's published guidance answered the question and turned up two
gaps the issue did not mention. Both
[Confluence](https://developer.atlassian.com/cloud/confluence/rate-limiting/) and
[Jira platform](https://developer.atlassian.com/cloud/jira/platform/rate-limiting/)
say the same things:

- 429 **does** carry `Retry-After` ("only returned with 429 responses"), alongside
  `X-RateLimit-Limit`, `-Remaining`, `-Reset`, `-NearLimit` and `RateLimit-Reason`.
- *"Some transient 5xx responses (such as 503) may also include a `Retry-After`
  header. While these are not rate limit responses, you can handle them with
  similar retry logic."*
- *"Only retry if the API is idempotent and the response includes a `Retry-After`
  header."*
- *"Add random jitter to delays to avoid the thundering herd problem."*
- They never distinguish 500 from 503.

Much of the existing policy is already right, and the issue undersells it:
`Retry-After` is parsed in both delta-seconds and HTTP-date form **and capped at
`maxBackoff`**, `backoff` guards a shift overflow, `GetBody` rebuilds the body so
a retry cannot send an empty one, and every attempt runs under a fresh timeout
context. So the real work is narrower than "verify the retry policy", and in one
place wider: it is not a policy gap at all.

### The gap that is not about status codes

`isIdempotent` includes `PUT`, and markfluence's PUTs carry version numbers.
`UpdatePage` sends `version.number = N`. If that PUT lands and its response is
lost, `send` re-sends it, Confluence rejects the already-used version, and a
successful update is reported as a failure.

The codebase already knows this failure mode. `SetContentProperty` has a bespoke
retry-once-that-re-reads-first, whose comment says it "recovers the case where
the non-idempotent create POST succeeded but its response was lost".
`UpdatePage` has no equivalent. That asymmetry, not the 500 question, is the only
correctness bug here.

## Out of scope (deliberately)

- **Provoking a 429.** Confirming a header Atlassian publishes would mean
  deliberately tripping rate limits on a shared corporate instance, degrading it
  for everyone else on it. Transcribed with a citation instead, and api.md says
  plainly why it was not provoked.
- **Probing what a stale-version PUT returns.** The recovery is written to
  trigger on any error precisely so that nothing depends on the answer. Recorded
  as unobserved.
- **Retuning `baseBackoff` / `maxRetries`.** Atlassian suggests a 2s initial
  delay and ~4 attempts; ours are 1s and 5. Their numbers are an example, not a
  contract, and ours are within its spirit — a 2s floor doubles what a user waits
  for a single blip. Changing them alters every command's worst case with no
  evidence either way.
- **General HTTP request tracing.** The hook reports retry decisions, not every
  request. That is a bigger feature than this issue.

## Decisions locked

### A 5xx carrying `Retry-After` is retryable; a bare 500 is not

`retryableStatus` gains the header as an input:

| response | retried? |
|---|---|
| 429 | always, any method |
| 502 / 503 / 504 | idempotent methods (unchanged) |
| any other 5xx **with** `Retry-After` | idempotent methods (new — this is 500) |
| any other 5xx **without** `Retry-After` | no |

This is Atlassian's own rule, and it answers #81 without having to guess whether
a given 500 is transient: retry when the server asks to be called back. A bare
500 is usually a deterministic rejection of that particular request, so retrying
costs up to ~15s of backoff on something that will never succeed.

502/503/504 keep their current unconditional treatment rather than conforming
fully. Atlassian's stricter reading would stop retrying a bare 502 from a proxy,
which is transient in practice — a robustness regression traded for conformance.

### Jitter on the exponential delay, never on `Retry-After`

The exponential path is multiplied by a random factor in [0.7, 1.3] before the
cap. A server-supplied `Retry-After` is left exactly as given: it is an
instruction, not a guess, and spreading it risks retrying early.

This matters concretely for #29. Parallel GitHub Action jobs sharing one rate
limit currently retry in lockstep at exactly 1s, 2s, 4s, 8s.

The RNG lives behind a package var, stubbable the way `sleep` already is, so the
existing backoff assertions stay deterministic.

### Retries are reported through a hook, and the client still prints nothing

`internal/client` imports no `ui` and emits no output. That is deliberate, and a
retry storm is nonetheless invisible: the worst case on an attachment upload is
five 120s attempts plus backoff, around twelve minutes of silence.

So the client gains a nil-able package-level logger:

```go
type RateLimitInfo struct{ Limit, Remaining, Reset, NearLimit, Reason string }

type RetryEvent struct {
    Method, URL string
    Attempt     int
    Status      int           // 0 for a transport error
    Err         error
    Delay       time.Duration // 0 when not retrying
    Retrying    bool
    RateLimit   RateLimitInfo
}

func SetRetryLogger(fn func(RetryEvent))
```

A struct rather than positional arguments, so fields can be added without
breaking the signature. It carries the rate-limit headers because they turn "why
is this slow" into an answerable question, and because they are the opportunistic
capture path that could later upgrade api.md's entry to **Verified** without
generating artificial load. The URL is safe to log: the token travels in the
`Authorization` header, never the query string.

**Set once in `root.go`'s `PersistentPreRun`, beside the existing
`ui.SetDebug(debugFlag)`.** Package-level rather than a field on `client.Options`
because ten commands build their client through `client.Resolve` with an
identical literal: a new command that forgot the field would silently lose retry
visibility, and no test would catch it — retry logging is invisible until
something is retrying.

**It fires whichever way the decision went.** `500, no Retry-After, not retrying`
is as useful as `503, retrying in 2.3s`, and without it the new rule is invisible
exactly when someone is working out why a 500 was not retried — which is the
question this issue exists to answer.

### `UpdatePage` recovers a lost response, and errs toward false failure

On **any** error from the PUT, re-read with `GetPageBodyOrNil` and treat the
update as successful only when all three match what was sent:

- the page is at the version we asked for, **and**
- its title is the title we sent, **and**
- its stored body is byte-identical to the body we sent.

Version alone is not enough. If a human's concurrent edit created that version,
version-only would report "published" over content that is not ours — and a false
success is far worse than a false failure. `body.storage` is the right field to
compare precisely because it says what was *stored* rather than what renders, and
storage persists verbatim on write.

Anything else — a differing body, a differing version, a re-read that itself
fails — returns the original error. If Confluence ever normalizes stored bytes
the comparison simply stops matching and the recovery stops firing, which is the
safe direction.

Triggering on any error, rather than on a conflict-looking status, is what keeps
this independent of what a stale-version PUT actually returns. Guessing that
wrong would leave the recovery silently never firing — failing exactly the way it
does today. The cost is one extra GET on a failed update, which is already the
rare path.

A recovery is reported through the retry hook under `--debug`, not as a warning.
The run did what was asked, and warning on a successful run trains people to
ignore warnings — but "the server errored and markfluence called it published" is
precisely what you want a trace of when something looks wrong later.

### `PUT` stays retryable

With the recovery in place, removing `PUT` from `isIdempotent` would trade a
recovered rare error for more frequent hard failures on genuinely transient
blips.

## Testing

- `retryableStatus`: 500 with `Retry-After` retried for a GET; bare 500 not;
  500 with `Retry-After` **not** retried for a POST; 502/503/504 unchanged; 429
  unchanged.
- `backoff`: jitter applied to the exponential path within [0.7, 1.3], not
  applied to `Retry-After`, cap still honored. RNG stubbed.
- The hook: fires with the expected fields on both a retried and a
  not-retried failure, carries the rate-limit headers, and is safe when unset.
- `UpdatePage`: recovers when version+title+body match; does **not** recover when
  the body differs, when the version differs, or when the re-read fails; returns
  the original error in each of those.

## Docs

- `docs/confluence/api.md`: the retry section moves from **Unverified** to
  **Transcribed** with the citation, and gains the new rule, jitter, the real
  worst-case timing, the PUT/version hazard and its recovery, and why a 429 was
  not provoked.
- `CLAUDE.md`: the `internal/client` bullet gains the retry rules and the
  `UpdatePage` recovery.

## Still unverified afterwards

- Whether Confluence's 429 matches its documentation in practice.
- What a stale-version PUT actually returns.
- Whether stored body bytes are always byte-identical to what was sent.

## Commits

1. `docs(confluence): transcribe Atlassian's rate-limit guidance`
2. `feat(client): retry a 5xx that carries Retry-After`
3. `feat(client): jitter the exponential backoff`
4. `feat(client): surface retry decisions through a hook`
5. `fix(client): recover a lost UpdatePage response`
6. `docs: update the architecture notes for the retry policy`

## What changed while implementing

Written before the code, and two decisions above turned out to be wrong as
specified. Recorded here rather than edited away, since the mistakes are the
useful part.

**"A 5xx *with* `Retry-After`" was specified as a positive delay, which is not
the same thing.** The first implementation gated on `retryAfter > 0`, and the
existing suite failed immediately: it sends `Retry-After: 0` to keep tests from
sleeping. But `0` — and an HTTP date already in the past — both parse to a zero
delay and both mean *retry, immediately*, which is still the server asking to be
called back. Reading them as "no header" would refuse to retry a response that
had explicitly requested one. `parseRetryAfter` now returns presence alongside
the delay, and `retryableStatus` takes the bool. The rule was right; the signal
chosen to express it was not.

**The `UpdatePage` recovery needed a field the hook did not have.** The plan said
a recovery is "reported through the retry hook", but a recovery is not a retry
decision — none of `Attempt`, `Delay` or `Retrying` mean anything for it.
`RetryEvent` gained a `Note` field carrying the recovery, and the renderer
short-circuits on it.

Neither changed the design's shape, and both are visible in the commits, but the
plan as written would mislead a reader into thinking the delay was the signal.
