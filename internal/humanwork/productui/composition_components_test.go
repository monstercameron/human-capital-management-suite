package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestSearchInputKeepsPageFiltersAccessibleWithoutOwningTheirForms(t *testing.T) {
	for _, test := range []struct {
		name        string
		props       SearchInputProps
		want        []string
		withoutAria bool
	}{
		{
			name: "named query", props: SearchInputProps{ID: "history-search", Name: "history_q", Value: "promotion", Placeholder: "Search history", AriaLabel: "Search workflow history"},
			want: []string{`id="history-search"`, `name="history_q"`, `value="promotion"`, `type="search"`, `placeholder="Search history"`, `aria-label="Search workflow history"`},
		},
		{
			name: "externally labeled query", props: SearchInputProps{ID: "role-directory-query", Name: "q", Placeholder: "Find an employee"},
			want: []string{`id="role-directory-query"`, `name="q"`, `type="search"`}, withoutAria: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			markup, err := ui.RenderToString(ui.CreateElement(SearchInput, test.props))
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range test.want {
				if !strings.Contains(markup, want) {
					t.Fatalf("search input missing %q: %s", want, markup)
				}
			}
			if test.withoutAria && strings.Contains(markup, "aria-label=") {
				t.Fatalf("search input duplicated its external label: %s", markup)
			}
			if strings.Contains(markup, "oninput=") || strings.Contains(markup, "<form") {
				t.Fatalf("search input leaked form or inline handler markup: %s", markup)
			}
		})
	}
}

func TestLabeledControlPreservesNativeControlsAndOptionalHelp(t *testing.T) {
	for _, test := range []struct {
		name  string
		props LabeledControlProps
		want  []string
	}{
		{name: "text with help", props: LabeledControlProps{For: "workspace-name", Label: "Workspace name", Control: html.Input(html.Props{ID: "workspace-name", Name: "brand_name", Type: "text"}), Help: "Shown to employees"},
			want: []string{`<label for="workspace-name">`, `<span>Workspace name</span>`, `id="workspace-name"`, `name="brand_name"`, `<small>Shown to employees</small>`}},
		{name: "textarea without help", props: LabeledControlProps{For: "role-description", Label: "Description", Control: html.Textarea(html.Props{ID: "role-description", Name: "description"})},
			want: []string{`<label for="role-description">`, `<span>Description</span>`, `<textarea`, `id="role-description"`}},
		{name: "select without help", props: LabeledControlProps{For: "separator", Label: "Separator", Control: html.Select(html.Props{ID: "separator"}, html.Option(html.Props{Value: "-"}, ui.Text("Dash")))},
			want: []string{`<label for="separator">`, `<span>Separator</span>`, `<select`, `id="separator"`}},
	} {
		t.Run(test.name, func(t *testing.T) {
			markup, err := ui.RenderToString(ui.CreateElement(LabeledControl, test.props))
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range test.want {
				if !strings.Contains(markup, want) {
					t.Fatalf("labeled control missing %q: %s", want, markup)
				}
			}
			if test.props.Help == "" && strings.Contains(markup, "<small") {
				t.Fatalf("labeled control emitted empty help: %s", markup)
			}
		})
	}
}

func TestSectionHeadingPreservesSimpleAndDescribedSurfaceShapes(t *testing.T) {
	for _, test := range []struct {
		name  string
		props SectionHeadingProps
		want  []string
	}{
		{
			name: "simple panel", props: SectionHeadingProps{Title: "Queue"},
			want: []string{`<div class="section-head"><h2>Queue</h2></div>`},
		},
		{
			name: "described section with action", props: SectionHeadingProps{
				ID: "journeys-title", Title: "Journeys", Description: "Governed work", Level: 2,
				Trailing: html.Span(html.Props{Class: "count"}, ui.Text("7")),
			},
			want: []string{`<div class="section-head"><div><h2 id="journeys-title">Journeys</h2><p class="muted">Governed work</p></div><span class="count">7</span></div>`},
		},
		{
			name: "settings subheading", props: SectionHeadingProps{ID: "accessibility-title", Title: "Accessibility", Description: "Your preferences", Level: 3},
			want: []string{`<h3 id="accessibility-title">Accessibility</h3>`, `<p class="muted">Your preferences</p>`},
		},
		{
			name: "identified heading without description", props: SectionHeadingProps{ID: "profile-title", Title: "Profile"},
			want: []string{`<div class="section-head"><div><h2 id="profile-title">Profile</h2></div></div>`},
		},
		{
			name: "reserved description slot", props: SectionHeadingProps{Title: "Active work", ShowDescription: true},
			want: []string{`<div class="section-head"><div><h2>Active work</h2><p class="muted"></p></div></div>`},
		},
		{
			name: "history class and count", props: SectionHeadingProps{
				ID: "workflow-history-title", Title: "History", Class: "history-heading", ShowDescription: true,
				Trailing: html.Span(html.Props{Class: "count"}, ui.Text("10 records")),
			},
			want: []string{`<div class="section-head history-heading">`, `<h2 id="workflow-history-title">History</h2>`, `<p class="muted"></p>`, `<span class="count">10 records</span>`},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			markup, err := ui.RenderToString(ui.CreateElement(SectionHeading, test.props))
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range test.want {
				if !strings.Contains(markup, want) {
					t.Fatalf("section heading missing %q: %s", want, markup)
				}
			}
		})
	}
}

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

func TestPlainUnavailableSurfacesShareAccessibleEmptyState(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	for _, test := range []struct {
		name, titleKey, detailKey, actionKey, href string
		node                                       ui.Node
	}{
		{
			name: "person", titleKey: "person.unavailable", detailKey: "person.unavailable_detail", actionKey: "person.return_directory", href: "/workspace/app/people",
			node: ui.CreateElement(PersonUnavailable, PersonUnavailableProps{I18nProps: I18nProps{Locale: locale}, DirectoryHref: "/workspace/app/people"}),
		},
		{
			name: "people", titleKey: "people.empty_title", detailKey: "people.empty_detail", actionKey: "people.clear_filter", href: "/workspace/app/people?q=",
			node: ui.CreateElement(PeopleEmptyState, PeopleEmptyStateProps{I18nProps: I18nProps{Locale: locale}, ClearHref: "/workspace/app/people?q="}),
		},
		{
			name: "myself", titleKey: "myself.unavailable_title", detailKey: "myself.unavailable_detail",
			node: ui.CreateElement(MyselfPage, MyselfPageProps{I18nProps: I18nProps{Locale: locale}}),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			markup, err := ui.RenderToString(test.node)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{`class="surface empty-state"`, `role="status"`, `aria-atomic="true"`, locale.Text(test.titleKey), locale.Text(test.detailKey)} {
				if !strings.Contains(markup, want) {
					t.Fatalf("unavailable surface missing %q: %s", want, markup)
				}
			}
			if test.href == "" {
				if strings.Contains(markup, "<a ") {
					t.Fatalf("unbound self-service view offered a navigation action: %s", markup)
				}
				return
			}
			for _, want := range []string{`class="button secondary"`, `href="` + test.href + `"`, locale.Text(test.actionKey)} {
				if !strings.Contains(markup, want) {
					t.Fatalf("unavailable surface lost its recovery action %q: %s", want, markup)
				}
			}
		})
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

func TestVisibilityPageSharesRecoveryEmptyStateWhenNoRolesAreActive(t *testing.T) {
	props := OrganizationVisibilityPageProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")},
		RolesLink: ActionLinkProps{Label: "Manage roles", Href: "/workspace/app/admin/roles", Class: "button secondary"},
	}
	markup, err := ui.RenderToString(ui.CreateElement(OrganizationVisibilityPage, props))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`class="surface empty-state"`, props.Text("organization_visibility.no_roles"), props.Text("organization_visibility.no_roles_detail"), `href="/workspace/app/admin/roles"`, "Manage roles"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("empty visibility page missing %q: %s", want, markup)
		}
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
