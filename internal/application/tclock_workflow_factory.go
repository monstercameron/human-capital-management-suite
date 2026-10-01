package application

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/clockpunch"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// ClockWorkflowDriverFactoryOptions contains the production ports needed to
// build one request-scoped execute.Driver for a punch workflow.
type ClockWorkflowDriverFactoryOptions struct {
	DB                  execute.Beginner
	BaseSteps           execute.StepRunner
	Commit              clockservice.PunchCommitter
	Telemetry           *clockservice.ClockWorkflowTelemetry
	ResolveTenant       clockservice.TenantResolver
	Clock               func() time.Time
	Advance             execute.AdvanceFunc
	Signals             execute.SignalSubscriber
	SignalReader        execute.SignalReader
	SignalTimeoutReader execute.SignalTimeoutReader
	Terminals           execute.TerminalRegistry
	Terminal            execute.TerminalWriter
	Guard               idempotency.Store
	Retention           idempotency.RetentionPolicy
}

// NewClockWorkflowDriverFactory composes the real PostgreSQL workflow engine.
// The authoritative commit node gets a request-scoped runner; every other
// node remains owned by the application's registered step handlers.
func NewClockWorkflowDriverFactory(opts ClockWorkflowDriverFactoryOptions) (clockservice.WorkflowDriverFactory, error) {
	if opts.DB == nil || opts.BaseSteps == nil || opts.Commit == nil || opts.Telemetry == nil || opts.ResolveTenant == nil {
		return nil, errors.New("clock workflow factory: database, steps, commit, telemetry and tenant resolver are required")
	}
	if opts.Terminal == nil || opts.Guard == nil || opts.Retention.Retention <= 0 || opts.Retention.RetryWindow <= 0 || opts.Retention.Retention < opts.Retention.RetryWindow {
		return nil, errors.New("clock workflow factory: terminal writer, replay guard and retention are required")
	}
	if err := opts.Telemetry.Validate(); err != nil {
		return nil, err
	}
	if opts.Clock == nil {
		return nil, errors.New("clock workflow factory: clock is required")
	}
	return func(ctx context.Context, tenant string, work clockservice.PunchWork) (clockservice.WorkflowRuntime, error) {
		tenantID, err := opts.ResolveTenant(tenant)
		if err != nil {
			return nil, err
		}
		if tenantID == uuid.Nil || strings.TrimSpace(tenant) == "" {
			return nil, errors.New("clock workflow factory: invalid tenant")
		}
		nodeID := clockpunch.NodeCommitClockOut
		if work.SessionIsNew {
			nodeID = clockpunch.NodeCommitPunch
		}
		steps := clockWorkflowStepRunner{base: opts.BaseSteps, commit: clockservice.PunchCommitStepRunner{
			Tenant: tenant, TenantID: tenantID, Work: work, Commit: opts.Commit, NodeID: nodeID,
		}}
		driver, err := execute.New(execute.Options{
			DB: opts.DB, Steps: steps, Clock: opts.Clock, Advance: opts.Advance,
			Instrumentation: opts.Telemetry.Instrumentation(), Recorder: opts.Telemetry.Recorder(),
			Signals: opts.Signals, SignalReader: opts.SignalReader, SignalTimeoutReader: opts.SignalTimeoutReader, Terminals: opts.Terminals, Terminal: opts.Terminal, Guard: opts.Guard, Retention: opts.Retention,
		})
		if err != nil {
			return nil, err
		}
		return driver, nil
	}, nil
}

type clockWorkflowStepRunner struct {
	base   execute.StepRunner
	commit clockservice.PunchCommitStepRunner
}

func (r clockWorkflowStepRunner) Run(ctx context.Context, req execute.StepRequest) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	if req.Node.ID == r.commit.NodeID {
		return r.commit.Run(ctx, req)
	}
	return r.base.Run(ctx, req)
}

var _ execute.StepRunner = clockWorkflowStepRunner{}

// PostgresClockWorkflowEvidenceReader reads runtime evidence using a
// tenant-scoped transaction. Effect references are the authoritative link
// between the workflow commit node and the exact observation/session; a
// matching instance or plan alone is insufficient.
type PostgresClockWorkflowEvidenceReader struct {
	DB dbport.Beginner
}

// LoadPunchNodeEvidence returns the latest successful commit_punch
// attempt linked to observationID, or found=false when no such proof exists.
func (r PostgresClockWorkflowEvidenceReader) LoadPunchNodeEvidence(ctx context.Context, tenantID, instanceID uuid.UUID, observationID string) (clockservice.PunchNodeEvidence, bool, error) {
	if r.DB == nil || tenantID == uuid.Nil || instanceID == uuid.Nil || strings.TrimSpace(observationID) == "" {
		return clockservice.PunchNodeEvidence{}, false, errors.New("clock workflow evidence: tenant, instance and observation are required")
	}
	var tx dbport.Tx
	var err error
	if ro, ok := r.DB.(interface {
		BeginReadOnly(context.Context) (dbport.Tx, error)
	}); ok {
		tx, err = ro.BeginReadOnly(ctx)
	} else {
		tx, err = r.DB.Begin(ctx)
	}
	if err != nil {
		return clockservice.PunchNodeEvidence{}, false, err
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return clockservice.PunchNodeEvidence{}, false, err
	}
	const query = `
		SELECT n.node_id, n.attempt, n.status, COALESCE(n.output_artifact_ref, ''),
		       COALESCE(n.trace_id, ''), n.effect_refs, i.compiled_plan_hash,
		       i.instance_version, n.completed_at, n.capability_execution_id
		FROM workflow_node_execution n
		JOIN workflow_instance i ON i.tenant_id = n.tenant_id AND i.instance_id = n.instance_id
		WHERE n.tenant_id = $1 AND n.instance_id = $2 AND n.node_id IN ('commit_punch', 'commit_clock_out')
		  AND n.status = 'SUCCEEDED'
		  AND ('time_observation:' || $3) = ANY(n.effect_refs)
		ORDER BY n.attempt DESC
		LIMIT 1`
	var evidence clockservice.PunchNodeEvidence
	var refs []string
	var completedAt *time.Time
	var capabilityExecutionID *uuid.UUID
	if err := tx.QueryRow(ctx, query, tenantID, instanceID, observationID).Scan(
		&evidence.NodeID, &evidence.Attempt, &evidence.CompletedState,
		&evidence.OutputDigest, &evidence.TraceID, &refs, &evidence.PlanDigest,
		&evidence.InstanceVersion, &completedAt, &capabilityExecutionID,
	); err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return clockservice.PunchNodeEvidence{}, false, nil
		}
		return clockservice.PunchNodeEvidence{}, false, err
	}
	evidence.TenantID = tenantID
	evidence.InstanceID = instanceID
	evidence.ObservationID = observationID
	if completedAt == nil || completedAt.IsZero() {
		return clockservice.PunchNodeEvidence{}, false, errors.New("clock workflow evidence: successful node has no completion timestamp")
	}
	if capabilityExecutionID == nil || *capabilityExecutionID != runtime.NodeExecutionID(tenantID, instanceID, evidence.NodeID, evidence.Attempt) {
		return clockservice.PunchNodeEvidence{}, false, errors.New("clock workflow evidence: capability execution identity does not match node attempt")
	}
	sessionRefs := 0
	observationRefs := 0
	for _, ref := range refs {
		if strings.HasPrefix(ref, "time_observation:") {
			observationRefs++
			if ref != "time_observation:"+observationID {
				return clockservice.PunchNodeEvidence{}, false, errors.New("clock workflow evidence: unrelated observation reference")
			}
		}
		if strings.HasPrefix(ref, "time_session:") {
			sessionRefs++
			evidence.SessionID = strings.TrimPrefix(ref, "time_session:")
		}
	}
	if sessionRefs != 1 || observationRefs != 1 || evidence.SessionID == "" {
		return clockservice.PunchNodeEvidence{}, false, errors.New("clock workflow evidence: expected exactly one time session reference")
	}
	return evidence, true, nil
}

var _ clockservice.PunchNodeEvidenceReader = PostgresClockWorkflowEvidenceReader{}
