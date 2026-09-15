package execution

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe/observetest"
)

// executionUninstrumentedByDesign lists the execution host's exported,
// context-taking functions that open no observe operation, with the reason.
var executionUninstrumentedByDesign = map[string]string{
	// The telemetry seams themselves.
	"observe_recorder.go ObserveRecorder.Start":                     "the recorder seam",
	"telemetry.go OTelInstrumentation.TraceID":                      "instrumentation seam",
	"telemetry.go OTelInstrumentation.StartNodeSpan":                "instrumentation seam",
	"telemetry.go OTelInstrumentation.StartAdvanceSpan":             "instrumentation seam",
	"telemetry.go OTelInstrumentation.StartTerminalSpan":            "instrumentation seam",
	"telemetry.go OTelInstrumentation.StartResumeSpan":              "instrumentation seam",
	"progress_observer.go ProgressObserver.StartSweep":              "progress telemetry seam (span and log)",
	"progress_observer.go ProgressObserver.Stuck":                   "progress telemetry seam (log)",
	"workload_observer.go WorkloadObserver.Observe":                 "workload telemetry seam (span and log)",
	"evidence.go capabilityEvidenceAdapter.RecordExecutionEvidence": "evidence sink write inside the instrumented driver advance",

	// Pure delegation to the instrumented execution driver.
	"execution.go executeDriverAdapter.Execute":        "delegates to the instrumented driver",
	"execution.go executeDriverAdapter.Resume":         "delegates to the instrumented driver",
	"timer_resume.go executeDriverAdapter.ResumeTimer": "delegates to the instrumented driver",
	"retry_admission.go RetryAdmission.OnRetry":        "delegates to the instrumented ConsumeRetry",
	"promotionterminal/writer.go ResolverFunc.Resolve": "function adapter",
	"scheduler/dispatch.go DispatcherFunc.Dispatch":    "function adapter",

	// A pure step decision with no I/O, run inside the driver's own node span
	// (execute.Instrumentation.StartNodeSpan). It does not call promotionsteps:
	// the served EXECUTE path is still stubbed (WF-RUN-034).
	"execution.go promotionStepRunner.Run": "pure in-memory step decision inside the driver node span",

	// Reads inside an instrumented operation.
	"approver_routing.go JourneyWorkerManagers.CurrentManagerOf": "manager lookup inside the instrumented work-item routing",
	"execution.go managerFallback.CurrentManagerOf":              "manager lookup inside the instrumented work-item routing",

	// The loop: every Tick it runs is instrumented and a failed tick is logged.
	"scheduler/scheduler.go Scheduler.Run": "poll loop over the instrumented Tick; logs tick failures",
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
