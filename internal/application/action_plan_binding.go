package application

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/intentcontrol"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/plan"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute/effects"
)

// executionActionPlanBinder resolves an approval written by the intent
// service, binds it to the transaction plan the provider prepared inside the
// commit transaction, and appends the full canonical binding proof there.
type executionActionPlanBinder struct{}

var _ effects.ActionPlanBinder = executionActionPlanBinder{}

func (executionActionPlanBinder) BindAndPersist(
	ctx context.Context, tx dbport.Tx, req execute.TerminalWriteRequest, prepared plan.TransactionPlan,
) (string, error) {
	if err := prepared.VerifyDigest(); err != nil {
		return "", fmt.Errorf("prepared transaction plan is invalid: %w", err)
	}
	revision := req.Proposal.Revision
	if revision.ProposalRevisionID == "" || revision.IntentID == "" ||
		revision.ProposalRevisionID != prepared.ProposalRevisionID ||
		revision.MaterialDigest.Digest != prepared.ProposalDigest ||
		prepared.Tenant.String() == "" ||
		prepared.IdempotencyKey != req.IdempotencyKey {
		return "", fmt.Errorf("terminal request does not match the prepared plan's proposal or semantic key")
	}
	tenantID := req.TenantID
	intentID, err := uuid.Parse(revision.IntentID)
	if err != nil || intentID == uuid.Nil {
		return "", fmt.Errorf("terminal proposal intent id %q is not a durable intent identity", revision.IntentID)
	}
	accepted, err := (app.DurableProposalFacts{}).ResolveAcceptedAction(ctx, tx, app.AcceptedActionLookup{
		Tenant: prepared.Tenant, TenantID: tenantID, IntentID: revision.IntentID,
		ActionID:           app.AcceptedIntentExecutionActionID,
		ProposalRevisionID: prepared.ProposalRevisionID,
		ProposalDigest:     prepared.ProposalDigest, IdempotencyKey: prepared.IdempotencyKey,
	})
	if err != nil {
		return "", fmt.Errorf("resolve durable accepted action: %w", err)
	}
	binding, err := app.BindAcceptedAction(accepted, prepared)
	if err != nil {
		return "", err
	}
	if err := binding.VerifyDigest(); err != nil {
		return "", err
	}
	decisionID, err := uuid.Parse(binding.DecisionID)
	if err != nil || decisionID == uuid.Nil {
		return "", fmt.Errorf("accepted decision id %q is invalid", binding.DecisionID)
	}
	planID, err := uuid.Parse(binding.PlanID)
	if err != nil || planID == uuid.Nil {
		return "", fmt.Errorf("prepared plan id %q is invalid", binding.PlanID)
	}
	stored, err := (intentcontrol.ActionPlanBindingStore{}).Record(ctx, tx, intentcontrol.ActionPlanBinding{
		TenantID: tenantID, DecisionID: decisionID, PlanID: planID,
		ActionID: binding.ActionID, IntentID: intentID,
		ProposalRevisionID: binding.ProposalRevisionID, ProposalDigest: binding.ProposalDigest,
		AcceptedBy: binding.AcceptedBy, AcceptedAt: binding.AcceptedAt.Time(),
		IdempotencyKey: binding.IdempotencyKey, PlanDigest: binding.PlanDigest,
		BindingDigest: binding.Digest, BindingPayload: binding.CanonicalBytes(),
	})
	if err != nil {
		return "", fmt.Errorf("persist action-plan binding: %w", err)
	}
	return stored.BindingDigest, nil
}
