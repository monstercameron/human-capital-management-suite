package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UXAUDIT_020(t *testing.T) {
	view := testView(PageOrganization)
	view.Appearance.Density = "compact"
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`data-organization-summary="true"`, `organization-summary-facts`,
		`class="org organization-density-compact"`, `class="organization-search"`,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("organization browsing output missing %q", want)
		}
	}
	if strings.Index(doc, `data-organization-summary="true"`) > strings.Index(doc, `class="organization-search"`) {
		t.Fatal("summary must precede the search control")
	}
}

func TestTodo_UXAUDIT_020_InteractionEnhancerIsConnected(t *testing.T) {
	view := testView(PageOrganization)
	doc, err := Render(ApplyRequest(view, PageRequest{OrganizationView: organizationViewFlat}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, `data-organization-action=`) || strings.Contains(doc, `data-organization-density=`) {
		t.Fatal("unhydrated organization controls must not be emitted")
	}
	if !strings.Contains(doc, `class="organization-unit-disclosure"`) {
		t.Fatal("enhancer has no flat disclosure targets")
	}
	tree, err := Render(ApplyRequest(view, PageRequest{OrganizationView: organizationViewTree}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tree, `class="ownership-reports"`) {
		view.People = []Person{
			{ID: "manager", WorkerID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", Name: "Manager", Team: "Team", ManagerRelationship: OrganizationRelationshipRoot},
			{ID: "worker", WorkerID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", Name: "Worker", Team: "Team", ManagerID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", ManagerWorkerRef: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", ManagerRelationship: OrganizationRelationshipVisible},
		}
		tree, err = Render(ApplyRequest(view, PageRequest{OrganizationView: organizationViewTree}))
		if err != nil {
			t.Fatal(err)
		}
	}
	if !strings.Contains(tree, `class="ownership-reports"`) {
		t.Fatal("enhancer has no reporting disclosure targets")
	}
}

func TestTodo_UXAUDIT_020_Browser(t *testing.T) {
	view := testView(PageOrganization)
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`<summary class="org-node manager">`, `class="organization-unit-members"`, `class="ownership-person-disclosure"`} {
		if !strings.Contains(doc, want) {
			t.Errorf("progressive disclosure/browser affordance missing %q", want)
		}
	}
	css := Stylesheet()
	for _, want := range []string{".organization-browse-summary", ".organization-summary-facts", ".organization-page .org-branches", "@media (max-width:430px)"} {
		if !strings.Contains(css, want) {
			t.Errorf("organization browsing stylesheet missing %q", want)
		}
	}
}

func TestTodo_UXAUDIT_020_Accessibility(t *testing.T) {
	view := testView(PageOrganization)
	view.Locale = ResolveProductLocale("ar")
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`aria-label=`, `aria-live="polite"`, `تفاصيل الموظف`} {
		if !strings.Contains(doc, want) {
			t.Errorf("keyboard/assistive affordance missing %q", want)
		}
	}
	if strings.Contains(doc, `role="treeitem"`) {
		t.Fatal("organization disclosure must retain native details semantics")
	}
}

func TestTodo_UXAUDIT_020_Performance(t *testing.T) {
	view := testView(PageOrganization)
	view.People = make([]Person, 0, 500)
	for i := 0; i < 500; i++ {
		view.People = append(view.People, Person{ID: string(rune(i + 1)), Name: "Worker", Team: "Team"})
	}
	doc, err := ui.RenderToString(ui.CreateElement(OrganizationPage, OrganizationPageProps{
		I18nProps: I18nProps{Locale: view.Locale}, Summary: OrganizationSummaryProps{VisiblePeople: len(view.People), Units: 1},
		Groups: []OrganizationGroupProps{{Name: "Team", Count: len(view.People), Members: make([]OwnershipNodeProps, len(view.People))}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(doc, `class="organization-unit"`) != 1 {
		t.Fatalf("large population rendered %d buckets; want one compact bucket", strings.Count(doc, `class="organization-unit"`))
	}
}

func TestTodo_UXAUDIT_020_Regression(t *testing.T) {
	view := testView(PageOrganization)
	view = ApplyRequest(view, PageRequest{Query: "Averi", OrganizationView: organizationViewFlat, SelectedPerson: "worker-avery"})
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, "Avery Patel") || strings.Contains(doc, "Jordan Lee") {
		t.Fatal("organization search escaped the admitted population")
	}
	if !strings.Contains(doc, `org_view="flat"`) && !strings.Contains(doc, "org_view=flat") {
		t.Fatal("flat view state was not preserved in the progressive search links")
	}
}
