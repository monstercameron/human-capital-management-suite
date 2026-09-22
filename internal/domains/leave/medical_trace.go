// The canonical Medical Leave logical-persistence trace proves the full
// leave lifecycle as one deterministic, sealed stage sequence (LEAVE-015).
//
// Every stage runs the real pure domain function and binds its seal:
// anchor, snapshot, eligibility, plan, simulation, proposal, restricted
// review, determination, leave-start compile, leave-start commit,
// extension, effect reconciliation, readiness, return commit and the
// final execution receipt. Hypothetical rules and public capabilities
// only: the trace certifies no real jurisdiction.
package leave

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/workreview"
	evidence "github.com/monstercameron/human-capital-management-suite/internal/evidence"
	delivery "github.com/monstercameron/human-capital-management-suite/internal/operations/messagingdelivery"
)

// errTraceFault aborts a fault-injected trace at the named stage.
var errTraceFault = errors.New("leave: injected trace fault")

// The canonical trace stage order mirrors the leave workflow.
var traceStageOrder = []string{
	"anchor", "snapshot", "eligibility", "plan", "simulation", "proposal",
	"review", "determination", "compile-start", "commit-start", "extension",
	"reconcile-start", "readiness", "return", "receipt",
}

// TraceInput carries every fixture the canonical trace binds. The trace
// computes readiness and the determination proposal digest itself: a
// caller-supplied return readiness never enters.
type TraceInput struct {
	Request           RequestLeave
	Context           TrustedContext
	SnapshotEntries   []InputEntry
	Queries           []ProgramQuery
	Facts             map[string]string
	PlanStartDay      int
	PlanEndDay        int
	PlanHoursPerDay   int
	PlanBalance       int
	PlanHolidays      []int
	PlanTimezone      string
	ProposalRevision  uint64
	LoopID            string
	LoopTask          string
	LoopPolicy        string
	Finding           workreview.Finding
	Resume            EvidenceRef
	Determination     DeterminationInput
	Notice            delivery.NoticeRequirement
	NoticeRules       map[string]bool
	NoticeAck         bool
	LeaveStart        LeaveStartInput
	StartCommitKey    string
	StartReceipts     map[string]string
	StartEffects      []ExternalEffect
	Funding           []FundingChange
	RetainedDecisions []string
	ReplanPolicy      string
	Successor         SuccessorIntent
	Readiness         ReturnToWorkInput
	Return            ReturnCommitInput
	ReceiptTenant     string
	ReceiptIntent     string
	ReceiptLineage    string
	ReceiptAuthority  []string
}

// TraceStage is one sealed lifecycle step.
type TraceStage struct {
	Name   string
	Digest string
}

// MedicalTrace is the sealed canonical trace with every bound artifact.
type MedicalTrace struct {
	Stages             []TraceStage
	Anchor             AnchorRecord
	Snapshot           LeaveInputSnapshot
	Resolution         EligibilityResolution
	Plan               LeaveEntitlementPlan
	Simulation         Simulation
	Proposal           ProposalRevision
	Loop               ReviewLoop
	Determination      Determination
	DeterminationInput DeterminationInput
	StartPlanDigest    string
	StartCommitKey     string
	StartCommit        CommitRecord
	StartReceiptsEcho  map[string]string
	Replan             Replan
	Successor          SuccessorIntent
	StartDimensions    EffectDimensions
	Readiness          ReturnToWorkResult
	ReturnInput        ReturnCommitInput
	Return             ReturnRecord
	Receipt            BusinessExecutionReceiptAlias
	Digest             string
}

// BusinessExecutionReceiptAlias keeps the trace in the leave package
// without re-exporting the evidence vocabulary.
type BusinessExecutionReceiptAlias = evidence.BusinessExecutionReceipt

func traceDigest(stages []TraceStage) string {
	parts := []string{"leave-medical-trace"}
	for _, stage := range stages {
		parts = append(parts, stage.Name+"="+stage.Digest)
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// TraceMedicalLeave walks the canonical Medical Leave lifecycle. Any
// stage failure aborts with the stage named and persists nothing beyond
// the in-memory registries the trace owns.
func TraceMedicalLeave(input TraceInput, inject func(string) error) (MedicalTrace, error) {
	var trace MedicalTrace
	seal := func(name, digest string) error {
		if inject != nil {
			if err := inject(name); err != nil {
				return fmt.Errorf("leave: trace aborted at %s: %v", name, err)
			}
		}
		if strings.TrimSpace(digest) == "" {
			return fmt.Errorf("leave: trace stage %s has no seal", name)
		}
		trace.Stages = append(trace.Stages, TraceStage{Name: name, Digest: digest})
		return nil
	}

	process, err := Bind(input.Request, input.Context)
	if err != nil {
		return MedicalTrace{}, fmt.Errorf("leave: trace intake: %v", err)
	}
	anchor, _, err := NewAnchorStore().Invoke(process)
	if err != nil {
		return MedicalTrace{}, fmt.Errorf("leave: trace intake: %v", err)
	}
	trace.Anchor = anchor
	if err := seal("anchor", anchor.Digest); err != nil {
		return MedicalTrace{}, err
	}

	snapshot, err := BuildSnapshot(input.SnapshotEntries)
	if err != nil {
		return MedicalTrace{}, fmt.Errorf("leave: trace snapshot: %v", err)
	}
	trace.Snapshot = snapshot
	if err := seal("snapshot", snapshot.Digest); err != nil {
		return MedicalTrace{}, err
	}

	resolution, err := ResolveEligibility(input.Queries, input.Facts)
	if err != nil {
		return MedicalTrace{}, fmt.Errorf("leave: trace eligibility: %v", err)
	}
	eligible := false
	for _, program := range resolution.Programs {
		if program.Result == ProgramEligible {
			eligible = true
		}
	}
	if !eligible {
		return MedicalTrace{}, fmt.Errorf("leave: trace eligibility: no eligible program explains the intent")
	}
	trace.Resolution = resolution
	if err := seal("eligibility", resolution.Digest); err != nil {
		return MedicalTrace{}, err
	}

	plan, err := ComposePlan(PlanRequest{
		StartDay: input.PlanStartDay, EndDay: input.PlanEndDay,
		HoursPerDay: input.PlanHoursPerDay, BalanceAvailable: input.PlanBalance,
		HolidayDays: input.PlanHolidays, Programs: resolution.Programs,
		Timezone: input.PlanTimezone,
	})
	if err != nil {
		return MedicalTrace{}, fmt.Errorf("leave: trace plan: %v", err)
	}
	trace.Plan = plan
	if err := seal("plan", plan.Digest); err != nil {
		return MedicalTrace{}, err
	}

	simulation, err := Simulate(snapshot, resolution, plan)
	if err != nil {
		return MedicalTrace{}, fmt.Errorf("leave: trace simulation: %v", err)
	}
	trace.Simulation = simulation
	if err := seal("simulation", simulation.Digest); err != nil {
		return MedicalTrace{}, err
	}

	proposal, err := FreezeProposal(simulation, plan, input.ProposalRevision)
	if err != nil {
		return MedicalTrace{}, fmt.Errorf("leave: trace proposal: %v", err)
	}
	trace.Proposal = proposal
	if err := seal("proposal", proposal.Digest); err != nil {
		return MedicalTrace{}, err
	}

	loop, err := OpenLoop(input.LoopID, input.LoopTask, "leave-administrator", input.LoopPolicy)
	if err != nil {
		return MedicalTrace{}, fmt.Errorf("leave: trace review: %v", err)
	}
	loop, err = RequestMoreInfo(loop, input.Finding)
	if err != nil {
		return MedicalTrace{}, fmt.Errorf("leave: trace review: %v", err)
	}
	loop, err = ResumeEvidence(loop, input.Resume)
	if err != nil {
		return MedicalTrace{}, fmt.Errorf("leave: trace review: %v", err)
	}
	trace.Loop = loop
	if err := seal("review", loop.Digest); err != nil {
		return MedicalTrace{}, err
	}

	determinationInput := input.Determination
	determinationInput.ProposalDigest = proposal.Digest
	determinationInput.Programs = resolution.Programs
	rendered, err := RenderDetermination(determinationInput)
	if err != nil {
		return MedicalTrace{}, fmt.Errorf("leave: trace determination: %v", err)
	}
	assessment, err := delivery.AssessRequirement(input.Notice, input.NoticeRules)
	if err != nil {
		return MedicalTrace{}, fmt.Errorf("leave: trace determination: %v", err)
	}
	determination, err := DeliverDetermination(rendered, determinationInput, assessment, false, input.NoticeAck)
	if err != nil {
		return MedicalTrace{}, fmt.Errorf("leave: trace determination: %v", err)
	}
	if !determination.LeaveStartAllowed {
		return MedicalTrace{}, fmt.Errorf("leave: trace determination: blocking notice unsatisfied")
	}
	trace.Determination = determination
	trace.DeterminationInput = determinationInput
	if err := seal("determination", determination.Digest); err != nil {
		return MedicalTrace{}, err
	}

	startPlan, err := CompileLeaveStart(input.LeaveStart)
	if err != nil {
		return MedicalTrace{}, fmt.Errorf("leave: trace compile-start: %v", err)
	}
	trace.StartPlanDigest = startPlan.Digest
	if err := seal("compile-start", startPlan.Digest); err != nil {
		return MedicalTrace{}, err
	}

	startCommit, err := NewCommitter().Commit(StartCommitInput{
		IdempotencyKey: input.StartCommitKey, EmploymentState: input.LeaveStart.EmploymentStatus,
		StepReceipts: input.StartReceipts,
	}, nil)
	if err != nil {
		return MedicalTrace{}, fmt.Errorf("leave: trace commit-start: %v", err)
	}
	trace.StartCommit = startCommit
	trace.StartCommitKey = input.StartCommitKey
	trace.StartReceiptsEcho = input.StartReceipts
	if err := seal("commit-start", startCommit.Digest); err != nil {
		return MedicalTrace{}, err
	}

	replan, err := ReplanLeave(plan, input.Funding, input.RetainedDecisions, input.ReplanPolicy)
	if err != nil {
		return MedicalTrace{}, fmt.Errorf("leave: trace extension: %v", err)
	}
	successor := input.Successor
	successor.LeaveRecordID = startCommit.CommitID
	successor.PriorIntentDigest = anchor.Digest
	successor.PriorProposalDigest = proposal.Digest
	successor.ReplanDigest = replan.NewDigest
	successor.ReplanPrior = plan.Digest
	declared, err := NewSuccessorRegistry().Declare(successor, plan.Digest)
	if err != nil {
		return MedicalTrace{}, fmt.Errorf("leave: trace extension: %v", err)
	}
	trace.Replan = replan
	trace.Successor = declared
	if err := seal("extension", declared.Digest); err != nil {
		return MedicalTrace{}, err
	}

	dimensions, err := ReconcileEffects(input.StartEffects)
	if err != nil {
		return MedicalTrace{}, fmt.Errorf("leave: trace reconcile-start: %v", err)
	}
	for _, target := range dimensions.RepairTargets {
		base := strings.TrimSuffix(target, ":observe")
		if _, err := dimensions.RepairPlan(base); err != nil {
			return MedicalTrace{}, fmt.Errorf("leave: trace reconcile-start: %v", err)
		}
	}
	trace.StartDimensions = dimensions
	if err := seal("reconcile-start", dimensions.Digest); err != nil {
		return MedicalTrace{}, err
	}

	readiness, err := EvaluateReturnToWorkReadiness(input.Readiness)
	if err != nil {
		return MedicalTrace{}, fmt.Errorf("leave: trace readiness: %v", err)
	}
	if readiness.Result != ReadinessReady && readiness.Result != ReadinessReadyWithRestrictions {
		return MedicalTrace{}, fmt.Errorf("leave: trace readiness: %q never returns", readiness.Result)
	}
	trace.Readiness = readiness
	if err := seal("readiness", readiness.Digest); err != nil {
		return MedicalTrace{}, err
	}

	returnInput := input.Return
	returnInput.Readiness = readiness
	returnRecord, err := NewReturnCommitter().Commit(returnInput, nil)
	if err != nil {
		return MedicalTrace{}, fmt.Errorf("leave: trace return: %v", err)
	}
	trace.ReturnInput = returnInput
	trace.Return = returnRecord
	if err := seal("return", returnRecord.Digest); err != nil {
		return MedicalTrace{}, err
	}

	receipt, err := evidence.Assemble(
		input.ReceiptTenant, input.ReceiptIntent, input.ReceiptLineage, input.ReceiptAuthority,
		[]evidence.DimensionInput{
			{Name: "intent", Status: evidence.StatusPresent, Digest: anchor.Digest},
			{Name: "request", Status: evidence.StatusPresent, Digest: process.CanonicalDigest},
			{Name: "snapshot", Status: evidence.StatusPresent, Digest: snapshot.Digest},
			{Name: "simulation", Status: evidence.StatusPresent, Digest: simulation.Digest},
			{Name: "proposal", Status: evidence.StatusPresent, Digest: proposal.Digest},
			{Name: "workflow-version", Status: evidence.StatusPresent, Digest: "leave.leave_and_return/v1"},
			{Name: "approvals", Status: evidence.StatusPresent, Digest: loop.Digest},
			{Name: "transaction-heads", Status: evidence.StatusPresent, Digest: startCommit.Digest},
			{Name: "domain-revisions", Status: evidence.StatusPresent, Digest: returnRecord.Digest},
			{Name: "effects", Status: evidence.StatusPresent, Digest: dimensions.Digest},
			{Name: "observations", Status: evidence.StatusRedacted, Note: "provider observations stay outside the receipt"},
			{Name: "reconciliation", Status: evidence.StatusPresent, Digest: dimensions.Digest},
			{Name: "repair", Status: evidence.StatusPresent, Digest: dimensions.Digest},
			{Name: "obligations", Status: evidence.StatusPresent, Digest: simulation.Digest},
			{Name: "terminal", Status: evidence.StatusPresent, Digest: returnRecord.Digest},
		},
	)
	if err != nil {
		return MedicalTrace{}, fmt.Errorf("leave: trace receipt: %v", err)
	}
	trace.Receipt = receipt
	if err := seal("receipt", receipt.Digest); err != nil {
		return MedicalTrace{}, err
	}

	trace.Digest = traceDigest(trace.Stages)
	return trace, nil
}

// StageNames returns the sealed stage order.
func (trace MedicalTrace) StageNames() []string {
	names := make([]string, 0, len(trace.Stages))
	for _, stage := range trace.Stages {
		names = append(names, stage.Name)
	}
	return names
}

// Verify recomputes the trace seal and every bound stage seal from the
// trace alone.
func (trace MedicalTrace) Verify() error {
	if len(trace.Stages) != len(traceStageOrder) {
		return fmt.Errorf("leave: trace covers %d of %d stages", len(trace.Stages), len(traceStageOrder))
	}
	for i, name := range traceStageOrder {
		if trace.Stages[i].Name != name {
			return fmt.Errorf("leave: trace stage %d is %q, want %q", i, trace.Stages[i].Name, name)
		}
	}
	if trace.Digest == "" || traceDigest(trace.Stages) != trace.Digest {
		return fmt.Errorf("leave: trace seal is broken")
	}
	for _, err := range []error{
		trace.Anchor.Verify(), trace.Snapshot.Verify(), trace.Resolution.Verify(),
		trace.Plan.Verify(), trace.Simulation.Verify(), trace.Proposal.Verify(),
		trace.Loop.Verify(), trace.Determination.Verify(trace.DeterminationInput),
		trace.Replan.Verify(), trace.Successor.Verify(), trace.StartDimensions.Verify(),
		trace.Readiness.Verify(), trace.Receipt.Verify(),
	} {
		if err != nil {
			return err
		}
	}
	if err := trace.StartCommit.Verify(trace.StartCommitKey, trace.StartReceiptsEcho); err != nil {
		return err
	}
	return trace.Return.Verify(trace.ReturnInput)
}
