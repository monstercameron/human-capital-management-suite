package bootstrap_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
)

// The workforce half of the Promotion journey, end to end over the same
// composed cell the rest of this file drives.
//
// The point of the whole slice is that a user can create an employee and then
// run that person through the promotion the release already demonstrates for
// omar-reyes. So the primary test does exactly that: create, see them listed,
// propose, execute, approve, and read back the one governed ledger fact.

// journeyWorkerInput is a complete create form, placed on a job code, grade
// and pay zone the fixture catalog covers, so the created worker is
// simulatable from the moment they exist.
func journeyWorkerInput() workspace.WorkerInput {
	return workspace.WorkerInput{
		LegalName:     "Lena Park",
		PreferredName: "Lena",
		JobCode:       "OPS-HRBP2",
		Grade:         "P2",
		OrgUnit:       "people-ops",
		PositionID:    "POS-HRBP-204",
		Location:      "Boston, MA",
		PayZone:       "US-EAST",
		BasePay:       "88000.00",
		Currency:      "USD",
		BonusTarget:   "0.0500",
		HireDate:      "2021-04-05",
	}
}

// journeyProposalFor is the manager's promotion form for one listed worker.
func journeyProposalFor(ref string) workspace.ProposalInput {
	in := journeyProposal()
	in.WorkerRef = ref
	return in
}

// ---------------------------------------------------------------------------
// The whole slice
// ---------------------------------------------------------------------------

// TestJourneyCreatedWorkerCompletesTheWholePromotion is the acceptance test
// for this change: an employee who did not exist when the process started is
// created, listed, promoted, executed, approved, and leaves exactly one
// governed ledger fact -- the same path omar-reyes walks, for somebody a user
// made.
func TestJourneyCreatedWorkerCompletesTheWholePromotion(t *testing.T) {
	h := newJourneyHarness(t)
	ctx := h.operatorCtx(t)

	created, err := h.engine.CreateWorker(ctx, journeyWorkerInput())
	if err != nil {
		t.Fatalf("CreateWorker: %v", err)
	}
	if created.Source != workspace.WorkerSourceCreated {
		t.Fatalf("source = %q, want CREATED", created.Source)
	}
	if created.WorkerRef == "" || created.WorkerID == "" {
		t.Fatalf("the created worker has no identity: %+v", created)
	}
	if !strings.HasPrefix(created.WorkerRef, "lena-") {
		t.Errorf("worker ref = %q, want it derived from the name", created.WorkerRef)
	}
	if created.BasePay != "88000.00" || created.Currency != "USD" || created.BonusTarget != "0.0500" {
		t.Errorf("baseline = %s/%s/%s, want the form's own",
			created.BasePay, created.Currency, created.BonusTarget)
	}

	// The list shows the created worker first, marked CREATED, beside the
	// release's own corpus population.
	workers, options, err := h.engine.ListWorkers(ctx)
	if err != nil {
		t.Fatalf("ListWorkers: %v", err)
	}
	if len(workers) == 0 || workers[0].WorkerRef != created.WorkerRef {
		t.Fatalf("the created worker is not at the top of the list: %+v", workers)
	}
	if workers[0].Source != workspace.WorkerSourceCreated {
		t.Errorf("listed source = %q, want CREATED", workers[0].Source)
	}
	corpusSeen := false
	for _, w := range workers[1:] {
		if w.Source == workspace.WorkerSourceCorpus && w.WorkerRef == "omar-reyes" {
			corpusSeen = true
		}
	}
	if !corpusSeen {
		t.Errorf("the corpus population disappeared from the list: %+v", workers)
	}
	if options.Currency != "USD" || len(options.JobCodes) == 0 {
		t.Fatalf("options arrived unusable: %+v", options)
	}

	// The governed workspace read discloses the created worker through the
	// same one WorkerFacts the capability gateway answers from. This is the
	// "one read path" claim: nothing about this call knows the worker was
	// created rather than shipped.
	assertWorkspaceDisclosesWorker(t, h, created)

	// Propose for the created worker. The current placement comes from the
	// governed read; the pay baseline from the worker's own durable record.
	proposed, err := h.engine.Propose(ctx, journeyProposalFor(created.WorkerRef))
	if err != nil {
		t.Fatalf("Propose(created worker): %v", err)
	}
	if proposed.Stage != workspace.JourneyStageProposed {
		t.Fatalf("stage = %s, want PROPOSED (the simulation must mint an executable proposal)", proposed.Stage)
	}
	if proposed.CurrentBase != "88000.00" {
		t.Errorf("current base = %q, want the created worker's own 88000.00 (not the ported scenario's)",
			proposed.CurrentBase)
	}
	if proposed.Current.JobCode != "OPS-HRBP2" || proposed.Current.Grade != "P2" {
		t.Errorf("current placement = %+v, want the governed read's own", proposed.Current)
	}
	if proposed.Current.OrgUnit != "people-ops" || proposed.Current.PayZone != "US-EAST" {
		t.Errorf("organizational placement = %+v, want the governed read's own", proposed.Current)
	}
	if proposed.WorkerName == "" {
		t.Error("the proposed journey names no worker")
	}

	// The journey lists under the key the worker list shows, not under a raw
	// entity id.
	journeys, err := h.engine.ListJourneys(ctx)
	if err != nil {
		t.Fatalf("ListJourneys: %v", err)
	}
	found := false
	for _, j := range journeys {
		if j.IntentID == proposed.IntentID {
			found = true
			if j.Worker.Id != created.WorkerID {
				t.Errorf("listed journey names worker %q, want the created worker's id %q",
					j.Worker.Id, created.WorkerID)
			}
		}
	}
	if !found {
		t.Fatalf("the promotion just proposed is not listed: %+v", journeys)
	}

	// Execute, approve, and check the one governed write.
	executed, err := h.engine.Execute(ctx, proposed.IntentID)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if executed.Summary.Stage != workspace.JourneyStageAwaitingApproval {
		t.Fatalf("stage = %s, want AWAITING_APPROVAL", executed.Summary.Stage)
	}
	decided, err := h.engine.Decide(h.approverCtx(t), proposed.IntentID, workspace.Decision{
		Approve: true, Reason: "promotion_approved_for_created_worker",
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if decided.Summary.Stage != workspace.JourneyStageCompleted {
		t.Fatalf("stage = %s, want COMPLETED", decided.Summary.Stage)
	}
	if decided.Ledger == nil {
		t.Fatal("a completed promotion recorded no ledger fact")
	}
	if decided.Instance == nil {
		t.Fatal("a completed promotion names no workflow instance")
	}
	if got := ledgerEventsOn(t, h.cell, decided.Instance.InstanceID); got != 1 {
		t.Fatalf("ledger events = %d, want exactly one governed business write", got)
	}
}

// assertWorkspaceDisclosesWorker drives the workspace's own governed read for
// one listed worker and asserts it discloses them.
//
// It is the single-read-path claim made checkable at the level that matters:
// workspace.Cell.ReadPromotion knows nothing about created workers, resolves
// the reference the way it always did, and still gets an answer -- because the
// cell composed one WorkerFacts and handed it to every path.
func assertWorkspaceDisclosesWorker(t *testing.T, h *journeyHarness, worker workspace.WorkerSummary) {
	t.Helper()
	query, err := workspace.DefaultQuery()
	if err != nil {
		t.Fatalf("workspace.DefaultQuery: %v", err)
	}
	query.WorkerRef = worker.WorkerRef
	query.CurrentBase = worker.BasePay
	query.BonusTargetPercent = worker.BonusTarget
	request, err := query.Typed()
	if err != nil {
		t.Fatalf("Query.Typed: %v", err)
	}

	reading, err := h.cell.app.WorkspacePort().ReadPromotion(h.operatorCtx(t), request)
	if err != nil {
		t.Fatalf("ReadPromotion(created worker): %v", err)
	}
	if reading.Explanation.Presence == people.SubjectAbsent {
		t.Fatalf("the workspace read discloses no such worker %q", worker.WorkerRef)
	}
	if string(reading.Preflight.Status) == "" {
		t.Errorf("the workspace read produced no preflight for a created worker: %+v", reading.Preflight)
	}
	disclosed := map[people.FieldID]string{}
	for _, fact := range reading.Explanation.AuthorizedFields() {
		if v, ok := fact.Value.Get(); ok {
			disclosed[fact.Field] = v
		}
	}
	if disclosed[people.FieldJobCode] != worker.JobCode || disclosed[people.FieldGrade] != worker.Grade {
		t.Errorf("the workspace read discloses %s/%s, want the created worker's %s/%s",
			disclosed[people.FieldJobCode], disclosed[people.FieldGrade], worker.JobCode, worker.Grade)
	}
	if disclosed[people.FieldPreferredName] != worker.PreferredName {
		t.Errorf("the workspace read discloses the name %q, want %q",
			disclosed[people.FieldPreferredName], worker.PreferredName)
	}
}

// ---------------------------------------------------------------------------
// Refusals
// ---------------------------------------------------------------------------

// TestJourneyCreateWorkerRequiresTheOperatorRole proves creating an employee
// is gated by the same P1B execution authority executing a promotion is: a
// caller who may read and propose may not add the subject of a promotion.
func TestJourneyCreateWorkerRequiresTheOperatorRole(t *testing.T) {
	h := newJourneyHarness(t)
	withoutRole := h.ctx(t, "intent_author", testRole)

	if _, err := h.engine.CreateWorker(withoutRole, journeyWorkerInput()); !errors.Is(err, workspace.ErrDenied) {
		t.Fatalf("CreateWorker without %s = %v, want ErrDenied", executionAuthorityTestRole, err)
	}

	// The refusal wrote nothing: the population is still the corpus alone.
	workers, _, err := h.engine.ListWorkers(h.operatorCtx(t))
	if err != nil {
		t.Fatalf("ListWorkers: %v", err)
	}
	for _, w := range workers {
		if w.Source == workspace.WorkerSourceCreated {
			t.Fatalf("a refused creation left a worker behind: %+v", w)
		}
	}

	// The refusal is recorded as evidence beside every other governed
	// decision this cell made, rather than vanishing.
	if !hasWorkforceEvidence(h, app.EvidenceKindWorkerRefused) {
		t.Error("a refused creation recorded no evidence")
	}
}

// TestJourneyCreateWorkerRefusesAPlacementNoBandCovers is the RED clause:
// creating a worker on a job code, grade or zone no pay band covers would
// produce somebody whose every promotion is refused later for a reason that
// has nothing to do with the promotion.
func TestJourneyCreateWorkerRefusesAPlacementNoBandCovers(t *testing.T) {
	h := newJourneyHarness(t)
	ctx := h.operatorCtx(t)

	for name, mutate := range map[string]func(*workspace.WorkerInput){
		"unknown job code": func(in *workspace.WorkerInput) { in.JobCode = "OPS-NOSUCH9" },
		"unknown grade":    func(in *workspace.WorkerInput) { in.Grade = "P9" },
		"unknown pay zone": func(in *workspace.WorkerInput) { in.PayZone = "EU-WEST" },
	} {
		t.Run(name, func(t *testing.T) {
			in := journeyWorkerInput()
			mutate(&in)
			_, err := h.engine.CreateWorker(ctx, in)
			if !errors.Is(err, workspace.ErrJourneyInput) {
				t.Fatalf("CreateWorker(%s) = %v, want ErrJourneyInput", name, err)
			}
			if !strings.Contains(err.Error(), "job_code") {
				t.Errorf("refusal %q does not name the field to fix", err)
			}
		})
	}

	t.Run("missing name", func(t *testing.T) {
		in := journeyWorkerInput()
		in.LegalName = ""
		if _, err := h.engine.CreateWorker(ctx, in); !errors.Is(err, workspace.ErrJourneyInput) {
			t.Fatalf("CreateWorker(no name) = %v, want ErrJourneyInput", err)
		}
	})

	t.Run("non-positive base pay", func(t *testing.T) {
		in := journeyWorkerInput()
		in.BasePay = "0.00"
		if _, err := h.engine.CreateWorker(ctx, in); !errors.Is(err, workspace.ErrJourneyInput) {
			t.Fatalf("CreateWorker(zero base) = %v, want ErrJourneyInput", err)
		}
	})

	t.Run("malformed hire date", func(t *testing.T) {
		in := journeyWorkerInput()
		in.HireDate = "05/04/2021"
		if _, err := h.engine.CreateWorker(ctx, in); !errors.Is(err, workspace.ErrJourneyInput) {
			t.Fatalf("CreateWorker(bad hire date) = %v, want ErrJourneyInput", err)
		}
	})
}

// TestJourneyCreateWorkerRefusesADuplicateReference documents the chosen
// answer for a colliding worker key: it is workspace.ErrJourneyInput naming
// worker_key, not a stage refusal.
//
// The reasoning is that the key is derived from a field on the form (the name)
// plus the minted identity, so a collision is something the person can act on
// by changing what they typed -- which is what ErrJourneyInput means. A stage
// refusal would say "not right now", and there is no later moment at which
// this creation would succeed.
//
// Reaching the collision at all takes a pinned id source: the key carries a
// short form of the freshly minted worker id, so two creations of the same
// person normally produce two different keys, which is the point. Pinning
// CellConfig.IDs makes the second creation mint the identity the first already
// used, which is the only way this branch can be reached and therefore the
// only way it can be tested.
func TestJourneyCreateWorkerRefusesADuplicateReference(t *testing.T) {
	const pinned = "0192f3c4-0000-7000-8000-00000000abcd"
	h := newJourneyHarness(t, func(cfg *app.CellConfig) {
		cfg.IDs = func() (string, error) { return pinned, nil }
	})
	ctx := h.operatorCtx(t)

	first, err := h.engine.CreateWorker(ctx, journeyWorkerInput())
	if err != nil {
		t.Fatalf("CreateWorker: %v", err)
	}
	if first.WorkerID != pinned {
		t.Fatalf("worker id = %q, want the pinned id source's own %q", first.WorkerID, pinned)
	}

	_, err = h.engine.CreateWorker(ctx, journeyWorkerInput())
	if !errors.Is(err, workspace.ErrJourneyInput) {
		t.Fatalf("CreateWorker(duplicate) = %v, want ErrJourneyInput", err)
	}
	if !strings.Contains(err.Error(), "worker_key") {
		t.Errorf("refusal %q does not name worker_key", err)
	}

	// The first worker is untouched: journey_worker is append-only, so a
	// refused second creation cannot have overwritten it.
	workers, _, err := h.engine.ListWorkers(ctx)
	if err != nil {
		t.Fatalf("ListWorkers: %v", err)
	}
	createdCount := 0
	for _, w := range workers {
		if w.Source == workspace.WorkerSourceCreated {
			createdCount++
		}
	}
	if createdCount != 1 {
		t.Fatalf("created population = %d, want exactly the one that was accepted", createdCount)
	}
}

// TestJourneyProposeRefusesAnUnknownWorker proves the resolver still refuses a
// reference neither population carries, rather than proposing a promotion for
// nobody.
func TestJourneyProposeRefusesAnUnknownWorker(t *testing.T) {
	h := newJourneyHarness(t)
	_, err := h.engine.Propose(h.operatorCtx(t), journeyProposalFor("nobody-at-all-9999"))
	if !errors.Is(err, workspace.ErrJourneyInput) {
		t.Fatalf("Propose(unknown worker) = %v, want ErrJourneyInput", err)
	}
}

// TestJourneyCreateWorkerRecordsEvidence proves the governed write leaves the
// same kind of trail every other decision in this cell leaves, on the one
// evidence sink Cell.Evidence reads back.
func TestJourneyCreateWorkerRecordsEvidence(t *testing.T) {
	h := newJourneyHarness(t)
	if _, err := h.engine.CreateWorker(h.operatorCtx(t), journeyWorkerInput()); err != nil {
		t.Fatalf("CreateWorker: %v", err)
	}
	if !hasWorkforceEvidence(h, app.EvidenceKindWorkerCreated) {
		t.Fatalf("an admitted creation recorded no evidence: %+v", h.cell.app.Evidence.Records())
	}
}

// hasWorkforceEvidence reports whether the cell's evidence sink carries a
// workforce decision of the given kind.
func hasWorkforceEvidence(h *journeyHarness, kind string) bool {
	for _, record := range h.cell.app.Evidence.Records() {
		if record.Decision == kind {
			return true
		}
	}
	return false
}

// TestJourneyWorkerRowsAreTenantScoped proves migration 00023's row level
// security reaches the composed cell: a worker created in this tenant is
// stored under this tenant's id and nobody else's.
func TestJourneyWorkerRowsAreTenantScoped(t *testing.T) {
	h := newJourneyHarness(t)
	created, err := h.engine.CreateWorker(h.operatorCtx(t), journeyWorkerInput())
	if err != nil {
		t.Fatalf("CreateWorker: %v", err)
	}
	got := queryOne[int](t, h.cell,
		`SELECT count(*) FROM journey_worker WHERE tenant_id = $1 AND worker_key = $2`,
		pgstore.TenantID(testTenant), created.WorkerRef)
	if got != 1 {
		t.Fatalf("journey_worker rows for this tenant = %d, want 1", got)
	}
	other := queryOne[int](t, h.cell,
		`SELECT count(*) FROM journey_worker WHERE tenant_id <> $1`, pgstore.TenantID(testTenant))
	if other != 0 {
		t.Fatalf("journey_worker carries %d rows outside this tenant", other)
	}
}
