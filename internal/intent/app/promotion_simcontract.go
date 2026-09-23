package app

import (
	"fmt"
	"slices"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/simassign"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/simcontract"
	"github.com/monstercameron/human-capital-management-suite/internal/governance/decision"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/conflict"
)

// promotionContractInput is everything the SIMULATE branch needs to bind the
// resolve-time governed simulations to the WorkflowSimulationContract
// PROMO-004 assembles, beyond what the capability call already carries.
type promotionContractInput struct {
	// IntentID identifies the simulated intent on the contract.
	IntentID string
	// Simulations are the resolve-time simassign/simcomp results bound to
	// one candidate digest and the governed snapshot digest.
	Simulations *PromotionSimulations
	// Preflight is the preflight the SIMULATE branch just computed over the
	// same request: its findings decide the contract's completion section.
	Preflight promotion.PreflightResult
	// ControlSnapshotDigest is the control snapshot the simulation ran
	// under; the contract's revalidation section cites it.
	ControlSnapshotDigest string
	// RevalidationRule is the intent definition's revalidation rule the
	// contract's revalidation section reruns at execution time.
	RevalidationRule string
}

// assemblePromotionContract binds one position-bound simulation to a
// validated [simcontract.SimulationResult]. It is the served path's first
// production caller of [simcontract.Assemble]: the resolve already ran
// simassign/simcomp over the governed snapshot, and this function produces
// the simulation contract [simcontract.SimulationResult.Validate] requires
// from exactly those results plus the served preflight's findings.
//
// Sections the zero-effect simulation genuinely declares nothing for --
// planned reads and writes, stream participants, conflict candidates, source
// authorities, legal obligations, side effects and their repair bindings --
// are stated as empty, never omitted: the simulation plans no future reads
// or writes (promotion.SimulationResult's own contract: P1A produces no
// write plan at all), touches no stream, and binds no authority because it
// executes nothing. What the contract does bind is the exact intent, the
// governed snapshot digest both simulations cite, the candidate digest, the
// deduplicated served findings, the evaluated cost delta and the completion
// verdict, digested once by Assemble.
//
// Anything the contract cannot state honestly fails closed: simulations the
// resolve never ran, a candidate or snapshot digest that is missing or
// disagreed between the two simulations, an unevaluated cost, or a missing
// revalidation rule or control digest refuses the simulation rather than
// minting a digest over a guess.
func assemblePromotionContract(in promotionContractInput) (simcontract.SimulationResult, error) {
	sims := in.Simulations
	if sims == nil {
		return simcontract.SimulationResult{}, fmt.Errorf("app: promotion contract needs governed simulations")
	}
	if strings.TrimSpace(in.IntentID) == "" {
		return simcontract.SimulationResult{}, fmt.Errorf("app: promotion contract needs an intent identity")
	}
	candidate := strings.TrimSpace(sims.CandidateDigest)
	if candidate == "" {
		return simcontract.SimulationResult{}, fmt.Errorf("app: promotion contract needs a candidate digest")
	}
	snapshotDigest := strings.TrimSpace(sims.Assignment.SnapshotDigest)
	if snapshotDigest == "" {
		return simcontract.SimulationResult{}, fmt.Errorf("app: promotion contract needs a snapshot digest")
	}
	if compDigest := strings.TrimSpace(sims.Compensation.SnapshotDigest); compDigest != snapshotDigest {
		return simcontract.SimulationResult{}, fmt.Errorf("app: promotion simulations disagree about their snapshot")
	}
	rule := strings.TrimSpace(in.RevalidationRule)
	if rule == "" {
		return simcontract.SimulationResult{}, fmt.Errorf("app: promotion contract needs a revalidation rule")
	}
	controls := strings.TrimSpace(in.ControlSnapshotDigest)
	if controls == "" {
		return simcontract.SimulationResult{}, fmt.Errorf("app: promotion contract needs a control-snapshot digest")
	}
	if !sims.Compensation.BasePay.Evaluated {
		return simcontract.SimulationResult{}, fmt.Errorf("app: promotion contract needs an evaluated cost")
	}

	refusals := make([]simassign.Refusal, 0, len(sims.Assignment.Refusals)+len(sims.Compensation.Refusals))
	refusals = append(refusals, sims.Assignment.Refusals...)
	refusals = append(refusals, sims.Compensation.Refusals...)

	deduped := slices.Clone(sims.Compensation.RequiredApprovals)
	slices.Sort(deduped)
	approvals := slices.Compact(deduped)
	if approvals == nil {
		approvals = []string{}
	}
	requirements := make([]decision.ApprovalRequirement, 0, len(approvals))
	for _, id := range approvals {
		if strings.TrimSpace(id) == "" {
			return simcontract.SimulationResult{}, fmt.Errorf("app: promotion contract names an empty approval")
		}
		requirements = append(requirements, decision.ApprovalRequirement{
			ID: id, Version: "v1", Satisfaction: decision.ApprovalPending,
		})
	}

	amount := sims.Compensation.BasePay.Delta
	completion := promotionContractCompletion(in.Preflight.Status, approvals)

	return simcontract.Assemble(simcontract.AssembleInput{
		Intent: simcontract.IntentRef{
			IntentID:      in.IntentID,
			IntentType:    promotion.IntentType,
			IntentVersion: promotion.IntentVersion,
		},
		Snapshot: simcontract.SnapshotRef{
			SnapshotDigest: snapshotDigest,
			Tenant:         sims.Assignment.Tenant,
			Subject:        sims.Assignment.Subject,
		},
		ProposalCandidateDigest: candidate,
		Reads:                   []intent.PlannedRead{},
		Writes:                  []intent.PlannedWrite{},
		Streams:                 []intent.PlanParticipant{},
		Conflicts:               []conflict.Candidate{},
		Approvals:               requirements,
		Authority:               []evidence.SourceAuthority{},
		LegalObligations:        []decision.Obligation{},
		SideEffects:             []simcontract.SideEffect{},
		Repair:                  []intent.CompensationBinding{},
		Cost:                    simcontract.Cost{State: simcontract.CostEvaluated, Amount: &amount},
		Completion:              completion,
		Revalidation:            simcontract.Revalidation{Rules: []string{rule}, ControlSnapshotDigest: controls},
		Findings:                in.Preflight.Findings,
		Refusals:                refusals,
	})
}

// promotionContractCompletion derives the contract's completion section from
// the served preflight status and the compensation simulation's required
// approvals. A blocked candidate carries no outstanding approvals however
// many the simulations named: no approval could clear a blocking finding,
// so naming one would promise a path to completion that does not exist.
func promotionContractCompletion(status promotion.Status, approvals []string) simcontract.Completion {
	if status == promotion.StatusReady && len(approvals) == 0 {
		return simcontract.Completion{
			State:  simcontract.CompletionReady,
			Detail: "no finding blocks the candidate and no approval is outstanding",
		}
	}
	if status == promotion.StatusReady {
		return simcontract.Completion{
			State:                simcontract.CompletionPendingApproval,
			OutstandingApprovals: approvals,
			Detail:               "the candidate is unblocked and waits for its outstanding approvals",
		}
	}
	return simcontract.Completion{
		State:  simcontract.CompletionBlocked,
		Detail: "a blocking finding or refusal governs the candidate and no approval could clear it",
	}
}
