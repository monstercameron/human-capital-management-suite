// REV-013-01 durable connector-operation journal.
//
// The connector role used to run on operation.NewMemoryJournal: a worker
// restart between the journal commit and the provider send silently lost the
// operation, contradicting INTG-011's promise that a planned/queued operation
// is durable before a worker obtains a dispatch lease. This file backs the
// existing connectorJournal seam with Postgres through
// internal/data/connectivityopstore, so the queue membership, the fenced
// lease claim and the append-only transition trail all survive a restart.
// connectorJournalForPool keeps NewMemoryJournal only as the no-database dev
// fallback.
//
// Design notes, kept here because they constrain every method below:
//
//   - The in-process *operation.MemoryJournal stays the semantic delegate:
//     planning validation, revalidation, causal checks, fencing math and the
//     credentialed dispatch state machine run there, unchanged. Each method
//     runs the kernel call first, then persists the outcome. A persistence
//     failure therefore returns an error while memory may be ahead; that
//     direction is safe (memory dies with the process, Postgres never holds
//     a phantom) and leases self-heal through expiry.
//   - A refused claim (live lease held elsewhere, operation not ready) is a
//     governed refusal, not an outage: the store's own sentinel (ErrNotFound,
//     ErrNotReady, ErrLeaseFenced) propagates so callers keep their
//     errors.Is contract. errDurableJournal marks the outage class instead:
//     the journal is unreachable, a tenant is not a UUID, or a row the
//     storage domains require cannot be built.
//   - Fair-scheduling reservations (ConnectorLedger.TryReserve/Release) are
//     held for exactly one dispatch attempt and released by defer, so their
//     net durable effect is zero; crash safety for an in-flight reservation
//     comes from the durable queue claim, which Store.Claim requeues after
//     expiry. The ledger therefore stays in-process (main.go keeps
//     NewConnectorLedger) while every granted lease persists as a queue row.
//   - Restart visibility has a schema boundary, and it is stated plainly:
//     connector_operation carries identity, lifecycle state, fence and
//     digests, but no payload bytes, connection name, or destination column,
//     and no new migration is in scope to add one. Post-restart shells
//     rebuilt by refreshShells therefore carry the stored digests and the
//     endpoint recorded verbatim as the destination, name the connection by
//     its provisioned UUID, and resume MappedPayload as nil. Shells are
//     read-only: leasing one fails closed with operation.ErrNotFound, while
//     the durable claim underneath still fences strangers. Payload-bearing
//     operations replay through the planner, whose re-Plan/re-Queue is
//     idempotent here (base insert and queue membership are ON CONFLICT
//     guarded, already-persisted journal sequences are skipped), so a
//     redeployed planner converges without duplicates.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/operation"
	"github.com/monstercameron/human-capital-management-suite/internal/data/connectivityopstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

// errDurableJournal marks the outage class: the durable journal is
// unreachable, misconfigured, or cannot store what it was given. Governed
// refusals (fenced leases, unknown operations, blocked revalidation) keep
// their own kernel/store sentinels instead.
var errDurableJournal = errors.New("worker: durable connector journal is unavailable")

// connectorJournalForPool selects the connector-role journal: the durable
// Postgres adapter whenever a database handle is configured, and the
// in-memory journal only as the no-database dev fallback.
func connectorJournalForPool(db dbport.Beginner) connectorJournal {
	if db == nil {
		return operation.NewMemoryJournal(nil)
	}
	return newDurableConnectorJournal(db, nil)
}

// durableConnectorJournal implements connectorJournal over an in-process
// semantic delegate plus durable queue, claim and journal rows. persisted
// tracks, per operation, the highest operation_sequence already stored, so
// replays and retries never re-insert a sequence. shells holds post-restart
// read-only bodies rebuilt from Postgres; the delegate wins for every
// operation it knows.
type durableConnectorJournal struct {
	mu        sync.Mutex
	mem       *operation.MemoryJournal
	store     *connectivityopstore.Store
	db        dbport.Beginner
	now       func() time.Time
	persisted map[uuid.UUID]uint64
	shells    map[uuid.UUID]operation.Operation
}

func newDurableConnectorJournal(db dbport.Beginner, now func() time.Time) *durableConnectorJournal {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &durableConnectorJournal{
		mem:       operation.NewMemoryJournal(now),
		store:     connectivityopstore.New(db),
		db:        db,
		now:       now,
		persisted: make(map[uuid.UUID]uint64),
		shells:    make(map[uuid.UUID]operation.Operation),
	}
}

func (d *durableConnectorJournal) ready() error {
	if d == nil || d.mem == nil || d.store == nil || d.db == nil {
		return fmt.Errorf("%w: journal handle is not configured", errDurableJournal)
	}
	return nil
}

// durableTenantID gates every mutating call: Postgres rows key tenants by
// UUID, and a non-UUID tenant string must fail closed before memory mutates
// rather than diverge silently.
func durableTenantID(tenant string) (uuid.UUID, error) {
	id, err := uuid.Parse(strings.TrimSpace(tenant))
	if err != nil || id == uuid.Nil {
		return uuid.Nil, fmt.Errorf("%w: tenant %q is not a UUID", errDurableJournal, tenant)
	}
	return id, nil
}

// Plan durably records a complete operation before it can be queued or leased.
func (d *durableConnectorJournal) Plan(ctx context.Context, req operation.PlanRequest) (operation.Operation, error) {
	if err := d.ready(); err != nil {
		return operation.Operation{}, err
	}
	tenant, err := durableTenantID(req.TenantID)
	if err != nil {
		return operation.Operation{}, err
	}
	op, err := d.mem.Plan(ctx, req)
	if err != nil {
		return operation.Operation{}, err
	}
	// The base insert already carries the PLANNED state, so a replayed Plan
	// over an existing row must not rewind it: skip the state sync then.
	existed, err := d.ensureOperation(ctx, tenant, op)
	if err != nil {
		return operation.Operation{}, err
	}
	if err := d.persistNewEvents(ctx, tenant, op.TenantID, op.OperationID); err != nil {
		return operation.Operation{}, err
	}
	if !existed {
		if err := d.syncBaseState(ctx, tenant, op); err != nil {
			return operation.Operation{}, err
		}
	}
	return op, nil
}

// Queue makes a planned operation eligible for a dispatch lease, durably.
func (d *durableConnectorJournal) Queue(ctx context.Context, tenant string, id uuid.UUID) (operation.Operation, error) {
	if err := d.ready(); err != nil {
		return operation.Operation{}, err
	}
	tenantID, err := durableTenantID(tenant)
	if err != nil {
		return operation.Operation{}, err
	}
	op, err := d.mem.Queue(ctx, tenant, id)
	if err != nil {
		return operation.Operation{}, err
	}
	queued, err := d.ensureOperation(ctx, tenantID, op)
	if err != nil {
		return operation.Operation{}, err
	}
	if !queued {
		if err := d.store.Enqueue(ctx, tenantID, id, connectivityopstore.QueueItem{AvailableAt: d.now().UTC()}); err != nil {
			return operation.Operation{}, fmt.Errorf("worker: durable enqueue for %s: %w", id, err)
		}
	}
	if err := d.persistNewEvents(ctx, tenantID, tenant, id); err != nil {
		return operation.Operation{}, err
	}
	if err := d.syncBaseState(ctx, tenantID, op); err != nil {
		return operation.Operation{}, err
	}
	return op, nil
}

// Get returns a defensive copy, from the delegate or, after a restart, from
// the durable shell.
func (d *durableConnectorJournal) Get(ctx context.Context, tenant string, id uuid.UUID) (operation.Operation, error) {
	if err := d.ready(); err != nil {
		return operation.Operation{}, err
	}
	if op, err := d.mem.Get(ctx, tenant, id); err == nil {
		return op, nil
	} else if !errors.Is(err, operation.ErrNotFound) {
		return operation.Operation{}, err
	}
	if tenantID, perr := durableTenantID(tenant); perr == nil {
		d.refreshShells(ctx, tenantID)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if shell, ok := d.shells[id]; ok && shell.TenantID == tenant {
		return shell, nil
	}
	return operation.Operation{}, fmt.Errorf("%w: %s", operation.ErrNotFound, id)
}

// List returns delegate operations plus durable shells for anything the
// delegate no longer knows, in the kernel's resource/sequence/id order.
// A refresh failure fails the sweep closed: dispatching from memory while
// the durable substrate is unreachable would lose the attempt record.
func (d *durableConnectorJournal) List(ctx context.Context, tenant string) ([]operation.Operation, error) {
	if err := d.ready(); err != nil {
		return nil, err
	}
	ops, err := d.mem.List(ctx, tenant)
	if err != nil {
		return nil, err
	}
	if tenantID, perr := durableTenantID(tenant); perr == nil {
		if err := d.refreshShells(ctx, tenantID); err != nil {
			return nil, err
		}
	}
	known := make(map[uuid.UUID]bool, len(ops))
	for _, op := range ops {
		known[op.OperationID] = true
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for id, shell := range d.shells {
		if shell.TenantID == tenant && !known[id] {
			ops = append(ops, shell)
		}
	}
	// The kernel's List order: resource, sequence, id.
	sort.Slice(ops, func(a, b int) bool {
		if ops[a].ExternalResourceKey != ops[b].ExternalResourceKey {
			return ops[a].ExternalResourceKey < ops[b].ExternalResourceKey
		}
		if ops[a].ResourceSequence != ops[b].ResourceSequence {
			return ops[a].ResourceSequence < ops[b].ResourceSequence
		}
		return ops[a].OperationID.String() < ops[b].OperationID.String()
	})
	return ops, nil
}

// Journal returns the delegate's in-memory trail. The durable trail is read
// through connectivityopstore directly.
func (d *durableConnectorJournal) Journal(ctx context.Context, tenant string, id uuid.UUID) ([]operation.JournalEvent, error) {
	if err := d.ready(); err != nil {
		return nil, err
	}
	return d.mem.Journal(ctx, tenant, id)
}

// VerifyJournal checks the delegate's global sequence and hash chain.
func (d *durableConnectorJournal) VerifyJournal(ctx context.Context) error {
	if err := d.ready(); err != nil {
		return err
	}
	return d.mem.VerifyJournal(ctx)
}

// Lease revalidates at the dispatch boundary and grants a fenced lease,
// then fences it durably: without the queue claim the lease is refused and
// no provider call can follow, even when memory already advanced.
func (d *durableConnectorJournal) Lease(ctx context.Context, req operation.LeaseRequest) (operation.Lease, error) {
	if err := d.ready(); err != nil {
		return operation.Lease{}, err
	}
	tenant, err := durableTenantID(req.TenantID)
	if err != nil {
		return operation.Lease{}, err
	}
	granted, err := d.mem.Lease(ctx, req)
	if err != nil {
		// A blocked revalidation still marks memory (repair-required or
		// rejected); mirror the marking durably on a best effort basis so
		// the rows track the delegate, then report the original refusal.
		d.syncAfterFailure(ctx, tenant, req.TenantID, req.OperationID)
		return operation.Lease{}, err
	}
	body, err := d.mem.Get(ctx, req.TenantID, req.OperationID)
	if err != nil {
		return operation.Lease{}, err
	}
	if _, err := d.ensureOperation(ctx, tenant, body); err != nil {
		return operation.Lease{}, err
	}
	at := req.At.UTC()
	if at.IsZero() {
		at = d.now().UTC()
	}
	if _, err := d.store.Claim(ctx, tenant, req.OperationID, req.WorkerID, at, req.Duration); err != nil {
		return operation.Lease{}, fmt.Errorf("worker: durable queue claim for %s: %w", req.OperationID, err)
	}
	if err := d.persistNewEvents(ctx, tenant, req.TenantID, req.OperationID); err != nil {
		return operation.Lease{}, err
	}
	if err := d.syncBaseState(ctx, tenant, body); err != nil {
		return operation.Lease{}, err
	}
	return granted, nil
}

// DispatchWithCredential binds and consumes the machine credential lease
// immediately before dispatch, persisting the attempt row, the transition
// trail and the reference-only binding evidence. A provider call already
// made is irreversible, so a late evidence failure still reports the result
// alongside the error.
func (d *durableConnectorJournal) DispatchWithCredential(ctx context.Context, req operation.CredentialDispatchRequest, authorizer operation.MachineLeaseAuthorizer) (operation.DispatchResult, error) {
	if err := d.ready(); err != nil {
		return operation.DispatchResult{}, err
	}
	tenant, err := durableTenantID(req.Lease.TenantID)
	if err != nil {
		return operation.DispatchResult{}, err
	}
	result, err := d.mem.DispatchWithCredential(ctx, req, authorizer)
	if err != nil {
		d.syncAfterFailure(ctx, tenant, req.Lease.TenantID, req.Lease.OperationID)
		return operation.DispatchResult{}, err
	}
	if err := d.ensureAttemptRow(ctx, tenant, result.Attempt); err != nil {
		return result, err
	}
	if err := d.persistNewEvents(ctx, tenant, req.Lease.TenantID, req.Lease.OperationID); err != nil {
		return result, err
	}
	if err := d.syncBaseState(ctx, tenant, result.Operation); err != nil {
		return result, err
	}
	binding := connectivityopstore.CredentialBinding{
		BindingID:          uuid.New(),
		OperationID:        req.Lease.OperationID,
		AttemptID:          result.Attempt.AttemptID,
		CredentialLeaseRef: req.Credential.Lease.ID,
		WorkloadRef:        req.Credential.WorkloadIdentity,
		DestinationRef:     result.Operation.DestinationRef,
		Purpose:            string(req.CredentialOperation),
		CustodyOperation:   string(req.CredentialOperation),
		LeaseExpiresAt:     req.Credential.Lease.ExpiresAt,
		RevocationEpoch:    req.Credential.RevocationEpoch,
		Outcome:            "BOUND",
		BoundAt:            d.now().UTC(),
	}
	if err := d.store.BindCredentialLease(ctx, tenant, binding); err != nil {
		return result, fmt.Errorf("worker: durable credential binding for %s: %w", req.Lease.OperationID, err)
	}
	return result, nil
}

// Recover settles work left leased or sending after a crash, persisting each
// recovery event. It reports the recovered operations together with the
// first durability gap, if any, so evidence loss is loud.
func (d *durableConnectorJournal) Recover(ctx context.Context, at time.Time) ([]operation.Operation, error) {
	if err := d.ready(); err != nil {
		return nil, err
	}
	if at.IsZero() {
		at = d.now().UTC()
	}
	recovered, err := d.mem.Recover(ctx, at)
	if err != nil {
		return nil, err
	}
	var firstErr error
	for _, op := range recovered {
		tenant, terr := durableTenantID(op.TenantID)
		if terr != nil {
			if firstErr == nil {
				firstErr = terr
			}
			continue
		}
		if _, err := d.ensureOperation(ctx, tenant, op); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if err := d.persistNewEvents(ctx, tenant, op.TenantID, op.OperationID); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if err := d.syncBaseState(ctx, tenant, op); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return recovered, firstErr
}

// syncAfterFailure mirrors a delegate state change made while refusing or
// failing a call (revalidation block, fenced or failed dispatch) into the
// durable rows. It is best effort: the original error dominates.
func (d *durableConnectorJournal) syncAfterFailure(ctx context.Context, tenant uuid.UUID, tenantStr string, id uuid.UUID) {
	body, err := d.mem.Get(ctx, tenantStr, id)
	if err != nil {
		return
	}
	if _, err := d.ensureOperation(ctx, tenant, body); err != nil {
		return
	}
	if err := d.persistNewEvents(ctx, tenant, tenantStr, id); err != nil {
		return
	}
	_ = d.syncBaseState(ctx, tenant, body)
}

// persistNewEvents appends every delegate event beyond the highest persisted
// operation_sequence. The per-success map update makes retries idempotent:
// a sequence stored before a crash is never stored twice.
func (d *durableConnectorJournal) persistNewEvents(ctx context.Context, tenant uuid.UUID, tenantStr string, id uuid.UUID) error {
	events, err := d.mem.Journal(ctx, tenantStr, id)
	if err != nil {
		return err
	}
	floor, err := d.persistedFloor(ctx, tenant, id)
	if err != nil {
		return err
	}
	for _, event := range events {
		if event.OperationSequence <= floor {
			continue
		}
		if err := d.store.AppendJournal(ctx, tenant, event); err != nil {
			return fmt.Errorf("%w: append journal event %s seq %d: %v", errDurableJournal, id, event.OperationSequence, err)
		}
		floor = event.OperationSequence
		d.mu.Lock()
		if event.OperationSequence > d.persisted[id] {
			d.persisted[id] = event.OperationSequence
		}
		d.mu.Unlock()
	}
	return nil
}

// persistedFloor returns the highest stored operation_sequence for id,
// seeding it from the durable trail on first sight so a fresh handle that
// replays an already-planned operation skips what is already there instead
// of colliding with it.
func (d *durableConnectorJournal) persistedFloor(ctx context.Context, tenant, id uuid.UUID) (uint64, error) {
	d.mu.Lock()
	floor, seen := d.persisted[id]
	d.mu.Unlock()
	if seen {
		return floor, nil
	}
	trail, err := d.store.Journal(ctx, tenant, id)
	if err != nil {
		return 0, fmt.Errorf("%w: read journal floor for %s: %v", errDurableJournal, id, err)
	}
	for _, event := range trail {
		if event.OperationSequence > floor {
			floor = event.OperationSequence
		}
	}
	d.mu.Lock()
	if floor < d.persisted[id] {
		floor = d.persisted[id]
	} else {
		d.persisted[id] = floor
	}
	d.mu.Unlock()
	return floor, nil
}

// --- durable operation rows (adapter-owned SQL) ------------------------------
// The base operation row and its connection parents are not owned by
// connectivityopstore (its charter is the 00235-00237 queue/journal/lease
// triad), so the adapter ensures them here with plain dbport statements.
// Every statement runs under the tenant scope, like the store itself.

// durablePilotID derives the stable UUID for one pilot provisioning row
// (system, definition or connection) from its tenant and name. Determinism
// is what makes re-Plan idempotent: the same connection name always maps to
// the same connection_id, so replays never fork duplicates.
func durablePilotID(tenant uuid.UUID, scope, name string) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(tenant.String()+"\x00"+scope+"\x00"+name))
}

// ensureOperation inserts the connection parents and the base operation row
// when absent (ON CONFLICT DO NOTHING everywhere, so planner replays are
// safe) and reports whether the queue membership already exists, so Queue
// never re-inserts it either.
func (d *durableConnectorJournal) ensureOperation(ctx context.Context, tenant uuid.UUID, op operation.Operation) (queued bool, err error) {
	if strings.TrimSpace(op.ConnectionID) == "" {
		return false, fmt.Errorf("%w: connection is required to persist operation %s", errDurableJournal, op.OperationID)
	}
	if op.CausalPredecessorID != uuid.Nil {
		exists, err := d.operationRowExists(ctx, tenant, op.CausalPredecessorID)
		if err != nil {
			return false, err
		}
		if !exists {
			return false, fmt.Errorf("%w: durable predecessor %s of %s is not planned", errDurableJournal, op.CausalPredecessorID, op.OperationID)
		}
	}
	systemID := durablePilotID(tenant, "system", op.ConnectionID)
	definitionID := durablePilotID(tenant, "definition", op.ConnectionID)
	connectionID := durablePilotID(tenant, "connection", op.ConnectionID)
	canonicalDigest, err := digestForStorage("canonical", op.CanonicalInputDigest)
	if err != nil {
		return false, err
	}
	mappedDigest, err := digestForStorage("mapped", op.MappedPayloadDigest)
	if err != nil {
		return false, err
	}
	authorityDigest, err := digestForStorage("authority", op.AuthorityPolicyFingerprint)
	if err != nil {
		return false, err
	}
	effectRef := op.BusinessTransactionID
	if strings.TrimSpace(effectRef) == "" {
		effectRef = "effect:" + op.OperationID.String()
	}
	workflowRef := op.WorkflowInstanceID
	if strings.TrimSpace(workflowRef) == "" {
		workflowRef = "workflow:" + op.OperationID.String()
	}
	sequenceNo := int64(op.ResourceSequence)
	if sequenceNo < 1 {
		sequenceNo = 1
	}
	fence := int64(op.FenceToken)
	if fence < 1 {
		fence = 1
	}
	var expectedVersion any
	if strings.TrimSpace(op.ExpectedExternalVersion) != "" {
		expectedVersion = op.ExpectedExternalVersion
	}
	var predecessor any
	if op.CausalPredecessorID != uuid.Nil {
		predecessor = op.CausalPredecessorID
	}

	tx, err := d.db.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("%w: begin ensure operation %s: %v", errDurableJournal, op.OperationID, err)
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO external_system
			(tenant_id, system_id, system_key, vendor, product, environment, residency_region, data_classification, owner_principal_ref)
		VALUES ($1, $2, $3, 'pilot-vendor', 'pilot-product', 'TEST', 'us-east-1', $4, 'pilot-owner')
		ON CONFLICT (tenant_id, system_id) DO NOTHING`,
		tenant, systemID, "pilot:"+op.ConnectionID, op.Classification); err != nil {
		return false, fmt.Errorf("%w: ensure external system: %v", errDurableJournal, err)
	}
	descriptorDigest, err := digestForStorage("descriptor", "pilot:"+op.ConnectionID)
	if err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO connector_definition
			(tenant_id, connector_id, connector_key, connector_version, vendor, auth_mode, write_mode,
			 idempotency_semantics, observation_semantics, descriptor_digest)
		VALUES ($1, $2, $3, 1, 'pilot-vendor', 'NONE', 'IDEMPOTENT', 'KEY', 'READ_BACK', $4)
		ON CONFLICT (tenant_id, connector_id) DO NOTHING`,
		tenant, definitionID, "pilot:"+op.ConnectionID, descriptorDigest); err != nil {
		return false, fmt.Errorf("%w: ensure connector definition: %v", errDurableJournal, err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO connector_connection
			(tenant_id, connection_id, system_id, connector_id, environment, endpoint, credential_ref, residency_region)
		VALUES ($1, $2, $3, $4, 'TEST', $5, 'secretref://pilot/default', 'us-east-1')
		ON CONFLICT (tenant_id, connection_id) DO NOTHING`,
		tenant, connectionID, systemID, definitionID, op.DestinationRef); err != nil {
		return false, fmt.Errorf("%w: ensure connector connection: %v", errDurableJournal, err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO connector_operation
			(tenant_id, operation_id, connection_id, causal_predecessor_id, sequence_no, effect_ref, workflow_ref,
			 semantic_operation, resource_key, expected_external_version, canonical_payload_digest, mapped_payload_digest,
			 idempotency_key, fence_token, authority_digest, classification, deadline_at, state, completion_state,
			 created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21)
		ON CONFLICT (tenant_id, operation_id) DO NOTHING`,
		tenant, op.OperationID, connectionID, predecessor, sequenceNo, effectRef, workflowRef,
		op.SemanticOperation, op.ExternalResourceKey, expectedVersion, canonicalDigest, mappedDigest,
		op.IdempotencyKey, fence, authorityDigest, op.Classification, op.DeadlineAt.UTC(), string(op.State), string(op.CompletionState),
		op.CreatedAt.UTC(), op.UpdatedAt.UTC()); err != nil {
		return false, fmt.Errorf("%w: ensure operation row: %v", errDurableJournal, err)
	}
	var one int
	if err := tx.QueryRow(ctx, `SELECT 1 FROM connector_operation_queue WHERE tenant_id = $1 AND operation_id = $2`, tenant, op.OperationID).Scan(&one); err != nil && !errors.Is(err, dbport.ErrNoRows) {
		return false, fmt.Errorf("%w: read queue membership: %v", errDurableJournal, err)
	} else if err == nil {
		queued = true
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("%w: commit ensure operation %s: %v", errDurableJournal, op.OperationID, err)
	}
	return queued, nil
}

func (d *durableConnectorJournal) operationRowExists(ctx context.Context, tenant, id uuid.UUID) (bool, error) {
	tx, err := d.db.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("%w: begin predecessor check: %v", errDurableJournal, err)
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		return false, err
	}
	var one int
	if err := tx.QueryRow(ctx, `SELECT 1 FROM connector_operation WHERE tenant_id = $1 AND operation_id = $2`, tenant, id).Scan(&one); err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("%w: read predecessor row: %v", errDurableJournal, err)
	}
	return true, nil
}

// syncBaseState carries the delegate's lifecycle state, fence and completion
// onto the base row. The base table has no append-only trigger, so UPDATE is
// its governed write.
func (d *durableConnectorJournal) syncBaseState(ctx context.Context, tenant uuid.UUID, op operation.Operation) error {
	fence := int64(op.FenceToken)
	if fence < 1 {
		fence = 1
	}
	var observation any
	if op.ObservationID != uuid.Nil {
		observation = op.ObservationID
	}
	tx, err := d.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("%w: begin sync operation %s: %v", errDurableJournal, op.OperationID, err)
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE connector_operation
		SET state = $3, completion_state = $4, observation_id = $5, fence_token = $6, updated_at = $7
		WHERE tenant_id = $1 AND operation_id = $2`,
		tenant, op.OperationID, string(op.State), string(op.CompletionState), observation, fence, op.UpdatedAt.UTC()); err != nil {
		return fmt.Errorf("%w: sync operation row: %v", errDurableJournal, err)
	}
	return tx.Commit(ctx)
}

// ensureAttemptRow persists one immutable provider round trip ahead of the
// journal event that references it. Replays are safe: the insert is keyed on
// the attempt identity.
func (d *durableConnectorJournal) ensureAttemptRow(ctx context.Context, tenant uuid.UUID, attempt operation.Attempt) error {
	requestDigest, err := digestForStorage("request", attempt.RequestDigest)
	if err != nil {
		return err
	}
	var responseDigest any
	if strings.TrimSpace(attempt.ResponseDigest) != "" {
		stored, err := digestForStorage("response", attempt.ResponseDigest)
		if err != nil {
			return err
		}
		responseDigest = stored
	}
	tx, err := d.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("%w: begin ensure attempt: %v", errDurableJournal, err)
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO connector_operation_attempt
			(tenant_id, attempt_id, operation_id, attempt_number, request_digest, response_digest,
			 provider_request_id, fence_token, attempted_at, received_at, provider_result, retry_disposition)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (tenant_id, attempt_id) DO NOTHING`,
		tenant, attempt.AttemptID, attempt.OperationID, attempt.AttemptNumber, requestDigest, responseDigest,
		attempt.ProviderRequestID, int64(attempt.FenceToken), attempt.AttemptedAt.UTC(), attempt.ReceivedAt.UTC(),
		string(attempt.ProviderResult), string(attempt.RetryDisposition)); err != nil {
		return fmt.Errorf("%w: ensure attempt row: %v", errDurableJournal, err)
	}
	return tx.Commit(ctx)
}

// digestForStorage encodes one kernel digest string into the content_digest
// storage domain (64 lowercase hex). Values already in that shape, with or
// without the sha256: prefix the journal uses, round-trip exactly; legacy
// fingerprints hash deterministically instead of failing the row.
func digestForStorage(label, value string) (string, error) {
	if value == "" {
		return "", fmt.Errorf("%w: %s digest is required", errDurableJournal, label)
	}
	if stripped := strings.TrimPrefix(value, "sha256:"); isHexDigest(stripped) {
		return strings.ToLower(stripped), nil
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:]), nil
}

func isHexDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

// --- restart shells ----------------------------------------------------------

// durableOperationRow is one connector_operation row read back for shell
// hydration.
type durableOperationRow struct {
	operationID     uuid.UUID
	connectionID    uuid.UUID
	predecessor     *uuid.UUID
	sequenceNo      int64
	effectRef       string
	workflowRef     string
	semanticOp      string
	resourceKey     string
	expectedVersion *string
	canonicalDigest string
	mappedDigest    string
	idempotencyKey  string
	fence           int64
	authorityDigest string
	classification  string
	deadline        time.Time
	state           string
	completion      string
	observation     *uuid.UUID
	createdAt       time.Time
	updatedAt       time.Time
}

// durableQueueInfo is one connector_operation_queue row read back for shell
// hydration.
type durableQueueInfo struct {
	state   string
	fence   int64
	expires *time.Time
}

// refreshShells rebuilds read-only operation bodies from the durable rows
// for every operation the delegate no longer knows. It only replaces
// shells, never delegate state, and takes no adapter lock while reading.
func (d *durableConnectorJournal) refreshShells(ctx context.Context, tenant uuid.UUID) error {
	now := d.now().UTC()
	known := make(map[uuid.UUID]bool)
	if live, err := d.mem.List(ctx, tenant.String()); err != nil {
		return err
	} else {
		for _, op := range live {
			known[op.OperationID] = true
		}
	}
	tx, err := d.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("%w: begin shell refresh: %v", errDurableJournal, err)
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT operation_id, connection_id, causal_predecessor_id, sequence_no,
			effect_ref, workflow_ref, semantic_operation, resource_key, expected_external_version,
			canonical_payload_digest, mapped_payload_digest, idempotency_key, fence_token, authority_digest,
			classification, deadline_at, state, completion_state, observation_id, created_at, updated_at
		FROM connector_operation WHERE tenant_id = $1 ORDER BY created_at, operation_id`, tenant)
	if err != nil {
		return fmt.Errorf("%w: read operation rows: %v", errDurableJournal, err)
	}
	var found []durableOperationRow
	func() {
		defer rows.Close()
		for rows.Next() {
			var row durableOperationRow
			if err := rows.Scan(&row.operationID, &row.connectionID, &row.predecessor, &row.sequenceNo,
				&row.effectRef, &row.workflowRef, &row.semanticOp, &row.resourceKey, &row.expectedVersion,
				&row.canonicalDigest, &row.mappedDigest, &row.idempotencyKey, &row.fence, &row.authorityDigest,
				&row.classification, &row.deadline, &row.state, &row.completion, &row.observation,
				&row.createdAt, &row.updatedAt); err != nil {
				return
			}
			found = append(found, row)
		}
	}()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("%w: read operation rows: %v", errDurableJournal, err)
	}
	queueRows, err := tx.Query(ctx, `SELECT operation_id, queue_state, fence_token, expires_at
		FROM connector_operation_queue WHERE tenant_id = $1`, tenant)
	if err != nil {
		return fmt.Errorf("%w: read queue rows: %v", errDurableJournal, err)
	}
	queues := make(map[uuid.UUID]durableQueueInfo)
	func() {
		defer queueRows.Close()
		for queueRows.Next() {
			var id uuid.UUID
			var info durableQueueInfo
			if err := queueRows.Scan(&id, &info.state, &info.fence, &info.expires); err != nil {
				return
			}
			queues[id] = info
		}
	}()
	if err := queueRows.Err(); err != nil {
		return fmt.Errorf("%w: read queue rows: %v", errDurableJournal, err)
	}
	endpoints := make(map[uuid.UUID]string)
	endpointRows, err := tx.Query(ctx, `SELECT connection_id, endpoint FROM connector_connection WHERE tenant_id = $1`, tenant)
	if err != nil {
		return fmt.Errorf("%w: read connection rows: %v", errDurableJournal, err)
	}
	func() {
		defer endpointRows.Close()
		for endpointRows.Next() {
			var id uuid.UUID
			var endpoint string
			if err := endpointRows.Scan(&id, &endpoint); err != nil {
				return
			}
			endpoints[id] = endpoint
		}
	}()
	if err := endpointRows.Err(); err != nil {
		return fmt.Errorf("%w: read connection rows: %v", errDurableJournal, err)
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	for _, row := range found {
		if known[row.operationID] {
			continue
		}
		d.shells[row.operationID] = buildShell(tenant, row, queues[row.operationID], endpoints[row.connectionID], now)
	}
	return nil
}

// buildShell maps one durable row triple onto the read-only operation body.
// Fields the schema holds round-trip exactly; the documented gaps are the
// process-held execution material (MappedPayload bytes, attempts, redrive
// lineage) and the kernel-only planning inputs, which restart shells do not
// need because shells never execute: leasing one fails closed.
func buildShell(tenant uuid.UUID, row durableOperationRow, queue durableQueueInfo, endpoint string, now time.Time) operation.Operation {
	op := operation.Operation{
		OperationID:                row.operationID,
		TenantID:                   tenant.String(),
		ConnectionID:               row.connectionID.String(),
		BusinessTransactionID:      row.effectRef,
		WorkflowInstanceID:         row.workflowRef,
		SemanticOperation:          row.semanticOp,
		ExternalResourceKey:        row.resourceKey,
		CanonicalInputDigest:       row.canonicalDigest,
		MappedPayloadDigest:        row.mappedDigest,
		IdempotencyKey:             row.idempotencyKey,
		ExternalIdempotencyKey:     row.idempotencyKey,
		ObservationRequirement:     operation.ObservationBySemanticIdentity,
		Classification:             row.classification,
		DestinationRef:             endpoint,
		DeadlineAt:                 row.deadline.UTC(),
		CreatedAt:                  row.createdAt.UTC(),
		UpdatedAt:                  row.updatedAt.UTC(),
		State:                      shellState(row.state, queue, now),
		CompletionState:            row.completion,
		AuthorityPolicyFingerprint: row.authorityDigest,
	}
	if row.predecessor != nil {
		op.CausalPredecessorID = *row.predecessor
	}
	if row.sequenceNo > 0 {
		op.ResourceSequence = uint64(row.sequenceNo)
	}
	if row.expectedVersion != nil {
		op.ExpectedExternalVersion = *row.expectedVersion
	}
	if row.fence > 0 {
		op.FenceToken = uint64(row.fence)
	}
	if queue.fence > 0 && (queue.state == "LEASED" || queue.state == "QUEUED" || queue.state == "REQUEUE") {
		op.FenceToken = uint64(queue.fence)
	}
	if row.observation != nil {
		op.ObservationID = *row.observation
	}
	return op
}

// shellState derives the effective lifecycle state: the queue claim is the
// truth while the base row is still early (planned, queued or leased), with
// an expired claim reading as queued; once the base row has advanced past
// the queue (dispatched, failed, terminal), the leftover claim no longer
// speaks and the base state wins.
func shellState(base string, queue durableQueueInfo, now time.Time) operation.State {
	switch base {
	case string(operation.StatePlanned), string(operation.StateQueued), string(operation.StateLeased):
		switch queue.state {
		case "LEASED":
			if queue.expires == nil || now.Before(*queue.expires) {
				return operation.StateLeased
			}
			return operation.StateQueued
		case "QUEUED", "REQUEUE":
			return operation.StateQueued
		case "":
			return mapBaseState(base)
		default:
			return operation.State(queue.state)
		}
	default:
		return mapBaseState(base)
	}
}

// mapBaseState carries a stored state string through, mapping every kernel
// lifecycle state onto itself and unknown values through raw so foreign rows
// stay visible instead of failing the sweep.
func mapBaseState(state string) operation.State {
	switch operation.State(state) {
	case operation.StatePlanned,
		operation.StateQueued,
		operation.StateLeased,
		operation.StateSending,
		operation.StateSent,
		operation.StateProviderAccepted,
		operation.StateObserving,
		operation.StateReconciled,
		operation.StateFailed,
		operation.StateRetryable,
		operation.StateDeadLetter,
		operation.StateAmbiguous,
		operation.StateRepairRequired,
		operation.StateRejected:
		return operation.State(state)
	default:
		return operation.State(state)
	}
}
