// Package contract preserves the historical UX qualification import path.
// Product and renderer code use the production-owned implementation under
// internal/experience/workspacecontract.
package contract

import production "github.com/monstercameron/human-capital-management-suite/internal/experience/workspacecontract"

type (
	FieldKind         = production.FieldKind
	Severity          = production.Severity
	SimulationStatus  = production.SimulationStatus
	ActionVariant     = production.ActionVariant
	FieldValidation   = production.FieldValidation
	RequestField      = production.RequestField
	PromotionRequest  = production.PromotionRequest
	PreflightFinding  = production.PreflightFinding
	SimulationCheck   = production.SimulationCheck
	SimulationResult  = production.SimulationResult
	TimelineEvent     = production.TimelineEvent
	AvailableAction   = production.AvailableAction
	Provenance        = production.Provenance
	WorkspaceContract = production.WorkspaceContract
	SourceRecord      = production.SourceRecord
	FieldVisibility   = production.FieldVisibility
	ActionVisibility  = production.ActionVisibility
)

const (
	FieldKindText     = production.FieldKindText
	FieldKindTextarea = production.FieldKindTextarea
	FieldKindDate     = production.FieldKindDate
	FieldKindMoney    = production.FieldKindMoney
	FieldKindLookup   = production.FieldKindLookup
	FieldKindReadOnly = production.FieldKindReadOnly

	SeverityBlocking = production.SeverityBlocking
	SeverityWarning  = production.SeverityWarning
	SeverityInfo     = production.SeverityInfo
	SeveritySuccess  = production.SeveritySuccess

	SimulationPending     = production.SimulationPending
	SimulationReady       = production.SimulationReady
	SimulationNeedsReview = production.SimulationNeedsReview
	SimulationFailed      = production.SimulationFailed

	ActionPrimary   = production.ActionPrimary
	ActionSecondary = production.ActionSecondary
	ActionDanger    = production.ActionDanger
)

func Allow(ids ...string) map[string]bool {
	return production.Allow(ids...)
}

func NewWorkspaceContract(source SourceRecord, fields FieldVisibility, actions ActionVisibility) WorkspaceContract {
	return production.NewWorkspaceContract(source, fields, actions)
}

func MaskedFieldIDs(source SourceRecord, fields FieldVisibility) []string {
	return production.MaskedFieldIDs(source, fields)
}
