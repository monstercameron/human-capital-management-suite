package timeclock

import "github.com/monstercameron/human-capital-management-suite/internal/workflow"

// Period timecard identity.
const (
	PeriodWorkflowID     = "hcmnext.workflows.time.period_timecard"
	PeriodVersion        = 1
	IntentSubmitTimecard = "hcmnext.time.submit_timecard/v1"
)

// Period timecard node ids.
const (
	NodeAwaitPeriodTrigger         = "await_period_trigger"
	NodeCollectPeriodInputs        = "collect_period_inputs"
	NodeFoldPeriod                 = "fold_period_inputs"
	NodeComputePeriodPremiums      = "compute_period_premiums"
	NodeAttestTimecard             = "attest_timecard"
	NodeResolvePeriodExceptions    = "resolve_period_exceptions"
	NodePeriodReady                = "period_ready"
	NodeSelectPeriodDestination    = "select_period_destination"
	NodeApproveTimecard            = "approve_timecard"
	NodeLockPeriodTimecard         = "lock_period_timecard"
	NodeDispatchApprovedTime       = "dispatch_approved_time"
	NodeAwaitDestinationAcceptance = "await_destination_acceptance"
	NodeObserveDestination         = "observe_destination"
	NodeAwaitPeriodReopen          = "await_period_reopen"
	NodeClassifyPeriodReopen       = "classify_period_reopen"
	NodeRefoldPeriodReopen         = "refold_period_reopen"
	NodeRecomputePeriodPremiums    = "recompute_period_premiums"
	NodeReattestTimecard           = "reattest_timecard"
	NodeReapproveTimecard          = "reapprove_timecard"
	NodeDispatchReopenedTime       = "dispatch_reopened_time"
	NodeAwaitReopenedAcceptance    = "await_reopened_acceptance"
	NodeObserveReopenedDestination = "observe_reopened_destination"
	NodeEndPeriodClosed            = "end_period_closed"
	NodeEndPeriodRejected          = "end_period_rejected"
	NodeEndPeriodFailed            = "end_period_failed"
	NodeEndPeriodCancelled         = "end_period_cancelled"
	NodeEndPeriodReopenRejected    = "end_period_reopen_rejected"
)

// Period trigger and destination signal vocabulary.
const (
	EventTypePeriodOpened        = "hcmnext.events.time.period_opened"
	EventTypePeriodReopen        = "hcmnext.events.time.period_reopen"
	EventTypeDestinationAccepted = "hcmnext.events.time.destination_accepted"
	CorrelationPeriod            = "subject:time_period"
	CorrelationDestination       = "subject:time_period_destination"
	SourceSchedule               = "time.schedule"
	SourceSessionWorkflow        = "time.session.workflow"
	SourceTimesheetWorkflow      = "time.timesheet.workflow"
	SourceExceptionWorkflow      = "time.exception.workflow"
	SourceDestinationConnector   = "time.destination.connector"
)

// Period decision routes.
const (
	RoutePeriodTriggered       = "PERIOD_TRIGGERED"
	RouteInputsCollected       = "INPUTS_COLLECTED"
	RouteInputsIncomplete      = "INPUTS_INCOMPLETE"
	RoutePeriodReady           = "PERIOD_READY"
	RoutePeriodHasExceptions   = "PERIOD_HAS_EXCEPTIONS"
	RouteDestinationPermitted  = "DESTINATION_PERMITTED"
	RouteDestinationForbidden  = "DESTINATION_FORBIDDEN"
	RouteLateSession           = "LATE_SESSION"
	RoutePeriodReopenRequested = "PERIOD_REOPEN_REQUESTED"
	RoutePayrollLocked         = "PAYROLL_LOCKED"
)

// Period timecard transform and terminal vocabulary.
const (
	TransformFoldPeriod          = "transforms.time.fold_period_inputs/v1"
	ObligationPeriodReopen       = "obligation.time.period_reopen"
	TerminalPeriodClosed         = "TIME_PERIOD_CLOSED"
	TerminalPeriodRejected       = "TIME_PERIOD_REJECTED"
	TerminalPeriodFailed         = "TIME_PERIOD_REPAIR_REQUIRED"
	TerminalPeriodCancelled      = "TIME_PERIOD_CANCELLED"
	TerminalPeriodReopenRejected = "TIME_PERIOD_REOPEN_REJECTED"
	ApprovalPeriodTimecard       = "approval.time.period_timecard/v1"
)

// PeriodTimecardDefinition returns the shared period workflow. It collects
// obligations from session, duration and exception runs, folds them with the
// registered period reducer, obtains worker attestation and manager approval,
// locks the exact aggregate, and completes a governed destination round-trip.
// A late session is a typed reopen signal inside the bounded reopen window;
// it re-enters folding and approval rather than being silently discarded.
func PeriodTimecardDefinition(p Params) workflow.Definition {
	p = p.normalized()
	k := kit{workflowID: PeriodWorkflowID, purpose: "TIME_SUBMIT_TIMECARD", classification: "CONFIDENTIAL_TIME_AND_ATTENDANCE",
		manifest: "data-access.time.period_timecard/v1", subjectBrand: "PeriodTimecardKey", params: p}

	nodes := []workflow.Node{
		k.signal(NodeAwaitPeriodTrigger, EventTypePeriodOpened, CorrelationPeriod, p.PeriodCloseAfterSeconds,
			NodeEndPeriodFailed, GapTypedSignal, SourceSchedule),
		k.capability(NodeCollectPeriodInputs, CapCollectPeriodInputs, "", NodeEndPeriodFailed),
		k.transform(NodeFoldPeriod, TransformFoldPeriod, GapReducingJoin),
		k.capability(NodeComputePeriodPremiums, CapComputePremiums, "", NodeEndPeriodFailed),
		k.task(NodeAttestTimecard, FormAttestTimecard, "worker"),
		k.task(NodeResolvePeriodExceptions, FormResolvePeriodExceptions, "worker"),
		k.decision(NodePeriodReady, RulePeriodReady,
			[]string{RoutePeriodReady, RoutePeriodHasExceptions}, nil, nil),
		k.decision(NodeSelectPeriodDestination, RuleDestinationPermitted,
			[]string{RouteDestinationPermitted, RouteDestinationForbidden}, nil, nil),
		k.approval(NodeApproveTimecard, ApprovalPeriodTimecard),
		k.capability(NodeLockPeriodTimecard, CapLockPeriodTimecard, workflow.RoleAuthoritativeCore, NodeEndPeriodFailed),
		k.capability(NodeDispatchApprovedTime, CapDispatchApprovedTime, workflow.RoleDownstreamEffect, NodeEndPeriodFailed),
		k.signal(NodeAwaitDestinationAcceptance, EventTypeDestinationAccepted, CorrelationDestination,
			p.ReopenWindowSeconds, NodeEndPeriodFailed, GapVendorRoundTrip, SourceDestinationConnector),
		k.observe(NodeObserveDestination, CapObserveDestination, "destination.connector", NodeEndPeriodFailed),
		k.signal(NodeAwaitPeriodReopen, EventTypePeriodReopen, CorrelationPeriod, p.ReopenWindowSeconds,
			NodeEndPeriodFailed, GapTypedSignal, SourceSessionWorkflow, SourceTimesheetWorkflow, SourceExceptionWorkflow),
		k.decision(NodeClassifyPeriodReopen, RulePeriodReady,
			[]string{RouteLateSession, RoutePeriodReopenRequested, RoutePayrollLocked}, nil, nil),
		k.transform(NodeRefoldPeriodReopen, TransformFoldPeriod, GapReducingJoin),
		k.capability(NodeRecomputePeriodPremiums, CapComputePremiums, "", NodeEndPeriodFailed),
		k.task(NodeReattestTimecard, FormAttestTimecard, "worker"),
		k.approval(NodeReapproveTimecard, ApprovalPeriodTimecard),
		k.capability(NodeDispatchReopenedTime, CapDispatchApprovedTime, workflow.RoleDownstreamEffect, NodeEndPeriodFailed),
		k.signal(NodeAwaitReopenedAcceptance, EventTypeDestinationAccepted, CorrelationDestination,
			p.ReopenWindowSeconds, NodeEndPeriodFailed, GapVendorRoundTrip, SourceDestinationConnector),
		k.observe(NodeObserveReopenedDestination, CapObserveDestination, "destination.connector", NodeEndPeriodFailed),
		k.end(NodeEndPeriodClosed, TerminalPeriodClosed, workflow.RuntimeCompleted, dimsClosed(),
			endOpts{receipt: "receipt.time.timecard/v1", obligations: []string{ObligationPeriodReopen}}),
		k.end(NodeEndPeriodRejected, TerminalPeriodRejected, workflow.RuntimeCompleted, dimsRejected(), endOpts{}),
		k.end(NodeEndPeriodFailed, TerminalPeriodFailed, workflow.RuntimeRepairRequired, dimsRepair(),
			endOpts{repair: "repair.time.period_timecard/v1"}),
		k.end(NodeEndPeriodCancelled, TerminalPeriodCancelled, workflow.RuntimeCancelled, dimsCancelled(), endOpts{}),
		k.end(NodeEndPeriodReopenRejected, TerminalPeriodReopenRejected, workflow.RuntimeBlocked, dimsBlocked(),
			endOpts{repair: "repair.time.period_reopen/v1"}),
	}
	for i := range nodes {
		switch nodes[i].ID {
		case NodeCollectPeriodInputs:
			nodes[i].Outputs = periodOutputFields("period_revision", "input_count")
		case NodeComputePeriodPremiums:
			nodes[i].Outputs = periodOutputFields("aggregate_hours", "premium_digest")
		case NodeLockPeriodTimecard:
			nodes[i].Outputs = periodOutputFields("lock_revision")
		case NodeDispatchApprovedTime:
			nodes[i].Outputs = periodOutputFields("dispatch_ref")
		case NodeObserveDestination:
			nodes[i].Outputs = periodOutputFields("destination_status")
		case NodeRecomputePeriodPremiums:
			nodes[i].Outputs = periodOutputFields("aggregate_hours", "premium_digest")
		case NodeDispatchReopenedTime:
			nodes[i].Outputs = periodOutputFields("dispatch_ref")
		case NodeObserveReopenedDestination:
			nodes[i].Outputs = periodOutputFields("destination_status")
		}
	}

	edges := []workflow.Edge{
		edge(NodeAwaitPeriodTrigger, NodeCollectPeriodInputs, string(workflow.OutcomeSucceeded)),
		edge(NodeAwaitPeriodTrigger, NodeEndPeriodFailed, "TIMED_OUT"),
		edge(NodeAwaitPeriodTrigger, NodeEndPeriodCancelled, "CANCELLED"),
	}
	edges = append(edges, capabilityEdges(NodeCollectPeriodInputs, NodeFoldPeriod, NodeEndPeriodFailed)...)
	edges = append(edges,
		edge(NodeFoldPeriod, NodeComputePeriodPremiums, string(workflow.OutcomeSucceeded)),
		edge(NodeFoldPeriod, NodeEndPeriodFailed, string(workflow.OutcomeFailed)),
	)
	edges = append(edges, capabilityEdges(NodeComputePeriodPremiums, NodeAttestTimecard, NodeEndPeriodFailed)...)
	edges = append(edges,
		edge(NodeAttestTimecard, NodePeriodReady, string(workflow.OutcomeSucceeded)),
		edge(NodeAttestTimecard, NodeEndPeriodRejected, string(workflow.OutcomeRejected)),
		edge(NodeAttestTimecard, NodeEndPeriodRejected, "EXPIRED"),
		edge(NodeAttestTimecard, NodeEndPeriodCancelled, "CANCELLED"),
		edge(NodePeriodReady, NodeSelectPeriodDestination, RoutePeriodReady),
		edge(NodePeriodReady, NodeResolvePeriodExceptions, RoutePeriodHasExceptions),
		edge(NodePeriodReady, NodeEndPeriodFailed, string(workflow.OutcomeUnknown)),
		edge(NodeResolvePeriodExceptions, NodeFoldPeriod, string(workflow.OutcomeSucceeded)),
		edge(NodeResolvePeriodExceptions, NodeEndPeriodRejected, string(workflow.OutcomeRejected)),
		edge(NodeResolvePeriodExceptions, NodeEndPeriodRejected, "EXPIRED"),
		edge(NodeResolvePeriodExceptions, NodeEndPeriodCancelled, "CANCELLED"),
		edge(NodeSelectPeriodDestination, NodeApproveTimecard, RouteDestinationPermitted),
		edge(NodeSelectPeriodDestination, NodeEndPeriodRejected, RouteDestinationForbidden),
		edge(NodeSelectPeriodDestination, NodeEndPeriodFailed, string(workflow.OutcomeUnknown)),
		edge(NodeApproveTimecard, NodeLockPeriodTimecard, "APPROVED"),
		edge(NodeApproveTimecard, NodeEndPeriodRejected, string(workflow.OutcomeRejected)),
		edge(NodeApproveTimecard, NodeEndPeriodRejected, "EXPIRED"),
		edge(NodeApproveTimecard, NodeEndPeriodRejected, "INVALIDATED"),
		edge(NodeApproveTimecard, NodeEndPeriodCancelled, "CANCELLED"),
	)
	edges = append(edges,
		edge(NodeAwaitPeriodReopen, NodeClassifyPeriodReopen, string(workflow.OutcomeSucceeded)),
		edge(NodeAwaitPeriodReopen, NodeEndPeriodClosed, "TIMED_OUT"),
		edge(NodeAwaitPeriodReopen, NodeEndPeriodCancelled, "CANCELLED"),
		edge(NodeClassifyPeriodReopen, NodeRefoldPeriodReopen, RouteLateSession),
		edge(NodeClassifyPeriodReopen, NodeRefoldPeriodReopen, RoutePeriodReopenRequested),
		edge(NodeClassifyPeriodReopen, NodeEndPeriodReopenRejected, RoutePayrollLocked),
		edge(NodeClassifyPeriodReopen, NodeEndPeriodFailed, string(workflow.OutcomeUnknown)),
	)
	edges = append(edges,
		edge(NodeRefoldPeriodReopen, NodeRecomputePeriodPremiums, string(workflow.OutcomeSucceeded)),
		edge(NodeRefoldPeriodReopen, NodeEndPeriodFailed, string(workflow.OutcomeFailed)),
	)
	edges = append(edges, capabilityEdges(NodeRecomputePeriodPremiums, NodeReattestTimecard, NodeEndPeriodFailed)...)
	edges = append(edges,
		edge(NodeReattestTimecard, NodeReapproveTimecard, string(workflow.OutcomeSucceeded)),
		edge(NodeReattestTimecard, NodeEndPeriodRejected, string(workflow.OutcomeRejected)),
		edge(NodeReattestTimecard, NodeEndPeriodRejected, "EXPIRED"),
		edge(NodeReattestTimecard, NodeEndPeriodCancelled, "CANCELLED"),
		edge(NodeReapproveTimecard, NodeDispatchReopenedTime, "APPROVED"),
		edge(NodeReapproveTimecard, NodeEndPeriodRejected, string(workflow.OutcomeRejected)),
		edge(NodeReapproveTimecard, NodeEndPeriodRejected, "EXPIRED"),
		edge(NodeReapproveTimecard, NodeEndPeriodRejected, "INVALIDATED"),
		edge(NodeReapproveTimecard, NodeEndPeriodCancelled, "CANCELLED"),
	)
	edges = append(edges, capabilityEdges(NodeDispatchReopenedTime, NodeAwaitReopenedAcceptance, NodeEndPeriodFailed)...)
	edges = append(edges,
		edge(NodeAwaitReopenedAcceptance, NodeObserveReopenedDestination, string(workflow.OutcomeSucceeded)),
		edge(NodeAwaitReopenedAcceptance, NodeEndPeriodFailed, "TIMED_OUT"),
		edge(NodeAwaitReopenedAcceptance, NodeEndPeriodCancelled, "CANCELLED"),
	)
	edges = append(edges, observeEdges(NodeObserveReopenedDestination, NodeEndPeriodClosed, NodeEndPeriodFailed)...)
	edges = append(edges, capabilityEdges(NodeLockPeriodTimecard, NodeDispatchApprovedTime, NodeEndPeriodFailed)...)
	edges = append(edges, capabilityEdges(NodeDispatchApprovedTime, NodeAwaitDestinationAcceptance, NodeEndPeriodFailed)...)
	edges = append(edges,
		edge(NodeAwaitDestinationAcceptance, NodeObserveDestination, string(workflow.OutcomeSucceeded)),
		edge(NodeAwaitDestinationAcceptance, NodeEndPeriodFailed, "TIMED_OUT"),
		edge(NodeAwaitDestinationAcceptance, NodeEndPeriodCancelled, "CANCELLED"),
	)
	edges = append(edges, observeEdges(NodeObserveDestination, NodeAwaitPeriodReopen, NodeEndPeriodFailed)...)

	def := k.definition(PeriodVersion, "Period timecard", IntentSubmitTimecard, NodeAwaitPeriodTrigger, nodes, edges)
	def.ApprovalRequirements = []workflow.ApprovalRequirement{approvalRequirement(ApprovalPeriodTimecard, "ManagerFor(assignment)", p)}
	def.Obligations = []workflow.ObligationRequirement{
		obligation(ObligationPeriodReopen, "A late session reopens the period timecard through a typed reopen", "time.period"),
	}
	def.Limits = workflow.Limits{MaxFanOut: 8, MaxDepth: 36, MaxNodes: 32,
		DeclaredCycles: []workflow.CycleDeclaration{{EntryNodeID: NodeFoldPeriod, GuardNodeID: NodePeriodReady, MaxIterations: p.MaxPeriodPasses}}}
	return def
}

func periodOutputFields(paths ...string) []workflow.Field {
	fields := make([]workflow.Field, 0, len(paths))
	for _, path := range paths {
		kind := workflow.KindString
		switch path {
		case "input_count":
			kind = workflow.KindInteger
		case "aggregate_hours":
			kind = workflow.KindDecimal
		}
		fields = append(fields, workflow.Field{Path: path, Type: workflow.ValueType{Kind: kind}})
	}
	return fields
}
