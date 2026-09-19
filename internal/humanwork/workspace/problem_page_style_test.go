package workspace

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestProblemPageStylesheetIsAllowedByItsOwnPolicy: a refusal page inlines one
// stylesheet and names one style hash in its policy. The two must be the same
// bytes, or the browser blocks the page's CSS and the 404 renders unstyled.
func TestProblemPageStylesheetIsAllowedByItsOwnPolicy(t *testing.T) {
	h := &Handler{}
	response := httptest.NewRecorder()
	h.writeProblem(response, http.StatusNotFound, "No such workspace route", "detail")
	body := response.Body.String()
	open := strings.Index(body, "<style>")
	end := strings.Index(body, "</style>")
	if open < 0 || end < open {
		t.Fatal("the problem page inlines no stylesheet")
	}
	inline := body[open+len("<style>") : end]
	policy := response.Header().Get("Content-Security-Policy")
	if want := sha256Source(inline); !strings.Contains(policy, want) {
		t.Fatalf("the problem page's stylesheet hash %s is not in its own policy %q", want, policy)
	}
}

// TestProblemPageToneAndWayBack: a refusal the reader can resolve (4xx) is an
// amber notice and a server failure (5xx) is red; both offer the way back.
func TestProblemPageToneAndWayBack(t *testing.T) {
	h := &Handler{}
	for status, tone := range map[int]string{
		http.StatusForbidden:           `data-status="needs_review"`,
		http.StatusNotFound:            `data-status="needs_review"`,
		http.StatusInternalServerError: `data-status="failed"`,
	} {
		response := httptest.NewRecorder()
		h.writeProblem(response, status, "Title", "detail")
		body := response.Body.String()
		if !strings.Contains(body, tone) {
			t.Errorf("%d: want %s", status, tone)
		}
		if !strings.Contains(body, `href="`+PathProductPrefix+`"`) {
			t.Errorf("%d: the problem page has no way back to the workspace", status)
		}
	}
}
