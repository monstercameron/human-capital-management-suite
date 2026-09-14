package app

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/wire/digest"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	intentapproval "github.com/monstercameron/human-capital-management-suite/internal/intent/approval"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/prototype"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

func TestTodo_PROMOUX_015_Recovery_CompletedApprovalStillAtFrontierNeedsResume(t *testing.T) {
	decision := decidedApproval{
		item: workitem.WorkItem{NodeID: "approve_finance"},
		instance: runtime.Instance{
			RuntimeStatus: runtime.InstanceWaiting, CurrentNodeIDs: []string{"approve_finance"},
		},
		replayed: true,
	}
	if !decision.needsResume() {
		t.Fatal("a committed approval left at its frontier was treated as fully resumed")
	}
	decision.instance.CurrentNodeIDs = []string{"approve_manager"}
	if decision.needsResume() {
		t.Fatal("an earlier approval tried to resume a later review")
	}
	decision.instance.CurrentNodeIDs = []string{"approve_finance"}
	decision.instance.RuntimeStatus = runtime.InstanceCompleted
	if decision.needsResume() {
		t.Fatal("a terminal instance tried to resume a completed approval")
	}
	decision.replayed = false
	if !decision.needsResume() {
		t.Fatal("a fresh approval must resume the driver")
	}
}

// journeyDecideFixtures is the durable state one Decide acts on: the routed,
// started approval WorkItem and the intent it belongs to.
func journeyDecideFixtures() (workitem.WorkItem, intent.Instance, digest.Reference, time.Time) {
	at := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	item := workitem.WorkItem{
		WorkItemID:             uuid.MustParse("99999999-8888-4777-8666-555555555555"),
		ItemVersion:            3,
		Kind:                   workitem.KindApproval,
		NodeID:                 prototype.NodeApproval,
		Status:                 workitem.StatusInProgress,
		ApprovalRequirementRef: prototype.ApprovalRequirementID,
		ProposalRef:            "sha256:proposal",
		AssignmentDigest:       "sha256:assignment",
		Assignment: workitem.Assignment{
			Resolution: humanwork.Resolution{
				RequirementID: prototype.ApprovalRequirementID, RequirementRevision: 1,
				RequirementDigest: "sha256:requirement", ExpressionDigest: "sha256:expression",
				QuorumRequired: 1,
				Candidates: []humanwork.Candidate{
					{PrincipalID: DefaultJourneyApprover, Via: humanwork.SourceDirect},
				},
			},
		},
		CompletedOutputDigest: "sha256:decision",
	}
	inst := intent.Instance{IntentID: "intent:journey-1"}
	proposal := digest.Reference{AlgorithmID: "SHA256", Digest: "sha256:proposal"}
	return item, inst, proposal, at
}

func TestJourneyApprovalRefIsDerivedFromTheIntent(t *testing.T) {
	if got := journeyApprovalRef("intent:1"); got != "approval:journey:intent:1" {
		t.Fatalf("journeyApprovalRef = %q", got)
	}
	if journeyApprovalRef("a") == journeyApprovalRef("b") {
		t.Fatal("two intents must not share an approval reference")
	}
}

func TestApprovalDecisionIsBuiltFromDurableFactsOnly(t *testing.T) {
	item, inst, proposal, at := journeyDecideFixtures()
	engine := newJourneyEngine(nil, nil, "", nil, nil)
	decision := engine.approvalDecision(item, inst, "revision-1", proposal,
		workspace.Decision{Approve: true, Reason: "supported"}, at, engine.approver)

	if decision.Binding.RequirementID != item.ApprovalRequirementRef {
		t.Errorf("binding requirement = %q, want the item's own requirement", decision.Binding.RequirementID)
	}
	res := item.Assignment.Resolution
	if decision.Binding.RequirementRevision != res.RequirementRevision ||
		decision.Binding.RequirementDigest != res.RequirementDigest ||
		decision.Binding.ResolutionExpressionDigest != res.ExpressionDigest {
		t.Errorf("binding does not carry the routed assignment's own requirement identity: %+v", decision.Binding)
	}
	if decision.Binding.ProposalDigest.Digest != item.ProposalRef {
		t.Errorf("binding proposal digest %q must equal the item's proposal_ref %q",
			decision.Binding.ProposalDigest.Digest, item.ProposalRef)
	}
	if decision.Binding.IntentID != inst.IntentID || decision.Binding.ProposalRevisionID != "revision-1" {
		t.Errorf("binding does not name the intent and revision: %+v", decision.Binding)
	}
	if decision.Approver.PrincipalID != DefaultJourneyApprover || decision.Approver.Via != humanwork.SourceDirect {
		t.Errorf("approver = %+v, want the routed candidate", decision.Approver)
	}
	if _, ok := res.Authorizes(decision.Approver.PrincipalID); !ok {
		t.Errorf("the decision names %q, who the routed assignment does not authorize", decision.Approver.PrincipalID)
	}
	if decision.Outcome != intentapproval.OutcomeApproved {
		t.Errorf("outcome = %s, want APPROVED", decision.Outcome)
	}
	if decision.Reason != "supported" {
		t.Errorf("reason = %q, want the approver's own reason", decision.Reason)
	}
	if !decision.DecidedAt.Time().Equal(at) {
		t.Errorf("decided at %v, want %v", decision.DecidedAt.Time(), at)
	}
}

func TestApprovalDecisionRecordsARejection(t *testing.T) {
	item, inst, proposal, at := journeyDecideFixtures()
	engine := newJourneyEngine(nil, nil, "", nil, nil)
	decision := engine.approvalDecision(item, inst, "revision-1", proposal,
		workspace.Decision{Approve: false, Reason: "not now"}, at, engine.approver)
	if decision.Outcome != intentapproval.OutcomeRejected {
		t.Fatalf("outcome = %s, want REJECTED", decision.Outcome)
	}
}

func TestApprovalDecisionDigestsIdenticallyOnEveryRebuild(t *testing.T) {
	item, inst, proposal, at := journeyDecideFixtures()
	engine := newJourneyEngine(nil, nil, "", nil, nil)
	build := func() intentapproval.ApprovalDecision {
		return engine.approvalDecision(item, inst, "revision-1", proposal,
			workspace.Decision{Approve: true, Reason: "supported"}, at, engine.approver)
	}
	first, second := build(), build()
	if first.Digest() == "" {
		t.Fatal("the decision must digest; the work item's completed output is that digest")
	}
	if first.Digest() != second.Digest() {
		t.Fatal("rebuilding the same decision must produce the same digest, or the completion binding breaks")
	}
	// A different instant is a different decision: the instant is part of the
	// digest, which is why Decide uses one reading for both the completion and
	// the resume.
	later := engine.approvalDecision(item, inst, "revision-1", proposal,
		workspace.Decision{Approve: true, Reason: "supported"}, at.Add(time.Second), engine.approver)
	if later.Digest() == first.Digest() {
		t.Fatal("two instants must not digest identically")
	}
}

func TestApprovalDecisionUsesTheConfiguredApprover(t *testing.T) {
	item, inst, proposal, at := journeyDecideFixtures()
	engine := newJourneyEngine(nil, nil, "principal:other-approver", nil, nil)
	decision := engine.approvalDecision(item, inst, "revision-1", proposal, workspace.Decision{Approve: true}, at, engine.approver)
	if decision.Approver.PrincipalID != "principal:other-approver" {
		t.Fatalf("approver = %q, want the configured principal", decision.Approver.PrincipalID)
	}
}

// journeyRoutedFixtures is the durable state one Decide leaves behind before
// it resolves: an approval WorkItem routed exactly the way
// internal/platform/execution's work-item factory routes one (the compiled
// prototype requirement's digests on the assignment, the requirement's
// deadline on the item) and then completed by the routed approver with the
// engine's own decision.
func journeyRoutedFixtures(t *testing.T, approve bool) (
	workitem.WorkItem, intent.ProposalRevision, intentapproval.ApprovalDecision, time.Time,
) {
	t.Helper()
	at := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	requirement, err := prototype.CompileApprovalRequirement(DefaultJourneyApprover, at.Add(48*time.Hour))
	if err != nil {
		t.Fatalf("CompileApprovalRequirement: %v", err)
	}
	intentID, revisionID := "intent:journey-1", "revision-1"
	revision := intent.ProposalRevision{
		IntentID: intentID, ProposalRevisionID: revisionID, Revision: 1,
		MaterialDigest: digest.Reference{
			ProfileID: "PROPOSAL", ProfileVersion: 1, AlgorithmID: "sha256",
			Digest: strings.Repeat("a", 64), ScopeBindingDigest: strings.Repeat("b", 64),
			IntentID: &intentID, ProposalRevisionID: &revisionID,
		},
	}
	item := workitem.WorkItem{
		TenantID:               uuid.MustParse("11111111-2222-4333-8444-555555555555"),
		WorkItemID:             uuid.MustParse("99999999-8888-4777-8666-555555555555"),
		WorkflowInstanceID:     uuid.MustParse("aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"),
		ItemVersion:            3,
		Kind:                   workitem.KindApproval,
		NodeID:                 prototype.NodeApproval,
		Status:                 workitem.StatusInProgress,
		ApprovalRequirementRef: requirement.RequirementID,
		ProposalRef:            revision.MaterialDigest.Digest,
		DeadlineAt:             requirement.Deadline.Expiry.Time(),
		AssignmentDigest:       "sha256:assignment",
		Assignment: workitem.Assignment{
			Resolution: humanwork.Resolution{
				RequirementID: requirement.RequirementID, RequirementRevision: requirement.Revision,
				RequirementDigest: requirement.Digest(), ExpressionDigest: requirement.ExpressionDigest,
				QuorumRequired: requirement.Quorum.MinApprovals,
				Candidates: []humanwork.Candidate{
					{PrincipalID: DefaultJourneyApprover, Via: humanwork.SourceDirect},
				},
			},
		},
	}
	engine := newJourneyEngine(nil, nil, "", nil, nil)
	decision := engine.approvalDecision(item, intent.Instance{IntentID: intentID}, revisionID,
		revision.MaterialDigest, workspace.Decision{Approve: approve, Reason: "reason"}, at, engine.approver)
	// What stepsapproval.Complete would have recorded.
	item.Status = workitem.StatusCompleted
	item.CompletedBy = decision.Approver.PrincipalID
	item.CompletedOutputDigest = decision.Digest()
	completedAt := at
	item.CompletedAt = &completedAt
	return item, revision, decision, at
}

// TestJourneyApprovalOutcomeIsResolvedFromTheRoutedRequirement proves the
// resume's typed outcome comes from internal/workflow/steps/approval.Resolve
// over a continuation rebuilt from durable rows alone - the item's own
// deadline and routed approver - and that the compiled edge keys are what
// Resolve names.
func TestJourneyApprovalOutcomeIsResolvedFromTheRoutedRequirement(t *testing.T) {
	item, revision, decision, at := journeyRoutedFixtures(t, true)
	approved, err := journeyApprovalOutcome(item, DefaultJourneyApprover, revision, decision, at)
	if err != nil {
		t.Fatalf("journeyApprovalOutcome(approve): %v", err)
	}
	if approved.NodeID != prototype.NodeApproval {
		t.Errorf("outcome node = %q, want %q", approved.NodeID, prototype.NodeApproval)
	}
	if approved.Outcome != workflow.Outcome("APPROVED") {
		t.Errorf("outcome = %q, want APPROVED (the compiled edge key)", approved.Outcome)
	}
	if approved.OutputDigest == "" || approved.OutputDigest == item.CompletedOutputDigest {
		t.Errorf("output digest = %q, want the resolution's own digest, not the item's completed output %q",
			approved.OutputDigest, item.CompletedOutputDigest)
	}
	if approved.Await != frontier.AwaitNone || approved.Failed {
		t.Errorf("a resume outcome must be a completed typed outcome: %+v", approved)
	}

	item, revision, decision, at = journeyRoutedFixtures(t, false)
	rejected, err := journeyApprovalOutcome(item, DefaultJourneyApprover, revision, decision, at)
	if err != nil {
		t.Fatalf("journeyApprovalOutcome(reject): %v", err)
	}
	if rejected.Outcome != workflow.OutcomeRejected {
		t.Errorf("outcome = %q, want REJECTED (the compiled edge key)", rejected.Outcome)
	}
}

// TestJourneyApprovalOutcomeRefusesABindingResolveRejects proves the binding
// checks are real: an item routed under another approver, or a decision
// whose digest is not the item's completed output, resolves to an error
// rather than to an outcome the driver would happily route.
func TestJourneyApprovalOutcomeRefusesABindingResolveRejects(t *testing.T) {
	item, revision, decision, at := journeyRoutedFixtures(t, true)
	if _, err := journeyApprovalOutcome(item, "principal:somebody-else", revision, decision, at); err == nil {
		t.Fatal("a continuation rebuilt for another approver must not resolve the routed item")
	}
	tampered := item
	tampered.CompletedOutputDigest = "sha256:" + strings.Repeat("f", 64)
	if _, err := journeyApprovalOutcome(tampered, DefaultJourneyApprover, revision, decision, at); err == nil {
		t.Fatal("a completed output that is not the decision's digest must not resolve")
	}
	stale := revision
	stale.ProposalRevisionID = "revision-2"
	if _, err := journeyApprovalOutcome(item, DefaultJourneyApprover, stale, decision, at); err == nil {
		t.Fatal("a decision bound to another proposal revision must not resolve")
	}
}

func TestJourneyWorkItemErrorSeparatesStageFromFault(t *testing.T) {
	if journeyWorkItemError(nil) != nil {
		t.Fatal("journeyWorkItemError(nil) must stay nil")
	}
	// A refusal the human-work store owns is a stage refusal.
	storeRefusal := workitemRefusalFixture(t)
	got := journeyWorkItemError(storeRefusal)
	if !errors.Is(got, workspace.ErrJourneyStage) {
		t.Fatalf("journeyWorkItemError(store refusal) = %v, want ErrJourneyStage", got)
	}
	// Anything else is this cell failing, not the caller acting out of order.
	plain := errors.New("connection reset")
	other := journeyWorkItemError(plain)
	if errors.Is(other, workspace.ErrJourneyStage) {
		t.Fatalf("journeyWorkItemError(plain) = %v, must not claim a stage refusal", other)
	}
	if !strings.Contains(other.Error(), "connection reset") {
		t.Fatalf("journeyWorkItemError(plain) lost the cause: %v", other)
	}
}

// workitemRefusalFixture produces a real typed refusal from
// internal/humanwork/workitem, rather than a hand-built error value, so the
// projection is tested against the errors the store actually returns.
func workitemRefusalFixture(t *testing.T) error {
	t.Helper()
	_, err := (workitem.Store{}).Claim(t.Context(), nil, workitem.ClaimInput{
		Meta: workitem.TransitionMeta{ActorPrincipalID: "principal:a", Reason: "test", At: time.Now().UTC()},
	})
	if err == nil {
		t.Fatal("a claim with no claimant must be refused")
	}
	if workitem.CodeOf(err) == "" {
		t.Fatalf("expected a typed work-item refusal, got %v", err)
	}
	return err
}
