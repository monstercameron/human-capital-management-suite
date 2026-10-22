package workspace

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestWorkflowStartDeepLinkRoutesOnlyCatalogVisibleIDs(t *testing.T) {
	h, token := newShellHandler(t, false)
	h.workflowStarts = func(_ context.Context, _ values.TenantId, _ bool) ([]WorkflowStartConfig, error) {
		return []WorkflowStartConfig{
			{WorkflowID: "hcmnext.workflows.new_hire", Name: "New hire", Availability: "available"},
			{WorkflowID: "workflow/time-entry", Name: "Time entry", Availability: "missing_authority"},
		}, nil
	}

	for _, test := range []struct {
		name         string
		workflowID   string
		availability string
	}{
		{name: "available catalog entry", workflowID: "hcmnext.workflows.new_hire", availability: "available"},
		{name: "encoded slash is one ID segment", workflowID: "workflow/time-entry", availability: "missing_authority"},
	} {
		t.Run(test.name, func(t *testing.T) {
			target := workflowStartDeepLinkPrefix + url.PathEscape(test.workflowID) + "?locale=en-US"
			request := httptest.NewRequest(http.MethodGet, "http://cell.test"+target, nil)
			request.Header.Set("Authorization", "Bearer "+token)
			recorder := httptest.NewRecorder()
			h.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusOK {
				t.Fatalf("GET %s = %d; want 200: %s", target, recorder.Code, recorder.Body.String())
			}
			config := island(t, recorder.Body.String())
			if config.WorkflowStartWorkflowID != test.workflowID {
				t.Fatalf("hydrated workflow ID = %q; want %q", config.WorkflowStartWorkflowID, test.workflowID)
			}
			found := false
			for _, entry := range config.WorkflowStarts {
				if entry.WorkflowID == test.workflowID && entry.Availability == test.availability {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("authorized catalog entry %q with availability %q missing from hydration: %+v", test.workflowID, test.availability, config.WorkflowStarts)
			}
		})
	}

	for _, target := range []string{
		workflowStartDeepLinkPrefix + "hidden-workflow",
		workflowStartDeepLinkPrefix + "bad/extra-segment",
	} {
		request := httptest.NewRequest(http.MethodGet, "http://cell.test"+target, nil)
		request.Header.Set("Authorization", "Bearer "+token)
		recorder := httptest.NewRecorder()
		h.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusNotFound {
			t.Errorf("GET unknown or malformed route %s = %d; want 404", target, recorder.Code)
		}
	}

	unauthenticated := httptest.NewRequest(http.MethodGet, "http://cell.test"+workflowStartDeepLinkPrefix+"hcmnext.workflows.new_hire", nil)
	unauthenticatedRecorder := httptest.NewRecorder()
	h.ServeHTTP(unauthenticatedRecorder, unauthenticated)
	if unauthenticatedRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated deep link = %d; want 401", unauthenticatedRecorder.Code)
	}
}

func TestProductWorkflowStartDeepLinkIDRequiresOneEscapedSegment(t *testing.T) {
	for _, test := range []struct {
		path string
		want string
		ok   bool
	}{
		{path: workflowStartDeepLinkPrefix + "hcmnext.workflows.new_hire", want: "hcmnext.workflows.new_hire", ok: true},
		{path: workflowStartDeepLinkPrefix + "workflow%2Ftime-entry", want: "workflow/time-entry", ok: true},
		{path: workflowStartDeepLinkPrefix + "workflow%00time-entry", ok: false},
		{path: workflowStartDeepLinkPrefix, ok: false},
		{path: workflowStartDeepLinkPrefix + "bad/extra", ok: false},
		{path: "/workspace/app/workflows/history", ok: false},
	} {
		requestURL, err := url.Parse("https://cell.test" + test.path)
		if err != nil {
			t.Fatalf("parse %q: %v", test.path, err)
		}
		got, ok := productWorkflowStartDeepLinkID(requestURL)
		if got != test.want || ok != test.ok {
			t.Errorf("productWorkflowStartDeepLinkID(%q) = (%q, %v), want (%q, %v)", test.path, got, ok, test.want, test.ok)
		}
	}
}
