package productui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestFeatureComponentPropsNeverEmbedPageView(t *testing.T) {
	viewType := reflect.TypeOf(View{})
	props := []any{
		ActionLinkProps{}, FactProps{}, MetricProps{}, ActivityProps{}, PanelProps{}, EmptyStateProps{}, PopoverSurfaceProps{}, TransientPopoverProps{},
		WorkPageProps{}, WorkCollectionProps{}, WorkTabProps{}, WorkRowProps{}, WorkCollectionFooterProps{}, WorkPreviewProps{},
		ProvenancePresentationProps{},
		HomePageProps{}, SummaryCardProps{}, QuickActionsProps{}, RecentActivityProps{},
		OrganizationPageProps{}, OrganizationGroupProps{}, BusinessMetadataProps{}, BusinessMetadataItemProps{}, InsightsPageProps{}, AttentionPanelProps{},
		AdminPageProps{}, AdminHeroProps{}, CapabilityCardProps{}, HelpPageProps{}, InformationalPanelProps{},
		SettingsPageProps{}, ViewerProfileProps{}, AccessContextProps{}, LocalePreferencesProps{}, LocaleOptionProps{}, StudioPageProps{}, AppearancePageProps{}, AppearanceOption{}, BrandLogoProps{},
		ContextSwitcherProps{},
		PeoplePageProps{}, PeopleSummaryProps{}, PeopleFilterProps{}, PeopleDirectoryProps{}, PeopleTableProps{}, PeopleRowProps{},
		DataTableProps{}, DataTableColumnProps{}, DataTableRowProps{}, DataTableCellProps{},
		PeoplePaginationProps{}, PageSizeControlProps{}, PaginationLinkProps{}, PeopleEmptyStateProps{}, PersonPageProps{}, MyselfPageProps{}, SelfServiceBoundaryProps{},
		PersonUnavailableProps{}, PersonProfileProps{}, PersonProfileCompositionProps{}, PersonHeroProps{}, EmploymentDetailsProps{}, SensitiveDetailsProps{},
		ProfileFactProps{}, WorkflowLauncherProps{}, ActionLauncherProps{}, ActionLauncherItem{}, WorkflowFilterProps{}, WorkflowCardProps{},
		WorkflowHistoryProps{}, WorkflowHistoryFilterProps{}, HistoryFilterOption{}, HistorySortColumnProps{}, WorkflowHistoryItemProps{},
	}
	for _, value := range props {
		typeOf := reflect.TypeOf(value)
		if typeContains(typeOf, viewType, map[reflect.Type]bool{}) {
			t.Fatalf("%s directly or transitively embeds the page-wide View instead of narrow props", typeOf.Name())
		}
	}
}

func typeContains(candidate, forbidden reflect.Type, seen map[reflect.Type]bool) bool {
	if candidate == forbidden || candidate == reflect.PointerTo(forbidden) {
		return true
	}
	if seen[candidate] {
		return false
	}
	seen[candidate] = true
	switch candidate.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array:
		return typeContains(candidate.Elem(), forbidden, seen)
	case reflect.Map:
		return typeContains(candidate.Key(), forbidden, seen) || typeContains(candidate.Elem(), forbidden, seen)
	case reflect.Struct:
		for index := 0; index < candidate.NumField(); index++ {
			if typeContains(candidate.Field(index).Type, forbidden, seen) {
				return true
			}
		}
	case reflect.Func:
		for index := 0; index < candidate.NumIn(); index++ {
			if typeContains(candidate.In(index), forbidden, seen) {
				return true
			}
		}
		for index := 0; index < candidate.NumOut(); index++ {
			if typeContains(candidate.Out(index), forbidden, seen) {
				return true
			}
		}
	}
	return false
}

func TestPeopleComponentsRenderIndependentlyFromRouteProjection(t *testing.T) {
	markup, err := ui.RenderToString(ui.CreateElement(PeoplePage, PeoplePageProps{
		Summary: PeopleSummaryProps{CountLabel: "1 person", ScopeLabel: "Authorized directory"},
		Filter:  PeopleFilterProps{Query: "Avery", Action: "/people", ClearHref: "/people", NavCollapsed: true},
		Directory: &PeopleDirectoryProps{
			Rows: []PeopleRowProps{{Initials: "AP", Name: "Avery Patel", Role: "Designer", Team: "Product", Location: "Toronto", Href: "/person/avery"}},
			Pagination: PeoplePaginationProps{
				First: 1, Last: 1, Total: 1, Page: 1, PageCount: 1,
				Previous: PaginationLinkProps{Label: "Previous", Disabled: true},
				Next:     PaginationLinkProps{Label: "Next", Disabled: true},
			},
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"1 person", "Authorized directory", `name="nav"`, "Avery Patel", `href="/person/avery"`,
		"1–1 of 1", "Page 1 of 1", `aria-disabled="true"`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("standalone People component missing %q", want)
		}
	}
}

func TestPersonComponentsRenderIndependentlyFromRouteProjection(t *testing.T) {
	markup, err := ui.RenderToString(ui.CreateElement(PersonPage, PersonPageProps{
		BackHref: "/people?q=product",
		Profile: &PersonProfileProps{
			Hero:         PersonHeroProps{Initials: "AP", Name: "Avery Patel", Role: "Designer"},
			Details:      EmploymentDetailsProps{Title: "Employment overview", Facts: []ProfileFactProps{{Label: "Worker number", Value: "NW-1"}}},
			Organization: EmploymentDetailsProps{Title: "Organization", Facts: []ProfileFactProps{{Label: "Organization unit", Value: "Product"}}},
			Compensation: EmploymentDetailsProps{Title: "Compensation", Facts: []ProfileFactProps{{Label: "Pay zone", Value: "CA-ON"}}},
			Personal: SensitiveDetailsProps{Title: "Personal information", Description: "Hidden by default", Badge: "Restricted",
				Facts: []ProfileFactProps{{Label: "Legal name", Value: "Avery Patel"}}},
			Workflows: WorkflowLauncherProps{
				PersonName: "Avery Patel", TotalCount: 1,
				Filter:    WorkflowFilterProps{Action: "/person", PersonID: "worker-1", DirectoryQuery: "product", DirectoryPage: 2, NavCollapsed: true},
				Workflows: []WorkflowCardProps{{Name: "Promotion", Category: "Career", Description: "Propose a change.", Href: "/journey?worker=worker-1"}},
			},
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`href="/people?q=product"`, "Avery Patel", "NW-1", "Organization", "Product", "Compensation", "CA-ON",
		"Personal information", "Restricted", "Legal name", `name="person"`, `value="worker-1"`,
		`name="q"`, `name="page"`, `name="nav"`, "Start Promotion", `href="/journey?worker=worker-1"`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("standalone Person component missing %q", want)
		}
	}
	if strings.Contains(markup, `<details open`) {
		t.Fatal("personal information disclosure rendered open by default")
	}
}

func TestPersonPageZeroPropsFailsClosed(t *testing.T) {
	markup, err := ui.RenderToString(ui.CreateElement(PersonPage, PersonPageProps{}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, "Person not available") || strings.Contains(markup, "Start a workflow") {
		t.Fatal("zero-value Person props did not render a safe unavailable state")
	}
}
