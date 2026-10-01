package timeclock

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	clockservice "github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type eventFeedFake struct {
	page clockservice.ClockEventPage
	err  error
	req  clockservice.ClockEventRequest
}

func (f *eventFeedFake) Pull(_ context.Context, req clockservice.ClockEventRequest) (clockservice.ClockEventPage, error) {
	f.req = req
	return f.page, f.err
}

func TestTodo_TCLOCK_012_HTTPRequiresPrincipalAndBindsScope(t *testing.T) {
	feed := &eventFeedFake{page: clockservice.ClockEventPage{Events: []clockservice.ClockEvent{{Tenant: "tenant-a", Sequence: 4}}}}
	h := NewClockEventHandler(feed)
	unauthenticated := httptest.NewRecorder()
	h.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, ClockEventsPath, nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want 401", unauthenticated.Code)
	}
	principal := testPrincipal(t)
	req := httptest.NewRequest(http.MethodGet, ClockEventsPath+"?page_size=7&worker=worker-a&cursor=opaque", nil)
	req = req.WithContext(trust.WithPrincipal(req.Context(), principal))
	got := httptest.NewRecorder()
	h.ServeHTTP(got, req)
	if got.Code != http.StatusOK || !strings.Contains(got.Body.String(), `"sequence":4`) {
		t.Fatalf("status=%d body=%s", got.Code, got.Body.String())
	}
	if feed.req.Tenant != string(principal.Tenant()) || feed.req.Organization != principal.OrganizationScopeID() || feed.req.Worker != "worker-a" || feed.req.Limit != 7 || feed.req.Cursor != "opaque" {
		t.Fatalf("request scope=%+v", feed.req)
	}
}

func TestTodo_TCLOCK_012_HTTPRejectsCursorAndFieldMask(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
		want int
	}{
		{name: "field mask", path: ClockEventsPath + "?fields=worker_id", want: http.StatusBadRequest},
		{name: "invalid page size", path: ClockEventsPath + "?page_size=1001", want: http.StatusBadRequest},
		{name: "foreign cursor", path: ClockEventsPath + "?cursor=foreign", want: http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			feed := &eventFeedFake{err: clockservice.ErrClockCursorInvalid}
			h := NewClockEventHandler(feed)
			r := httptest.NewRequest(http.MethodGet, tc.path, nil)
			r = r.WithContext(trust.WithPrincipal(r.Context(), testPrincipal(t)))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("status=%d want %d body=%s", w.Code, tc.want, w.Body.String())
			}
		})
	}
}

func TestTodo_TCLOCK_012_HTTPMapsFeedErrorsAndBodyBound(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want int
	}{
		{name: "scope", err: clockservice.ErrClockScopeDenied, want: http.StatusForbidden},
		{name: "unavailable", err: clockservice.ErrClockEventsUnavailable, want: http.StatusServiceUnavailable},
		{name: "invalid event", err: clockservice.ErrClockEventInvalid, want: http.StatusBadGateway},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := NewClockEventHandler(&eventFeedFake{err: tc.err})
			r := httptest.NewRequest(http.MethodGet, ClockEventsPath, nil)
			r = r.WithContext(trust.WithPrincipal(r.Context(), testPrincipal(t)))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.want || !strings.Contains(w.Header().Get("Content-Type"), "application/problem+json") {
				t.Fatalf("status=%d content-type=%q", w.Code, w.Header().Get("Content-Type"))
			}
		})
	}
	h := NewClockEventHandler(&eventFeedFake{})
	r := httptest.NewRequest(http.MethodGet, ClockEventsPath, strings.NewReader(strings.Repeat("x", maxClockEventsBody+1)))
	r = r.WithContext(trust.WithPrincipal(r.Context(), testPrincipal(t)))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestTodo_TCLOCK_012_HTTPMethodAndPath(t *testing.T) {
	h := NewClockEventHandler(&eventFeedFake{})
	for _, tc := range []struct {
		method string
		path   string
		want   int
	}{
		{method: http.MethodPost, path: ClockEventsPath, want: http.StatusMethodNotAllowed},
		{method: http.MethodGet, path: "/v1/other-events", want: http.StatusNotFound},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.path, nil)
			r = r.WithContext(trust.WithPrincipal(r.Context(), testPrincipal(t)))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("status=%d want %d", w.Code, tc.want)
			}
		})
	}
	if clockEventStatus(clockservice.ErrClockCursorInvalid) != http.StatusBadRequest {
		t.Fatal("cursor status mapping changed")
	}
}
