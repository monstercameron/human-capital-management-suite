package bootstrap_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/intentcontrol"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
)

// WF-RUN-027 end to end on the composed journey cell: the workflow runtime
// admits a start from decisions recorded in intent_decision and supersession
// recorded in intent_relationship, never from the approval flags the caller
// presented.
//
// The runtime's own refusal vocabulary is proved against in-memory fact
// adapters in internal/workflow/runtime (TestTodo_WF_RUN_027*), and the
// durable adapter against embedded PostgreSQL in internal/intent/app. What
// these tests add is the composition: the journey's Execute records the
// admission, its Decide records the approver's decision, and the same rows are
// what runtime.Start read to let the run happen at all.

// wfrun027Decisions reads every intent_decision row one intent carries, as the
// migration/admin role, so an assertion about what the journey recorded is
// made from outside the transactions that recorded it.
func wfrun027Decisions(t *testing.T, c *cell, intentID string) []intentcontrol.Decision {
	t.Helper()
	rows, err := c.pool.Query(context.Background(), `
		SELECT decision_id, revision, requirement_id, decision_kind, decision_outcome,
			proposal_digest, decided_by, authority_ref
		FROM intent_decision
		WHERE tenant_id = $1 AND intent_id = $2
		ORDER BY decision_kind`, pgstore.TenantID(testTenant), uuid.MustParse(intentID))
	if err != nil {
		t.Fatalf("read intent_decision: %v", err)
	}
	defer rows.Close()

	var out []intentcontrol.Decision
	for rows.Next() {
		var d intentcontrol.Decision
		if err := rows.Scan(&d.DecisionID, &d.Revision, &d.RequirementID, &d.Kind, &d.Outcome,
			&d.ProposalDigest, &d.DecidedBy, &d.AuthorityRef); err != nil {
			t.Fatalf("scan intent_decision: %v", err)
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read intent_decision: %v", err)
	}
	return out
}

// wfrun027MaterialDigest is the material proposal digest one journey's
// re-simulation mints, which is the digest every decision recorded against it
// must be bound to.
func wfrun027MaterialDigest(t *testing.T, h *journeyHarness, ctx context.Context, intentID string) string {
	t.Helper()
	simulated, err := h.cell.app.Service.SimulateIntent(ctx, &intentsv1.SimulateIntentRequest{IntentId: intentID})
	if err != nil {
		t.Fatalf("SimulateIntent: %v", err)
	}
	return simulated.GetSimulation().GetMaterialProposalDigest().GetDigest()
}

// TestTodo_WF_RUN_027_Bootstrap is the durable end-to-end proof: the browser
// journey's Execute then Decide still completes, and it does so because each
// step wrote the intent_decision row the runtime's durable ProposalFacts
// adapter read back.
func TestTodo_WF_RUN_027_Bootstrap(t *testing.T) {
	h := newJourneyHarness(t)
	ctx := h.operatorCtx(t)

	proposed, err := h.engine.Propose(ctx, journeyProposal())
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	material := wfrun027MaterialDigest(t, h, ctx, proposed.IntentID)

	// Nothing is recorded before the operator acts, so a start attempted now
	// would have no decision at all to admit it.
	if before := wfrun027Decisions(t, h.cell, proposed.IntentID); len(before) != 0 {
		t.Fatalf("intent_decision rows before Execute = %d, want 0", len(before))
	}

	if _, err := h.engine.Execute(ctx, proposed.IntentID); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// The start happened, which means runtime.Start found an APPROVED decision
	// bound to this exact revision's material digest -- it had no caller
	// assertion to fall back on.
	admitted := wfrun027Decisions(t, h.cell, proposed.IntentID)
	if len(admitted) != 1 {
		t.Fatalf("intent_decision rows after Execute = %d, want exactly the AUTHZ admission", len(admitted))
	}
	if admitted[0].Kind != intentcontrol.DecisionAuthZ || admitted[0].Outcome != intentcontrol.OutcomeApproved {
		t.Errorf("admission = %s/%s, want AUTHZ/APPROVED", admitted[0].Kind, admitted[0].Outcome)
	}
	if admitted[0].ProposalDigest != material {
		t.Errorf("admission is bound to %q, want the revision's own material digest %q",
			admitted[0].ProposalDigest, material)
	}
	if admitted[0].Revision != 1 {
		t.Errorf("admission names revision %d, want 1", admitted[0].Revision)
	}
	if admitted[0].DecidedBy != testSubject {
		t.Errorf("admission decided_by = %q, want the operator %q", admitted[0].DecidedBy, testSubject)
	}

	detail, err := h.engine.Decide(h.approverCtx(t), proposed.IntentID, workspace.Decision{
		Approve: true, Reason: "reason.promotion_supported/v1",
	})
	if err != nil {
		t.Fatalf("Decide(approve): %v", err)
	}
	if detail.Summary.Stage != workspace.JourneyStageCompleted {
		t.Fatalf("stage = %s, want COMPLETED", detail.Summary.Stage)
	}

	// Decide now records the approver's own decision in intent_decision too,
	// bound to the same revision and digest, as the routed approver rather
	// than as whoever pressed the button.
	decided := wfrun027Decisions(t, h.cell, proposed.IntentID)
	if len(decided) != 2 {
		t.Fatalf("intent_decision rows after Decide = %d, want the AUTHZ admission and the human approval", len(decided))
	}
	var human *intentcontrol.Decision
	for i := range decided {
		if decided[i].Kind == intentcontrol.DecisionHumanApproval {
			human = &decided[i]
		}
	}
	if human == nil {
		t.Fatal("Decide recorded no HUMAN_APPROVAL decision for the durable adapter to read")
	}
	if human.Outcome != intentcontrol.OutcomeApproved {
		t.Errorf("human decision outcome = %s, want APPROVED", human.Outcome)
	}
	if human.ProposalDigest != material {
		t.Errorf("human decision is bound to %q, want %q", human.ProposalDigest, material)
	}
	if human.DecidedBy != journeyApprover {
		t.Errorf("human decision decided_by = %q, want the routed approver %q", human.DecidedBy, journeyApprover)
	}
	if human.RequirementID == "" {
		t.Error("a human approval decision must name the requirement it satisfies")
	}
}

// TestTodo_WF_RUN_027_Bootstrap_Fault proves the fallback is gone: an
// ExecuteIntent presenting Approved=true with an approval reference nobody
// recorded is refused, because the runtime resolves approval from stored facts
// on a cell composed with the execution database.
func TestTodo_WF_RUN_027_Bootstrap_Fault(t *testing.T) {
	h := newJourneyHarness(t)
	ctx := h.operatorCtx(t)

	proposed, err := h.engine.Propose(ctx, journeyProposal())
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	simulated, simErr := h.cell.app.Service.SimulateIntent(ctx, &intentsv1.SimulateIntentRequest{
		IntentId: proposed.IntentID,
	})
	if simErr != nil {
		t.Fatalf("SimulateIntent: %v", simErr)
	}
	artifact := simulated.GetSimulation()

	// Exactly what the journey presents, minus the recorded decision: the
	// right revision, the right digest, Approved=true and an approval
	// reference this caller invented.
	_, execErr := h.cell.app.Service.ExecuteIntent(ctx, &intentsv1.ExecuteIntentRequest{
		IdempotencyKey: "wfrun027:fabricated:" + proposed.IntentID,
		IntentId:       proposed.IntentID,
		Approval: &intentsv1.ProposalApproval{
			ProposalRevisionId:     artifact.GetProposalRevisionId(),
			MaterialProposalDigest: artifact.GetMaterialProposalDigest(),
			Approved:               true,
			ApprovalRef:            "approval:invented-by-the-caller",
		},
	})
	if execErr == nil {
		t.Fatal("an Execute presenting Approved=true with no recorded decision must be refused")
	}
	owned, ok := execErr.(*envelope.Error)
	if !ok {
		t.Fatalf("ExecuteIntent error = %T (%v), want the owned envelope", execErr, execErr)
	}
	if owned.ReasonRef() != "p1b.unapproved_proposal" {
		t.Fatalf("refusal reason = %q, want p1b.unapproved_proposal", owned.ReasonRef())
	}
	if n := queryOne[int](t, h.cell,
		`SELECT count(*) FROM workflow_instance WHERE tenant_id = $1`, pgstore.TenantID(testTenant)); n != 0 {
		t.Fatalf("workflow instances after the refusal = %d, want 0", n)
	}
}

// TestTodo_WF_RUN_027_Bootstrap_Security proves the second half of the same
// rule: a revision whose intent the relationship graph has superseded is
// refused even though the caller asserts it is current. Approval resolution
// cannot override that durable relationship fact.
func TestTodo_WF_RUN_027_Bootstrap_Security(t *testing.T) {
	h := newJourneyHarness(t)
	ctx := h.operatorCtx(t)

	first, err := h.engine.Propose(ctx, journeyProposal())
	if err != nil {
		t.Fatalf("Propose(first): %v", err)
	}
	// PROMOUX-002: two proposals for the same worker on the same effective
	// date are now admitted at most once (an active-intent guard refuses the
	// second as a conflict). This test's own point is supersession, not
	// concurrent-start admission, so its second proposal targets a
	// genuinely different, non-overlapping effective date -- exactly the
	// case PROMOUX-002's GREEN clause carves out as not a conflict -- rather
	// than relying on the two-distinct-intents-for-one-window behavior this
	// todo closes.
	secondInput := journeyProposal()
	secondInput.EffectiveDate = "2026-07-01"
	second, err := h.engine.Propose(ctx, secondInput)
	if err != nil {
		t.Fatalf("Propose(second): %v", err)
	}
	if first.IntentID == second.IntentID {
		t.Fatal("the two proposals must be distinct intents for this test to mean anything")
	}
	material := wfrun027MaterialDigest(t, h, ctx, first.IntentID)
	tenantID := pgstore.TenantID(testTenant)

	// Supersede the first intent through the durable relationship authority.
	// Do not manufacture a proposal_revision payload here: ExecuteIntent owns
	// encoding and verifying that full proposal, and placeholder JSON is not a
	// valid substitute for its signed material.
	wfrun027Tx(t, h.cell, tenantID, func(tx dbport.Tx) error {
		return (intentcontrol.RelationshipStore{}).Link(context.Background(), tx, intentcontrol.Relationship{
			TenantID: tenantID, RelationshipID: uuid.New(),
			Type:                intentcontrol.RelationSupersedes,
			Parent:              uuid.MustParse(second.IntentID),
			Child:               uuid.MustParse(first.IntentID),
			Ordinal:             1,
			MaterialInputDigest: material,
			EstablishedAt:       baseTime,
		})
	})

	if _, execErr := h.engine.Execute(ctx, first.IntentID); execErr == nil {
		t.Fatal("a superseded revision must not start, however current the caller says it is")
	} else if !strings.Contains(execErr.Error(), "p1b.superseded_proposal") {
		t.Fatalf("Execute(superseded) = %v, want the superseded-proposal refusal", execErr)
	}
	if n := queryOne[int](t, h.cell,
		`SELECT count(*) FROM workflow_instance WHERE tenant_id = $1`, tenantID); n != 0 {
		t.Fatalf("workflow instances after the refusal = %d, want 0", n)
	}

	// The intent nothing superseded still starts, on its own recorded
	// admission: the refusal above was about supersession, not about the
	// journey no longer working.
	if _, execErr := h.engine.Execute(ctx, second.IntentID); execErr != nil {
		t.Fatalf("Execute(current) = %v, want the current revision to start", execErr)
	}
}

// wfrun027Tx runs fn inside one tenant-scoped transaction on the cell's own
// pool and commits it, which is how a test writes the durable facts the
// runtime is about to read.
func wfrun027Tx(t *testing.T, c *cell, tenantID uuid.UUID, fn func(tx dbport.Tx) error) {
	t.Helper()
	ctx := context.Background()
	tx, err := c.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		t.Fatalf("scope tenant: %v", err)
	}
	if err := fn(tx); err != nil {
		t.Fatalf("transaction: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
}
