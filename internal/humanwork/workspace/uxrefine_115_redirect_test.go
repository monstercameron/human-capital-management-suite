package workspace

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestUXRefine115_LegacyHistoryRedirectPreservesQuery(t *testing.T) {
	h, token := newShellHandler(t, false)
	request := httptest.NewRequest(http.MethodGet, "http://cell.test"+PathProductPrefix+"history?status=completed&query=workflow+history&sort=updated%26desc", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("legacy history status = %d, want 303: %s", recorder.Code, recorder.Body.String())
	}
	location, err := url.Parse(recorder.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse redirect location: %v", err)
	}
	if location.Path != PathProductPrefix+"workflows/history" {
		t.Fatalf("redirect path = %q, want canonical workflow history", location.Path)
	}
	if location.RawQuery != request.URL.RawQuery {
		t.Fatalf("redirect query = %q, want exact original %q", location.RawQuery, request.URL.RawQuery)
	}
}

func TestUXRefine115_LegacyHistoryStillRequiresAuthentication(t *testing.T) {
	h, _ := newShellHandler(t, false)
	request := httptest.NewRequest(http.MethodGet, "http://cell.test"+PathProductPrefix+"history?status=completed", nil)
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated legacy history status = %d, want existing 401 admission response", recorder.Code)
	}
	if recorder.Header().Get("Location") != "" {
		t.Fatalf("unauthenticated legacy history unexpectedly redirected to %q", recorder.Header().Get("Location"))
	}
}
