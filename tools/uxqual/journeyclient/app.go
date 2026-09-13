package journeyclient

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/taskmux"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// App is the page's state machine: the one place that decides which RPC a
// route or a button means, what the reader sees while it is in flight, and
// what the answer becomes.
//
// It owns no DOM and no connection. The browser reaches it through four
// methods -- [App.Start], [App.OnHashChange], [App.Navigate] and
// [App.Submit] -- and it reaches the browser only by writing a projected
// [journey.Page] into the store the renderer is mounted on. That is what
// makes the whole client testable natively: a fake [Service], a real
// [journey.Store], and the assertions are on pages.
//
// # Concurrency
//
// Every RPC runs off the caller's goroutine. This is not a preference: on
// wasm the Go runtime and the browser's event loop share one thread, and a
// blocking call on the goroutine that handled a click freezes the page --
// including the WebSocket callbacks the call itself is waiting for, which
// deadlocks rather than merely stutters. [App.Tasks] is the production seam
// that bounds and prioritizes finite work; [App.Async] is its small-embedding
// fallback and the separate lane for the long-lived change stream. Async
// defaults to `go f()` and can be replaced by tests with a synchronous runner.
//
// The store is safe for concurrent writers, and every field below is guarded
// by mu. Answers are matched to the route that asked for them by a
// generation counter, so a list answer that arrives after the reader has
// already opened a journey is dropped instead of redrawing the page they
// left.
type App struct {
	cfg   Config
	svc   Service
	store *journey.Store
	now   func() time.Time

	// Async runs one unit of client work. It defaults to `go f()`; a test
	// sets it to run f inline so an assertion follows the call.
	Async func(func())

	// Tasks bounds finite reads and writes in the browser. It is optional so
	// the state machine remains usable by native embeddings and synchronous
	// tests. The long-lived WatchJourney stream deliberately stays on Async:
	// a subscription must never consume capacity needed by a click.
	Tasks *taskmux.Scheduler

	// Locate, when set, is how the client changes the browser's address:
	// [App.Navigate] hands it the new fragment and expects the browser's
	// hashchange event to come back through [App.OnHashChange]. When it is
	// nil (every native test, and any embedding with no address bar)
	// Navigate applies the route directly.
	Locate func(href string)

	// NavigateProduct upgrades links back into the host product shell when
	// the journey client is embedded there. It stays nil on the standalone
	// page, where those links retain ordinary document navigation.
	NavigateProduct func(href string)

	// WatchRetry is how long the watch waits before re-opening a stream that
	// ended without the client cancelling it. See watch.go.
	WatchRetry time.Duration

	mu          sync.Mutex
	ctx         context.Context
	route       Route
	generation  int
	list        []*journeyv1.Journey
	workers     []*journeyv1.Worker
	options     *journeyv1.WorkforceOptions
	listLoaded  bool
	detail      *journeyv1.JourneyDetail
	cancelWatch context.CancelFunc
	// withdrawPreview and cancelPreview are PROMOUX-013's own consequence
	// previews for the journey now on screen, read once when the detail is
	// first loaded (loadDetail). They are best-effort: a refusal or
	// transport error leaves the corresponding field nil, which
	// DetailPageWithInterventions reads as "no server-worded preview yet"
	// rather than as a second notice on top of whatever InspectJourney
	// already reported. They are not refreshed on every watch tick; see
	// PROMOUX-013's report for that known staleness window.
	withdrawPreview *journeyv1.PreviewJourneyInterventionResponse
	cancelPreview   *journeyv1.PreviewJourneyInterventionResponse
	// workerErrors are the last CreateWorker refusal's field violations,
	// keyed by request field name. They are cleared by the next attempt, so
	// a form never shows an error the reader has already answered.
	workerErrors map[string]string
	// applied is an address this client wrote into the browser's own bar
	// after it had already applied the route. The browser answers that write
	// with a hashchange, which would otherwise land here as a fresh
	// navigation and reload the page (taking the notice the write was made
	// to show with it). See [App.selectWorker].
	applied string
}

// New returns a client over cfg, svc and store. now is the clock the form's
// default effective date is computed from; nil means time.Now.
func New(cfg Config, svc Service, store *journey.Store, now func() time.Time) *App {
	if now == nil {
		now = time.Now
	}
	return &App{
		cfg:        cfg,
		svc:        svc,
		store:      store,
		now:        now,
		Async:      func(f func()) { go f() },
		WatchRetry: defaultWatchRetry,
		ctx:        context.Background(),
	}
}

// runTask schedules one finite RPC unit without keeping a browser event
// handler on the stack. Native tests and small embeddings retain the Async
// seam; the production WASM composition supplies a bounded Scheduler.
func (a *App) runTask(ctx context.Context, spec taskmux.Spec, work func(context.Context)) {
	if a.Tasks == nil {
		a.Async(func() { work(ctx) })
		return
	}
	_, err := a.Tasks.Submit(ctx, spec, func(taskCtx context.Context) error {
		work(taskCtx)
		return nil
	})
	if err != nil {
		a.show(refusal("The browser is handling too much work",
			"This action was not sent. Wait for another request to finish, then try again."))
	}
}

// Start seeds the form's clock-dependent default, records the context every
// later RPC hangs off, and loads the initial route.
//
// ctx is the page's lifetime: cancelling it ends every in-flight call and
// the watch. In the browser nothing cancels it, because the page's lifetime
// is the document's.
func (a *App) Start(ctx context.Context, initialHash string) {
	if ctx == nil {
		ctx = context.Background()
	}
	a.mu.Lock()
	a.ctx = ctx
	a.mu.Unlock()

	// The clock-dependent defaults are computed once, from the client's
	// clock, and live in the store rather than in the projection: the
	// projection is pure, and a default that changed on every re-render
	// would move under the reader's cursor at midnight.
	a.seedValues(nil, "")

	a.OnHashChange(initialHash)
}

// seedValues writes the value every control needs to render and to submit.
//
// It exists because of one property of the renderer's live path: a
// controlled <select> takes its selection from Page.Values, never from
// Option.Selected, and a form collects what it submits from the same map. A
// select the reader has not touched therefore shows nothing and submits
// nothing unless the client seeds it -- so every default this page offers is
// written here, once, before the page is first drawn with it.
//
// Nothing already set is overwritten: these are defaults, and a redraw must
// not undo a keystroke. The selection is the one exception -- it is a choice
// the reader just made, not a default.
func (a *App) seedValues(options *journeyv1.WorkforceOptions, selectedRef string) {
	a.store.Update(func(p *journey.Page) {
		if p.Values == nil {
			p.Values = map[string]string{}
		}
		seed := func(id, value string) {
			if value == "" || p.Values[id] != "" {
				return
			}
			p.Values[id] = value
		}
		seed(FieldEffective, DefaultEffectiveDate(a.now()))
		seed(FieldWorkerHireDate, DefaultHireDate(a.now()))
		seed(FieldWorkerBonusTarget, DefaultBonusTarget)
		// Everything the cell publishes a closed set for waits for the cell
		// to publish it. Seeding this client's own fallback first would pin
		// the form to a value the answer then contradicts, and the reader
		// would have to notice and undo it.
		if options != nil {
			seed(FieldWorkerPosition, firstOr(options.GetPositions(), DefaultPositionID))
			seed(FieldWorkerJobCode, firstOr(options.GetJobCodes(), ""))
			seed(FieldWorkerGrade, firstOr(options.GetGrades(), ""))
			seed(FieldWorkerOrgUnit, firstOr(options.GetOrgUnits(), ""))
			seed(FieldWorkerPayZone, firstOr(options.GetPayZones(), ""))
			// A declared currency travels as a hidden input carrying the
			// cell's own answer: there is nothing to choose, so there is
			// nothing to seed.
			if options.GetCurrency() == "" {
				seed(FieldWorkerCurrency, DefaultCurrency)
			}
		}
		if selectedRef != "" {
			p.Values[FieldWorker] = selectedRef
		}
	})
}

// OnHashChange applies one address.
//
// It is the single entry point for routing: [App.Navigate] and the browser's
// own hashchange event both land here, so there is one place that cancels
// the previous journey's watch and one place that starts the next load.
func (a *App) OnHashChange(hash string) {
	route := Parse(hash)

	a.mu.Lock()
	if a.applied != "" && a.applied == Href(route) {
		// The browser is telling this client about its own write to the
		// address bar, for a route it has already applied. Re-running it
		// would reload the page and discard the notice that write was made
		// to show.
		a.applied = ""
		a.mu.Unlock()
		return
	}
	previous := a.route
	a.route = route
	resetProposal := route.Kind == RouteProposal &&
		(previous.Kind != RouteProposal || previous.WorkerRef != route.WorkerRef)
	if route.Kind == RouteList && previous.Kind == RouteList && a.listLoaded && route.WorkerRef != previous.WorkerRef {
		// Only the selection changed, and the answers it selects from are
		// already in hand. Re-reading the whole tenant to move a highlight
		// would be a round trip the reader can see.
		a.mu.Unlock()
		a.selectValue(route.WorkerRef)
		a.show(nil)
		return
	}
	a.generation++
	generation := a.generation
	ctx := a.ctx
	// Leaving a journey ends its stream. Nothing else does: a watch that
	// outlived its route would keep re-projecting a journey the reader is no
	// longer looking at, over a socket they are no longer paying attention
	// to.
	a.stopWatchLocked()
	a.mu.Unlock()

	// A promotion draft is scoped to exactly one employee. Carrying its target
	// placement, compensation or rationale onto another employee is not a
	// convenience; it is an unsafe change of subject. Entering a focused
	// proposal therefore starts a clean draft, while a redraw of the same
	// employee's proposal preserves their in-progress typing.
	if resetProposal {
		a.resetProposalValues(route.WorkerRef)
	}

	if route.Kind == RouteDetail {
		a.loadDetail(ctx, generation, route.IntentID)
		return
	}
	a.loadList(ctx, generation)
}

// Navigate is what a live link or card calls.
//
// Only fragment addresses are this client's business: the masthead's
// Workspace link is an ordinary document link and is never wired to it (see
// App.wire), and anything else that reaches here is left to the browser.
func (a *App) Navigate(href string) {
	if !strings.HasPrefix(href, "#") {
		return
	}
	if a.Locate != nil {
		a.Locate(href)
		return
	}
	a.OnHashChange(href)
}

// selectWorker records the employee the reader picked.
//
// It is not a navigation even though it changes the address: the answers are
// already in hand, so the page is re-projected in place and the address bar
// is brought along afterwards, so the selection survives a reload and can be
// copied out of the bar. The write to the bar is remembered (App.applied) so
// the hashchange it provokes is recognised as this client's own.
func (a *App) selectWorker(ref string) {
	ref = strings.TrimSpace(ref)
	href := WorkerHref(ref)

	a.mu.Lock()
	if a.route.Kind != RouteList {
		// Nothing on the detail view selects an employee, but a stray
		// selection there is a route change rather than a re-projection:
		// there is no People table on that page to re-project.
		a.mu.Unlock()
		a.Navigate(href)
		return
	}
	a.route.WorkerRef = ref
	if a.Locate != nil {
		a.applied = href
	}
	a.mu.Unlock()

	a.selectValue(ref)
	if a.Locate != nil {
		a.Locate(href)
	}
	a.show(nil)
}

// selectValue puts the selection into the proposal form, so the select below
// the People table shows the person the table has highlighted and submits
// them. Clearing the selection clears the field rather than leaving the last
// person in a form that no longer names them.
func (a *App) selectValue(ref string) {
	a.store.SetValue(FieldWorker, ref)
}

// resetProposalValues removes every subject-specific proposal answer and
// then restores only the clock-derived default and the new immutable subject.
func (a *App) resetProposalValues(workerRef string) {
	a.store.Update(func(p *journey.Page) {
		if p.Values == nil {
			p.Values = map[string]string{}
		}
		for _, field := range []string{
			FieldWorker, FieldJobCode, FieldGrade, FieldPosition,
			FieldBase, FieldEffective, FieldReason,
		} {
			delete(p.Values, field)
		}
		p.Values[FieldEffective] = DefaultEffectiveDate(a.now())
		if workerRef != "" {
			p.Values[FieldWorker] = strings.TrimSpace(workerRef)
		}
	})
}

// clearDecisionValues prevents one routed approver's rationale from appearing
// in the next approver's form after the workflow advances.
func (a *App) clearDecisionValues() {
	a.store.Update(func(p *journey.Page) {
		delete(p.Values, FieldApproveReason)
		delete(p.Values, FieldRejectReason)
	})
}

// selectedWorker is the employee the route names.
func (a *App) selectedWorker() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.route.WorkerRef
}

func (a *App) proposalTargetIsGoverned(workerRef, jobCode, grade string) bool {
	a.mu.Lock()
	options := a.options
	worker := findWorker(a.workers, workerRef)
	a.mu.Unlock()
	if options == nil || len(options.GetPlacements()) == 0 {
		// Older servers do not publish exact placement tuples. They remain the
		// authority and will validate the proposal; the new client must not
		// make their whole surface unusable during a rolling deployment.
		return true
	}
	if len(options.GetPromotionPaths()) > 0 && worker != nil {
		published := false
		for _, path := range options.GetPromotionPaths() {
			if path.GetSourceJobCode() == worker.GetJobCode() && path.GetSourceGrade() == worker.GetGrade() &&
				path.GetTargetJobCode() == jobCode && path.GetTargetGrade() == grade {
				published = true
				break
			}
		}
		if !published {
			return false
		}
	}
	payZone, currency := "", options.GetCurrency()
	if worker != nil {
		payZone = worker.GetPayZone()
		if worker.GetCurrency() != "" {
			currency = worker.GetCurrency()
		}
	}
	for _, placement := range options.GetPlacements() {
		if placement.GetJobCode() == jobCode && placement.GetGrade() == grade &&
			(payZone == "" || placement.GetPayZone() == payZone) &&
			(currency == "" || placement.GetCurrency() == currency) {
			return true
		}
	}
	return false
}

// Submit is what a form calls. values is keyed by field name, as the
// renderer collects it.
func (a *App) Submit(actionID string, values map[string]string) {
	a.mu.Lock()
	ctx, generation, route := a.ctx, a.generation, a.route
	a.mu.Unlock()

	switch actionID {
	case ActionPropose:
		a.propose(ctx, generation, values)
	case ActionProposeFor:
		// A row's promotion action opens the same focused transaction screen
		// as the person's product profile. Selecting a row and starting a
		// transaction are different actions: conflating them is what used to
		// bury one person's promotion inside the tenant-wide dashboard.
		a.Navigate(ProposalHref(values[NameWorker]))
	case ActionCreateWorker:
		a.createWorker(ctx, generation, values)
	case ActionExecute:
		a.execute(ctx, generation, route.IntentID)
	case ActionApprove:
		a.decide(ctx, generation, route.IntentID, true, values[NameDecisionReason])
	case ActionReject:
		a.decide(ctx, generation, route.IntentID, false, values[NameDecisionReason])
	case ActionWithdraw:
		a.intervene(ctx, generation, route.IntentID, journeyv1.JourneyInterventionKind_JOURNEY_INTERVENTION_KIND_WITHDRAW, values[NameInterventionReason])
	case ActionCancel:
		a.intervene(ctx, generation, route.IntentID, journeyv1.JourneyInterventionKind_JOURNEY_INTERVENTION_KIND_CANCEL, values[NameInterventionReason])
	case ActionEditProposal:
		a.editProposal(ctx, generation, route.IntentID, values)
	default:
		// An action id the projection does not emit is a projection bug, and
		// the reader should see that their click did nothing rather than
		// wonder whether it worked.
		a.show(&journey.Notice{
			Tone:   toneDanger,
			Title:  "That action is not available",
			Detail: "The page offered an action this client does not know how to take (" + actionID + "). Reload the page.",
		})
	}
}

// ---------------------------------------------------------------------
// Loads
// ---------------------------------------------------------------------

// loadList reads the two things the list route shows -- this tenant's
// journeys and its workforce -- and draws the page once, when both have
// answered.
//
// They are read concurrently because they are independent reads of the same
// cell, and drawn together because they are one page: painting the journeys
// and then the People table a moment later would move the whole page under
// the reader for no reason either answer could explain.
func (a *App) loadList(ctx context.Context, generation int) {
	a.show(busy("Loading this tenant's workforce and journeys."))
	a.runTask(ctx, taskmux.Spec{Key: "journey:route-read", Priority: taskmux.UserVisible, Duplicate: taskmux.ReplaceExisting}, func(ctx context.Context) {
		var (
			wg        sync.WaitGroup
			journeys  *journeyv1.ListJourneysResponse
			journeyEr error
			workers   *journeyv1.ListWorkersResponse
			workerEr  error
		)
		wg.Add(2)
		go func() {
			defer wg.Done()
			journeys, journeyEr = a.svc.ListJourneys(ctx, &journeyv1.ListJourneysRequest{})
		}()
		go func() {
			defer wg.Done()
			workers, workerEr = a.svc.ListWorkers(ctx, &journeyv1.ListWorkersRequest{})
		}()
		wg.Wait()
		if a.stale(generation) {
			return
		}

		a.mu.Lock()
		if journeyEr == nil {
			a.list = journeys.GetJourneys()
		}
		if workerEr == nil {
			a.workers = workers.GetWorkers()
			a.options = workers.GetOptions()
		}
		// Only a round in which both answered is a loaded list: a partial
		// round must not let a later selection re-project a page that is
		// missing half of itself.
		a.listLoaded = journeyEr == nil && workerEr == nil
		selected := a.route.WorkerRef
		options := a.options
		a.mu.Unlock()

		// Whichever half answered is shown. A refusal of one read is a
		// notice, not a blank page: the reader can still act on the other.
		a.seedValues(options, selected)
		switch {
		case journeyEr != nil:
			a.show(NoticeFromError(journeyEr))
		case workerEr != nil:
			a.show(NoticeFromError(workerEr))
		default:
			a.show(nil)
		}
	})
}

func (a *App) loadDetail(ctx context.Context, generation int, intentID string) {
	a.show(busy("Reading this journey from the engine."))
	a.runTask(ctx, taskmux.Spec{Key: "journey:route-read", Priority: taskmux.UserVisible, Duplicate: taskmux.ReplaceExisting}, func(ctx context.Context) {
		resp, err := a.svc.InspectJourney(ctx, &journeyv1.InspectJourneyRequest{IntentId: intentID})
		if a.stale(generation) {
			return
		}
		if err != nil {
			a.show(routeReadNotice(err))
			return
		}
		a.loadInterventionPreviews(ctx, generation, intentID)
		a.applyDetail(generation, resp.GetDetail(), nil)
		a.startWatch(generation, intentID, resp.GetDetail().GetDetailDigest())
	})
}

// loadInterventionPreviews reads PROMOUX-013's two typed-intervention
// previews (WITHDRAW, CANCEL) for intentID, concurrently, and records
// whatever answered. EditProposal has no preview kind of its own -- see
// interventionActions' own doc comment for why its availability is derived
// from these same two answers instead. A refusal or transport error on
// either leaves that field nil rather than producing a second notice on top
// of whatever the caller already showed: the corresponding action still
// renders, gated by the journey's own stage, only without the server's own
// worded consequence text.
func (a *App) loadInterventionPreviews(ctx context.Context, generation int, intentID string) {
	if intentID == "" {
		return
	}
	var (
		wg               sync.WaitGroup
		withdraw, cancel *journeyv1.PreviewJourneyInterventionResponse
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		resp, err := a.svc.PreviewJourneyIntervention(ctx, &journeyv1.PreviewJourneyInterventionRequest{
			IntentId: intentID, Kind: journeyv1.JourneyInterventionKind_JOURNEY_INTERVENTION_KIND_WITHDRAW,
		})
		if err == nil {
			withdraw = resp
		}
	}()
	go func() {
		defer wg.Done()
		resp, err := a.svc.PreviewJourneyIntervention(ctx, &journeyv1.PreviewJourneyInterventionRequest{
			IntentId: intentID, Kind: journeyv1.JourneyInterventionKind_JOURNEY_INTERVENTION_KIND_CANCEL,
		})
		if err == nil {
			cancel = resp
		}
	}()
	wg.Wait()
	if a.stale(generation) {
		return
	}
	a.mu.Lock()
	a.withdrawPreview = withdraw
	a.cancelPreview = cancel
	a.mu.Unlock()
}

// routeReadNotice deliberately gives unknown, stale, and unauthorized
// resource selectors the same presentation. A copied address is untrusted;
// neither its title nor a service-supplied diagnostic may reveal whether a
// journey exists outside the current principal's authorized projection.
func routeReadNotice(err error) *journey.Notice {
	if err == nil {
		return nil
	}
	return &journey.Notice{
		Tone:   toneDanger,
		Title:  "This journey is unavailable",
		Detail: "The link cannot be opened in this session. Return to Journeys to choose an item you can currently access.",
	}
}

// ---------------------------------------------------------------------
// Writes
// ---------------------------------------------------------------------

func (a *App) propose(ctx context.Context, generation int, values map[string]string) {
	// The submitted select is the reader's answer; the People selection is
	// the fallback for a client whose select never carried one (an SSR-shaped
	// submission, or a form submitted before the seed landed).
	worker := strings.TrimSpace(values[NameWorker])
	if worker == "" {
		worker = a.selectedWorker()
	}
	req := &journeyv1.ProposeJourneyRequest{
		WorkerRef: worker,
		Target: &journeyv1.Placement{
			JobCode:    strings.TrimSpace(values[NameJobCode]),
			Grade:      strings.TrimSpace(values[NameGrade]),
			PositionId: strings.TrimSpace(values[NamePosition]),
		},
		ProposedBase:   strings.TrimSpace(values[NameBase]),
		EffectiveDate:  strings.TrimSpace(values[NameEffective]),
		BusinessReason: strings.TrimSpace(values[NameReason]),
	}
	if req.GetWorkerRef() == "" {
		a.show(refusal("Choose a worker", "A promotion is proposed for one worker; the engine reads their placement, pay and budget authority from the record."))
		return
	}
	missing := make([]string, 0, 6)
	for _, field := range []struct {
		label string
		value string
	}{
		{label: "target job code", value: req.GetTarget().GetJobCode()},
		{label: "target grade", value: req.GetTarget().GetGrade()},
		{label: "target position", value: req.GetTarget().GetPositionId()},
		{label: "proposed base pay", value: req.GetProposedBase()},
		{label: "effective date", value: req.GetEffectiveDate()},
		{label: "business reason", value: req.GetBusinessReason()},
	} {
		if field.value == "" {
			missing = append(missing, field.label)
		}
	}
	if len(missing) > 0 {
		a.show(refusal("Complete the required fields", "Add "+strings.Join(missing, ", ")+" before simulating this proposal."))
		return
	}
	if !a.proposalTargetIsGoverned(req.GetWorkerRef(), req.GetTarget().GetJobCode(), req.GetTarget().GetGrade()) {
		a.show(refusal("Choose a governed target role", "That job and grade are not a published next step from this employee's current profile, or have no pay band in their pay zone and currency."))
		return
	}

	a.show(busy("Creating and simulating the proposal."))
	a.runTask(ctx, taskmux.Spec{Key: "journey:propose:" + worker, Priority: taskmux.Interactive, Duplicate: taskmux.KeepExisting}, func(ctx context.Context) {
		resp, err := a.svc.ProposeJourney(ctx, req)
		if a.stale(generation) {
			return
		}
		if err != nil {
			a.show(NoticeFromError(err))
			return
		}
		a.resetProposalValues("")
		// The proposal answer is a summary, not a detail, so the client
		// navigates and reads the new journey rather than half-drawing it
		// from what it has.
		a.Navigate(DetailHref(resp.GetJourney().GetIntentId()))
	})
}

// createWorker records one new employee.
//
// On success it does three things in order, and the order is the point: the
// workforce is re-read so the new person is a fact the page holds rather
// than one it drew from the answer to its own write, the reader's selection
// moves to them, and the notice says what to do next. The reader arrived
// here to promote somebody; adding them is a step in that, not the end of
// it.
func (a *App) createWorker(ctx context.Context, generation int, values map[string]string) {
	req := &journeyv1.CreateWorkerRequest{
		LegalName:     strings.TrimSpace(values[NameLegalName]),
		PreferredName: strings.TrimSpace(values[NamePreferredName]),
		JobCode:       strings.TrimSpace(values[NameWorkerJobCode]),
		Grade:         strings.TrimSpace(values[NameWorkerGrade]),
		OrgUnit:       strings.TrimSpace(values[NameOrgUnit]),
		PositionId:    strings.TrimSpace(values[NameWorkerPosition]),
		Location:      strings.TrimSpace(values[NameLocation]),
		PayZone:       strings.TrimSpace(values[NamePayZone]),
		BasePay:       strings.TrimSpace(values[NameWorkerBasePay]),
		Currency:      strings.TrimSpace(values[NameCurrency]),
		BonusTarget:   strings.TrimSpace(values[NameBonusTarget]),
		HireDate:      strings.TrimSpace(values[NameHireDate]),
		ManagerRef:    strings.TrimSpace(values[NameManagerRef]),
	}
	// Whatever the last attempt was told is answered by this one: the form
	// must not carry an error beside a value the reader has since changed.
	a.mu.Lock()
	a.workerErrors = nil
	a.mu.Unlock()

	a.show(busy("Recording the employee in this cell's workforce."))
	a.runTask(ctx, taskmux.Spec{Key: "journey:create-worker", Priority: taskmux.Interactive, Duplicate: taskmux.KeepExisting}, func(ctx context.Context) {
		resp, err := a.svc.CreateWorker(ctx, req)
		if a.stale(generation) {
			return
		}
		if err != nil {
			a.refuseWorker(err, values)
			return
		}
		worker := resp.GetWorker()
		ref := worker.GetWorkerRef()

		// The list is re-read rather than appended to, so the page shows the
		// cell's own answer -- including the worker number and the ordering
		// the engine gave the new row.
		refreshed, listErr := a.svc.ListWorkers(ctx, &journeyv1.ListWorkersRequest{})
		if a.stale(generation) {
			return
		}
		a.mu.Lock()
		if listErr == nil {
			a.workers = refreshed.GetWorkers()
			a.options = refreshed.GetOptions()
		}
		if a.route.Kind == RouteList && ref != "" {
			a.route.WorkerRef = ref
			if a.Locate != nil {
				a.applied = WorkerHref(ref)
			}
		}
		options := a.options
		a.mu.Unlock()

		// The form empties so the next employee is typed into a blank one
		// rather than edited out of the last one; seedValues then puts the
		// cell's defaults back.
		a.clearWorkerValues()
		a.seedValues(options, ref)
		if a.Locate != nil && ref != "" {
			a.Locate(WorkerHref(ref))
		}
		a.show(&journey.Notice{
			Tone:   toneSuccess,
			Title:  "Employee added",
			Detail: createdWorkerName(worker) + " is now a fact in this cell's workforce; propose their promotion below.",
		})
	})
}

// refuseWorker projects a refused CreateWorker.
//
// An INVALID_ARGUMENT names a field, so it is shown on that field as well as
// in the notice, and the values the reader typed are put back into the store
// so the form they are looking at is still the form they submitted. Every
// other refusal -- no execution authority (UNAVAILABLE), not an operator
// (PERMISSION_DENIED) -- is about the caller rather than about a field, and
// is only ever the notice.
func (a *App) refuseWorker(err error, values map[string]string) {
	notice := NoticeFromError(err)
	if status.Code(err) == codes.InvalidArgument {
		notice.Title = "That employee is not valid"
		a.mu.Lock()
		a.workerErrors = fieldViolations(err)
		a.mu.Unlock()
		a.keepWorkerValues(values)
	}
	a.show(notice)
}

// keepWorkerValues writes a submitted form back into the store, keyed by
// field id, so a refusal does not empty the form.
func (a *App) keepWorkerValues(values map[string]string) {
	a.store.Update(func(p *journey.Page) {
		if p.Values == nil {
			p.Values = map[string]string{}
		}
		for name, value := range values {
			if id, ok := workerFieldIDs[name]; ok {
				p.Values[id] = value
			}
		}
	})
}

// clearWorkerValues empties the new-employee form.
func (a *App) clearWorkerValues() {
	a.store.Update(func(p *journey.Page) {
		for _, id := range workerFieldIDs {
			delete(p.Values, id)
		}
	})
}

// createdWorkerName is what the notice calls the new employee.
func createdWorkerName(w *journeyv1.Worker) string {
	if name := strings.TrimSpace(w.GetPreferredName()); name != "" {
		return name
	}
	if name := strings.TrimSpace(w.GetLegalName()); name != "" {
		return name
	}
	return "The employee"
}

// fieldViolations reads the owned error model's per-field refusals out of a
// status, keyed by the field path the engine named.
func fieldViolations(err error) map[string]string {
	out := map[string]string{}
	for _, d := range status.Convert(err).Details() {
		detail, ok := d.(*commonv1.ErrorDetail)
		if !ok {
			continue
		}
		for _, v := range detail.GetFieldViolations() {
			path := strings.TrimSpace(v.GetFieldPath())
			if path == "" || out[path] != "" {
				continue
			}
			out[path] = strings.TrimSpace(v.GetDescription())
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (a *App) execute(ctx context.Context, generation int, intentID string) {
	if intentID == "" {
		return
	}
	a.show(busy("Asking the P1B execution authority to admit the plan."))
	a.runTask(ctx, taskmux.Spec{Key: "journey:execute:" + intentID, Priority: taskmux.Interactive, Duplicate: taskmux.KeepExisting}, func(ctx context.Context) {
		resp, err := a.svc.ExecuteJourney(ctx, &journeyv1.ExecuteJourneyRequest{IntentId: intentID})
		if a.stale(generation) {
			return
		}
		if err != nil {
			a.show(NoticeFromError(err))
			return
		}
		a.applyDetail(generation, resp.GetDetail(), &journey.Notice{
			Tone:   toneSuccess,
			Title:  "The plan was admitted",
			Detail: "The workflow instance started and is parked on its approval work item.",
		})
	})
}

func (a *App) decide(ctx context.Context, generation int, intentID string, approve bool, reason string) {
	if intentID == "" {
		return
	}
	reason = strings.TrimSpace(reason)
	if !approve && reason == "" {
		// The engine would refuse this too, but a round trip to be told what
		// the form already said is not worth the reader's time.
		a.show(refusal("Say what needs to change",
			"A rejection is returned to the manager verbatim, so it needs a reason."))
		return
	}

	if approve {
		a.show(busy("Completing the approval work item and resuming the workflow."))
	} else {
		a.show(busy("Returning the proposal to the manager."))
	}
	// Approve and reject share a key: they are mutually exclusive decisions
	// for the same work item, not merely duplicates of the same button.
	a.runTask(ctx, taskmux.Spec{Key: "journey:decision:" + intentID, Priority: taskmux.Interactive, Duplicate: taskmux.KeepExisting}, func(ctx context.Context) {
		resp, err := a.svc.DecideJourney(ctx, &journeyv1.DecideJourneyRequest{
			IntentId: intentID,
			Approve:  approve,
			Reason:   reason,
		})
		if a.stale(generation) {
			return
		}
		if err != nil {
			a.show(NoticeFromError(err))
			return
		}
		a.clearDecisionValues()
		notice := &journey.Notice{
			Tone:   toneSuccess,
			Title:  "Returned to the manager",
			Detail: "The instance ended at its REJECTED terminal. No promotion fact was written.",
		}
		if approve {
			notice = approvalNotice(resp.GetDetail())
		}
		a.applyDetail(generation, resp.GetDetail(), notice)
	})
}

// approvalNotice says only what the engine's returned detail proves. An
// accepted finance decision may lead to a manager decision, and completed
// approvals may lead to an effective-date wait; neither is a ledger write.
func approvalNotice(detail *journeyv1.JourneyDetail) *journey.Notice {
	if detail != nil && detail.GetLedger() != nil {
		return &journey.Notice{
			Tone:   toneSuccess,
			Title:  "Promotion recorded",
			Detail: "All required approvals completed and the terminal node recorded the governed promotion fact.",
		}
	}
	stage := ""
	if detail != nil {
		stage = stageOf(detail.GetJourney().GetStage())
	}
	switch stage {
	case stageFinanceApproval:
		return &journey.Notice{Tone: toneSuccess, Title: "Approval recorded", Detail: "The workflow advanced to finance approval. No promotion fact has been recorded yet."}
	case stageManagerApproval:
		return &journey.Notice{Tone: toneSuccess, Title: "Approval recorded", Detail: "The workflow advanced to manager approval. No promotion fact has been recorded yet."}
	case stageReapproval:
		return &journey.Notice{Tone: toneSuccess, Title: "Approval recorded", Detail: "A material change requires another routed approval. No promotion fact has been recorded yet."}
	case stageWaitingEffective:
		return &journey.Notice{Tone: toneSuccess, Title: "Approvals complete", Detail: "The workflow is waiting for the effective date. No promotion fact has been recorded yet."}
	default:
		return &journey.Notice{Tone: toneSuccess, Title: "Approval recorded", Detail: "The workflow resumed. The recorded outcome below is authoritative; no terminal write is claimed unless a ledger fact is present."}
	}
}

// currentGovernanceVersion is the loaded journey's own optimistic-
// concurrency version -- unrelated to the workflow instance version shown
// elsewhere on the page -- which RequestJourneyIntervention and
// EditProposal both require as expected_instance_version. It comes from the
// already-loaded detail, never from a caller-supplied value: presenting a
// stale one is refused by the engine's own compare-and-swap, which is the
// correctness boundary, not this client's guess.
func (a *App) currentGovernanceVersion(intentID string) uint64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.detail.GetJourney().GetIntentId() != intentID {
		return 0
	}
	return a.detail.GetJourney().GetGovernanceVersion()
}

// intervene runs PROMOUX-013's typed WITHDRAW or CANCEL intervention. Both
// kinds are the identical governed call (internal/intent/app/journey_
// intervention.go's own doc comment: RequestIntervention forwards to
// CancelIntent either way); kind only selects the wording shown here and
// which stage the projector offered the action at.
func (a *App) intervene(ctx context.Context, generation int, intentID string, kind journeyv1.JourneyInterventionKind, reason string) {
	if intentID == "" {
		return
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		a.show(refusal("Say why", "A withdrawal or cancellation is retained as evidence on the governed record, so it needs a reason."))
		return
	}
	verb := "Withdrawing"
	if kind == journeyv1.JourneyInterventionKind_JOURNEY_INTERVENTION_KIND_CANCEL {
		verb = "Requesting cancellation for"
	}
	a.show(busy(verb + " this proposal."))
	a.runTask(ctx, taskmux.Spec{Key: "journey:intervene:" + intentID, Priority: taskmux.Interactive, Duplicate: taskmux.KeepExisting}, func(ctx context.Context) {
		resp, err := a.svc.RequestJourneyIntervention(ctx, &journeyv1.RequestJourneyInterventionRequest{
			IntentId: intentID, Kind: kind, Reason: reason,
			ExpectedInstanceVersion: a.currentGovernanceVersion(intentID),
			IdempotencyKey:          uuid.NewString(),
		})
		if a.stale(generation) {
			return
		}
		if err != nil {
			a.show(NoticeFromError(err))
			return
		}
		a.show(interventionNotice(resp.GetOutcome(), resp.GetRetainedEvidenceRef()))
		a.reloadDetail(ctx, generation, intentID)
	})
}

// interventionNotice reports only what RequestJourneyIntervention's own
// typed outcome says -- never "cancelled" for a disposition that was
// actually deferred to a safe point that has not been reached yet, which
// would be exactly RED's own falsification concern restated for this
// surface.
func interventionNotice(outcome commonv1.InterventionOutcome, evidenceRef string) *journey.Notice {
	suffix := ""
	if evidenceRef != "" {
		suffix = " Evidence: " + evidenceRef + "."
	}
	switch outcome {
	case commonv1.InterventionOutcome_INTERVENTION_OUTCOME_APPLIED:
		return &journey.Notice{Tone: toneSuccess, Title: "Stopped", Detail: "The proposal was cancelled." + suffix}
	case commonv1.InterventionOutcome_INTERVENTION_OUTCOME_PENDING_SAFE_POINT:
		return &journey.Notice{Tone: toneInfo, Title: "Cancellation requested", Detail: "The workflow has not reached a safe point yet; nothing has changed. It will be evaluated again as the workflow proceeds." + suffix}
	case commonv1.InterventionOutcome_INTERVENTION_OUTCOME_TOO_LATE:
		return &journey.Notice{Tone: toneWarning, Title: "Too late", Detail: "The business effect already committed before this request reached the engine. This cannot be reversed." + suffix}
	case commonv1.InterventionOutcome_INTERVENTION_OUTCOME_REPAIR_REQUIRED:
		return &journey.Notice{Tone: toneDanger, Title: "Repair required", Detail: "The workflow reached neither a clean stop nor a completion. Governed repair is required." + suffix}
	default:
		return &journey.Notice{Tone: toneWarning, Title: "Outcome unclear", Detail: "The engine did not report a recognized outcome for this request." + suffix}
	}
}

// editProposal runs PROMOUX-013's EditProposal: it cancels the original and
// mints a corrected successor. On success the reader is navigated to the
// successor, exactly as a fresh Propose navigates to its own new journey --
// the edit is a distinct governed intent, not an in-place change to the one
// on screen.
func (a *App) editProposal(ctx context.Context, generation int, intentID string, values map[string]string) {
	if intentID == "" {
		return
	}
	reason := strings.TrimSpace(values[NameEditReason])
	if reason == "" {
		a.show(refusal("Say why", "An edit is retained as evidence on the governed record, so it needs a reason."))
		return
	}
	missing := make([]string, 0, 5)
	for _, field := range []struct {
		label, value string
	}{
		{"target job code", strings.TrimSpace(values[NameEditJobCode])},
		{"target grade", strings.TrimSpace(values[NameEditGrade])},
		{"proposed base pay", strings.TrimSpace(values[NameEditBase])},
		{"effective date", strings.TrimSpace(values[NameEditEffective])},
		{"business reason", strings.TrimSpace(values[NameEditBusinessReason])},
	} {
		if field.value == "" {
			missing = append(missing, field.label)
		}
	}
	if len(missing) > 0 {
		a.show(refusal("Complete the required fields", "Add "+strings.Join(missing, ", ")+" before submitting this edit."))
		return
	}

	a.show(busy("Cancelling the original proposal and creating the corrected successor."))
	a.runTask(ctx, taskmux.Spec{Key: "journey:edit:" + intentID, Priority: taskmux.Interactive, Duplicate: taskmux.KeepExisting}, func(ctx context.Context) {
		resp, err := a.svc.EditProposal(ctx, &journeyv1.EditProposalRequest{
			IntentId: intentID, Reason: reason,
			ExpectedInstanceVersion: a.currentGovernanceVersion(intentID),
			IdempotencyKey:          uuid.NewString(),
			Target: &journeyv1.Placement{
				JobCode: strings.TrimSpace(values[NameEditJobCode]),
				Grade:   strings.TrimSpace(values[NameEditGrade]),
			},
			ProposedBase:   strings.TrimSpace(values[NameEditBase]),
			EffectiveDate:  strings.TrimSpace(values[NameEditEffective]),
			BusinessReason: strings.TrimSpace(values[NameEditBusinessReason]),
		})
		if a.stale(generation) {
			return
		}
		if err != nil {
			a.show(NoticeFromError(err))
			return
		}
		a.Navigate(DetailHref(resp.GetJourney().GetIntentId()))
	})
}

// reloadDetail re-reads the journey after a governed write that does not
// itself return a JourneyDetail (RequestJourneyIntervention answers with
// only the journey summary and the outcome), so the page's findings,
// timeline and work items are the engine's own post-write state rather than
// stale pre-write ones sitting under a fresh notice.
func (a *App) reloadDetail(ctx context.Context, generation int, intentID string) {
	resp, err := a.svc.InspectJourney(ctx, &journeyv1.InspectJourneyRequest{IntentId: intentID})
	if a.stale(generation) || err != nil {
		return
	}
	a.loadInterventionPreviews(ctx, generation, intentID)
	a.mu.Lock()
	if a.generation == generation && a.route.Kind == RouteDetail {
		a.detail = resp.GetDetail()
	}
	a.mu.Unlock()
	a.show(a.currentNotice())
}

// ---------------------------------------------------------------------
// Rendering
// ---------------------------------------------------------------------

// applyDetail records one detail answer and redraws, unless the reader has
// moved on.
func (a *App) applyDetail(generation int, detail *journeyv1.JourneyDetail, notice *journey.Notice) {
	if detail == nil {
		return
	}
	a.mu.Lock()
	if a.generation != generation || a.route.Kind != RouteDetail {
		a.mu.Unlock()
		return
	}
	a.detail = detail
	a.mu.Unlock()
	a.show(notice)
}

// stale reports whether the route has changed since generation was taken, in
// which case whatever the caller is holding is an answer to a question the
// reader has stopped asking.
func (a *App) stale(generation int) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.generation != generation
}

// show projects the current state with one notice and writes it to the
// store, which re-renders every mounted view.
func (a *App) show(notice *journey.Notice) {
	a.mu.Lock()
	route, detail := a.route, a.detail
	withdrawPreview, cancelPreview := a.withdrawPreview, a.cancelPreview
	data := ListData{
		Journeys:     a.list,
		Workers:      a.workers,
		Options:      a.options,
		SelectedRef:  a.route.WorkerRef,
		WorkerErrors: a.workerErrors,
	}
	a.mu.Unlock()

	values := a.store.Values()
	var page journey.Page
	if route.Kind == RouteDetail && detail.GetJourney().GetIntentId() == route.IntentID {
		page = DetailPageWithInterventions(a.cfg, detail, notice, values, withdrawPreview, cancelPreview)
	} else if route.Kind == RouteDetail {
		// The route names a journey whose answer has not arrived (or whose
		// answer was a refusal). The chrome, the notice and the navigation
		// still render, which is what leaves the reader a way back.
		page = DetailPage(a.cfg, nil, notice, values)
	} else if route.Kind == RouteProposal {
		page = ProposalPage(a.cfg, data, notice, values)
	} else {
		page = ListPage(a.cfg, data, notice, values)
	}
	a.store.Set(a.wire(page))
}

// wire binds the renderer's live callbacks.
//
// journey.Wire binds every masthead link, but only fragment routes are this
// client's to intercept: the Workspace link must stay an ordinary document
// link so it actually leaves the page. Unbinding it afterwards is a smaller
// and more obviously correct change than duplicating Wire's whole body here.
// Picking an employee is bound to the client rather than left to Wire's own
// fallback (a plain navigation to the row's address), because the client can
// do it without re-reading the tenant: the answers are already here.
func (a *App) wire(p journey.Page) journey.Page {
	p = journey.Wire(a.store, p, a.Navigate, a.Submit, journey.WithSelectWorker(a.selectWorker))
	// Re-project the dependent grade choices when a target job changes. The
	// store remains the one controlled-input authority; this small wrapper
	// only prevents a stale grade from surviving a new job selection.
	p.OnFieldChange = func(fieldID, value string) {
		if fieldID != FieldJobCode {
			a.store.SetValue(fieldID, value)
			return
		}
		a.store.Update(func(page *journey.Page) {
			if page.Values == nil {
				page.Values = map[string]string{}
			}
			page.Values[FieldJobCode] = value
			page.Values[FieldGrade] = a.uniqueGradeForJob(value)
		})
		a.show(nil)
	}
	for i := range p.Nav {
		if !strings.HasPrefix(p.Nav[i].Href, "#") {
			if a.NavigateProduct == nil {
				p.Nav[i].OnNavigate = nil
			} else {
				href := p.Nav[i].Href
				p.Nav[i].OnNavigate = func() { a.NavigateProduct(href) }
			}
		}
	}
	if p.Proposal != nil && p.Proposal.BackHref != "" && a.NavigateProduct != nil {
		href := p.Proposal.BackHref
		p.Proposal.BackNavigate = func() { a.NavigateProduct(href) }
	}
	if p.List != nil && p.List.People != nil && p.List.People.DirectoryLink.Href != "" && a.NavigateProduct != nil {
		href := p.List.People.DirectoryLink.Href
		p.List.People.DirectoryLink.OnNavigate = func() { a.NavigateProduct(href) }
	}
	if p.Detail != nil && strings.HasPrefix(p.Detail.JourneysLink.Href, "#") {
		href := p.Detail.JourneysLink.Href
		p.Detail.JourneysLink.OnNavigate = func() { a.Navigate(href) }
	}
	if p.Detail != nil && p.Detail.BackLink.Href != "" && a.NavigateProduct != nil {
		href := p.Detail.BackLink.Href
		p.Detail.BackLink.OnNavigate = func() { a.NavigateProduct(href) }
	}
	return p
}

func (a *App) uniqueGradeForJob(jobCode string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	worker := findWorker(a.workers, a.route.WorkerRef)
	if worker != nil && len(a.options.GetPromotionPaths()) > 0 {
		grade := ""
		for _, path := range a.options.GetPromotionPaths() {
			if path.GetSourceJobCode() != worker.GetJobCode() || path.GetSourceGrade() != worker.GetGrade() || path.GetTargetJobCode() != jobCode {
				continue
			}
			if grade != "" && grade != path.GetTargetGrade() {
				return ""
			}
			grade = path.GetTargetGrade()
		}
		return grade
	}
	grade := ""
	for _, placement := range a.options.GetPlacements() {
		if placement.GetJobCode() != jobCode {
			continue
		}
		if grade != "" && grade != placement.GetGrade() {
			return ""
		}
		grade = placement.GetGrade()
	}
	return grade
}

// busy is the notice shown while an RPC is in flight. It is a notice rather
// than a spinner because the reader needs to know which of the page's
// answers are about to change, not merely that something is happening.
func busy(detail string) *journey.Notice {
	return &journey.Notice{Tone: toneInfo, Title: "Working…", Detail: detail}
}

// refusal is a notice for something this client refused without asking the
// engine.
func refusal(title, detail string) *journey.Notice {
	return &journey.Notice{Tone: toneWarning, Title: title, Detail: detail}
}

// NoticeFromError projects one refusal onto the page's one status message.
//
// The title is the shape of the refusal in the reader's terms and the detail
// is the engine's own message, plus every field violation the owned error
// model carried. The gRPC code is never shown as a code: a reader who can
// act on "you may not do that" cannot act on "PERMISSION_DENIED".
func NoticeFromError(err error) *journey.Notice {
	if err == nil {
		return nil
	}
	st := status.Convert(err)
	parts := []string{}
	if msg := strings.TrimSpace(st.Message()); msg != "" {
		parts = append(parts, msg)
	}
	for _, d := range st.Details() {
		detail, ok := d.(*commonv1.ErrorDetail)
		if !ok {
			continue
		}
		for _, v := range detail.GetFieldViolations() {
			part := strings.TrimSpace(v.GetFieldPath() + ": " + v.GetDescription())
			if ref := v.GetRuleRef(); ref != "" {
				part += " (" + ref + ")"
			}
			parts = append(parts, part)
		}
		if id := detail.GetCorrelationId(); id != "" {
			parts = append(parts, "Correlation: "+id)
		}
	}
	return &journey.Notice{
		Tone:   toneDanger,
		Title:  titleForCode(st.Code()),
		Detail: strings.Join(parts, " — "),
	}
}

// titleForCode names a refusal the way the page talks.
func titleForCode(code codes.Code) string {
	switch code {
	case codes.PermissionDenied:
		return "Refused"
	case codes.Unauthenticated:
		return "This page is no longer signed in"
	case codes.NotFound:
		return "No such journey"
	case codes.InvalidArgument:
		return "That proposal is not valid"
	case codes.FailedPrecondition:
		return "Not available at this stage"
	case codes.Unavailable:
		return "The cell is not answering"
	case codes.DeadlineExceeded:
		return "The engine did not answer in time"
	case codes.Canceled:
		return "The call was cancelled"
	case codes.AlreadyExists:
		return "That has already been done"
	case codes.ResourceExhausted:
		return "Too many requests"
	default:
		return "The engine refused this"
	}
}
