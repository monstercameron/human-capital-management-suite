package operator

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe/observetest"
)

// operatorUninstrumentedByDesign lists the operator gateway's exported,
// context-taking functions that open no observe operation, with the reason.
var operatorUninstrumentedByDesign = map[string]string{
	"journal.go MemoryJournal.Lookup":                   "in-memory journal inside the instrumented Gateway.Submit",
	"journal.go MemoryJournal.Begin":                    "in-memory journal inside the instrumented Gateway.Submit",
	"journal.go MemoryJournal.Complete":                 "in-memory journal inside the instrumented Gateway.Submit",
	"journal.go MemoryJournal.Abort":                    "in-memory journal inside the instrumented Gateway.Submit",
	"operator.go ExecutorFunc.Apply":                    "function adapter; its executor is instrumented",
	"workflowcontrol/production.go PlanSet.ResolvePlan": "in-memory digest lookup inside an instrumented control",
}

// TestOperatorOperationsAreInstrumented holds the operator gateway and the
// governed workflow controls to the workflow engine's telemetry rule.
func TestOperatorOperationsAreInstrumented(t *testing.T) {
	found, err := observetest.Uninstrumented(".", nil)
	if err != nil {
		t.Fatal(err)
	}
	missing, stale := observetest.CheckExemptions(found, operatorUninstrumentedByDesign)
	for _, key := range missing {
		t.Errorf("%s takes a context but opens no observe operation; bracket it with observe.Begin or document why not", key)
	}
	for _, key := range stale {
		t.Errorf("exemption %q no longer matches an uninstrumented function; remove it", key)
	}
}
