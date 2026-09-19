package productclient

import (
	"context"
	"errors"
	"strings"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestOpenWorkCountExcludesTerminalJourneys(t *testing.T) {
	items := []productui.WorkItem{
		{ID: "awaiting"},
		{ID: "blocked"},
		{ID: "complete", Terminal: true},
		{ID: "rejected", Terminal: true},
	}
	if got := len(productui.OpenWorkItems(items)); got != 2 {
		t.Fatalf("len(OpenWorkItems) = %d, want 2", got)
	}
}

func TestTodo_UIPOLISH_009_LoadingHomeTitleDoesNotFlashPrincipalIdentifier(t *testing.T) {
	for _, tc := range []struct{ locale, want string }{{"en-US", "Home"}, {"de-DE", "Start"}, {"ar", "الرئيسية"}} {
		view := LoadingView(Session{Principal: "hc-050-rafael-torres"}, State{Page: productui.PageHome, Request: productui.PageRequest{Page: productui.PageHome, Locale: tc.locale}})
		if view.Title != tc.want || strings.Contains(view.Title, "Hc 050") {
			t.Fatalf("%s loading Home title = %q; want stable registry title %q", tc.locale, view.Title, tc.want)
		}
	}
}

func TestRecordedJourneyLeavesOpenWorkAndAllKnownStagesHaveLabels(t *testing.T) {
	items, err := projectJourneys([]*journeyv1.Journey{{IntentId: "recorded", Stage: journeyv1.JourneyStage_JOURNEY_STAGE_RECORDED,
		Viewer: &journeyv1.JourneyViewerProjection{Closed: true, Responsibility: journeyv1.JourneyViewerResponsibility_JOURNEY_VIEWER_RESPONSIBILITY_CLOSED}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || !items[0].Terminal || len(productui.OpenWorkItems(items)) != 0 {
		t.Fatal("recorded promotion incorrectly remains open")
	}
	if items[0].TitleKey != "journey.detail_title" || items[0].StatusKey != "journey.stage_recorded" {
		t.Fatalf("recorded promotion lost its locale-independent display identity: %+v", items[0])
	}
	for number, name := range journeyv1.JourneyStage_name {
		if number == 0 {
			continue
		}
		label, _ := journeyclient.StagePresentation(journeyv1.JourneyStage(number))
		if label == "Status unavailable" {
			t.Errorf("known stage has no presentation: %s", name)
		}
		key := journeyStageKey(journeyv1.JourneyStage(number))
		for _, locale := range productui.SupportedProductLocales() {
			translated := productui.ResolveProductLocale(locale).Text(key)
			if key == "" || translated == "" || strings.Contains(translated, "⟦") {
				t.Errorf("%s stage %s has no translated semantic key %q: %q", locale, name, key, translated)
			}
		}
	}
}

func TestProjectJourneysCarriesViewerScopedAssignmentWithoutOtherPrincipal(t *testing.T) {
	items, err := projectJourneys([]*journeyv1.Journey{{
		IntentId: "journey-finance", WorkerRef: "worker-omar", WorkerName: "Omar",
		Stage:  journeyv1.JourneyStage_JOURNEY_STAGE_FINANCE_APPROVAL,
		Viewer: &journeyv1.JourneyViewerProjection{Relationships: []journeyv1.JourneyViewerRelationship{journeyv1.JourneyViewerRelationship_JOURNEY_VIEWER_RELATIONSHIP_ASSIGNEE}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].PersonRef != "worker-omar" || items[0].AssigneeRef != "" || len(items[0].ViewerRelationships) != 1 {
		t.Fatalf("projected assignment = %+v", items)
	}
}

func TestUXBLIND014RoleFilterSurvivesCanonicalization(t *testing.T) {
	state, err := ParseState("/workspace/app/admin/roles", "locale=en-US&q=Rafael")
	if err != nil {
		t.Fatal(err)
	}
	if state.Request.Query != "Rafael" || CanonicalHref(state) != "/workspace/app/admin/roles?locale=en-US&q=Rafael" {
		t.Fatalf("role query lost: %+v, %s", state.Request, CanonicalHref(state))
	}
}

func TestContentLoadingViewRetargetsRouteWithoutDiscardingAuthorizedShell(t *testing.T) {
	previous := productui.NewView(productui.PageHome, "HarborCare Demo", "Rafael Torres", "admin")
	previous.Viewer = productui.ViewerProfile{Name: "Rafael Torres", PhotoURL: "/workspace/assets/rafael-small.jpg"}
	previous.Work = []productui.WorkItem{{ID: "work-live", Title: "Review promotion"}}
	previous.Navigation = []productui.NavItem{{Page: productui.PageHome}, {Page: productui.PageWork}}

	view := ContentLoadingView(previous, State{Page: productui.PageWork, Request: productui.PageRequest{
		Page: productui.PageWork, WorkFilter: "review", NavCollapsed: true,
	}})

	if view.Page != productui.PageWork || view.Title != "My Work" || !view.ContentLoading || view.Loading || view.Refreshing {
		t.Fatalf("route transition state = %+v", view)
	}
	if view.Viewer.Name != "Rafael Torres" || view.Viewer.PhotoURL == "" || len(view.Navigation) != 2 {
		t.Fatalf("authorized shell projection was discarded: %+v", view)
	}
	if !view.NavCollapsed || view.WorkFilter != "review" {
		t.Fatalf("destination address state was not applied: %+v", view)
	}
}

func TestTodo_UXAUDIT_003_RevokedLauncherProjectionIsNotReused(t *testing.T) {
	permissions := []productui.RolePagePermission{{Page: productui.PageHome, View: true}, {Page: productui.PageSettings, View: true}}
	baselineSession := Session{
		Tenant: "harborcare", Principal: "worker-1", Roles: []string{"worker_self"}, Permissions: permissions,
		LauncherActions: []productui.LauncherActionProjection{{
			ID: productui.SemanticActionPromoteWorker, State: productui.ActionState{Availability: productui.ActionAvailable},
		}},
	}
	baseline := LoadingView(baselineSession, State{Page: productui.PageHome, Request: productui.PageRequest{Page: productui.PageHome}})
	if len(baseline.LauncherActions) != 1 {
		t.Fatal("test baseline has no semantic action")
	}
	current, err := LoadWithBaseline(context.Background(), Service{}, Session{
		Tenant: "harborcare", Principal: "worker-1", Roles: []string{"worker_self"}, Permissions: permissions,
	}, State{Page: productui.PageSettings, Request: productui.PageRequest{Page: productui.PageSettings}}, baseline)
	if err != nil {
		t.Fatal(err)
	}
	if len(current.LauncherActions) != 0 {
		t.Fatalf("revoked semantic action was resurrected from baseline: %+v", current.LauncherActions)
	}
}

func TestConcisePlacementLabelRemovesDuplicatedSourceCodes(t *testing.T) {
	if got := concisePlacementLabel("OPS-HRBP3OPS-HRBP3", "P3"); got != "OPS-HRBP3 P3" {
		t.Fatalf("concisePlacementLabel = %q, want OPS-HRBP3 P3", got)
	}
	if got := concisePlacementLabel("OPS-HRBP3OPS-HRBP3OPS-HRBP3", "P3"); got != "OPS-HRBP3 P3" {
		t.Fatalf("triple source-code repetition = %q, want OPS-HRBP3 P3", got)
	}
	if got := concisePlacementLabel("ENG-SWE3", "P3"); got != "ENG-SWE3 P3" {
		t.Fatalf("ordinary placement changed to %q", got)
	}
}

func TestManagerProjectionDoesNotInferFromTechnicalRelationshipReferences(t *testing.T) {
	worker := &journeyv1.Worker{WorkerRef: "worker-live", ManagerRef: "rel-mgr-01a07058"}
	if _, _, _, err := projectManagerRelationship(worker, map[string]int{"worker-live": 1}, nil); err == nil {
		t.Fatal("missing typed relationship projection was inferred from the opaque reference")
	}
	worker.ManagerRelationship = &journeyv1.ManagerRelationshipProjection{Disposition: journeyv1.ManagerRelationshipProjection_DISPOSITION_WITHHELD}
	state, ref, label, err := projectManagerRelationship(worker, map[string]int{"worker-live": 1}, nil)
	if err != nil || state != productui.OrganizationRelationshipWithheld || ref != "" || label != "" {
		t.Fatalf("withheld projection = %q/%q/%q err=%v", state, ref, label, err)
	}
}

func TestTodo_UXAUDIT_004_ClientUsesOnlyAuthorizedStableManagerEdges(t *testing.T) {
	workers := []*journeyv1.Worker{
		{WorkerRef: "manager-a", WorkerId: "manager-id", PreferredName: "Alex Morgan", ManagerRelationship: rootManagerRelationship()},
		{WorkerRef: "manager-b", WorkerId: "other-manager-id", PreferredName: "Alex Morgan", ManagerRelationship: rootManagerRelationship()},
		{WorkerRef: "report", WorkerId: "report-id", PreferredName: "Casey Lee", ManagerRef: "opaque-relationship", ManagerRelationship: &journeyv1.ManagerRelationshipProjection{
			Disposition: journeyv1.ManagerRelationshipProjection_DISPOSITION_VISIBLE, ManagerWorkerRef: "manager-a",
		}},
		{WorkerRef: "withheld", PreferredName: "Taylor", ManagerRef: "secret-manager", ManagerRelationship: &journeyv1.ManagerRelationshipProjection{Disposition: journeyv1.ManagerRelationshipProjection_DISPOSITION_WITHHELD}},
	}
	people, err := projectWorkers(workers)
	if err != nil {
		t.Fatal(err)
	}
	if people[2].ManagerWorkerRef != "manager-a" || people[2].Manager != "Alex Morgan" || people[2].ManagerRelationship != productui.OrganizationRelationshipVisible {
		t.Fatalf("stable duplicate-name relationship = %+v", people[2])
	}
	if people[3].Manager != "" || people[3].ManagerWorkerRef != "" || people[3].ManagerRelationship != productui.OrganizationRelationshipWithheld {
		t.Fatalf("withheld relationship leaked through the client: %+v", people[3])
	}

	workers[2].ManagerRelationship.ManagerWorkerRef = "not-returned"
	if _, err := projectWorkers(workers); err == nil {
		t.Fatal("client admitted a VISIBLE relationship whose endpoint was absent")
	}
	workers[2].ManagerRelationship.ManagerWorkerRef = "manager-a"
	workers = append(workers, &journeyv1.Worker{WorkerRef: "manager-a", PreferredName: "Imposter", ManagerRelationship: rootManagerRelationship()})
	if _, err := projectWorkers(workers); err == nil {
		t.Fatal("client admitted an ambiguous VISIBLE manager endpoint")
	}
}

func TestTodo_UXAUDIT_004_OrganizationAddressStatePreservesSelectionAndFilter(t *testing.T) {
	for _, page := range []productui.PageID{productui.PageOrganization, productui.PageOrgExplorer, productui.PageOrgOutline, productui.PageOrgResponsive} {
		t.Run(string(page), func(t *testing.T) {
			state, err := ParseState(productui.Path(page), "org_view=tree&q=casey&person=worker-casey&nav=collapsed")
			if err != nil {
				t.Fatal(err)
			}
			if state.Request.OrganizationView != "tree" || state.Request.Query != "casey" || state.Request.SelectedPerson != "worker-casey" {
				t.Fatalf("organization state = %+v", state.Request)
			}
			want := productui.Path(page) + "?nav=collapsed&org_view=tree&person=worker-casey&q=casey"
			if got := CanonicalHref(state); got != want {
				t.Fatalf("organization canonical href = %q, want %q", got, want)
			}
		})
	}
}

func TestTodo_UXAUDIT_004_OutlineCanonicalHrefForcesTree(t *testing.T) {
	for _, raw := range []string{"", "org_view=flat", "org_view=tree", "org_view=flat&q=casey&person=worker-casey&nav=collapsed"} {
		state, err := ParseState(productui.Path(productui.PageOrgOutline), raw)
		if err != nil {
			t.Fatalf("ParseState(%q): %v", raw, err)
		}
		got := CanonicalHref(state)
		if !strings.Contains(got, "org_view=tree") || strings.Contains(got, "org_view=flat") {
			t.Errorf("CanonicalHref(%q) = %q", raw, got)
		}
		view := productui.ApplyRequest(productui.NewView(productui.PageOrgOutline, "tenant", "principal", "scope"), state.Request)
		resolved := ResolvedCanonicalHref(state, view)
		if !strings.Contains(resolved, "org_view=tree") || strings.Contains(resolved, "org_view=flat") {
			t.Errorf("ResolvedCanonicalHref(%q) = %q", raw, resolved)
		}
	}
}

func TestLoadProjectsOnlyLiveServiceAnswers(t *testing.T) {
	service := Service{
		ListJourneys: func(context.Context, *journeyv1.ListJourneysRequest) (*journeyv1.ListJourneysResponse, error) {
			return &journeyv1.ListJourneysResponse{Journeys: []*journeyv1.Journey{{
				IntentId: "intent-live", WorkerName: "Riley Chen", EffectiveDate: "2026-10-01",
				InstanceId: "instance-live", InstanceVersion: 7, MaterialDigest: "sha256:proposal-live",
				Current: &journeyv1.Placement{JobCode: "ENG2", Grade: "G6"},
				Target:  &journeyv1.Placement{JobCode: "ENG3", Grade: "G7"},
				Stage:   journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_APPROVAL,
				// PROMOUX-012: the badge counts work the server says the
				// viewer must act on.
				Viewer: &journeyv1.JourneyViewerProjection{Responsibility: journeyv1.JourneyViewerResponsibility_JOURNEY_VIEWER_RESPONSIBILITY_ACTION_REQUIRED},
			}}}, nil
		},
		ListWorkers: func(context.Context, *journeyv1.ListWorkersRequest) (*journeyv1.ListWorkersResponse, error) {
			return &journeyv1.ListWorkersResponse{Workers: []*journeyv1.Worker{{
				WorkerRef: "worker-live", WorkerId: "worker-id-live", LegalName: "Riley Morgan Chen", PreferredName: "Riley Chen", WorkerNumber: "NW-9", JobCode: "ENG2", Grade: "G6", OrgUnit: "Engineering",
				PositionId: "pos-9", PayZone: "US-1", BasePay: "120000", Currency: "USD", BonusTarget: "0.10", HireDate: "2020-02-03", Source: "CREATED",
				JobTitle: "Senior Software Engineer", ManagerRef: "manager-live", ProfilePhotoUrl: "/workspace/assets/person-live-small.jpg",
				ManagerRelationship: &journeyv1.ManagerRelationshipProjection{Disposition: journeyv1.ManagerRelationshipProjection_DISPOSITION_ORPHAN},
			}}}, nil
		},
	}
	view, err := Load(context.Background(), service, Session{Tenant: "tenant-live", Principal: "Riley", Scope: "manager"}, State{Page: productui.PageHome, Request: productui.PageRequest{Page: productui.PageHome}})
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Work) != 1 || view.Work[0].ID != "intent-live" || len(view.People) != 1 || view.People[0].ID != "worker-live" {
		t.Fatalf("projection did not use server answers: %+v", view)
	}
	if view.Work[0].InstanceID != "instance-live" || view.Work[0].InstanceVersion != 7 || view.Work[0].MaterialDigest != "sha256:proposal-live" {
		t.Fatalf("durable history provenance was not preserved: %+v", view.Work[0])
	}
	if view.Title != "Good morning, Riley." || workNavigationCount(view.Navigation) != 1 {
		t.Fatalf("session/count projection = %+v", view)
	}
	person := view.People[0]
	if person.WorkerID != "worker-id-live" || person.LegalName != "Riley Morgan Chen" || person.PreferredName != "Riley Chen" || person.WorkerNumber != "NW-9" || person.PositionID != "pos-9" ||
		person.BasePay.Amount().String() != "120000" || person.BasePay.Currency() != "USD" || person.Source != "CREATED" {
		t.Fatalf("worker detail projection lost live facts: %+v", person)
	}
	if person.Role != "Senior Software Engineer · G6" || person.Manager != "" || person.ManagerRelationship != productui.OrganizationRelationshipOrphan || person.PhotoURL != "/workspace/assets/person-live-small.jpg" {
		t.Fatalf("worker display projection lost title, manager, or photo: %+v", person)
	}
	profile, err := Load(context.Background(), service, Session{Tenant: "tenant-live", Principal: "Riley", Scope: "manager"}, State{
		Page: productui.PagePerson, Request: productui.PageRequest{Page: productui.PagePerson, SelectedPerson: "worker-live"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(profile.RecordVerdicts) != 0 || productui.ResolveDocumentPageTitle(profile) != "Riley Chen · NW-9" {
		t.Fatalf("live ListWorkers profile title = %q with verdicts %v", productui.ResolveDocumentPageTitle(profile), profile.RecordVerdicts)
	}
}

func TestMoneyProjectionRejectsFloatLikeAndCurrencylessAmounts(t *testing.T) {
	for _, tc := range []struct {
		name, amount, currency string
	}{
		{name: "binary-float spelling", amount: "1e3", currency: "USD"},
		{name: "non-finite", amount: "NaN", currency: "USD"},
		{name: "missing currency", amount: "1000.00"},
		{name: "noncanonical currency", amount: "1000.00", currency: "usd"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := moneyFromWire(tc.amount, tc.currency); err == nil {
				t.Fatalf("moneyFromWire(%q, %q) accepted a non-money value", tc.amount, tc.currency)
			}
		})
	}
}

func TestMoneyProjectionPreservesExactDecimalScale(t *testing.T) {
	money, err := moneyFromWire("9007199254740993.01", "USD")
	if err != nil {
		t.Fatal(err)
	}
	if got := money.Amount().String(); got != "9007199254740993.01" {
		t.Fatalf("exact amount = %q, want the cent above float64's safe integer range", got)
	}
}

func TestLoadNeverSubstitutesFixturesOnFailure(t *testing.T) {
	view, err := Load(context.Background(), Service{
		ListJourneys: func(context.Context, *journeyv1.ListJourneysRequest) (*journeyv1.ListJourneysResponse, error) {
			return nil, errors.New("offline")
		},
		ListWorkers: func(context.Context, *journeyv1.ListWorkersRequest) (*journeyv1.ListWorkersResponse, error) {
			return nil, errors.New("offline")
		},
	}, Session{}, State{Page: productui.PageHome})
	if err == nil || len(view.Work) != 0 || len(view.People) != 0 {
		t.Fatalf("failed live load invented records: view=%+v err=%v", view, err)
	}
}

func TestParseStateUsesProductionRoutes(t *testing.T) {
	state, err := ParseState("/workspace/app/work", "nav=collapsed&selected=intent-1&page=3&menu_q=work&favorites=history,people,history&locale=de-DE")
	if err != nil {
		t.Fatal(err)
	}
	if state.Page != productui.PageWork || !state.Request.NavCollapsed || state.Request.SelectedWork != "intent-1" || state.Request.PeoplePage != 1 ||
		state.Request.MenuQuery != "work" || state.Request.Locale != "de-DE" || len(state.Request.FavoritePages) != 2 || state.Request.FavoritePages[0] != productui.PageHistory {
		t.Fatalf("state = %+v", state)
	}
	if _, err := ParseState("/app/work", ""); err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("legacy mock route unexpectedly accepted: %v", err)
	}
	if _, err = ParseState("/workspace/app/people", "page=invalid"); err == nil {
		t.Fatal("invalid people page was accepted as canonical route state")
	}
	state, err = ParseState("/workspace/app/people", "q=engineer&team=Platform&location=Boston&sort=location&dir=desc&page=2")
	if err != nil || state.Request.Query != "engineer" || state.Request.PeopleTeam != "Platform" || state.Request.PeopleLocation != "Boston" ||
		state.Request.PeopleSort != "location" || state.Request.PeopleDirection != "desc" || state.Request.PeoplePage != 2 {
		t.Fatalf("people directory state was not preserved: state=%+v err=%v", state, err)
	}
	state, err = ParseState("/workspace/app/history", "history_q=Avery&outcome=completed&history_person=worker-avery&history_year=2026&history_sort=person&history_dir=asc&nav=collapsed")
	if err != nil || state.Page != productui.PageHistory || state.Request.HistoryQuery != "Avery" || state.Request.HistoryOutcome != "completed" ||
		state.Request.HistoryPerson != "worker-avery" || state.Request.HistoryYear != "2026" || state.Request.HistorySort != "person" ||
		state.Request.HistoryDirection != "asc" || !state.Request.NavCollapsed {
		t.Fatalf("history route state was not preserved: state=%+v err=%v", state, err)
	}
}

func TestPromotionEligibilityProjectsPublishedChoices(t *testing.T) {
	state, err := ParseState("/workspace/app/people", "")
	if err != nil {
		t.Fatal(err)
	}
	options := &journeyv1.WorkforceOptions{
		Currency:       "USD",
		Placements:     []*journeyv1.WorkforcePlacementOption{{JobCode: "ENG2", Grade: "P2", PayZone: "US", Currency: "USD"}},
		PromotionPaths: []*journeyv1.PromotionPathOption{{SourceJobCode: "ENG1", SourceGrade: "P1", TargetJobCode: "ENG2", TargetGrade: "P2"}},
	}
	service := Service{
		ListJourneys: func(context.Context, *journeyv1.ListJourneysRequest) (*journeyv1.ListJourneysResponse, error) {
			return &journeyv1.ListJourneysResponse{}, nil
		},
		ListWorkers: func(context.Context, *journeyv1.ListWorkersRequest) (*journeyv1.ListWorkersResponse, error) {
			return &journeyv1.ListWorkersResponse{Options: options, Workers: []*journeyv1.Worker{
				{WorkerRef: "eligible", JobCode: "ENG1", Grade: "P1", PayZone: "US", Currency: "USD", ManagerRelationship: rootManagerRelationship()},
				{WorkerRef: "no-path", JobCode: "SALES1", Grade: "P1", PayZone: "US", Currency: "USD", ManagerRelationship: rootManagerRelationship()},
			}}, nil
		},
	}
	view, err := Load(context.Background(), service, Session{}, state)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.People) != 2 ||
		view.People[0].PromotionAvailability != productui.PromotionEligible ||
		view.People[1].PromotionAvailability != productui.PromotionIneligible {
		t.Fatalf("published eligibility not preserved: %+v", view.People)
	}
}

func TestPersonRouteProjectsOnlySupportedWorkflowForSelectedWorker(t *testing.T) {
	state, err := ParseState("/workspace/app/person", "person=worker-live&workflow_q=promo&nav=collapsed")
	if err != nil {
		t.Fatal(err)
	}
	if state.Page != productui.PagePerson || state.Request.SelectedPerson != "worker-live" || state.Request.WorkflowQuery != "promo" || !state.Request.NavCollapsed {
		t.Fatalf("person route state = %+v", state)
	}
	view, err := Load(context.Background(), Service{
		ListJourneys: func(context.Context, *journeyv1.ListJourneysRequest) (*journeyv1.ListJourneysResponse, error) {
			return &journeyv1.ListJourneysResponse{}, nil
		},
		ListWorkers: func(context.Context, *journeyv1.ListWorkersRequest) (*journeyv1.ListWorkersResponse, error) {
			return &journeyv1.ListWorkersResponse{Workers: []*journeyv1.Worker{{WorkerRef: "worker-live", PreferredName: "Riley Chen", ManagerRelationship: rootManagerRelationship()}}}, nil
		},
	}, Session{}, state)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.PersonWorkflows) != 1 || view.PersonWorkflows[0].ID != "promotion" || view.PersonWorkflows[0].Href != "/workspace/app/journeys?mode=new&nav=collapsed&worker=worker-live" {
		t.Fatalf("person workflow projection = %+v", view.PersonWorkflows)
	}
}

func rootManagerRelationship() *journeyv1.ManagerRelationshipProjection {
	return &journeyv1.ManagerRelationshipProjection{Disposition: journeyv1.ManagerRelationshipProjection_DISPOSITION_ROOT}
}

func TestSessionTokensBecomeReadableLabelsWithoutChangingRPCState(t *testing.T) {
	if got := displayLabel("compensation_review"); got != "Compensation Review" {
		t.Fatalf("display label = %q", got)
	}
	if got := displayLabel("local-developer"); got != "Local Developer" {
		t.Fatalf("principal label = %q", got)
	}
}

func TestOrganizationLabelsReconcileLegacyAliasesAndPunctuation(t *testing.T) {
	for code, want := range map[string]string{
		"eng-platform":         "Engineering Platform",
		"engineering-platform": "Engineering Platform",
		"people-ops":           "People Operations",
		"people-operations":    "People Operations",
		"data-analytics":       "Data & Analytics",
		"security-it":          "Security & IT",
	} {
		if got := orgUnitLabel(code); got != want {
			t.Errorf("orgUnitLabel(%q) = %q, want %q", code, got, want)
		}
	}
}

func TestPromotionWorkflowHrefEscapesReservedWorkerReferences(t *testing.T) {
	workflows := projectPersonWorkflows(productui.NewView(productui.PagePerson, "", "", ""), "worker/a+b & c")
	if len(workflows) != 1 || workflows[0].Href != "/workspace/app/journeys?mode=new&worker=worker%2Fa%2Bb+%26+c" {
		t.Fatalf("workflow hrefs = %+v", workflows)
	}
}

func workNavigationCount(items []productui.NavItem) int {
	for _, item := range items {
		if item.Page == productui.PageWork {
			return item.Count
		}
	}
	return -1
}

func TestEmployeePhotoURLUsesKnownIdentityAndUnknownFallback(t *testing.T) {
	for _, test := range []struct {
		ref, name, want string
	}{
		{"eref:v1:demo:worker:444", "Noor Haddad", "/workspace/assets/person-noor-small.jpg"},
		{"priya-01a07058", "", "/workspace/assets/person-priya-small.jpg"},
		{"worker-live", "Riley Chen", ""},
	} {
		if got := employeePhotoURL(test.ref, test.name); got != test.want {
			t.Errorf("employeePhotoURL(%q, %q) = %q, want %q", test.ref, test.name, got, test.want)
		}
	}
}

// TestFinanceApproverMyWorkLoadsWithoutTheDirectory: a finance approver can
// see My Work but neither Journeys nor the People directory. The client
// skipped the journey read (it only asked whether Journeys was visible),
// failed the whole page on the directory's PermissionDenied, and -- with no
// directory to resolve the viewer -- left the routed review out of My Work.
func TestFinanceApproverMyWorkLoadsWithoutTheDirectory(t *testing.T) {
	if !productui.PageVisible(productui.PageWork, []string{"finance_partner"}) {
		t.Skip("finance_partner no longer sees My Work; this scenario no longer exists")
	}
	state, err := ParseState("/workspace/app/work", "")
	if err != nil {
		t.Fatal(err)
	}
	routed := &journeyv1.Journey{
		IntentId: "intent-finance", WorkerRef: "worker-amara", WorkerName: "Amara",
		Stage: journeyv1.JourneyStage_JOURNEY_STAGE_FINANCE_APPROVAL, Currency: "USD",
		CurrentBase: "146000.00", ProposedBase: "170000.00",
		CurrentWorkItem: &journeyv1.JourneyWorkItemSummary{AssigneePrincipalId: "hc-054-thomas-baker"},
		Viewer:          &journeyv1.JourneyViewerProjection{Responsibility: journeyv1.JourneyViewerResponsibility_JOURNEY_VIEWER_RESPONSIBILITY_ACTION_REQUIRED},
	}
	load := func(principal string) (productui.View, bool) {
		journeysRead := false
		view, err := Load(context.Background(), Service{
			ListJourneys: func(context.Context, *journeyv1.ListJourneysRequest) (*journeyv1.ListJourneysResponse, error) {
				journeysRead = true
				return &journeyv1.ListJourneysResponse{Journeys: []*journeyv1.Journey{routed}}, nil
			},
			ListWorkers: func(context.Context, *journeyv1.ListWorkersRequest) (*journeyv1.ListWorkersResponse, error) {
				return nil, status.Error(codes.PermissionDenied, "the assigned role does not permit this action on the page feature")
			},
		}, Session{Principal: principal, EnforceRoleVisibility: true, Roles: []string{"finance_partner"}}, state)
		if err != nil {
			t.Fatalf("My Work failed for %s: %v", principal, err)
		}
		return view, journeysRead
	}
	view, journeysRead := load("hc-054-thomas-baker")
	if !journeysRead {
		t.Fatal("My Work never read the journeys its queue is built from")
	}
	if len(view.People) != 0 {
		t.Fatalf("a denied directory read produced people: %+v", view.People)
	}
	if view.Viewer.PersonID != "hc-054-thomas-baker" || len(productui.MyWorkItems(view.Work, view.Viewer)) != 1 {
		t.Fatalf("the routed approver's queue = viewer %+v, work %+v", view.Viewer, view.Work)
	}
	// An account nothing is routed to stays unbound (UXAUDIT-017).
	if other, _ := load("local-operator"); other.Viewer.PersonID != "" {
		t.Fatalf("an unrouted account was bound to %q", other.Viewer.PersonID)
	}
}
