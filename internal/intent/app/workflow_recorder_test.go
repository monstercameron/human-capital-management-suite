package app

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe/observetest"
)

// TestJourneyWorkflowRecorderReachesDirectSteps proves the journey threads the
// cell's workflow recorder into engine steps it runs outside the driver,
// keeps a caller's own recorder, and is inert without one.
func TestJourneyWorkflowRecorderReachesDirectSteps(t *testing.T) {
	ctx := context.Background()
	if (*journeyEngine)(nil).observed(ctx) != ctx || (&journeyEngine{}).observed(ctx) != ctx {
		t.Fatal("a journey without a recorder altered the context")
	}
	var cellRec, callerRec observetest.Recorder
	e := &journeyEngine{recorder: &cellRec}
	_, op := observe.Begin(e.observed(ctx), "workflow.approval.complete")
	op.End(observe.OutcomeSuccess, nil)
	if len(cellRec.Named("workflow.approval.complete")) != 1 {
		t.Fatalf("cell recorder ops = %+v", cellRec.Ops())
	}
	if observe.RecorderFrom(e.observed(callerRec.Context(ctx))) != observe.Recorder(&callerRec) {
		t.Fatal("the journey replaced the caller's recorder")
	}
	if ctrl, ids, err := composeWorkflowControl(nil, nil, nil, &cellRec); ctrl != nil || ids != nil || err != nil {
		t.Fatalf("control without a database = %v, %v, %v", ctrl, ids, err)
	}
}
