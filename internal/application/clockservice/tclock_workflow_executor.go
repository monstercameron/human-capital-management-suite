package clockservice

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
)

// PunchReceiptReader reads the committed authoritative punch after the first
// workflow node. It is the recovery boundary for a start whose response was
// interrupted after the time-plane commit.
type PunchReceiptReader interface {
	LoadPunchResult(context.Context, string, string) (PunchResult, bool, error)
}

// WorkflowDriverFactory builds one real execute.Driver with a request-scoped
// PunchCommitStepRunner and the caller's registered step handlers.
type WorkflowDriverFactory func(context.Context, string, PunchWork) (WorkflowRuntime, error)

// WorkflowPunchDriver executes the synchronous first node through the real
// runtime driver and resolves ambiguous starts from durable punch evidence.
type WorkflowPunchDriver struct {
	Factory   WorkflowDriverFactory
	Adapter   *TimeClockRuntimeAdapter
	Resolver  TenantResolver
	Bindings  RunBindingStore
	Receipts  PunchReceiptReader
	Evidence  PunchNodeEvidenceReader
	Telemetry *ClockWorkflowTelemetry
	Admission *ClockWorkflowAdmission
}

// TenantResolver maps canonical tenant keys to runtime UUIDs.
type TenantResolver func(string) (uuid.UUID, error)

// ExecutePunch starts the registered workflow and returns only after the
// authoritative punch receipt is durably observable.
func (d WorkflowPunchDriver) ExecutePunch(ctx context.Context, tenant string, work PunchWork) (WorkflowPunchResult, error) {
	if d.Factory == nil || d.Adapter == nil || d.Resolver == nil || d.Bindings == nil || d.Receipts == nil || d.Evidence == nil || d.Telemetry.Validate() != nil || strings.TrimSpace(tenant) == "" || work.Observation.ID == "" || work.Session.ID == "" || work.Observation.TenantID != tenant || work.Session.TenantID != tenant {
		return WorkflowPunchResult{}, errors.New("clock workflow executor: incomplete composition")
	}
	if d.Admission != nil {
		release, err := d.Admission.Admit(ctx, tenant, clockWorkflowPriority(work))
		if err != nil {
			return WorkflowPunchResult{}, err
		}
		defer release()
	}
	result, found, err := d.Receipts.LoadPunchResult(ctx, tenant, work.Observation.ID)
	if err != nil {
		return WorkflowPunchResult{}, err
	}
	if found {
		if result.Session.TenantID != tenant || result.Session.ID != work.Session.ID || result.Observation.TenantID != tenant || result.Observation.ID != work.Observation.ID || result.Observation.Digest != work.Observation.Digest {
			return WorkflowPunchResult{}, errors.New("clock workflow executor: replayed punch content does not match committed receipt")
		}
		binding, err := d.validBinding(ctx, tenant, work.Session.ID)
		if err == nil {
			evidence, proven, readErr := d.Evidence.LoadPunchNodeEvidence(ctx, binding.TenantID, binding.InstanceID, work.Observation.ID)
			if readErr != nil {
				return WorkflowPunchResult{}, readErr
			}
			if proven {
				if !validPunchNodeEvidence(evidence, binding, work) {
					return WorkflowPunchResult{}, errors.New("clock workflow executor: replayed engine evidence does not match committed punch")
				}
				return d.provenResult(ctx, binding, work, result)
			}
		}
		// The time-plane effect may have committed before the engine's
		// advancement or binding did. Re-enter the same idempotent runtime
		// request to recover its tracking before acknowledging the punch.
	}
	runtimeDriver, err := d.Factory(ctx, tenant, work)
	if err != nil {
		return WorkflowPunchResult{}, err
	}
	if runtimeDriver == nil {
		return WorkflowPunchResult{}, errors.New("clock workflow executor: nil runtime driver")
	}
	tenantID, err := d.Resolver(tenant)
	if err != nil || tenantID == uuid.Nil {
		return WorkflowPunchResult{}, errors.New("clock workflow executor: invalid tenant")
	}
	adapter := *d.Adapter
	adapter.Runtime = runtimeDriver
	adapter.Bindings = d.Bindings
	adapter.ResolveTenant = d.Resolver
	if err := executeSessionPunch(ctx, adapter, tenant, work); err != nil {
		return WorkflowPunchResult{}, err
	}
	binding, err := d.validBinding(ctx, tenant, work.Session.ID)
	if err != nil {
		return WorkflowPunchResult{}, err
	}
	committed, found, err := d.Receipts.LoadPunchResult(ctx, tenant, work.Observation.ID)
	if err != nil {
		return WorkflowPunchResult{}, err
	}
	if !found {
		return WorkflowPunchResult{}, errors.New("clock workflow executor: first node returned without durable punch receipt")
	}
	return d.provenResult(ctx, binding, work, committed)
}

func (d WorkflowPunchDriver) provenResult(ctx context.Context, binding TimeClockRunBinding, work PunchWork, result PunchResult) (WorkflowPunchResult, error) {
	if result.Session.TenantID != work.Session.TenantID || result.Session.ID != work.Session.ID || result.Observation.TenantID != work.Observation.TenantID || result.Observation.ID != work.Observation.ID || result.Observation.Digest != work.Observation.Digest {
		return WorkflowPunchResult{}, errors.New("clock workflow executor: punch receipt identity mismatch")
	}
	evidence, found, err := d.Evidence.LoadPunchNodeEvidence(ctx, binding.TenantID, binding.InstanceID, work.Observation.ID)
	if err != nil {
		return WorkflowPunchResult{}, err
	}
	if !found || !validPunchNodeEvidence(evidence, binding, work) {
		return WorkflowPunchResult{}, errors.New("clock workflow executor: completed engine attempt is not proven")
	}
	if d.Telemetry == nil {
		return WorkflowPunchResult{}, errors.New("clock workflow executor: telemetry is required")
	}
	if err := d.Telemetry.RecordAccepted(ctx, ClockWorkflowLink{
		Source: "WORKER_SELF", TenantID: binding.TenantKey, SessionID: work.Session.ID,
		ObservationID: work.Observation.ID, InstanceID: binding.InstanceID.String(), WorkflowID: binding.WorkflowID,
		PlanDigest: binding.PlanDigest, CorrelationID: binding.CorrelationID, TraceID: evidence.TraceID,
		NodeID: evidence.NodeID, Attempt: evidence.Attempt, Outcome: evidence.CompletedState,
	}); err != nil {
		return WorkflowPunchResult{}, err
	}
	return WorkflowPunchResult{PunchResult: result, InstanceID: binding.InstanceID, WorkflowID: binding.WorkflowID, PlanDigest: binding.PlanDigest, StartKey: binding.StartKey, TraceID: evidence.TraceID, NodeID: evidence.NodeID, Attempt: evidence.Attempt, InstanceVersion: evidence.InstanceVersion, Committed: true}, nil
}

func executeSessionPunch(ctx context.Context, adapter TimeClockRuntimeAdapter, tenant string, work PunchWork) error {
	if work.SessionIsNew {
		return adapter.StartRun(ctx, tenant, work.Session.ID, work.Session.WorkerRef, work.Session.AssignmentRef, work.Observation.ID, work.Observation.OccurredAt)
	}
	signal := work.Observation.EventType
	switch signal {
	case "OUT":
		signal = "OUT_PUNCH"
	case "BREAK_START", "BREAK_END", "JOB_TRANSFER", "TRAVEL_TRANSFER":
	default:
		return errors.New("clock workflow executor: unsupported session event")
	}
	return adapter.SignalRun(ctx, tenant, work.Session.ID, signal, work.Observation.ID, work.Observation.OccurredAt)
}

func (d WorkflowPunchDriver) validBinding(ctx context.Context, tenant, session string) (TimeClockRunBinding, error) {
	id, err := d.Resolver(tenant)
	if err != nil || id == uuid.Nil {
		return TimeClockRunBinding{}, errors.New("clock workflow executor: invalid tenant")
	}
	b, err := d.Bindings.Load(ctx, id, session)
	if err != nil {
		return TimeClockRunBinding{}, err
	}
	wantKey := "timeclock:session:" + id.String() + ":" + session
	if b.TenantID != id || b.SessionID != session || b.TenantKey != tenant || b.InstanceID == uuid.Nil || b.WorkflowID == "" || b.PlanDigest == "" || b.StartKey == "" || b.StartKey != wantKey || b.CorrelationID != wantKey {
		return TimeClockRunBinding{}, errors.New("clock workflow executor: incomplete durable binding")
	}
	return b, nil
}
