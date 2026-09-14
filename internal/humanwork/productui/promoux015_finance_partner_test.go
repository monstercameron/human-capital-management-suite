package productui

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
)

// TestFinancePartnerPageVisibleMirrorsRoleAccess proves PROMOUX-015's
// finance_partner case in PageVisible agrees with
// roleaccess.DefaultPagePermissions for every page the roleaccess catalog
// governs, so a handler without a roleaccess store and the live server admit
// the same destinations, and that it reaches no workforce directory, person
// profile or journey launcher.
func TestFinancePartnerPageVisibleMirrorsRoleAccess(t *testing.T) {
	roles := []string{"finance_partner"}
	permissions := roleaccess.EffectivePagePermissions(roleaccess.Snapshot{PagePermissions: roleaccess.DefaultPagePermissions()}, roles)
	governed := map[PageID]bool{}
	for _, permission := range roleaccess.DefaultPagePermissions() {
		governed[PageID(permission.PageID)] = true
	}
	checked := 0
	for _, definition := range PageDefinitions() {
		if !governed[definition.ID] {
			continue
		}
		checked++
		want := roleaccess.CanPageAction(permissions, string(definition.ID), roleaccess.ActionView)
		if got := PageVisible(definition.ID, roles); got != want {
			t.Errorf("PageVisible(%s, finance_partner) = %v, roleaccess view = %v", definition.ID, got, want)
		}
	}
	if checked == 0 {
		t.Fatal("no registry page is governed by roleaccess; the mirror check proved nothing")
	}
	for _, page := range []PageID{PageWork, PageHistory, PageMyself, PageOrganization} {
		if !PageVisible(page, roles) {
			t.Errorf("finance_partner cannot see %s", page)
		}
	}
	for _, page := range []PageID{PagePeople, PagePerson, PageJourneys, PageInsights} {
		if PageVisible(page, roles) {
			t.Errorf("finance_partner can see %s", page)
		}
	}
}
