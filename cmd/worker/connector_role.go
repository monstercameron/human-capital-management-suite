package main

// SVC-008 hosts governed connector-operation execution as one cmd/worker
// role. Unlike messaging_role.go, which drains internal/data/outbox, this
// role drains internal/connectivity/operation's own journal: an operation
// only ever leaves QUEUED/RETRYABLE through connectorRole.dispatchOperation,
// the single funnel that reserves fair-scheduling capacity, claims a fenced
// queue lease, binds a destination-scoped machine credential lease, and only
// then calls the journal's own credentialed dispatch (which is the only path
// that can reach a provider Writer). Every one of those four steps is
// independently required: a failure at any one returns before the next
// step runs, and an operation this role never listed out of the journal is
// never reachable at all. Nothing here imports a specific connector
// provider: provider packages remain adapters plugged in behind
// operation.CredentialWriter and connectorCredentialSource.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/operation"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/lease"
)

// ErrConnectorNotConfigured fails a dispatch attempt closed when any
// required dependency (journal, ledger, credential source, writer or
// authorizer) is missing. A partially wired role must refuse to attempt
// anything rather than silently skip the checks it cannot perform: the Go
// zero value here means "refuse", never "permitted".
var ErrConnectorNotConfigured = errors.New("worker: connector role is not configured")

// ErrConnectorDeferred reports that fair scheduling refused capacity for an
// operation this pass; the operation is left exactly as journaled (QUEUED or
// RETRYABLE) for a later sweep to retry.
var ErrConnectorDeferred = errors.New("worker: connector operation deferred by fair scheduling")

// connectorJournal is the narrow journal seam the connector role drives.
// *operation.MemoryJournal satisfies it directly. List only surfaces
// operations that already have a durable journal row; Lease is the only way
// to obtain a fenced claim; DispatchWithCredential is the only way to reach
// a provider, and it independently requires both a bound credential lease
// and a MachineLeaseAuthorizer; Recover settles work a dead worker left
// leased or sending, which List would otherwise skip forever. There is no
// path from an operation this interface never listed or leased to a
// provider call.
type connectorJournal interface {
	List(ctx context.Context, tenant string) ([]operation.Operation, error)
	Lease(ctx context.Context, req operation.LeaseRequest) (operation.Lease, error)
	DispatchWithCredential(ctx context.Context, req operation.CredentialDispatchRequest, authorizer operation.MachineLeaseAuthorizer) (operation.DispatchResult, error)
	Recover(ctx context.Context, at time.Time) ([]operation.Operation, error)
}

// connectorLedger is the fair-scheduling capacity port.
// *operation.ConnectorLedger satisfies it directly.
type connectorLedger interface {
	TryReserve(now time.Time, c operation.ScheduleCandidate) (bool, string)
	Release(operationID uuid.UUID, tenant string) bool
}

// connectorCredentialSource resolves a fresh, destination-bound machine
// credential lease immediately before dispatch. Implementations must never
// return persisted raw secret material - only a reference-only
// lease.MachineCredentialLease, exactly as
// internal/connectivity/operation/credential.go requires at the dispatch
// boundary.
type connectorCredentialSource interface {
	LeaseCredential(ctx context.Context, tenant, destination string, op custody.Operation) (lease.MachineCredentialLease, error)
}

// connectorRevalidator supplies the journal's required RevalidateFunc
// evidence. A connectorRole with no Revalidate configured always presents an
// unconfirmed operation.Revalidation{}, which MemoryJournal.Lease refuses
// (fail closed): an operation is never leased on the strength of a role that
// forgot to wire a revalidator.
type connectorRevalidator interface {
	RevalidateConnectorOperation(operation.Operation) operation.Revalidation
}

// connectorRole is the SVC-008 worker role: the only cmd/worker code
// permitted to move a journaled connector operation out of QUEUED/RETRYABLE.
type connectorRole struct {
	logger              bootstrap.Logger
	journal             connectorJournal
	ledger              connectorLedger
	credentials         connectorCredentialSource
	revalidate          connectorRevalidator
	writer              operation.CredentialWriter
	authorizer          operation.MachineLeaseAuthorizer
	credentialOperation custody.Operation
	workerID            string
	leaseFor            time.Duration
	now                 func() time.Time
}

func (r connectorRole) clock() time.Time {
	if r.now != nil {
		return r.now().UTC()
	}
	return time.Now().UTC()
}

// configured reports whether every dependency this role needs is present.
// It is checked before any governed step runs, so an incomplete role never
// gets partial credit for the checks it happened to be able to perform.
func (r connectorRole) configured() bool {
	return r.journal != nil && r.ledger != nil && r.credentials != nil && r.writer != nil && r.authorizer != nil
}

// dispatchQueued lists tenant's journaled operations and attempts exactly
// the QUEUED/RETRYABLE ones through dispatchOperation. It returns how many
// operations it attempted (regardless of outcome), so a caller can tell
// "swept, found nothing queued" apart from "swept, tried and failed".
func (r connectorRole) dispatchQueued(ctx context.Context, tenant string) (int, error) {
	if !r.configured() {
		return 0, ErrConnectorNotConfigured
	}
	ops, err := r.journal.List(ctx, tenant)
	if err != nil {
		return 0, err
	}
	now := r.clock()
	attempted := 0
	for _, op := range ops {
		if op.State != operation.StateQueued && op.State != operation.StateRetryable {
			continue
		}
		attempted++
		if _, dispatchErr := r.dispatchOperation(ctx, op, now); dispatchErr != nil && r.logger != nil {
			r.logger.Error("worker.connector_dispatch_failed",
				"operation_id", op.OperationID.String(), "tenant", tenant, "error", dispatchErr.Error())
		}
	}
	return attempted, nil
}

// dispatchOperation is the single funnel every governed step must pass
// through, in this fixed order: (1) reserve fair-scheduling capacity, (2)
// claim a fenced queue lease from the journal, (3) obtain a destination-bound
// credential lease, (4) dispatch through the journal's own credentialed
// path, which persists the attempt. A failure at any step returns
// immediately, before the next step runs and before any provider call is
// possible; the fair-scheduling reservation is always released.
func (r connectorRole) dispatchOperation(ctx context.Context, op operation.Operation, now time.Time) (operation.DispatchResult, error) {
	if !r.configured() {
		return operation.DispatchResult{}, ErrConnectorNotConfigured
	}

	candidate := operation.CandidateFromOperation(op, op.UpdatedAt)
	admitted, reason := r.ledger.TryReserve(now, candidate)
	if !admitted {
		return operation.DispatchResult{}, fmt.Errorf("%w: %s", ErrConnectorDeferred, reason)
	}
	defer r.ledger.Release(op.OperationID, op.TenantID)

	claim, err := r.journal.Lease(ctx, operation.LeaseRequest{
		TenantID: op.TenantID, OperationID: op.OperationID, WorkerID: r.workerID,
		At: now, Duration: r.leaseFor, Revalidate: r.revalidateFunc(),
	})
	if err != nil {
		return operation.DispatchResult{}, err
	}

	credential, err := r.credentials.LeaseCredential(ctx, op.TenantID, op.DestinationRef, r.credentialOperation)
	if err != nil {
		return operation.DispatchResult{}, err
	}

	result, err := r.journal.DispatchWithCredential(ctx, operation.CredentialDispatchRequest{
		Lease: claim, Credential: credential, CredentialOperation: r.credentialOperation, Writer: r.writer,
	}, r.authorizer)
	if err != nil {
		return operation.DispatchResult{}, err
	}
	if r.logger != nil {
		r.logger.Info("worker.connector_operation_dispatched",
			"operation_id", op.OperationID.String(), "state", string(result.Operation.State),
			"provider_call", result.ProviderCall, "observation_required", result.ObservationRequired)
	}
	return result, nil
}

func (r connectorRole) revalidateFunc() operation.RevalidateFunc {
	return func(current operation.Operation) operation.Revalidation {
		if r.revalidate == nil {
			return operation.Revalidation{}
		}
		return r.revalidate.RevalidateConnectorOperation(current)
	}
}

// unconfiguredConnectorCredentialSource fails closed until a real
// destination-credential minting path is wired for a specific integration.
// It is intentionally not a fake success: minting a credential lease this
// role never actually validated would let an unauthenticated provider call
// through on a forged binding.
type unconfiguredConnectorCredentialSource struct{}

func (unconfiguredConnectorCredentialSource) LeaseCredential(context.Context, string, string, custody.Operation) (lease.MachineCredentialLease, error) {
	return lease.MachineCredentialLease{}, errors.New("worker: connector credential source is not configured")
}

// unconfiguredConnectorWriter fails closed until a real provider adapter is
// wired behind operation.CredentialWriter for a specific connector.
type unconfiguredConnectorWriter struct{}

func (unconfiguredConnectorWriter) WriteWithCredential(context.Context, operation.WriteRequest, lease.MachineCredentialLease) (operation.WriteResponse, error) {
	return operation.WriteResponse{}, errors.New("worker: connector provider transport is not configured")
}

// unconfiguredMachineLeaseAuthorizer fails closed until a real tenant-scoped
// machine credential authority (internal/trust/lease.MachineManager or
// equivalent) is wired for this deployment.
type unconfiguredMachineLeaseAuthorizer struct{}

func (unconfiguredMachineLeaseAuthorizer) Use(lease.MachineCredentialLease, string, custody.Operation) (lease.MachineEvidence, error) {
	return lease.MachineEvidence{}, errors.New("worker: connector credential authorizer is not configured")
}

// connectorRoleFor builds the production connectorRole. The credential
// source, provider writer and machine-lease authorizer are all
// fail-closed stand-ins: no production destination-credential authority or
// connector provider adapter exists yet for this role, so every dispatch
// attempt refuses at the credential step until real integration wiring
// supplies them. That keeps the role safe to enable today (it never leaks a
// secret or reaches a provider) while still exercising the queue-claim and
// fair-scheduling steps against the real journal and ledger.
func connectorRoleFor(deps bootstrap.Deps, journal connectorJournal, ledger connectorLedger, workerID string, leaseFor time.Duration) connectorRole {
	return connectorRole{
		logger: deps.Logger, journal: journal, ledger: ledger,
		credentials: unconfiguredConnectorCredentialSource{}, writer: unconfiguredConnectorWriter{},
		authorizer: unconfiguredMachineLeaseAuthorizer{}, credentialOperation: custody.Encrypt,
		workerID: workerID, leaseFor: leaseFor,
	}
}

// runConnectorLoop repeatedly sweeps every active tenant's journaled
// connector operations until ctx is canceled, sleeping pollInterval between
// sweeps that found nothing to attempt. It mirrors runOutboxLoopWithTelemetry's
// shape without sharing its outbox-specific dispatcher type.
func runConnectorLoop(ctx context.Context, logger bootstrap.Logger, tenants tenantLister, role connectorRole, pollInterval time.Duration) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		didWork, err := connectorSweep(ctx, logger, tenants, role)
		if err != nil {
			logger.Error("worker.connector_sweep_failed", "error", err.Error())
		}
		if didWork {
			continue
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(pollInterval):
		}
	}
}

// connectorSweep settles work a dead worker left leased or sending and then
// attempts every active tenant's queued connector operations once, reporting
// whether any tenant had work to attempt. Recovery runs first because the
// dispatch below only lists QUEUED operations: without it an abandoned lease
// would never become dispatchable again. A recovery failure is logged and
// retried on the next pass; it never blocks healthy tenants from dispatching
// on this one.
func connectorSweep(ctx context.Context, logger bootstrap.Logger, tenants tenantLister, role connectorRole) (bool, error) {
	if recovered, err := role.journal.Recover(ctx, role.clock()); err != nil {
		logger.Error("worker.connector_recovery_failed", "error", err.Error())
	} else if len(recovered) > 0 {
		logger.Info("worker.connector_recovered", "operations", len(recovered))
	}
	ids, err := tenants.ActiveTenants(ctx)
	if err != nil {
		return false, fmt.Errorf("list tenants: %w", err)
	}
	didWork := false
	for _, tenant := range ids {
		attempted, err := role.dispatchQueued(ctx, tenant.String())
		if err != nil {
			if !errors.Is(err, ErrConnectorNotConfigured) {
				logger.Error("worker.connector_sweep_tenant_failed", "tenant", tenant.String(), "error", err.Error())
			}
			continue
		}
		if attempted > 0 {
			didWork = true
		}
	}
	return didWork, nil
}
