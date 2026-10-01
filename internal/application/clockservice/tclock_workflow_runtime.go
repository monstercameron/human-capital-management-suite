package clockservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/timeprofile"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/clockpunch"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

const timeClockIntentRecordPunch = "hcmnext.time.record_punch/v1"
const timeClockSessionEvent = "hcmnext.events.time.session_punch"
const timeClockSessionCorrelation = "subject:time_session"

// WorkflowRuntime is the real caller-driven runtime seam. A composition root
// supplies execute.Driver; this adapter never implements a second engine.
type WorkflowRuntime interface {
	Execute(context.Context, execute.ExecuteRequest) (execute.Result, error)
	ResumeSignal(context.Context, execute.ResumeSignalRequest) (execute.Result, error)
}

// TimeClockRunBinding is the durable identity needed to resume a session
// run. A binding must be persisted by RunBindings before later signals are
// accepted; an in-memory map is not a supported implementation.
type TimeClockRunBinding struct {
	TenantID       uuid.UUID
	TenantKey      string
	SessionID      string
	WorkerRef      string
	AssignmentRef  string
	InstanceID     uuid.UUID
	WorkflowID     string
	PlanDigest     string
	ProfileID      string
	ProfileVersion uint64
	ProfileDigest  string
	TemplateID     string
	StartKey       string
	CorrelationID  string
	CreatedAt      time.Time
}

// RunBindingStore persists the session-to-runtime identity mapping.
type RunBindingStore interface {
	Save(context.Context, TimeClockRunBinding) error
	Load(context.Context, uuid.UUID, string) (TimeClockRunBinding, error)
}

// SignalDelivery is a typed clock event submitted to the runtime signal
// receive path. The receiver owns the transaction that records the signal,
// match, receipt and continuation.
type SignalDelivery struct {
	TenantID           uuid.UUID
	SessionID          string
	Signal             string
	ObservationID      string
	OccurredAt         time.Time
	EventType          string
	Source             string
	CorrelationKey     string
	CorrelationValue   string
	SchemaRef          string
	IdempotencyKey     string
	Payload            []byte
	ExpectedInstanceID uuid.UUID
}

// SignalReceipt identifies the durable signal and matched subscription.
type SignalReceipt struct {
	SignalID       uuid.UUID
	SubscriptionID uuid.UUID
}

// SignalReceiver is implemented by the platform's durable signals.Store
// adapter. It must deduplicate ObservationID and return the original receipt.
type SignalReceiver interface {
	Receive(context.Context, SignalDelivery) (SignalReceipt, error)
}

// WorkflowInstanceVersionReader reads the current durable runtime version
// after signal admission, so a resume cannot guess its concurrency fence.
type WorkflowInstanceVersionReader interface {
	LoadWorkflowInstanceVersion(context.Context, uuid.UUID, uuid.UUID) (int64, error)
}

// TimeClockRuntimeAdapter starts and signals the published time punch
// workflow through the real runtime driver. Resolver and Versions are the
// active registry; no workflow is selected by a name-only fallback.
type TimeClockRuntimeAdapter struct {
	Runtime  WorkflowRuntime
	Resolver runtime.WorkflowResolver
	Versions version.Store
	// Profiles and PlanRegistry are optional for legacy runtime compositions;
	// when supplied together they make every new session select its plan from
	// the assignment's resolved profile. Signals never re-resolve a profile.
	Profiles       ProfileResolver
	PlanRegistry   *TimeWorkflowPlanRegistry
	TenantKey      func(uuid.UUID) string
	Bindings       RunBindingStore
	Signals        SignalReceiver
	VersionsReader WorkflowInstanceVersionReader
	CellID         string
	Clock          func() time.Time
	SchemaRef      string
	Source         string
	// ResolveTenant maps the caller's canonical tenant key to the database
	// tenant UUID used by the workflow runtime. Tenant keys are not required to
	// be UUID strings (for example, "ironridge").
	ResolveTenant func(string) (uuid.UUID, error)
}

// StartRun starts one trigger-sourced punch-session run and durably binds the
// resulting runtime instance to the clock session.
func (a TimeClockRuntimeAdapter) StartRun(ctx context.Context, tenant, sessionID, workerRef, assignmentRef, observationID string, at time.Time) error {
	if err := a.validate(); err != nil {
		return err
	}
	tenantID, err := a.resolveTenant(tenant)
	if err != nil {
		return err
	}
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(workerRef) == "" || strings.TrimSpace(assignmentRef) == "" || strings.TrimSpace(observationID) == "" {
		return errors.New("clock workflow runtime: session, worker, assignment and observation are required")
	}
	if at.IsZero() {
		return errors.New("clock workflow runtime: occurred time is required")
	}
	when, err := a.serverNow()
	if err != nil {
		return err
	}
	key := startKey(tenantID, sessionID)
	profile, err := a.profileFromStart(ctx, tenant, workerRef, assignmentRef, at)
	if err != nil {
		return err
	}
	resolver, profileID, profileVersion, profileDigest, templateID, err := a.startResolver(ctx, tenant, workerRef, assignmentRef, at, profile)
	if err != nil {
		return err
	}
	request := a.startRequest(tenantID, sessionID, workerRef, assignmentRef, key, when, at, observationID)
	request.Resolver = resolver
	result, err := a.Runtime.Execute(ctx, execute.ExecuteRequest{Start: request})
	if err != nil {
		return fmt.Errorf("clock workflow runtime: start session %s: %w", sessionID, err)
	}
	started := result.Start
	if started.InstanceID == uuid.Nil || strings.TrimSpace(started.WorkflowID) == "" || strings.TrimSpace(started.CompiledPlanDigest) == "" {
		return errors.New("clock workflow runtime: start returned incomplete runtime identity")
	}
	createdAt := started.CreatedAt
	if createdAt.IsZero() {
		createdAt = when
	}
	binding := TimeClockRunBinding{TenantID: tenantID, TenantKey: strings.TrimSpace(tenant), SessionID: sessionID, WorkerRef: workerRef, AssignmentRef: assignmentRef, InstanceID: started.InstanceID, WorkflowID: started.WorkflowID, PlanDigest: started.CompiledPlanDigest, ProfileID: profileID, ProfileVersion: profileVersion, ProfileDigest: profileDigest, TemplateID: templateID, StartKey: key, CorrelationID: key, CreatedAt: createdAt}
	if err := a.Bindings.Save(ctx, binding); err != nil {
		return fmt.Errorf("clock workflow runtime: bind session %s: %w", sessionID, err)
	}
	return nil
}

// SignalRun receives one correlated punch and resumes the matching durable
// SIGNAL continuation through execute.Driver.
func (a TimeClockRuntimeAdapter) SignalRun(ctx context.Context, tenant, sessionID, signal, observationID string, at time.Time) error {
	return a.signalRun(ctx, tenant, sessionID, "", "", signal, observationID, at)
}

// SignalRunForAssignment is the correlated signal path used by the outbox
// dispatcher. It refuses a signal whose worker or assignment does not match
// the durable session binding before it reaches the runtime subscription.
func (a TimeClockRuntimeAdapter) SignalRunForAssignment(ctx context.Context, tenant, sessionID, workerRef, assignmentRef, signal, observationID string, at time.Time) error {
	return a.signalRun(ctx, tenant, sessionID, workerRef, assignmentRef, signal, observationID, at)
}

func (a TimeClockRuntimeAdapter) signalRun(ctx context.Context, tenant, sessionID, workerRef, assignmentRef, signal, observationID string, at time.Time) error {
	if err := a.validate(); err != nil {
		return err
	}
	if a.VersionsReader == nil {
		return errors.New("clock workflow runtime: durable instance version reader is required for signal resume")
	}
	tenantID, err := a.resolveTenant(tenant)
	if err != nil {
		return err
	}
	binding, err := a.Bindings.Load(ctx, tenantID, sessionID)
	if err != nil {
		return fmt.Errorf("clock workflow runtime: load session %s: %w", sessionID, err)
	}
	if binding.TenantID != tenantID || binding.SessionID != sessionID || binding.InstanceID == uuid.Nil || binding.PlanDigest == "" {
		return errors.New("clock workflow runtime: invalid durable session binding")
	}
	if (strings.TrimSpace(workerRef) != "" && binding.WorkerRef != strings.TrimSpace(workerRef)) || (strings.TrimSpace(assignmentRef) != "" && binding.AssignmentRef != strings.TrimSpace(assignmentRef)) {
		return errors.New("clock workflow runtime: session signal identity does not match durable binding")
	}
	if at.IsZero() {
		return errors.New("clock workflow runtime: occurred time is required")
	}
	when := at.UTC()
	eventType, schemaRef := timeClockSessionEvent, a.SchemaRef
	if binding.WorkflowID == clockpunch.WorkflowID {
		if signal != "OUT_PUNCH" {
			return errors.New("clock workflow runtime: clock-in/out workflow admits only clock-out signals")
		}
		eventType = "hcmnext.events.time.clock_out"
		schemaRef = clockpunch.ClockOutSignalSchemaRef()
	}
	payload, err := json.Marshal(struct {
		SessionID string    `json:"session_id"`
		Signal    string    `json:"signal"`
		At        time.Time `json:"occurred_at"`
	}{SessionID: sessionID, Signal: signal, At: when})
	if err != nil {
		return fmt.Errorf("clock workflow runtime: encode session %s signal: %w", sessionID, err)
	}
	receipt, err := a.Signals.Receive(ctx, SignalDelivery{
		TenantID: tenantID, SessionID: sessionID, Signal: signal, ObservationID: observationID,
		OccurredAt: when, EventType: eventType, Source: a.Source,
		CorrelationKey: timeClockSessionCorrelation, CorrelationValue: "time_session:" + sessionID,
		SchemaRef: schemaRef, IdempotencyKey: observationID, Payload: payload,
		ExpectedInstanceID: binding.InstanceID,
	})
	if err != nil {
		return fmt.Errorf("clock workflow runtime: receive session %s signal: %w", sessionID, err)
	}
	if receipt.SignalID == uuid.Nil || receipt.SubscriptionID == uuid.Nil {
		return errors.New("clock workflow runtime: incomplete durable signal receipt")
	}
	instanceVersion, err := a.VersionsReader.LoadWorkflowInstanceVersion(ctx, tenantID, binding.InstanceID)
	if err != nil {
		return fmt.Errorf("clock workflow runtime: read session %s runtime version: %w", sessionID, err)
	}
	if instanceVersion < 1 {
		return errors.New("clock workflow runtime: invalid durable instance version")
	}
	serverNow, err := a.serverNow()
	if err != nil {
		return err
	}
	request := a.startRequest(tenantID, sessionID, "", "", binding.StartKey, serverNow, time.Time{}, "")
	request.PinnedCompiledPlanDigest = binding.PlanDigest
	_, err = a.Runtime.ResumeSignal(ctx, execute.ResumeSignalRequest{Start: request, InstanceID: binding.InstanceID, ExpectedInstanceVersion: instanceVersion, SignalID: receipt.SignalID, SubscriptionID: receipt.SubscriptionID, RecordedAt: serverNow})
	if err != nil {
		return fmt.Errorf("clock workflow runtime: resume session %s signal: %w", sessionID, err)
	}
	return nil
}

func (a TimeClockRuntimeAdapter) validate() error {
	switch {
	case a.Runtime == nil:
		return ErrUnavailable
	case a.Resolver == nil || a.Versions == nil:
		return errors.New("clock workflow runtime: active resolver and version store are required")
	case a.Bindings == nil || a.Signals == nil:
		return errors.New("clock workflow runtime: durable bindings and signal receiver are required")
	case strings.TrimSpace(a.SchemaRef) == "" || strings.TrimSpace(a.Source) == "":
		return errors.New("clock workflow runtime: signal source and schema are required")
	case a.ResolveTenant == nil:
		return errors.New("clock workflow runtime: tenant resolver is required")
	}
	return nil
}

func (a TimeClockRuntimeAdapter) serverNow() (time.Time, error) {
	if a.Clock != nil {
		at := a.Clock().UTC()
		if !at.IsZero() {
			return at, nil
		}
	}
	return time.Time{}, errors.New("clock workflow runtime: trusted server clock is required")
}

func (a TimeClockRuntimeAdapter) resolveTenant(tenant string) (uuid.UUID, error) {
	key := strings.TrimSpace(tenant)
	if key == "" {
		return uuid.Nil, errors.New("clock workflow runtime: tenant is required")
	}
	id, err := a.ResolveTenant(key)
	if err != nil {
		return uuid.Nil, fmt.Errorf("clock workflow runtime: resolve tenant %q: %w", key, err)
	}
	if id == uuid.Nil {
		return uuid.Nil, errors.New("clock workflow runtime: resolved tenant is nil")
	}
	return id, nil
}

func (a TimeClockRuntimeAdapter) startRequest(tenantID uuid.UUID, sessionID, workerRef, assignmentRef, key string, at, occurredAt time.Time, observationID string) runtime.StartRequest {
	subjects := []string{"time_session:" + sessionID}
	if workerRef != "" {
		subjects = append(subjects, "worker:"+workerRef)
	}
	facts := map[string]string{"assignment_ref": assignmentRef}
	if workerRef != "" {
		facts["worker_ref"] = workerRef
	}
	facts["tenant_ref"] = tenantID.String()
	if !occurredAt.IsZero() {
		facts["occurred_at"] = occurredAt.UTC().Format(time.RFC3339Nano)
	}
	if observationID != "" {
		facts["observation_id"] = observationID
	}
	return runtime.StartRequest{TenantID: tenantID, CellID: a.CellID, StartIdempotencyKey: key, Resolver: a.Resolver, Versions: a.Versions, Source: &runtime.StartSource{Kind: runtime.StartSourceTrigger, IntentType: timeClockIntentRecordPunch, Trigger: &runtime.TriggerStartSource{TriggerID: key, TriggerType: "hcmnext.time.clock", Key: sessionID, Facts: facts}}, BusinessSubjectRefs: subjects, ExecutionMode: workflow.ModeExecute, CorrelationID: key, CreatedAt: at}
}

func (a TimeClockRuntimeAdapter) startResolver(ctx context.Context, tenant, workerRef, assignmentRef string, at time.Time, profile *timeprofile.TimeProfile) (runtime.WorkflowResolver, string, uint64, string, string, error) {
	if a.Profiles == nil && a.PlanRegistry == nil {
		return a.Resolver, "", 0, "", "", nil
	}
	if a.Profiles == nil || a.PlanRegistry == nil {
		return nil, "", 0, "", "", errors.New("clock workflow runtime: profile resolver and plan registry are required together")
	}
	if profile == nil {
		resolved, err := a.Profiles.Resolve(ctx, tenant, workerRef, assignmentRef, at)
		if err != nil {
			return nil, "", 0, "", "", err
		}
		profile = &resolved
	}
	id, version, digest, templateID, err := profileSnapshot(*profile)
	if err != nil {
		return nil, "", 0, "", "", fmt.Errorf("clock workflow runtime: profile snapshot: %w", err)
	}
	return TimeWorkflowResolver{Profiles: a.Profiles, Plans: *a.PlanRegistry, TenantKey: a.TenantKey, ResolvedProfile: profile}, id, version, digest, templateID, nil
}

func startKey(tenant uuid.UUID, session string) string {
	return "timeclock:session:" + tenant.String() + ":" + session
}

var _ SessionWorkflow = TimeClockRuntimeAdapter{}
