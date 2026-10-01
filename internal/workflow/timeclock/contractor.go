package timeclock

import (
	"github.com/monstercameron/human-capital-management-suite/internal/domains/contractortime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// ContractorWorkflowID identifies the contractor time-to-invoice workflow.
const ContractorWorkflowID = "hcmnext.workflows.time.contractor_invoice"

// ContractorVersion is the published contractor workflow version.
const ContractorVersion = 1

// IntentContractorTime is the typed intent accepted by the contractor plan.
const IntentContractorTime = "hcmnext.time.contractor_time/v1"

// Contractor event and terminal identities are stable integration vocabulary.
const (
	EventTypeContractorEntry  = "hcmnext.events.time.contractor_entry"
	EventTypeContractorReopen = "hcmnext.events.time.contractor_reopen_exception"
	CorrelationContractor     = "subject:contractor_invoice"
	TerminalContractorInvoice = "TIME_CONTRACTOR_INVOICE_SUBMITTED"
	TerminalContractorMissing = "TIME_CONTRACTOR_ENTRY_MISSING"
	TerminalContractorHeld    = "TIME_CONTRACTOR_ENTRY_HELD"
	TerminalContractorRepair  = "TIME_CONTRACTOR_INVOICE_REPAIR_REQUIRED"
	TerminalContractorCancel  = "TIME_CONTRACTOR_INVOICE_CANCELLED"
)

// Contractor workflow node identities.
const (
	NodeContractorOpenPeriod   = "contractor_open_invoice_period"
	NodeContractorAwaitEntry   = "contractor_await_entry"
	NodeContractorRecordEntry  = "contractor_record_entry"
	NodeContractorValidateSOW  = "contractor_validate_sow"
	NodeContractorReviewEntry  = "contractor_review_entry"
	NodeContractorApproveEntry = "contractor_approve_entry"
	NodeContractorSubmitToAP   = "contractor_submit_to_ap"
	NodeContractorObserveAP    = "contractor_observe_ap"
	NodeContractorAwaitReopen  = "contractor_await_reopen_exception"
	NodeContractorEndSubmitted = "contractor_end_invoice_submitted"
	NodeContractorEndMissing   = "contractor_end_missing_entry"
	NodeContractorEndHeld      = "contractor_end_entry_held"
	NodeContractorEndRepair    = "contractor_end_invoice_repair"
	NodeContractorEndCancelled = "contractor_end_cancelled"
)

// Contractor validation routes are ordered from the strongest rejection to
// the accepted result. The evaluator owns the SOW, currency, tax and pricing
// checks; this package only publishes their closed route vocabulary.
const (
	ContractorRouteMissingSOW       = "SOW_MISSING"
	ContractorRouteRateOutsideSOW   = "RATE_OUTSIDE_SOW"
	ContractorRouteCurrencyMissing  = "CURRENCY_MISSING"
	ContractorRouteTaxTreatmentMiss = "TAX_TREATMENT_MISSING"
	ContractorRouteEntryInvalid     = "ENTRY_INVALID"
	ContractorRouteValid            = "VALID"
)

// ContractorPolicy is the minimum typed policy context required to price an
// invoice. It deliberately has no schedule, shift, clock, geofence, photo or
// break fields: those are employee controls and are outside this workflow.
type ContractorPolicy struct {
	Pricing      contractortime.PricingModel
	SOWReference string
	POReference  string
	RateCardRef  string
	Currency     string
	TaxTreatment contractortime.TaxKind
	SelfBilling  bool
}

// Valid reports whether the policy has the references needed for governed
// invoice construction. The detailed amount and rate checks remain in the
// contractortime domain package.
func (p ContractorPolicy) Valid() bool {
	return p.Pricing.Valid() && p.SOWReference != "" && p.POReference != "" &&
		p.Currency != "" && p.TaxTreatment.Valid()
}

// ContractorInvoiceDefinition returns the contractor workflow. Contractor
// duration or milestone evidence is submitted by the contractor, validated
// against the approved SOW, approved by the client, handed to AP, and
// reconciled against the AP authority. A bounded reopen exception can repeat
// the evidence and approval path without changing the original history.
func ContractorInvoiceDefinition(p Params) workflow.Definition {
	p = p.normalized()
	k := kit{workflowID: ContractorWorkflowID, purpose: "TIME_CONTRACTOR_INVOICE",
		classification: "CONFIDENTIAL_CONTRACTOR_FINANCE", manifest: "data-access.time.contractor_invoice/v1",
		subjectBrand: "ContractorInvoiceKey", params: p}

	invalid := []string{ContractorRouteMissingSOW, ContractorRouteRateOutsideSOW,
		ContractorRouteCurrencyMissing, ContractorRouteTaxTreatmentMiss, ContractorRouteEntryInvalid}
	nodes := []workflow.Node{
		k.capability(NodeContractorOpenPeriod, CapContractorOpenPeriod, workflow.RoleAuthoritativeCore, NodeContractorEndRepair, contractorOutput(k)),
		k.signal(NodeContractorAwaitEntry, EventTypeContractorEntry, CorrelationContractor, p.PeriodCloseAfterSeconds,
			NodeContractorEndRepair, GapTypedSignal, SourceContractorPortal),
		k.capability(NodeContractorRecordEntry, CapContractorRecordEntry, workflow.RoleAuthoritativeCore, NodeContractorEndRepair, contractorOutput(k)),
		k.decision(NodeContractorValidateSOW, RuleContractorValidateSOW,
			append(invalid, ContractorRouteValid), nil, nil),
		k.task(NodeContractorReviewEntry, FormReviewContractorEntry, "ClientApproverFor(contractor_engagement)"),
		k.approval(NodeContractorApproveEntry, "approval.time.contractor_entry/v1"),
		k.capability(NodeContractorSubmitToAP, CapContractorSubmitToAP, workflow.RoleDownstreamEffect, NodeContractorEndRepair, contractorOutput(k)),
		contractorObservation(k),
		k.signal(NodeContractorAwaitReopen, EventTypeContractorReopen, CorrelationContractor, p.ReopenWindowSeconds,
			NodeContractorEndRepair, GapTypedSignal, SourceContractorPortal),
		k.end(NodeContractorEndSubmitted, TerminalContractorInvoice, workflow.RuntimeCompleted, dimsClosed(),
			endOpts{receipt: "receipt.time.contractor_invoice/v1", obligations: []string{"obligation.time.contractor_invoice_acceptance"}}),
		k.end(NodeContractorEndMissing, TerminalContractorMissing, workflow.RuntimeBlocked, dimsBlocked(),
			endOpts{repair: "repair.time.contractor_missing_entry/v1"}),
		k.end(NodeContractorEndHeld, TerminalContractorHeld, workflow.RuntimeCompleted, dimsHeld(),
			endOpts{receipt: "receipt.time.contractor_invoice/v1", obligations: []string{"obligation.time.contractor_entry_correction"}}),
		k.end(NodeContractorEndRepair, TerminalContractorRepair, workflow.RuntimeRepairRequired, dimsRepair(),
			endOpts{repair: "repair.time.contractor_invoice/v1"}),
		k.end(NodeContractorEndCancelled, TerminalContractorCancel, workflow.RuntimeCancelled, dimsCancelled(), endOpts{}),
	}

	edges := capabilityEdges(NodeContractorOpenPeriod, NodeContractorAwaitEntry, NodeContractorEndRepair)
	edges = append(edges,
		edge(NodeContractorAwaitEntry, NodeContractorRecordEntry, string(workflow.OutcomeSucceeded)),
		edge(NodeContractorAwaitEntry, NodeContractorEndMissing, "TIMED_OUT"),
		edge(NodeContractorAwaitEntry, NodeContractorEndCancelled, "CANCELLED"))
	edges = append(edges, capabilityEdges(NodeContractorRecordEntry, NodeContractorValidateSOW, NodeContractorEndRepair)...)
	edges = append(edges, edge(NodeContractorValidateSOW, NodeContractorReviewEntry, ContractorRouteValid))
	edges = append(edges, routesTo(NodeContractorValidateSOW, NodeContractorEndHeld, invalid...)...)
	edges = append(edges,
		edge(NodeContractorValidateSOW, NodeContractorEndRepair, string(workflow.OutcomeUnknown)),
		edge(NodeContractorReviewEntry, NodeContractorApproveEntry, string(workflow.OutcomeSucceeded)),
		edge(NodeContractorReviewEntry, NodeContractorEndHeld, string(workflow.OutcomeRejected)),
		edge(NodeContractorReviewEntry, NodeContractorEndHeld, "EXPIRED"),
		edge(NodeContractorReviewEntry, NodeContractorEndCancelled, "CANCELLED"),
		edge(NodeContractorApproveEntry, NodeContractorSubmitToAP, "APPROVED"),
		edge(NodeContractorApproveEntry, NodeContractorEndHeld, string(workflow.OutcomeRejected)),
		edge(NodeContractorApproveEntry, NodeContractorEndHeld, "EXPIRED"),
		edge(NodeContractorApproveEntry, NodeContractorEndHeld, "INVALIDATED"),
		edge(NodeContractorApproveEntry, NodeContractorEndCancelled, "CANCELLED"))
	edges = append(edges, capabilityEdges(NodeContractorSubmitToAP, NodeContractorObserveAP, NodeContractorEndRepair)...)
	edges = append(edges, observeEdges(NodeContractorObserveAP, NodeContractorAwaitReopen, NodeContractorEndRepair)...)
	edges = append(edges,
		edge(NodeContractorAwaitReopen, NodeContractorValidateSOW, string(workflow.OutcomeSucceeded)),
		edge(NodeContractorAwaitReopen, NodeContractorEndSubmitted, "TIMED_OUT"),
		edge(NodeContractorAwaitReopen, NodeContractorEndCancelled, "CANCELLED"))

	def := k.definition(ContractorVersion, "Contractor time to invoice", IntentContractorTime, NodeContractorOpenPeriod, nodes, edges)
	def.ApprovalRequirements = []workflow.ApprovalRequirement{
		approvalRequirement("approval.time.contractor_entry/v1", "ClientApproverFor(contractor_engagement)", p),
	}
	def.Obligations = []workflow.ObligationRequirement{
		obligation("obligation.time.contractor_invoice_acceptance", "Observe AP acceptance or rejection", "accounts_payable"),
		obligation("obligation.time.contractor_entry_correction", "Resolve a rejected contractor entry", "contractor_engagement"),
	}
	def.Limits = workflow.Limits{MaxFanOut: 10, MaxDepth: 16, MaxNodes: 16,
		DeclaredCycles: []workflow.CycleDeclaration{{EntryNodeID: NodeContractorValidateSOW, GuardNodeID: NodeContractorValidateSOW, MaxIterations: p.MaxPeriodPasses}}}
	return def
}

func contractorOutput(k kit) workflow.Field {
	return workflow.Field{Path: "subject_key", Type: k.subjectType()}
}

func contractorObservation(k kit) workflow.Node {
	n := k.observe(NodeContractorObserveAP, CapContractorObserveAP, "accounts_payable", NodeContractorEndRepair)
	n.Outputs = []workflow.Field{contractorOutput(k)}
	return n
}
