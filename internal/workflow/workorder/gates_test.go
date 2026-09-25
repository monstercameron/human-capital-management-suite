package workorder

import (
	"errors"
	"testing"

	orderdomain "github.com/monstercameron/human-capital-management-suite/internal/domains/workorder"
	templatedomain "github.com/monstercameron/human-capital-management-suite/internal/domains/workordertemplate"
)

func gateTemplate(t *testing.T) templatedomain.Published {
	t.Helper()
	draft := templatedomain.Draft{
		ID: "STAIR_REPAIR", Name: "Stair repair",
		Phases: []templatedomain.Phase{
			{ID: "DRAFT", AllowedExits: []string{"AUTHORIZATION"}, ActorRoles: []string{"INITIATOR"}},
			{ID: "AUTHORIZATION", AllowedExits: []string{"READY"}, ActorRoles: []string{"SUPERVISOR"}, RequiredRequests: []string{"INITIAL_BUDGET"}, RequiredDecisions: []string{"SCOPE_APPROVAL"}, RequiredEvidence: []string{"SCOPE_EVIDENCE"}, Gates: []templatedomain.Gate{{ID: "AUTHORIZATION_GATE", Kind: "AUTHORIZATION", PolicyRef: "approval.scope/v1", Required: true}, {ID: "SAFETY_GATE", Kind: "SAFETY", PolicyRef: "safety.work_order/v1", Required: true}}},
			{ID: "READY", AllowedExits: []string{"EXECUTION"}, ActorRoles: []string{"SUPERVISOR"}},
			{ID: "EXECUTION", AllowedExits: []string{"INSPECTION"}, ActorRoles: []string{"CREW"}},
			{ID: "INSPECTION", AllowedExits: []string{"ACCEPTED"}, ActorRoles: []string{"INSPECTOR"}, Gates: []templatedomain.Gate{{ID: "RECONCILIATION_GATE", Kind: "RECONCILIATION", Required: true}}},
			{ID: "ACCEPTED", AllowedExits: []string{"CLOSED"}, ActorRoles: []string{"SUPERVISOR"}},
			{ID: "CLOSED", ActorRoles: []string{"SUPERVISOR"}, Gates: []templatedomain.Gate{{ID: "CLOSURE_GATE", Kind: "CLOSURE", Required: true}}},
		},
		Forms:    []templatedomain.RequestForm{{ID: "BUDGET_FORM", Version: "1", Fields: []templatedomain.FormField{{ID: "amount", Type: templatedomain.FieldDecimal, Required: true}}}},
		Requests: []templatedomain.RequestDefinition{{ID: "INITIAL_BUDGET", Kind: templatedomain.RequestBudget, FormRef: "BUDGET_FORM", AllowedPhases: []string{"AUTHORIZATION"}}},
		Evidence: []templatedomain.EvidenceRequirement{{ID: "SCOPE_EVIDENCE", Description: "Approved scope drawing", AtPhase: "AUTHORIZATION", Required: true}},
		Roles:    []templatedomain.RoleGrant{{Role: "INITIATOR", Actions: []string{"create"}}, {Role: "SUPERVISOR", Actions: []string{"advance"}}, {Role: "CREW", Actions: []string{"log_work"}}, {Role: "INSPECTOR", Actions: []string{"inspect"}}},
	}
	published, err := templatedomain.Publish(draft, templatedomain.PublishMeta{Version: "1.0.0", PublishedBy: "principal:owner", ReviewRef: "review:approved"}, nil)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	return published
}

func gateOrder(published templatedomain.Published) orderdomain.Snapshot {
	return orderdomain.Snapshot{
		ID: "wo-1", TenantID: "tenant-1", ProjectID: "project-1", TemplateID: published.Pin().TemplateID, TemplateVersion: published.Pin().Version, TemplateDigest: published.Pin().Digest,
		WorkflowID: WorkflowIDForTemplate(published.Pin().TemplateID), WorkflowVersion: "1", WorkflowDigest: "sha256:work-order-plan", WorkflowInstanceID: "workflow-instance-1",
		Phase: "AUTHORIZATION", Revision: 4, Transitions: []orderdomain.PhaseTransition{{From: "AUTHORIZATION", To: "READY"}},
		Requests: []orderdomain.InitiatorRequest{{ID: "request-instance-1", DefinitionID: "INITIAL_BUDGET", Kind: orderdomain.RequestBudget, Status: orderdomain.RequestPending}},
	}
}

func completeGateFacts(published templatedomain.Published, order orderdomain.Snapshot) GateFacts {
	return GateFacts{
		WorkOrderID: order.ID, OrderRevision: order.Revision, TemplateDigest: published.Pin().Digest,
		WorkflowVersion: order.WorkflowVersion, WorkflowDigest: order.WorkflowDigest, WorkflowInstanceID: order.WorkflowInstanceID,
		Decisions:           []DecisionFact{{ID: "SCOPE_APPROVAL", Satisfied: true}},
		Evidence:            []EvidenceFact{{ID: "SCOPE_EVIDENCE", SourceRef: "evidence://scope-approved", Satisfied: true}},
		Gates:               []GateFact{{ID: "AUTHORIZATION_GATE", Kind: "AUTHORIZATION", PolicyRef: "approval.scope/v1", Satisfied: true}, {ID: "SAFETY_GATE", Kind: "SAFETY", PolicyRef: "safety.work_order/v1", Satisfied: true}},
		WorkflowCompletions: []WorkflowCompletionFact{{NodeID: NodeApproveBudget, Outcome: "APPROVED", PlanVersion: order.WorkflowVersion, PlanDigest: order.WorkflowDigest, InstanceID: order.WorkflowInstanceID, WorkOrderID: order.ID, OrderRevision: order.Revision}},
	}
}

func TestEvaluateTransitionRequiresAllPinnedPhaseRequirements(t *testing.T) {
	published := gateTemplate(t)
	order := gateOrder(published)
	order.Requests[0].Status = orderdomain.RequestApproved
	decision, err := EvaluateTransition(published, order, "READY", completeGateFacts(published, order))
	if err != nil || !decision.Allowed || len(decision.Blockers) != 0 {
		t.Fatalf("complete gate decision = %+v, err %v", decision, err)
	}
	if decision.Pin.Digest != published.Pin().Digest || decision.From != "AUTHORIZATION" || decision.To != "READY" {
		t.Fatalf("gate decision not bound to pinned transition: %+v", decision)
	}

	order.Requests[0].Status = orderdomain.RequestPending
	facts := completeGateFacts(published, order)
	facts.Decisions = nil
	facts.Evidence = nil
	facts.Gates = facts.Gates[:1]
	facts.WorkflowCompletions = nil
	decision, err = EvaluateTransition(published, order, "READY", facts)
	if err != nil || decision.Allowed || len(decision.Blockers) != 5 {
		t.Fatalf("incomplete gate decision = %+v, err %v; want five blockers", decision, err)
	}
}

func TestEvaluateTransitionRejectsStalePinsAndUndeclaredExit(t *testing.T) {
	published := gateTemplate(t)
	order := gateOrder(published)
	facts := completeGateFacts(published, order)
	facts.OrderRevision++
	if _, err := EvaluateTransition(published, order, "READY", facts); !errors.Is(err, ErrStaleGateFacts) {
		t.Fatalf("stale facts error = %v, want ErrStaleGateFacts", err)
	}
	facts = completeGateFacts(published, order)
	if _, err := EvaluateTransition(published, order, "CLOSED", facts); !errors.Is(err, ErrPhaseTransition) {
		t.Fatalf("undeclared transition error = %v, want ErrPhaseTransition", err)
	}
	wrongPin := order
	wrongPin.TemplateVersion = "2.0.0"
	if _, err := EvaluateTransition(published, wrongPin, "READY", completeGateFacts(published, wrongPin)); !errors.Is(err, ErrTemplateMismatch) {
		t.Fatalf("template mismatch error = %v, want ErrTemplateMismatch", err)
	}
	wrongWorkflow := order
	wrongWorkflow.WorkflowDigest = "sha256:other-plan"
	if _, err := EvaluateTransition(published, wrongWorkflow, "READY", completeGateFacts(published, order)); !errors.Is(err, ErrStaleGateFacts) {
		t.Fatalf("workflow digest mismatch error = %v, want ErrStaleGateFacts", err)
	}
	missingRuntimeFact := completeGateFacts(published, order)
	missingRuntimeFact.WorkflowCompletions = nil
	decision, err := EvaluateTransition(published, order, "READY", missingRuntimeFact)
	workflowBlocked := false
	for _, blocker := range decision.Blockers {
		workflowBlocked = workflowBlocked || blocker.Kind == "WORKFLOW"
	}
	if err != nil || decision.Allowed || !workflowBlocked {
		t.Fatalf("missing runtime completion decision = %+v, err %v", decision, err)
	}
	forged := completeGateFacts(published, order)
	forged.WorkflowCompletions = []WorkflowCompletionFact{{
		NodeID: NodeApproveBudget, Outcome: "APPROVED", PlanVersion: order.WorkflowVersion,
		PlanDigest: order.WorkflowDigest, InstanceID: order.WorkflowInstanceID,
		OrderRevision: order.Revision + 1,
	}}
	decision, err = EvaluateTransition(published, order, "READY", forged)
	workflowBlocked = false
	for _, blocker := range decision.Blockers {
		workflowBlocked = workflowBlocked || blocker.Kind == "WORKFLOW"
	}
	if err != nil || decision.Allowed || !workflowBlocked {
		t.Fatalf("node-only/forged subject completion decision = %+v, err %v", decision, err)
	}
}

func TestEvaluateTransitionRejectsTemplateShortcutAroundCompiledPhases(t *testing.T) {
	published := gateTemplate(t)
	draft := published.Snapshot()
	for i := range draft.Phases {
		if draft.Phases[i].ID == "AUTHORIZATION" {
			draft.Phases[i].AllowedExits = append(draft.Phases[i].AllowedExits, "CLOSED")
		}
	}
	shortcut, err := templatedomain.Publish(draft, templatedomain.PublishMeta{Version: "1.0.0", PublishedBy: "principal:owner", ReviewRef: "review:approved"}, nil)
	if err != nil {
		t.Fatalf("publish shortcut template fixture: %v", err)
	}
	order := gateOrder(shortcut)
	if _, err := EvaluateTransition(shortcut, order, "READY", completeGateFacts(shortcut, order)); !errors.Is(err, ErrPhaseBinding) {
		t.Fatalf("shortcut template error = %v, want ErrPhaseBinding", err)
	}
}
