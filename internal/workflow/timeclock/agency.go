package timeclock

import "github.com/monstercameron/human-capital-management-suite/internal/workflow"

// AgencyWorkflowID identifies the agency/VMS time workflow.
const (
	AgencyWorkflowID      = "hcmnext.workflows.time.agency_vms"
	AgencyVersion         = 1
	IntentAgencyTimesheet = "hcmnext.time.submit_agency_timesheet/v1"
)

// Agency workflow node identifiers.
const (
	NodeAgencyOpenTimesheet   = "agency_open_timesheet"
	NodeAgencyAwaitSubmission = "agency_await_submission"
	NodeAgencyReview          = "agency_review_timesheet"
	NodeAgencyApprove         = "agency_client_approval"
	NodeAgencyValidate        = "agency_validate_timesheet"
	NodeAgencyExport          = "agency_export_to_vms"
	NodeAgencyAwaitAcceptance = "agency_await_vms_acceptance"
	NodeAgencyObserve         = "agency_observe_vms_acceptance"
	NodeAgencyEndClosed       = "agency_end_closed"
	NodeAgencyEndMissing      = "agency_end_missing_submission"
	NodeAgencyEndRejected     = "agency_end_rejected"
	NodeAgencyEndReopen       = "agency_end_reopen_required"
	NodeAgencyEndAmbiguous    = "agency_end_ambiguous_handoff"
	NodeAgencyEndRepair       = "agency_end_repair"
	NodeAgencyEndCancelled    = "agency_end_cancelled"
)

// Agency routes are typed outcomes of the validation and acceptance steps.
const (
	RouteAgencyValid        = "AGENCY_TIMESHEET_VALID"
	RouteAgencyInvalid      = "AGENCY_TIMESHEET_INVALID"
	RouteAgencyPeriodClosed = "PERIOD_CLOSED"
	RouteAgencyPeriodOpen   = "PERIOD_OPEN"
	RouteAgencyAcceptance   = "ACCEPTED"
	RouteAgencyRejected     = "REJECTED"
	RouteAgencyAmbiguous    = "AMBIGUOUS"
)

const (
	EventTypeAgencyTimesheet = "hcmnext.events.time.agency_timesheet_submitted"
	CorrelationAgency        = "subject:agency_timesheet"
	ApprovalAgencyTimesheet  = "approval.time.agency_timesheet/v1"
	TerminalAgencyClosed     = "TIME_AGENCY_VMS_ACCEPTED"
	TerminalAgencyMissing    = "TIME_AGENCY_MISSING_SUBMISSION"
	TerminalAgencyRejected   = "TIME_AGENCY_REJECTED"
	TerminalAgencyReopen     = "TIME_AGENCY_REOPEN_REQUIRED"
	TerminalAgencyAmbiguous  = "TIME_AGENCY_AMBIGUOUS_HANDOFF"
	TerminalAgencyRepair     = "TIME_AGENCY_REPAIR_REQUIRED"
	TerminalAgencyCancelled  = "TIME_AGENCY_CANCELLED"
	ObligationAgencyDelivery = "obligation.time.agency_delivery"
)

// AgencyValidationRoutes returns the validation routes in precedence order.
func AgencyValidationRoutes() []string {
	return []string{RouteAgencyPeriodClosed, RouteAgencyInvalid, RouteAgencyValid}
}

// AgencyVMSDefinition returns the agency-temp time workflow. The host opens
// and approves a typed timesheet, then exports it through the registered
// agency/VMS capability and observes acceptance. A timeout, rejection, closed
// period, or ambiguous external result has an explicit terminal route; no
// agency time is ever sent to the host payroll workflow.
func AgencyVMSDefinition(p Params) workflow.Definition {
	p = p.normalized()
	k := kit{workflowID: AgencyWorkflowID, purpose: "TIME_APPROVE_AGENCY_TIMESHEET", classification: "CONFIDENTIAL_TIME_AND_ATTENDANCE",
		manifest: "data-access.time.agency_timesheet/v1", subjectBrand: "AgencyTimesheetKey", params: p}

	nodes := []workflow.Node{
		k.capability(NodeAgencyOpenTimesheet, CapAgencyOpenTimesheet, workflow.RoleAuthoritativeCore, NodeAgencyEndRepair, agencyOutput(k)),
		k.signal(NodeAgencyAwaitSubmission, EventTypeAgencyTimesheet, CorrelationAgency, p.PeriodCloseAfterSeconds,
			NodeAgencyEndRepair, GapTypedSignal, SourceAgencyPortal, SourceVMSConnector, SourceTimesheetPortal),
		k.task(NodeAgencyReview, FormReviewAgencyTimesheet, "HostManagerFor(assignment)"),
		k.approval(NodeAgencyApprove, ApprovalAgencyTimesheet),
		k.decision(NodeAgencyValidate, RuleAgencyValidateTimesheet, AgencyValidationRoutes(), nil, nil),
		k.capability(NodeAgencyExport, CapAgencyExportToVMS, workflow.RoleDownstreamEffect, NodeAgencyEndAmbiguous, agencyOutput(k)),
		k.signal(NodeAgencyAwaitAcceptance, EventTypeAgencyTimesheet+"_response", CorrelationAgency, p.PeriodCloseAfterSeconds,
			NodeAgencyEndAmbiguous, GapVendorRoundTrip, SourceAgencyPortal, SourceVMSConnector),
		agencyObservation(k),
		k.end(NodeAgencyEndClosed, TerminalAgencyClosed, workflow.RuntimeCompleted, dimsClosed(),
			endOpts{receipt: "receipt.time.agency_delivery/v1", obligations: []string{ObligationAgencyDelivery}}),
		k.end(NodeAgencyEndMissing, TerminalAgencyMissing, workflow.RuntimeBlocked, dimsBlocked(),
			endOpts{repair: "repair.time.agency_missing_submission/v1", obligations: []string{ObligationAgencyDelivery}}),
		k.end(NodeAgencyEndRejected, TerminalAgencyRejected, workflow.RuntimeCompleted, dimsRejected(), endOpts{}),
		k.end(NodeAgencyEndReopen, TerminalAgencyReopen, workflow.RuntimeBlocked, dimsBlocked(),
			endOpts{repair: "repair.time.agency_reopen_period/v1", obligations: []string{ObligationAgencyDelivery}}),
		k.end(NodeAgencyEndAmbiguous, TerminalAgencyAmbiguous, workflow.RuntimeBlocked, dimsBlocked(),
			endOpts{repair: "repair.time.agency_durable_handoff/v1", obligations: []string{ObligationAgencyDelivery}}),
		k.end(NodeAgencyEndRepair, TerminalAgencyRepair, workflow.RuntimeRepairRequired, dimsRepair(),
			endOpts{repair: "repair.time.agency_vms/v1"}),
		k.end(NodeAgencyEndCancelled, TerminalAgencyCancelled, workflow.RuntimeCancelled, dimsCancelled(), endOpts{}),
	}

	edges := capabilityEdges(NodeAgencyOpenTimesheet, NodeAgencyAwaitSubmission, NodeAgencyEndRepair)
	edges = append(edges,
		edge(NodeAgencyAwaitSubmission, NodeAgencyReview, string(workflow.OutcomeSucceeded)),
		edge(NodeAgencyAwaitSubmission, NodeAgencyEndMissing, "TIMED_OUT"),
		edge(NodeAgencyAwaitSubmission, NodeAgencyEndCancelled, "CANCELLED"),
		edge(NodeAgencyReview, NodeAgencyApprove, string(workflow.OutcomeSucceeded)),
		edge(NodeAgencyReview, NodeAgencyEndRejected, string(workflow.OutcomeRejected)),
		edge(NodeAgencyReview, NodeAgencyEndRejected, "EXPIRED"),
		edge(NodeAgencyReview, NodeAgencyEndCancelled, "CANCELLED"),
		edge(NodeAgencyApprove, NodeAgencyValidate, "APPROVED"),
		edge(NodeAgencyApprove, NodeAgencyEndRejected, string(workflow.OutcomeRejected)),
		edge(NodeAgencyApprove, NodeAgencyEndRejected, "EXPIRED"),
		edge(NodeAgencyApprove, NodeAgencyEndRejected, "INVALIDATED"),
		edge(NodeAgencyApprove, NodeAgencyEndCancelled, "CANCELLED"),
		edge(NodeAgencyValidate, NodeAgencyEndReopen, RouteAgencyPeriodClosed),
		edge(NodeAgencyValidate, NodeAgencyEndRejected, RouteAgencyInvalid),
		edge(NodeAgencyValidate, NodeAgencyExport, RouteAgencyValid),
		edge(NodeAgencyValidate, NodeAgencyEndAmbiguous, string(workflow.OutcomeUnknown)),
	)
	edges = append(edges, capabilityEdges(NodeAgencyExport, NodeAgencyAwaitAcceptance, NodeAgencyEndAmbiguous)...)
	edges = append(edges,
		edge(NodeAgencyAwaitAcceptance, NodeAgencyObserve, string(workflow.OutcomeSucceeded)),
		edge(NodeAgencyAwaitAcceptance, NodeAgencyEndAmbiguous, "TIMED_OUT"),
		edge(NodeAgencyAwaitAcceptance, NodeAgencyEndCancelled, "CANCELLED"),
	)
	edges = append(edges,
		edge(NodeAgencyObserve, NodeAgencyEndClosed, string(workflow.OutcomePass)),
		edge(NodeAgencyObserve, NodeAgencyEndRejected, string(workflow.OutcomeFail)),
		edge(NodeAgencyObserve, NodeAgencyEndAmbiguous, string(workflow.OutcomePartial)),
		edge(NodeAgencyObserve, NodeAgencyEndAmbiguous, string(workflow.OutcomeUnknown)),
	)

	def := k.definition(AgencyVersion, "Agency VMS time", IntentAgencyTimesheet, NodeAgencyOpenTimesheet, nodes, edges)
	def.ApprovalRequirements = []workflow.ApprovalRequirement{
		approvalRequirement(ApprovalAgencyTimesheet, "HostManagerFor(assignment)", p),
	}
	def.Obligations = []workflow.ObligationRequirement{
		obligation(ObligationAgencyDelivery, "Deliver approved agency time and observe the destination result", "time.agency.vms"),
	}
	def.Limits = workflow.Limits{MaxFanOut: 8, MaxDepth: 12, MaxNodes: 16}
	return def
}

func agencyOutput(k kit) workflow.Field {
	return workflow.Field{Path: "subject_key", Type: k.subjectType()}
}

func agencyObservation(k kit) workflow.Node {
	n := k.observe(NodeAgencyObserve, CapAgencyObserveVMS, "agency.vms", NodeAgencyEndAmbiguous)
	n.Outputs = []workflow.Field{agencyOutput(k)}
	return n
}
