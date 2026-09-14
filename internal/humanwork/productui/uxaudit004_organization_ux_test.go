package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// The mainline tree tests exercise the authorized manager-resolution statuses.
// These tests retain the UX branch's search, route, locale and field-disclosure
// contracts against that same identity-based tree rather than a second
// display-name relationship builder.
func uxAudit004View() View {
	view := testView(PageOrganization)
	manager := uxaudit004Person(81, "Alex Morgan", "")
	worker := uxaudit004Person(82, "Casey Lee", manager.WorkerID)
	manager.Team, worker.Team = "People", "People"
	view.People = []Person{manager, worker}
	view.SelectedPerson = worker.ID
	return view
}

func TestTodo_UXAUDIT_004_SearchKeepsAuthorizedAncestors(t *testing.T) {
	view := uxAudit004View()
	view.Query = "Casey"
	index := newOrganizationRelationshipIndex(view)
	match := filterOrganizationPeople(admittedPeople(view), view.Query)
	if len(match) != 1 || match[0].Name != "Casey Lee" {
		t.Fatalf("filtered people = %+v", match)
	}
	forest := filterOwnershipTree(index.tree(view), map[string]bool{match[0].ID: true})
	if len(forest) != 1 || len(forest[0].Reports) != 1 || forest[0].Reports[0].ID != match[0].ID {
		t.Fatalf("filter detached the admitted manager edge: %+v", forest)
	}
	doc, err := ui.RenderToString(organizationPage(view))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, "Casey Lee") || !strings.Contains(doc, "Alex Morgan") || !strings.Contains(doc, `role="tree"`) {
		view.OrganizationView = organizationViewTree
		doc, err = ui.RenderToString(organizationPage(view))
		if err != nil || !strings.Contains(doc, `role="tree"`) || !strings.Contains(doc, "Alex Morgan") {
			t.Fatalf("searched tree lost context: %v %s", err, doc)
		}
	}
}

func TestTodo_UXAUDIT_004_SearchPreservesAddressAndLocale(t *testing.T) {
	view := uxAudit004View()
	view.OrganizationView = organizationViewTree
	view.NavCollapsed = true
	view.Locale = ResolveProductLocale("de-DE")
	href := organizationFilterHref(view, "Casey")
	for _, want := range []string{"locale=de-DE", "nav=collapsed", "org_view=tree", "q=Casey"} {
		if !strings.Contains(href, want) {
			t.Errorf("filter href %q lost %q", href, want)
		}
	}
	search, err := ui.RenderToString(ui.CreateElement(OrganizationSearch, OrganizationSearchProps{
		I18nProps: I18nProps{Locale: view.Locale}, Query: "Casey", Action: pageHref(PageOrganization), HiddenInputs: organizationSearchHiddenInputs(view),
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{`name="org_view"`, `name="nav"`, `name="locale"`} {
		if !strings.Contains(search, name) {
			t.Errorf("search lost %s: %s", name, search)
		}
	}
}

func TestTodo_UXAUDIT_004_SpecializedRoutesAreLocalized(t *testing.T) {
	keys := []string{
		"page.org_explorer.label", "page.org_explorer.title", "page.org_explorer.subtitle",
		"page.org_outline.label", "page.org_outline.title", "page.org_outline.subtitle",
		"page.org_responsive.label", "page.org_responsive.title", "page.org_responsive.subtitle",
	}
	for _, locale := range []string{"de-DE", "ar"} {
		context, english := ResolveProductLocale(locale), ResolveProductLocale("en-US")
		for _, key := range keys {
			if got := context.Text(key); strings.TrimSpace(got) == "" || got == english.Text(key) {
				t.Errorf("%s %s fell back to %q", locale, key, got)
			}
		}
	}
}

func TestTodo_UXAUDIT_004_FieldDeniedManagerNameDoesNotLeak(t *testing.T) {
	view := uxAudit004View()
	manager, worker := view.People[0], view.People[1]
	view.RecordVerdicts = map[string]AuthorizedRecord{
		manager.ID: {ID: manager.ID, Disclosable: true, Fields: map[string]AuthorizedField{"name": {Effect: PresentationDenied}}},
		worker.ID:  {ID: worker.ID, Disclosable: true},
	}
	index := newOrganizationRelationshipIndex(view)
	child := index.annotate(view, index.indexByName[worker.ID])
	if strings.Contains(child.ManagerSummary, manager.Name) {
		t.Fatalf("field-denied manager leaked through report summary: %+v", child)
	}
	if got := ownershipPerson(view, manager).Name; got == manager.Name {
		t.Fatalf("field-denied manager leaked through node: %q", got)
	}
}

func TestTodo_UXAUDIT_004_OutlineCanonicalizesItsLockedTreeState(t *testing.T) {
	view := uxAudit004View()
	view.Page = PageOrgOutline
	view = ApplyRequest(view, PageRequest{Page: PageOrgOutline, OrganizationView: organizationViewFlat})
	if view.OrganizationView != organizationViewTree {
		t.Fatalf("outline organization view = %q, want tree", view.OrganizationView)
	}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, `data-organization-view="flat"`) || !strings.Contains(doc, `data-organization-view="tree"`) {
		t.Fatal("locked outline emitted contradictory view state")
	}
}
