package productui

import "testing"

func TestIssue3959_CanOpenAnalyticsNeedsAResolvedViewRight(t *testing.T) {
	view := NewView(PageHome, "t", "p", "s")
	if view.CanOpenAnalytics() {
		t.Error("a view with no resolved permissions may open analytics")
	}
	view.EffectivePermissions = []RolePagePermission{{Page: PagePeople, View: true}}
	if view.CanOpenAnalytics() {
		t.Error("a right to another page opens analytics")
	}
	view.EffectivePermissions = []RolePagePermission{{Page: PageReportCatalog, View: false}}
	if view.CanOpenAnalytics() {
		t.Error("a denied view right opens analytics")
	}
	view.EffectivePermissions = []RolePagePermission{{Page: PageReportCatalog, View: true}}
	if !view.CanOpenAnalytics() {
		t.Error("a granted view right is refused")
	}
}
