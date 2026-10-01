package workspace

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/preferences"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func TestTodo_WFPAGE_002_StartAuthority(t *testing.T) {
	grant := func(page string, create bool) roleaccess.PagePermission {
		return roleaccess.PagePermission{RoleID: "r", PageID: page, View: true, Create: create}
	}
	cases := []struct {
		name   string
		access productAccess
		want   bool
	}{
		{"compat policy without a role store", productAccess{}, true},
		{"start view and journeys create", productAccess{configured: true, permissions: []roleaccess.PagePermission{grant("workflow-start", false), grant("journeys", true)}}, true},
		{"journeys create missing", productAccess{configured: true, permissions: []roleaccess.PagePermission{grant("workflow-start", false), grant("journeys", false)}}, false},
		{"start page not granted", productAccess{configured: true, permissions: []roleaccess.PagePermission{grant("journeys", true)}}, false},
	}
	for _, tc := range cases {
		if got := workflowStartAuthority(tc.access); got != tc.want {
			t.Errorf("%s: authority = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestTodo_WFPAGE_002_ResolveWorkflowStartsFailsClosed(t *testing.T) {
	principal := &trust.Principal{}
	h := &Handler{}
	if got := h.resolveWorkflowStarts(context.Background(), principal, productAccess{}); got != nil {
		t.Fatalf("no source produced %+v", got)
	}
	h.workflowStarts = func(context.Context, values.TenantId, bool) ([]WorkflowStartConfig, error) {
		return nil, errors.New("store down")
	}
	if got := h.resolveWorkflowStarts(context.Background(), principal, productAccess{}); got != nil {
		t.Fatalf("a failed read produced %+v", got)
	}
	var sawCanStart bool
	h.workflowStarts = func(_ context.Context, _ values.TenantId, canStart bool) ([]WorkflowStartConfig, error) {
		sawCanStart = canStart
		return []WorkflowStartConfig{{WorkflowID: "hire", Name: "New hire", Availability: "available"}}, nil
	}
	got := h.resolveWorkflowStarts(context.Background(), principal, productAccess{configured: true})
	if len(got) != 1 || sawCanStart {
		t.Fatalf("got %+v, canStart=%v; the source must receive the resolved (denied) authority", got, sawCanStart)
	}
	if h.resolveWorkflowStarts(context.Background(), nil, productAccess{}) != nil {
		t.Fatal("no principal must yield no catalog")
	}
}

func TestTodo_WFPAGE_002_ProductWorkflowStartItems(t *testing.T) {
	items := productWorkflowStartItems([]WorkflowStartConfig{
		{WorkflowID: "hire", Name: "New hire", Keywords: []string{"onboarding"}, Availability: "available"},
		{WorkflowID: "", Name: "no id"},
		{WorkflowID: "noname", Name: " "},
	})
	if len(items) != 1 || items[0].Availability != productui.WorkflowStartAvailable || items[0].Keywords[0] != "onboarding" {
		t.Fatalf("items = %+v", items)
	}
}

func TestTodo_WFPAGE_004_PreferencesProjectPerUserState(t *testing.T) {
	user := preferences.User{
		FavoritePages: []string{"workflow-start:hire", "people"},
		WorkflowUses:  map[string]int64{"hire": 9, "promotion": 4},
	}
	got := applyWorkflowStartPreferences([]WorkflowStartConfig{
		{WorkflowID: "hire"}, {WorkflowID: "promotion"}, {WorkflowID: "hidden"},
	}, user)
	if !got[0].Favorite || got[0].RecentRank != 9 || got[1].Favorite || got[1].RecentRank != 4 || got[2].Favorite || got[2].RecentRank != 0 {
		t.Fatalf("preference projection = %+v", got)
	}
}
