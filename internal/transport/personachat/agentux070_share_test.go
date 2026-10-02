package personachat

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
)

type shareFixture struct {
	surfaceFixture
	shareID, shareKey string
	shareErr          error
}

func (s *shareFixture) ShareAnswer(_ context.Context, id, key string) (ShareResult, error) {
	s.shareID, s.shareKey = id, key
	return ShareResult{InvocationID: id, PostID: "shared-post"}, s.shareErr
}

// TestTodo_AGENTUX_070_ShareRoute is the browser's way to post a private answer
// to its channel: one POST, answered with the message it became, or with the
// reason it stays private.
func TestTodo_AGENTUX_070_ShareRoute(t *testing.T) {
	surface := &shareFixture{}
	post := func(path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		Handler{Surface: surface}.ServeHTTP(w, httptest.NewRequest("POST", path, strings.NewReader(body)))
		return w
	}
	w := post(Path+"/invocations/run-1/share", `{"idempotency_key":"share-key-0001"}`)
	if w.Code != 200 || surface.shareID != "run-1" || surface.shareKey != "share-key-0001" || !strings.Contains(w.Body.String(), `"post_id":"shared-post"`) || !strings.Contains(w.Body.String(), `"invocation_id":"run-1"`) {
		t.Fatalf("share = %d %s (%q, %q)", w.Code, w.Body.String(), surface.shareID, surface.shareKey)
	}
	// A refusal names its reason, and only the reason: the browser turns it into words.
	for _, reason := range []string{"agent", "audience"} {
		surface.shareErr = &FinalStateConflict{State: reason}
		w = post(Path+"/invocations/run-1/share", `{"idempotency_key":"share-key-0001"}`)
		if w.Code != 409 || !strings.Contains(w.Body.String(), `"error":"conflict"`) || !strings.Contains(w.Body.String(), `"state":"`+reason+`"`) {
			t.Fatalf("refused share (%s) = %d %s", reason, w.Code, w.Body.String())
		}
	}
	surface.shareErr = ErrDenied
	if w = post(Path+"/invocations/run-1/share", `{"idempotency_key":"share-key-0001"}`); w.Code != 403 {
		t.Fatalf("a share of someone else's answer = %d", w.Code)
	}
	surface.shareErr = nil
	for name, tc := range map[string]struct{ path, body string }{
		"unknown field": {Path + "/invocations/run-1/share", `{"idempotency_key":"share-key-0001","post_id":"x"}`},
		"no body":       {Path + "/invocations/run-1/share", ``},
		"nested id":     {Path + "/invocations/a/b/share", `{"idempotency_key":"share-key-0001"}`},
		"no id":         {Path + "/invocations//share", `{"idempotency_key":"share-key-0001"}`},
	} {
		if w = post(tc.path, tc.body); w.Code != 400 {
			t.Errorf("%s = %d, want 400", name, w.Code)
		}
	}
	// A surface that cannot share is unavailable, not a pretend success.
	w = httptest.NewRecorder()
	Handler{Surface: &surfaceFixture{}}.ServeHTTP(w, httptest.NewRequest("POST", Path+"/invocations/run-1/share", strings.NewReader(`{"idempotency_key":"share-key-0001"}`)))
	if w.Code != 503 {
		t.Fatalf("a surface that cannot share = %d", w.Code)
	}
	// Only POST shares.
	w = httptest.NewRecorder()
	Handler{Surface: surface}.ServeHTTP(w, httptest.NewRequest("GET", Path+"/invocations/run-1/share", nil))
	if w.Code != 404 {
		t.Fatalf("GET share = %d", w.Code)
	}
}
