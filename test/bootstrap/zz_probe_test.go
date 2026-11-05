package bootstrap_test

// TEMPORARY diagnostic probe for WF-RUN-032 (impl-wf-e). Dumps the full
// SimulateIntent error (grpc status + details) that TestTodo_WF_RUN_032's
// setup swallows. DELETE before finishing.

import (
	"context"
	"testing"

	"google.golang.org/grpc/status"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
)

func TestProbeWFRun032SimulateError(t *testing.T) {
	terminal := &recordingTerminalWriter{}
	c := newExecutionCell(t, terminal)
	seedWorkforce(t, c)
	ctx := c.grpcContext(context.Background())

	created, err := c.grpcIntent.CreateIntent(ctx, promoteWorkerRequest(t, "wf-run-032-probe"))
	if err != nil {
		t.Fatalf("CreateIntent: %v", err)
	}
	intentID := created.GetIntent().GetIntentId()
	t.Logf("created intent %s", intentID)

	_, err = c.grpcIntent.SimulateIntent(ctx, &intentsv1.SimulateIntentRequest{IntentId: intentID})
	if err == nil {
		t.Fatal("SimulateIntent unexpectedly succeeded")
	}
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("non-status error: %v", err)
	}
	t.Logf("code=%s message=%q", st.Code(), st.Message())
	for i, d := range st.Details() {
		t.Logf("detail[%d] %T: %v", i, d, d)
	}
	t.FailNow()
}
