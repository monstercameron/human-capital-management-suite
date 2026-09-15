package app

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/intentcontrol"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

type intentCorrelationReader interface {
	LoadIntentByCorrelation(context.Context, string, string) (IntentRecord, error)
}

type resumeTenantContextKey struct{}

// WithResumeTenant supplies the tenant a scheduler workload is allowed to
// dispatch. The tenant is deliberately explicit at this boundary: a ready
// work row is not permission to infer or cross a tenant.
func WithResumeTenant(ctx context.Context, tenant string) context.Context {
	return context.WithValue(ctx, resumeTenantContextKey{}, tenant)
}

func resumeTenant(ctx context.Context) (string, bool) {
	tenant, ok := ctx.Value(resumeTenantContextKey{}).(string)
	return tenant, ok && tenant != ""
}

// ResumeFiredTimer reconstructs the same approved StartRequest used by
// ExecuteIntent, then resumes the parked WAIT through the executor's durable
// timer path. Intent lookup is correlation-based because a timer parked
// instance has no work item to carry the intent identity.
func (c *Cell) ResumeFiredTimer(ctx context.Context, instanceID, nodeID string, attempt int) (ExecutionResult, error) {
	var timerID uuid.UUID
	prepared, err := c.prepareParkedResume(ctx, "timer", instanceID, nodeID, attempt,
		func(ctx context.Context, tx dbport.Tx, tenantID, parsedInstanceID uuid.UUID) (string, error) {
			if err := tx.QueryRow(ctx, `
				SELECT timer_id FROM workflow_timer
				WHERE tenant_id = $1 AND instance_id = $2 AND node_id = $3 AND timer_state = $4
				ORDER BY fired_at DESC NULLS LAST, timer_id DESC
				LIMIT 1`, tenantID, parsedInstanceID, nodeID, "FIRED").Scan(&timerID); err != nil {
				return "", fmt.Errorf("app: load fired timer for %s/%s: %w", instanceID, nodeID, err)
			}
			return "timer:" + timerID.String(), nil
		})
	if err != nil {
		return ExecutionResult{}, err
	}
	result, resumeErr := c.Service.executor.ResumeTimer(ctx, ExecutionTimerResumeRequest{
		Start: prepared.start, InstanceID: prepared.instanceID,
		ExpectedInstanceVersion: prepared.instance.InstanceVersion, TimerID: timerID,
		Outcome: frontier.NodeOutcome{NodeID: nodeID, Outcome: workflow.OutcomeSucceeded},
	})
	if resumeErr != nil {
		return ExecutionResult{}, resumeErr
	}
	return c.consumeParkedResume(ctx, "timer", prepared, result)
}

// parkedResume is everything [Cell.prepareParkedResume] rebuilt for one
// parked instance: the approved StartRequest ExecuteIntent used, the instance
// row it was read from, and the intent the result is consumed against.
type parkedResume struct {
	start          runtime.StartRequest
	instanceID     uuid.UUID
	instance       runtime.Instance
	intentInstance intent.Instance
	intentRecord   IntentRecord
}

// parkedResumeLocator finds the durable evidence (a fired timer, a matched
// signal) a parked node resumes from, inside the preparation transaction, and
// returns its source reference.
type parkedResumeLocator func(ctx context.Context, tx dbport.Tx, tenantID, instanceID uuid.UUID) (string, error)

// prepareParkedResume reconstructs the same approved StartRequest used by
// ExecuteIntent for an instance parked on a durable non-human wait. kind names
// the wait ("timer", "signal") in every refusal.
func (c *Cell) prepareParkedResume(ctx context.Context, kind, instanceID, nodeID string, attempt int, locate parkedResumeLocator) (parkedResume, error) {
	what := kind + " resume"
	if c == nil || c.Service == nil {
		return parkedResume{}, fmt.Errorf("app: %s has no intent service", what)
	}
	engine, ok := c.Journey.(*journeyEngine)
	if !ok || engine == nil || engine.db == nil {
		return parkedResume{}, fmt.Errorf("app: %s has no execution journey", what)
	}
	if c.Service.executor == nil || c.Service.tenantUUID == nil || c.Service.executionResolver == nil || c.Service.executionVersions == nil {
		return parkedResume{}, fmt.Errorf("app: %s has incomplete execution authority", what)
	}
	if nodeID == "" || attempt < 1 {
		return parkedResume{}, fmt.Errorf("app: %s needs a node and positive attempt", what)
	}
	parsedInstanceID, err := uuid.Parse(instanceID)
	if err != nil {
		return parkedResume{}, fmt.Errorf("app: %s instance id: %w", what, err)
	}
	tenant, ok := resumeTenant(ctx)
	if !ok {
		return parkedResume{}, fmt.Errorf("app: %s needs an explicit tenant", what)
	}
	tenantID := c.Service.tenantUUID(values.TenantId(tenant))
	if tenantID == uuid.Nil {
		return parkedResume{}, fmt.Errorf("app: %s resolved a nil tenant", what)
	}

	tx, err := engine.db.Begin(ctx)
	if err != nil {
		return parkedResume{}, fmt.Errorf("app: begin %s: %w", what, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return parkedResume{}, err
	}
	instance, err := (runtime.Store{}).LoadInstance(ctx, tx, tenantID, parsedInstanceID)
	if err != nil {
		return parkedResume{}, err
	}
	// A timer- or signal-parked instance is RUNNING (the driver records
	// WAITING only for human work); either is a live instance whose frontier
	// the resumed node must still be on. Every other status (paused,
	// quarantined, terminal, repair) refuses: durable evidence never revives an
	// intervention or a finished run.
	if instance.RuntimeStatus != runtime.InstanceRunning && instance.RuntimeStatus != runtime.InstanceWaiting {
		return parkedResume{}, fmt.Errorf("app: %s instance %s is %s, not RUNNING or WAITING", what, instanceID, instance.RuntimeStatus)
	}
	foundNode := false
	for _, current := range instance.CurrentNodeIDs {
		if current == nodeID {
			foundNode = true
			break
		}
	}
	if !foundNode {
		return parkedResume{}, fmt.Errorf("%w: app: %s node %q is not on instance frontier", ErrParkedResumeStale, what, nodeID)
	}
	sourceRef, err := locate(ctx, tx, tenantID, parsedInstanceID)
	if err != nil {
		return parkedResume{}, err
	}

	reader, ok := c.Service.store.(intentCorrelationReader)
	if !ok {
		return parkedResume{}, fmt.Errorf("app: intent store cannot resolve workflow correlation")
	}
	intentRecord, err := reader.LoadIntentByCorrelation(ctx, tenant, instance.CorrelationID)
	if err != nil {
		return parkedResume{}, fmt.Errorf("app: resolve %s instance intent: %w", kind, err)
	}
	intentInstance, err := decodeEnvelope(intentRecord.Envelope)
	if err != nil {
		return parkedResume{}, err
	}
	intentID, err := executionIntentUUID(intentInstance.IntentID)
	if err != nil {
		return parkedResume{}, err
	}
	stored, err := (intentcontrol.RevisionStore{}).Load(ctx, tx, tenantID, intentID, simulationRevision)
	if err != nil {
		return parkedResume{}, fmt.Errorf("app: load stored proposal revision: %w", err)
	}
	revision, err := intentcontrol.DecodeFullProposal(stored.Payload, fullProposalVerifier{c.Service.digester})
	if err != nil {
		return parkedResume{}, fmt.Errorf("app: %s legacy/tampered proposal: %w", what, err)
	}
	if stored.TenantID != tenantID || stored.IntentID != intentID || stored.Revision != simulationRevision ||
		stored.SchemaRef != executionProposalSchemaRef || stored.ProposalDigest != stored.MaterialDigest ||
		revision.Tenant != values.TenantId(tenant) || revision.IntentID != intentInstance.IntentID || revision.Revision != simulationRevision ||
		revision.MaterialDigest.Digest != stored.MaterialDigest {
		return parkedResume{}, fmt.Errorf("app: %s proposal identity mismatch", what)
	}
	artifact := &intentsv1.SimulationArtifact{
		IntentId: intentInstance.IntentID, ProposalRevisionId: revision.ProposalRevisionID,
		MaterialProposalDigest: revision.MaterialDigest.ToProto(),
	}
	start, startErr := c.Service.executionStart(intentInstance, artifact, sourceRef, revision)
	if startErr != nil {
		return parkedResume{}, startErr
	}
	if err := tx.Commit(ctx); err != nil {
		return parkedResume{}, fmt.Errorf("app: commit %s preparation: %w", what, err)
	}
	return parkedResume{start: start, instanceID: parsedInstanceID, instance: instance,
		intentInstance: intentInstance, intentRecord: intentRecord}, nil
}

// consumeParkedResume projects a resumed instance's result onto its intent,
// exactly as ExecuteIntent does for the run that parked it.
func (c *Cell) consumeParkedResume(ctx context.Context, kind string, prepared parkedResume, result ExecutionResult) (ExecutionResult, error) {
	def, err := c.Service.defs.Resolve(prepared.intentInstance.Definition)
	if err != nil {
		return ExecutionResult{}, fmt.Errorf("app: %s resume definition: %w", kind, err)
	}
	if err := c.Service.consumeExecutionResult(ctx, prepared.intentInstance, def, prepared.intentRecord, result); err != nil {
		return ExecutionResult{}, err
	}
	return result, nil
}
