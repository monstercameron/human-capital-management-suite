package timeclock

import "github.com/monstercameron/human-capital-management-suite/internal/workflow"

// Duration timesheet identity.
const (
	DurationWorkflowID    = "hcmnext.workflows.time.duration_timesheet"
	DurationVersion       = 1
	IntentSubmitTimesheet = "hcmnext.time.submit_timesheet/v1"
)

// Duration timesheet node ids.
const (
	NodeOpenTimesheet         = "open_timesheet"
	NodeAwaitTimesheetLines   = "await_timesheet_lines"
	NodeRecordLines           = "record_lines"
	NodeValidateLines         = "validate_lines"
	NodeCorrectLines          = "correct_lines"
	NodeTimesheetComplete     = "timesheet_complete"
	NodeFoldTimesheet         = "fold_timesheet"
	NodeEndTimesheetSubmitted = "end_timesheet_submitted"
	NodeEndMissingSubmission  = "end_missing_submission"
	NodeEndLinesHeld          = "end_lines_held"
	NodeEndTimesheetFailed    = "end_timesheet_failed"
	NodeEndTimesheetCancelled = "end_timesheet_cancelled"
)

// Duration line validation routes (DCAA daily entry, taxonomy, limits).
const (
	RouteLinesValid        = "LINES_VALID"
	RouteMissingDailyEntry = "MISSING_DAILY_ENTRY"
	RouteTaxonomyInvalid   = "TAXONOMY_INVALID"
	RouteOverDailyLimit    = "OVER_DAILY_LIMIT"
	RouteLinesRestBreach   = "REST_BREACH"
	RouteLinesMinorHours   = "MINOR_HOURS"
	RoutePeriodOpen        = "PERIOD_OPEN"
	RoutePeriodComplete    = "PERIOD_COMPLETE"
)

// Duration vocabulary.
const (
	EventTypeTimesheetLines    = "hcmnext.events.time.timesheet_lines_submitted"
	CorrelationTimesheet       = "subject:time_timesheet"
	TransformFoldTimesheet     = "transforms.time.fold_timesheet/v1"
	ObligationLineCorrection   = "obligation.time.line_correction"
	TerminalTimesheetSubmitted = "TIME_TIMESHEET_SUBMITTED"
	TerminalMissingSubmission  = "TIME_TIMESHEET_MISSING_SUBMISSION"
	TerminalLinesHeld          = "TIME_TIMESHEET_LINES_HELD"
	TerminalTimesheetFailed    = "TIME_TIMESHEET_REPAIR_REQUIRED"
	TerminalTimesheetCancelled = "TIME_TIMESHEET_CANCELLED"
)

// DurationValidationRoutes lists validate_lines' routes in precedence order.
func DurationValidationRoutes() []string {
	return []string{RouteMissingDailyEntry, RouteTaxonomyInvalid, RouteLinesMinorHours, RouteLinesRestBreach, RouteOverDailyLimit, RouteLinesValid}
}

// DurationTimesheetDefinition returns the duration timesheet template: exempt
// staff, government contractors and grant-funded work that report hours per
// day and project. Lines arrive as the typed timesheet signal; each batch is
// recorded, validated against daily-entry, taxonomy, minor and rest rules and
// either accepted, corrected by the worker, or held. The completed sheet ends
// with the period-collect obligation.
func DurationTimesheetDefinition(p Params) workflow.Definition {
	p = p.normalized()
	k := kit{workflowID: DurationWorkflowID, purpose: "TIME_SUBMIT_TIMESHEET", classification: "CONFIDENTIAL_TIME_AND_ATTENDANCE",
		manifest: "data-access.time.duration_timesheet/v1", subjectBrand: "TimesheetKey", params: p}

	invalid := []string{RouteMissingDailyEntry, RouteTaxonomyInvalid, RouteLinesMinorHours, RouteLinesRestBreach, RouteOverDailyLimit}
	nodes := []workflow.Node{
		k.capability(NodeOpenTimesheet, CapOpenDurationTimesheet, workflow.RoleAuthoritativeCore, NodeEndTimesheetFailed,
			workflow.Field{Path: "subject_key", Type: k.subjectType()}),
		k.signal(NodeAwaitTimesheetLines, EventTypeTimesheetLines, CorrelationTimesheet, p.PeriodCloseAfterSeconds,
			NodeEndTimesheetFailed, GapTypedSignal, SourceTimesheetPortal, SourcePunchImport),
		k.capability(NodeRecordLines, CapRecordDurationLines, workflow.RoleAuthoritativeCore, NodeEndTimesheetFailed,
			workflow.Field{Path: "subject_key", Type: k.subjectType()}),
		k.decision(NodeValidateLines, RuleValidateDurationLines, DurationValidationRoutes(), nil, nil),
		k.task(NodeCorrectLines, FormCorrectDurationLines, "worker"),
		k.decision(NodeTimesheetComplete, RuleTimesheetComplete, []string{RoutePeriodOpen, RoutePeriodComplete}, nil, nil),
		k.transform(NodeFoldTimesheet, TransformFoldTimesheet, GapReducingJoin),
		k.end(NodeEndTimesheetSubmitted, TerminalTimesheetSubmitted, workflow.RuntimeCompleted, dimsSubmittedPending(),
			endOpts{receipt: "receipt.time.timesheet/v1", outstanding: []string{ObligationPeriodCollect}, obligations: []string{ObligationPeriodCollect}}),
		k.end(NodeEndMissingSubmission, TerminalMissingSubmission, workflow.RuntimeBlocked, dimsBlocked(),
			endOpts{repair: "repair.time.timesheet_missing/v1", outstanding: []string{ObligationPeriodCollect}, obligations: []string{ObligationPeriodCollect}}),
		k.end(NodeEndLinesHeld, TerminalLinesHeld, workflow.RuntimeCompleted, dimsHeld(),
			endOpts{receipt: "receipt.time.timesheet/v1", outstanding: []string{ObligationPeriodCollect, ObligationLineCorrection},
				obligations: []string{ObligationPeriodCollect, ObligationLineCorrection}}),
		k.end(NodeEndTimesheetFailed, TerminalTimesheetFailed, workflow.RuntimeRepairRequired, dimsRepair(), endOpts{repair: "repair.time.timesheet/v1"}),
		k.end(NodeEndTimesheetCancelled, TerminalTimesheetCancelled, workflow.RuntimeCancelled, dimsCancelled(), endOpts{}),
	}

	edges := capabilityEdges(NodeOpenTimesheet, NodeAwaitTimesheetLines, NodeEndTimesheetFailed)
	edges = append(edges,
		edge(NodeAwaitTimesheetLines, NodeRecordLines, string(workflow.OutcomeSucceeded)),
		edge(NodeAwaitTimesheetLines, NodeEndMissingSubmission, "TIMED_OUT"),
		edge(NodeAwaitTimesheetLines, NodeEndTimesheetCancelled, "CANCELLED"),
	)
	edges = append(edges, capabilityEdges(NodeRecordLines, NodeValidateLines, NodeEndTimesheetFailed)...)
	edges = append(edges, edge(NodeValidateLines, NodeTimesheetComplete, RouteLinesValid))
	edges = append(edges, routesTo(NodeValidateLines, NodeCorrectLines, invalid...)...)
	edges = append(edges,
		edge(NodeValidateLines, NodeEndTimesheetFailed, string(workflow.OutcomeUnknown)),
		edge(NodeCorrectLines, NodeRecordLines, string(workflow.OutcomeSucceeded)),
		edge(NodeCorrectLines, NodeEndLinesHeld, string(workflow.OutcomeRejected)),
		edge(NodeCorrectLines, NodeEndLinesHeld, "EXPIRED"),
		edge(NodeCorrectLines, NodeEndTimesheetCancelled, "CANCELLED"),
		edge(NodeTimesheetComplete, NodeAwaitTimesheetLines, RoutePeriodOpen),
		edge(NodeTimesheetComplete, NodeFoldTimesheet, RoutePeriodComplete),
		edge(NodeTimesheetComplete, NodeEndTimesheetFailed, string(workflow.OutcomeUnknown)),
		edge(NodeFoldTimesheet, NodeEndTimesheetSubmitted, string(workflow.OutcomeSucceeded)),
		edge(NodeFoldTimesheet, NodeEndTimesheetFailed, string(workflow.OutcomeFailed)),
	)

	def := k.definition(DurationVersion, "Duration timesheet", IntentSubmitTimesheet, NodeOpenTimesheet, nodes, edges)
	def.Obligations = []workflow.ObligationRequirement{
		obligation(ObligationPeriodCollect, "The period timecard run collects this timesheet", "time.period"),
		obligation(ObligationLineCorrection, "Route the held lines to correction", "time.correction"),
	}
	def.Limits = workflow.Limits{MaxFanOut: 8, MaxDepth: 16, MaxNodes: 16,
		DeclaredCycles: []workflow.CycleDeclaration{
			{EntryNodeID: NodeAwaitTimesheetLines, GuardNodeID: NodeTimesheetComplete, MaxIterations: 62},
			{EntryNodeID: NodeRecordLines, GuardNodeID: NodeValidateLines, MaxIterations: 8},
		}}
	return def
}
