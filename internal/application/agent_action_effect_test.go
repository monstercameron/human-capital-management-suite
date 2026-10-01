package application

import (
	"context"
	"testing"

	registryv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/registry/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// This proof uses real PostgreSQL, the served HCM command driver, distinct
// human approvers, the timer and durable provider confirmations. Only the
// external providers are fixture ports; the HCM effect and receipts are real.
func TestTodo_AGENT_036_ObservedEffect(t *testing.T) {
	runAgentActionEffect(t, false)
}

func TestTodo_AGENT_036_RevokedBeforeEffect(t *testing.T) {
	runAgentActionEffect(t, true)
}

func newAgentActionPublishedFixture(t *testing.T) (*agentActionFixture, *promoux015Harness, workspace.JourneyEngine) {
	t.Helper()
	h := promoux015ComposeWithPublishedWorkforce(t)
	discovered, _ := h.discoverPromotion("hiring-manager")
	owner, ctx := h.engine("hiring-manager")
	cell := h.composed.Cell()
	material, err := cell.Service.PrepareAgentPromotionRequest(ctx, workspace.ProposalInput{WorkerRef: h.subject, TargetJobCode: discovered.GetTarget().GetJobCode(), TargetGrade: discovered.GetTarget().GetGrade(), TargetPositionID: discovered.GetTarget().GetPositionId(), ProposedBase: discovered.GetProposedBase(), EffectiveDate: h.effective, BusinessReason: discovered.GetBusinessReason()})
	if err != nil {
		t.Fatal(err)
	}
	published, err := cell.Service.GetIntentDefinition(ctx, &registryv1.GetIntentDefinitionRequest{Definition: material.Definition})
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewAgentActionService(cell)
	if err != nil {
		t.Fatal(err)
	}
	proposal := AgentActionCompileRequest{Definition: published.IntentDefinition, Request: material, AgentRunID: "served-effect-run", AgentVersionRef: "served-effect-agent@1", ModelDigest: "sha256:typed-model-output", Sources: []string{"worker:" + h.subjectWorkerID.String()}, Taint: []string{"AGENT_DERIVED"}, Uncertainty: "awaiting normal human approvals and observed providers"}
	principal, _ := trust.FromContext(ctx)
	authority := &agentActionTestAuthority{proposal: proposal, user: principal.Subject(), tenant: principal.Tenant().String()}
	if err := service.BindAuthority(authority); err != nil {
		t.Fatal(err)
	}
	return &agentActionFixture{service: service, cell: cell, ctx: ctx, req: proposal, authority: authority, harness: h}, h, owner
}

func runAgentActionEffect(t *testing.T, revoke bool) {
	f, h, owner := newAgentActionPublishedFixture(t)
	service, ctx, proposal := f.service, f.ctx, f.req
	authority := f.authority
	draft, err := service.Compile(ctx, proposal)
	if err != nil {
		t.Fatalf("compile served material: %v %s", err, promoux013Diagnostic(err))
	}
	before := h.effects()
	if submitted, err := service.Submit(ctx, agentActionSubmission(draft)); err != nil || submitted.State != "awaiting approval" {
		t.Fatalf("exact submission: %+v %v %s", submitted, err, promoux013Diagnostic(err))
	}
	if err := promoux015Unchanged(before, h.effects()); err != nil {
		t.Fatalf("submission mutated HCM before business approval: %v", err)
	}
	_, financeCtx := h.engine("finance-partner")
	if _, err := owner.Decide(financeCtx, draft.IntentID, workspace.Decision{Approve: true, Reason: "finance authorizes exact proposed budget"}); err != nil {
		t.Fatal(err)
	}
	_, managerCtx := h.engine("admin")
	if _, err := owner.Decide(managerCtx, draft.IntentID, workspace.Decision{Approve: true, Reason: "manager authorizes exact proposed promotion"}); err != nil {
		t.Fatal(err)
	}
	if err := promoux015Unchanged(before, h.effects()); err != nil {
		t.Fatalf("HCM changed before effective date: %v", err)
	}
	if revoke {
		authority.refused = true
	}
	fired, err := h.scheduler(h.afterEffectiveDate()).Tick(context.Background())
	if revoke {
		if err := promoux015Unchanged(before, h.effects()); err != nil {
			t.Fatalf("revoked agent run wrote HCM effect: %v", err)
		}
		state, err := service.Observe(ctx, draft.IntentID)
		if err != nil || state.State == "observed" {
			t.Fatalf("revoked action claimed observed success: %+v %v", state, err)
		}
		return
	}
	if err != nil || fired.Fired != 1 {
		t.Fatalf("effective-date drive: %+v %v", fired, err)
	}
	pending, err := service.Observe(ctx, draft.IntentID)
	if err != nil || pending.State == "observed" {
		t.Fatalf("success claimed before provider observations: %+v %v", pending, err)
	}
	h.confirmProviders()
	if _, err := owner.Acknowledge(h.ackOperatorCtx(), draft.IntentID, workspace.Acknowledgement{EvidenceRef: "hris:signature:agent-action", Note: "Exact promotion and signed record verified"}); err != nil {
		t.Fatal(err)
	}
	after := h.effects()
	if err := promoux015CommittedOnce(before, after); err != nil {
		t.Fatalf("authorized effect: %v", err)
	}
	observed, err := service.Observe(ctx, draft.IntentID)
	if err != nil || observed.State != "observed" || observed.ReceiptRef == "" {
		t.Fatalf("durable observed action: %+v %v", observed, err)
	}
	if _, err := service.Observe(ctx, draft.IntentID); err != nil {
		t.Fatal(err)
	}
	if err := promoux015Unchanged(after, h.effects()); err != nil {
		t.Fatalf("observation duplicated effects: %v", err)
	}
}
