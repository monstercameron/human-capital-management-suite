package personachat

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// TestTodo_AGENTP_020_RetryRefusesTrailingData pins the retry action to the
// same request shape as every other invocation action: one JSON object and
// nothing after it. A body with trailing bytes is refused rather than
// retried, so a client that framed two requests as one is told instead of
// having the second silently dropped.
func TestTodo_AGENTP_020_RetryRefusesTrailingData(t *testing.T) {
	surface := &surfaceFixture{}
	handler := Handler{Surface: surface}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("POST", Path+"/invocations/invocation/retry", strings.NewReader(`{"idempotency_key":"k"} {}`)))
	if w.Code != 400 {
		t.Fatalf("trailing data retry=%d %s", w.Code, w.Body.String())
	}
	if surface.retryID != "" {
		t.Fatalf("a refused retry reached the surface: id=%q", surface.retryID)
	}
}
