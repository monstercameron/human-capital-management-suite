package workspace

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/preferences"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type uxblindTPreferenceStore struct {
	snapshot preferences.Snapshot
}

func (s uxblindTPreferenceStore) Load(context.Context, values.TenantId, string, string) (preferences.Snapshot, error) {
	return s.snapshot, nil
}

func (s uxblindTPreferenceStore) SaveUser(context.Context, values.TenantId, string, preferences.User) (preferences.User, error) {
	return s.snapshot.User, nil
}

func (uxblindTPreferenceStore) SaveTheme(context.Context, values.TenantId, string, string, preferences.TenantTheme) (preferences.TenantTheme, error) {
	return preferences.TenantTheme{}, nil
}

func (uxblindTPreferenceStore) SaveOrganizationVisibility(context.Context, values.TenantId, string, string, preferences.OrganizationVisibility) (preferences.OrganizationVisibility, error) {
	return preferences.OrganizationVisibility{}, nil
}

func (uxblindTPreferenceStore) RecordWorkflowUse(context.Context, values.TenantId, string, string) (preferences.User, error) {
	return preferences.User{}, nil
}

func TestTodo_UXBLIND_067_Integration(t *testing.T) {
	h, token := newShellHandler(t, false)
	snapshot := preferences.DefaultSnapshot()
	snapshot.User.Accessibility = preferences.Accessibility{TextSize: "larger", Contrast: "more", Motion: "reduce", Links: "underlined"}
	h.preferences = uxblindTPreferenceStore{snapshot: snapshot}
	recorder := uxblindTRequest(t, h, token, PathProductHome)
	if recorder.Code != 200 {
		t.Fatalf("GET product shell = %d: %s", recorder.Code, recorder.Body.String())
	}
	doc := recorder.Body.String()
	for _, want := range []string{`data-hcm-text-size="larger"`, `data-hcm-contrast="more"`, `data-hcm-motion-preference="reduce"`, `data-hcm-links="underlined"`} {
		if !strings.Contains(doc, want) {
			t.Errorf("stored preference was not present in the initial document: %q", want)
		}
	}
}

func TestTodo_UXBLIND_067_Browser(t *testing.T) {
	stored := productui.AccessibilityPreferences{TextSize: "larger", Contrast: "more", Motion: "reduce", Links: "underlined"}
	doc := firstPaintDocument(t, productui.DefaultCustomerTheme(), stored)
	root := firstPaintRoot(t, doc)
	if !strings.Contains(root, `data-hcm-text-size="larger"`) || !strings.Contains(doc, `<meta name="color-scheme"`) {
		t.Fatal("the browser's first paint did not receive the stored presentation state")
	}
}

func uxblindTRequest(t *testing.T, h *Handler, token, path string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "http://cell.test"+path, nil)
	request.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(recorder, request)
	return recorder
}
