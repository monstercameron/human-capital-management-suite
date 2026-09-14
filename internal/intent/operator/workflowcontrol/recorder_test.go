package workflowcontrol

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/operator"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe/observetest"
)

// TestControllerRecorderObservesTransportControls proves a control arriving
// with no recorder in its context -- as a transport call does -- is still
// recorded, together with the operator submission and runtime transition
// beneath it, and that a caller's own recorder is never replaced.
func TestControllerRecorderObservesTransportControls(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	var own observetest.Recorder
	c, err := New(f.conn, operator.NewMemoryJournal(), plans{f.plan}, f.auth, func() time.Time { return fixedNow }, WithRecorder(&own))
	if err != nil {
		t.Fatal(err)
	}
	id, v := f.instance([]string{"approve_manager"})
	r, err := c.Pause(ctx, f.cmd(id, v, "pause-recorded"))
	expect(t, "recorded pause", r, err, OutcomeApplied, "")
	pause := own.Named("workflow.control.pause")
	if len(pause) != 1 || pause[0].Outcome != observe.OutcomeSuccess || len(own.Named("operator.submit")) != 1 || len(own.Ops()) < 3 {
		t.Fatalf("recorded ops = %+v", own.Ops())
	}

	var caller observetest.Recorder
	before := len(own.Ops())
	r, err = c.Resume(caller.Context(ctx), f.cmd(id, r.InstanceVersion, "resume-caller-recorder"))
	expect(t, "resume with caller recorder", r, err, OutcomeApplied, "")
	if len(caller.Named("workflow.control.resume")) != 1 || len(own.Ops()) != before {
		t.Fatalf("caller recorder replaced: caller %+v, controller gained %d", caller.Ops(), len(own.Ops())-before)
	}
	if (*Controller)(nil).observed(ctx) != ctx {
		t.Fatal("nil controller altered the context")
	}
}
