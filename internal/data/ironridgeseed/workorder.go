// Package ironridgeseed contains the reviewed, deterministic work-order
// fixtures for the Ironridge demo tenant.
package ironridgeseed

import (
	"encoding/json"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/workordertemplate"
)

const (
	TenantKey            = "ironridge-demo"
	TemplateID           = "IRONRIDGE_FIELD_WORK"
	TemplateVersion      = "1.0.0"
	TemplatePublishedBy  = "hcmnext.seed/ironridge-demo"
	TemplateReviewRef    = "planning/research/work-order-workflow-2026-09-25.md#Ironridge-pilot-case"
	PilotProjectKey      = "RIV"
	PilotWorkOrderKey    = "RIV-WO-GYM-CLOSEOUT-01"
	PilotInitiatorWorker = "ir-005-greg-novak"
	PilotSupervisor      = "ir-008-curtis-bell"
)

// PilotSpec names the existing project artifacts a seed adapter must resolve
// before it calls the authenticated work order service. These are natural
// keys from the Ironridge corpus; they are never treated as database IDs.
type PilotSpec struct {
	Key, TenantKey, ProjectKey, TemplateID, TemplateVersion string
	Title, Scope, InitiatorWorker, SupervisorWorker         string
	LinkedTaskKeys, DocumentKeys, ConversationKeys          []string
}

// RiversideGymWingPilot is deliberately only a plan. A runner must
// resolve these natural keys and call Service.Create with a verified principal
// and current project authority; it must not persist a hand-built snapshot.
func RiversideGymWingPilot() PilotSpec {
	return PilotSpec{
		Key: PilotWorkOrderKey, TenantKey: TenantKey, ProjectKey: PilotProjectKey,
		TemplateID: TemplateID, TemplateVersion: TemplateVersion,
		Title:           "Riverside gym wing punch and closeout",
		Scope:           "Coordinate the gym wing punch walk, wall protection repairs, inspection evidence, crew time, material spend, and turnover billing before the district walk. The earlier stair stringer CO-03 remains in the approved change-order record.",
		InitiatorWorker: PilotInitiatorWorker, SupervisorWorker: PilotSupervisor,
		LinkedTaskKeys:   []string{"RIV-35", "RIV-32", "RIV-34"},
		DocumentKeys:     []string{"riverside-project-overview", "daily-report-template", "riverside-gym-wing-work-order"},
		ConversationKeys: []string{"jobsite-riverside", "foremen"},
	}
}

// PublishedTemplate builds the immutable, configurable field-work template
// used by the Riverside pilot. Every approval, phase gate, evidence item,
// report, and billing policy is pinned by the returned digest.
func PublishedTemplate() (workordertemplate.Published, error) {
	return workordertemplate.Publish(fieldWorkDraft(), workordertemplate.PublishMeta{
		Version: TemplateVersion, PublishedBy: TemplatePublishedBy, ReviewRef: TemplateReviewRef,
	}, nil)
}

// PublishedTemplatePayload returns the digest-bearing JSON envelope accepted
// by workorderstore.PublishTemplate.
func PublishedTemplatePayload() (json.RawMessage, string, error) {
	published, err := PublishedTemplate()
	if err != nil {
		return nil, "", err
	}
	raw, err := json.Marshal(published)
	if err != nil {
		return nil, "", err
	}
	return raw, published.Digest(), nil
}

func fieldWorkDraft() workordertemplate.Draft {
	roleActions := []string{"read", "note", "work_log"}
	return workordertemplate.Draft{
		ID: TemplateID, Name: "Ironridge field work order",
		Phases: []workordertemplate.Phase{
			{ID: "DRAFT", AllowedExits: []string{"AUTHORIZATION"}, ActorRoles: []string{"INITIATOR"}},
			{ID: "AUTHORIZATION", AllowedExits: []string{"READY"}, RequiredRequests: []string{"SCOPE_APPROVAL", "BUDGET"}, Gates: []workordertemplate.Gate{{ID: "AUTHORIZATION_GATE", Kind: "AUTHORIZATION", PolicyRef: "ironridge.work_order.authorization/v1", Required: true}}, ActorRoles: []string{"SUPERVISOR", "FINANCE"}},
			{ID: "READY", AllowedExits: []string{"EXECUTION"}, RequiredDecisions: []string{"SCOPE_APPROVAL", "BUDGET"}, Gates: []workordertemplate.Gate{{ID: "SAFETY_GATE", Kind: "SAFETY", PolicyRef: "ironridge.work_order.site_safety/v1", Required: true}}, ActorRoles: []string{"SUPERVISOR"}},
			{ID: "EXECUTION", AllowedExits: []string{"INSPECTION"}, ActorRoles: []string{"SUPERVISOR", "FOREMAN", "LABORER"}},
			{ID: "INSPECTION", AllowedExits: []string{"ACCEPTED"}, RequiredEvidence: []string{"INSTALLATION_PHOTOS", "INSPECTION_RESULT"}, Gates: []workordertemplate.Gate{{ID: "RECONCILIATION_GATE", Kind: "RECONCILIATION", PolicyRef: "ironridge.work_order.cost_reconciliation/v1", Required: true}}, ActorRoles: []string{"INSPECTOR", "SUPERVISOR"}},
			{ID: "ACCEPTED", AllowedExits: []string{"CLOSED"}, RequiredRequests: []string{"BILLING_REVIEW"}, Gates: []workordertemplate.Gate{{ID: "CLOSURE_GATE", Kind: "CLOSURE", PolicyRef: "ironridge.work_order.closeout/v1", Required: true}}, ActorRoles: []string{"SUPERVISOR", "FINANCE"}},
			{ID: "CLOSED", ActorRoles: []string{"SUPERVISOR"}},
			{ID: "CANCELLED", ActorRoles: []string{"SUPERVISOR"}, Optional: true},
		},
		Forms: []workordertemplate.RequestForm{
			{ID: "SCOPE_FORM", Version: "1", Fields: []workordertemplate.FormField{{ID: "scope_summary", Type: workordertemplate.FieldText, Required: true}, {ID: "drawing_revision", Type: workordertemplate.FieldText, Required: true}, {ID: "evidence_ref", Type: workordertemplate.FieldEvidence, Required: true}}},
			{ID: "BUDGET_FORM", Version: "1", Fields: []workordertemplate.FormField{{ID: "amount", Type: workordertemplate.FieldDecimal, Required: true}, {ID: "currency", Type: workordertemplate.FieldText, Required: true}, {ID: "baseline_revision", Type: workordertemplate.FieldText, Required: true}, {ID: "funding_source", Type: workordertemplate.FieldText, Required: true}}},
			{ID: "BILLING_FORM", Version: "1", Fields: []workordertemplate.FormField{{ID: "period", Type: workordertemplate.FieldText, Required: true}, {ID: "quantities_ref", Type: workordertemplate.FieldEvidence, Required: true}, {ID: "cost_reconciliation_ref", Type: workordertemplate.FieldEvidence, Required: true}}},
		},
		Requests: []workordertemplate.RequestDefinition{
			{ID: "SCOPE_APPROVAL", Kind: workordertemplate.RequestApproval, FormRef: "SCOPE_FORM", AllowedPhases: []string{"AUTHORIZATION"}, ApprovalPolicyRef: "ironridge.work_order.scope_approval/v1"},
			{ID: "BUDGET", Kind: workordertemplate.RequestBudget, FormRef: "BUDGET_FORM", AllowedPhases: []string{"AUTHORIZATION"}, ApprovalPolicyRef: "ironridge.work_order.budget_approval/v1"},
			{ID: "BILLING_REVIEW", Kind: workordertemplate.RequestBillingReview, FormRef: "BILLING_FORM", AllowedPhases: []string{"ACCEPTED"}, ApprovalPolicyRef: "ironridge.work_order.billing_approval/v1"},
		},
		Evidence: []workordertemplate.EvidenceRequirement{
			{ID: "INSTALLATION_PHOTOS", Description: "Dated field photographs of completed work and punch corrections", AtPhase: "INSPECTION", Required: true},
			{ID: "INSPECTION_RESULT", Description: "Signed inspection result and any corrective work evidence", AtPhase: "INSPECTION", Required: true},
		},
		Roles: []workordertemplate.RoleGrant{
			{Role: "INITIATOR", Actions: []string{"create", "request", "note", "read"}},
			{Role: "SUPERVISOR", Actions: []string{"read", "advance", "assign", "approve", "note", "work_log", "report"}},
			{Role: "FINANCE", Actions: []string{"read", "approve_budget", "approve_billing", "report"}},
			{Role: "FOREMAN", Actions: roleActions},
			{Role: "LABORER", Actions: []string{"read", "work_log"}},
			{Role: "INSPECTOR", Actions: []string{"read", "inspect", "note"}},
		},
		ReportPolicies: []workordertemplate.ReportPolicy{{ID: "ironridge.work_order.field_report", Version: "1", Kind: workordertemplate.ReportDailyField, Required: true}, {ID: "ironridge.work_order.billing_report", Version: "1", Kind: workordertemplate.ReportCost, Required: true}},
		Billing:        &workordertemplate.BillingPolicy{ID: "ironridge.work_order.field_unit_rates", Version: "1", Mode: "UNIT_PRICE", Required: true},
		PolicyRefs: []workordertemplate.PolicyRef{
			{ID: "ironridge.work_order.authorization", Version: "v1"},
			{ID: "ironridge.work_order.site_safety", Version: "v1"},
			{ID: "ironridge.work_order.cost_reconciliation", Version: "v1"},
			{ID: "ironridge.work_order.closeout", Version: "v1"},
			{ID: "ironridge.work_order.scope_approval", Version: "v1"},
			{ID: "ironridge.work_order.budget_approval", Version: "v1"},
			{ID: "ironridge.work_order.billing_approval", Version: "v1"},
		},
	}
}
