package workflow_test

import (
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

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
