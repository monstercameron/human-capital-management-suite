package journeyclient

import (
	"context"
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/taskmux"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ---------------------------------------------------------------------
// A fake engine
// ---------------------------------------------------------------------

// fakeService is the whole cell, as far as these tests are concerned. Every
// method answers from a field, records what it was asked, and can be made to
// block so a test can look at the page while a call is in flight.
type fakeService struct {
	mu sync.Mutex

	list         []*journeyv1.Journey
	listErr      error
	detail       *journeyv1.JourneyDetail
	detailErr    error
	proposed     *journeyv1.Journey
	proposeErr   error
	executed     *journeyv1.JourneyDetail
	executeErr   error
	decided      *journeyv1.JourneyDetail
	decideErr    error
	edited       *journeyv1.EditProposalResponse
	editErr      error
	preview      *journeyv1.PreviewJourneyInterventionResponse
	previewErr   error
	intervened   *journeyv1.RequestJourneyInterventionResponse
	interveneErr error
	watchErr     error
	workers      []*journeyv1.Worker
	workforce    *journeyv1.WorkforceOptions
	workersErr   error
	created      *journeyv1.Worker
	createErr    error

	calls        []string
	proposeReq   *journeyv1.ProposeJourneyRequest
	decideReq    *journeyv1.DecideJourneyRequest
	inspectReq   *journeyv1.InspectJourneyRequest
	executeReq   *journeyv1.ExecuteJourneyRequest
	editReq      *journeyv1.EditProposalRequest
	previewReqs  []*journeyv1.PreviewJourneyInterventionRequest
	interveneReq *journeyv1.RequestJourneyInterventionRequest
	createReq    *journeyv1.CreateWorkerRequest
	watchReqs    []*journeyv1.WatchJourneyRequest
	streams      []*fakeStream
	streamOpened chan struct{}
	// instantEOF makes every stream end the moment it is opened, which is
	// how a server at its ceiling (or one with nothing to say to a client
	// that already holds the current digest) behaves.
	instantEOF bool

	// gate, when non-nil, blocks every unary call until it is closed.
	gate chan struct{}
}

func newFakeService() *fakeService {
	return &fakeService{streamOpened: make(chan struct{}, 8)}
}

func (f *fakeService) record(name string) {
	f.mu.Lock()
	f.calls = append(f.calls, name)
	gate := f.gate
	f.mu.Unlock()
	if gate != nil {
		<-gate
	}
}

func (f *fakeService) called(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c == name {
			n++
		}
	}
	return n
}

func (f *fakeService) ListJourneys(context.Context, *journeyv1.ListJourneysRequest) (*journeyv1.ListJourneysResponse, error) {
	f.record("ListJourneys")
	if f.listErr != nil {
		return nil, f.listErr
	}
	return &journeyv1.ListJourneysResponse{Journeys: f.list}, nil
}

func (f *fakeService) ProposeJourney(_ context.Context, in *journeyv1.ProposeJourneyRequest) (*journeyv1.ProposeJourneyResponse, error) {
	f.mu.Lock()
	f.proposeReq = in
	f.mu.Unlock()
	f.record("ProposeJourney")
	if f.proposeErr != nil {
		return nil, f.proposeErr
	}
	return &journeyv1.ProposeJourneyResponse{Journey: f.proposed}, nil
}

func (f *fakeService) InspectJourney(_ context.Context, in *journeyv1.InspectJourneyRequest) (*journeyv1.InspectJourneyResponse, error) {
	f.mu.Lock()
	f.inspectReq = in
	f.mu.Unlock()
	f.record("InspectJourney")
	if f.detailErr != nil {
		return nil, f.detailErr
	}
	return &journeyv1.InspectJourneyResponse{Detail: f.detail}, nil
}

func (f *fakeService) ExecuteJourney(_ context.Context, in *journeyv1.ExecuteJourneyRequest) (*journeyv1.ExecuteJourneyResponse, error) {
	f.mu.Lock()
	f.executeReq = in
	f.mu.Unlock()
	f.record("ExecuteJourney")
	if f.executeErr != nil {
		return nil, f.executeErr
	}
	return &journeyv1.ExecuteJourneyResponse{Detail: f.executed}, nil
}

func (f *fakeService) DecideJourney(_ context.Context, in *journeyv1.DecideJourneyRequest) (*journeyv1.DecideJourneyResponse, error) {
	f.mu.Lock()
	f.decideReq = in
	f.mu.Unlock()
	f.record("DecideJourney")
	if f.decideErr != nil {
		return nil, f.decideErr
	}
	return &journeyv1.DecideJourneyResponse{Detail: f.decided}, nil
}

func (f *fakeService) EditProposal(_ context.Context, in *journeyv1.EditProposalRequest) (*journeyv1.EditProposalResponse, error) {
	f.mu.Lock()
	f.editReq = in
	f.mu.Unlock()
	f.record("EditProposal")
	if f.editErr != nil {
		return nil, f.editErr
	}
	return f.edited, nil
}

func (f *fakeService) PreviewJourneyIntervention(_ context.Context, in *journeyv1.PreviewJourneyInterventionRequest) (*journeyv1.PreviewJourneyInterventionResponse, error) {
	f.mu.Lock()
	f.previewReqs = append(f.previewReqs, in)
	f.mu.Unlock()
	f.record("PreviewJourneyIntervention")
	if f.previewErr != nil {
		return nil, f.previewErr
	}
	return f.preview, nil
}

func (f *fakeService) RequestJourneyIntervention(_ context.Context, in *journeyv1.RequestJourneyInterventionRequest) (*journeyv1.RequestJourneyInterventionResponse, error) {
	f.mu.Lock()
	f.interveneReq = in
	f.mu.Unlock()
	f.record("RequestJourneyIntervention")
	if f.interveneErr != nil {
		return nil, f.interveneErr
	}
	return f.intervened, nil
}

// ListWorkers answers with the fake cell's current population.
//
// The slice is copied because CreateWorker below adds to it: a test that
// handed the client the same backing array it then appended to would be
// racing its own client.
func (f *fakeService) ListWorkers(context.Context, *journeyv1.ListWorkersRequest) (*journeyv1.ListWorkersResponse, error) {
	f.record("ListWorkers")
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.workersErr != nil {
		return nil, f.workersErr
	}
	return &journeyv1.ListWorkersResponse{
		Workers: append([]*journeyv1.Worker(nil), f.workers...),
		Options: f.workforce,
	}, nil
}

// CreateWorker records the employee the way the engine does: the new row
// joins the population, newest created first, so a client that re-reads the
// list after creating actually sees them.
func (f *fakeService) CreateWorker(_ context.Context, in *journeyv1.CreateWorkerRequest) (*journeyv1.CreateWorkerResponse, error) {
	f.mu.Lock()
	f.createReq = in
	f.mu.Unlock()
	f.record("CreateWorker")
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return nil, f.createErr
	}
	worker := f.created
	if worker == nil {
		worker = &journeyv1.Worker{
			WorkerRef: "worker:created-1", WorkerId: "created-1",
			LegalName: in.GetLegalName(), PreferredName: in.GetPreferredName(),
			WorkerNumber: "W-9001", JobCode: in.GetJobCode(), Grade: in.GetGrade(),
			OrgUnit: in.GetOrgUnit(), PositionId: in.GetPositionId(),
			Location: in.GetLocation(), PayZone: in.GetPayZone(),
			BasePay: in.GetBasePay(), Currency: in.GetCurrency(),
			BonusTarget: in.GetBonusTarget(), HireDate: in.GetHireDate(),
			Source: "CREATED",
		}
	}
	f.workers = append([]*journeyv1.Worker{worker}, f.workers...)
	return &journeyv1.CreateWorkerResponse{Worker: worker}, nil
}

func (f *fakeService) WatchJourney(ctx context.Context, in *journeyv1.WatchJourneyRequest) (WatchStream, error) {
	f.mu.Lock()
	f.calls = append(f.calls, "WatchJourney")
	f.watchReqs = append(f.watchReqs, in)
	err := f.watchErr
	stream := &fakeStream{
		ctx:      ctx,
		req:      in,
		updates:  make(chan *journeyv1.JourneyDetail, 4),
		finished: make(chan struct{}),
	}
	if err == nil {
		f.streams = append(f.streams, stream)
	}
	instant := f.instantEOF
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if instant {
		stream.finish()
	}
	select {
	case f.streamOpened <- struct{}{}:
	default:
	}
	return stream, nil
}

// awaitStream blocks until the client has opened its nth stream.
func (f *fakeService) awaitStream(t *testing.T, n int) *fakeStream {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		count := len(f.streams)
		var s *fakeStream
		if count >= n {
			s = f.streams[n-1]
		}
		f.mu.Unlock()
		if s != nil {
			return s
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for watch stream %d to be opened", n)
	return nil
}

// fakeStream is one WatchJourney call: it delivers whatever a test pushes
// into it and ends when the test finishes it or the client cancels.
type fakeStream struct {
	ctx      context.Context
	req      *journeyv1.WatchJourneyRequest
	updates  chan *journeyv1.JourneyDetail
	finished chan struct{}
	once     sync.Once
}

func (s *fakeStream) Recv() (*journeyv1.WatchJourneyResponse, error) {
	select {
	case detail := <-s.updates:
		return &journeyv1.WatchJourneyResponse{Detail: detail}, nil
	case <-s.finished:
		return nil, io.EOF
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	}
}

// finish ends the stream the way the server's fifteen-minute ceiling does.
func (s *fakeStream) finish() { s.once.Do(func() { close(s.finished) }) }

// ---------------------------------------------------------------------
// Harness
// ---------------------------------------------------------------------

type harness struct {
	app   *App
	store *journey.Store
	svc   *fakeService
}

// newHarness builds a client over a fake engine with real goroutines, which
// is how it runs in the browser: an answer arrives on a goroutine that is
// not the one that asked, and the assertions below wait for the page rather
// than assuming the call already finished.
func newHarness(t *testing.T) *harness {
	t.Helper()
	svc := newFakeService()
	store := journey.NewStore(journey.Page{})
	app := New(testConfig(), svc, store, func() time.Time {
		return time.Date(2026, 9, 3, 14, 5, 0, 0, time.UTC)
	})
	app.WatchRetry = 0
	return &harness{app: app, store: store, svc: svc}
}

// awaitPage waits for the store to hold a page satisfying cond.
func (h *harness) awaitPage(t *testing.T, what string, cond func(journey.Page) bool) journey.Page {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		p := h.store.Page()
		if cond(p) {
			return p
		}
		time.Sleep(time.Millisecond)
	}
	p := h.store.Page()
	t.Fatalf("timed out waiting for %s; page has list=%v proposal=%v detail=%v notice=%+v",
		what, p.List != nil, p.Proposal != nil, p.Detail != nil, p.Notice)
	return journey.Page{}
}

func listLoaded(p journey.Page) bool  { return p.List != nil && p.Notice == nil }
func detailShown(p journey.Page) bool { return p.Detail != nil && p.Detail.Journey.IntentID != "" }

func noticeTitled(title string) func(journey.Page) bool {
	return func(p journey.Page) bool { return p.Notice != nil && p.Notice.Title == title }
}

// ---------------------------------------------------------------------
// Routes and loads
// ---------------------------------------------------------------------

func TestStartLoadsTheListAndSeedsTheForm(t *testing.T) {
	h := newHarness(t)
	h.svc.list = []*journeyv1.Journey{testJourney(t, journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED)}

	h.app.Start(context.Background(), "")

	p := h.awaitPage(t, "the list", listLoaded)
	if len(p.List.Journeys) != 1 {
		t.Fatalf("the list shows %d journeys, want 1", len(p.List.Journeys))
	}
	if h.svc.called("ListJourneys") != 1 {
		t.Errorf("ListJourneys called %d times, want once", h.svc.called("ListJourneys"))
	}
	// The clock-dependent default lives in the store, not in the projection.
	if got, want := h.store.Values()[FieldEffective], "2026-12-01"; got != want {
		t.Errorf("seeded effective date = %q, want %q", got, want)
	}
	if got := p.List.Form.Fields[5].Value; got != "2026-12-01" {
		t.Errorf("the form's effective date = %q, want the seeded value", got)
	}
}

func TestAnInFlightCallSaysSo(t *testing.T) {
	h := newHarness(t)
	h.svc.gate = make(chan struct{})

	h.app.Start(context.Background(), "#/journeys")

	p := h.awaitPage(t, "the busy notice", noticeTitled("Working…"))
	if p.Notice.Tone != toneInfo || p.Notice.Detail == "" {
		t.Errorf("busy notice = %+v, want an informational notice that says what is happening", p.Notice)
	}
	close(h.svc.gate)
	h.awaitPage(t, "the answer", listLoaded)
}

func TestOpeningAJourneyReadsItAndWatchesIt(t *testing.T) {
	h := newHarness(t)
	h.svc.detail = testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_APPROVAL)

	h.app.Start(context.Background(), DetailHref(testIntentID))

	p := h.awaitPage(t, "the detail", detailShown)
	if p.Detail.Journey.IntentID != testIntentID {
		t.Errorf("the page shows journey %q, want %q", p.Detail.Journey.IntentID, testIntentID)
	}
	if h.svc.inspectReq.GetIntentId() != testIntentID {
		t.Errorf("InspectJourney asked for %q", h.svc.inspectReq.GetIntentId())
	}

	stream := h.svc.awaitStream(t, 1)
	if stream.req.GetIntentId() != testIntentID {
		t.Errorf("the watch asked for %q", stream.req.GetIntentId())
	}
	// The initial read's digest is carried so the server does not re-send
	// the detail the page is already showing.
	if stream.req.GetSinceDigest() != "sha256:aaaa1111" {
		t.Errorf("the watch since_digest = %q, want the digest the page already holds", stream.req.GetSinceDigest())
	}
}

func TestNavigatingAwayCancelsTheWatch(t *testing.T) {
	h := newHarness(t)
	h.svc.detail = testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_APPROVAL)
	h.app.Start(context.Background(), DetailHref(testIntentID))
	h.awaitPage(t, "the detail", detailShown)
	stream := h.svc.awaitStream(t, 1)

	h.app.Navigate(ListHref())
	h.awaitPage(t, "the list", listLoaded)

	select {
	case <-stream.ctx.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("the previous journey's watch was still running after the route changed")
	}
}

// TestALateAnswerDoesNotRedrawAPageTheReaderLeft is the stale-answer guard:
// the list call is held open, the reader opens a journey, and the list
// answer must be dropped rather than replacing the detail.
func TestALateAnswerDoesNotRedrawAPageTheReaderLeft(t *testing.T) {
	h := newHarness(t)
	h.svc.list = []*journeyv1.Journey{testJourney(t, journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED)}
	h.svc.detail = testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_APPROVAL)
	gate := make(chan struct{})
	h.svc.gate = gate

	h.app.Start(context.Background(), ListHref())
	h.awaitPage(t, "the busy notice", noticeTitled("Working…"))

	// The reader opens a journey while the list is still in flight. The
	// second call is not gated, so the detail arrives first.
	h.svc.mu.Lock()
	h.svc.gate = nil
	h.svc.mu.Unlock()
	h.app.Navigate(DetailHref(testIntentID))
	h.awaitPage(t, "the detail", detailShown)

	close(gate) // the list answer arrives now, for a route nobody is on
	time.Sleep(20 * time.Millisecond)
	if p := h.store.Page(); p.List != nil || p.Detail == nil {
		t.Error("a stale list answer replaced the journey the reader had opened")
	}
}

func TestNavigateUsesTheBrowsersAddressWhenItHasOne(t *testing.T) {
	h := newHarness(t)
	var located []string
	h.app.Locate = func(href string) { located = append(located, href) }

	h.app.Navigate(DetailHref("int_99"))

	if len(located) != 1 || located[0] != "#/journeys/int_99" {
		t.Fatalf("Locate got %v, want the detail fragment", located)
	}
	// Nothing is loaded until the browser's hashchange comes back, which is
	// what keeps the address bar and the page from disagreeing.
	if h.svc.called("InspectJourney") != 0 {
		t.Error("the client loaded a route before the browser's address changed")
	}
}

func TestNavigateLeavesDocumentLinksToTheBrowser(t *testing.T) {
	h := newHarness(t)
	var located []string
	h.app.Locate = func(href string) { located = append(located, href) }

	h.app.Navigate(WorkspacePath)

	if len(located) != 0 {
		t.Errorf("the client intercepted a document link: %v", located)
	}
	if h.svc.called("ListJourneys") != 0 {
		t.Error("a document link triggered a route load")
	}
}

// ---------------------------------------------------------------------
// Wiring
// ---------------------------------------------------------------------

func TestTheRenderedPageIsWired(t *testing.T) {
	h := newHarness(t)
	h.svc.list = []*journeyv1.Journey{testJourney(t, journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED)}
	h.app.Start(context.Background(), ListHref())
	p := h.awaitPage(t, "the list", listLoaded)

	if p.OnFieldChange == nil {
		t.Error("fields are not controlled; typing would not reach the store")
	}
	if p.List.Form.OnSubmit == nil {
		t.Error("the proposal form is not wired to the client")
	}
	if p.List.Journeys[0].OnOpen == nil {
		t.Error("journey cards are not wired to the router")
	}
	if p.Nav[1].OnNavigate == nil {
		t.Error("the journeys link is not wired as a live route change")
	}
	// The workspace link must stay an ordinary document link: it leaves this
	// client entirely, and intercepting it would strand the reader.
	if p.Nav[0].OnNavigate != nil {
		t.Error("the workspace link was intercepted by the client")
	}
	if p.Nav[0].Href != WorkspacePath {
		t.Errorf("the workspace link href = %q", p.Nav[0].Href)
	}
}

func TestEmbeddedClientUsesHostSoftwareNavigationForProductLinks(t *testing.T) {
	h := staffed(t)
	var navigated []string
	h.app.NavigateProduct = func(href string) { navigated = append(navigated, href) }
	h.app.Start(context.Background(), ProposalHref("jane-doe"))
	p := h.awaitPage(t, "the focused proposal", proposalFor("jane-doe"))
	if p.Proposal.BackNavigate == nil {
		t.Fatal("embedded proposal did not wire its profile exit")
	}
	p.Proposal.BackNavigate()
	if len(navigated) != 1 || navigated[0] != p.Proposal.BackHref {
		t.Fatalf("profile exit navigated to %v, want %q", navigated, p.Proposal.BackHref)
	}
}

func TestEmbeddedJourneyDirectoryHandoffUsesHostSoftwareNavigation(t *testing.T) {
	h := staffed(t)
	var navigated []string
	h.app.NavigateProduct = func(href string) { navigated = append(navigated, href) }
	h.app.Start(context.Background(), ListHref())
	p := h.awaitPage(t, "the list", listLoaded)
	if p.List.People == nil || p.List.People.DirectoryLink.OnNavigate == nil {
		t.Fatal("embedded workforce preview did not wire its People directory handoff")
	}
	p.List.People.DirectoryLink.OnNavigate()
	if p.List.People.DirectoryLink.Href != "/workspace/app/people" {
		t.Fatalf("directory href = %q", p.List.People.DirectoryLink.Href)
	}
	if len(navigated) != 1 || navigated[0] != p.List.People.DirectoryLink.Href {
		t.Fatalf("directory handoff navigated to %v, want %q", navigated, p.List.People.DirectoryLink.Href)
	}
}

func TestTypedValuesSurviveARedraw(t *testing.T) {
	h := newHarness(t)
	h.app.Start(context.Background(), ListHref())
	h.awaitPage(t, "the list", listLoaded)

	h.store.SetValue(FieldJobCode, "FIN-ANALYST3")
	h.app.OnHashChange(ListHref())
	p := h.awaitPage(t, "the reloaded list", listLoaded)

	if got := p.Values[FieldJobCode]; got != "FIN-ANALYST3" {
		t.Errorf("the typed job code = %q after a redraw, want it kept", got)
	}
	html, err := journey.RenderToString(p)
	if err != nil {
		t.Fatalf("rendering: %v", err)
	}
	if !strings.Contains(html, "FIN-ANALYST3") {
		t.Error("the typed value is not in the rendered form")
	}
}

// ---------------------------------------------------------------------
// Writes
// ---------------------------------------------------------------------

func TestProposeCreatesTheJourneyAndOpensIt(t *testing.T) {
	h := newHarness(t)
	h.svc.proposed = testJourney(t, journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED)
	h.svc.detail = testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED)
	h.app.Start(context.Background(), ListHref())
	h.awaitPage(t, "the list", listLoaded)

	h.app.Submit(ActionPropose, map[string]string{
		NameWorker:    "omar-reyes",
		NameJobCode:   "OPS-HRBP3",
		NameGrade:     "P3",
		NamePosition:  "POS-HRBP-301",
		NameBase:      "98000.00",
		NameEffective: "2026-12-01",
		NameReason:    "promotion_into_senior_hrbp",
	})

	h.awaitPage(t, "the new journey", detailShown)

	req := h.svc.proposeReq
	if req.GetWorkerRef() != "omar-reyes" || req.GetProposedBase() != "98000.00" ||
		req.GetEffectiveDate() != "2026-12-01" || req.GetBusinessReason() != "promotion_into_senior_hrbp" {
		t.Errorf("ProposeJourney request = %+v", req)
	}
	if req.GetTarget().GetJobCode() != "OPS-HRBP3" || req.GetTarget().GetGrade() != "P3" ||
		req.GetTarget().GetPositionId() != "POS-HRBP-301" {
		t.Errorf("the target placement = %+v", req.GetTarget())
	}
	if h.svc.called("InspectJourney") != 1 {
		t.Error("the client did not read the journey it had just created")
	}
}

func TestTaskMuxSuppressesADuplicateProposalClick(t *testing.T) {
	h := newHarness(t)
	h.svc.proposed = testJourney(t, journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED)
	h.svc.detail = testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED)
	h.app.Tasks = taskmux.New(taskmux.Options{MaxRunning: 2, MaxQueued: 8})
	h.app.Start(context.Background(), ListHref())
	h.awaitPage(t, "the list", listLoaded)

	h.svc.gate = make(chan struct{})
	values := map[string]string{
		NameWorker: "omar-reyes", NameJobCode: "OPS-HRBP3", NameGrade: "P3",
		NamePosition: "POS-HRBP-301", NameBase: "98000.00",
		NameEffective: "2026-12-01", NameReason: "promotion_into_senior_hrbp",
	}
	h.app.Submit(ActionPropose, values)
	h.app.Submit(ActionPropose, values)

	deadline := time.Now().Add(time.Second)
	for h.svc.called("ProposeJourney") == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := h.svc.called("ProposeJourney"); got != 1 {
		t.Fatalf("ProposeJourney called %d times while the first click was in flight, want once", got)
	}
	close(h.svc.gate)
	h.awaitPage(t, "the new journey", detailShown)
}

func TestProposeWithoutAWorkerAsksNothing(t *testing.T) {
	h := newHarness(t)
	h.app.Start(context.Background(), ListHref())
	h.awaitPage(t, "the list", listLoaded)

	h.app.Submit(ActionPropose, map[string]string{NameWorker: "  ", NameBase: "98000.00"})

	h.awaitPage(t, "the refusal", noticeTitled("Choose a worker"))
	if h.svc.called("ProposeJourney") != 0 {
		t.Error("a proposal with no worker was sent to the engine anyway")
	}
}

func TestProposeWithMissingRequiredFieldsStaysInTheBrowser(t *testing.T) {
	h := newHarness(t)
	h.app.Start(context.Background(), ListHref())
	h.awaitPage(t, "the list", listLoaded)

	h.app.Submit(ActionPropose, map[string]string{NameWorker: "omar-reyes", NameEffective: "2026-12-01"})

	p := h.awaitPage(t, "the refusal", noticeTitled("Complete the required fields"))
	for _, field := range []string{"target job code", "target grade", "target position", "proposed base pay", "business reason"} {
		if !strings.Contains(p.Notice.Detail, field) {
			t.Errorf("missing-fields notice %q does not name %q", p.Notice.Detail, field)
		}
	}
	if h.svc.called("ProposeJourney") != 0 {
		t.Error("an incomplete proposal was sent to the engine")
	}
}

func TestExecuteAdmitsThePlan(t *testing.T) {
	h := newHarness(t)
	h.svc.detail = testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED)
	h.svc.executed = testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_APPROVAL)
	h.app.Start(context.Background(), DetailHref(testIntentID))
	h.awaitPage(t, "the detail", detailShown)

	h.app.Submit(ActionExecute, map[string]string{})

	p := h.awaitPage(t, "the admission", noticeTitled("The plan was admitted"))
	if p.Notice.Tone != toneSuccess {
		t.Errorf("notice tone = %q, want success", p.Notice.Tone)
	}
	if p.Detail.Journey.Stage != stageAwaitingApproval {
		t.Errorf("the page still shows stage %q", p.Detail.Journey.Stage)
	}
	if h.svc.executeReq.GetIntentId() != testIntentID {
		t.Errorf("ExecuteJourney asked for %q", h.svc.executeReq.GetIntentId())
	}
}

func TestApproveCompletesTheJourney(t *testing.T) {
	h := newHarness(t)
	h.svc.detail = testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_APPROVAL)
	h.svc.decided = testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_COMPLETED)
	h.app.Start(context.Background(), DetailHref(testIntentID))
	h.awaitPage(t, "the detail", detailShown)

	h.app.Submit(ActionApprove, map[string]string{NameDecisionReason: "Scope and budget both check out."})

	p := h.awaitPage(t, "the approval", noticeTitled("Promotion recorded"))
	if !h.svc.decideReq.GetApprove() {
		t.Error("the decision was not an approval")
	}
	if h.svc.decideReq.GetReason() != "Scope and budget both check out." {
		t.Errorf("the reason = %q", h.svc.decideReq.GetReason())
	}
	if p.Detail.Ledger == nil {
		t.Error("the completed journey shows no ledger fact")
	}
	for _, a := range p.Detail.Actions {
		if a.ID == ActionApprove || a.ID == ActionReject || a.ID == ActionExecute {
			t.Errorf("a completed journey still offers a decision: %+v", a)
		}
		// PROMOUX-013: Withdraw/Cancel/EditProposal still render on a
		// terminal journey, but only as Disabled actions explaining why --
		// never as something the reader could actually submit.
		if !a.Disabled {
			t.Errorf("action %q is not disabled on a terminal (COMPLETED) journey: %+v", a.ID, a)
		}
	}
}

func TestIntermediateApprovalNeverClaimsATerminalWrite(t *testing.T) {
	for _, tc := range []struct {
		name  string
		stage journeyv1.JourneyStage
		title string
	}{
		{"manager approval", journeyv1.JourneyStage_JOURNEY_STAGE_MANAGER_APPROVAL, "Approval recorded"},
		{"effective-date wait", journeyv1.JourneyStage_JOURNEY_STAGE_WAITING_EFFECTIVE_DATE, "Approvals complete"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.svc.detail = testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_FINANCE_APPROVAL)
			h.svc.decided = testDetail(t, tc.stage)
			h.app.Start(context.Background(), DetailHref(testIntentID))
			h.awaitPage(t, "the finance approval", detailShown)
			h.store.SetValue(FieldApproveReason, "Finance rationale")

			h.app.Submit(ActionApprove, map[string]string{NameDecisionReason: "Finance rationale"})
			p := h.awaitPage(t, "the intermediate outcome", noticeTitled(tc.title))
			if strings.Contains(strings.ToLower(p.Notice.Detail), "terminal node recorded") {
				t.Fatalf("intermediate notice overclaimed a ledger write: %+v", p.Notice)
			}
			if p.Detail.Ledger != nil {
				t.Fatal("intermediate detail unexpectedly contains a ledger fact")
			}
			if got := h.store.Values()[FieldApproveReason]; got != "" {
				t.Errorf("approval rationale leaked to the next stage as %q", got)
			}
		})
	}
}

func TestChangingProposalSubjectClearsConsequentialDraftValues(t *testing.T) {
	h := staffed(t)
	h.app.Start(context.Background(), ProposalHref("jane-doe"))
	h.awaitPage(t, "Jane's proposal", proposalFor("jane-doe"))
	for field, value := range map[string]string{
		FieldJobCode: "ENG-MGR1", FieldGrade: "P3", FieldPosition: "POS-MGR-1",
		FieldBase: "180000.00", FieldReason: "Jane-specific rationale",
	} {
		h.store.SetValue(field, value)
	}

	h.app.OnHashChange(ProposalHref("omar-reyes"))
	h.awaitPage(t, "Omar's proposal", proposalFor("omar-reyes"))
	values := h.store.Values()
	for _, field := range []string{FieldJobCode, FieldGrade, FieldPosition, FieldBase, FieldReason} {
		if values[field] != "" {
			t.Errorf("%s leaked from Jane to Omar as %q", field, values[field])
		}
	}
	if values[FieldWorker] != "omar-reyes" {
		t.Errorf("worker = %q, want Omar", values[FieldWorker])
	}
	if values[FieldEffective] != "2026-12-01" {
		t.Errorf("effective-date default = %q", values[FieldEffective])
	}
}

func TestProposalRefusesAnUnpublishedJobGradeCombination(t *testing.T) {
	h := staffed(t)
	h.app.Start(context.Background(), ProposalHref("omar-reyes"))
	h.awaitPage(t, "Omar's proposal", proposalFor("omar-reyes"))

	h.app.Submit(ActionPropose, map[string]string{
		NameWorker: "omar-reyes", NameJobCode: "OPS-HRBP3", NameGrade: "P2",
		NamePosition: "POS-HRBP-301", NameBase: "98000.00",
		NameEffective: "2026-12-01", NameReason: "Invalid cross-product",
	})
	p := h.awaitPage(t, "the governed-target refusal", noticeTitled("Choose a governed target role"))
	if !strings.Contains(p.Notice.Detail, "published next step") || !strings.Contains(p.Notice.Detail, "pay zone and currency") {
		t.Errorf("notice = %+v", p.Notice)
	}
	if h.svc.called("ProposeJourney") != 0 {
		t.Fatal("the unsupported target was sent to the engine")
	}
}

func TestProposalRefusesAPayableButUnrelatedRole(t *testing.T) {
	h := staffed(t)
	h.app.Start(context.Background(), ProposalHref("omar-reyes"))
	h.awaitPage(t, "Omar's proposal", proposalFor("omar-reyes"))

	h.app.Submit(ActionPropose, map[string]string{
		NameWorker: "omar-reyes", NameJobCode: "CLN-NURSE4", NameGrade: "N4",
		NamePosition: "POS-NURSE-401", NameBase: "100000.00",
		NameEffective: "2026-12-01", NameReason: "A pay band is not a ladder edge",
	})
	h.awaitPage(t, "the ladder refusal", noticeTitled("Choose a governed target role"))
	if h.svc.called("ProposeJourney") != 0 {
		t.Fatal("a payable but unrelated role was sent to the engine")
	}
}

func TestTaskMuxSerializesMutuallyExclusiveDecisions(t *testing.T) {
	h := newHarness(t)
	h.svc.detail = testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_APPROVAL)
	h.svc.decided = testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_COMPLETED)
	h.app.Tasks = taskmux.New(taskmux.Options{MaxRunning: 2, MaxQueued: 8})
	h.app.Start(context.Background(), DetailHref(testIntentID))
	h.awaitPage(t, "the detail", detailShown)

	h.svc.gate = make(chan struct{})
	h.app.Submit(ActionApprove, map[string]string{NameDecisionReason: "Approved first."})
	h.app.Submit(ActionReject, map[string]string{NameDecisionReason: "Conflicting second click."})

	deadline := time.Now().Add(time.Second)
	for h.svc.called("DecideJourney") == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := h.svc.called("DecideJourney"); got != 1 {
		t.Fatalf("DecideJourney called %d times, want one mutually exclusive decision", got)
	}
	if !h.svc.decideReq.GetApprove() {
		t.Fatal("the first decision was not preserved")
	}
	close(h.svc.gate)
	h.awaitPage(t, "the approval", noticeTitled("Promotion recorded"))
}

func TestRejectNeedsAReasonAndSendsIt(t *testing.T) {
	h := newHarness(t)
	h.svc.detail = testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_APPROVAL)
	h.svc.decided = testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_REJECTED)
	h.app.Start(context.Background(), DetailHref(testIntentID))
	h.awaitPage(t, "the detail", detailShown)

	h.app.Submit(ActionReject, map[string]string{NameDecisionReason: "   "})
	h.awaitPage(t, "the refusal", noticeTitled("Say what needs to change"))
	if h.svc.called("DecideJourney") != 0 {
		t.Fatal("a rejection with no reason was sent to the engine")
	}

	h.app.Submit(ActionReject, map[string]string{NameDecisionReason: "The budget line is not approved yet."})
	h.awaitPage(t, "the rejection", noticeTitled("Returned to the manager"))
	if h.svc.decideReq.GetApprove() {
		t.Error("the decision was recorded as an approval")
	}
	if h.svc.decideReq.GetReason() != "The budget line is not approved yet." {
		t.Errorf("the reason = %q", h.svc.decideReq.GetReason())
	}
}

func TestAnUnknownActionSaysSoRatherThanDoingNothing(t *testing.T) {
	h := newHarness(t)
	h.app.Start(context.Background(), ListHref())
	h.awaitPage(t, "the list", listLoaded)

	h.app.Submit("teleport", map[string]string{})

	p := h.awaitPage(t, "the refusal", noticeTitled("That action is not available"))
	if p.Notice.Tone != toneDanger {
		t.Errorf("notice tone = %q, want danger", p.Notice.Tone)
	}
}

// ---------------------------------------------------------------------
// Refusals
// ---------------------------------------------------------------------

func TestARefusalBecomesTheNotice(t *testing.T) {
	h := newHarness(t)
	h.svc.listErr = status.Error(codes.PermissionDenied, "the caller may not read journeys in this tenant")

	h.app.Start(context.Background(), ListHref())

	p := h.awaitPage(t, "the refusal", noticeTitled("Refused"))
	if p.Notice.Tone != toneDanger {
		t.Errorf("notice tone = %q, want danger", p.Notice.Tone)
	}
	if !strings.Contains(p.Notice.Detail, "may not read journeys") {
		t.Errorf("notice detail = %q, want the engine's own message", p.Notice.Detail)
	}
	// The chrome survives a refusal, so the reader still has a way out.
	if len(p.Nav) != 2 {
		t.Error("a refusal took the navigation with it")
	}
}

func TestNoticeFromErrorCarriesTheOwnedErrorModelsViolations(t *testing.T) {
	st, err := status.New(codes.InvalidArgument, "the proposal is not valid").WithDetails(&commonv1.ErrorDetail{
		FieldViolations: []*commonv1.FieldViolation{
			{FieldPath: "effective_date", Description: "must not be in the past", RuleRef: "promotion.effective_date"},
			{FieldPath: "proposed_base", Description: "outside the target grade's band"},
		},
		CorrelationId: "cor_01JX6Y8B2C7D9EFG",
	})
	if err != nil {
		t.Fatalf("building the status: %v", err)
	}

	notice := NoticeFromError(st.Err())

	if notice.Title != "That proposal is not valid" {
		t.Errorf("title = %q", notice.Title)
	}
	for _, want := range []string{
		"the proposal is not valid",
		"effective_date: must not be in the past (promotion.effective_date)",
		"proposed_base: outside the target grade's band",
		"Correlation: cor_01JX6Y8B2C7D9EFG",
	} {
		if !strings.Contains(notice.Detail, want) {
			t.Errorf("notice detail %q does not carry %q", notice.Detail, want)
		}
	}
}

func TestNoticeTitlesReadAsSentencesNotCodes(t *testing.T) {
	cases := map[codes.Code]string{
		codes.PermissionDenied:   "Refused",
		codes.Unauthenticated:    "This page is no longer signed in",
		codes.NotFound:           "No such journey",
		codes.InvalidArgument:    "That proposal is not valid",
		codes.FailedPrecondition: "Not available at this stage",
		codes.Unavailable:        "The cell is not answering",
		codes.DeadlineExceeded:   "The engine did not answer in time",
		codes.Internal:           "The engine refused this",
	}
	for code, want := range cases {
		notice := NoticeFromError(status.Error(code, "message"))
		if notice.Title != want {
			t.Errorf("title for %s = %q, want %q", code, notice.Title, want)
		}
		if strings.Contains(notice.Title, "_") || strings.ToUpper(notice.Title) == notice.Title {
			t.Errorf("title for %s reads as a code: %q", code, notice.Title)
		}
	}
	if NoticeFromError(nil) != nil {
		t.Error("NoticeFromError(nil) invented a notice")
	}
}

func TestAJourneyThatCannotBeReadStillRendersItsChrome(t *testing.T) {
	h := newHarness(t)
	h.svc.detailErr = status.Error(codes.NotFound, "no such journey in this tenant")

	h.app.Start(context.Background(), DetailHref("int_missing"))

	p := h.awaitPage(t, "the refusal", noticeTitled("This journey is unavailable"))
	if p.Detail == nil {
		t.Fatal("the page rendered nothing at all")
	}
	if h.svc.called("WatchJourney") != 0 {
		t.Error("the client watched a journey it could not read")
	}
	html, err := journey.RenderToString(p)
	if err != nil {
		t.Fatalf("rendering: %v", err)
	}
	if strings.Contains(html, "no such journey in this tenant") || !strings.Contains(html, "cannot be opened in this session") {
		t.Error("the rendered page exposed the engine's existence-bearing refusal")
	}
}

func TestTodo_WEB_030_Security(t *testing.T) {
	notFound := routeReadNotice(status.Error(codes.NotFound, "intent-secret exists in tenant-a"))
	denied := routeReadNotice(status.Error(codes.PermissionDenied, "principal-b lacks intent-secret"))
	stale := routeReadNotice(status.Error(codes.FailedPrecondition, "intent-secret is stale at version 7"))
	if !reflect.DeepEqual(notFound, denied) || !reflect.DeepEqual(denied, stale) {
		t.Fatalf("route refusal disclosed resource disposition: not-found=%+v denied=%+v stale=%+v", notFound, denied, stale)
	}
	for _, secret := range []string{"intent-secret", "tenant-a", "principal-b", "version 7", "not found", "denied"} {
		if strings.Contains(strings.ToLower(notFound.Title+" "+notFound.Detail), strings.ToLower(secret)) {
			t.Fatalf("safe route refusal leaked %q: %+v", secret, notFound)
		}
	}
}

// ---------------------------------------------------------------------
// The workforce
// ---------------------------------------------------------------------

// staffed is a harness whose fake cell knows the fixture population.
func staffed(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	h.svc.workers = testWorkers()
	h.svc.workforce = testWorkforceOptions()
	return h
}

func peopleShown(p journey.Page) bool {
	return p.List != nil && p.List.People != nil && p.Notice == nil
}

func selectedIs(ref string) func(journey.Page) bool {
	return func(p journey.Page) bool {
		return p.List != nil && p.List.People != nil && p.List.People.SelectedRef == ref
	}
}

func proposalFor(ref string) func(journey.Page) bool {
	return func(p journey.Page) bool {
		return p.Proposal != nil && p.Proposal.Subject != nil && p.Proposal.Subject.Ref == ref && p.Notice == nil
	}
}

func fieldByID(fields []journey.Field, id string) (journey.Field, bool) {
	for _, f := range fields {
		if f.ID == id {
			return f, true
		}
	}
	return journey.Field{}, false
}

// TestStartLoadsTheWorkforceWithTheJourneys is the list route's whole read:
// two calls, made together, drawn once.
func TestStartLoadsTheWorkforceWithTheJourneys(t *testing.T) {
	h := staffed(t)
	h.svc.list = []*journeyv1.Journey{testJourney(t, journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED)}

	h.app.Start(context.Background(), "")

	p := h.awaitPage(t, "the list and its workforce", peopleShown)
	if len(p.List.Journeys) != 1 {
		t.Errorf("the list shows %d journeys, want 1", len(p.List.Journeys))
	}
	if len(p.List.People.Workers) != 3 {
		t.Errorf("People shows %d employees, want 3", len(p.List.People.Workers))
	}
	if h.svc.called("ListWorkers") != 1 || h.svc.called("ListJourneys") != 1 {
		t.Errorf("calls = %d ListWorkers, %d ListJourneys, want one each",
			h.svc.called("ListWorkers"), h.svc.called("ListJourneys"))
	}
}

// TestSafeSelectDefaultsAreSeeded is the live-path contract with the
// renderer: catalog-backed employee fields get valid defaults, while the
// consequential target grade deliberately stays on its required prompt.
func TestSafeSelectDefaultsAreSeeded(t *testing.T) {
	h := staffed(t)
	h.app.Start(context.Background(), ListHref())
	h.awaitPage(t, "the list and its workforce", peopleShown)

	values := h.store.Values()
	for id, want := range map[string]string{
		FieldEffective:         "2026-12-01",
		FieldWorkerJobCode:     "CLN-NURSE4",
		FieldWorkerGrade:       "M1",
		FieldWorkerOrgUnit:     "eng-platform",
		FieldWorkerPayZone:     "US-EAST",
		FieldWorkerPosition:    "POS-HRBP-204",
		FieldWorkerBonusTarget: DefaultBonusTarget,
		FieldWorkerHireDate:    "2026-10-01",
	} {
		if values[id] != want {
			t.Errorf("the seeded value for %s = %q, want %q", id, values[id], want)
		}
	}
	if got := values[FieldGrade]; got != "" {
		t.Errorf("the promotion target grade was preselected as %q, want explicit review", got)
	}
	// The currency this cell declared travels as a hidden input carrying the
	// cell's own answer; there is nothing for the reader to choose and
	// therefore nothing to seed.
	if got, ok := values[FieldWorkerCurrency]; ok {
		t.Errorf("the currency was seeded as %q, want it left to the hidden field", got)
	}
	if got := values[FieldWorker]; got != "" {
		t.Errorf("a worker was selected before anyone picked one: %q", got)
	}
}

// TestTheWorkforcePanelIsWired pins that every affordance on the panel
// reaches this client rather than the browser's own form post.
func TestTheWorkforcePanelIsWired(t *testing.T) {
	h := staffed(t)
	h.app.Start(context.Background(), ListHref())
	p := h.awaitPage(t, "the workforce", peopleShown)

	if p.List.People.Form.OnSubmit == nil {
		t.Error("the new-employee form is not wired to the client")
	}
	for i, row := range p.List.People.Workers {
		if row.OnSelect == nil || row.OnPropose == nil {
			t.Errorf("row %d (%s) is not wired: select=%v propose=%v",
				i, row.Ref, row.OnSelect != nil, row.OnPropose != nil)
		}
		if row.ProposeHref == "" {
			t.Errorf("row %d has no address to fall back to with no client", i)
		}
	}
}

// TestPickingAnEmployeeIsNotAReload is the whole point of holding the
// answers: moving the selection re-projects what is already here.
func TestPickingAnEmployeeIsNotAReload(t *testing.T) {
	h := staffed(t)
	h.app.Start(context.Background(), ListHref())
	p := h.awaitPage(t, "the workforce", peopleShown)

	p.List.People.Workers[1].OnSelect() // jane-doe

	p = h.awaitPage(t, "the selection", selectedIs("jane-doe"))
	if h.svc.called("ListWorkers") != 1 || h.svc.called("ListJourneys") != 1 {
		t.Error("picking an employee re-read the tenant")
	}
	if got := h.store.Values()[FieldWorker]; got != "jane-doe" {
		t.Errorf("the proposal form's worker = %q, want the picked employee", got)
	}
	worker, ok := fieldByID(p.List.Form.Fields, FieldWorker)
	if !ok {
		t.Fatal("the proposal form has no worker field")
	}
	if worker.Value != "jane-doe" {
		t.Errorf("the worker select shows %q", worker.Value)
	}
	selected := ""
	for _, o := range worker.Options {
		if o.Selected {
			selected = o.Value
		}
	}
	if selected != "jane-doe" {
		t.Errorf("the worker select marks %q as chosen", selected)
	}
	// The route says so too, which is what makes a selection linkable.
	if got := Href(Route{Kind: RouteList, WorkerRef: "jane-doe"}); got != "#/journeys?worker=jane-doe" {
		t.Errorf("the selection address = %q", got)
	}
}

// TestPickingAnEmployeeMovesTheAddressBarWithoutReloading covers the browser
// half: the client writes the address, the browser answers with a
// hashchange, and that hashchange must not re-run the route it describes.
func TestPickingAnEmployeeMovesTheAddressBarWithoutReloading(t *testing.T) {
	h := staffed(t)
	var located []string
	h.app.Locate = func(href string) { located = append(located, href) }
	h.app.Start(context.Background(), ListHref())
	p := h.awaitPage(t, "the workforce", peopleShown)

	p.List.People.Workers[1].OnSelect()
	h.awaitPage(t, "the selection", selectedIs("jane-doe"))

	if len(located) != 1 || located[0] != "#/journeys?worker=jane-doe" {
		t.Fatalf("the address bar got %v, want the selection route once", located)
	}
	h.app.OnHashChange(located[0]) // the browser's own event, for our own write
	if h.svc.called("ListWorkers") != 1 {
		t.Error("the client's own address change came back as a fresh load")
	}
	if !selectedIs("jane-doe")(h.store.Page()) {
		t.Error("the selection was lost when the browser reported the address change")
	}
}

// TestAnAddressWithAnEmployeeSelectsThem is the other direction: a link, a
// bookmark or a reload arrives with the selection already in the address.
func TestAnAddressWithAnEmployeeSelectsThem(t *testing.T) {
	h := staffed(t)
	h.app.Start(context.Background(), "#/journeys?worker=omar-reyes")

	p := h.awaitPage(t, "the selection", selectedIs("omar-reyes"))
	if got := h.store.Values()[FieldWorker]; got != "omar-reyes" {
		t.Errorf("the proposal form's worker = %q", got)
	}
	row, ok := cardFor(p.List.People, "omar-reyes")
	if !ok || !row.Selected {
		t.Error("the addressed employee's row is not marked")
	}
}

// TestProposeForOpensAFocusedTransactionWithoutWriting covers the row
// action: it leaves the global overview, fixes the chosen person as the
// transaction subject, and still makes no write until the form is submitted.
func TestProposeForOpensAFocusedTransactionWithoutWriting(t *testing.T) {
	h := staffed(t)
	h.app.Start(context.Background(), ListHref())
	p := h.awaitPage(t, "the workforce", peopleShown)

	p.List.People.Workers[0].OnPropose() // the created employee

	focused := h.awaitPage(t, "the focused proposal", proposalFor(testCreatedRef))
	if focused.List != nil || focused.Proposal.Form.Fields[0].Kind != kindHidden || focused.Proposal.Form.Fields[0].Value != testCreatedRef {
		t.Fatalf("focused proposal = %+v", focused.Proposal)
	}
	if h.svc.called("ProposeJourney") != 0 {
		t.Error("a row action proposed a promotion nobody had described")
	}
}

// TestAFocusedRouteStartsWithNoTargetDecision proves the live app path—not
// only the pure projector—leaves consequential promotion fields for explicit
// review when a profile deep-links directly into the workflow.
func TestAFocusedRouteStartsWithNoTargetDecision(t *testing.T) {
	h := staffed(t)
	h.app.Start(context.Background(), ProposalHref("jane-doe"))
	h.awaitPage(t, "the focused proposal", proposalFor("jane-doe"))

	values := h.store.Values()
	for _, id := range []string{FieldJobCode, FieldGrade, FieldPosition, FieldBase, FieldReason} {
		if got := values[id]; got != "" {
			t.Errorf("%s was prefilled as %q, want an explicit decision", id, got)
		}
	}
}

// TestProposeFallsBackToTheSelection covers a submission that carried no
// worker of its own -- the selection is what the page says the proposal is
// about, so it is what the engine is asked about.
func TestProposeFallsBackToTheSelection(t *testing.T) {
	h := staffed(t)
	h.svc.proposed = testJourney(t, journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED)
	h.svc.detail = testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED)
	h.app.Start(context.Background(), "#/journeys?worker=omar-reyes")
	h.awaitPage(t, "the selection", selectedIs("omar-reyes"))

	h.app.Submit(ActionPropose, map[string]string{
		NameJobCode: "OPS-HRBP3", NameGrade: "P3", NameBase: "98000.00",
		NamePosition:  "POS-HRBP-301",
		NameEffective: "2026-12-01", NameReason: "promotion_into_senior_hrbp",
	})

	h.awaitPage(t, "the new journey", detailShown)
	if got := h.svc.proposeReq.GetWorkerRef(); got != "omar-reyes" {
		t.Errorf("ProposeJourney asked about %q, want the selected employee", got)
	}
}

// ---------------------------------------------------------------------
// Recording an employee
// ---------------------------------------------------------------------

func createWorkerValues() map[string]string {
	return map[string]string{
		NameLegalName:      "Nadia Rahman",
		NamePreferredName:  "Nadia",
		NameWorkerJobCode:  "OPS-HRBP2",
		NameWorkerGrade:    "P2",
		NameOrgUnit:        "people-ops",
		NameWorkerPosition: "POS-NEW-001",
		NameLocation:       "Lisbon, PT",
		NamePayZone:        "US-EAST",
		NameWorkerBasePay:  "88000.00",
		NameCurrency:       "USD",
		NameBonusTarget:    "0.0500",
		NameHireDate:       "2026-10-01",
		NameManagerRef:     "worker:NW-40092",
	}
}

func TestCreateWorkerRecordsSelectsAndRefreshes(t *testing.T) {
	h := staffed(t)
	h.svc.created = &journeyv1.Worker{
		WorkerRef: "worker:created-nadia", WorkerId: "created-nadia",
		LegalName: "Nadia Rahman", PreferredName: "Nadia", WorkerNumber: "W-9001",
		JobCode: "OPS-HRBP2", Grade: "P2", OrgUnit: "people-ops", PositionId: "POS-NEW-001",
		Location: "Lisbon, PT", PayZone: "US-EAST", BasePay: "88000.00", Currency: "USD",
		BonusTarget: "0.0500", HireDate: "2026-10-01", Source: "CREATED",
	}
	h.app.Start(context.Background(), ListHref())
	h.awaitPage(t, "the workforce", peopleShown)

	h.app.Submit(ActionCreateWorker, createWorkerValues())

	p := h.awaitPage(t, "the new employee", noticeTitled("Employee added"))
	if p.Notice.Tone != toneSuccess {
		t.Errorf("notice tone = %q, want success", p.Notice.Tone)
	}
	if !strings.Contains(p.Notice.Detail, "Nadia") ||
		!strings.Contains(p.Notice.Detail, "propose their promotion below") {
		t.Errorf("notice detail = %q, want it to name the employee and the next step", p.Notice.Detail)
	}

	req := h.svc.createReq
	if req.GetLegalName() != "Nadia Rahman" || req.GetJobCode() != "OPS-HRBP2" ||
		req.GetBasePay() != "88000.00" || req.GetHireDate() != "2026-10-01" ||
		req.GetPayZone() != "US-EAST" || req.GetCurrency() != "USD" ||
		req.GetBonusTarget() != "0.0500" || req.GetManagerRef() != "worker:NW-40092" ||
		req.GetPositionId() != "POS-NEW-001" || req.GetLocation() != "Lisbon, PT" ||
		req.GetOrgUnit() != "people-ops" || req.GetGrade() != "P2" ||
		req.GetPreferredName() != "Nadia" {
		t.Errorf("CreateWorker request = %+v", req)
	}

	// The workforce is re-read rather than appended to in the browser, so
	// the page shows the cell's own answer.
	if h.svc.called("ListWorkers") != 2 {
		t.Errorf("ListWorkers called %d times, want the initial read and the refresh", h.svc.called("ListWorkers"))
	}
	if len(p.List.People.Workers) != 4 {
		t.Fatalf("People shows %d employees after the write, want 4", len(p.List.People.Workers))
	}
	if p.List.People.Workers[0].Ref != "worker:created-nadia" {
		t.Errorf("the newest employee is %q, want the one just recorded", p.List.People.Workers[0].Ref)
	}
	if p.List.People.SelectedRef != "worker:created-nadia" {
		t.Errorf("the selection = %q, want the new employee", p.List.People.SelectedRef)
	}
	if got := h.store.Values()[FieldWorker]; got != "worker:created-nadia" {
		t.Errorf("the proposal form's worker = %q, want the new employee", got)
	}
	// The form is empty again, with the cell's defaults back in it, so the
	// next employee is typed into a blank form rather than edited out of the
	// last one.
	name, _ := fieldByID(p.List.People.Form.Fields, FieldWorkerLegalName)
	if name.Value != "" {
		t.Errorf("the new-employee form still carries %q", name.Value)
	}
	if got := h.store.Values()[FieldWorkerJobCode]; got != "CLN-NURSE4" {
		t.Errorf("the job code default did not come back: %q", got)
	}

	// They are proposable immediately: the picker is the answer the cell just
	// gave, not a list this client keeps.
	worker, _ := fieldByID(p.List.Form.Fields, FieldWorker)
	found := false
	for _, o := range worker.Options {
		if o.Value == "worker:created-nadia" {
			found = o.Selected
		}
	}
	if !found {
		t.Error("the new employee is not offered, or not chosen, in the proposal form")
	}
}

// TestCreateWorkerInputErrorLandsOnTheField is the whole reason the owned
// error model carries a field path: the reader is told which control to fix,
// beside that control, with what they typed still in it.
func TestCreateWorkerInputErrorLandsOnTheField(t *testing.T) {
	h := staffed(t)
	st, err := status.New(codes.InvalidArgument, "the employee cannot be recorded").WithDetails(&commonv1.ErrorDetail{
		FieldViolations: []*commonv1.FieldViolation{
			{FieldPath: "base_pay", Description: "must be greater than zero", RuleRef: "workforce.base_pay"},
		},
		CorrelationId: "cor_01JX6Y8B2C7D9EFG",
	})
	if err != nil {
		t.Fatalf("building the status: %v", err)
	}
	h.svc.createErr = st.Err()
	h.app.Start(context.Background(), ListHref())
	h.awaitPage(t, "the workforce", peopleShown)

	values := createWorkerValues()
	values[NameWorkerBasePay] = "0"
	h.app.Submit(ActionCreateWorker, values)

	p := h.awaitPage(t, "the refusal", noticeTitled("That employee is not valid"))
	if p.Notice.Tone != toneDanger {
		t.Errorf("notice tone = %q, want danger", p.Notice.Tone)
	}
	if !strings.Contains(p.Notice.Detail, "must be greater than zero") {
		t.Errorf("notice detail = %q, want the engine's own violation", p.Notice.Detail)
	}

	pay, ok := fieldByID(p.List.People.Form.Fields, FieldWorkerBasePay)
	if !ok {
		t.Fatal("the new-employee form has no base pay field")
	}
	if pay.Error != "must be greater than zero" {
		t.Errorf("the base pay field's error = %q", pay.Error)
	}
	if pay.Value != "0" {
		t.Errorf("the base pay field = %q, want what the reader submitted", pay.Value)
	}
	name, _ := fieldByID(p.List.People.Form.Fields, FieldWorkerLegalName)
	if name.Value != "Nadia Rahman" || name.Error != "" {
		t.Errorf("the legal name field = %+v, want the reader's value and no error", name)
	}
	if h.svc.called("ListWorkers") != 1 {
		t.Error("a refused write refreshed the workforce anyway")
	}
	if p.List.People.SelectedRef != "" {
		t.Error("a refused write moved the selection")
	}

	// The next attempt answers the last one: the error does not outlive it.
	h.svc.createErr = nil
	h.app.Submit(ActionCreateWorker, createWorkerValues())
	after := h.awaitPage(t, "the recorded employee", noticeTitled("Employee added"))
	pay, _ = fieldByID(after.List.People.Form.Fields, FieldWorkerBasePay)
	if pay.Error != "" {
		t.Errorf("the field error survived the attempt that answered it: %q", pay.Error)
	}
}

// TestCreateWorkerRefusalsAboutTheCallerAreOnlyANotice: a cell composed
// without the execution authority, and a caller without the operator role,
// are refusals about the caller rather than about a field.
func TestCreateWorkerRefusalsAboutTheCallerAreOnlyANotice(t *testing.T) {
	cases := map[string]struct {
		err   error
		title string
	}{
		"no execution authority": {
			status.Error(codes.Unavailable, "this cell was composed without the execution authority"),
			"The cell is not answering",
		},
		"not an operator": {
			status.Error(codes.PermissionDenied, "the caller does not carry the operator role"),
			"Refused",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			h := staffed(t)
			h.svc.createErr = c.err
			h.app.Start(context.Background(), ListHref())
			h.awaitPage(t, "the workforce", peopleShown)

			h.app.Submit(ActionCreateWorker, createWorkerValues())

			p := h.awaitPage(t, "the refusal", noticeTitled(c.title))
			if p.Notice.Tone != toneDanger {
				t.Errorf("notice tone = %q, want danger", p.Notice.Tone)
			}
			for _, f := range p.List.People.Form.Fields {
				if f.Error != "" {
					t.Errorf("field %s carries an error the engine did not name: %q", f.ID, f.Error)
				}
			}
		})
	}
}

// TestAWorkforceRefusalStillShowsTheJourneys: one read failing does not take
// the other's answer with it, and the panel is absent rather than empty.
func TestAWorkforceRefusalStillShowsTheJourneys(t *testing.T) {
	h := staffed(t)
	h.svc.list = []*journeyv1.Journey{testJourney(t, journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED)}
	h.svc.workersErr = status.Error(codes.PermissionDenied, "the caller may not read this tenant's workforce")

	h.app.Start(context.Background(), ListHref())

	p := h.awaitPage(t, "the refusal", noticeTitled("Refused"))
	if p.List == nil || len(p.List.Journeys) != 1 {
		t.Fatal("a refused workforce read took the journeys with it")
	}
	if p.List.People != nil {
		t.Error("the page claimed a workforce it could not read")
	}
}
