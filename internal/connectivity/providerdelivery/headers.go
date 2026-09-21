package providerdelivery

import (
	"context"
	"net/http"
	"strings"
)

// HeaderSource returns observability headers (for example traceparent and
// X-Correlation-Id) to add to one outbound provider request, read from the
// request context. It is optional: a nil HeaderSource adds nothing.
//
// Its headers are strictly additive. A header the client already set, and
// every credential- or protocol-bearing header (Authorization, X-Api-Key,
// Idempotency-Key, Content-Type and the like), is never overwritten or
// introduced by a HeaderSource, and a header whose name or value is not
// valid on the wire is skipped rather than allowed to fail the request.
type HeaderSource func(context.Context) http.Header

// protectedHeaders are never taken from a HeaderSource, even when the
// client did not set them on this request (for example Idempotency-Key on a
// Status GET): they carry credentials, idempotency or framing, which only
// the client itself decides.
var protectedHeaders = map[string]bool{
	"Authorization":       true,
	"Proxy-Authorization": true,
	APIKeyHeader:          true,
	"Idempotency-Key":     true,
	"Content-Type":        true,
	"Content-Length":      true,
	"Accept":              true,
	"Cookie":              true,
	"Host":                true,
	"Transfer-Encoding":   true,
	"Connection":          true,
}

// maxSourcedHeaderValue bounds one sourced header value.
const maxSourcedHeaderValue = 512

// applyHeaderSource merges src's headers into h without overwriting
// anything h already carries and without ever touching a protected header.
func applyHeaderSource(ctx context.Context, h http.Header, src HeaderSource) {
	if src == nil {
		return
	}
	extra := src(ctx)
	for name, values := range extra {
		key := http.CanonicalHeaderKey(name)
		if protectedHeaders[key] || len(h.Values(key)) > 0 || !validHeaderName(key) {
			continue
		}
		for _, v := range values {
			if validHeaderValue(v) {
				h.Add(key, v)
			}
		}
	}
}

// validHeaderName accepts an RFC 9110 token.
func validHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c <= 0x20 || c >= 0x7f || strings.IndexByte(`"(),/:;<=>?@[\]{}`, c) >= 0 {
			return false
		}
	}
	return true
}

// validHeaderValue accepts bounded, non-empty printable ASCII, so a sourced
// value can neither inject a header line nor make net/http refuse the
// request.
func validHeaderValue(v string) bool {
	if v == "" || len(v) > maxSourcedHeaderValue {
		return false
	}
	for i := 0; i < len(v); i++ {
		if c := v[i]; c < 0x20 || c > 0x7e {
			return false
		}
	}
	return true
}

// headerDoer wraps a Doer so every request also carries src's headers.
type headerDoer struct {
	next Doer
	src  HeaderSource
}

// NewHeaderDoer wraps next so every request it sends also carries the
// headers src returns for the request's context, under the same additive
// rules as a client's HeaderSource. It exists for the OAuth token fetch:
// pass it as oauthcc.Config.Client so the token request carries the
// caller's traceparent too (the token source's refresh context keeps the
// caller's values). A nil next is a fresh http.Client; a *http.Client is
// copied with redirects disabled, because the wrapper hides it from
// oauthcc's own redirect guard. A nil src returns next unchanged apart from
// that redirect guard.
func NewHeaderDoer(next Doer, src HeaderSource) Doer {
	switch c := next.(type) {
	case nil:
		next = &http.Client{CheckRedirect: noFollow}
	case *http.Client:
		cp := *c
		cp.CheckRedirect = noFollow
		next = &cp
	}
	if src == nil {
		return next
	}
	return headerDoer{next: next, src: src}
}

// Do implements Doer. The caller's request is cloned before headers are
// added, so the caller's Header map is never mutated.
func (d headerDoer) Do(req *http.Request) (*http.Response, error) {
	out := req.Clone(req.Context())
	applyHeaderSource(req.Context(), out.Header, d.src)
	return d.next.Do(out)
}
