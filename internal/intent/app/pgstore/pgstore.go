// Package pgstore is the PostgreSQL adapter behind internal/intent/app.Store.
//
// Semantic owner: intent-and-capability. Phase: P1A. Todos: NEXT-004, NEXT-005.
//
// It is a separate package from internal/intent/app on purpose. The
// application layer may depend on ports and never on a concrete adapter
// (definitions/architecture/package-dependency-policy.yaml); putting the SQL
// here is what makes that a compile-time fact rather than a convention, and a
// composition root (cmd/hcmnext, test/) is the only thing that names both.
//
// # What one created intent writes
//
// One AppendIntent call writes, inside one transaction and nowhere else:
//
//   - one ledger_event carrying the marshalled hcmnext.intents.v1.IntentInstance,
//   - one projection_checkpoint advance to that event's sequence,
//   - one outbox row for the distribution of that fact,
//   - one intent_instance row, the queryable projection of the same bytes.
//
// The ledger event is the authoritative record and the intent_instance row is
// derived from it: every read below takes the envelope from the ledger and the
// lifecycle columns from the projection, so a projection that ever disagrees
// with the ledger is visible rather than authoritative.
package pgstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	datalogger "github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/data/outbox"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projection"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	ledgerport "github.com/monstercameron/human-capital-management-suite/internal/ledger"
)

// ProjectionName is the projection checkpoint an intent's stream advances.
const ProjectionName = "intent_instance"

// StreamKind is the ledger_stream kind an intent's chronology is registered
// under. migrations/00005 enumerates the legal kinds; an intent envelope is a
// business transaction.
const StreamKind = "TRANSACTION"

// envelopeSequence is the sequence the creation event occupies on an intent's
// own stream. One intent, one stream, one creation event at sequence 1.
const envelopeSequence int64 = 1

// tenantNamespace derives a stable tenant UUID from a tenant slug.
//
// The wire and the credential speak in slugs ("acme-corp"); migrations/00002
// declares tenant_id as a uuid. Deriving it rather than looking it up means the
// same slug names the same tenant in every process and after every restart,
// with no registry to keep in sync.
var tenantNamespace = uuid.MustParse("8f1d7b52-3a0a-4d1e-9d4a-0a0f2c1b7e10")

// DB is the database capability this adapter needs, stated in [dbport]'s
// driver-free terms: a pooled handle or a single connection both satisfy it.
type DB interface {
	dbport.Beginner
	dbport.Querier
}

// Store implements app.Store over PostgreSQL.
type Store struct {
	db       DB
	appender ledgerport.Appender
	reader   ledgerport.Reader
	cellID   string
	now      func() time.Time
}

var _ app.Store = (*Store)(nil)

// Option configures a [Store].
type Option func(*Store)

// WithCellID names the cell a bootstrapped tenant is registered against.
func WithCellID(id string) Option {
	return func(s *Store) { s.cellID = id }
}

// WithClock pins the ledger's recording clock. A composition that pins the
// cell clock (app.CellConfig.Now) must pass the same reading here, so the
// recorded_at of every event it appends is not later than the as-known-at the
// same cell stamps on the intents that read it back. Nil keeps the host clock.
func WithClock(now func() time.Time) Option {
	return func(s *Store) { s.now = now }
}

// New builds the adapter with the kernel-digest-backed ledger appender, so a
// stored event's digest is minted by the same published canonicalization
// profile every other canonical digest in the platform uses.
func New(db DB, opts ...Option) (*Store, error) {
	registry, err := ledgerport.NewLedgerEventDigestRegistry()
	if err != nil {
		return nil, fmt.Errorf("pgstore: build the ledger event digest registry: %w", err)
	}
	s := &Store{
		db:       db,
		appender: ledgerport.NewAppender(registry),
		reader:   ledgerport.NewReader(),
		cellID:   "cell-local",
	}
	for _, opt := range opts {
		opt(s)
	}
	if s.now != nil {
		s.appender = ledgerport.NewAppenderWithClock(registry, s.now)
	}
	return s, nil
}

// TenantID is the uuid a tenant slug resolves to.
func TenantID(tenant string) uuid.UUID {
	return uuid.NewSHA1(tenantNamespace, []byte(tenant))
}

// StreamKey is the ledger stream one intent's chronology lives on.
//
// One stream per intent rather than one per tenant: an intent's chronology is
// its own, and a shared stream would make two unrelated concurrent creations
// contend on a single compare-and-swap head for no semantic reason.
func StreamKey(intentID string) string { return "intent:" + intentID }

// Bootstrap registers the tenant and the envelope payload schema.
//
// It is idempotent and safe on every start. In P1A a cell provisions the tenant
// it is asked to serve; tenant lifecycle (onboarding, suspension, exit) is a
// separate contract that does not exist yet, and pretending otherwise by
// failing here would only mean the same INSERT lived in a shell script.
func (s *Store) Bootstrap(ctx context.Context, tenant string) error {
	if tenant == "" {
		return errors.New("pgstore: bootstrap needs a tenant")
	}
	id := TenantID(tenant)
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("pgstore: begin bootstrap: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, $3, $4, 'ACTIVE', now())
		ON CONFLICT (tenant_id) DO NOTHING`, id, tenant, s.cellID, tenant); err != nil {
		return fmt.Errorf("pgstore: register tenant %s: %w", tenant, err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO payload_schema (
			tenant_id, schema_ref, schema_id, schema_version,
			message_full_name, wire_format, canonicalization_profile)
		VALUES ($1, $2, 'hcmnext.intents.v1.IntentInstance', 1,
			'hcmnext.intents.v1.IntentInstance', 'PROTOBUF', 'LEDGER_EVENT')
		ON CONFLICT (tenant_id, schema_ref) DO NOTHING`, id, app.EnvelopeSchemaRef); err != nil {
		return fmt.Errorf("pgstore: register the envelope payload schema: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("pgstore: commit bootstrap: %w", err)
	}
	return nil
}

// AppendIntent records one drafted instance as chronology.
//
// A replay of the same idempotency key returns the originally stored record and
// writes nothing: the check is a read of intent_instance's own uniqueness
// constraint, and the constraint itself is what settles a race between two
// concurrent first attempts.
func (s *Store) AppendIntent(ctx context.Context, rec app.IntentRecord) (app.AppendResult, error) {
	tenantID := TenantID(rec.Tenant)

	if existing, err := s.loadByIdempotencyKey(ctx, rec.Tenant, tenantID, rec.IdempotencyKey); err == nil {
		return app.AppendResult{
			Record:         existing,
			Replayed:       true,
			StreamKey:      StreamKey(existing.IntentID),
			ProjectionName: ProjectionName,
		}, nil
	} else if !errors.Is(err, app.ErrIntentNotFound) {
		return app.AppendResult{}, err
	}

	streamKey := StreamKey(rec.IntentID)
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return app.AppendResult{}, fmt.Errorf("pgstore: begin append: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := datalogger.EnsureStream(ctx, tx, tenantID, streamKey, StreamKind, rec.IntentID); err != nil {
		return app.AppendResult{}, fmt.Errorf("pgstore: register stream %s: %w", streamKey, err)
	}
	if err := projection.EnsureProjection(ctx, tx, tenantID, ProjectionName, streamKey); err != nil {
		return app.AppendResult{}, fmt.Errorf("pgstore: register projection checkpoint: %w", err)
	}

	receipt, err := outbox.Commit(ctx, tx, s.appender, outbox.CommitRequest{
		Append: datalogger.AppendRequest{
			Tenant:         tenantID,
			StreamKey:      streamKey,
			ExpectedHead:   0,
			AssertionClass: datalogger.TransactionFact,
			SourceRef:      "hcmnext:intent",
			SchemaRef:      rec.EnvelopeSchemaRef,
			Payload:        rec.Envelope,
			OccurredAt:     rec.CreatedAt,
			EffectiveAt:    rec.CreatedAt,
			CorrelationID:  correlationUUID(rec.CorrelationID),
			IdempotencyKey: rec.IdempotencyKey,
		},
		Projection: outbox.ProjectionSpec{Name: ProjectionName},
		Outbox: outbox.OutboxSpec{
			EffectIdentity: "intent.created:" + rec.IntentID,
			OrderingKey:    streamKey,
			SchemaRef:      rec.EnvelopeSchemaRef,
			Payload:        rec.Envelope,
		},
	})
	if err != nil {
		return app.AppendResult{}, fmt.Errorf("pgstore: append intent chronology: %w", err)
	}

	request, execution, business, consistency, obligation := app.LifecycleColumns(rec.Lifecycle)
	affected, err := tx.Exec(ctx, `
		INSERT INTO intent_instance (
			tenant_id, intent_id, definition_ref, definition_version,
			request_digest, request_digest_algorithm, idempotency_key,
			request_state, execution_state, business_state, consistency_state, obligation_state,
			instance_version, created_at, recorded_at, last_transition_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
		ON CONFLICT (tenant_id, idempotency_key) DO NOTHING`,
		tenantID, rec.IntentID, rec.Definition.TypeID, int64(rec.Definition.Version),
		rec.RequestDigest.Digest, rec.RequestDigest.AlgorithmID, rec.IdempotencyKey,
		request, execution, business, consistency, obligation,
		int64(rec.InstanceVersion), rec.CreatedAt, rec.RecordedAt, rec.LastTransitionAt)
	if err != nil {
		return app.AppendResult{}, fmt.Errorf("pgstore: project intent instance: %w", err)
	}
	if affected == 0 {
		// Another writer won the race on this idempotency key. Roll this
		// transaction back whole - the ledger append, the checkpoint advance and
		// the outbox row go with it - and answer with what is actually stored.
		_ = tx.Rollback(ctx)
		existing, loadErr := s.loadByIdempotencyKey(ctx, rec.Tenant, tenantID, rec.IdempotencyKey)
		if loadErr != nil {
			return app.AppendResult{}, loadErr
		}
		return app.AppendResult{
			Record:         existing,
			Replayed:       true,
			StreamKey:      StreamKey(existing.IntentID),
			ProjectionName: ProjectionName,
		}, nil
	}

	if err := tx.Commit(ctx); err != nil {
		return app.AppendResult{}, fmt.Errorf("pgstore: commit append: %w", err)
	}

	return app.AppendResult{
		Record:            rec,
		Ledger:            receipt.Ledger,
		Replayed:          receipt.Ledger.Replayed,
		ProjectionApplied: receipt.Projection.Applied,
		OutboxID:          receipt.Outbox.OutboxID.String(),
		StreamKey:         streamKey,
		ProjectionName:    ProjectionName,
	}, nil
}

// correlationUUID derives the ledger's uuid correlation identifier from the
// transport's own correlation string, so one request identifier ties the
// request log and the ledger row together without a second identifier to keep
// in sync.
func correlationUUID(correlation string) uuid.UUID {
	if correlation == "" {
		return uuid.New()
	}
	if parsed, err := uuid.Parse(correlation); err == nil {
		return parsed
	}
	return uuid.NewSHA1(tenantNamespace, []byte("correlation:"+correlation))
}

// intentColumns is the projection every read selects.
const intentColumns = `
	intent_id, definition_ref, definition_version,
	request_digest, request_digest_algorithm, idempotency_key,
	request_state, execution_state, business_state, consistency_state, obligation_state,
	instance_version, created_at, recorded_at, last_transition_at,
	commit_receipt_ref, repair_ref,
	legal_evaluation_receipt_ref, legal_evaluation_receipt_digest,
	legal_evaluation_binding_digest, legal_evaluation_proposal_revision_id,
	legal_evaluation_material_digest, legal_applied_obligations,
	legal_obligation_discharges`

// scanRecord reads one projected row and then takes the authoritative envelope
// from the ledger event the row was projected from.
func (s *Store) scanRecord(ctx context.Context, tenant string, tenantID uuid.UUID, row dbport.Row) (app.IntentRecord, error) {
	var (
		rec                                                                                           app.IntentRecord
		definitionVersion                                                                             int64
		instanceVersion                                                                               int64
		req, exec, bus, cons, obligation                                                              string
		commitReceiptRef, repairRef                                                                   *string
		legalReceiptRef, legalReceiptDigest, legalBindingDigest, legalProposalID, legalMaterialDigest *string
		legalAppliedObligations, legalObligationDischarges                                            []byte
	)
	err := row.Scan(
		&rec.IntentID, &rec.Definition.TypeID, &definitionVersion,
		&rec.RequestDigest.Digest, &rec.RequestDigest.AlgorithmID, &rec.IdempotencyKey,
		&req, &exec, &bus, &cons, &obligation,
		&instanceVersion, &rec.CreatedAt, &rec.RecordedAt, &rec.LastTransitionAt,
		&commitReceiptRef, &repairRef, &legalReceiptRef, &legalReceiptDigest, &legalBindingDigest,
		&legalProposalID, &legalMaterialDigest, &legalAppliedObligations, &legalObligationDischarges)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return app.IntentRecord{}, app.ErrIntentNotFound
		}
		return app.IntentRecord{}, fmt.Errorf("pgstore: read intent projection: %w", err)
	}

	rec.Tenant = tenant
	if commitReceiptRef != nil {
		rec.CommitReceiptRef = *commitReceiptRef
	}
	if repairRef != nil {
		rec.RepairRef = *repairRef
	}
	if legalReceiptRef != nil {
		e := &intent.LegalObligationEvidence{ReceiptRef: *legalReceiptRef}
		if legalReceiptDigest != nil {
			e.ReceiptDigest = *legalReceiptDigest
		}
		if legalBindingDigest != nil {
			e.BindingDigest = *legalBindingDigest
		}
		if legalProposalID != nil {
			e.ProposalRevisionID = *legalProposalID
		}
		if legalMaterialDigest != nil {
			e.MaterialDigest = *legalMaterialDigest
		}
		if len(legalAppliedObligations) > 0 && string(legalAppliedObligations) != "null" {
			// A corrupt obligations blob is a corrupt projection, not an
			// empty one: returning it decoded-to-zero would launder the
			// legal evidence the receipt cites.
			if err := json.Unmarshal(legalAppliedObligations, &e.AppliedObligations); err != nil {
				return app.IntentRecord{}, fmt.Errorf("pgstore: decode applied legal obligations: %w", err)
			}
		}
		if len(legalObligationDischarges) > 0 && string(legalObligationDischarges) != "null" {
			if err := json.Unmarshal(legalObligationDischarges, &e.Discharges); err != nil {
				return app.IntentRecord{}, fmt.Errorf("pgstore: decode legal obligation discharges: %w", err)
			}
		}
		rec.LegalEvidence = e
	}
	rec.Definition.Version = uint32(definitionVersion)
	rec.InstanceVersion = uint64(instanceVersion)
	rec.EnvelopeSchemaRef = app.EnvelopeSchemaRef

	dims, err := app.LifecycleFromColumns(req, exec, bus, cons, obligation)
	if err != nil {
		return app.IntentRecord{}, err
	}
	rec.Lifecycle = dims

	event, err := s.reader.ReadEvent(ctx, s.db, tenantID, StreamKey(rec.IntentID), envelopeSequence)
	if err != nil {
		return app.IntentRecord{}, fmt.Errorf("pgstore: read the intent envelope from the ledger: %w", err)
	}
	rec.Envelope = event.Payload
	rec.RequestDigest.CanonicalLength = uint64(event.CanonicalLength)
	return rec, nil
}

func (s *Store) loadByIdempotencyKey(ctx context.Context, tenant string, tenantID uuid.UUID, key string) (app.IntentRecord, error) {
	row := s.db.QueryRow(ctx,
		`SELECT `+intentColumns+` FROM intent_instance WHERE tenant_id = $1 AND idempotency_key = $2`,
		tenantID, key)
	return s.scanRecord(ctx, tenant, tenantID, row)
}

// LoadIntent implements app.Store.
func (s *Store) LoadIntent(ctx context.Context, tenant, intentID string) (app.IntentRecord, error) {
	if _, err := uuid.Parse(intentID); err != nil {
		// intent_id is a uuid column; a non-uuid identifier is simply not
		// visible, and answering NOT_FOUND rather than raising a type error
		// keeps a probe from learning the column's type.
		return app.IntentRecord{}, app.ErrIntentNotFound
	}
	tenantID := TenantID(tenant)
	row := s.db.QueryRow(ctx,
		`SELECT `+intentColumns+` FROM intent_instance WHERE tenant_id = $1 AND intent_id = $2`,
		tenantID, intentID)
	return s.scanRecord(ctx, tenant, tenantID, row)
}

// ListIntents implements app.Store. The cursor is the last intent identifier of
// the previous page; identifiers are UUIDv7, so ordering by identity is
// ordering by creation.
func (s *Store) ListIntents(ctx context.Context, tenant string, pageSize int32, cursor string) (app.IntentPage, error) {
	tenantID := TenantID(tenant)
	if pageSize <= 0 {
		pageSize = 50
	}
	rows, err := s.db.Query(ctx,
		`SELECT intent_id
		 FROM intent_instance
		 WHERE tenant_id = $1 AND ($2 = '' OR intent_id > $2::uuid)
		 ORDER BY intent_id
		 LIMIT $3`,
		tenantID, cursor, int64(pageSize)+1)
	if err != nil {
		return app.IntentPage{}, fmt.Errorf("pgstore: list intents: %w", err)
	}
	ids := make([]string, 0, pageSize+1)
	for rows.Next() {
		var id uuid.UUID
		if scanErr := rows.Scan(&id); scanErr != nil {
			rows.Close()
			return app.IntentPage{}, fmt.Errorf("pgstore: list intents: %w", scanErr)
		}
		ids = append(ids, id.String())
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return app.IntentPage{}, fmt.Errorf("pgstore: list intents: %w", err)
	}

	page := app.IntentPage{}
	if int32(len(ids)) > pageSize {
		page.NextCursor = ids[pageSize-1]
		ids = ids[:pageSize]
	}
	for _, id := range ids {
		rec, loadErr := s.LoadIntent(ctx, tenant, id)
		if loadErr != nil {
			return app.IntentPage{}, loadErr
		}
		page.Records = append(page.Records, rec)
	}
	return page, nil
}

// Timeline implements app.Store, reading the authoritative ledger.
func (s *Store) Timeline(ctx context.Context, tenant, intentID string) ([]app.TimelineEntry, error) {
	tenantID := TenantID(tenant)
	events, err := s.reader.ReadStream(ctx, s.db, tenantID, StreamKey(intentID))
	if err != nil {
		return nil, fmt.Errorf("pgstore: read intent timeline: %w", err)
	}
	out := make([]app.TimelineEntry, 0, len(events))
	for _, event := range events {
		out = append(out, app.TimelineEntry{
			EventID:         event.EventID.String(),
			Kind:            string(event.AssertionClass),
			Sequence:        event.Sequence,
			SchemaRef:       event.SchemaRef,
			Digest:          event.Digest,
			DigestAlgorithm: event.DigestAlgorithm,
			OccurredAt:      event.OccurredAt,
			RecordedAt:      event.RecordedAt,
		})
	}
	return out, nil
}

// Verify recomputes every stored event digest on an intent's stream, so a
// caller can prove the chronology it just read is the chronology that was
// written rather than trusting the stored value.
func (s *Store) Verify(ctx context.Context, tenant, intentID string) error {
	registry, err := ledgerport.NewLedgerEventDigestRegistry()
	if err != nil {
		return err
	}
	digester := ledgerport.NewKernelDigester(registry)
	tenantID := TenantID(tenant)
	events, err := s.reader.ReadStream(ctx, s.db, tenantID, StreamKey(intentID))
	if err != nil {
		return fmt.Errorf("pgstore: read intent stream: %w", err)
	}
	for _, event := range events {
		if err := digester.VerifyEvent(event); err != nil {
			return fmt.Errorf("pgstore: verify event %d: %w", event.Sequence, err)
		}
	}
	return nil
}

// ActiveTenants lists the tenant identifiers a worker or projector sweeps.
func ActiveTenants(ctx context.Context, q DB) ([]uuid.UUID, error) {
	rows, err := q.Query(ctx, `SELECT tenant_id FROM tenant WHERE status = 'ACTIVE'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
