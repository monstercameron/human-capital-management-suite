// Package testdata holds the fixture data UX-QUAL-001's qualification
// fixture (and both renderers, for manual/demo use) render against. It is
// copied and hand-authored for this lane rather than imported from
// internal/intent/app or internal/domains, per this lane's constraints:
// other agents own those packages, and this lane must not import them.
//
// The shape and field names mirror the legacy TypeScript console's
// Promotion-adjacent screens (src/platform/ui-runtime/pages.ts's
// manager-request/approval-review/simulation/audit-timeline pages, and
// src/tests/e2e/compensation-change.e2e.spec.ts's proposedCompensation/
// effectiveAt/businessReason fields), read for the workspace's fields and
// flows as directed, but nothing here imports src/.
package testdata

import (
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/workspacecontract"
)

// SourceRecord returns the full, unmasked record a capability would resolve
// server-side for one Promotion request, before authorization narrows it.
// NationalID and ForceExecute exist specifically to be masked: no
// VisibleFields/VisibleActions fixture in this package or in tools/uxqual
// ever allows them, so any renderer output containing "555-11-2222",
// "nationalId", or "force_execute" is a masking defect.
func SourceRecord() contract.SourceRecord {
	return contract.SourceRecord{
		WorkspaceID: "wf-promo-1048",
		Title:       "Promotion: Jane Rivera",
		WorkerID:    "worker-1048",
		WorkerName:  "Jane Rivera",
		AllFields: []contract.RequestField{
			{
				ID: "currentJobTitle", Label: "Current job title",
				Kind: contract.FieldKindReadOnly, Value: "Registered Nurse II",
			},
			{
				ID: "proposedJobTitle", Label: "Proposed job title",
				Kind: contract.FieldKindLookup, Value: "Registered Nurse III",
				Validation: contract.FieldValidation{Required: true},
			},
			{
				ID: "proposedGrade", Label: "Proposed grade",
				Kind: contract.FieldKindText, Value: "RN3",
				Validation: contract.FieldValidation{Required: true},
			},
			{
				ID: "proposedCompensation", Label: "Proposed base pay",
				Kind: contract.FieldKindMoney, Value: "$98,000.00",
				Validation: contract.FieldValidation{Required: true},
			},
			{
				ID: "effectiveDate", Label: "Effective date",
				Kind: contract.FieldKindDate, Value: "2026-10-01",
				Validation: contract.FieldValidation{Required: true},
			},
			{
				ID: "businessReason", Label: "Business reason",
				Kind: contract.FieldKindTextarea,
				Value: "Scope of practice increase; unit is at retention risk " +
					"for this level.",
				Validation: contract.FieldValidation{Required: true},
			},
			// Masked in every fixture visibility map below.
			{ID: "nationalId", Label: "National ID", Kind: contract.FieldKindText, Value: "555-11-2222"},
		},
		Preflight: []contract.PreflightFinding{
			{
				ID: "band-check", Severity: contract.SeveritySuccess,
				Label: "Compensation band", Detail: "Proposed amount is within the approved band for the target level.",
			},
			{
				ID: "payroll-cutoff", Severity: contract.SeverityWarning,
				Label: "Payroll cutoff", Detail: "Effective date falls inside the next payroll window.",
			},
			{
				ID: "access-groups", Severity: contract.SeverityInfo,
				Label: "Identity access", Detail: "Department change will update downstream access groups.",
			},
		},
		Simulation: contract.SimulationResult{
			Status:      contract.SimulationReady,
			Summary:     "Simulation completed with no blocking findings.",
			GeneratedAt: time.Date(2026, 9, 1, 11, 10, 0, 0, time.UTC),
			Checks: []contract.SimulationCheck{
				{Label: "Compensation band", Status: contract.SeveritySuccess, Detail: "Proposed amount is within approved band."},
				{Label: "Payroll cutoff", Status: contract.SeverityWarning, Detail: "Effective date is inside the next payroll window."},
				{Label: "Identity access", Status: contract.SeverityInfo, Detail: "Department change will update downstream access groups."},
			},
		},
		Timeline: []contract.TimelineEvent{
			{At: time.Date(2026, 9, 1, 9, 5, 0, 0, time.UTC), Actor: "Alex Manager", Label: "Submitted request"},
			{At: time.Date(2026, 9, 1, 10, 20, 0, 0, time.UTC), Actor: "Riley HRBP", Label: "Approved HRBP review"},
			{At: time.Date(2026, 9, 1, 11, 10, 0, 0, time.UTC), Actor: "System", Label: "Simulation completed"},
		},
		AllActions: []contract.AvailableAction{
			{ID: "approve", Label: "Approve", Transition: "approve", Variant: contract.ActionPrimary},
			{ID: "reject", Label: "Reject", Transition: "reject", Variant: contract.ActionDanger, RequiresReason: true},
			{ID: "request_more_information", Label: "Request info", Transition: "request_more_information", Variant: contract.ActionSecondary},
			// Masked in every fixture visibility map below.
			{ID: "force_execute", Label: "Force execute", Transition: "force_execute", Variant: contract.ActionDanger},
		},
		Provenance: contract.Provenance{
			CapabilityID:      "people.promote",
			CapabilityVersion: "v1",
			SourceSystem:      "hcm-next",
			AsOf:              time.Date(2026, 9, 1, 11, 10, 0, 0, time.UTC),
		},
	}
}

// VisibleFields is the authorized field allowlist used everywhere in this
// lane: it deliberately excludes "nationalId".
func VisibleFields() contract.FieldVisibility {
	return contract.Allow("currentJobTitle", "proposedJobTitle", "proposedGrade", "proposedCompensation", "effectiveDate", "businessReason")
}

// VisibleActions is the authorized action allowlist used everywhere in this
// lane: it deliberately excludes "force_execute".
func VisibleActions() contract.ActionVisibility {
	return contract.Allow("approve", "reject", "request_more_information")
}

// MaskedNeedles are the raw strings that must never appear in any rendered
// output (HTML, JSON, or otherwise) for the fixture contract, because they
// belong to a masked field or action.
func MaskedNeedles() []string {
	return []string{"nationalId", "555-11-2222", "force_execute", "Force execute"}
}

// PromotionFixture is the ready-to-render WorkspaceContract: SourceRecord
// already projected through VisibleFields/VisibleActions, exactly as a
// capability would hand it to a renderer.
func PromotionFixture() contract.WorkspaceContract {
	return contract.NewWorkspaceContract(SourceRecord(), VisibleFields(), VisibleActions())
}
