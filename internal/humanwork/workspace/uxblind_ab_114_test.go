package workspace

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/preferences"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type uxblindAB114Preferences struct {
	snapshot preferences.Snapshot
}

func (s uxblindAB114Preferences) Load(context.Context, values.TenantId, string, string) (preferences.Snapshot, error) {
	return s.snapshot, nil
}

func (s uxblindAB114Preferences) SaveUser(context.Context, values.TenantId, string, preferences.User) (preferences.User, error) {
	return s.snapshot.User, nil
}

func (uxblindAB114Preferences) SaveTheme(context.Context, values.TenantId, string, string, preferences.TenantTheme) (preferences.TenantTheme, error) {
	return preferences.TenantTheme{}, nil
}

func (uxblindAB114Preferences) SaveOrganizationVisibility(context.Context, values.TenantId, string, string, preferences.OrganizationVisibility) (preferences.OrganizationVisibility, error) {
	return preferences.OrganizationVisibility{}, nil
}

func (uxblindAB114Preferences) RecordWorkflowUse(context.Context, values.TenantId, string, string) (preferences.User, error) {
	return preferences.User{}, nil
}

func TestTodo_UXBLIND_114(t *testing.T) {
	snapshot := preferences.DefaultSnapshot()
	snapshot.User.FavoritePages = []string{string(productui.PagePeople)}
	snapshot.User.NavigationGroups = map[string]bool{string(productui.PageWork): false, string(productui.PageAdmin): true}
	h, token := newShellHandler(t, false)
	h.preferences = uxblindAB114Preferences{snapshot: snapshot}

	request := httptest.NewRequest(http.MethodGet, "http://cell.test"+PathProductPrefix+"journeys", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET product shell = %d: %s", recorder.Code, recorder.Body.String())
	}

	loading := recorder.Body.String()
	view := productui.NewView(productui.PageJourneys, productui.DisplayLabel(shellTenant), productui.DisplayLabel(shellSubject), "")
	view.FavoritePages = []productui.PageID{productui.PagePeople}
	view.NavigationGroupOpen = map[productui.PageID]bool{productui.PageWork: false, productui.PageAdmin: true}
	view = productui.ApplyRoleVisibility(view, []string{"intent_author", "comp_admin"})
	hydrated, err := ui.RenderToString(productui.Build(view))
	if err != nil {
		t.Fatal(err)
	}
	for _, tag := range []string{"header", "aside", "footer"} {
		if got, want := uxblindAB114Element(loading, tag), uxblindAB114Element(hydrated, tag); got != want {
			t.Fatalf("%s chrome differs between loading and hydrated models\nloading: %s\nhydrated: %s", tag, got, want)
		}
	}
}

func TestTodo_UXBLIND_114_Browser(t *testing.T) {
	snapshot := preferences.DefaultSnapshot()
	snapshot.User.FavoritePages = []string{string(productui.PagePeople)}
	snapshot.User.NavigationGroups = map[string]bool{string(productui.PageWork): false}
	h, token := newShellHandler(t, false)
	h.preferences = uxblindAB114Preferences{snapshot: snapshot}

	request := httptest.NewRequest(http.MethodGet, "http://cell.test"+PathProductPrefix+"journeys", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET product shell = %d: %s", recorder.Code, recorder.Body.String())
	}
	doc := recorder.Body.String()
	for _, want := range []string{
		`class="app-shell is-content-loading"`,
		`data-hcm-transient-popover="account"`,
		`data-hcm-transient-popover="notification"`,
		`data-async-region="page-content"`,
		"Favorites",
		"People",
		"Acme Industries",
		`aria-busy="true"`,
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("loading chrome missing %q", want)
		}
	}
	if strings.Contains(uxblindAB114Element(doc, "footer"), "Human Capital Management Suite") {
		t.Fatal("loading footer retained the product default brand")
	}
	for _, unwanted := range []string{"viewer-profile-loading", "notification-loading"} {
		if strings.Contains(uxblindAB114Element(doc, "header"), unwanted) {
			t.Fatalf("loading chrome retained stale or placeholder value %q", unwanted)
		}
	}
}

func uxblindAB114Element(document, tag string) string {
	start := strings.Index(document, "<"+tag)
	if start < 0 {
		return ""
	}
	end := strings.Index(document[start:], "</"+tag+">")
	if end < 0 {
		return document[start:]
	}
	return document[start : start+end+len("</"+tag+">")]
}
