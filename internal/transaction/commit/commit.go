// Package commit owns the local PostgreSQL commit boundary for a prepared
// transaction plan. It keeps the ledger, rebuildable critical checkpoints,
// outbox intent and replay receipt in the same caller-owned transaction.
package commit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	datalogger "github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/data/outbox"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projection"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/conflict"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/plan"
)

var (
	ErrInvalidPlan   = errors.New("transaction commit: invalid plan")
	ErrAlreadyOpen   = errors.New("transaction commit: transaction is already open")
	ErrReceiptDecode = errors.New("transaction commit: durable receipt is unreadable")
)

// DefaultProjectionName is the checkpoint used when a caller does not select
// a domain-specific critical projection.
const DefaultProjectionName = "transaction.commit"

// Failpoint is called at deterministic points before the caller's transaction
// commits. Returning an error proves that all earlier local writes roll back.
type Failpoint func(stage string) error

// Options controls the commit adapter without widening TransactionPlan. The
// clock is injected so crash-boundary tests do not depend on wall time.
type Options struct {
	ProjectionName string
	Clock          func() time.Time
	Failpoint      Failpoint
	// ConflictFence executes a conflict reference already bound into the plan.
	// Legacy plans with no reference retain the existing stream-head CAS.
	ConflictFence ConflictFence
}

// ConflictFence closes a registered write intent inside the transaction that
// also appends the plan's domain events and outbox records.
type ConflictFence interface {
	ValidateAtCommit(context.Context, dbport.Tx, conflict.CommitRequest) (conflict.CommitResult, error)
}

// Receipt is the durable result of one local plan commit. Replayed is true
// only when the result came from the already-completed idempotency record.
type Receipt struct {
	ReceiptID   uuid.UUID
	Tenant      uuid.UUID
	PlanID      string
	PlanDigest  string
	Events      []datalogger.AppendReceipt
	Projections []projection.ApplyResult
	Outbox      []outbox.Record
	Replayed    bool
}

// Committer opens exactly one transaction for Commit. CommitInTx is provided
// for workflow TerminalWriter adapters that already own the transaction.
type Committer struct {
	db   dbport.Beginner
	opts Options
}

// New constructs a committer. The database handle is the only object allowed
// to open or finish the PostgreSQL transaction.
func New(db dbport.Beginner, opts Options) *Committer {
	if opts.ProjectionName == "" {
		opts.ProjectionName = DefaultProjectionName
	}
	if opts.Clock == nil {
		opts.Clock = func() time.Time { return time.Now().UTC() }
	}
	return &Committer{db: db, opts: opts}
}

// Commit opens a transaction, runs CommitInTx, and publishes no external
// effect. Outbox dispatch is intentionally outside this database boundary.
func (c *Committer) Commit(ctx context.Context, prepared plan.TransactionPlan) (Receipt, error) {
	if c == nil || c.db == nil {
		return Receipt{}, errors.New("transaction commit: database is required")
	}
	tx, err := c.db.Begin(ctx)
	if err != nil {
		return Receipt{}, fmt.Errorf("transaction commit: begin: %w", err)
	}
	defer tx.Rollback(ctx)
	receipt, err := c.CommitInTx(ctx, tx, prepared)
	if err != nil {
		return Receipt{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return receipt, fmt.Errorf("transaction commit: commit acknowledgement is ambiguous: %w", err)
	}
	if err := c.fail("after-commit"); err != nil {
		// The transaction is already durable. Returning the receipt with the
		// injected error models a crash at the acknowledgement boundary: a
		// caller must replay by idempotency key, never execute a second effect.
		return receipt, err
	}
	return receipt, nil
}

// CommitPlan is a descriptive package-level convenience for callers that do
// not need to retain a Committer.
func CommitPlan(ctx context.Context, db dbport.Beginner, prepared plan.TransactionPlan, opts Options) (Receipt, error) {
	return New(db, opts).Commit(ctx, prepared)
}

// CommitInTx appends every planned stream in canonical order, advances one
// checkpoint per event, enqueues every declared effect, and completes the
// durable idempotency receipt before returning. The caller owns Commit or
// Rollback of tx.
func (c *Committer) CommitInTx(ctx context.Context, tx dbport.Tx, prepared plan.TransactionPlan) (Receipt, error) {
	if c == nil {
		return Receipt{}, errors.New("transaction commit: committer is required")
	}
	if tx == nil {
		return Receipt{}, errors.New("transaction commit: transaction is required")
	}
	declaresFence := strings.TrimSpace(prepared.ConflictIntentID) != "" || strings.TrimSpace(prepared.ConflictSnapshotDigest) != ""
	if declaresFence && (strings.TrimSpace(prepared.ConflictIntentID) == "" || strings.TrimSpace(prepared.ConflictSnapshotDigest) == "") {
		return Receipt{}, fmt.Errorf("%w: conflict intent id and snapshot digest must be supplied together", ErrInvalidPlan)
	}
	if declaresFence && c.opts.ConflictFence == nil {
		return Receipt{}, fmt.Errorf("%w: plan declares conflict intent %s but no durable fence is configured", ErrInvalidPlan, prepared.ConflictIntentID)
	}
	if declaresFence && len(prepared.ConflictFootprintDigests) == 0 {
		return Receipt{}, fmt.Errorf("%w: plan declares conflict intent %s without footprint digests", ErrInvalidPlan, prepared.ConflictIntentID)
	}
	if declaresFence {
		for _, write := range prepared.Writes {
			if !write.Operation.Valid() {
				return Receipt{}, fmt.Errorf("%w: fenced write %s has no declared operation", ErrInvalidPlan, write.FieldPath)
			}
			if err := write.EffectiveInterval.Validate(); err != nil {
				return Receipt{}, fmt.Errorf("%w: fenced write %s has invalid effective interval: %v", ErrInvalidPlan, write.FieldPath, err)
			}
		}
	}
	if !declaresFence && c.opts.ConflictFence != nil {
		return Receipt{}, fmt.Errorf("%w: durable fence cannot be supplied for a plan with no bound conflict intent", ErrInvalidPlan)
	}
	if err := prepared.VerifyDigest(); err != nil {
		return Receipt{}, fmt.Errorf("%w: %v", ErrInvalidPlan, err)
	}
	tenant, err := uuid.Parse(string(prepared.Tenant))
	if err != nil {
		return Receipt{}, fmt.Errorf("%w: tenant %q is not a UUID: %v", ErrInvalidPlan, prepared.Tenant, err)
	}
	now := c.opts.Clock().UTC()
	if now.IsZero() || !prepared.ExpiresAt.Time().After(now) {
		return Receipt{}, fmt.Errorf("%w: plan %s is expired at %s", ErrInvalidPlan, prepared.PlanID, prepared.ExpiresAt)
	}
	if strings.TrimSpace(prepared.IdempotencyKey) == "" {
		return Receipt{}, fmt.Errorf("%w: idempotency key is required", ErrInvalidPlan)
	}
	if err := setTenant(ctx, tx, tenant); err != nil {
		return Receipt{}, err
	}

	scope := idempotency.Scope{
		Tenant: tenant, Capability: "transaction.commit",
		EffectScope: prepared.PlanID, Key: prepared.IdempotencyKey,
	}
	digest := digestHex(prepared.Digest)
	retention := prepared.ExpiresAt.Time().Sub(now)
	if retention <= 0 {
		return Receipt{}, fmt.Errorf("%w: plan expiry is not in the future", ErrInvalidPlan)
	}
	effectiveByStream := map[string]time.Time{}
	if declaresFence {
		for _, write := range prepared.Writes {
			start, ok := write.EffectiveInterval.StartInstant()
			if !ok {
				return Receipt{}, fmt.Errorf("%w: fenced write %s has a local-date interval that cannot map to ledger effective_at without a timezone", ErrInvalidPlan, write.FieldPath)
			}
			stream := write.ExpectedRevision.Stream()
			if existing, found := effectiveByStream[stream]; found && !existing.Equal(start.Time()) {
				return Receipt{}, fmt.Errorf("%w: stream %s has multiple effective starts", ErrInvalidPlan, stream)
			}
			effectiveByStream[stream] = start.Time().UTC()
		}
	}
	_, replayed, lookupErr := idempotency.PostgresStore{}.Lookup(ctx, tx, scope)
	if lookupErr != nil {
		return Receipt{}, fmt.Errorf("transaction commit: read replay receipt: %w", lookupErr)
	}
	identity, err := idempotency.Guard(ctx, tx, idempotency.PostgresStore{}, scope, digest,
		idempotency.RetentionPolicy{Retention: retention, RetryWindow: 0}, now,
		func(ctx context.Context, tx dbport.Tx) (idempotency.ResultIdentity, error) {
			if err := c.fail("before-append"); err != nil {
				return idempotency.ResultIdentity{}, err
			}
			if declaresFence {
				baselines := make([]conflict.StreamBaseline, len(prepared.Streams))
				for i, stream := range prepared.Streams {
					if stream.ExpectedSequence < 0 {
						return idempotency.ResultIdentity{}, fmt.Errorf("%w: stream %s has a negative baseline", ErrInvalidPlan, stream.StreamKey)
					}
					baselines[i] = conflict.StreamBaseline{StreamKey: stream.StreamKey, ExpectedSequence: uint64(stream.ExpectedSequence)}
				}
				writes := make([]conflict.WriteBaseline, len(prepared.Writes))
				for i, write := range prepared.Writes {
					footprint := conflict.WriteFootprint{
						Resource: write.ResourceKey, Field: conflict.FieldPath(write.FieldPath),
						Interval: write.EffectiveInterval, Operation: conflict.Operation(write.Operation),
						ExpectedRevision: write.ExpectedRevision,
						Authority:        conflict.AuthorityScope{Domain: write.Subject.AuthorityDomain, PolicyRef: write.SourceAuthorityDecision},
					}
					baseline, err := conflict.BaselineFromFootprint(footprint)
					if err != nil {
						return idempotency.ResultIdentity{}, fmt.Errorf("%w: invalid conflict footprint: %v", ErrInvalidPlan, err)
					}
					writes[i] = baseline
				}
				if _, err := c.opts.ConflictFence.ValidateAtCommit(ctx, tx, conflict.CommitRequest{
					TenantID: tenant.String(), IntentID: prepared.ConflictIntentID, SnapshotDigest: prepared.ConflictSnapshotDigest,
					Streams: baselines, Writes: writes, FootprintDigests: append([]string(nil), prepared.ConflictFootprintDigests...),
				}); err != nil {
					return idempotency.ResultIdentity{}, fmt.Errorf("transaction commit: conflict fence: %w", err)
				}
			}
			if err := validateEvents(prepared); err != nil {
				return idempotency.ResultIdentity{}, err
			}
			for _, event := range prepared.Events {
				if err := ensurePayloadSchema(ctx, tx, tenant, event.SchemaRef); err != nil {
					return idempotency.ResultIdentity{}, err
				}
			}
			for _, effect := range prepared.OutboxEffects {
				if err := ensurePayloadSchema(ctx, tx, tenant, effect.SchemaRef); err != nil {
					return idempotency.ResultIdentity{}, err
				}
			}

			requests, err := c.appendRequest(ctx, tx, tenant, prepared, now, effectiveByStream)
			if err != nil {
				return idempotency.ResultIdentity{}, err
			}
			ledgerReceipt, err := datalogger.AppendMulti(ctx, tx, requests)
			if err != nil {
				return idempotency.ResultIdentity{}, fmt.Errorf("transaction commit: ledger: %w", err)
			}
			if err := c.fail("after-append"); err != nil {
				return idempotency.ResultIdentity{}, err
			}

			projections, err := applyProjections(ctx, tx, tenant, c.opts.ProjectionName, ledgerReceipt.Events)
			if err != nil {
				return idempotency.ResultIdentity{}, err
			}
			if err := c.fail("after-projection"); err != nil {
				return idempotency.ResultIdentity{}, err
			}

			outboxRecords, err := enqueueEffects(ctx, tx, tenant, prepared, ledgerReceipt)
			if err != nil {
				return idempotency.ResultIdentity{}, err
			}
			if err := c.fail("after-outbox"); err != nil {
				return idempotency.ResultIdentity{}, err
			}

			receipt := Receipt{
				ReceiptID: uuid.New(), Tenant: tenant, PlanID: prepared.PlanID,
				PlanDigest: prepared.Digest, Events: ledgerReceipt.Events,
				Projections: projections, Outbox: outboxRecords,
			}
			if err := c.fail("before-receipt"); err != nil {
				return idempotency.ResultIdentity{}, err
			}
			if err := recordCommitReceipt(ctx, tx, prepared, receipt, now, scope); err != nil {
				return idempotency.ResultIdentity{}, err
			}
			encoded, err := json.Marshal(receipt)
			if err != nil {
				return idempotency.ResultIdentity{}, fmt.Errorf("transaction commit: encode receipt: %w", err)
			}
			return idempotency.ResultIdentity{
				ResultRef:      string(encoded),
				EventRef:       fmt.Sprintf("%d ledger event(s)", len(receipt.Events)),
				EffectIdentity: fmt.Sprintf("%d outbox effect(s)", len(receipt.Outbox)),
				EvidenceID:     receipt.ReceiptID.String(),
			}, nil
		})
	if err != nil {
		return Receipt{}, fmt.Errorf("transaction commit: %w", err)
	}
	var receipt Receipt
	if err := json.Unmarshal([]byte(identity.Identity.ResultRef), &receipt); err != nil {
		return Receipt{}, fmt.Errorf("%w: %v", ErrReceiptDecode, err)
	}
	receipt.Replayed = replayed
	return receipt, nil
}

func (c *Committer) fail(stage string) error {
	if c.opts.Failpoint == nil {
		return nil
	}
	if err := c.opts.Failpoint(stage); err != nil {
		return fmt.Errorf("transaction commit: failpoint %s: %w", stage, err)
	}
	return nil
}

func setTenant(ctx context.Context, tx dbport.Tx, tenant uuid.UUID) error {
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenant.String()); err != nil {
		return fmt.Errorf("transaction commit: bind tenant %s: %w", tenant, err)
	}
	return nil
}

func validateEvents(prepared plan.TransactionPlan) error {
	if len(prepared.Events) == 0 {
		return fmt.Errorf("%w: plan has no events", ErrInvalidPlan)
	}
	headByStream := make(map[string]int64, len(prepared.Streams))
	for _, stream := range prepared.Streams {
		if stream.StreamKey == "" {
			return fmt.Errorf("%w: stream key is empty", ErrInvalidPlan)
		}
		if _, exists := headByStream[stream.StreamKey]; exists {
			return fmt.Errorf("%w: duplicate stream %s", ErrInvalidPlan, stream.StreamKey)
		}
		headByStream[stream.StreamKey] = stream.ExpectedSequence
	}
	seen := make(map[string]int, len(headByStream))
	for _, event := range prepared.Events {
		head, ok := headByStream[event.StreamKey]
		if !ok || event.SchemaRef == "" || event.Digest == "" {
			return fmt.Errorf("%w: event %s is not fully bound to a prepared stream", ErrInvalidPlan, event.StreamKey)
		}
		index := seen[event.StreamKey]
		want := head + int64(index) + 1
		if event.Sequence != want {
			return fmt.Errorf("%w: event %s has sequence %d, want %d", ErrInvalidPlan, event.StreamKey, event.Sequence, want)
		}
		seen[event.StreamKey] = index + 1
	}
	for stream := range headByStream {
		if seen[stream] == 0 {
			return fmt.Errorf("%w: stream %s has no planned event", ErrInvalidPlan, stream)
		}
	}
	for _, effect := range prepared.OutboxEffects {
		if effect.EffectID == "" || effect.DestinationRef == "" || effect.SchemaRef == "" || effect.PayloadDigest == "" || effect.IdempotencyKey == "" {
			return fmt.Errorf("%w: outbox effect %q is incomplete", ErrInvalidPlan, effect.EffectID)
		}
	}
	return nil
}

func (c *Committer) appendRequest(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, prepared plan.TransactionPlan, now time.Time, effectiveByStream map[string]time.Time) (datalogger.MultiStreamAppendRequest, error) {
	byStream := make(map[string][]datalogger.AppendRequest, len(prepared.Streams))
	for index, event := range prepared.Events {
		stream := event.StreamKey
		effectiveAt := now
		if len(effectiveByStream) > 0 {
			var ok bool
			effectiveAt, ok = effectiveByStream[stream]
			if !ok {
				return datalogger.MultiStreamAppendRequest{}, fmt.Errorf("%w: event stream %s has no typed effective interval", ErrInvalidPlan, stream)
			}
		}
		byStream[stream] = append(byStream[stream], datalogger.AppendRequest{
			Tenant: tenant, StreamKey: stream,
			ExpectedHead:   event.Sequence - int64(len(byStream[stream])) - 1,
			AssertionClass: datalogger.TransactionFact,
			SourceRef:      fmt.Sprintf("transaction-plan:%s:%s", prepared.PlanID, event.EventType),
			SchemaRef:      event.SchemaRef, ArtifactRef: event.Digest,
			OccurredAt: now, EffectiveAt: effectiveAt,
			CorrelationID:  uuid.NewSHA1(commitNamespace, []byte(prepared.PlanID)),
			IdempotencyKey: fmt.Sprintf("%s:event:%d", prepared.IdempotencyKey, index),
		})
	}
	streams := make([]datalogger.StreamAppend, 0, len(prepared.Streams))
	for _, stream := range prepared.Streams {
		if err := datalogger.EnsureStream(ctx, tx, tenant, stream.StreamKey, "TRANSACTION", prepared.PlanID); err != nil {
			return datalogger.MultiStreamAppendRequest{}, fmt.Errorf("transaction commit: register stream %s: %w", stream.StreamKey, err)
		}
		if err := projection.EnsureProjection(ctx, tx, tenant, c.opts.ProjectionName, stream.StreamKey); err != nil {
			return datalogger.MultiStreamAppendRequest{}, fmt.Errorf("transaction commit: register projection %s: %w", stream.StreamKey, err)
		}
		expected := stream.ExpectedSequence
		streams = append(streams, datalogger.StreamAppend{StreamKey: stream.StreamKey, ExpectedHead: expected, Events: byStream[stream.StreamKey]})
	}
	return datalogger.MultiStreamAppendRequest{Tenant: tenant, Streams: streams}, nil
}

func applyProjections(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, name string, events []datalogger.AppendReceipt) ([]projection.ApplyResult, error) {
	results := make([]projection.ApplyResult, 0, len(events))
	for _, event := range events {
		applied, err := projection.Apply(ctx, tx, projection.ApplyRequest{
			Tenant: tenant, ProjectionName: name, StreamKey: event.StreamKey,
			Sequence: event.Sequence, Digest: event.Digest,
		})
		if err != nil {
			return nil, fmt.Errorf("transaction commit: apply critical projection %s@%d: %w", event.StreamKey, event.Sequence, err)
		}
		results = append(results, applied)
	}
	return results, nil
}

func enqueueEffects(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, prepared plan.TransactionPlan, ledgerReceipt datalogger.MultiStreamAppendReceipt) ([]outbox.Record, error) {
	if len(prepared.OutboxEffects) == 0 {
		return nil, nil
	}
	records := make([]outbox.Record, 0, len(prepared.OutboxEffects))
	for i, effect := range prepared.OutboxEffects {
		outboxID := uuid.NewSHA1(anchorNamespace, []byte(prepared.PlanID+":"+effect.EffectID))
		record, err := outbox.Enqueue(ctx, tx, outbox.EnqueueRequest{
			Tenant: tenant, OutboxID: outboxID,
			EffectIdentity: effect.EffectID, OrderingKey: effect.DestinationRef,
			SchemaRef: effect.SchemaRef, Payload: []byte(effect.PayloadDigest),
		})
		if err != nil {
			return nil, fmt.Errorf("transaction commit: enqueue effect %d/%s: %w", i, effect.EffectID, err)
		}
		records = append(records, record)
	}
	return records, nil
}

var (
	commitNamespace = uuid.MustParse("6d6f8d77-2e5c-4e58-9a42-9db8dbb5a1b2")
	anchorNamespace = uuid.MustParse("b3a8df0e-4e1a-4a3d-9d5a-70c1ad8c39e0")
)

func ensurePayloadSchema(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, schemaRef string) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO payload_schema (
			tenant_id, schema_ref, schema_id, schema_version,
			message_full_name, wire_format, canonicalization_profile)
		VALUES ($1, $2, $2, 1, $2, 'PROTOBUF', 'LEDGER_EVENT')
		ON CONFLICT (tenant_id, schema_ref) DO NOTHING`, tenant, schemaRef); err != nil {
		return fmt.Errorf("transaction commit: register payload schema %s: %w", schemaRef, err)
	}
	return nil
}

// recordCommitReceipt uses the existing transaction_commit_receipt table when
// the plan has already been durably registered. TX-003's storage-neutral plan
// can also be committed before that registry row exists; in that case the
// idempotency_record written by Guard is the durable replay receipt and the
// caller must register the plan before promoting it to the registry receipt.
func recordCommitReceipt(ctx context.Context, tx dbport.Tx, prepared plan.TransactionPlan, receipt Receipt, committedAt time.Time, scope idempotency.Scope) error {
	planID, err := uuid.Parse(prepared.PlanID)
	if err != nil {
		return nil
	}
	var intentDigest, proposalDigest, controlDigest, storedPlanDigest string
	err = tx.QueryRow(ctx, `
		SELECT intent_digest, proposal_digest, control_digest, plan_digest
		FROM transaction_plan WHERE tenant_id = $1 AND plan_id = $2`, receipt.Tenant, planID).
		Scan(&intentDigest, &proposalDigest, &controlDigest, &storedPlanDigest)
	if errors.Is(err, dbport.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("transaction commit: read registered plan %s: %w", prepared.PlanID, err)
	}
	if storedPlanDigest != digestHex(prepared.Digest) {
		return fmt.Errorf("%w: registered plan digest %s differs from prepared %s", ErrInvalidPlan, storedPlanDigest, digestHex(prepared.Digest))
	}
	refs := make([]string, 0, len(receipt.Events)+len(receipt.Outbox))
	for _, event := range receipt.Events {
		refs = append(refs, event.EventID.String())
	}
	for _, effect := range receipt.Outbox {
		refs = append(refs, effect.OutboxID.String())
	}
	sort.Strings(refs)
	sum := sha256.Sum256([]byte(strings.Join(refs, "\x00")))
	_, err = tx.Exec(ctx, `
		INSERT INTO transaction_commit_receipt (
			tenant_id, receipt_id, plan_id, intent_digest, proposal_digest,
			control_digest, plan_digest, database_transaction_id,
			idempotency_record_ref, appended_event_count,
			projection_mutation_count, outbox_effect_count,
			produced_reference_digest, committed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, txid_current()::text, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (tenant_id, plan_id) DO NOTHING`,
		receipt.Tenant, receipt.ReceiptID, planID,
		contentDigest(intentDigest), contentDigest(proposalDigest), contentDigest(controlDigest), contentDigest(prepared.Digest),
		scope.String(), len(receipt.Events), len(receipt.Projections), len(receipt.Outbox), hex.EncodeToString(sum[:]), committedAt)
	if err != nil {
		return fmt.Errorf("transaction commit: record commit receipt: %w", err)
	}
	return nil
}

func digestHex(digest string) string {
	if index := strings.IndexByte(digest, ':'); index >= 0 {
		digest = digest[index+1:]
	}
	return digest
}

func contentDigest(digest string) string {
	plain := digestHex(digest)
	if len(plain) == 64 {
		return plain
	}
	sum := sha256.Sum256([]byte(digest))
	return hex.EncodeToString(sum[:])
}

// Version is the package contract version used by architecture conformance
// tooling.
func Version() int { return 1 }

// Explain returns a bounded description without plan material or payloads.
func Explain() string {
	return fmt.Sprintf("transaction.commit v%d: one PostgreSQL transaction for ordered ledger, critical checkpoints, outbox and replay receipt", Version())
}

// ExplainReceipt returns only commit dimensions and identities safe for
// operator telemetry; event payload bytes are never rendered.
func ExplainReceipt(receipt Receipt) string {
	return fmt.Sprintf("plan=%s digest=%s events=%d projections=%d outbox=%d replayed=%t", receipt.PlanID, receipt.PlanDigest, len(receipt.Events), len(receipt.Projections), len(receipt.Outbox), receipt.Replayed)
}
