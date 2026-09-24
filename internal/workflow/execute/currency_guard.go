package execute

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/rules"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/approval"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// Currency-block reason codes [CurrencyGuard.Check] reports, spelled as one
// closed, stable vocabulary so a caller inspecting [CurrencyVerdict.Reason]
// never has to parse [CurrencyVerdict.Explanation] prose.
const (
	// ReasonCurrencyProposalSuperseded reports that the proposal store
	// already holds a materially different later revision of the pinned
	// proposal.
	ReasonCurrencyProposalSuperseded = "CURRENCY_PROPOSAL_SUPERSEDED"
	// ReasonCurrencyApprovalInvalid reports that the pinned revision carries
	// no standing (non-invalidated, APPROVED) decision.
	ReasonCurrencyApprovalInvalid = "CURRENCY_APPROVAL_INVALID"
	// ReasonCurrencyApprovalBindingMismatch reports an approval decision the
	// approval store hands back for the pinned revision's own id, bound to a
	// different material digest.
	ReasonCurrencyApprovalBindingMismatch = "CURRENCY_APPROVAL_BINDING_MISMATCH"
	// ReasonCurrencyRuleInvalidated reports that RULE-004's execution-time
	// re-evaluation of the plan's decision-table approval no longer confirms
	// it: the required tier moved under the currently published threshold
	// table, the inputs moved so the old approval set cannot stay valid, or
	// the frozen approval record itself no longer reproduces.
	ReasonCurrencyRuleInvalidated = "CURRENCY_RULE_INVALIDATED"
)

// RuleApproval is the decision-table approval context [RuleFacts.Lookup]
// resolves for one bound proposal revision (REV-010-01).
type RuleApproval struct {
	// Resolved is false when the plan's approval was not resolved by a
	// decision table: [CurrencyGuard.Check] skips re-evaluation and the
	// existing currency verdict stands unchanged.
	Resolved bool
	// Approved is the frozen approval-time record the decision-table
	// approval produced; Current is the execution-time input to re-run
	// the published table against. Both are read only when Resolved.
	Approved rules.ApprovedPlan
	Current  rules.PromotionApprovalInput
}

// RuleFacts resolves the decision-table approval record and current rule
// inputs for one bound proposal revision, from the caller-owned rule
// store -- never from a boolean the request asserts. A store that knows
// no decision-table approval for the revision reports the zero value
// (not resolved), the same answer a plan that never touched a decision
// table gets.
type RuleFacts interface {
	Lookup(ctx context.Context, ex runtime.Executor, tenantID uuid.UUID, rev intent.ProposalRevision, checkedAt time.Time) (RuleApproval, error)
}

// CurrencyCheckRequest is what [CurrencyGuard.Check] revalidates.
type CurrencyCheckRequest struct {
	TenantID   uuid.UUID
	InstanceID uuid.UUID
	// Proposal is the pinned binding the parked (or completing) instance
	// started under -- runContext.start.Proposal at every call site.
	Proposal  runtime.ProposalBinding
	CheckedAt time.Time
}

// CurrencyVerdict is [CurrencyGuard.Check]'s outcome.
type CurrencyVerdict struct {
	// Blocked is true when a material change was found: the instance must
	// move to BLOCKED and the call that asked must not advance.
	Blocked bool
	// Reason is one of the Reason* constants, present iff Blocked.
	Reason string
	// Explanation is deterministic, human-readable detail for Reason.
	Explanation []string

	// Immaterial is true when the pinned proposal was superseded by a later
	// revision whose material result is unchanged (a pure control-snapshot
	// revalidation) -- the run continues, and this revalidation is recorded
	// on the caller's own receipt rather than on [runtime.AdvanceReceipt],
	// which WF-RUN-029 has no field to add to.
	Immaterial bool
	// RevalidatedAgainstRevisionID names the current revision an immaterial
	// supersession was revalidated against, present iff Immaterial.
	RevalidatedAgainstRevisionID string
	// RuleReevaluation is the RULE-004 execution-time verdict
	// (REV-010-01), present iff Rules was non-nil and the plan's approval
	// was resolved by a decision table. A CONFIRM verdict joins the
	// continue path above; any other verdict joins the BLOCKED path under
	// [ReasonCurrencyRuleInvalidated], with the cited table versions
	// carried both here and in Explanation.
	RuleReevaluation *rules.Reevaluation
}

// CurrencyGuard is the one currency check WF-RUN-029 requires: it
// revalidates a pinned proposal's supersession through [runtime.ProposalFacts]
// and its approval decisions through [runtime.ApprovalFacts], applying
// internal/intent/approval's own materiality rule
// (internal/intent/approval.MaterialResultEqual, INTENT-006) to tell a
// material change (block) from an immaterial one (continue, recorded).
//
// [Driver.advanceOnce] is the single call site (WF-RUN-029's REFACTOR
// clause): every [Driver.Resume] and every advancement that may reach the
// terminal write goes through it, so this check is never duplicated in a
// step package.
type CurrencyGuard struct {
	Proposal runtime.ProposalFacts
	Approval runtime.ApprovalFacts
	// Rules, when non-nil, re-evaluates the plan's decision-table approval
	// against the currently published threshold table on every check
	// (REV-010-01, RULE-004). Nil skips re-evaluation entirely, exactly
	// reproducing the pre-REV-010-01 verdict for every plan.
	Rules RuleFacts
}

// Check revalidates req.Proposal.Revision's currency. A nil Proposal or
// Approval port is a caller wiring mistake, not a currency fact, and is
// reported as such rather than silently passing everything.
func (g CurrencyGuard) Check(ctx context.Context, ex runtime.Executor, req CurrencyCheckRequest) (ret0 CurrencyVerdict, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.execute.currency_check", req)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if g.Proposal == nil || g.Approval == nil {
		return CurrencyVerdict{}, invalid("currency guard requires both ProposalFacts and ApprovalFacts ports")
	}
	rev := req.Proposal.Revision

	supersession, err := g.Proposal.Supersession(ctx, ex, req.TenantID, rev)
	if err != nil {
		return CurrencyVerdict{}, err
	}
	verdict := CurrencyVerdict{}
	if supersession.Superseded {
		if supersession.CurrentRevision == nil || !approval.MaterialResultEqual(rev, *supersession.CurrentRevision) {
			return CurrencyVerdict{
				Blocked: true, Reason: ReasonCurrencyProposalSuperseded,
				Explanation: []string{"proposal " + rev.ProposalRevisionID + " superseded by " +
					supersession.SupersededByRevisionID + " with a material change"},
			}, nil
		}
		verdict.Immaterial = true
		verdict.RevalidatedAgainstRevisionID = supersession.CurrentRevision.ProposalRevisionID
	}

	decisions, err := g.Approval.Decisions(ctx, ex, req.TenantID, rev)
	if err != nil {
		return CurrencyVerdict{}, err
	}
	approved := false
	for _, d := range decisions {
		if d.ProposalDigest != rev.MaterialDigest.Digest {
			return CurrencyVerdict{
				Blocked: true, Reason: ReasonCurrencyApprovalBindingMismatch,
				Explanation: []string{"decision " + d.DecisionID + " is bound to " + d.ProposalDigest +
					", not " + rev.MaterialDigest.Digest},
			}, nil
		}
		if d.Invalidated {
			continue
		}
		if d.Outcome == runtime.ApprovalOutcomeApproved {
			approved = true
		}
	}
	if !approved {
		return CurrencyVerdict{
			Blocked: true, Reason: ReasonCurrencyApprovalInvalid,
			Explanation: []string{"proposal " + rev.ProposalRevisionID + " carries no standing approval decision"},
		}, nil
	}
	// REV-010-01: re-run the decision-table approval that produced this
	// plan's required tier against the currently published threshold table
	// (RULE-004). This is the same Resume and terminal-write point the
	// checks above already run at -- every advancement goes through
	// advanceOnce -- so no step package ever duplicates it.
	if g.Rules != nil {
		ruleCtx, err := g.Rules.Lookup(ctx, ex, req.TenantID, rev, req.CheckedAt)
		if err != nil {
			return CurrencyVerdict{}, err
		}
		if ruleCtx.Resolved {
			reevaluated, err := rules.ReevaluatePromotionApproval(rules.PromotionApprovalThresholdTable(), ruleCtx.Approved, ruleCtx.Current)
			if err != nil {
				// A frozen record that no longer reproduces cannot
				// honestly continue: block fail-closed under the same
				// closed reason rather than erroring the call.
				return CurrencyVerdict{
					Blocked: true, Reason: ReasonCurrencyRuleInvalidated,
					Explanation: []string{"rule re-evaluation refused: " + err.Error()},
				}, nil
			}
			if reevaluated.Verdict != rules.VerdictConfirmed {
				row := reevaluated.MatchedRowID
				if row == "" {
					row = "unmatched"
				}
				return CurrencyVerdict{
					Blocked: true, Reason: ReasonCurrencyRuleInvalidated,
					Explanation: []string{
						"rule " + reevaluated.Verdict + ": tier " + string(reevaluated.Tier) + " via row " + row,
						"original table " + reevaluated.OriginalTableID + "@" + reevaluated.OriginalTableVer,
						"current table " + reevaluated.CurrentTableID + "@" + reevaluated.CurrentTableVer,
					},
					RuleReevaluation: &reevaluated,
				}, nil
			}
			verdict.RuleReevaluation = &reevaluated
		}
	}
	return verdict, nil
}

// blockInstance moves instanceID to BLOCKED through the runtime store's
// existing transition path (no new table), loading its current row fresh so
// the transition carries every field [runtime.InstanceTransition] requires
// unchanged except Status.
func blockInstance(ctx context.Context, ex runtime.Executor, tenantID, instanceID uuid.UUID, verdict CurrencyVerdict) error {
	store := runtime.Store{}
	current, err := store.LoadInstance(ctx, ex, tenantID, instanceID)
	if err != nil {
		return err
	}
	_, err = store.RecordInstanceState(ctx, ex, runtime.InstanceTransition{
		TenantID: tenantID, InstanceID: instanceID, ExpectedVersion: current.InstanceVersion,
		Status:               runtime.InstanceBlocked,
		CurrentNodeIDs:       current.CurrentNodeIDs,
		VariableRevisionHead: current.VariableRevisionHead,
		EffectiveContextRef:  current.EffectiveContextRef,
		LastCheckpointRef:    current.LastCheckpointRef,
		CompletionDimensions: current.CompletionDimensions,
	})
	return err
}
