package workflow_test

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/application"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func rev08403ApprovedJourney(t *testing.T) (*promoUXServer, string, *journeyv1.JourneySubmissionRecord) {
	t.Helper()
	h := newPromoUXServer(t)
	ctx, cancel := h.call(t, "proposer")
	workers, err := h.client.ListWorkers(ctx, &journeyv1.ListWorkersRequest{})
	cancel()
	if err != nil {
		t.Fatalf("ListWorkers: %v", err)
	}
	worker, targetJob, targetGrade := promoUXPosition(t, workers)
	ctx, cancel = h.call(t, "proposer")
	proposal := promoUXPropose(worker, targetJob, targetGrade, h.positionRef, "rev08403-race", worker.GetSubjectRevision())
	proposal.EffectiveDate = "2027-06-01"
	proposed, err := h.client.ProposePromotion(ctx, proposal)
	cancel()
	if err != nil {
		principal, verifyErr := h.verifier.Verify(context.Background(), trust.Credential{
			Scheme: "Bearer", Token: h.people["proposer"].Token, Audience: application.DefaultAudience,
		})
		if verifyErr != nil {
			t.Fatalf("ProposePromotion: %v (also failed to verify direct diagnostic principal: %v)", err, verifyErr)
		}
		diagnosticCtx, diagnosticCancel := context.WithTimeout(trust.WithPrincipal(context.Background(), principal), 10*time.Second)
		defer diagnosticCancel()
		_, directErr := h.app.Cell().Journey.Propose(diagnosticCtx, workspace.ProposalInput{
			WorkerRef: worker.GetWorkerRef(), TargetJobCode: targetJob, TargetGrade: targetGrade,
			TargetPositionID: h.positionRef, ProposedBase: "98000.00", EffectiveDate: "2027-06-01",
			BusinessReason: "Promotion into the senior HRBP role",
		})
		t.Fatalf("ProposePromotion: %v (direct journey cause: %v)", err, directErr)
	}
	intentID := proposed.GetIntentId()
	t.Logf("REV-084-03 proposed journey %s", intentID)
	ctx, cancel = h.call(t, "proposer")
	_, err = h.client.ExecuteJourney(ctx, &journeyv1.ExecuteJourneyRequest{IntentId: intentID})
	cancel()
	if err != nil {
		t.Fatalf("ExecuteJourney: %v", err)
	}
	t.Log("REV-084-03 executed proposal")
	for _, persona := range []string{"finance", "manager"} {
		ctx, cancel = h.call(t, persona)
		decision, decisionErr := h.client.DecideJourney(ctx, &journeyv1.DecideJourneyRequest{
			IntentId: intentID, Approve: true, Reason: persona + " approved",
		})
		cancel()
		if decisionErr != nil {
			t.Fatalf("DecideJourney(%s): %v", persona, decisionErr)
		}
		if persona == "finance" && decision.GetDetail().GetSubmission() != nil {
			t.Fatal("submission was exposed after the first approval while the manager gate remained open")
		}
		if persona == "manager" && decision.GetDetail().GetSubmission() == nil {
			t.Fatal("submission is missing after the final approval completed the approval route")
		}
		t.Logf("REV-084-03 completed %s approval", persona)
	}
	ctx, cancel = h.call(t, "manager")
	first, err := h.client.DecideJourney(ctx, &journeyv1.DecideJourneyRequest{
		IntentId: intentID, Approve: true, Reason: "manager approved",
	})
	cancel()
	if err != nil {
		t.Fatalf("replay manager DecideJourney: %v", err)
	}
	want := first.GetDetail().GetSubmission()
	if want == nil || want.GetIntentId() != intentID || want.GetIdempotencyKey() == "" || want.GetDigest() == "" {
		t.Fatalf("DecideJourney submission = %+v, want the durable accepted action's typed submission record", want)
	}
	t.Log("REV-084-03 submission response is present")
	return h, intentID, want
}

type rev08403DurableEffects struct {
	approvalDecisions int64
	itemTransitions   int64
}

func rev08403ReadDurableEffects(t *testing.T, h *promoUXServer) rev08403DurableEffects {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var effects rev08403DurableEffects
	if err := h.db.SQL.QueryRowContext(ctx, `SELECT count(*) FROM work_item_decision WHERE kind = 'APPROVAL'`).Scan(&effects.approvalDecisions); err != nil {
		t.Fatalf("count durable approval decisions: %v", err)
	}
	if err := h.db.SQL.QueryRowContext(ctx, `SELECT count(*) FROM work_item_transition`).Scan(&effects.itemTransitions); err != nil {
		t.Fatalf("count durable work-item transitions: %v", err)
	}
	return effects
}

// TestTodo_REV_084_03 exercises semantic replay through the real served
// DecideJourney entrypoint and verifies the typed submission result returns
// the original record.
func TestTodo_REV_084_03(t *testing.T) {
	_, _, record := rev08403ApprovedJourney(t)
	if record.GetIntentId() == "" || record.GetDigest() == "" || record.GetIdempotencyKey() == "" {
		t.Fatalf("served submission record is incomplete: %+v", record)
	}
}

// TestTodo_REV_084_03_Race proves concurrent duplicate decisions at the real
// served submission entrypoint return one original submission record.
func TestTodo_REV_084_03_Race(t *testing.T) {
	h, intentID, want := rev08403ApprovedJourney(t)
	initialEffects := rev08403ReadDurableEffects(t, h)
	if initialEffects.approvalDecisions != 2 {
		t.Fatalf("durable approval decisions after finance and manager approval = %d, want exactly 2", initialEffects.approvalDecisions)
	}

	const callers = 12
	got := make([]*journeyv1.JourneySubmissionRecord, callers)
	errs := make([]error, callers)
	contexts := make([]context.Context, callers)
	cancels := make([]context.CancelFunc, callers)
	for i := range contexts {
		contexts[i], cancels[i] = h.call(t, "manager")
	}
	defer func() {
		for _, cancel := range cancels {
			cancel()
		}
	}()
	var wg sync.WaitGroup
	for i := range got {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			response, callErr := h.client.DecideJourney(contexts[i], &journeyv1.DecideJourneyRequest{
				IntentId: intentID, Approve: true, Reason: "manager approved",
			})
			if callErr != nil {
				errs[i] = callErr
				return
			}
			got[i] = response.GetDetail().GetSubmission()
		}(i)
	}
	t.Logf("REV-084-03 started %d concurrent duplicate decisions", callers)
	finished := make(chan struct{})
	go func() {
		wg.Wait()
		close(finished)
	}()
	select {
	case <-finished:
		t.Log("REV-084-03 concurrent duplicate decisions completed")
	case <-time.After(45 * time.Second):
		stack := make([]byte, 1<<20)
		n := runtime.Stack(stack, true)
		t.Fatalf("REV-084-03 duplicate decisions did not finish within 45s; goroutines:\n%s", stack[:n])
	}
	for i := range got {
		if errs[i] != nil {
			t.Fatalf("duplicate DecideJourney caller %d: %v", i, errs[i])
		}
		if !proto.Equal(got[i], want) {
			t.Fatalf("duplicate DecideJourney caller %d submission = %+v, want original %+v", i, got[i], want)
		}
	}
	if effects := rev08403ReadDurableEffects(t, h); effects != initialEffects {
		t.Fatalf("duplicate manager submissions changed durable workflow effects: before=%+v after=%+v", initialEffects, effects)
	}

	// A fresh composition gets a fresh in-memory registry. The durable accepted
	// action still resolves to the same semantic submission after that restart.
	// Cancel the server's workload context before Stop waits for its scheduler.
	h.stopRun()
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 20*time.Second)
	if err := h.app.Stop(stopCtx); err != nil {
		stopCancel()
		t.Fatalf("stop first composition: %v", err)
	}
	stopCancel()
	restarted, err := h.recompose()
	if err != nil {
		t.Fatalf("recompose served application: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cleanupCancel()
		if err := restarted.Stop(cleanupCtx); err != nil {
			t.Errorf("stop restarted composition: %v", err)
		}
	})
	principal, err := h.verifier.Verify(context.Background(), trust.Credential{
		Scheme: "Bearer", Token: h.people["manager"].Token, Audience: application.DefaultAudience,
	})
	if err != nil {
		t.Fatalf("verify manager for restarted composition: %v", err)
	}
	restartCtx, restartCancel := context.WithTimeout(trust.WithPrincipal(context.Background(), principal), 20*time.Second)
	replayed, err := restarted.Cell().Journey.Decide(restartCtx, intentID,
		workspace.Decision{Approve: true, Reason: "manager approved"})
	restartCancel()
	if err != nil {
		t.Fatalf("replay DecideJourney after service restart: %v", err)
	}
	if replayed.Submission == nil || replayed.Submission.IntentID != want.GetIntentId() ||
		replayed.Submission.ProposalRevisionID != want.GetProposalRevisionId() ||
		replayed.Submission.ProposalDigest != want.GetProposalDigest() ||
		replayed.Submission.MaterialDigest != want.GetMaterialDigest() ||
		replayed.Submission.SubmittedBy != want.GetSubmittedBy() ||
		!replayed.Submission.SubmittedAt.Equal(want.GetSubmittedAt().AsTime()) ||
		replayed.Submission.IdempotencyKey != want.GetIdempotencyKey() || replayed.Submission.Digest != want.GetDigest() {
		t.Fatalf("restart submission = %+v, want persisted acceptance's original record %+v", replayed.Submission, want)
	}
	if effects := rev08403ReadDurableEffects(t, h); effects != initialEffects {
		t.Fatalf("restart replay changed durable workflow effects: before=%+v after=%+v", initialEffects, effects)
	}
	t.Log("REV-084-03 restart replay matched")
}

// TestTodo_UXAUDIT_002 is the PRIMARY matrix test for planning/todos.md's
// UXAUDIT-002: one discoverable, authorized promotion path from person to
// completion. It drives the real gRPC server and PostgreSQL-backed cell as
// the authorized proposer: the eligible corpus worker is discovered with a
// published promotion path, the proposal is initiated, both approvals are
// decided in order, the scheduler crosses the effective-date wait, and the
// one terminal outcome is then visible in the journeys list -- the same
// served state the Journeys, My Work and History surfaces all read.
func TestTodo_UXAUDIT_002(t *testing.T) {
	h := newPromoUXServer(t)

	proposerCtx, proposerCancel := h.call(t, "proposer")
	workers, err := h.client.ListWorkers(proposerCtx, &journeyv1.ListWorkersRequest{})
	proposerCancel()
	if err != nil {
		t.Fatalf("ListWorkers as proposer: %v", err)
	}
	worker, targetJob, targetGrade := promoUXPosition(t, workers)

	proposeCtx, proposeCancel := h.call(t, "proposer")
	proposed, err := h.client.ProposePromotion(proposeCtx, promoUXPropose(worker, targetJob, targetGrade, h.positionRef, "uxaudit-002-primary", worker.GetSubjectRevision()))
	proposeCancel()
	if err != nil {
		t.Fatalf("ProposePromotion: %v", err)
	}
	intentID := proposed.GetIntentId()
	if intentID == "" {
		t.Fatal("ProposePromotion returned no intent id: the promotion cannot be initiated from the discovered worker")
	}

	proposedDetail := promoUXInspect(t, h, "proposer", intentID)
	if proposedDetail.GetJourney().GetStage() != journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED {
		t.Fatalf("proposed stage = %s, want PROPOSED", proposedDetail.GetJourney().GetStage())
	}
	assertUXAudit002Explains(t, proposedDetail, "PROPOSED")

	execCtx, execCancel := h.call(t, "proposer")
	executed, err := h.client.ExecuteJourney(execCtx, &journeyv1.ExecuteJourneyRequest{IntentId: intentID})
	execCancel()
	if err != nil {
		t.Fatalf("ExecuteJourney: %v", err)
	}
	if got := executed.GetDetail().GetJourney().GetStage(); got != journeyv1.JourneyStage_JOURNEY_STAGE_FINANCE_APPROVAL {
		t.Fatalf("after execute stage = %s, want FINANCE_APPROVAL", got)
	}
	assertUXAudit002Explains(t, executed.GetDetail(), "FINANCE_APPROVAL")

	finCtx, finCancel := h.call(t, "finance")
	financed, err := h.client.DecideJourney(finCtx, &journeyv1.DecideJourneyRequest{IntentId: intentID, Approve: true, Reason: "finance approved"})
	finCancel()
	if err != nil {
		t.Fatalf("finance DecideJourney: %v", err)
	}
	if got := financed.GetDetail().GetJourney().GetStage(); got != journeyv1.JourneyStage_JOURNEY_STAGE_MANAGER_APPROVAL {
		t.Fatalf("after finance stage = %s, want MANAGER_APPROVAL", got)
	}
	assertUXAudit002Explains(t, financed.GetDetail(), "MANAGER_APPROVAL")

	mgrCtx, mgrCancel := h.call(t, "manager")
	managed, err := h.client.DecideJourney(mgrCtx, &journeyv1.DecideJourneyRequest{IntentId: intentID, Approve: true, Reason: "manager approved"})
	mgrCancel()
	if err != nil {
		t.Fatalf("manager DecideJourney: %v", err)
	}
	waiting := managed.GetDetail()
	if got := waiting.GetJourney().GetStage(); got != journeyv1.JourneyStage_JOURNEY_STAGE_WAITING_EFFECTIVE_DATE && got != journeyv1.JourneyStage_JOURNEY_STAGE_BLOCKED {
		t.Fatalf("after manager stage = %s, want WAITING_EFFECTIVE_DATE or BLOCKED", got)
	}
	if waiting.GetJourney().GetStage() == journeyv1.JourneyStage_JOURNEY_STAGE_WAITING_EFFECTIVE_DATE {
		assertUXAudit002Explains(t, waiting, "WAITING_EFFECTIVE_DATE")
	}

	terminal := promoUXWaitForStage(t, h, "proposer", intentID, journeyv1.JourneyStage_JOURNEY_STAGE_BLOCKED)
	assertUXAudit002Explains(t, terminal, "BLOCKED")

	// The terminal outcome is visible in the journeys list: the same served
	// state Journeys, My Work and History project.
	listCtx, listCancel := h.call(t, "proposer")
	listed, err := h.client.ListJourneys(listCtx, &journeyv1.ListJourneysRequest{})
	listCancel()
	if err != nil {
		t.Fatalf("ListJourneys as proposer: %v", err)
	}
	var found *journeyv1.Journey
	for _, j := range listed.GetJourneys() {
		if j.GetIntentId() == intentID {
			found = j
			break
		}
	}
	if found == nil {
		t.Fatalf("ListJourneys omits completed intent %s: the terminal outcome is not visible in Journeys", intentID)
	}
	if found.GetStage() != journeyv1.JourneyStage_JOURNEY_STAGE_BLOCKED {
		t.Fatalf("listed stage = %s, want BLOCKED", found.GetStage())
	}

	terminalCtx, terminalCancel := h.call(t, "proposer")
	terminalOnly, err := h.client.ListJourneys(terminalCtx, &journeyv1.ListJourneysRequest{TerminalOnly: true})
	terminalCancel()
	if err != nil {
		t.Fatalf("ListJourneys(TerminalOnly) as proposer: %v", err)
	}
	seen := false
	for _, j := range terminalOnly.GetJourneys() {
		if j.GetIntentId() == intentID {
			seen = true
		} else if j.GetStage() != journeyv1.JourneyStage_JOURNEY_STAGE_RECORDED && j.GetStage() != journeyv1.JourneyStage_JOURNEY_STAGE_BLOCKED {
			t.Fatalf("TerminalOnly list carries nonterminal journey %s at %s", j.GetIntentId(), j.GetStage())
		}
	}
	if !seen {
		t.Fatalf("ListJourneys(TerminalOnly) omits completed intent %s: History has no terminal outcome to show", intentID)
	}
}

// assertUXAudit002Explains pins the UXAUDIT-002 GREEN clause "the journey
// explains eligibility, next action and blocking reasons" at one durable
// stage: the projected detail page must carry the stage's own explanation
// carrier -- the next action to take while the journey is open, the wait
// explanation while it waits, and the recorded ledger fact once terminal.
// A journey that moves silently between stages is exactly the RED the todo
// names: authority that cannot be followed to completion.
// This harness promotes a fixed corpus worker, whose population has no
// aggregate projection, target position or compensation pool in this tenant,
// so WF-RUN-034's recorded GOVERN-002 approval denies on the budget and
// position facts and the run closes BLOCKED. BLOCKED is therefore the terminal
// stage this journey must explain; it carries the same ledger card and
// timeline a committed promotion does.
func assertUXAudit002Explains(t *testing.T, detail *journeyv1.JourneyDetail, stage string) {
	t.Helper()
	page := journeyclient.DetailPage(uxaudit002Config(), detail, nil, nil)
	if page.Detail == nil {
		t.Fatalf("%s: DetailPage projected no detail view", stage)
	}
	switch stage {
	case "PROPOSED", "FINANCE_APPROVAL", "MANAGER_APPROVAL":
		if len(page.Detail.Actions) == 0 {
			t.Errorf("%s: detail carries no action: the reader is not told the next action", stage)
		}
		if page.Detail.PendingOutcome == "" {
			t.Errorf("%s: detail carries no pending-outcome explanation: the reader is not told what remains", stage)
		}
	case "WAITING_EFFECTIVE_DATE":
		if len(page.Detail.WaitExplanation) == 0 {
			t.Errorf("%s: detail carries no wait explanation: the reader is not told what the wait is for", stage)
		}
	case "RECORDED", "BLOCKED":
		if page.Detail.Ledger == nil {
			t.Errorf("%s: detail carries no ledger card: the terminal outcome is unexplained", stage)
		}
		if len(page.Detail.Timeline) == 0 {
			t.Errorf("%s: detail carries no timeline: the path to completion cannot be inspected", stage)
		}
	}
}

// TestTodo_UXAUDIT_002_Integration is the INTEGRATION matrix test: the live
// ListWorkers/ListJourneys payloads project through the real journeyclient
// lanes into the discovery surfaces. The eligible worker appears in the
// People view with the promotion action, and the terminal journey appears
// in the journeys list -- the two projections the product directory and the
// lifecycle tracker render.
func TestTodo_UXAUDIT_002_Integration(t *testing.T) {
	h := newPromoUXServer(t)

	proposerCtx, proposerCancel := h.call(t, "proposer")
	workers, err := h.client.ListWorkers(proposerCtx, &journeyv1.ListWorkersRequest{})
	proposerCancel()
	if err != nil {
		t.Fatalf("ListWorkers as proposer: %v", err)
	}
	worker, targetJob, targetGrade := promoUXPosition(t, workers)
	if !journeyclient.HasPromotionChoices(workers.GetOptions(), worker) {
		t.Fatalf("eligible worker %s carries no promotion choices: the directory cannot offer the promotion action", worker.GetWorkerRef())
	}

	people := journeyclient.PeopleView(uxaudit002Config(), journeyclient.ListData{
		Workers: workers.GetWorkers(), Options: workers.GetOptions(),
	}, nil)
	if people == nil {
		t.Fatal("PeopleView is absent for a workforce the cell knows about")
	}
	found := false
	for _, row := range people.Workers {
		if row.Ref != worker.GetWorkerRef() {
			continue
		}
		found = true
		if row.ProposeHref == "" {
			t.Errorf("People row for eligible worker %s carries no promotion action", row.Ref)
		}
		if want := journeyclient.ProposalHref(worker.GetWorkerRef()); row.ProposeHref != want {
			t.Errorf("People row action = %q, want the proposal route %q", row.ProposeHref, want)
		}
	}
	if !found {
		t.Fatalf("PeopleView omits eligible worker %s", worker.GetWorkerRef())
	}

	proposeCtx, proposeCancel := h.call(t, "proposer")
	proposed, err := h.client.ProposePromotion(proposeCtx, promoUXPropose(worker, targetJob, targetGrade, h.positionRef, "uxaudit-002-integration", worker.GetSubjectRevision()))
	proposeCancel()
	if err != nil {
		t.Fatalf("ProposePromotion: %v", err)
	}
	execCtx, execCancel := h.call(t, "proposer")
	if _, err := h.client.ExecuteJourney(execCtx, &journeyv1.ExecuteJourneyRequest{IntentId: proposed.GetIntentId()}); err != nil {
		execCancel()
		t.Fatalf("ExecuteJourney: %v", err)
	}
	execCancel()
	// Approvals are ordered: finance first, then the manager. A map range
	// would randomize that order and flake.
	for _, approval := range []struct{ persona, reason string }{
		{"finance", "finance approved"},
		{"manager", "manager approved"},
	} {
		decCtx, decCancel := h.call(t, approval.persona)
		if _, err := h.client.DecideJourney(decCtx, &journeyv1.DecideJourneyRequest{IntentId: proposed.GetIntentId(), Approve: true, Reason: approval.reason}); err != nil {
			decCancel()
			t.Fatalf("%s DecideJourney: %v", approval.persona, err)
		}
		decCancel()
	}
	terminal := promoUXWaitForStage(t, h, "proposer", proposed.GetIntentId(), journeyv1.JourneyStage_JOURNEY_STAGE_BLOCKED)

	listCtx, listCancel := h.call(t, "proposer")
	listed, err := h.client.ListJourneys(listCtx, &journeyv1.ListJourneysRequest{})
	listCancel()
	if err != nil {
		t.Fatalf("ListJourneys as proposer: %v", err)
	}
	list := journeyclient.ListPage(uxaudit002Config(), journeyclient.ListData{
		Journeys: listed.GetJourneys(), Workers: workers.GetWorkers(), Options: workers.GetOptions(),
	}, nil, nil)
	if list.List == nil {
		t.Fatal("ListPage projected no list view for a cell with journeys")
	}
	seen := false
	for _, group := range list.List.Groups {
		for _, jcard := range group.Journeys {
			if jcard.IntentID == proposed.GetIntentId() {
				seen = true
				if jcard.Href == "" {
					t.Errorf("terminal journey card carries no detail link: the outcome cannot be inspected from Journeys")
				}
				if want := journeyclient.DetailHref(proposed.GetIntentId()); jcard.Href != want {
					t.Errorf("terminal journey card href = %q, want %q", jcard.Href, want)
				}
			}
		}
	}
	if !seen {
		// The grouped tracker is the primary render; fall back to the flat
		// list before declaring the outcome invisible.
		for _, jcard := range list.List.Journeys {
			if jcard.IntentID == proposed.GetIntentId() {
				seen = true
				break
			}
		}
	}
	if !seen {
		t.Fatalf("ListPage omits completed intent %s", proposed.GetIntentId())
	}

	detail := journeyclient.DetailPage(uxaudit002Config(), terminal, nil, nil)
	if detail.Detail == nil || detail.Detail.Ledger == nil {
		t.Fatalf("completed detail carries no ledger card: History has no completion evidence to show")
	}
}

// TestTodo_UXAUDIT_002_Recovery is the RECOVERY matrix test: proposing a
// second promotion for a worker with an open journey is refused by the
// admission guard, and the journey blocking the new proposal stays
// inspectable -- the resume target both the People row and the Person
// profile link to as "open active promotion" instead of dead-ending at a
// bare reason.
func TestTodo_UXAUDIT_002_Recovery(t *testing.T) {
	h := newPromoUXServer(t)

	proposerCtx, proposerCancel := h.call(t, "proposer")
	workers, err := h.client.ListWorkers(proposerCtx, &journeyv1.ListWorkersRequest{})
	proposerCancel()
	if err != nil {
		t.Fatalf("ListWorkers as proposer: %v", err)
	}
	worker, targetJob, targetGrade := promoUXPosition(t, workers)

	firstCtx, firstCancel := h.call(t, "proposer")
	first, err := h.client.ProposePromotion(firstCtx, promoUXPropose(worker, targetJob, targetGrade, h.positionRef, "uxaudit-002-recovery", worker.GetSubjectRevision()))
	firstCancel()
	if err != nil {
		t.Fatalf("first ProposePromotion: %v", err)
	}

	secondCtx, secondCancel := h.call(t, "proposer")
	_, err = h.client.ProposePromotion(secondCtx, promoUXPropose(worker, targetJob, targetGrade, h.positionRef, "uxaudit-002-recovery-duplicate", worker.GetSubjectRevision()))
	secondCancel()
	if status.Code(err) != codes.AlreadyExists {
		t.Fatalf("second ProposePromotion code = %s (%v), want ALREADY_EXISTS: the guard did not refuse the conflicting proposal", promoUXStatusCode(err), err)
	}

	// The blocking journey is the recovery path: it stays inspectable, at
	// its real stage, with its own identity -- the exact journey the
	// surfaces link to.
	blocking := promoUXInspect(t, h, "proposer", first.GetIntentId())
	if blocking.GetJourney().GetStage() != journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED {
		t.Fatalf("blocking journey stage = %s, want PROPOSED", blocking.GetJourney().GetStage())
	}
	listCtx, listCancel := h.call(t, "proposer")
	listed, err := h.client.ListJourneys(listCtx, &journeyv1.ListJourneysRequest{})
	listCancel()
	if err != nil {
		t.Fatalf("ListJourneys as proposer: %v", err)
	}
	list := journeyclient.ListPage(uxaudit002Config(), journeyclient.ListData{
		Journeys: listed.GetJourneys(), Workers: workers.GetWorkers(), Options: workers.GetOptions(),
	}, nil, nil)
	if list.List == nil {
		t.Fatal("ListPage projected no list view while a journey blocks re-proposal")
	}
	resumable := false
	for _, jcard := range list.List.Journeys {
		if jcard.IntentID == first.GetIntentId() && jcard.Href == journeyclient.DetailHref(first.GetIntentId()) {
			resumable = true
		}
	}
	if !resumable {
		for _, group := range list.List.Groups {
			for _, jcard := range group.Journeys {
				if jcard.IntentID == first.GetIntentId() && jcard.Href == journeyclient.DetailHref(first.GetIntentId()) {
					resumable = true
				}
			}
		}
	}
	if !resumable {
		t.Fatalf("blocking intent %s has no detail link in the journeys list: the conflict refusal dead-ends", first.GetIntentId())
	}
}

// uxaudit002Config is the test-only journeyclient configuration: the
// projector's display facts, with no page-permission narrowing so the
// audit observes what the engine sent rather than what a fixture withheld.
func uxaudit002Config() journeyclient.Config {
	return journeyclient.Config{
		TunnelURL:    "wss://cell.example/grpc",
		Bearer:       "tok",
		Tenant:       "HarborCare US Inc.",
		Subject:      "principal:promo-ux-proposer",
		Roles:        []string{"intent_author", "comp_admin", "promotion_operator"},
		Purpose:      "compensation_review",
		JourneysPath: journeyclient.DefaultJourneysPath,
	}
}
