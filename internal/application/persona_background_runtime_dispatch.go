package application

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentrunstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentrunstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// PersonaBackgroundOutputRecovery restores only independently verified signed
// output from its owner store, after checking current admission and output scope.
type PersonaBackgroundOutputRecovery interface {
	RecoverPersonaRunOutput(context.Context, agentrun.Record, runstate.Run) (agentsecurity.FinalOutputPersistence, error)
}

// PersonaBackgroundDispatcher handles an exact durable persona admission after
// restart or wake. It never creates a delegation grant or new human admission.
type PersonaBackgroundDispatcher struct {
	agents     *agentstore.Store
	tenantUUID func(values.TenantId) uuid.UUID
	worker     *PersonaRunModelWorker
	outputs    PersonaBackgroundOutputRecovery
	scanMu     sync.Mutex
	cursors    map[string]personaBackgroundScanCursor
}

type personaBackgroundScanCursor struct {
	at time.Time
	id string
}

func NewPersonaBackgroundDispatcher(agents *agentstore.Store, tenantUUID func(values.TenantId) uuid.UUID, worker *PersonaRunModelWorker, outputs PersonaBackgroundOutputRecovery) (*PersonaBackgroundDispatcher, error) {
	if agents == nil || tenantUUID == nil || worker == nil || worker.tenants == nil || worker.fence == nil || worker.leases == nil || isNilPersonaOutputPort(outputs) {
		return nil, errPersonaRunModelWorker
	}
	return &PersonaBackgroundDispatcher{agents: agents, tenantUUID: tenantUUID, worker: worker, outputs: outputs, cursors: make(map[string]personaBackgroundScanCursor)}, nil
}

// DispatchTenant scans one explicitly server-owned tenant's durable persona
// admissions. The bound prevents one tenant monopolizing a background poll.
func (d *PersonaBackgroundDispatcher) DispatchTenant(ctx context.Context, tenant string, limit int) error {
	if d == nil || ctx == nil || d.worker == nil || d.worker.tenants == nil || d.agents == nil || d.tenantUUID == nil || d.cursors == nil || limit < 1 || limit > 1000 {
		return errPersonaRunModelWorker
	}
	// A cursor influences scheduling only. Every selected record still passes
	// current native authority before any execution mutation or output.
	d.scanMu.Lock()
	defer d.scanMu.Unlock()
	cfg, err := d.worker.tenants.ForPersonaRunTenant(ctx, tenant)
	if err != nil || cfg.Now == nil {
		return errPersonaRunModelWorker
	}
	repo, err := agentrunstore.NewAdmissionRepository(d.agents, d.tenantUUID(values.TenantId(tenant)), values.TenantId(tenant))
	if err != nil {
		return err
	}
	cursor := d.cursors[tenant]
	at := cfg.Now().UTC()
	records, err := repo.ListRunnableBySourceAfter(ctx, agentrun.SourcePersonaMention, limit, at, cursor.at, cursor.id)
	if err != nil {
		return err
	}
	if len(records) == 0 && cursor.id != "" {
		records, err = repo.ListRunnableBySource(ctx, agentrun.SourcePersonaMention, limit, at)
		if err != nil {
			return err
		}
	}
	var failures []error
	for _, record := range records {
		// Refusals must also advance the scheduling cursor so revoked work does
		// not consume every poll and prevent later valid admissions from running.
		d.cursors[tenant] = personaBackgroundScanCursor{at: record.AdmittedAt, id: record.ID}
		if record.Decision != agentrun.DecisionAccepted {
			continue
		}
		if err = d.Wake(ctx, tenant, record.ID); err != nil && !errors.Is(err, ErrPersonaRunExecutorBusy) {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

// Wake authenticates the configured workload through current admission before
// any run mutation. Unknown external effects remain in owner reconciliation.
func (d *PersonaBackgroundDispatcher) Wake(ctx context.Context, tenant, admissionID string) error {
	if d == nil || ctx == nil || d.worker == nil || d.worker.tenants == nil || d.agents == nil || d.tenantUUID == nil || isNilPersonaOutputPort(d.outputs) {
		return errPersonaRunModelWorker
	}
	if _, ok := trust.FromContext(ctx); ok {
		return errPersonaRunModelWorker
	}
	cfg, err := d.worker.tenants.ForPersonaRunTenant(ctx, tenant)
	if err != nil {
		return err
	}
	if isNilPersonaOutputPort(cfg.BackgroundReply) || cfg.Now == nil || cfg.Authority == nil || cfg.AdmissionRecheck == nil || cfg.ExecutionStore == nil {
		return errPersonaRunModelWorker
	}
	repo, err := agentrunstore.NewAdmissionRepository(d.agents, d.tenantUUID(values.TenantId(tenant)), values.TenantId(tenant))
	if err != nil {
		return err
	}
	record, err := repo.GetByID(ctx, admissionID)
	if err != nil || record.Decision != agentrun.DecisionAccepted {
		return errPersonaRunModelWorker
	}
	ctx = WithPersonaBackgroundAdmission(ctx, record)
	current, err := cfg.Authority.VerifyAdmission(ctx, record.Request)
	if err != nil || !reflect.DeepEqual(current, record.Authority) {
		return fmt.Errorf("%w: background current admission", agentrun.ErrAuthorityRefusal)
	}
	state, err := runstate.New(cfg.ExecutionStore, cfg.AdmissionRecheck)
	if err != nil {
		return err
	}
	run, err := cfg.ExecutionStore.Get(ctx, record.ID)
	if errors.Is(err, runstate.ErrNotFound) || errors.Is(err, agentrunstate.ErrNotFound) {
		run, err = state.Start(ctx, record)
	}
	if err != nil {
		return err
	}
	if run.ID != record.ID || run.AdmissionID != record.ID || run.TenantID != tenant || run.RequestDigest != record.RequestDigest {
		return errPersonaRunModelWorker
	}
	if err = validatePersonaModelWorkBinding(record, run); err != nil {
		return err
	}
	if run.State == runstate.StateCompleted {
		return nil
	}
	if run.State == runstate.StateRunning {
		if run.Lease == nil || run.Lease.Until.After(cfg.Now().UTC()) {
			return ErrPersonaRunExecutorBusy
		}
		run, err = state.Recover(ctx, run.ID, run.Version, cfg.Now().UTC())
		if err != nil {
			return err
		}
	}
	if run.State == runstate.StateWaiting {
		return ErrPersonaRunExecutorBusy
	}
	if run.State != runstate.StateReady {
		return fmt.Errorf("%w: run requires owner reconciliation or is terminal", ErrPersonaRunExecutorBusy)
	}
	steps := &personaRunStepIdentityState{}
	executor := &personaAdmittedRunExecutor{state: state, store: cfg.ExecutionStore, model: fencedPersonaRunModelExecutor{inner: cfg.Model, fence: d.worker.fence, steps: steps}, work: fencedPersonaRunModelWorkSource{inner: cfg.Work, fence: d.worker.fence, leases: d.worker.leases, steps: steps}, tools: cfg.Tools, output: fencedPersonaRunOutputValidator{inner: cfg.Output, fence: d.worker.fence, steps: steps}, reply: cfg.Reply, backgroundReply: personaFencedBackgroundReply{inner: cfg.BackgroundReply, fence: d.worker.fence, leases: d.worker.leases}, workerID: cfg.WorkerID, leaseTTL: cfg.LeaseTTL, now: cfg.Now}
	output, err := d.outputs.RecoverPersonaRunOutput(ctx, record, run)
	if errors.Is(err, agentpersonastore.ErrNotFound) {
		for _, cp := range run.Checkpoints {
			if cp.Phase == runstate.PhaseValidation || cp.Phase == runstate.PhaseDelivery {
				return ErrPersonaRunOutputRejected
			}
		}
		return executor.execute(ctx, record, run)
	}
	if err != nil {
		return err
	}
	if output.Identity() != personaRunFinalOutputIdentity(record, run) {
		return ErrPersonaRunOutputRejected
	}
	validated := false
	for _, checkpoint := range run.Checkpoints {
		if checkpoint.Phase == runstate.PhaseValidation {
			if checkpoint.Ref != output.Identity().OutputID || checkpoint.Digest != output.SemanticDigest() {
				return ErrPersonaRunOutputRejected
			}
			validated = true
		}
	}
	claimed, err := state.Claim(ctx, run.ID, cfg.WorkerID, cfg.Now().UTC(), cfg.LeaseTTL)
	if err != nil {
		return err
	}
	// A crash may occur after the immutable output write but before its run
	// checkpoint. Record the independently restored projection before delivery.
	if !validated {
		claimed, err = state.Checkpoint(ctx, claimed.ID, cfg.WorkerID, claimed.Fence, claimed.Version, runstate.PhaseValidation, 1, output.Identity().OutputID, output.SemanticDigest(), cfg.Now().UTC())
		if err != nil {
			return err
		}
	}
	receipt, err := executor.deliver(ctx, record, claimed, output)
	if err != nil {
		return err
	}
	if !validPersonaRunReplyReceipt(receipt) {
		return ErrPersonaRunDeliveryFailure
	}
	digest, err := personaRunDeliveryDigest(receipt)
	if err != nil {
		return err
	}
	_, err = state.Checkpoint(ctx, claimed.ID, cfg.WorkerID, claimed.Fence, claimed.Version, runstate.PhaseDelivery, 1, record.ID, digest, cfg.Now().UTC())
	return err
}

type personaFencedBackgroundReply struct {
	inner  PersonaRunBackgroundReplyDeliverer
	fence  PersonaRunSecurityFence
	leases PersonaRunSecurityLeaseResolver
}

func (d personaFencedBackgroundReply) DeliverBackgroundPersonaReply(ctx context.Context, record agentrun.Record, run runstate.Run, output agentsecurity.FinalOutputPersistence) (PersonaReplyDeliveryReceipt, error) {
	if d.inner == nil || d.fence == nil || d.leases == nil {
		return PersonaReplyDeliveryReceipt{}, ErrPersonaRunDeliveryFailure
	}
	lease, err := d.leases.ResolvePersonaRunSecurityLease(ctx, record, run)
	if err != nil {
		return PersonaReplyDeliveryReceipt{}, err
	}
	if err = d.fence.Bind(agentsecurity.PersonaRunID(run.ID), run.TenantID, lease); err != nil {
		return PersonaReplyDeliveryReceipt{}, err
	}
	var receipt PersonaReplyDeliveryReceipt
	_, err = d.fence.RunStep(ctx, agentsecurity.PersonaRunID(run.ID), personaRunSecurityStepID(run.ID, "background-delivery", personaRunStepGenerationKey(run)), func(ctx context.Context) error {
		var e error
		receipt, e = d.inner.DeliverBackgroundPersonaReply(ctx, record, run, output)
		return e
	})
	return receipt, err
}

// RecoverPersonaRunOutput retains the verifier boundary used by foreground
// retrieval. It exposes no API to manufacture a FinalOutputPersistence value.
func (s *PersonaFinalOutputSource) RecoverPersonaRunOutput(ctx context.Context, record agentrun.Record, run runstate.Run) (agentsecurity.FinalOutputPersistence, error) {
	if s == nil || ctx == nil || s.stores == nil || s.verifier == nil || s.rehydrator == nil || validatePersonaModelWorkBinding(record, run) != nil {
		return agentsecurity.FinalOutputPersistence{}, errPersonaFinalOutputSourceUnavailable
	}
	store, err := s.stores.ForTenant(ctx, values.TenantId(record.Request.Source.TenantID))
	if err != nil {
		return agentsecurity.FinalOutputPersistence{}, err
	}
	identity := personaRunFinalOutputIdentity(record, run)
	stored, err := store.GetFinalOutput(ctx, identity.OutputID)
	if err != nil {
		return agentsecurity.FinalOutputPersistence{}, err
	}
	if personaOutputRecordIdentity(stored) != identity {
		return agentsecurity.FinalOutputPersistence{}, errPersonaFinalOutputSourceUnavailable
	}
	return store.RecoverFinalOutputForIdentity(WithPersonaBackgroundAdmission(ctx, record), identity, s.verifier, s.rehydrator)
}
