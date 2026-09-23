package journey_test

import (
	"context"
	"sync"
	"testing"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/journey"
)

// pagingFakeEngine is a fakeEngine that also implements
// workspace.HistoryEngine. It answers one canned page while recording the
// query it was asked, so the tests prove the server maps the wire request
// onto the port instead of ignoring it.
type pagingFakeEngine struct {
	*fakeEngine
	mu       sync.Mutex
	lastReq  workspace.JourneyListRequest
	page     workspace.JourneyListPage
	pageErr  error
	pageCall int
}

func (f *pagingFakeEngine) ListJourneysPage(_ context.Context, req workspace.JourneyListRequest) (workspace.JourneyListPage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pageCall++
	f.lastReq = req
	if f.pageErr != nil {
		return workspace.JourneyListPage{}, f.pageErr
	}
	return f.page, nil
}

func (f *pagingFakeEngine) recordedReq() workspace.JourneyListRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastReq
}

func pagedSummary(id string, closed bool) workspace.JourneySummary {
	s := fixtureSummary()
	s.IntentID = id
	if closed {
		s.Stage = workspace.JourneyStageRecorded
		s.Viewer = workspace.JourneyViewerProjection{Responsibility: workspace.JourneyResponsibilityClosed, Closed: true}
	}
	return s
}

// TestListJourneysPagesThroughTheEngine is INTAPI-006's RED for the paging
// defect: ListJourneys ignored its page request entirely — no page size,
// no cursor, no filters — and answered the whole collection with no page
// token at all.
func TestListJourneysPagesThroughTheEngine(t *testing.T) {
	first, second := pagedSummary("intent-page-1", false), pagedSummary("intent-page-2", false)
	engine := &pagingFakeEngine{fakeEngine: newFakeEngine(), page: workspace.JourneyListPage{
		Journeys: []workspace.JourneySummary{first, second}, NextCursor: "cursor-page-2", TotalCount: 5,
	}}
	client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine}))
	ctx := testContext(t)

	resp, err := client.ListJourneys(ctx, &journeyv1.ListJourneysRequest{
		Page:      &commonv1.PageRequest{PageSize: 2},
		WorkerRef: "worker-1", Query: "vega", Sort: "person", Direction: "desc",
		PageNumber: 1, Outcome: "PROPOSED", Year: "2026",
	})
	if err != nil {
		t.Fatalf("ListJourneys: %v", err)
	}
	got := engine.recordedReq()
	want := workspace.JourneyListRequest{
		PageSize: 2, Page: 1,
		WorkerRef: "worker-1", Query: "vega", Sort: "person", Direction: "desc",
		Outcome: "PROPOSED", Year: "2026",
	}
	if got != want {
		t.Fatalf("engine saw %+v, want %+v: the page request never reached the port", got, want)
	}
	if len(resp.GetJourneys()) != 2 {
		t.Fatalf("journeys = %d, want the engine's page of 2", len(resp.GetJourneys()))
	}
	if resp.GetPage().GetNextCursor() != "cursor-page-2" {
		t.Fatalf("next_cursor = %q, want cursor-page-2", resp.GetPage().GetNextCursor())
	}
	if resp.GetTotalCount() != 5 {
		t.Fatalf("total_count = %d, want 5", resp.GetTotalCount())
	}

	// A resume cursor travels back to the engine untouched.
	if _, err := client.ListJourneys(ctx, &journeyv1.ListJourneysRequest{
		Page: &commonv1.PageRequest{PageSize: 2, Cursor: "cursor-page-2"},
	}); err != nil {
		t.Fatalf("ListJourneys resume: %v", err)
	}
	if engine.recordedReq().Cursor != "cursor-page-2" {
		t.Fatalf("engine saw cursor %q, want cursor-page-2", engine.recordedReq().Cursor)
	}
}

// TestListJourneysTerminalOnlyFiltersToClosed proves the History surface's
// contract: terminal_only answers only journeys at a terminal stage, as the
// engine's own closed flag reports them.
func TestListJourneysTerminalOnlyFiltersToClosed(t *testing.T) {
	engine := &pagingFakeEngine{fakeEngine: newFakeEngine(), page: workspace.JourneyListPage{
		Journeys:   []workspace.JourneySummary{pagedSummary("intent-open", false), pagedSummary("intent-closed", true)},
		TotalCount: 2,
	}}
	client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine}))

	resp, err := client.ListJourneys(testContext(t), &journeyv1.ListJourneysRequest{TerminalOnly: true})
	if err != nil {
		t.Fatalf("ListJourneys: %v", err)
	}
	if len(resp.GetJourneys()) != 1 || resp.GetJourneys()[0].GetIntentId() != "intent-closed" {
		t.Fatalf("terminal journeys = %v, want only intent-closed", resp.GetJourneys())
	}
}

// TestListJourneysFallbackSlicesWithoutHistoryEngine proves an engine that
// predates the paging port still pages: the transport slices locally by
// size and number, and refuses an opaque cursor it cannot honor rather
// than silently answering the first page.
func TestListJourneysFallbackSlicesWithoutHistoryEngine(t *testing.T) {
	engine := newFakeEngine()
	engine.summaries = []workspace.JourneySummary{
		pagedSummary("intent-1", false), pagedSummary("intent-2", false), pagedSummary("intent-3", false),
	}
	client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine}))
	ctx := testContext(t)

	first, err := client.ListJourneys(ctx, &journeyv1.ListJourneysRequest{Page: &commonv1.PageRequest{PageSize: 2}})
	if err != nil {
		t.Fatalf("ListJourneys: %v", err)
	}
	if len(first.GetJourneys()) != 2 || first.GetTotalCount() != 3 {
		t.Fatalf("page 1 = %d journeys total %d, want 2 of 3", len(first.GetJourneys()), first.GetTotalCount())
	}
	second, err := client.ListJourneys(ctx, &journeyv1.ListJourneysRequest{
		Page: &commonv1.PageRequest{PageSize: 2}, PageNumber: 2,
	})
	if err != nil {
		t.Fatalf("ListJourneys page 2: %v", err)
	}
	if len(second.GetJourneys()) != 1 || second.GetJourneys()[0].GetIntentId() != "intent-3" {
		t.Fatalf("page 2 = %v, want intent-3", second.GetJourneys())
	}
	if _, err := client.ListJourneys(ctx, &journeyv1.ListJourneysRequest{
		Page: &commonv1.PageRequest{Cursor: "cursor-opaque"},
	}); err == nil {
		t.Fatal("an opaque cursor against a non-paging engine was accepted")
	} else {
		assertOwnedCode(t, err, envelope.CodeInvalidArgument)
	}
}
