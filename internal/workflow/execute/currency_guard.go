package execute

import (
	"context"
	"time"

	"github.com/google/uuid"

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
)

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
