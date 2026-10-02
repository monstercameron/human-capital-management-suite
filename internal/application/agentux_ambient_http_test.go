package application

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/ambientagents"
)

// TestAgentUXAmbient_ReadRouteIsMounted: Chat reads /api/chat/ambient for every
// conversation it opens. The route is on the served assembly: another path is
// left alone, an unauthenticated read is refused by admission (not "not found"),
// and where the feature is off the answer is 200 with nothing in it.
func TestAgentUXAmbient_ReadRouteIsMounted(t *testing.T) {
	nextCalls := 0
	h := OverlayAgentUXAmbient(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { nextCalls++; w.WriteHeader(http.StatusNoContent) }), nil, transport.Config{})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/chat/other", nil))
	if w.Code != http.StatusNoContent || nextCalls != 1 {
		t.Fatalf("another route was intercepted: %d", w.Code)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, ambientagents.Path+"?conversation=general", nil))
	if w.Code == http.StatusNotFound || w.Code < 400 || nextCalls != 1 {
		t.Fatalf("an unauthenticated read was %d, want a refusal from admission and not a missing route", w.Code)
	}
	w = httptest.NewRecorder()
	writeAgentUXAmbientEmpty(w)
	var snapshot map[string]json.RawMessage
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &snapshot) != nil {
		t.Fatalf("the empty answer is %d %s", w.Code, w.Body.String())
	}
	for _, field := range []string{"cards", "grants", "tasks"} {
		if strings.TrimSpace(string(snapshot[field])) != "[]" {
			t.Fatalf("%s is %s, want an empty list and not null", field, snapshot[field])
		}
	}
	if string(snapshot["opt_out"]) != "false" {
		t.Fatalf("opt_out is %s", snapshot["opt_out"])
	}
}
