---
title: markfluence test codeblock
space: ~60c36d0718e9f60071326951
parent: null
page_id: 2941386773
page_width: max
---

# Narrow Code block

```python
def hello(name):
    print(f"hello, {name}")
```


# Wide code block

```go
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
```
