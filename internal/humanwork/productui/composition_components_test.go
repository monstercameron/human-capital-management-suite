package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestWorkComponentsRenderWithoutPageProjection(t *testing.T) {
	markup, err := ui.RenderToString(ui.CreateElement(WorkPage, WorkPageProps{
		Collection: WorkCollectionProps{
			Title: "Promotion journeys", CountLabel: "1 item",
			Tabs:   []WorkTabProps{{Label: "All work", Href: "/work", Active: true}},
			Rows:   []WorkRowProps{{Initials: "AP", Title: "Promotion", Person: "Avery Patel", Summary: "Director", Href: "/work/one", Selected: true}},
			Footer: WorkCollectionFooterProps{Label: "Authorized work", Action: ActionLinkProps{Label: "View My Work →", Href: "/work"}},
		},
		Preview: WorkPreviewProps{
			Initials: "AP", Title: "Promotion", Person: "Avery Patel", Summary: "Director", FactsTitle: "Proposal",
			Facts: []FactProps{{Label: "Effective date", Value: "2026-10-01"}}, Action: ActionLinkProps{Label: "Open live journey", Href: "/journey", Class: "button primary full"},
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Promotion journeys", "1 item", "Avery Patel", "Lifecycle status is not available in this view", "Effective date", "2026-10-01", `href="/journey"`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("standalone Work composition missing %q", want)
		}
	}
}

func TestWorkRowUsesJourneyStageWithoutInventingLifecycleDimensions(t *testing.T) {
	markup, err := ui.RenderToString(WorkRow(WorkRowProps{JourneyStage: "Waiting for effective date", Href: "/work"}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, "Waiting for effective date") || strings.Contains(markup, "Lifecycle status is not available") || strings.Contains(markup, "data-status-dimension=") {
		t.Fatal("workflow stage missing or converted into invented lifecycle dimensions")
	}
	markup, err = ui.RenderToString(WorkRow(WorkRowProps{Href: "/work"}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, "Lifecycle status is not available") {
		t.Fatal("missing status was silently hidden")
	}
}

func TestWorkPreviewAndRowShareStageAndRespectCanonicalProjection(t *testing.T) {
	markup, err := ui.RenderToString(WorkPreview(WorkPreviewProps{JourneyStage: "Waiting for effective date"}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `class="workflow-stage"`) || !strings.Contains(markup, "Waiting for effective date") || strings.Contains(markup, "Lifecycle status is not available") {
		t.Fatal("preview disagrees with stage-only row")
	}
	markup, err = ui.RenderToString(workStatus(I18nProps{}, "test", StatusProjection{Available: true}, "must-not-override"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markup, "must-not-override") || !strings.Contains(markup, "data-status-dimension=") {
		t.Fatal("stage overrode canonical lifecycle projection")
	}
}

func TestHistoryEmptyFilterDoesNotClaimNoRecordedWorkflows(t *testing.T) {
	view := testView(PageHistory)
	view.HistoryQuery = "unmatched-worker-xyz"
	props := workflowHistoryProps(view, "", "History", "", true)
	if props.TotalCount == 0 || props.FilteredCount != 0 || props.EmptyText != view.Locale.Text("history.none_detail") {
		t.Fatal("filtered empty state must distinguish existing records from an empty history")
	}
	view.Work = nil
	props = workflowHistoryProps(view, "", "History", "", true)
	if props.EmptyText != view.Locale.Text("history.empty_terminal") {
		t.Fatal("empty history lost onboarding explanation")
	}
}

func TestPeopleMissingFactsAreExplicitWithoutGuessing(t *testing.T) {
	row := peopleDataTableRow(PeopleRowProps{Name: "Employee", Manager: "Visible manager"})
	for _, cell := range row.Cells {
		switch cell.ColumnID {
		case peopleSortRole, peopleSortTeam, peopleSortLocation:
			if cell.Text != "Not reported" {
				t.Fatalf("missing %s rendered as %q", cell.ColumnID, cell.Text)
			}
		case peopleSortManager:
			if cell.Text != "Visible manager" {
				t.Fatal("known manager replaced")
			}
		}
	}
}

func TestSettingsAccessCopyUsesPlainLocalizedLanguage(t *testing.T) {
	for _, locale := range SupportedProductLocales() {
		text := ResolveProductLocale(locale).Text("settings.access_callout")
		if text == "" || strings.Contains(text, "RPC") {
			t.Fatalf("technical access copy in %s: %s", locale, text)
		}
	}
}

func TestAdminHeroWithoutActionDoesNotRenderEmptyNavigation(t *testing.T) {
	markup, err := ui.RenderToString(AdminHero(AdminHeroProps{Title: "Organization"}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markup, "<a") || strings.Contains(markup, "<button") {
		t.Fatal("hero rendered an unconfigured action")
	}
}

func TestVisibilityPageUsesOneRoleSelector(t *testing.T) {
	props := OrganizationVisibilityPageProps{I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, Roles: []AccessRole{{ID: "reviewer", Name: "Reviewer", Active: true}}}
	markup, err := ui.RenderToString(OrganizationVisibilityPage(props))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(markup, `class="role-visibility-list"`) != 1 || strings.Contains(markup, "organization-visibility-role-selector") || !strings.Contains(markup, `name="organization-visibility-editors"`) {
		t.Fatal("expected one native role selector without duplicate navigation")
	}
}

func TestSharedActionLinkUsesSoftwareNavigationOnlyInsideTheProduct(t *testing.T) {
	navigate := func(string) {}
	internal := ActionLink(ActionLinkProps{Label: "People", Href: "/workspace/app/people", Navigate: navigate})
	if internal == nil || internal.Props["onclick"] == nil {
		t.Fatal("shared action link did not install software navigation for an internal product route")
	}
	external := ActionLink(ActionLinkProps{Label: "Journey", Href: "/workspace/journey#/journeys", Navigate: navigate})
	if external != nil && external.Props["onclick"] != nil {
		t.Fatal("shared action link intercepted a cross-application route")
	}
}

func TestEnterprisePageCompositionsRenderFromNarrowProps(t *testing.T) {
	nodes := []ui.Node{
		ui.CreateElement(HomePage, HomePageProps{Work: WorkCollectionProps{Footer: WorkCollectionFooterProps{}}, Overview: SummaryCardProps{Title: "Overview"}, QuickStart: QuickActionsProps{Title: "Start"}, Recent: RecentActivityProps{Title: "Recent"}}),
		ui.CreateElement(OrganizationPage, OrganizationPageProps{Title: "Organization", Groups: []OrganizationGroupProps{{Name: "Product", Count: 3}}}),
		ui.CreateElement(InsightsPage, InsightsPageProps{Metrics: []MetricProps{{Label: "Active", Value: "3", Note: "Live"}}, Attention: AttentionPanelProps{Title: "Attention"}}),
		ui.CreateElement(AdminPage, AdminPageProps{Hero: AdminHeroProps{Title: "Cell"}, Capabilities: []CapabilityCardProps{{Title: "Journey service", State: "Connected"}}}),
		ui.CreateElement(HelpPage, HelpPageProps{Guidance: QuickActionsProps{Title: "Guidance"}, Support: InformationalPanelProps{Title: "Support"}}),
		ui.CreateElement(SettingsPage, SettingsPageProps{Access: AccessContextProps{Title: "Access", Facts: []FactProps{{Label: "Principal", Value: "Taylor"}}}, Preferences: EmptyStateProps{Title: "Preferences"}}),
		ui.CreateElement(StudioPage, StudioPageProps{Back: ActionLinkProps{Label: "Back", Href: "/admin"}, State: EmptyStateProps{Title: "Unavailable"}}),
	}
	for index, node := range nodes {
		if _, err := ui.RenderToString(node); err != nil {
			t.Fatalf("composition %d did not render independently: %v", index, err)
		}
	}
}
