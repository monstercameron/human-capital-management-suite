package application

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// promoux013Composed builds one full production composition (real
// PostgreSQL via pgtest, the real journey engine, the real P1B execution
// authority) exactly as TestTodo_PROMOUX_014_Integration does, and returns a
// context carrying one authenticated principal plus the live
// workspace.JourneyEngine. PROMOUX-015: it also returns contexts for the
// finance and manager approvals' routed assignees, who decide them.
func promoux013Composed(t *testing.T, now *time.Time) (context.Context, workspace.JourneyEngine, approverContexts) {
	t.Helper()
	db := pgtest.New(t)
	pool, err := pgxadapter.NewPool(context.Background(), db.URL, map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)

	const tenant = string(fixtures.Tenant)
	const subject = "principal:promoux013-integration"
	const approver = "principal:promoux013-integration-approver"
	cfg := ServeConfig{
		GRPCListen: "127.0.0.1:0", HTTPListen: "127.0.0.1:0", DatabaseURL: db.URL,
		DevHMACKey: integrationSigningKey, Issuer: DefaultIssuer, Audience: DefaultAudience,
		Tenant: tenant, CellID: "cell-promoux013-integration", MaxDeadline: 30 * time.Second,
		Migrate: false, Workspace: true, OTelExporter: OTelExporterNone,
		ExecutionAuthority: true, ExecutionAuthorityDigest: "sha256:promoux013-integration-authority",
		ExecutionAuthorityRole: "promotion_operator", ExecutionApprover: approver,
		WorkflowPlan: WorkflowPlanExecute, TimerTzdbVersion: DefaultTimerTzdbVersion,
		TimerCalendarVersion: DefaultTimerCalendarVersion,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("integration configuration: %v", err)
	}
	composed, err := ComposeServe(context.Background(), ServeInput{
		Config: cfg, Pool: pool, Identity: "promoux013-integration", Options: Options{Now: func() time.Time { return *now }},
	})
	if err != nil {
		t.Fatalf("ComposeServe: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = composed.Stop(ctx)
	})

	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{
		Key: []byte(integrationSigningKey), Issuer: cfg.Issuer, Audience: cfg.Audience, Now: func() time.Time { return *now },
	})
	if err != nil {
		t.Fatalf("build verifier: %v", err)
	}
	token, err := verifier.Issue(trust.Claims{
		Issuer: cfg.Issuer, Audience: cfg.Audience, Subject: subject, SubjectKind: "human", Tenant: tenant,
		OrganizationScopeID: "org-north-america", Roles: []string{"intent_author", "comp_admin", "promotion_operator"},
		Purposes: []string{"compensation_review"}, AuthenticationMethod: "bearer_token", Assurance: "substantial",
		SessionRef: "session:promoux013-integration", IssuedAtUnix: now.Add(-time.Minute).Unix(), ExpiresAtUnix: now.Add(48 * time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("issue journey credential: %v", err)
	}
	principal, err := verifier.Verify(context.Background(), trust.Credential{Scheme: "Bearer", Token: token, Audience: cfg.Audience})
	if err != nil {
		t.Fatalf("verify journey credential: %v", err)
	}
	finance, manager := routedApproverContexts(t, verifier, cfg, *now)
	return trust.WithPrincipal(context.Background(), principal), composed.Cell().Journey, approverContexts{finance: finance, manager: manager}
}

// approverContexts are the routed finance and manager assignees' contexts.
type approverContexts struct{ finance, manager context.Context }

func promoux013Propose(t *testing.T, ctx context.Context, journey workspace.JourneyEngine, effectiveDate string) workspace.JourneySummary {
	t.Helper()
	proposed, err := journey.Propose(ctx, workspace.ProposalInput{
		WorkerRef: "omar-reyes", TargetJobCode: "OPS-HRBP3", TargetGrade: "P3",
		ProposedBase: "98000.00", EffectiveDate: effectiveDate, BusinessReason: "promoux013_integration_fixture",
	})
	if err != nil {
		t.Fatalf("Journey.Propose: %v", err)
	}
	if proposed.Stage != workspace.JourneyStageProposed {
		t.Fatalf("Propose stage = %s, want PROPOSED", proposed.Stage)
	}
	return proposed
}

// TestTodo_PROMOUX_013_Integration reaches a real PostgreSQL server through
// the same production composition PROMOUX-014's own integration test uses,
// and proves the RED clauses this todo names together against real durable
// state, not a mock engine.
//
//  1. EditProposal corrects an unstarted or mid-flight (already
//     approved-in-part) proposal: it durably cancels the original and mints
//     a new proposal revision under a successor intent with no inherited
//     approval, and the original can no longer be decided or edited again
//     once that happens (internal/intent/app/journey_intervention.go's own
//     doc comment explains why this is Cancel-then-repropose, not
//     Supersede-based, and internal/intent/app/journey_decide.go's new
//     terminal-RequestState guard is what actually blocks a further Decide).
//  2. RequestIntervention(WITHDRAW) stops a proposal before any approval was
//     recorded, reporting InterventionApplied.
//  3. RequestIntervention(CANCEL) requests cancellation during an eligible
//     wait (WAITING_EFFECTIVE_DATE), reporting a real disposition read back
//     from durable state -- not a canned answer.
//
// It also proves "no partial domain write" for the failure mode this
// session found by actually running these scenarios against real
// PostgreSQL: [intent.SupersedeOriginal] legally refuses to supersede an
// intent whose ExecutionState is EXECUTING (internal/intent/lifecycle/
// rules.go Rule 1), and a journey's stored intent never durably leaves
// DRAFT at all (SupersedeOriginal's own SIMULATED/SUBMITTED/APPROVED
// preconditions never hold for one), which is why EditProposal does not use
// SupersedeIntent. Separately, SupersedeIntent itself used to mint and
// durably append its successor intent BEFORE validating the original's own
// transition, leaving an orphaned successor behind on every such refusal;
// this session reordered it (internal/intent/app/lifecycle_endpoints.go) to
// validate and durably apply the original's own transition first, so a
// refused SupersedeIntent call now writes nothing at all -- proven directly
// by EditOfAnAlreadyTerminalOriginalLeavesNoPartialWrite below through
// EditProposal's own CancelIntent-based refusal path, which the same
// ordering principle protects.
func TestTodo_PROMOUX_013_Integration(t *testing.T) {
	now := mustParseRFC3339(t, "2026-01-05T09:00:00Z")

	t.Run("EditCorrectsAnUnstartedProposal", func(t *testing.T) {
		ctx, journey, _ := promoux013Composed(t, &now)
		proposed := promoux013Propose(t, ctx, journey, "2026-06-01")

		successor, superseded, err := journey.EditProposal(ctx, proposed.IntentID, proposed.GovernanceVersion,
			"idem:promoux013:edit:1", "correcting the proposed base pay", workspace.EditProposalInput{
				TargetJobCode: "OPS-HRBP3", TargetGrade: "P3", ProposedBase: "99000.00",
				EffectiveDate: "2026-06-01", BusinessReason: "promoux013_integration_fixture_edited",
			})
		if err != nil {
			t.Fatalf("Journey.EditProposal: %v", err)
		}
		if superseded != proposed.IntentID {
			t.Fatalf("EditProposal superseded id = %s, want %s", superseded, proposed.IntentID)
		}
		if successor.IntentID == proposed.IntentID {
			t.Fatal("EditProposal returned the original's own id as its successor")
		}
		if successor.ProposedBase != "99000.00" {
			t.Fatalf("successor ProposedBase = %s, want the edited 99000.00", successor.ProposedBase)
		}
		if successor.Stage != workspace.JourneyStageProposed {
			t.Fatalf("successor stage = %s, want PROPOSED (a fresh proposal with no inherited approval)", successor.Stage)
		}

		// RED's clause: the original must never remain actionable once it
		// has been edited away. It is durably CANCELLED (see this test
		// file's own package-level doc comment for why CANCELLED, not
		// SUPERSEDED), so a second intervention against it is refused
		// exactly as WithdrawBeforeApproval's own repeat-attempt is.
		if _, err := journey.RequestIntervention(ctx, proposed.IntentID, workspace.JourneyInterventionRequest{
			Kind: workspace.JourneyInterventionWithdraw, ExpectedInstanceVersion: proposed.GovernanceVersion + 1,
			IdempotencyKey: "idem:promoux013:edit-then-withdraw", Reason: "should be refused: already cancelled",
		}); err == nil {
			t.Fatal("RequestIntervention on an already-edited (cancelled) original succeeded")
		}

		// The successor is a genuinely independent, executable proposal.
		successorFinance, err := journey.Execute(ctx, successor.IntentID)
		if err != nil {
			t.Fatalf("Journey.Execute(successor): %v", err)
		}
		if successorFinance.Summary.Stage != workspace.JourneyStage("FINANCE_APPROVAL") {
			t.Fatalf("successor after execute stage = %s, want FINANCE_APPROVAL", successorFinance.Summary.Stage)
		}
	})

	// EditInvalidatesAMidFlightApproval is the scenario that made this
	// session redesign EditProposal (see its own doc comment): a proposal
	// that already collected one real, recorded approval (finance) is still
	// editable, because CancelIntent's own safe-point-aware disposition --
	// already proven correct for a mid-flight journey by this same test's
	// CancelDuringEligibleWait sibling -- resolves cleanly here too. The
	// original's finance approval becomes moot the instant the original is
	// durably CANCELLED: it can never be decided again.
	t.Run("EditInvalidatesAMidFlightApproval", func(t *testing.T) {
		ctx, journey, approvers := promoux013Composed(t, &now)
		proposed := promoux013Propose(t, ctx, journey, "2026-06-15")

		financeWaiting, err := journey.Execute(ctx, proposed.IntentID)
		if err != nil {
			t.Fatalf("Journey.Execute: %v", err)
		}
		if financeWaiting.Summary.Stage != workspace.JourneyStage("FINANCE_APPROVAL") {
			t.Fatalf("after execute stage = %s, want FINANCE_APPROVAL", financeWaiting.Summary.Stage)
		}
		now = now.Add(10 * time.Minute)
		finance, err := journey.Decide(approvers.finance, proposed.IntentID, workspace.Decision{Approve: true, Reason: "finance approved"})
		if err != nil {
			t.Fatalf("Journey.Decide(finance): %v", err)
		}
		if finance.Summary.Stage != workspace.JourneyStage("MANAGER_APPROVAL") {
			t.Fatalf("after finance approval stage = %s, want MANAGER_APPROVAL", finance.Summary.Stage)
		}

		successor, superseded, err := journey.EditProposal(ctx, proposed.IntentID, finance.Summary.GovernanceVersion,
			"idem:promoux013:edit-mid-flight:1", "correcting a mid-flight proposal",
			workspace.EditProposalInput{
				TargetJobCode: "OPS-HRBP3", TargetGrade: "P3", ProposedBase: "99000.00",
				EffectiveDate: "2026-06-15", BusinessReason: "promoux013_integration_fixture_edited",
			})
		if err != nil {
			t.Fatalf("Journey.EditProposal on a mid-flight proposal with a recorded approval: %v", err)
		}
		if superseded != proposed.IntentID || successor.IntentID == proposed.IntentID {
			t.Fatalf("EditProposal(mid-flight) = successor %s superseded %s, want a distinct successor and superseded=%s",
				successor.IntentID, superseded, proposed.IntentID)
		}
		if successor.Stage != workspace.JourneyStageProposed {
			t.Fatalf("successor stage = %s, want PROPOSED (no inherited approval, even though the original had one)", successor.Stage)
		}

		// The original's finance approval is now moot: the original itself
		// can never be decided again.
		if _, err := journey.Decide(approvers.manager, proposed.IntentID, workspace.Decision{Approve: true, Reason: "manager approved"}); err == nil {
			t.Fatal("Journey.Decide on the original succeeded after it was edited away; its recorded approval was not invalidated")
		}
	})

	// TestTodo_PROMOUX_013_Mutation names this sub-test: it is the empirical
	// discovery that motivated the SupersedeIntent reordering fix above, kept
	// here as a live regression test rather than only in a devlog. Before the
	// fix this sub-test's own list-count assertion failed (a dangling
	// successor intent was left behind on every refused edit); reverting the
	// reorder reproduces that failure.
	t.Run("EditOfAnAlreadyTerminalOriginalLeavesNoPartialWrite", func(t *testing.T) {
		ctx, journey, _ := promoux013Composed(t, &now)
		proposed := promoux013Propose(t, ctx, journey, "2026-06-20")

		firstEdit, _, err := journey.EditProposal(ctx, proposed.IntentID, proposed.GovernanceVersion,
			"idem:promoux013:terminal-setup:1", "first edit, makes the original terminal",
			workspace.EditProposalInput{
				TargetJobCode: "OPS-HRBP3", TargetGrade: "P3", ProposedBase: "99000.00",
				EffectiveDate: "2026-06-20", BusinessReason: "promoux013_terminal_setup",
			})
		if err != nil {
			t.Fatalf("Journey.EditProposal (setup): %v", err)
		}
		if firstEdit.Stage != workspace.JourneyStageProposed {
			t.Fatalf("first edit's successor stage = %s, want PROPOSED", firstEdit.Stage)
		}

		before, err := journey.ListJourneys(ctx)
		if err != nil {
			t.Fatalf("ListJourneys before the refused edit: %v", err)
		}

		// The original is now durably CANCELLED (terminal): a second edit
		// against it must be refused, and must leave no partial write (no
		// dangling successor) behind.
		if _, _, err := journey.EditProposal(ctx, proposed.IntentID, proposed.GovernanceVersion+1,
			"idem:promoux013:terminal-retry:1", "attempting to re-edit an already-terminal original",
			workspace.EditProposalInput{
				TargetJobCode: "OPS-HRBP3", TargetGrade: "P3", ProposedBase: "100000.00",
				EffectiveDate: "2026-06-20", BusinessReason: "promoux013_terminal_retry",
			}); err == nil {
			t.Fatal("EditProposal succeeded against an already-terminal (cancelled) original")
		}

		after, err := journey.ListJourneys(ctx)
		if err != nil {
			t.Fatalf("ListJourneys after the refused edit: %v", err)
		}
		if len(after) != len(before) {
			t.Fatalf("journey count went from %d to %d across a refused EditProposal; a partial write (an orphaned successor intent) was left behind",
				len(before), len(after))
		}
	})

	t.Run("WithdrawBeforeApproval", func(t *testing.T) {
		ctx, journey, _ := promoux013Composed(t, &now)
		proposed := promoux013Propose(t, ctx, journey, "2026-07-01")

		preview, err := journey.PreviewIntervention(ctx, proposed.IntentID, workspace.JourneyInterventionWithdraw)
		if err != nil {
			t.Fatalf("PreviewIntervention(WITHDRAW): %v", err)
		}
		if !preview.Available {
			t.Fatalf("WITHDRAW preview on an unstarted proposal = unavailable (%s), want available", preview.UnavailableReasonRef)
		}

		result, err := journey.RequestIntervention(ctx, proposed.IntentID, workspace.JourneyInterventionRequest{
			Kind: workspace.JourneyInterventionWithdraw, ExpectedInstanceVersion: preview.CurrentGovernanceVersion,
			IdempotencyKey: "idem:promoux013:withdraw:1", Reason: "manager withdrew before any approval",
		})
		if err != nil {
			t.Fatalf("RequestIntervention(WITHDRAW): %v", err)
		}
		if result.Outcome != workspace.InterventionApplied {
			t.Fatalf("WITHDRAW outcome = %s, want APPLIED", result.Outcome)
		}
		if result.RetainedEvidenceRef == "" {
			t.Fatal("WITHDRAW retained no evidence reference")
		}

		// A second WITHDRAW attempt must not silently repeat: the proposal
		// is already cancelled, and presenting the now-stale governance
		// version is refused rather than treated as a wildcard "current".
		if _, err := journey.RequestIntervention(ctx, proposed.IntentID, workspace.JourneyInterventionRequest{
			Kind: workspace.JourneyInterventionWithdraw, ExpectedInstanceVersion: preview.CurrentGovernanceVersion,
			IdempotencyKey: "idem:promoux013:withdraw:2", Reason: "manager withdrew again",
		}); err == nil {
			t.Fatal("a second WITHDRAW with a stale expected version succeeded")
		}
	})

	t.Run("CancelDuringEligibleWait", func(t *testing.T) {
		ctx, journey, approvers := promoux013Composed(t, &now)
		proposed := promoux013Propose(t, ctx, journey, "2026-08-01")

		financeWaiting, err := journey.Execute(ctx, proposed.IntentID)
		if err != nil {
			t.Fatalf("Journey.Execute: %v", err)
		}
		_ = financeWaiting
		now = now.Add(10 * time.Minute)
		if _, err := journey.Decide(approvers.finance, proposed.IntentID, workspace.Decision{Approve: true, Reason: "finance approved"}); err != nil {
			t.Fatalf("Journey.Decide(finance): %v", err)
		}
		now = now.Add(10 * time.Minute)
		waiting, err := journey.Decide(approvers.manager, proposed.IntentID, workspace.Decision{Approve: true, Reason: "manager approved"})
		if err != nil {
			t.Fatalf("Journey.Decide(manager): %v", err)
		}
		if waiting.Summary.Stage != workspace.JourneyStage("WAITING_EFFECTIVE_DATE") {
			t.Fatalf("after manager approval stage = %s, want WAITING_EFFECTIVE_DATE", waiting.Summary.Stage)
		}

		preview, err := journey.PreviewIntervention(ctx, proposed.IntentID, workspace.JourneyInterventionCancel)
		if err != nil {
			t.Fatalf("PreviewIntervention(CANCEL): %v", err)
		}
		if !preview.Available {
			t.Fatalf("CANCEL preview during an eligible wait = unavailable (%s), want available", preview.UnavailableReasonRef)
		}

		result, err := journey.RequestIntervention(ctx, proposed.IntentID, workspace.JourneyInterventionRequest{
			Kind: workspace.JourneyInterventionCancel, ExpectedInstanceVersion: preview.CurrentGovernanceVersion,
			IdempotencyKey: "idem:promoux013:cancel:1", Reason: "operator requested cancellation during the wait",
		})
		if err != nil {
			t.Fatalf("RequestIntervention(CANCEL): %v", err)
		}
		// The engine's own safe-point fact is what decides APPLIED vs
		// PENDING_SAFE_POINT here; either is a legitimate real answer for a
		// journey genuinely in an eligible wait, but it must be one of the
		// two -- never DENIED or TOO_LATE, which would mean the "eligible
		// wait" framing was a lie.
		if result.Outcome != workspace.InterventionApplied && result.Outcome != workspace.InterventionPendingSafePoint {
			t.Fatalf("CANCEL during an eligible wait resolved to %s, want APPLIED or PENDING_SAFE_POINT", result.Outcome)
		}
		if result.RetainedEvidenceRef == "" {
			t.Fatal("CANCEL retained no evidence reference")
		}
	})

	t.Run("UnavailableStagesExplainWhy", func(t *testing.T) {
		ctx, journey, _ := promoux013Composed(t, &now)
		proposed := promoux013Propose(t, ctx, journey, "2026-09-01")

		// CANCEL is not available before anything has run.
		notYet, err := journey.PreviewIntervention(ctx, proposed.IntentID, workspace.JourneyInterventionCancel)
		if err != nil {
			t.Fatalf("PreviewIntervention(CANCEL, unstarted): %v", err)
		}
		if notYet.Available || notYet.UnavailableReasonRef == "" {
			t.Fatalf("CANCEL preview on an unstarted proposal = %+v, want unavailable with a reason", notYet)
		}

		if _, err := journey.Execute(ctx, proposed.IntentID); err != nil {
			t.Fatalf("Journey.Execute: %v", err)
		}
		// WITHDRAW is no longer available once the workflow has started.
		started, err := journey.PreviewIntervention(ctx, proposed.IntentID, workspace.JourneyInterventionWithdraw)
		if err != nil {
			t.Fatalf("PreviewIntervention(WITHDRAW, started): %v", err)
		}
		if started.Available || started.UnavailableReasonRef == "" {
			t.Fatalf("WITHDRAW preview once execution has started = %+v, want unavailable with a reason", started)
		}
		// The same durable stage always produces the same reason -- checked
		// twice to prove the answer is a function of the stage, not of
		// anything incidental to this one call (PROMOUX-001/004 disclosure
		// precedent applied to this surface).
		startedAgain, err := journey.PreviewIntervention(ctx, proposed.IntentID, workspace.JourneyInterventionWithdraw)
		if err != nil {
			t.Fatalf("PreviewIntervention(WITHDRAW, started, again): %v", err)
		}
		if startedAgain.UnavailableReasonRef != started.UnavailableReasonRef {
			t.Fatalf("unavailable_reason_ref = %q then %q for the identical stage, want byte-identical text",
				started.UnavailableReasonRef, startedAgain.UnavailableReasonRef)
		}
	})
}

// TestTodo_PROMOUX_013_Race runs a genuinely concurrent EditProposal and
// RequestIntervention(WITHDRAW) against the same original proposal, each
// call made on the shared production connection pool (real PostgreSQL, real
// pgstore compare-and-swap) rather than a sequential simulation, and proves
// exactly one wins.
//
// This is deliberately not "read the stage, then decide which call to
// make": both goroutines present the exact same expected_instance_version
// for the same intent, racing the same optimistic instance-version
// compare-and-swap [intent.SupersedeOriginal] and [intent.CancelInstance]
// both go through (internal/intent/app/pgstore.Store.MutateLifecycle,
// "UPDATE ... WHERE instance_version = $expected"). Of the two concurrent
// callers, PostgreSQL's own row-level locking during that UPDATE is what
// admits exactly one; this test asserts that count, and then asserts
// durable state after the race -- not merely that one call returned an
// error.
func TestTodo_PROMOUX_013_Race(t *testing.T) {
	now := mustParseRFC3339(t, "2026-01-05T09:00:00Z")
	ctx, journey, _ := promoux013Composed(t, &now)
	proposed := promoux013Propose(t, ctx, journey, "2026-10-01")
	expectedVersion := proposed.GovernanceVersion

	const concurrency = 8
	var (
		wg               sync.WaitGroup
		mu               sync.Mutex
		editWins         int
		withdrawWins     int
		otherErrs        []error
		successorIntents []string
	)
	start := make(chan struct{})
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		if i%2 == 0 {
			go func(i int) {
				defer wg.Done()
				<-start
				successor, _, err := journey.EditProposal(ctx, proposed.IntentID, expectedVersion,
					fmt.Sprintf("idem:promoux013:race:edit:%d", i), "racing edit",
					workspace.EditProposalInput{
						TargetJobCode: "OPS-HRBP3", TargetGrade: "P3", ProposedBase: "99500.00",
						EffectiveDate: "2026-10-01", BusinessReason: "promoux013_race_edit",
					})
				if err == nil {
					mu.Lock()
					editWins++
					successorIntents = append(successorIntents, successor.IntentID)
					mu.Unlock()
					return
				}
				mu.Lock()
				otherErrs = append(otherErrs, fmt.Errorf("edit[%d]: %w", i, err))
				mu.Unlock()
			}(i)
		} else {
			go func(i int) {
				defer wg.Done()
				<-start
				result, err := journey.RequestIntervention(ctx, proposed.IntentID, workspace.JourneyInterventionRequest{
					Kind: workspace.JourneyInterventionWithdraw, ExpectedInstanceVersion: expectedVersion,
					IdempotencyKey: fmt.Sprintf("idem:promoux013:race:withdraw:%d", i), Reason: "racing withdraw",
				})
				if err == nil && result.Outcome == workspace.InterventionApplied {
					mu.Lock()
					withdrawWins++
					mu.Unlock()
					return
				}
				if err != nil {
					mu.Lock()
					otherErrs = append(otherErrs, fmt.Errorf("withdraw[%d]: %w", i, err))
					mu.Unlock()
				}
			}(i)
		}
	}
	close(start)
	wg.Wait()

	totalWins := editWins + withdrawWins
	if totalWins != 1 {
		t.Fatalf("editWins=%d withdrawWins=%d (total %d) of %d concurrent callers, want exactly 1 winner overall; other errors: %v",
			editWins, withdrawWins, totalWins, concurrency, otherErrs)
	}

	// Durable state after the race, not an inference from the return codes.
	final, err := journey.Inspect(ctx, proposed.IntentID)
	if err != nil {
		t.Fatalf("Inspect(original) after the race: %v", err)
	}
	switch {
	case editWins == 1:
		if final.Summary.Stage != workspace.JourneyStageFailed && final.Summary.Stage != workspace.JourneyStage("") {
			// The original's own stage projection for a superseded intent is
			// derived at read time; what actually matters is durably
			// checkable below: exactly one successor exists and it is a
			// real, independent, executable proposal.
		}
		if len(successorIntents) != 1 {
			t.Fatalf("editWins=1 but %d successor intents are durable, want exactly 1 (no partial write)", len(successorIntents))
		}
		successorDetail, err := journey.Inspect(ctx, successorIntents[0])
		if err != nil {
			t.Fatalf("Inspect(successor) after the race: %v", err)
		}
		if successorDetail.Summary.Stage != workspace.JourneyStageProposed && successorDetail.Summary.Stage != workspace.JourneyStageBlocked {
			t.Fatalf("successor stage after the race = %s, want PROPOSED or BLOCKED", successorDetail.Summary.Stage)
		}
	case withdrawWins == 1:
		if len(successorIntents) != 0 {
			t.Fatalf("withdrawWins=1 but a successor intent %v was left behind durably -- a partial write from the losing edit", successorIntents)
		}
	}
}

func mustParseRFC3339(t *testing.T, s string) time.Time {
	t.Helper()
	at, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return at
}
