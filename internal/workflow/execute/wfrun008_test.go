package execute

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// TestResumeRefusesWhileTheInstanceIsPaused is WF-RUN-008's driver-facing
// half: a governed pause stops the driver, not only a direct
// [runtime.Advance] caller.
//
// The refusal is not implemented here. [runtime.Advance] holds the one pause
// gate, inside the same fenced transaction as the advancement it guards, and
// this driver surfaces its typed refusal unchanged -- which is exactly the
// property worth pinning: a second, driver-local pause check could drift out
// of agreement with the runtime's, and there is none.
func TestResumeRefusesWhileTheInstanceIsPaused(t *testing.T) {
	f := newWfrun028Fixture(t, "paused")

	var paused runtime.PauseReceipt
	work006Tx(t, f.conn, f.tenantID, func(tx dbport.Tx) error {
		var pauseErr error
		paused, pauseErr = runtime.RequestPause(context.Background(), tx, runtime.PauseRequest{
			TenantID: f.tenantID, InstanceID: f.instanceID,
			ExpectedInstanceVersion: f.instanceVersion,
			Plan:                    f.plan,
			Reason:                  "INCIDENT_REVIEW",
			RequestedBy:             "principal:operations-duty",
			RequestedAt:             f.at,
		})
		return pauseErr
	})
	if paused.Status != runtime.InstancePaused {
		t.Fatalf("pause at a parked safe point produced %s, want PAUSED", paused.Status)
	}

	req := f.resumeRequest()
	req.ExpectedInstanceVersion = paused.InstanceVersion
	_, err := f.driver(t).Resume(context.Background(), req)
	if runtime.CodeOf(err) != runtime.CodeInstancePaused {
		t.Fatalf("Resume of a paused instance: code = %q, want %q (%v)",
			runtime.CodeOf(err), runtime.CodeInstancePaused, err)
	}

	// And the refusal changed nothing: the work item is untouched and the
	// instance is still exactly where the pause left it.
	var inst runtime.Instance
	work006Tx(t, f.conn, f.tenantID, func(tx dbport.Tx) error {
		var loadErr error
		inst, loadErr = (runtime.Store{}).LoadInstance(context.Background(), tx, f.tenantID, f.instanceID)
		return loadErr
	})
	if inst.RuntimeStatus != runtime.InstancePaused || inst.InstanceVersion != paused.InstanceVersion {
		t.Fatalf("after a refused resume the instance is %s at version %d, want PAUSED at %d",
			inst.RuntimeStatus, inst.InstanceVersion, paused.InstanceVersion)
	}
}

// TestResumeAppliesAPendingPauseAtItsSafePoint is WF-RUN-008's driver-owned
// transition: an instance whose pause was requested while it could not stop
// reaches a safe point on the next advancement, and the driver applies the
// pause there instead of surfacing a refusal that would leave the instance
// PAUSE_REQUESTED forever.
func TestResumeAppliesAPendingPauseAtItsSafePoint(t *testing.T) {
	f := newWfrun028Fixture(t, "pause-pending")

	var requested runtime.Instance
	work006Tx(t, f.conn, f.tenantID, func(tx dbport.Tx) error {
		store := runtime.Store{}
		inst, err := store.LoadInstance(context.Background(), tx, f.tenantID, f.instanceID)
		if err != nil {
			return err
		}
		requested, err = store.RecordInstanceState(context.Background(), tx, runtime.InstanceTransition{
			TenantID: f.tenantID, InstanceID: f.instanceID, ExpectedVersion: inst.InstanceVersion,
			Status: runtime.InstancePauseRequested, CurrentNodeIDs: inst.CurrentNodeIDs,
			VariableRevisionHead: inst.VariableRevisionHead, EffectiveContextRef: inst.EffectiveContextRef,
			LastCheckpointRef: inst.LastCheckpointRef, CompletionDimensions: inst.CompletionDimensions,
		})
		return err
	})

	req := f.resumeRequest()
	req.ExpectedInstanceVersion = requested.InstanceVersion
	result, err := f.driver(t).Resume(context.Background(), req)
	if err != nil {
		t.Fatalf("Resume at a safe point with a pause pending: %v", err)
	}
	if result.Status != StatusPaused || result.InstanceVersion <= requested.InstanceVersion {
		t.Fatalf("Resume result = %+v, want PAUSED past version %d", result, requested.InstanceVersion)
	}

	var inst runtime.Instance
	work006Tx(t, f.conn, f.tenantID, func(tx dbport.Tx) error {
		var loadErr error
		inst, loadErr = (runtime.Store{}).LoadInstance(context.Background(), tx, f.tenantID, f.instanceID)
		return loadErr
	})
	if inst.RuntimeStatus != runtime.InstancePaused || inst.InstanceVersion != result.InstanceVersion {
		t.Fatalf("instance is %s at version %d, want PAUSED at %d", inst.RuntimeStatus, inst.InstanceVersion, result.InstanceVersion)
	}
}

func TestSettlePauseLeavesOtherErrorsUnchanged(t *testing.T) {
	cause := errors.New("storage unavailable")
	if got := (&Driver{}).settlePause(context.Background(), runContext{}, time.Time{}, cause); got != cause {
		t.Fatalf("settlePause rewrote a non-pause error: %v", got)
	}
	if _, ok := pausedResult(cause, Result{}); ok {
		t.Fatal("pausedResult accepted an error that is not a settled pause")
	}
	paused := &pausedAtSafePoint{receipt: runtime.PauseReceipt{InstanceVersion: 7, Frontier: []string{"approve"}}}
	r, ok := pausedResult(fmt.Errorf("wrapped: %w", paused), Result{})
	if !ok || r.Status != StatusPaused || r.InstanceVersion != 7 || len(r.Frontier) != 1 || paused.Error() == "" {
		t.Fatalf("pausedResult = %+v, %v", r, ok)
	}
}

// TestStepInputsRefusesANodeThatDoesNotAdmitTheRunMode is WF-RUN-040's
// driver-side gate: the step handler never runs for a node whose compiled
// effect class does not admit the run's execution mode.
func TestStepInputsRefusesANodeThatDoesNotAdmitTheRunMode(t *testing.T) {
	node := workflow.CompiledNode{ID: "commit", AllowedModes: []workflow.ExecutionMode{workflow.ModeExecute}}
	run := runContext{start: runtime.StartRequest{ExecutionMode: workflow.ModeSimulate}}
	_, err := (&Driver{}).stepInputs(context.Background(), run, StepRequest{Node: node}, 1)
	if !errors.Is(err, ErrModeNotAllowed) {
		t.Fatalf("stepInputs in SIMULATE for an EXECUTE-only node = %v, want ErrModeNotAllowed", err)
	}
}
