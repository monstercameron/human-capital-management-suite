package productclient

import (
	"context"
	"errors"
	"reflect"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func web030Service(journeys []*journeyv1.Journey, workers []*journeyv1.Worker, journeyCalls, workerCalls *atomic.Int32) Service {
	return Service{
		ListJourneys: func(context.Context, *journeyv1.ListJourneysRequest) (*journeyv1.ListJourneysResponse, error) {
			journeyCalls.Add(1)
			return &journeyv1.ListJourneysResponse{Journeys: journeys}, nil
		},
		ListWorkers: func(context.Context, *journeyv1.ListWorkersRequest) (*journeyv1.ListWorkersResponse, error) {
			workerCalls.Add(1)
			return &journeyv1.ListWorkersResponse{Workers: workers}, nil
		},
	}
}

func web030Journey(id string) *journeyv1.Journey {
	return &journeyv1.Journey{IntentId: id, WorkerName: "Riley Chen", Stage: journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_APPROVAL}
}

func web030Worker(id string) *journeyv1.Worker {
	return &journeyv1.Worker{WorkerRef: id, PreferredName: "Riley Chen", WorkerId: id, ManagerRelationship: rootManagerRelationship()}
}

func TestTodo_WEB_030(t *testing.T) {
	state, err := ParseState("/workspace/app/people", "q=Riley+Chen&team=Platform&page=2&page_size=50&sort=role&dir=desc&nav=collapsed&locale=de-DE")
	if err != nil {
		t.Fatal(err)
	}
	if got := CanonicalHref(state); got != "/workspace/app/people?dir=desc&locale=de-DE&nav=collapsed&page=2&page_size=50&q=Riley+Chen&sort=role&team=Platform" {
		t.Fatalf("cold deep link = %q", got)
	}
	var journeyCalls, workerCalls atomic.Int32
	view, err := Load(context.Background(), web030Service([]*journeyv1.Journey{web030Journey("intent-current")}, []*journeyv1.Worker{web030Worker("worker-current")}, &journeyCalls, &workerCalls), Session{Tenant: "tenant-a", Principal: "worker-current"}, state)
	if err != nil {
		t.Fatal(err)
	}
	if view.Page != productui.PagePeople || view.Query != "Riley Chen" || view.PeoplePage != 1 || view.PeoplePageSize != 50 || view.Locale.Resolved != "de-DE" || !view.NavCollapsed {
		t.Fatalf("cold route projection = %+v", view)
	}
	if got, want := ResolvedCanonicalHref(state, view), "/workspace/app/people?dir=desc&locale=de-DE&nav=collapsed&page=1&page_size=50&q=Riley+Chen&sort=role&team=Platform"; got != want {
		t.Fatalf("resolved cold route = %q, want %q", got, want)
	}
	if journeyCalls.Load() != 1 || workerCalls.Load() != 1 || len(view.People) != 1 || view.People[0].ID != "worker-current" {
		t.Fatalf("fresh authorized cold reads = journeys %d workers %d view %+v", journeyCalls.Load(), workerCalls.Load(), view)
	}
}

func TestTodo_WEB_030_Golden(t *testing.T) {
	state, err := ParseState("/workspace/app/journeys", "worker=stale-worker&mode=new&journey=intent%2B17&credential=Bearer-secret&action=approve&authority_token=old")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := CanonicalHref(state), "/workspace/app/journeys?journey=intent%2B17"; got != want {
		t.Fatalf("canonical resume address = %q, want %q", got, want)
	}
	got := ProductJourneyHref(journeyclient.DetailHref("intent+17"), "nav=collapsed&menu_q=journey&favorites=people,history&journey=stale&worker=stale&csrf=secret")
	want := "/workspace/app/journeys?favorites=people%2Chistory&journey=intent%2B17&menu_q=journey&nav=collapsed"
	if got != want {
		t.Fatalf("canonical journey address = %q, want %q", got, want)
	}
	for _, forbidden := range []string{"stale", "Bearer", "approve", "token", "csrf"} {
		if strings.Contains(got+CanonicalHref(state), forbidden) {
			t.Fatalf("canonical address replayed %q: %s %s", forbidden, got, CanonicalHref(state))
		}
	}
}

// The js/wasm counterpart with this exact name exercises GWC v5 pushState,
// popstate, cancellation, and loader generations. This native half pins the
// address contract used by server-rendered links and cold document requests.
func TestTodo_WEB_030_Browser(t *testing.T) {
	for raw, want := range map[string]string{
		"/workspace/app/journeys?journey=intent-17&nav=collapsed":                "/workspace/app/journeys?journey=intent-17&nav=collapsed",
		"/workspace/app/people?q=Riley&page=02&sort=role&dir=desc":               "/workspace/app/people?dir=desc&page=2&q=Riley&sort=role",
		"/workspace/app/history?history_q=promotion&outcome=completed&page=9":    "/workspace/app/history?history_q=promotion&outcome=completed",
		"/workspace/app/settings?journey=stale&worker=stale&selected=stale-work": "/workspace/app/settings",
	} {
		parts := strings.SplitN(raw, "?", 2)
		query := ""
		if len(parts) == 2 {
			query = parts[1]
		}
		state, err := ParseState(parts[0], query)
		if err != nil {
			t.Fatalf("browser address %q rejected: %v", raw, err)
		}
		if got := CanonicalHref(state); got != want {
			t.Fatalf("browser address %q canonicalized to %q, want %q", raw, got, want)
		}
	}
}

func TestTodo_WEB_030_Conformance(t *testing.T) {
	for name, query := range map[string]string{
		"duplicate":         "nav=collapsed&nav=expanded",
		"control":           "menu_q=hello%00world",
		"bad escape":        "q=%zz",
		"invalid page":      "page=zero",
		"zero page size":    "page_size=0",
		"unknown page size": "page_size=75",
		"invalid nav":       "nav=hidden",
		"invalid sort":      "sort=salary",
		"invalid direction": "dir=sideways",
		"invalid journey":   "mode=execute",
		"oversized":         "q=" + strings.Repeat("x", maxRouteQueryBytes),
	} {
		t.Run(name, func(t *testing.T) {
			path := "/workspace/app/people"
			if name == "invalid journey" {
				path = "/workspace/app/journeys"
			}
			if _, err := ParseState(path, query); err == nil {
				t.Fatalf("ParseState accepted unsafe route state %q", query)
			}
		})
	}
	if _, err := ParseState("/workspace/app/unknown", "credential=secret"); err == nil {
		t.Fatal("unknown product resource was accepted")
	}
	state, err := ParseState("/workspace/app/people", "q=%20Riley%20&page=0002&journey=wrong-page&credential=secret&locale=de-DE")
	if err != nil {
		t.Fatal(err)
	}
	if got := CanonicalHref(state); got != "/workspace/app/people?locale=de-DE&page=2&q=Riley" {
		t.Fatalf("page-scoped canonical address = %q", got)
	}
	// GWC's js runtime correctly requires a mounted DOM adapter for components
	// with event hooks. The actual js/wasm router contract is covered in the
	// journeywasm package; static accessible markup remains a native SSR gate.
	if runtime.GOOS == "js" {
		return
	}
	loading := ContentLoadingView(productui.NewView(productui.PageHome, "Tenant", "Principal", "Scope"), state)
	markup, err := ui.RenderToString(productui.BuildContentLoading(loading))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`aria-busy="true"`, `aria-live="polite"`, `class="sr-only route-announcer"`, `id="main-content"`, productui.ResolveProductLocale("de-DE").Text("shell.loading_authorized")} {
		if !strings.Contains(markup, want) {
			t.Errorf("destination loading projection missing %q", want)
		}
	}
}

func TestTodo_WEB_030_Security(t *testing.T) {
	baseline := productui.NewView(productui.PageHome, "Tenant A", "Principal A", "Scope")
	baseline.Work = []productui.WorkItem{{ID: "secret-work"}}
	baseline.People = []productui.Person{{ID: "secret-person", Name: "Secret Person"}}
	var journeyCalls, workerCalls atomic.Int32
	denied := Service{
		ListJourneys: func(context.Context, *journeyv1.ListJourneysRequest) (*journeyv1.ListJourneysResponse, error) {
			journeyCalls.Add(1)
			return nil, status.Error(codes.PermissionDenied, "tenant-a journey exists")
		},
		ListWorkers: func(context.Context, *journeyv1.ListWorkersRequest) (*journeyv1.ListWorkersResponse, error) {
			workerCalls.Add(1)
			return nil, status.Error(codes.PermissionDenied, "tenant-a worker exists")
		},
	}
	view, err := LoadWithBaseline(context.Background(), denied, Session{Tenant: "tenant-b", Principal: "principal-b", Scope: "scope"}, State{Page: productui.PageSettings, Request: productui.PageRequest{Page: productui.PageSettings}}, baseline)
	if err == nil || journeyCalls.Load() != 1 || workerCalls.Load() != 1 || len(view.Work) != 0 || len(view.People) != 0 {
		t.Fatalf("cross-session resume did not reauthorize and fail closed: calls=%d/%d view=%+v err=%v", journeyCalls.Load(), workerCalls.Load(), view, err)
	}
	serviceType := reflect.TypeOf(Service{})
	for index := 0; index < serviceType.NumField(); index++ {
		name := serviceType.Field(index).Name
		if !strings.HasPrefix(name, "List") && !strings.HasPrefix(name, "Get") {
			t.Fatalf("route projection acquired mutation capability %q", name)
		}
	}
	state, err := ParseState("/workspace/app/work", "selected=work-1&approve=true&idempotency_key=replay&authority_token=secret")
	if err != nil {
		t.Fatal(err)
	}
	if got := CanonicalHref(state); got != "/workspace/app/work?selected=work-1" {
		t.Fatalf("route retained mutation or authority state: %q", got)
	}
}

func TestTodo_WEB_030_Integration(t *testing.T) {
	var journeyCalls, workerCalls atomic.Int32
	service := web030Service([]*journeyv1.Journey{web030Journey("intent-current")}, []*journeyv1.Worker{web030Worker("worker-current")}, &journeyCalls, &workerCalls)
	session := Session{Tenant: "tenant-a", Principal: "principal-a", Scope: "manager"}
	first, err := Load(context.Background(), service, session, State{Page: productui.PageHome, Request: productui.PageRequest{Page: productui.PageHome}})
	if err != nil {
		t.Fatal(err)
	}
	state, err := ParseState("/workspace/app/work", "selected=intent-current")
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadWithBaseline(context.Background(), service, session, state, first)
	if err != nil {
		t.Fatal(err)
	}
	if journeyCalls.Load() != 2 || workerCalls.Load() != 2 || second.SelectedWork != "intent-current" || len(second.Work) != 1 {
		t.Fatalf("same-session history resume did not refetch destination data: calls=%d/%d view=%+v", journeyCalls.Load(), workerCalls.Load(), second)
	}
	settings, err := LoadWithBaseline(context.Background(), service, session, State{Page: productui.PageSettings, Request: productui.PageRequest{Page: productui.PageSettings}}, second)
	if err != nil {
		t.Fatal(err)
	}
	if journeyCalls.Load() != 2 || workerCalls.Load() != 2 || len(settings.Work) != 1 || len(settings.People) != 1 {
		t.Fatalf("destination-scoped baseline regressed: calls=%d/%d view=%+v", journeyCalls.Load(), workerCalls.Load(), settings)
	}
}

func TestTodo_WEB_030_ResolvedPagination(t *testing.T) {
	var journeyCalls, workerCalls atomic.Int32
	journey := web030Journey("intent-complete")
	journey.Stage = journeyv1.JourneyStage_JOURNEY_STAGE_COMPLETED
	service := web030Service([]*journeyv1.Journey{journey}, []*journeyv1.Worker{web030Worker("worker-current")}, &journeyCalls, &workerCalls)
	state, err := ParseState("/workspace/app/history", "history_page=0009&history_page_size=10&history_sort=closed&history_dir=desc")
	if err != nil {
		t.Fatal(err)
	}
	view, err := Load(context.Background(), service, Session{Tenant: "tenant-a", Principal: "worker-current"}, state)
	if err != nil {
		t.Fatal(err)
	}
	if view.HistoryPage != 1 || view.HistoryPageSize != 10 {
		t.Fatalf("resolved history window = page %d size %d", view.HistoryPage, view.HistoryPageSize)
	}
	if got, want := ResolvedCanonicalHref(state, view), "/workspace/app/history?history_dir=desc&history_page=1&history_page_size=10&history_sort=closed"; got != want {
		t.Fatalf("resolved history route = %q, want %q", got, want)
	}
}

func TestTodo_WEB_030_Fault(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var journeyObserved, workerObserved atomic.Bool
	service := Service{
		ListJourneys: func(ctx context.Context, _ *journeyv1.ListJourneysRequest) (*journeyv1.ListJourneysResponse, error) {
			journeyObserved.Store(errors.Is(ctx.Err(), context.Canceled))
			return nil, ctx.Err()
		},
		ListWorkers: func(ctx context.Context, _ *journeyv1.ListWorkersRequest) (*journeyv1.ListWorkersResponse, error) {
			workerObserved.Store(errors.Is(ctx.Err(), context.Canceled))
			return nil, ctx.Err()
		},
	}
	view, err := Load(ctx, service, Session{}, State{Page: productui.PageHome, Request: productui.PageRequest{Page: productui.PageHome}})
	if err == nil || !journeyObserved.Load() || !workerObserved.Load() || view.Work != nil || view.People != nil {
		t.Fatalf("cancelled route did not fail closed: view=%+v err=%v observed=%v/%v", view, err, journeyObserved.Load(), workerObserved.Load())
	}
}

func BenchmarkProductDeepLinkResolve(b *testing.B) {
	const query = "q=Riley+Chen&team=Platform&page=2&page_size=50&sort=role&dir=desc&nav=collapsed&locale=de-DE"
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		state, err := ParseState("/workspace/app/people", query)
		if err != nil {
			b.Fatal(err)
		}
		if CanonicalHref(state) == "" {
			b.Fatal("empty canonical route")
		}
	}
}

func BenchmarkProductResolvedRouteCanonicalize(b *testing.B) {
	state, err := ParseState("/workspace/app/people", "q=Rafael&page=0002&page_size=50&sort=role&dir=desc")
	if err != nil {
		b.Fatal(err)
	}
	view := productui.NewView(productui.PagePeople, "Tenant", "Principal", "Scope")
	view.People = []productui.Person{{ID: "worker-1", Name: "Rafael"}}
	view = productui.ApplyRequest(view, state.Request)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if href := ResolvedCanonicalHref(state, view); href != "/workspace/app/people?dir=desc&page=1&page_size=50&q=Rafael&sort=role" {
			b.Fatal(href)
		}
	}
}

func BenchmarkProductRouteResume(b *testing.B) {
	session := Session{Tenant: "tenant-a", Principal: "principal-a", Scope: "manager"}
	baseline := LoadingView(session, State{Page: productui.PageHome, Request: productui.PageRequest{Page: productui.PageHome}})
	baseline.Work = []productui.WorkItem{{ID: "intent-current"}}
	baseline.People = []productui.Person{{ID: "worker-current", Name: "Riley Chen"}}
	state := State{Page: productui.PageSettings, Request: productui.PageRequest{Page: productui.PageSettings}}
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		if _, err := LoadWithBaseline(context.Background(), Service{}, session, state, baseline); err != nil {
			b.Fatal(err)
		}
	}
}
