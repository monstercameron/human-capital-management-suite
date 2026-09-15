package approval_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	intentapproval "github.com/monstercameron/human-capital-management-suite/internal/intent/approval"
	stepapproval "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/approval"
)

var itemCID = uuid.MustParse("30000000-0000-0000-0000-000000000003")

func withInvalidators(req humanwork.ApprovalRequirement, kinds ...humanwork.InvalidatorKind) humanwork.ApprovalRequirement {
	for _, kind := range kinds {
		req.Invalidators = append(req.Invalidators, humanwork.Invalidator{Kind: kind, RuleID: "rule:" + string(kind)})
	}
	return req
}

// TestTodo_WF_STEP_018_QuorumRules pins the pure rules the approval kernel
// applies around a vote.
func TestTodo_WF_STEP_018_QuorumRules(t *testing.T) {
	distinct := requirement("approval.board", 2, true)
	a := pendingItem(itemAID, distinct, "principal:one")
	b := pendingItem(itemBID, distinct, "principal:one")
	c := pendingItem(itemCID, distinct, "principal:one")
	set := humanwork.RequirementSet{Requirements: []humanwork.ApprovalRequirement{distinct}}
	cont := continuation(t, set, []workitem.WorkItem{a, b, c})
	dA := decision(distinct, "principal:one", "decision:a", intentapproval.OutcomeApproved)
	doneA := completed(a, dA)

	t.Run("a distinct requirement refuses a second vote by the same principal", func(t *testing.T) {
		prior, dup := stepapproval.DuplicateApprover(cont, []workitem.WorkItem{doneA, b, c}, b, "principal:one")
		if !dup || prior.WorkItemID != itemAID {
			t.Fatalf("DuplicateApprover = %v, %v; want the completed slot A", prior.WorkItemID, dup)
		}
		if _, dup := stepapproval.DuplicateApprover(cont, []workitem.WorkItem{doneA, b, c}, b, "principal:two"); dup {
			t.Fatal("a different principal was reported as a duplicate")
		}
		if _, dup := stepapproval.DuplicateApprover(cont, []workitem.WorkItem{doneA, b, c}, doneA, "principal:one"); dup {
			t.Fatal("a slot was reported as its own duplicate")
		}
	})

	t.Run("a non-distinct requirement has no duplicates", func(t *testing.T) {
		shared := requirement("approval.shared", 1, false)
		x := pendingItem(itemAID, shared, "principal:one")
		y := pendingItem(itemBID, shared, "principal:one")
		sc := continuation(t, humanwork.RequirementSet{Requirements: []humanwork.ApprovalRequirement{shared}}, []workitem.WorkItem{x, y})
		done := completed(x, decision(shared, "principal:one", "decision:x", intentapproval.OutcomeApproved))
		if _, dup := stepapproval.DuplicateApprover(sc, []workitem.WorkItem{done, y}, y, "principal:one"); dup {
			t.Fatal("a non-distinct requirement reported a duplicate")
		}
	})

	t.Run("open slots exclude decided and closed items", func(t *testing.T) {
		closed := c
		closed.Status = workitem.StatusCancelled
		open, err := stepapproval.OpenSlots(cont, []workitem.WorkItem{doneA, b, closed})
		if err != nil {
			t.Fatalf("OpenSlots: %v", err)
		}
		if len(open) != 1 || open[0].WorkItemID != itemBID {
			t.Fatalf("OpenSlots = %+v, want only slot B", open)
		}
		if _, err := stepapproval.OpenSlots(cont, []workitem.WorkItem{doneA, b}); !errors.Is(err, stepapproval.ErrInvalidEvidence) {
			t.Fatalf("OpenSlots with a missing slot = %v, want ErrInvalidEvidence", err)
		}
		stray := b
		stray.NodeID = "another"
		if _, err := stepapproval.Slots(cont, []workitem.WorkItem{doneA, stray, c}); !errors.Is(err, stepapproval.ErrBindingMismatch) {
			t.Fatalf("Slots with a slot bound elsewhere = %v, want ErrBindingMismatch", err)
		}
	})

	t.Run("only a declared invalidator of the pinned requirement fires", func(t *testing.T) {
		declared := withInvalidators(requirement("approval.board", 2, true),
			humanwork.InvalidatorMaterialProposalChange, humanwork.InvalidatorAuthorityRevoked)
		dset := humanwork.RequirementSet{Requirements: []humanwork.ApprovalRequirement{declared}}
		dc := continuation(t, dset, []workitem.WorkItem{pendingItem(itemAID, declared, "p"), pendingItem(itemBID, declared, "p")})
		inv, err := stepapproval.DeclaredInvalidator(dc, dset, humanwork.InvalidatorAuthorityRevoked)
		if err != nil || inv.Kind != humanwork.InvalidatorAuthorityRevoked || inv.RuleID != "rule:AUTHORITY_REVOKED" {
			t.Fatalf("DeclaredInvalidator = %+v, %v", inv, err)
		}
		if _, err := stepapproval.DeclaredInvalidator(dc, dset, humanwork.InvalidatorMandatoryDeny); !errors.Is(err, stepapproval.ErrInvalidatorNotDeclared) {
			t.Fatalf("undeclared invalidator = %v, want ErrInvalidatorNotDeclared", err)
		}
		if _, err := stepapproval.DeclaredInvalidator(dc, dset, "NOT_A_KIND"); !errors.Is(err, stepapproval.ErrInvalidatorNotDeclared) {
			t.Fatalf("unknown invalidator kind = %v, want ErrInvalidatorNotDeclared", err)
		}
		// A caller cannot widen the invalidators by presenting a different
		// compilation of the same requirement id.
		widened := withInvalidators(requirement("approval.board", 2, true), humanwork.InvalidatorMaterialProposalChange,
			humanwork.InvalidatorAuthorityRevoked, humanwork.InvalidatorMandatoryDeny)
		if _, err := stepapproval.DeclaredInvalidator(dc, humanwork.RequirementSet{Requirements: []humanwork.ApprovalRequirement{widened}},
			humanwork.InvalidatorMandatoryDeny); !errors.Is(err, stepapproval.ErrBindingMismatch) {
			t.Fatalf("widened requirement = %v, want ErrBindingMismatch", err)
		}
		if _, err := stepapproval.DeclaredInvalidator(stepapproval.Continuation{}, dset, humanwork.InvalidatorAuthorityRevoked); !errors.Is(err, stepapproval.ErrInvalidContinuation) {
			t.Fatalf("empty continuation = %v, want ErrInvalidContinuation", err)
		}
	})
}

// TestTodo_WF_STEP_018_DecisionFromRecord proves a recorded decision body
// decodes back to the exact decision and a tampered body is refused.
func TestTodo_WF_STEP_018_DecisionFromRecord(t *testing.T) {
	req := requirement("approval.board", 1, false)
	d := decision(req, "principal:one", "decision:1", intentapproval.OutcomeApproved)
	body, err := json.Marshal(map[string]any{
		"decision_id": d.DecisionID,
		"binding": map[string]any{
			"requirement_id": d.Binding.RequirementID, "requirement_revision": d.Binding.RequirementRevision,
			"intent_id": d.Binding.IntentID, "proposal_revision_id": d.Binding.ProposalRevisionID,
			"proposal_digest": d.Binding.ProposalDigest, "task_version": d.Binding.TaskVersion,
			"rendered_projection_digest": d.Binding.RenderedProjectionDigest, "control_snapshots": d.Binding.ControlSnapshots,
			"requirement_digest": d.Binding.RequirementDigest, "resolution_expression_digest": d.Binding.ResolutionExpressionDigest,
		},
		"outcome":                string(d.Outcome),
		"approver":               map[string]any{"principal_id": d.Approver.PrincipalID, "identity_assurance_ref": "", "session_ref": "", "via": string(d.Approver.Via)},
		"authority_decision_ref": d.AuthorityDecisionRef, "reason": d.Reason, "decided_at": d.DecidedAt.String(),
		"vote_digest": d.VoteDigest,
	})
	if err != nil {
		t.Fatal(err)
	}
	rec := workitem.DecisionRecord{WorkItemID: itemAID, Kind: workitem.DecisionKindApproval, Body: body, BodyDigest: d.Digest()}
	got, err := stepapproval.DecisionFromRecord(rec)
	if err != nil {
		t.Fatalf("DecisionFromRecord: %v", err)
	}
	if got.Digest() != d.Digest() || got.DecisionID != d.DecisionID {
		t.Fatalf("decoded decision %s, want %s", got.Digest(), d.Digest())
	}
	for name, mutate := range map[string]func(workitem.DecisionRecord) workitem.DecisionRecord{
		"tampered body": func(r workitem.DecisionRecord) workitem.DecisionRecord {
			r.Body = []byte(strings.Replace(string(r.Body), "APPROVED", "REJECTED", 1))
			return r
		},
		"task record": func(r workitem.DecisionRecord) workitem.DecisionRecord { r.Kind = workitem.DecisionKindTask; return r },
		"not json":    func(r workitem.DecisionRecord) workitem.DecisionRecord { r.Body = []byte("{"); return r },
		"bad instant": func(r workitem.DecisionRecord) workitem.DecisionRecord {
			r.Body = []byte(`{"decided_at":"yesterday"}`)
			return r
		},
		"wrong digest": func(r workitem.DecisionRecord) workitem.DecisionRecord {
			r.BodyDigest = "sha256:" + strings.Repeat("0", 64)
			return r
		},
	} {
		if _, err := stepapproval.DecisionFromRecord(mutate(rec)); !errors.Is(err, stepapproval.ErrInvalidEvidence) {
			t.Errorf("%s: DecisionFromRecord = %v, want ErrInvalidEvidence", name, err)
		}
	}
}
