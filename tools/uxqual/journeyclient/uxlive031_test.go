package journeyclient

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// UXLIVE-031's RED was measured on the live Journeys page: cards grouped by
// person, but no search, status, date, sort or grouping control, and no
// address state, so a growing request set had to be scanned by eye.

func uxlive031Journey(intentID, name, ref string, stage journeyv1.JourneyStage, updated string) *journeyv1.Journey {
	at, err := time.Parse(time.RFC3339, updated)
	if err != nil {
		panic(err)
	}
	return &journeyv1.Journey{
		IntentId: intentID, WorkerName: name, WorkerRef: ref, Stage: stage,
		Current: &journeyv1.Placement{JobCode: "OPS-HRBP2", Grade: "P2"}, Target: &journeyv1.Placement{JobCode: "OPS-HRBP3", Grade: "P3"},
		CurrentBase: "93000.00", ProposedBase: "98000.00", Currency: "USD", EffectiveDate: "2026-10-01",
		CreatedAt: timestamppb.New(at.Add(-time.Hour)), UpdatedAt: timestamppb.New(at),
	}
}

// uxlive031Journeys is four requests across three people, every lifecycle
// bucket but one, and four different update dates.
func uxlive031Journeys() []*journeyv1.Journey {
	return []*journeyv1.Journey{
		uxlive031Journey("01a0b189-e04c-7265-8926-9095318cf888", "Amara Okafor", "worker:amara", journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_APPROVAL, "2026-09-18T10:00:00Z"),
		uxlive031Journey("01a0b182-28a0-78a5-a93a-8a30a0a89ac8", "Omar Reyes", "worker:omar", journeyv1.JourneyStage_JOURNEY_STAGE_BLOCKED, "2026-09-10T10:00:00Z"),
		uxlive031Journey("01a0b17f-0000-7000-8000-000000a11ce5", "Amara Okafor", "worker:amara", journeyv1.JourneyStage_JOURNEY_STAGE_RECORDED, "2026-08-02T10:00:00Z"),
		uxlive031Journey("01a0b17a-0000-7000-8000-0000005a1c0b", "Isaac Chen", "worker:isaac", journeyv1.JourneyStage_JOURNEY_STAGE_WAITING_EFFECTIVE_DATE, "2026-09-15T10:00:00Z"),
	}
}

func uxlive031List(t *testing.T, filter productui.JourneyListFilter) *journey.ListView {
	t.Helper()
	page := ListPage(testConfig(), ListData{Journeys: uxlive031Journeys(), Filter: filter}, nil, nil)
	if page.List == nil || page.List.Filter == nil {
		t.Fatalf("the tracker projected no filter: %+v", page.List)
	}
	return page.List
}

func uxlive031Intents(cards []journey.JourneyCard) []string {
	ids := make([]string, 0, len(cards))
	for _, card := range cards {
		ids = append(ids, card.IntentID)
	}
	return ids
}

// TestTodo_UXLIVE_031 is the primary red/green test: search by person and
// by request reference, status and date-range filters, a recency sort and a
// grouping choice each narrow or reorder the list and state the result.
func TestTodo_UXLIVE_031(t *testing.T) {
	all := uxlive031Journeys()

	byPerson := uxlive031List(t, productui.JourneyListFilter{Query: "amara"})
	if got := uxlive031Intents(byPerson.Journeys); len(got) != 2 || got[0] != all[0].GetIntentId() || got[1] != all[2].GetIntentId() {
		t.Fatalf("search by person = %v, want Amara's two requests newest first", got)
	}
	if byPerson.Filter.Result != "2 of 4 requests" || !byPerson.Filter.Narrowed {
		t.Fatalf("a narrowed list states %q (narrowed=%v), want \"2 of 4 requests\"", byPerson.Filter.Result, byPerson.Filter.Narrowed)
	}

	byReference := uxlive031List(t, productui.JourneyListFilter{Query: "89ac8"})
	if got := uxlive031Intents(byReference.Journeys); len(got) != 1 || got[0] != all[1].GetIntentId() {
		t.Fatalf("search by request reference = %v, want Omar's request only", got)
	}
	upper := uxlive031List(t, productui.JourneyListFilter{Query: journey.JourneyReference(all[1].GetIntentId())})
	if len(upper.Journeys) != 1 {
		t.Fatalf("the visible reference %q does not find its own request", journey.JourneyReference(all[1].GetIntentId()))
	}

	closed := uxlive031List(t, productui.JourneyListFilter{Status: productui.JourneyListStatusClosed})
	if got := uxlive031Intents(closed.Journeys); len(got) != 1 || got[0] != all[2].GetIntentId() {
		t.Fatalf("status closed = %v, want the recorded request only", got)
	}
	open := uxlive031List(t, productui.JourneyListFilter{Status: productui.JourneyListStatusOpen})
	if len(open.Journeys) != 3 {
		t.Fatalf("status open shows %d requests, want the three not closed", len(open.Journeys))
	}

	september := uxlive031List(t, productui.JourneyListFilter{From: "2026-09-11", To: "2026-09-30"})
	if got := uxlive031Intents(september.Journeys); len(got) != 2 || got[0] != all[0].GetIntentId() || got[1] != all[3].GetIntentId() {
		t.Fatalf("updated 11-30 Sep = %v, want Amara's and Isaac's open requests", got)
	}

	oldest := uxlive031List(t, productui.JourneyListFilter{Sort: productui.JourneyListSortOldest, Group: productui.JourneyListGroupNone})
	if got := uxlive031Intents(oldest.Journeys); got[0] != all[2].GetIntentId() || got[len(got)-1] != all[0].GetIntentId() {
		t.Fatalf("oldest first = %v", got)
	}
	if len(oldest.Groups) != 0 || oldest.Grouping != productui.JourneyListGroupNone {
		t.Fatalf("no grouping still grouped: groups=%d grouping=%q", len(oldest.Groups), oldest.Grouping)
	}

	byStatus := uxlive031List(t, productui.JourneyListFilter{Group: productui.JourneyListGroupStatus})
	if len(byStatus.Groups) != 0 || byStatus.Grouping != productui.JourneyListGroupStatus {
		t.Fatalf("group by status fell back to person groups: %+v", byStatus.Groups)
	}
	markup, err := journey.RenderToString(journey.Page{List: byStatus})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"journeys-group-review", "journeys-group-waiting", "journeys-group-issue", "journeys-group-closed"} {
		if !strings.Contains(markup, `id="`+id+`"`) {
			t.Fatalf("group by status is missing %s:\n%s", id, markup)
		}
	}

	nothing := uxlive031List(t, productui.JourneyListFilter{Query: "nobody-by-this-name"})
	if len(nothing.Journeys) != 0 || nothing.Filter.EmptyTitle == "" || nothing.Filter.ClearHref != ListHref() {
		t.Fatalf("an empty result is not explicit: %+v", nothing.Filter)
	}
	if nothing.Filter.Result != "0 of 4 requests" {
		t.Fatalf("empty result states %q", nothing.Filter.Result)
	}
}

// TestTodo_UXLIVE_031_Regression keeps the unfiltered tracker exactly as it
// was and keeps every existing fragment meaning what it meant.
func TestTodo_UXLIVE_031_Regression(t *testing.T) {
	plain := uxlive031List(t, productui.JourneyListFilter{})
	if len(plain.Journeys) != 4 || len(plain.Groups) != 3 || plain.Grouping != "" {
		t.Fatalf("the default tracker changed: %d cards, %d groups, grouping %q", len(plain.Journeys), len(plain.Groups), plain.Grouping)
	}
	if plain.Filter.Narrowed || plain.Filter.ClearHref != "" || plain.Filter.Result != "4 requests" {
		t.Fatalf("an unfiltered tracker claims a filter: %+v", plain.Filter)
	}
	for fragment, want := range map[string]string{
		"":                        "#/journeys",
		"#/journeys":              "#/journeys",
		"#/journeys?worker=x":     "#/journeys?worker=x",
		"#/journeys/int_1":        "#/journeys/int_1",
		"#/journeys/new?worker=x": "#/journeys/new?worker=x",
	} {
		if got := Href(Parse(fragment)); got != want {
			t.Errorf("Href(Parse(%q)) = %q, want %q", fragment, got, want)
		}
	}
	// Defaults and values outside the vocabulary have no spelling.
	route := Parse("#/journeys?sort=recent&group=person&status=everything&from=yesterday&q=%20%20")
	if !route.Filter.IsZero() || Href(route) != "#/journeys" {
		t.Fatalf("defaults or junk survived in the address: %+v -> %q", route.Filter, Href(route))
	}
	// A filter round-trips, and the worker selection keeps its place.
	filter := productui.JourneyListFilter{Query: "Amara O", Status: "review", From: "2026-09-01", To: "2026-09-30", Sort: "oldest", Group: "none"}
	href := ListFilterHref("worker:amara", filter)
	if href != "#/journeys?worker=worker%3Aamara&q=Amara+O&status=review&from=2026-09-01&to=2026-09-30&sort=oldest&group=none" {
		t.Fatalf("filter href = %q", href)
	}
	if back := Parse(href); back.WorkerRef != "worker:amara" || back.Filter != filter {
		t.Fatalf("filter did not round-trip: %+v", back)
	}
	// A reversed range is ordered, not refused.
	if got := productui.NormalizeJourneyListFilter(productui.JourneyListFilter{From: "2026-09-30", To: "2026-09-01"}); got.From != "2026-09-01" || got.To != "2026-09-30" {
		t.Fatalf("reversed range = %+v", got)
	}
}

// TestTodo_UXLIVE_031_Integration drives the live client: the filter form
// navigates, the narrowed list is projected from the answers already in
// hand, and going Back to the unfiltered address restores the full list and
// the form's controls.
func TestTodo_UXLIVE_031_Integration(t *testing.T) {
	h := newHarness(t)
	h.svc.list = uxlive031Journeys()
	var located []string
	h.app.Locate = func(href string) { located = append(located, href) }
	h.app.Start(context.Background(), "")
	h.awaitPage(t, "the list", func(p journey.Page) bool { return listLoaded(p) && len(p.List.Journeys) == 4 })
	reads := h.svc.called("ListJourneys")

	h.app.Submit(ActionFilterList, map[string]string{productui.JourneyListQueryKey: "amara", productui.JourneyListStatusKey: "review"})
	if len(located) != 1 || located[0] != "#/journeys?q=amara&status=review" {
		t.Fatalf("applying the filter navigated to %v", located)
	}
	// The browser (or product router) hands the address back.
	h.app.OnHashChange(located[0])
	p := h.awaitPage(t, "the narrowed list", func(p journey.Page) bool { return p.List != nil && len(p.List.Journeys) == 1 })
	if p.List.Journeys[0].IntentID != uxlive031Journeys()[0].GetIntentId() || p.List.Filter.Result != "1 of 4 requests" {
		t.Fatalf("narrowed list = %v, %q", uxlive031Intents(p.List.Journeys), p.List.Filter.Result)
	}
	if h.svc.called("ListJourneys") != reads {
		t.Fatal("narrowing the list re-read the tenant")
	}
	if got := h.store.Values()[FieldFilterQuery]; got != "amara" {
		t.Fatalf("the search box shows %q after applying", got)
	}

	// Picking a person keeps the filter.
	h.app.selectWorker("worker:amara")
	if last := located[len(located)-1]; last != "#/journeys?worker=worker%3Aamara&q=amara&status=review" {
		t.Fatalf("selecting a person dropped the filter: %q", last)
	}

	// Back: the unfiltered address restores everything, including the form.
	h.store.SetValue(FieldFilterQuery, "typed but never applied")
	h.app.OnHashChange("#/journeys")
	p = h.awaitPage(t, "the restored list", func(p journey.Page) bool { return p.List != nil && len(p.List.Journeys) == 4 })
	if p.List.Filter.Narrowed || h.store.Values()[FieldFilterQuery] != "" || h.store.Values()[FieldFilterStatus] != "" {
		t.Fatalf("Back did not restore the unfiltered list and form: narrowed=%v values=%v", p.List.Filter.Narrowed, h.store.Values())
	}
}

// TestTodo_UXLIVE_031_Performance keeps filtering a large authorized
// answer, projecting it and rendering it inside the list-interaction
// budget the tracker's other list interactions are held to.
func TestTodo_UXLIVE_031_Performance(t *testing.T) {
	const count = 2000
	journeys := make([]*journeyv1.Journey, 0, count)
	for index := 0; index < count; index++ {
		stage := journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_APPROVAL
		if index%3 == 0 {
			stage = journeyv1.JourneyStage_JOURNEY_STAGE_RECORDED
		}
		journeys = append(journeys, uxlive031Journey(fmt.Sprintf("01a0b189-e04c-7265-8926-%012d", index),
			fmt.Sprintf("Worker %04d", index%400), fmt.Sprintf("worker:%04d", index%400), stage,
			time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(index)*time.Minute).Format(time.RFC3339)))
	}
	// A person search over a large tenant: ten people's requests out of two
	// thousand, narrowed further to the open ones in a two-day window.
	filter := productui.JourneyListFilter{Query: "Worker 031", Status: productui.JourneyListStatusOpen, From: "2026-09-01", To: "2026-09-02"}
	var elapsed []time.Duration
	for run := 0; run < 7; run++ {
		start := time.Now()
		page := ListPage(testConfig(), ListData{Journeys: journeys, Filter: filter}, nil, nil)
		if _, err := journey.RenderToString(journey.Page{List: page.List}); err != nil {
			t.Fatal(err)
		}
		elapsed = append(elapsed, time.Since(start))
		if len(page.List.Journeys) == 0 || len(page.List.Journeys) == count {
			t.Fatalf("the large fixture did not narrow: %d cards", len(page.List.Journeys))
		}
	}
	best := elapsed[0]
	for _, d := range elapsed[1:] {
		if d < best {
			best = d
		}
	}
	// 100 ms is the list-interaction budget (a filter change must feel
	// immediate); the best of seven runs is compared so a busy test host
	// does not fail a projection that is not slow.
	if best > 100*time.Millisecond {
		t.Fatalf("filtering %d requests took %v, over the 100ms list-interaction budget (runs %v)", count, best, elapsed)
	}
}

// TestUXLIVE031FilterControlsApplyLive: a select applies as soon as it
// changes, the search applies once after typing pauses, and an address that
// arrives while a search is still pending does not overwrite what is typed.
func TestUXLIVE031FilterControlsApplyLive(t *testing.T) {
	h := newHarness(t)
	h.svc.list = uxlive031Journeys()
	var located []string
	h.app.Locate = func(href string) { located = append(located, href) }
	var scheduled []func()
	h.app.listFilter.After = func(_ time.Duration, f func()) func() bool {
		scheduled = append(scheduled, f)
		return func() bool { return true }
	}
	h.app.Start(context.Background(), "")
	h.awaitPage(t, "the list", func(p journey.Page) bool { return listLoaded(p) && len(p.List.Journeys) == 4 })
	page := h.store.Page()

	page.OnFieldChange(FieldFilterStatus, "review")
	if len(located) != 1 || located[0] != "#/journeys?status=review" {
		t.Fatalf("changing the status navigated to %v", located)
	}
	page.OnFieldChange(FieldFilterQuery, "am")
	page.OnFieldChange(FieldFilterQuery, "amara")
	if len(located) != 1 || len(scheduled) != 2 {
		t.Fatalf("typing navigated before the pause: located=%v scheduled=%d", located, len(scheduled))
	}
	h.app.OnHashChange(located[0])
	if got := h.store.Values()[FieldFilterQuery]; got != "amara" {
		t.Fatalf("an address arriving mid-typing overwrote the search with %q", got)
	}
	scheduled[len(scheduled)-1]()
	if last := located[len(located)-1]; last != "#/journeys?q=amara&status=review" {
		t.Fatalf("the paused search applied %q", last)
	}
	if !h.app.applyListFilterField(FieldFilterGroup, "none") || h.app.applyListFilterField(FieldJobCode, "x") {
		t.Fatal("filter controls and other fields are not told apart")
	}
}
