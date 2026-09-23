package execution

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe/observetest"
)

// executionUninstrumentedByDesign lists the execution host's exported,
// context-taking functions that open no observe operation, with the reason.
var executionUninstrumentedByDesign = map[string]string{
	// The telemetry seams themselves.
	"observe_recorder.go ObserveRecorder.Start":                       "the recorder seam",
	"telemetry.go OTelInstrumentation.TraceID":                        "instrumentation seam",
	"telemetry.go OTelInstrumentation.StartNodeSpan":                  "instrumentation seam",
	"telemetry.go OTelInstrumentation.StartAdvanceSpan":               "instrumentation seam",
	"telemetry.go OTelInstrumentation.StartTerminalSpan":              "instrumentation seam",
	"telemetry.go OTelInstrumentation.StartResumeSpan":                "instrumentation seam",
	"progress_observer.go ProgressObserver.StartSweep":                "progress telemetry seam (span and log)",
	"progress_observer.go ProgressObserver.Stuck":                     "progress telemetry seam (log)",
	"workload_observer.go WorkloadObserver.Observe":                   "workload telemetry seam (span and log)",
	"evidence.go capabilityEvidenceAdapter.RecordExecutionEvidence":   "evidence sink write inside the instrumented driver advance",
	"evidence.go capabilityEvidenceAdapter.RecordExecutionEvidenceTx": "evidence sink write inside the instrumented driver advance",

	// Pure delegation to the instrumented execution driver.
	"execution.go executeDriverAdapter.Execute":        "delegates to the instrumented driver",
	"execution.go executeDriverAdapter.Resume":         "delegates to the instrumented driver",
	"timer_resume.go executeDriverAdapter.ResumeTimer": "delegates to the instrumented driver",
	"retry_admission.go RetryAdmission.OnRetry":        "delegates to the instrumented ConsumeRetry",
	"promotionterminal/writer.go ResolverFunc.Resolve": "function adapter",
	"scheduler/dispatch.go DispatcherFunc.Dispatch":    "function adapter",

	// WF-RUN-005: the signal driver adapter and the resumer function adapter.
	"signal_resume.go executeDriverAdapter.ResumeSignal":          "delegates to the instrumented driver",
	"scheduler/signal_dispatch.go SignalResumerFunc.ResumeSignal": "function adapter",

	// The expiry sweeper: the timeout driver adapter and the expirer
	// function adapter.
	"signal_resume.go executeDriverAdapter.ResumeSignalTimeout":          "delegates to the instrumented driver",
	"scheduler/signal_dispatch.go SignalExpirerFunc.ResumeExpiredSignal": "function adapter",

	// WF-STEP-018: the approval kernel adapter and its authority port adapter.
	"approval_kernel.go executeDriverAdapter.CompleteApproval":   "delegates to the instrumented driver",
	"approval_kernel.go executeDriverAdapter.InvalidateApproval": "delegates to the instrumented driver",
	"approval_kernel.go recheckAuthority.Recheck":                "function adapter inside the instrumented approval completion",

	// WF-RUN-003: the recovery role redelivers through the instrumented driver.
	"ready_redelivery.go executeDriverAdapter.RedeliverReady": "delegates to the instrumented driver",

	// Reads inside an instrumented operation.
	"approver_routing.go JourneyWorkerManagers.CurrentManagerOf": "manager lookup inside the instrumented work-item routing",
	"execution.go managerFallback.CurrentManagerOf":              "manager lookup inside the instrumented work-item routing",

	// The loop: every Tick it runs is instrumented and a failed tick is logged.
	"scheduler/scheduler.go Scheduler.Run": "poll loop over the instrumented Tick; logs tick failures",

	// WF-REV-002: the served compensation composes the compensate executor
	// into the instrumented advance, discharge and governed transactions.
	// Its entry points and compensate-port adapters run inside the caller's
	// span and open none of their own.
	"compensate_serve.go PromotionExecution.CancelGoverned":         "delegates to the instrumented driver with the served compensator",
	"compensate_serve.go ServedCompensation.Compensate":             "discharge port inside the instrumented discharge transaction",
	"compensate_serve.go ServedCompensation.Discharge":              "drains through the instrumented discharge transaction",
	"compensate_serve.go ServedCompensation.ReleaseHoldViaExecutor": "COMPENSATE node inside the instrumented advance transaction",
	"compensate_serve.go WithCompensationIntent":                    "context key helper; no operation",
	"compensate_serve.go recordingLedger.AppendCompensation":        "ledger port inside the caller's span",
	"compensate_serve.go servedAuthorizer.Authorize":                "authorizer port inside the caller's span",
	"compensate_serve.go servedHoldCapability.Compensate":           "capability port inside the caller's span",
	"compensate_serve.go servedHoldCapability.Manifest":             "capability manifest read; no operation",
	"compensate_serve.go servedHoldObserver.ObserveCompensation":    "observer port inside the caller's span",
	"compensate_serve.go servedOperations.Complete":                 "operation port inside the caller's span",
	"compensate_serve.go servedOperations.RecordEffect":             "operation port inside the caller's span",
	"compensate_serve.go servedOperations.Reserve":                  "operation port inside the caller's span",

	// REV-010-01: the served RULE-004 wiring. Both run inside the
	// instrumented driver's own advance transaction and span, and open
	// none of their own.
	"promotionsteps/promotionsteps.go Runner.RunInTx": "threshold node inside the instrumented advance transaction",
	"rule_facts.go ServedRuleFacts.Lookup":            "rule facts read inside the instrumented currency check",
}

// TestExecutionHostOperationsAreInstrumented holds the execution host to the
// same rule as the workflow engine: every exported context-taking operation
// brackets itself with an observe operation or documents why not.
func TestExecutionHostOperationsAreInstrumented(t *testing.T) {
	found, err := observetest.Uninstrumented(".", nil)
	if err != nil {
		t.Fatal(err)
	}
	missing, stale := observetest.CheckExemptions(found, executionUninstrumentedByDesign)
	for _, key := range missing {
		t.Errorf("%s takes a context but opens no observe operation; bracket it with observe.Begin or document why not", key)
	}
	for _, key := range stale {
		t.Errorf("exemption %q no longer matches an uninstrumented function; remove it", key)
	}
}
