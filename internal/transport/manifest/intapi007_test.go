package manifest

import (
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestTodo_INTAPI_007(t *testing.T) {
	m, err := Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	routes := PublicHTTPRoutes(m)
	if len(routes) == 0 {
		t.Fatal("public manifest has no resource aliases")
	}
	for _, route := range routes {
		if route.Procedure == "" || route.ResourcePath == "" || route.Method == "" {
			t.Fatalf("incomplete public route: %+v", route)
		}
	}
	if got := PublicHTTPRoutes(nil); got != nil {
		t.Fatalf("nil manifest routes = %v, want nil", got)
	}
}

func TestTodo_INTAPI_007_Integration(t *testing.T) {
	published := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p := DeprecationPolicy{
		DeprecatedAt: published.Add(90 * 24 * time.Hour), SunsetAt: published.Add(180 * 24 * time.Hour),
		NoticePublishedAt: published, NoticePeriod: 90 * 24 * time.Hour, NoticeURL: "https://example.test/api/notices/1",
	}
	h, err := p.Headers(published.Add(91 * 24 * time.Hour))
	if err != nil {
		t.Fatalf("Headers: %v", err)
	}
	if h.Get("Deprecation") == "" || h.Get("Sunset") == "" {
		t.Fatalf("deprecation headers = %v", h)
	}
	if p.RemovalAllowed(p.SunsetAt.Add(-time.Second)) {
		t.Fatal("route became removable before sunset")
	}
	if !p.RemovalAllowed(p.SunsetAt) {
		t.Fatal("route remained non-removable at sunset")
	}
}

func TestTodo_INTAPI_007_Golden(t *testing.T) {
	bad := DeprecationPolicy{DeprecatedAt: time.Now(), SunsetAt: time.Now().Add(time.Hour)}
	if !errors.Is(bad.Validate(), ErrDeprecationPolicyInvalid) {
		t.Fatalf("invalid policy error = %v", bad.Validate())
	}
	if _, err := bad.Headers(time.Now()); !errors.Is(err, ErrDeprecationPolicyInvalid) {
		t.Fatalf("invalid policy headers error = %v", err)
	}
	if http.TimeFormat == "" {
		t.Fatal("net/http time format is empty")
	}
}
