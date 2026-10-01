package timeclock

import "github.com/monstercameron/human-capital-management-suite/internal/workflow"

// ExceptionWorkflowID identifies the exception-only capture workflow. It is
// selected only for a validated, genuinely exempt profile whose jurisdiction
// does not impose an affirmative daily-recording duty.
const (
	ExceptionWorkflowID    = "hcmnext.workflows.time.exception_period"
	ExceptionPeriodVersion = 1
	IntentReportExceptions = "hcmnext.time.report_exceptions/v1"
)

// Exception period node identifiers. The prefix keeps these names distinct
// from punch, duration, contractor, agency, and shared-period nodes.
const (
	ExceptionNodeSeedPattern     = "exception_seed_expected_pattern"
	ExceptionNodeAwaitDeviations = "exception_await_deviations"
	ExceptionNodeRecordDeviation = "exception_record_deviation"
	ExceptionNodePrioritize      = "exception_prioritize"
	ExceptionNodeClassify        = "exception_classify"
	ExceptionNodeReview          = "exception_review"
	ExceptionNodeApprove         = "exception_approve"
	ExceptionNodeAwaitReopen     = "exception_await_reopen"
	ExceptionNodeEndAccepted     = "exception_end_accepted"
	ExceptionNodeEndNoDeviation  = "exception_end_no_deviation"
	ExceptionNodeEndRejected     = "exception_end_rejected"
	ExceptionNodeEndReopened     = "exception_end_reopened"
	ExceptionNodeEndFailed       = "exception_end_failed"
	ExceptionNodeEndCancelled    = "exception_end_cancelled"
)

// Exception period routes are deliberately closed vocabulary. A default at
// cutoff is explicit and keeps the unresolved item as an obligation.
const (
	ExceptionRouteNoDeviation       = "NO_DEVIATION"
	ExceptionRouteNeedsReview       = "NEEDS_REVIEW"
	ExceptionRouteReopen            = "REOPEN_REQUIRED"
	ExceptionRouteInvalid           = "INVALID_EXCEPTION"
	ExceptionRouteApproved          = "APPROVED"
	ExceptionRouteDefaultAtCutoff   = "DEFAULT_AT_CUTOFF"
	ExceptionRouteUnresolved        = "UNRESOLVED"
	ExceptionRouteEmployeeConfirmed = "EMPLOYEE_CONFIRMED"
)

// Exception period event and policy vocabulary.
const (
	ExceptionEventTypeDeviation  = "hcmnext.events.time.exception_deviation"
	ExceptionCorrelationPeriod   = "subject:time_exception_period"
	ExceptionTransformPriority   = "transforms.time.prioritize_exceptions/v1"
	ExceptionRuleClassify        = "rules.time.classify_exception_period/v1"
	ExceptionRuleResolution      = "rules.time.exception_resolution/v1"
	ExceptionApprovalResolution  = "approval.time.exception_resolution/v1"
	ExceptionObligationReview    = "obligation.time.exception_review"
	ExceptionObligationReopen    = "obligation.time.exception_reopen"
	ExceptionTerminalAccepted    = "TIME_EXCEPTION_PERIOD_ACCEPTED"
	ExceptionTerminalNoDeviation = "TIME_EXCEPTION_PERIOD_NO_DEVIATION"
	ExceptionTerminalRejected    = "TIME_EXCEPTION_PERIOD_REJECTED"
	ExceptionTerminalReopened    = "TIME_EXCEPTION_PERIOD_REOPENED"
	ExceptionTerminalFailed      = "TIME_EXCEPTION_PERIOD_REPAIR_REQUIRED"
	ExceptionTerminalCancelled   = "TIME_EXCEPTION_PERIOD_CANCELLED"
)

// ExceptionPeriodDefinition returns the typed exception-only period plan.
// The plan records the assumed pattern, accepts typed deviations, prioritizes
// them against cutoff, and requires review plus approval before closure.
// Reopen is an explicit outcome: it preserves the unresolved obligation and
// never overwrites an already accepted period.
func ExceptionPeriodDefinition(p Params) workflow.Definition {
	p = p.normalized()
	k := kit{workflowID: ExceptionWorkflowID, purpose: "TIME_REPORT_EXCEPTIONS",
		classification: "CONFIDENTIAL_TIME_AND_ATTENDANCE",
		manifest:       "data-access.time.exception_period/v1", subjectBrand: "ExceptionPeriodKey", params: p}

	nodes := []workflow.Node{
		k.capability(ExceptionNodeSeedPattern, CapSeedExpectedPattern, workflow.RoleAuthoritativeCore, ExceptionNodeEndFailed,
			workflow.Field{Path: "pattern_ref", Type: plainString()}),
		k.signal(ExceptionNodeAwaitDeviations, ExceptionEventTypeDeviation, ExceptionCorrelationPeriod,
			p.PeriodCloseAfterSeconds, ExceptionNodeEndFailed, GapTypedSignal,
			SourceFirstPartyClock, SourceKiosk, SourceTimesheetPortal, SourcePunchImport),
		k.capability(ExceptionNodeRecordDeviation, CapRecordExceptionLine, workflow.RoleAuthoritativeCore, ExceptionNodeEndFailed,
			workflow.Field{Path: "deviation_ref", Type: plainString()}),
		k.transform(ExceptionNodePrioritize, ExceptionTransformPriority, ""),
		k.decision(ExceptionNodeClassify, ExceptionRuleClassify,
			[]string{ExceptionRouteNoDeviation, ExceptionRouteNeedsReview, ExceptionRouteReopen,
				ExceptionRouteInvalid, ExceptionRouteDefaultAtCutoff, ExceptionRouteUnresolved}, nil, nil),
		k.task(ExceptionNodeReview, FormReviewException, "ManagerFor(assignment)"),
		k.approval(ExceptionNodeApprove, ExceptionApprovalResolution),
		k.signal(ExceptionNodeAwaitReopen, ExceptionEventTypeDeviation, ExceptionCorrelationPeriod,
			p.ReopenWindowSeconds, ExceptionNodeEndFailed, GapTypedSignal, SourceFirstPartyClock, SourceTimesheetPortal),
		k.end(ExceptionNodeEndAccepted, ExceptionTerminalAccepted, workflow.RuntimeCompleted, dimsSubmittedPending(),
			endOpts{receipt: "receipt.time.exception_period/v1", outstanding: []string{ObligationPeriodCollect}, obligations: []string{ObligationPeriodCollect}}),
		k.end(ExceptionNodeEndNoDeviation, ExceptionTerminalNoDeviation, workflow.RuntimeCompleted, dimsClosed(),
			endOpts{receipt: "receipt.time.exception_period/v1"}),
		k.end(ExceptionNodeEndRejected, ExceptionTerminalRejected, workflow.RuntimeCompleted, dimsHeld(),
			endOpts{receipt: "receipt.time.exception_period/v1", outstanding: []string{ExceptionObligationReview}, obligations: []string{ExceptionObligationReview}}),
		k.end(ExceptionNodeEndReopened, ExceptionTerminalReopened, workflow.RuntimeBlocked, dimsBlocked(),
			endOpts{repair: "repair.time.exception_period_reopen/v1", outstanding: []string{ExceptionObligationReopen}, obligations: []string{ExceptionObligationReopen}}),
		k.end(ExceptionNodeEndFailed, ExceptionTerminalFailed, workflow.RuntimeRepairRequired, dimsRepair(),
			endOpts{repair: "repair.time.exception_period/v1"}),
		k.end(ExceptionNodeEndCancelled, ExceptionTerminalCancelled, workflow.RuntimeCancelled, dimsCancelled(), endOpts{}),
	}

	edges := capabilityEdges(ExceptionNodeSeedPattern, ExceptionNodeAwaitDeviations, ExceptionNodeEndFailed)
	edges = append(edges,
		edge(ExceptionNodeAwaitDeviations, ExceptionNodeRecordDeviation, string(workflow.OutcomeSucceeded)),
		edge(ExceptionNodeAwaitDeviations, ExceptionNodeClassify, "TIMED_OUT"),
		edge(ExceptionNodeAwaitDeviations, ExceptionNodeEndCancelled, "CANCELLED"),
	)
	edges = append(edges, capabilityEdges(ExceptionNodeRecordDeviation, ExceptionNodePrioritize, ExceptionNodeEndFailed)...)
	edges = append(edges, edge(ExceptionNodePrioritize, ExceptionNodeClassify, string(workflow.OutcomeSucceeded)))
	edges = append(edges,
		edge(ExceptionNodeClassify, ExceptionNodeEndNoDeviation, ExceptionRouteNoDeviation),
		edge(ExceptionNodeClassify, ExceptionNodeReview, ExceptionRouteNeedsReview),
		edge(ExceptionNodeClassify, ExceptionNodeAwaitReopen, ExceptionRouteReopen),
		edge(ExceptionNodeClassify, ExceptionNodeEndRejected, ExceptionRouteInvalid),
		edge(ExceptionNodeClassify, ExceptionNodeReview, ExceptionRouteDefaultAtCutoff),
		edge(ExceptionNodeClassify, ExceptionNodeReview, ExceptionRouteUnresolved),
		edge(ExceptionNodeClassify, ExceptionNodeEndFailed, string(workflow.OutcomeUnknown)),
		edge(ExceptionNodePrioritize, ExceptionNodeEndFailed, string(workflow.OutcomeFailed)),
		edge(ExceptionNodeReview, ExceptionNodeApprove, string(workflow.OutcomeSucceeded)),
		edge(ExceptionNodeReview, ExceptionNodeEndRejected, string(workflow.OutcomeRejected)),
		edge(ExceptionNodeReview, ExceptionNodeEndRejected, "EXPIRED"),
		edge(ExceptionNodeReview, ExceptionNodeEndCancelled, "CANCELLED"),
		edge(ExceptionNodeApprove, ExceptionNodeEndAccepted, ExceptionRouteApproved),
		edge(ExceptionNodeApprove, ExceptionNodeEndRejected, string(workflow.OutcomeRejected)),
		edge(ExceptionNodeApprove, ExceptionNodeEndRejected, "EXPIRED"),
		edge(ExceptionNodeApprove, ExceptionNodeAwaitReopen, "INVALIDATED"),
		edge(ExceptionNodeApprove, ExceptionNodeEndCancelled, "CANCELLED"),
		edge(ExceptionNodeAwaitReopen, ExceptionNodeEndReopened, string(workflow.OutcomeSucceeded)),
		edge(ExceptionNodeAwaitReopen, ExceptionNodeEndReopened, "TIMED_OUT"),
		edge(ExceptionNodeAwaitReopen, ExceptionNodeEndCancelled, "CANCELLED"),
	)

	def := k.definition(ExceptionPeriodVersion, "Exception-only time period", IntentReportExceptions,
		ExceptionNodeSeedPattern, nodes, edges)
	def.ApprovalRequirements = []workflow.ApprovalRequirement{
		approvalRequirement(ExceptionApprovalResolution, "TimekeeperFor(assignment)", p),
	}
	def.Obligations = []workflow.ObligationRequirement{
		obligation(ObligationPeriodCollect, "The period timecard run collects this exception period", "time.period"),
		obligation(ExceptionObligationReview, "Resolve or document every exception before cutoff", "time.manager"),
		obligation(ExceptionObligationReopen, "Reopen the accepted period through a linked correction", "time.correction"),
	}
	def.Limits = workflow.Limits{MaxFanOut: 8, MaxDepth: 16, MaxNodes: 16,
		DeclaredCycles: []workflow.CycleDeclaration{{EntryNodeID: ExceptionNodeAwaitDeviations, GuardNodeID: ExceptionNodeClassify, MaxIterations: p.MaxPeriodPasses}}}
	return def
}
