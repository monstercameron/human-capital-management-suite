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
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// ExecutionRedeliveryRequest redelivers the READY frontier of one instance
// whose driver died mid-drain (WF-RUN-003). Start is the instance's pinned
// start request, reconstructed from durable rows exactly as a timer resume
// reconstructs it; ExpectedInstanceVersion is the version the recovery sweep
// claimed the instance at.
type ExecutionRedeliveryRequest struct {
	Start                   runtime.StartRequest
	InstanceID              uuid.UUID
	ExpectedInstanceVersion int64
}

// ReadyRedeliverer is the optional [ProposalExecutor] extension a recovery
// sweep redelivers through. It is separate from ProposalExecutor so an
// executor that predates WF-RUN-003 keeps satisfying the port; a cell whose
// executor does not implement it refuses redelivery rather than guessing.
type ReadyRedeliverer interface {
	RedeliverReady(ctx context.Context, req ExecutionRedeliveryRequest) (ExecutionResult, error)
}

// RedeliverReady reconstructs the approved StartRequest an orphaned instance
// was started with and drains its READY frontier through the executor. The
// caller (the recovery sweep) has already taken the instance's lease over and
// carries its fence on ctx; the tenant is explicit on ctx through
// [WithResumeTenant], exactly as for [Cell.ResumeFiredTimer].
func (c *Cell) RedeliverReady(ctx context.Context, instanceID string, expectedVersion int64) (ExecutionResult, error) {
	if c == nil || c.Service == nil {
		return ExecutionResult{}, fmt.Errorf("app: ready redelivery has no intent service")
	}
	engine, ok := c.Journey.(*journeyEngine)
	if !ok || engine == nil || engine.db == nil {
		return ExecutionResult{}, fmt.Errorf("app: ready redelivery has no execution journey")
	}
	redeliverer, ok := c.Service.executor.(ReadyRedeliverer)
	if !ok || c.Service.tenantUUID == nil || c.Service.executionResolver == nil || c.Service.executionVersions == nil {
		return ExecutionResult{}, fmt.Errorf("app: ready redelivery has incomplete execution authority")
	}
	if expectedVersion < 1 {
		return ExecutionResult{}, fmt.Errorf("app: ready redelivery needs a positive expected instance version")
	}
	parsedInstanceID, err := uuid.Parse(instanceID)
	if err != nil {
		return ExecutionResult{}, fmt.Errorf("app: ready redelivery instance id: %w", err)
	}
	tenant, ok := resumeTenant(ctx)
	if !ok {
		return ExecutionResult{}, fmt.Errorf("app: ready redelivery needs an explicit tenant")
	}
	tenantID := c.Service.tenantUUID(values.TenantId(tenant))
	if tenantID == uuid.Nil {
		return ExecutionResult{}, fmt.Errorf("app: ready redelivery resolved a nil tenant")
	}

	tx, err := engine.db.Begin(ctx)
	if err != nil {
		return ExecutionResult{}, fmt.Errorf("app: begin ready redelivery: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return ExecutionResult{}, err
	}
	instance, err := (runtime.Store{}).LoadInstance(ctx, tx, tenantID, parsedInstanceID)
	if err != nil {
		return ExecutionResult{}, err
	}
	start, intentInstance, intentRecord, err := c.parkedExecutionStart(ctx, tx, tenant, tenantID, instance, "ready redelivery")
	if err != nil {
		return ExecutionResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ExecutionResult{}, fmt.Errorf("app: commit ready redelivery preparation: %w", err)
	}
	result, err := redeliverer.RedeliverReady(ctx, ExecutionRedeliveryRequest{
		Start: start, InstanceID: parsedInstanceID, ExpectedInstanceVersion: expectedVersion,
	})
	if err != nil {
		return ExecutionResult{}, err
	}
	def, err := c.Service.defs.Resolve(intentInstance.Definition)
	if err != nil {
		return ExecutionResult{}, fmt.Errorf("app: ready redelivery definition: %w", err)
	}
	if err := c.Service.consumeExecutionResult(ctx, intentInstance, def, intentRecord, result); err != nil {
		return ExecutionResult{}, err
	}
	return result, nil
}

// parkedExecutionStart reconstructs the approved StartRequest a running
// instance was started with, from durable rows only: the intent the instance's
// correlation names, and the stored, digest-verified proposal revision. It is
// shared by every path that continues an instance no request is carrying the
// proposal for -- a fired timer and a redelivered drain -- so both present the
// start identity the instance pinned. what labels its refusals.
func (c *Cell) parkedExecutionStart(
	ctx context.Context, tx dbport.Tx, tenant string, tenantID uuid.UUID, instance runtime.Instance, what string,
) (runtime.StartRequest, intent.Instance, IntentRecord, error) {
	reader, ok := c.Service.store.(intentCorrelationReader)
	if !ok {
		return runtime.StartRequest{}, intent.Instance{}, IntentRecord{}, fmt.Errorf("app: intent store cannot resolve workflow correlation")
	}
	intentRecord, err := reader.LoadIntentByCorrelation(ctx, tenant, instance.CorrelationID)
	if err != nil {
		return runtime.StartRequest{}, intent.Instance{}, IntentRecord{}, fmt.Errorf("app: resolve %s instance intent: %w", what, err)
	}
	intentInstance, err := decodeEnvelope(intentRecord.Envelope)
	if err != nil {
		return runtime.StartRequest{}, intent.Instance{}, IntentRecord{}, err
	}
	intentID, err := executionIntentUUID(intentInstance.IntentID)
	if err != nil {
		return runtime.StartRequest{}, intent.Instance{}, IntentRecord{}, err
	}
	stored, err := (intentcontrol.RevisionStore{}).Load(ctx, tx, tenantID, intentID, simulationRevision)
	if err != nil {
		return runtime.StartRequest{}, intent.Instance{}, IntentRecord{}, fmt.Errorf("app: load stored proposal revision: %w", err)
	}
	revision, err := intentcontrol.DecodeFullProposal(stored.Payload, fullProposalVerifier{c.Service.digester})
	if err != nil {
		return runtime.StartRequest{}, intent.Instance{}, IntentRecord{}, fmt.Errorf("app: %s legacy/tampered proposal: %w", what, err)
	}
	if stored.TenantID != tenantID || stored.IntentID != intentID || stored.Revision != simulationRevision ||
		stored.SchemaRef != executionProposalSchemaRef || stored.ProposalDigest != stored.MaterialDigest ||
		revision.Tenant != values.TenantId(tenant) || revision.IntentID != intentInstance.IntentID || revision.Revision != simulationRevision ||
		revision.MaterialDigest.Digest != stored.MaterialDigest {
		return runtime.StartRequest{}, intent.Instance{}, IntentRecord{}, fmt.Errorf("app: %s proposal identity mismatch", what)
	}
	artifact := &intentsv1.SimulationArtifact{
		IntentId: intentInstance.IntentID, ProposalRevisionId: revision.ProposalRevisionID,
		MaterialProposalDigest: revision.MaterialDigest.ToProto(),
	}
	start, startErr := c.Service.executionStart(intentInstance, artifact, what, revision)
	if startErr != nil {
		return runtime.StartRequest{}, intent.Instance{}, IntentRecord{}, startErr
	}
	return start, intentInstance, intentRecord, nil
}
