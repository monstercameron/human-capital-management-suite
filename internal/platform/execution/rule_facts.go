package execution

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/rulethreshold"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/rules"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// composeCurrencyGuard honors a unit composition's guard override and
// defaults every other composition to the served RULE-004 guard. The
// override exists for harnesses that exercise non-currency concerns with
// synthetic intents the durable tables cannot key on; traffic-serving
// compositions leave PromotionExecutionConfig.Currency nil.
func composeCurrencyGuard(cfg PromotionExecutionConfig, steps *promotionStepPorts) *execute.CurrencyGuard {
	if cfg.Currency != nil {
		return cfg.Currency
	}
	return servedCurrencyGuard(steps)
}

// servedCurrencyGuard is the served driver's currency check (REV-010-01):
// every served advance revalidates the pinned proposal's supersession and
// standing approval, and re-runs the decision-table approval that produced
// its tier against the currently published threshold table (RULE-004). The
// frozen half comes from the threshold decisions the raise_threshold branch
// freezes in the advancement transaction; the live half re-derives through
// the same governed reads the threshold port uses. Revisions that never ran
// the threshold node, or that carry no standing approval, resolve RULE-004
// to silence and keep the WF-RUN-029 verdict exactly.
func servedCurrencyGuard(steps *promotionStepPorts) *execute.CurrencyGuard {
	return &execute.CurrencyGuard{
		Proposal: app.DurableProposalFacts{},
		Approval: app.DurableProposalFacts{},
		Rules:    &ServedRuleFacts{Thresholds: steps, Approval: app.DurableProposalFacts{}},
	}
}

// ServedRuleFacts resolves the RULE-004 re-evaluation facts for one served
// proposal revision (REV-010-01). The frozen half comes from the threshold
// decision the raise_threshold branch froze in the advancement transaction;
// the live half re-derives the threshold inputs through the same governed
// reads the threshold port uses; the standing approval digest comes from
// the recorded approval decisions. A revision that never ran the threshold
// node, or that carries no standing approval, resolves to the zero value:
// the guard's own approval checks then refuse with their typed errors, and
// RULE-004 simply has nothing to re-evaluate.
type ServedRuleFacts struct {
	// Thresholds derives the live threshold inputs. Nil is a composition
	// mistake, reported loudly rather than read as unchanged inputs.
	Thresholds ruleInputDeriver
	// Approval reads the standing approval decisions. Nil is a composition
	// mistake, reported loudly rather than read as no approval.
	Approval runtime.ApprovalFacts
}

// ruleInputDeriver is the one governed read the served facts need.
// *promotionStepPorts is the production implementation; tests substitute a
// func-backed fake without standing up step services.
type ruleInputDeriver interface {
	thresholdInputs(ctx context.Context, req execute.StepRequest) (rules.PromotionApprovalInput, error)
}

// currentThresholdInputs re-derives RULE-003's inputs for the recorded
// instance through the governed threshold-inputs path, so the derivation
// the guard compares against is the same one the threshold port evaluated.
func (s *ServedRuleFacts) currentThresholdInputs(ctx context.Context, record rulethreshold.Decision, tenantID uuid.UUID, rev intent.ProposalRevision) (rules.PromotionApprovalInput, error) {
	req := execute.StepRequest{
		TenantID:   tenantID,
		InstanceID: record.InstanceID,
		Attempt:    record.Attempt,
		Node:       workflow.CompiledNode{ID: promotionexec.NodeRaiseThreshold, Type: workflow.StepDecision},
		Proposal:   runtime.ProposalBinding{Revision: rev},
	}
	return s.Thresholds.thresholdInputs(ctx, req)
}

// Lookup implements [execute.RuleFacts].
func (s *ServedRuleFacts) Lookup(ctx context.Context, ex runtime.Executor, tenantID uuid.UUID, rev intent.ProposalRevision) (execute.RuleApproval, error) {
	if s.Thresholds == nil || s.Approval == nil {
		return execute.RuleApproval{}, fmt.Errorf("platform execution: served rule facts need threshold inputs and approval facts ports")
	}
	intentID, err := uuid.Parse(rev.IntentID)
	if err != nil {
		return execute.RuleApproval{}, fmt.Errorf("platform execution: threshold lookup needs an intent-keyed revision: %w", err)
	}
	record, found, err := rulethreshold.Latest(ctx, ex, tenantID, intentID, rev.Revision)
	if err != nil {
		return execute.RuleApproval{}, err
	}
	if !found {
		return execute.RuleApproval{}, nil
	}
	decisions, err := s.Approval.Decisions(ctx, ex, tenantID, rev)
	if err != nil {
		return execute.RuleApproval{}, err
	}
	var approvalDigest string
	for _, d := range decisions {
		if d.ProposalDigest != rev.MaterialDigest.Digest || d.Invalidated || d.Outcome != runtime.ApprovalOutcomeApproved {
			continue
		}
		// ApprovalDecisionFact carries no digest column; the decision's
		// own content identity stands in. Minting a sha256: label for it
		// would be theater, and the verdict never compares this value --
		// it cites the approval the re-evaluation ran under.
		approvalDigest = d.DecisionID
		break
	}
	if approvalDigest == "" {
		return execute.RuleApproval{}, nil
	}
	current, err := s.currentThresholdInputs(ctx, record, tenantID, rev)
	if err != nil {
		return execute.RuleApproval{}, err
	}
	return execute.RuleApproval{
		Resolved: true,
		Approved: rules.ApprovedPlan{
			Input: record.Input, InputDigest: record.InputDigest,
			Tier: rules.ApprovalTier(record.Tier), MatchedRowID: record.MatchedRow,
			TableID: record.TableID, TableVersion: record.TableVersion, TableDigest: record.TableDigest,
			ApprovalDigest: approvalDigest,
		},
		Current: current,
	}, nil
}
