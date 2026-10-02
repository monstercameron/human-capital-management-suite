package application

import (
	"context"
	"errors"
	"io"
	"net"
	"syscall"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentrunstate"
)

const personaQualityRetryBackoff = 50 * time.Millisecond

func personaQualityTransient(result AgentModelExecutorResult, err error) string {
	if result.Result.Refusal != nil {
		return ""
	}
	if failure := result.Result.Failure; failure != nil {
		if failure.Retryable && failure.Code == agentmodel.FailureTimeout {
			return "timeout"
		}
		if failure.Retryable && failure.Code == agentmodel.FailureUnavailable {
			return "provider"
		}
		return ""
	}
	if errors.Is(err, runstate.ErrLease) {
		return "lease"
	}
	if errors.Is(err, syscall.ECONNRESET) || errors.Is(err, io.ErrUnexpectedEOF) {
		return "network"
	}
	var network net.Error
	if errors.As(err, &network) && network.Timeout() {
		return "timeout"
	}
	return ""
}

func personaQualityBackoff(ctx context.Context, deadline, now time.Time) error {
	if !deadline.After(now.Add(personaQualityRetryBackoff)) {
		return context.DeadlineExceeded
	}
	timer := time.NewTimer(personaQualityRetryBackoff)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// executeQualityModel retries only transient model calls. The second call goes
// through the same budget and egress gateway and is recorded under the lease;
// tool effects and delivery are never replayed by this loop.
func (e *personaAdmittedRunExecutor) executeQualityModel(ctx context.Context, run runstate.Run, request AgentModelExecutorRequest, admissions ...agentrun.Record) (AgentModelExecutorResult, runstate.Run, error) {
	if err := ctx.Err(); err != nil {
		return AgentModelExecutorResult{}, run, err
	}
	if !run.Deadline.After(e.now().UTC()) {
		return AgentModelExecutorResult{}, run, context.DeadlineExceeded
	}
	callCtx, cancel := context.WithTimeout(ctx, run.Deadline.Sub(e.now().UTC()))
	defer cancel()
	result, err := e.executeQualityCall(callCtx, request)
	cause := personaQualityTransient(result, err)
	if cause == "" {
		return result, run, err
	}
	if backoffErr := personaQualityBackoff(callCtx, run.Deadline, e.now().UTC()); backoffErr != nil {
		return result, run, backoffErr
	}
	if cause == "lease" {
		current, readErr := e.store.Get(callCtx, run.ID)
		if readErr != nil {
			return result, run, readErr
		}
		if current.Fence != run.Fence || current.State != runstate.StateRunning {
			return result, run, runstate.ErrLease
		}
		if current.Lease == nil {
			return result, run, runstate.ErrLease
		}
		if !current.Lease.Until.After(e.now().UTC()) {
			current, readErr = e.state.Recover(callCtx, run.ID, current.Version, e.now().UTC())
			if readErr != nil || current.State != runstate.StateReady {
				return result, run, runstate.ErrLease
			}
			current, readErr = e.state.Claim(callCtx, run.ID, e.workerID, e.now().UTC(), e.leaseTTL)
			if readErr != nil {
				return result, run, readErr
			}
		}
		run = current
		if e.work != nil {
			if len(admissions) != 1 || admissions[0].ID != run.AdmissionID {
				return result, run, runstate.ErrLease
			}
			// Rebuild through the trusted work port to bind a fresh security lease
			// and generation. Preserve this turn's already authorized tool result.
			work, buildErr := e.work.BuildPersonaRunModelWork(callCtx, admissions[0], run)
			if buildErr != nil || work.Request.Task.TenantID != run.TenantID || work.Request.Task.TaskID != run.ID || work.Request.Task.AgentID != run.AgentDigest {
				return result, run, runstate.ErrLease
			}
		}
	}
	currentRun := run
	run, err = e.qualityCheckpoint(callCtx, run.ID, e.workerID, run.Fence, run.Version, runstate.PhaseModelCall, 2, request.StepID+":retry", personaRunBytesDigest([]byte(cause)), e.now().UTC())
	if err != nil {
		return result, currentRun, err
	}
	// A distinct admitted step identity accounts for the second provider call.
	request.StepID += ":retry"
	request.Route.TraceID, request.Model.TraceID = request.StepID, request.StepID
	result, err = e.executeQualityCall(callCtx, request)
	return result, run, err
}

type personaQualityCapturedCall struct {
	inner interface {
		Execute(context.Context, AgentModelExecutorRequest) (AgentModelExecutorResult, error)
	}
	result AgentModelExecutorResult
	err    error
}

func (c *personaQualityCapturedCall) Execute(ctx context.Context, request AgentModelExecutorRequest) (AgentModelExecutorResult, error) {
	c.result, c.err = c.inner.Execute(ctx, request)
	return c.result, c.err
}

func (e *personaAdmittedRunExecutor) executeQualityCall(ctx context.Context, request AgentModelExecutorRequest) (AgentModelExecutorResult, error) {
	// The fence's older error wrapper drops the typed provider result and cause.
	// Capture them inside that same fence; no provider call bypasses RunStep.
	if fenced, ok := e.model.(fencedPersonaRunModelExecutor); ok && fenced.inner != nil {
		captured := &personaQualityCapturedCall{inner: fenced.inner}
		fenced.inner = captured
		result, err := fenced.Execute(ctx, request)
		if err != nil && captured.err != nil {
			return captured.result, errors.Join(err, captured.err)
		}
		return result, err
	}
	return e.model.Execute(ctx, request)
}

// A serialization abort has applied no update. A revision conflict is retried
// only when reading proves the expected snapshot and fence are still current.
func (e *personaAdmittedRunExecutor) qualityCheckpoint(ctx context.Context, id, owner string, fence, expected uint64, phase runstate.Phase, attempt uint32, ref, digest string, _ time.Time) (runstate.Run, error) {
	return e.qualityStateChange(ctx, runstate.Run{ID: id, Fence: fence, Version: expected}, func() (runstate.Run, error) {
		return e.state.Checkpoint(ctx, id, owner, fence, expected, phase, attempt, ref, digest, e.now().UTC())
	})
}

func (e *personaAdmittedRunExecutor) qualityStateChange(ctx context.Context, before runstate.Run, change func() (runstate.Run, error)) (runstate.Run, error) {
	run, err := change()
	if err == nil {
		return run, nil
	}
	var database interface{ SQLState() string }
	serialization := errors.As(err, &database) && database.SQLState() == "40001"
	if !serialization && !errors.Is(err, runstate.ErrConflict) && !errors.Is(err, agentrunstate.ErrConflict) {
		return run, err
	}
	current, readErr := e.store.Get(ctx, before.ID)
	if readErr != nil || current.Version != before.Version || current.Fence != before.Fence {
		return current, err
	}
	if backoffErr := personaQualityBackoff(ctx, current.Deadline, e.now().UTC()); backoffErr != nil {
		return current, backoffErr
	}
	run, err = change()
	if err != nil {
		return current, err
	}
	if run.State != runstate.StateRunning {
		return run, nil
	}
	return e.state.Checkpoint(ctx, run.ID, e.workerID, run.Fence, run.Version, runstate.PhaseContext, 2, "state-save-retry", personaRunBytesDigest([]byte("serialization")), e.now().UTC())
}

func personaQualityModelFailureCode(result AgentModelExecutorResult, err error) string {
	if errors.Is(err, runstate.ErrLease) {
		return "ANSWER_INTERRUPTED"
	}
	if errors.Is(err, context.DeadlineExceeded) || personaQualityTransient(result, err) == "timeout" {
		return "MODEL_TIMEOUT"
	}
	if result.Result.Failure != nil && result.Result.Failure.Code == agentmodel.FailureLimit {
		return "MODEL_LIMIT"
	}
	return "MODEL_UNAVAILABLE"
}
