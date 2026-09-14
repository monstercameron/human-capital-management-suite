package application

import (
	"context"
	"strings"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
)

// align028Decision is one durable human decision and the proposal revision
// digest it must pin.
type align028Decision struct {
	revision                     int64
	decidedBy, outcome           string
	pinnedDigest, revisionDigest string
}

// decisions reads every HUMAN_APPROVAL decision recorded for an intent, joined
// to the immutable proposal revision it references.
func (h *promoux015Harness) align028Decisions(intentID string) []align028Decision {
	h.t.Helper()
	rows, err := h.pool.Query(context.Background(), `
		SELECT d.revision, d.decided_by, d.decision_outcome, d.proposal_digest, r.proposal_digest
		FROM intent_decision d
		JOIN proposal_revision r ON r.tenant_id = d.tenant_id AND r.intent_id = d.intent_id AND r.revision = d.revision
		WHERE d.intent_id = $1::uuid AND d.decision_kind = 'HUMAN_APPROVAL'
		ORDER BY d.decided_at, d.decided_by`, intentID)
	if err != nil {
		h.t.Fatalf("read decisions: %v", err)
	}
	defer rows.Close()
	var out []align028Decision
	for rows.Next() {
		var d align028Decision
		if err := rows.Scan(&d.revision, &d.decidedBy, &d.outcome, &d.pinnedDigest, &d.revisionDigest); err != nil {
			h.t.Fatal(err)
		}
		out = append(out, d)
	}
	return out
}

func (h *promoux015Harness) align028LatestRevision(intentID string) (int64, string) {
	h.t.Helper()
	var rev int64
	var digest string
	if err := h.pool.QueryRow(context.Background(), `SELECT revision, proposal_digest FROM proposal_revision
		WHERE intent_id = $1::uuid ORDER BY revision DESC LIMIT 1`, intentID).Scan(&rev, &digest); err != nil {
		h.t.Fatalf("latest revision of %s: %v", intentID, err)
	}
	return rev, digest
}

// TestTodo_ALIGN_028 proves every human decision the product records pins the
// exact immutable proposal revision it was made against: each durable
// intent_decision row names a proposal revision and carries that revision's
// digest, byte for byte.
func TestTodo_ALIGN_028(t *testing.T) {
	h := promoux015Compose(t)
	id := h.runSeparatedPromotion()
	decisions := h.align028Decisions(id)
	if len(decisions) != 2 {
		t.Fatalf("recorded %d human decisions, want the finance and manager approvals", len(decisions))
	}
	latest, digest := h.align028LatestRevision(id)
	for _, d := range decisions {
		if d.pinnedDigest == "" || d.pinnedDigest != d.revisionDigest {
			t.Errorf("decision by %s pins %q but its revision %d digests to %q", d.decidedBy, d.pinnedDigest, d.revision, d.revisionDigest)
		}
		if d.revision != latest || d.pinnedDigest != digest || d.outcome != "APPROVED" {
			t.Errorf("decision %+v is not bound to the current revision %d/%s", d, latest, digest)
		}
	}
}

// TestTodo_ALIGN_028_Property proves the binding holds for every decision on
// every intent in the cell: no decision row anywhere references a
// digest other than its own revision's.
func TestTodo_ALIGN_028_Property(t *testing.T) {
	h := promoux015Compose(t)
	id := h.runSeparatedPromotion()
	if len(h.align028Decisions(id)) == 0 {
		t.Fatal("the fixture recorded no decisions to check")
	}
	var unbound int
	if err := h.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM intent_decision d
		LEFT JOIN proposal_revision r ON r.tenant_id = d.tenant_id AND r.intent_id = d.intent_id AND r.revision = d.revision
		WHERE r.intent_id IS NULL OR r.proposal_digest <> d.proposal_digest`).Scan(&unbound); err != nil {
		t.Fatal(err)
	}
	if unbound != 0 {
		t.Fatalf("%d decisions are not bound to their revision's digest", unbound)
	}
}

// TestTodo_ALIGN_028_Golden pins the decision binding columns the schema
// guarantees: a decision cannot be recorded without a proposal digest and a
// revision foreign key.
func TestTodo_ALIGN_028_Golden(t *testing.T) {
	h := promoux015Compose(t)
	var nullable string
	if err := h.pool.QueryRow(context.Background(), `SELECT string_agg(column_name || '=' || is_nullable, ',' ORDER BY column_name)
		FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'intent_decision'
		AND column_name IN ('proposal_digest', 'revision', 'intent_id', 'decided_by', 'control_digest')`).Scan(&nullable); err != nil {
		t.Fatal(err)
	}
	if want := "control_digest=NO,decided_by=NO,intent_id=NO,proposal_digest=NO,revision=NO"; nullable != want {
		t.Fatalf("decision binding columns = %s, want %s", nullable, want)
	}
	var fk int
	if err := h.pool.QueryRow(context.Background(), `SELECT count(*) FROM information_schema.table_constraints
		WHERE table_schema = current_schema() AND table_name = 'intent_decision' AND constraint_name = 'intent_decision_revision'
		AND constraint_type = 'FOREIGN KEY'`).Scan(&fk); err != nil || fk != 1 {
		t.Fatalf("intent_decision_revision foreign key present = %d, %v", fk, err)
	}
}

// TestTodo_ALIGN_028_Security proves a decision cannot be rewritten to a
// different digest after the fact and that a decision cannot be recorded by
// the requester: the append-only trigger refuses the rewrite, and the
// requester's own approval attempt records nothing.
func TestTodo_ALIGN_028_Security(t *testing.T) {
	h := promoux015Compose(t)
	id := h.proposeAndExecute()
	if _, err := h.client.DecideJourney(h.rpc("hiring-manager"), &journeyv1.DecideJourneyRequest{IntentId: id, Approve: true, Reason: "self approval"}); err == nil {
		t.Error("the requester approved their own proposal")
	}
	if n := len(h.align028Decisions(id)); n != 0 {
		t.Fatalf("a refused decision recorded %d rows", n)
	}
	if _, err := h.client.DecideJourney(h.rpc("finance-partner"), &journeyv1.DecideJourneyRequest{IntentId: id, Approve: true, Reason: "finance approves"}); err != nil {
		t.Fatalf("finance decision: %v", err)
	}
	_, err := h.pool.Exec(context.Background(), `UPDATE intent_decision SET proposal_digest = repeat('0', 64) WHERE intent_id = $1::uuid`, id)
	if err == nil {
		t.Fatal("a recorded decision's pinned digest was rewritten")
	}
	for _, d := range h.align028Decisions(id) {
		if d.pinnedDigest != d.revisionDigest {
			t.Fatalf("decision digest changed to %s", d.pinnedDigest)
		}
	}
}

// TestTodo_ALIGN_028_Integration proves editing a partly approved proposal
// never carries an approval across digests: the edit mints a successor with a
// new revision digest and no decisions, the original's decision stays pinned
// to the original digest, and the original can no longer be decided.
func TestTodo_ALIGN_028_Integration(t *testing.T) {
	h := promoux015Compose(t)
	id := h.proposeAndExecute()
	if _, err := h.client.DecideJourney(h.rpc("finance-partner"), &journeyv1.DecideJourneyRequest{IntentId: id, Approve: true, Reason: "finance approves"}); err != nil {
		t.Fatalf("finance decision: %v", err)
	}
	_, originalDigest := h.align028LatestRevision(id)
	journey := h.journeyFor("hiring-manager", id)
	if journey == nil {
		t.Fatal("the proposer cannot see the journey to edit it")
	}
	edited, err := h.client.EditProposal(h.rpc("hiring-manager"), &journeyv1.EditProposalRequest{
		IntentId: id, ExpectedInstanceVersion: journey.GetGovernanceVersion(), IdempotencyKey: "idem:align028:edit",
		Reason: "corrects the proposed base", Target: &journeyv1.Placement{JobCode: "OPS-HRBP3", Grade: "P3"},
		ProposedBase: "99500.00", EffectiveDate: h.effective, BusinessReason: "align028 edited",
	})
	if err != nil {
		t.Fatalf("EditProposal: %v", err)
	}
	successor := edited.GetJourney().GetIntentId()
	if successor == "" || successor == id || edited.GetSupersededIntentId() != id {
		t.Fatalf("edit = %v", edited)
	}
	_, successorDigest := h.align028LatestRevision(successor)
	if successorDigest == originalDigest {
		t.Fatal("the edited proposal kept the original digest")
	}
	if n := len(h.align028Decisions(successor)); n != 0 {
		t.Fatalf("the successor inherited %d decisions", n)
	}
	for _, d := range h.align028Decisions(id) {
		if d.pinnedDigest != originalDigest {
			t.Fatalf("the original decision moved to %s", d.pinnedDigest)
		}
	}
	if _, err := h.client.DecideJourney(h.rpc("admin"), &journeyv1.DecideJourneyRequest{IntentId: id, Approve: true, Reason: "late approval"}); err == nil {
		t.Fatal("the edited-away original accepted another decision")
	}
}

// TestTodo_ALIGN_028_Fault proves a decision against a missing or foreign
// intent records nothing and a decision row cannot reference a revision that
// does not exist.
func TestTodo_ALIGN_028_Fault(t *testing.T) {
	h := promoux015Compose(t)
	if _, err := h.client.DecideJourney(h.rpc("admin"), &journeyv1.DecideJourneyRequest{IntentId: "00000000-0000-7000-8000-000000000001", Approve: true, Reason: "x"}); err == nil {
		t.Error("deciding an unknown intent succeeded")
	}
	id := h.proposeAndExecute()
	tenant := pgstore.TenantID(demoworkforce.CompanyKey)
	_, err := h.pool.Exec(context.Background(), `INSERT INTO intent_decision
		(tenant_id, decision_id, intent_id, revision, requirement_id, decision_kind, decision_outcome,
		 proposal_digest, control_digest, materiality_class, decided_by, authority_ref, decision_reason, decided_at)
		VALUES ($1, gen_random_uuid(), $2::uuid, 999, 'req.x', 'HUMAN_APPROVAL', 'APPROVED', repeat('a', 64), repeat('b', 64),
		 'MATERIAL', 'principal:x', 'authority:x', 'orphan', now())`, tenant, id)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "foreign key") {
		t.Fatalf("a decision for a nonexistent revision = %v, want a foreign key refusal", err)
	}
}

// TestTodo_ALIGN_028_Conformance proves the decision binding is enforced by
// the database itself, independent of any handler: the decision table is
// append-only by trigger.
func TestTodo_ALIGN_028_Conformance(t *testing.T) {
	h := promoux015Compose(t)
	var triggers int
	if err := h.pool.QueryRow(context.Background(), `SELECT count(*) FROM information_schema.triggers
		WHERE event_object_schema = current_schema() AND event_object_table = 'intent_decision'
		AND trigger_name = 'intent_decision_append_only' AND event_manipulation IN ('UPDATE', 'DELETE')`).Scan(&triggers); err != nil {
		t.Fatal(err)
	}
	if triggers != 2 {
		t.Fatalf("intent_decision append-only trigger covers %d of UPDATE/DELETE", triggers)
	}
}
