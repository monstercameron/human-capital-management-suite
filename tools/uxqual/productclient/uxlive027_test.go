package productclient

import (
	"context"
	"fmt"
	"html"
	"regexp"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// UXLIVE-027 / UXLIVE-030: Journeys, Insights and Home read one authorized
// journey population. The fixture is the RED session's shape: nine visible
// promotion requests for one viewer, across open, closed and exception
// stages, with the server's population summary over exactly those nine.

const uxlive027Viewer = "worker-rafael"

func uxlive027Journey(id string, stage journeyv1.JourneyStage, responsibility journeyv1.JourneyViewerResponsibility, initiator, closed bool) *journeyv1.Journey {
	viewer := &journeyv1.JourneyViewerProjection{Responsibility: responsibility, Closed: closed}
	if initiator {
		viewer.Relationships = []journeyv1.JourneyViewerRelationship{journeyv1.JourneyViewerRelationship_JOURNEY_VIEWER_RELATIONSHIP_INITIATOR}
	}
	if !closed {
		viewer.NextStep = journeyv1.JourneyNextStep_JOURNEY_NEXT_STEP_APPROVAL_DECISION
		viewer.AwaitsPerson = true
	}
	return &journeyv1.Journey{
		IntentId: id, WorkerRef: "worker-" + id, WorkerName: "Worker " + strings.ToUpper(id), EffectiveDate: "2026-10-01",
		Stage: stage, Viewer: viewer, UpdatedAt: timestamppb.New(time.Date(2026, 9, 12, 8, 0, 0, 0, time.UTC)),
		Current: &journeyv1.Placement{JobCode: "OPS-HRBP2", Grade: "P2"}, Target: &journeyv1.Placement{JobCode: "OPS-HRBP3", Grade: "P3"},
	}
}

// uxlive027Response is the nine-journey answer plus the server summary that
// transport.toPopulation computes over it.
func uxlive027Response() *journeyv1.ListJourneysResponse {
	action := journeyv1.JourneyViewerResponsibility_JOURNEY_VIEWER_RESPONSIBILITY_ACTION_REQUIRED
	tracking := journeyv1.JourneyViewerResponsibility_JOURNEY_VIEWER_RESPONSIBILITY_TRACKING
	observing := journeyv1.JourneyViewerResponsibility_JOURNEY_VIEWER_RESPONSIBILITY_OBSERVING
	closed := journeyv1.JourneyViewerResponsibility_JOURNEY_VIEWER_RESPONSIBILITY_CLOSED
	journeys := []*journeyv1.Journey{
		uxlive027Journey("j1", journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED, action, true, false),
		uxlive027Journey("j2", journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED, tracking, true, false),
		uxlive027Journey("j3", journeyv1.JourneyStage_JOURNEY_STAGE_MANAGER_APPROVAL, action, false, false),
		uxlive027Journey("j4", journeyv1.JourneyStage_JOURNEY_STAGE_FINANCE_APPROVAL, observing, false, false),
		uxlive027Journey("j5", journeyv1.JourneyStage_JOURNEY_STAGE_BLOCKED, action, true, false),
		uxlive027Journey("j6", journeyv1.JourneyStage_JOURNEY_STAGE_WAITING_EFFECTIVE_DATE, tracking, true, false),
		uxlive027Journey("j7", journeyv1.JourneyStage_JOURNEY_STAGE_RECORDED, closed, true, true),
		uxlive027Journey("j8", journeyv1.JourneyStage_JOURNEY_STAGE_REJECTED, closed, false, true),
		uxlive027Journey("j9", journeyv1.JourneyStage_JOURNEY_STAGE_FAILED, closed, false, true),
	}
	population := &journeyv1.JourneyPopulationSummary{
		Total: 9, Active: 6, Closed: 3, NeedsAction: 3, Tracking: 4, Exceptions: 2,
		LatestUpdate: timestamppb.New(time.Date(2026, 9, 12, 8, 0, 0, 0, time.UTC)),
		ComputedAt:   timestamppb.New(time.Date(2026, 9, 12, 9, 0, 0, 0, time.UTC)),
	}
	byStage := map[journeyv1.JourneyStage]int32{}
	for _, j := range journeys {
		byStage[j.GetStage()]++
	}
	for stage := journeyv1.JourneyStage(0); stage < 32; stage++ {
		if count := byStage[stage]; count > 0 {
			population.Stages = append(population.Stages, &journeyv1.JourneyStageCount{Stage: stage, Count: count})
		}
	}
	return &journeyv1.ListJourneysResponse{Journeys: journeys, Population: population}
}

func uxlive027Service(response *journeyv1.ListJourneysResponse) Service {
	return Service{
		ListJourneys: func(context.Context, *journeyv1.ListJourneysRequest) (*journeyv1.ListJourneysResponse, error) {
			return response, nil
		},
		ListWorkers: func(context.Context, *journeyv1.ListWorkersRequest) (*journeyv1.ListWorkersResponse, error) {
			return &journeyv1.ListWorkersResponse{Workers: []*journeyv1.Worker{
				{WorkerRef: uxlive027Viewer, WorkerId: uxlive027Viewer, LegalName: "Rafael Torres", JobCode: "OPS-DIR", Grade: "M4", ManagerRelationship: rootManagerRelationship()},
				{WorkerRef: "worker-j1", WorkerId: "worker-j1", LegalName: "Worker J1", JobCode: "OPS-HRBP2", Grade: "P2", ManagerRelationship: rootManagerRelationship()},
			}}, nil
		},
	}
}

var uxlive027Session = Session{Tenant: "tenant", Principal: uxlive027Viewer, Roles: []string{productui.RoleHCMAdmin}}

func uxlive027Load(t *testing.T, service Service, path, query string) productui.View {
	t.Helper()
	state, err := ParseState(path, query)
	if err != nil {
		t.Fatalf("ParseState(%s?%s): %v", path, query, err)
	}
	view, err := Load(context.Background(), service, uxlive027Session, state)
	if err != nil {
		t.Fatalf("Load(%s?%s): %v", path, query, err)
	}
	return view
}

func uxlive027Render(t *testing.T, view productui.View) string {
	t.Helper()
	doc, err := ui.RenderToString(productui.BuildPageContent(view))
	if err != nil {
		t.Fatalf("render %s: %v", view.Page, err)
	}
	return html.UnescapeString(doc)
}

// TestTodo_UXLIVE_027 is the PRIMARY case, through the production client's
// Load: for one unchanged session the Journeys tracker, Insights and Home all
// show the same nine journeys, and neither summary page can print its empty
// copy.
func TestTodo_UXLIVE_027(t *testing.T) {
	service := uxlive027Service(uxlive027Response())

	journeys := uxlive027Load(t, service, "/workspace/app/journeys", "")
	if journeys.JourneyPopulation == nil || journeys.JourneyPopulation.Total != 9 || len(journeys.Work) != 9 {
		t.Fatalf("Journeys loaded %d journeys with population %+v, want 9 and 9", len(journeys.Work), journeys.JourneyPopulation)
	}
	tracker := uxlive027Render(t, journeys)
	for _, j := range uxlive027Response().GetJourneys() {
		if !strings.Contains(tracker, j.GetWorkerName()) {
			t.Fatalf("the Journeys tracker does not list %s", j.GetWorkerName())
		}
	}

	insights := uxlive027Load(t, service, "/workspace/app/insights", "")
	insightsDoc := uxlive027Render(t, insights)
	if strings.Contains(insightsDoc, insights.Locale.Text("insights.no_data_title")) {
		t.Fatalf("Insights printed its empty copy over nine visible journeys:\n%s", insightsDoc)
	}
	for _, metric := range []struct{ label, value string }{
		{insights.Locale.Text("insights.visible_label"), "9"},
		{insights.Locale.Text("insights.in_progress_label"), "6"},
		{insights.Locale.Text("insights.closed_label"), "3"},
	} {
		if !regexp.MustCompile(regexp.QuoteMeta(metric.label) + `</span><strong>` + metric.value + `</strong>`).MatchString(insightsDoc) {
			t.Fatalf("Insights %q is not %s:\n%s", metric.label, metric.value, insightsDoc)
		}
	}

	home := uxlive027Load(t, service, "/workspace/app/home", "")
	homeDoc := uxlive027Render(t, home)
	if strings.Contains(homeDoc, home.Locale.Text("home.empty_title")) {
		t.Fatalf("Home claimed no work in progress over nine visible journeys:\n%s", homeDoc)
	}
}

// TestTodo_UXLIVE_027_Regression pins the RED itself: an authorized source
// that is not empty can never produce empty copy, even when the list the
// page was handed is (a projection that dropped rows, a stale baseline).
func TestTodo_UXLIVE_027_Regression(t *testing.T) {
	response := uxlive027Response()
	service := uxlive027Service(response)
	for _, page := range []string{"/workspace/app/insights", "/workspace/app/home"} {
		view := uxlive027Load(t, service, page, "")
		view.Work = nil
		doc := uxlive027Render(t, view)
		for _, empty := range []string{view.Locale.Text("insights.no_data_title"), view.Locale.Text("home.empty_title")} {
			if strings.Contains(doc, empty) {
				t.Fatalf("%s printed %q while the server counts %d journeys:\n%s", page, empty, view.JourneyPopulation.Total, doc)
			}
		}
	}

	// The RED's cause: a saved My Work tab ("tracked") was adopted on every
	// route and narrowed Home, Insights and History to it. It now applies
	// only to My Work.
	saved := service
	saved.GetPreferences = func(context.Context, *journeyv1.GetProductPreferencesRequest) (*journeyv1.GetProductPreferencesResponse, error) {
		return &journeyv1.GetProductPreferencesResponse{User: &journeyv1.UserPreferences{
			Tables: map[string]*journeyv1.TablePreferences{"work": {Filters: map[string]string{"filter": "tracked"}}},
		}}, nil
	}
	for _, page := range []string{"/workspace/app/home", "/workspace/app/insights", "/workspace/app/history", "/workspace/app/journeys"} {
		view := uxlive027Load(t, saved, page, "")
		if len(view.Work) != 9 || view.WorkFilter != "" {
			t.Fatalf("%s with a saved My Work tab holds %d journeys (filter %q), want all 9", page, len(view.Work), view.WorkFilter)
		}
	}
	if work := uxlive027Load(t, saved, "/workspace/app/work", ""); work.WorkFilter != "tracked" {
		t.Fatalf("My Work lost its own saved tab: %q", work.WorkFilter)
	}

	// A failed journey read drops the summary with the list: the counts fail
	// closed rather than outliving the read.
	failing := service
	failing.ListJourneys = func(context.Context, *journeyv1.ListJourneysRequest) (*journeyv1.ListJourneysResponse, error) {
		return nil, fmt.Errorf("unavailable")
	}
	state, _ := ParseState("/workspace/app/home", "")
	view, err := Load(context.Background(), failing, uxlive027Session, state)
	if err == nil {
		t.Fatal("a failed journey read reported success")
	}
	if view.JourneyPopulation != nil || view.Work != nil {
		t.Fatalf("a failed read kept population %+v and %d journeys", view.JourneyPopulation, len(view.Work))
	}

	// A warm navigation that reuses the baseline carries the same summary
	// as the list it reuses.
	baseline := uxlive027Load(t, service, "/workspace/app/journeys", "")
	next, _ := ParseState("/workspace/app/settings", "")
	warm, err := LoadWithBaseline(context.Background(), service, uxlive027Session, next, baseline)
	if err != nil {
		t.Fatalf("LoadWithBaseline: %v", err)
	}
	if warm.JourneyPopulation == nil || warm.JourneyPopulation.Total != len(warm.Work) {
		t.Fatalf("a warm baseline carried population %+v for %d journeys", warm.JourneyPopulation, len(warm.Work))
	}
}

var (
	uxlive030FactLink = regexp.MustCompile(`<a ([^>]*class="fact-link"[^>]*)>(\d+)<span class="sr-only"> ([^<]+)</span></a>`)
	uxlive030Href     = regexp.MustCompile(`href="([^"]+)"`)
)

// TestTodo_UXLIVE_030_Integration follows every number on a populated Home
// through the production client: parsing the link, loading the destination
// exactly as the router would, and counting the population that page lists.
// Each count must equal the number that linked to it.
func TestTodo_UXLIVE_030_Integration(t *testing.T) {
	service := uxlive027Service(uxlive027Response())
	home := uxlive027Load(t, service, "/workspace/app/home", "")
	doc := uxlive027Render(t, home)
	links := uxlive030FactLink.FindAllStringSubmatch(doc, -1)
	if len(links) < 7 {
		t.Fatalf("Home drill-down links = %d, want every journey and workforce number linked:\n%s", len(links), doc)
	}
	for _, link := range links {
		hrefMatch := uxlive030Href.FindStringSubmatch(link[1])
		if hrefMatch == nil {
			t.Fatalf("drill-down %q has no href: %s", link[3], link[0])
		}
		href, value, label := html.UnescapeString(hrefMatch[1]), link[2], link[3]
		path, query, _ := strings.Cut(href, "?")
		destination := uxlive027Load(t, service, path, query)
		var listed int
		switch label {
		case home.Locale.Text("home.needs_action"):
			listed = len(productui.ActionableWorkItems(productui.MyWorkItems(destination.Work, destination.Viewer)))
		case home.Locale.Text("home.following"):
			listed = len(productui.MyWorkItems(destination.Work, destination.Viewer))
		case home.Locale.Text("home.closed"):
			// History renders the closed journeys it lists; count the rows by
			// the closed workers' names on the rendered page.
			historyDoc := uxlive027Render(t, destination)
			for _, j := range uxlive027Response().GetJourneys() {
				if j.GetViewer().GetClosed() && strings.Contains(historyDoc, j.GetWorkerName()) {
					listed++
				}
			}
		case home.Locale.Text("home.in_progress"), home.Locale.Text("home.exceptions"):
			// Journeys narrows by the tracker's own status buckets
			// (UXLIVE-031): open, or issue = blocked, failed, repair.
			want := productui.JourneyListStatusOpen
			if label == home.Locale.Text("home.exceptions") {
				want = productui.JourneyListStatusIssue
			}
			if destination.Page != productui.PageJourneys || destination.JourneyList.Status != want {
				t.Fatalf("%q drills into %s, not Journeys with status %q", label, href, want)
			}
			for _, item := range destination.Work {
				issue := item.StatusKey == "journey.stage_blocked" || item.StatusKey == "journey.stage_failed" || item.StatusKey == "journey.stage_repair_required"
				if want == productui.JourneyListStatusIssue && issue || want == productui.JourneyListStatusOpen && !item.Terminal {
					listed++
				}
			}
		case home.Locale.Text("home.visible_workers"):
			listed = len(destination.People)
		case home.Locale.Text("home.eligible_workers"):
			for _, person := range destination.People {
				if person.PromotionAvailability == productui.PromotionEligible {
					listed++
				}
			}
		default:
			t.Fatalf("unexpected drill-down %q -> %s", label, href)
		}
		if fmt.Sprint(listed) != value {
			t.Fatalf("Home says %s %q, but %s lists %d", value, label, href, listed)
		}
	}
}

// TestTodo_UXLIVE_027_PersonPage: the person page reads the same authorized
// population. With a saved My Work tab in preferences (the RED's cause),
// Amara's page still lists her open promotion in Active workflows, offers a
// direct "Open active promotion" link to it, and lists her closed requests in
// Past workflows.
func TestTodo_UXLIVE_027_PersonPage(t *testing.T) {
	action := journeyv1.JourneyViewerResponsibility_JOURNEY_VIEWER_RESPONSIBILITY_OBSERVING
	closed := journeyv1.JourneyViewerResponsibility_JOURNEY_VIEWER_RESPONSIBILITY_CLOSED
	open := uxlive027Journey("amara-open", journeyv1.JourneyStage_JOURNEY_STAGE_MANAGER_APPROVAL, action, false, false)
	rejected := uxlive027Journey("amara-rejected", journeyv1.JourneyStage_JOURNEY_STAGE_REJECTED, closed, false, true)
	failed := uxlive027Journey("amara-failed", journeyv1.JourneyStage_JOURNEY_STAGE_FAILED, closed, false, true)
	for _, j := range []*journeyv1.Journey{open, rejected, failed} {
		j.WorkerRef, j.WorkerName = "worker-amara", "Amara Diallo"
	}
	service := uxlive027Service(&journeyv1.ListJourneysResponse{Journeys: []*journeyv1.Journey{open, rejected, failed}})
	workers := service.ListWorkers
	service.ListWorkers = func(ctx context.Context, request *journeyv1.ListWorkersRequest) (*journeyv1.ListWorkersResponse, error) {
		response, err := workers(ctx, request)
		if err == nil {
			response.Workers = append(response.Workers, &journeyv1.Worker{WorkerRef: "worker-amara", WorkerId: "worker-amara", LegalName: "Amara Diallo",
				JobCode: "PRD-UX3", Grade: "P4", ManagerRelationship: rootManagerRelationship()})
		}
		return response, err
	}
	service.GetPreferences = func(context.Context, *journeyv1.GetProductPreferencesRequest) (*journeyv1.GetProductPreferencesResponse, error) {
		return &journeyv1.GetProductPreferencesResponse{User: &journeyv1.UserPreferences{
			Tables: map[string]*journeyv1.TablePreferences{"work": {Filters: map[string]string{"filter": "tracked"}}},
		}}, nil
	}
	view := uxlive027Load(t, service, "/workspace/app/person", "person=worker-amara")
	doc := uxlive027Render(t, view)
	openHref := "journey=amara-open"
	launcherEnd := strings.Index(doc, "person-active-workflows")
	if launcherEnd < 0 || !strings.Contains(doc[:launcherEnd], view.Locale.Text("people.open_active_promotion")) || !strings.Contains(doc[:launcherEnd], openHref) {
		t.Fatalf("the person page offers no direct link to Amara's active promotion:\n%s", doc)
	}
	if !strings.Contains(doc[launcherEnd:], openHref) || strings.Contains(doc, view.Locale.Text("person.active_workflows_empty")) {
		t.Fatalf("Active workflows says none are in progress beside an open promotion:\n%s", doc)
	}
	for _, id := range []string{"amara-rejected", "amara-failed"} {
		if !strings.Contains(doc, id) {
			t.Fatalf("Past workflows omits %s:\n%s", id, doc)
		}
	}
}

// TestTodo_UXLIVE_027_PersonLauncherTitle: the launcher that is empty because
// a promotion is running says so rather than "No workflows available".
func TestTodo_UXLIVE_027_PersonLauncherTitle(t *testing.T) {
	open := uxlive027Journey("amara-open", journeyv1.JourneyStage_JOURNEY_STAGE_MANAGER_APPROVAL,
		journeyv1.JourneyViewerResponsibility_JOURNEY_VIEWER_RESPONSIBILITY_OBSERVING, false, false)
	open.WorkerRef, open.WorkerName = "worker-j1", "Worker J1"
	view := uxlive027Load(t, uxlive027Service(&journeyv1.ListJourneysResponse{Journeys: []*journeyv1.Journey{open}}), "/workspace/app/person", "person=worker-j1")
	doc := uxlive027Render(t, view)
	if strings.Contains(doc, view.Locale.Text("workflow.unavailable")) || !strings.Contains(doc, view.Locale.Text("workflow.promotion_in_progress_title")) {
		t.Fatalf("the launcher over a running promotion is not titled as one:\n%s", doc)
	}
}
