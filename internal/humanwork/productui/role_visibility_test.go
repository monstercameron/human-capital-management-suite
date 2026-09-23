package productui

import "testing"

func TestPageVisibilityRoleMatrix(t *testing.T) {
	tests := []struct {
		name    string
		roles   []string
		allowed []PageID
		denied  []PageID
	}{
		{name: "admin", roles: []string{RoleHCMAdmin}, allowed: []PageID{PageHome, PagePeople, PageAdmin, PageChatSettings, PageWorkerIDs, PageAppearance}},
		{name: "legacy comp admin", roles: []string{"comp_admin"}, allowed: []PageID{PageAdmin, PageOrganizationVisibility}},
		{name: "hiring manager", roles: []string{"manager", "hiring_manager"}, allowed: []PageID{PageHome, PagePeople, PageJourneys, PageInsights}, denied: []PageID{PageAdmin, PageChatSettings, PageWorkerIDs}},
		{name: "payroll manager", roles: []string{"payroll_manager"}, allowed: []PageID{PageHome, PagePeople, PageWork, PageInsights}, denied: []PageID{PageAdmin, PageAppearance}},
		{name: "individual contributor", roles: []string{"worker_self"}, allowed: []PageID{PageHome, PageMyself, PageOrganization, PageHelp, PageSettings}, denied: []PageID{PagePeople, PagePerson, PageJourneys, PageWork, PageInsights, PageAdmin}},
		{name: "zero roles", roles: []string{}, allowed: []PageID{PageHome, PageHelp, PageSettings}, denied: []PageID{PageMyself, PageOrganization, PagePeople, PageJourneys, PageAdmin}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, page := range test.allowed {
				if !PageVisible(page, test.roles) {
					t.Errorf("PageVisible(%s) = false, want true", page)
				}
			}
			for _, page := range test.denied {
				if PageVisible(page, test.roles) {
					t.Errorf("PageVisible(%s) = true, want false", page)
				}
			}
		})
	}
}

func TestApplyRoleVisibilityFiltersNavigationChildren(t *testing.T) {
	view := ApplyRoleVisibility(NewView(PageHome, "tenant", "person", "scope"), []string{"worker_self"})
	for _, item := range view.Navigation {
		if !PageVisible(item.Page, view.Roles) {
			t.Errorf("navigation exposed %s", item.Page)
		}
		for _, child := range item.Children {
			if !PageVisible(child.Page, view.Roles) {
				t.Errorf("navigation child exposed %s", child.Page)
			}
		}
	}
}

func TestRoleVisibilityAlsoFiltersGlobalSearchRecords(t *testing.T) {
	view := ApplyRoleVisibility(NewView(PageHome, "tenant", "person", "scope"), []string{"worker_self"})
	view.Work = []WorkItem{{ID: "journey-secret", Title: "Promotion", Person: "Other worker"}}
	for _, item := range globalSearchItems(view) {
		if item.ID == "workflow-instance:journey-secret" || item.ID == "page:people" || item.ID == "page:admin" {
			t.Errorf("global search leaked %q", item.ID)
		}
	}
	if got := globalSearchProps(view).FallbackHref; got != Path(PageHome) {
		t.Errorf("IC search fallback = %q, want home", got)
	}
}
