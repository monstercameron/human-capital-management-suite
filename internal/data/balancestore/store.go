// Package balancestore is the PostgreSQL adapter for the balance domain's
// immutable definitions and append-only balance/lifecycle ledger.
package balancestore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/balance"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/cycle"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type Executor interface {
	dbport.Execer
	dbport.Querier
}

// Store is stateless; callers own the transaction and must scope it with
// tenancy.WithTenant before calling a method that touches tenant data.
type Store struct{}

func New() Store { return Store{} }

// AppendRestatement persists a previously computed cycle correction through
// the caller's transaction. The unique tenant/cycle/prior-sequence key is its
// compare-and-swap: concurrent transactions can append only one successor.
// Repeating the same restatement ID and digest returns the original row.
func (s Store) AppendRestatement(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, restatement cycle.CycleRestatement) (RestatementRecord, error) {
	if tenant == uuid.Nil || restatement.Prior.TenantID != tenant.String() {
		return RestatementRecord{}, invalid("tenant_id", "must match the prior cycle close")
	}
	if err := restatement.Verify(); err != nil {
		return RestatementRecord{}, fmt.Errorf("%w: restatement: %v", ErrInvalid, err)
	}
	if restatement.Revision > math.MaxInt64 || restatement.Prior.Sequence > math.MaxInt64 {
		return RestatementRecord{}, invalid("revision", "exceeds durable storage range")
	}
	body, err := restatement.Canonical()
	if err != nil {
		return RestatementRecord{}, fmt.Errorf("canonicalize cycle restatement: %w", err)
	}
	var record RestatementRecord
	err = tx.QueryRow(ctx, `
		INSERT INTO cycle_restatement
			(row_id, tenant_id, restatement_id, revision, cycle_id, prior_sequence,
			 prior_close_digest, digest, correction_at, canonical_body)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb)
		ON CONFLICT DO NOTHING
		RETURNING row_id, recorded_at`,
		uuid.New(), tenant, restatement.RestatementID, int64(restatement.Revision), restatement.Prior.CycleID,
		int64(restatement.Prior.Sequence), restatement.Prior.CloseResultDigest, restatement.Digest,
		restatement.CorrectionAt.UTC().Format(time.RFC3339Nano), string(body)).Scan(&record.RowID, &record.RecordedAt)
	if err == nil {
		record.TenantID, record.Restatement = tenant, restatement
		return record, nil
	}
	if !errors.Is(err, dbport.ErrNoRows) {
		return RestatementRecord{}, fmt.Errorf("append cycle restatement: %w", err)
	}
	existing, found, err := s.loadRestatement(ctx, tx, tenant, restatement.RestatementID)
	if err != nil {
		return RestatementRecord{}, err
	}
	if found {
		if existing.Restatement.Digest != restatement.Digest {
			return RestatementRecord{}, codedError{code: CodeIdempotencyConflict, err: ErrIdempotencyConflict,
				text: fmt.Sprintf("%s: restatement %s", CodeIdempotencyConflict, restatement.RestatementID)}
		}
		existing.Replay = true
		return existing, nil
	}
	return RestatementRecord{}, codedError{code: CodeRestatementConflict, err: ErrRestatementConflict,
		text: fmt.Sprintf("%s: cycle %s sequence %d", CodeRestatementConflict, restatement.Prior.CycleID, restatement.Prior.Sequence)}
}

// LoadRestatement loads and verifies an immutable correction after a process
// restart. Tenant scoping is supplied by both the explicit key and caller tx.
func (s Store) LoadRestatement(ctx context.Context, q dbport.Querier, tenant uuid.UUID, restatementID string) (RestatementRecord, error) {
	if tenant == uuid.Nil || strings.TrimSpace(restatementID) == "" {
		return RestatementRecord{}, invalid("restatement", "tenant and id are required")
	}
	record, found, err := s.loadRestatement(ctx, q, tenant, restatementID)
	if err != nil {
		return RestatementRecord{}, err
	}
	if !found {
		return RestatementRecord{}, codedError{code: CodeRestatementNotFound, err: ErrRestatementNotFound,
			text: CodeRestatementNotFound + ": " + restatementID}
	}
	return record, nil
}

func (s Store) loadRestatement(ctx context.Context, q dbport.Querier, tenant uuid.UUID, restatementID string) (RestatementRecord, bool, error) {
	var record RestatementRecord
	var body string
	var digest, cycleID, priorDigest string
	var revision, priorSequence int64
	var correctionAt string
	err := q.QueryRow(ctx, `SELECT row_id, tenant_id, restatement_id, revision, cycle_id,
		prior_sequence, prior_close_digest, digest, correction_at, canonical_body::text, recorded_at
		FROM cycle_restatement WHERE tenant_id=$1 AND restatement_id=$2`, tenant, restatementID).Scan(
		&record.RowID, &record.TenantID, &restatementID, &revision, &cycleID,
		&priorSequence, &priorDigest, &digest, &correctionAt, &body, &record.RecordedAt)
	if errors.Is(err, dbport.ErrNoRows) {
		return RestatementRecord{}, false, nil
	}
	if err != nil {
		return RestatementRecord{}, false, fmt.Errorf("load cycle restatement: %w", err)
	}
	restored, err := cycle.DecodeRestatement([]byte(body), digest)
	if err != nil {
		return RestatementRecord{}, false, fmt.Errorf("verify stored cycle restatement: %w", err)
	}
	if restored.RestatementID != restatementID || int64(restored.Revision) != revision || restored.Prior.CycleID != cycleID || int64(restored.Prior.Sequence) != priorSequence || restored.Prior.CloseResultDigest != priorDigest || restored.CorrectionAt.UTC().Format(time.RFC3339Nano) != correctionAt {
		return RestatementRecord{}, false, errors.New("balancestore: cycle restatement index does not match canonical body")
	}
	record.Restatement = restored
	return record, true, nil
}

// DefinitionRecord is the durable envelope around a balance definition.
type DefinitionRecord struct {
	TenantID   uuid.UUID
	RowID      uuid.UUID
	Revision   uint64
	Definition balance.AccumulatorDefinition
}

// LifecycleRecord is the durable envelope around one lifecycle fact.
type LifecycleRecord struct {
	TenantID      uuid.UUID
	RowID         uuid.UUID
	EntryRef      uuid.UUID
	Entry         balance.LifecycleEntry
	EventSequence int64
	RecordedAt    time.Time
}

// RestatementRecord is the durable envelope around an immutable cycle correction.
type RestatementRecord struct {
	TenantID    uuid.UUID
	RowID       uuid.UUID
	Restatement cycle.CycleRestatement
	RecordedAt  time.Time
	Replay      bool
}

var (
	ErrInvalid             = errors.New("balancestore: invalid request")
	ErrDuplicateRevision   = errors.New("balancestore: duplicate definition revision")
	ErrDefinitionNotFound  = errors.New("balancestore: definition not found")
	ErrEntryNotFound       = errors.New("balancestore: entry not found")
	ErrStaleCAS            = errors.New("balancestore: stale compare-and-swap")
	ErrIdempotencyConflict = errors.New("balancestore: idempotency key conflict")
	ErrCorrectionConflict  = errors.New("balancestore: correction parent already superseded")
	ErrSequenceGap         = errors.New("balancestore: event sequence gap")
	ErrDuplicateEvent      = errors.New("balancestore: duplicate lifecycle event")
	ErrRestatementConflict = errors.New("balancestore: prior cycle close already restated")
	ErrRestatementNotFound = errors.New("balancestore: restatement not found")
)

const (
	CodeDuplicateRevision   = "BALANCE_DUPLICATE_REVISION"
	CodeDefinitionNotFound  = "BALANCE_DEFINITION_NOT_FOUND"
	CodeEntryNotFound       = "BALANCE_ENTRY_NOT_FOUND"
	CodeStaleCAS            = "BALANCE_STALE_CAS"
	CodeIdempotencyConflict = "BALANCE_IDEMPOTENCY_CONFLICT"
	CodeCorrectionConflict  = "BALANCE_CORRECTION_CONFLICT"
	CodeSequenceGap         = "BALANCE_SEQUENCE_GAP"
	CodeDuplicateEvent      = "BALANCE_DUPLICATE_EVENT"
	CodeRestatementConflict = "CYCLE_RESTATEMENT_CONFLICT"
	CodeRestatementNotFound = "CYCLE_RESTATEMENT_NOT_FOUND"
)

type codedError struct {
	code string
	err  error
	text string
}

func (e codedError) Error() string { return e.text }
func (e codedError) Unwrap() error { return e.err }
func (e codedError) Code() string  { return e.code }

func CodeOf(err error) string {
	var coded interface{ Code() string }
	if errors.As(err, &coded) {
		return coded.Code()
	}
	return ""
}

func invalid(field, reason string) error {
	return fmt.Errorf("%w: %s: %s", ErrInvalid, field, reason)
}

func (s Store) PutDefinition(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, def balance.AccumulatorDefinition, revision uint64) (DefinitionRecord, error) {
	if tenant == uuid.Nil {
		return DefinitionRecord{}, invalid("tenant_id", "is required")
	}
	if revision == 0 {
		return DefinitionRecord{}, invalid("revision", "must be positive")
	}
	if err := def.Validate(); err != nil {
		return DefinitionRecord{}, fmt.Errorf("%w: definition: %v", ErrInvalid, err)
	}
	dimensions, err := json.Marshal(def.Dimensions)
	if err != nil {
		return DefinitionRecord{}, fmt.Errorf("marshal dimensions: %w", err)
	}
	entryTypes, err := json.Marshal(def.EntryTypes)
	if err != nil {
		return DefinitionRecord{}, fmt.Errorf("marshal entry types: %w", err)
	}
	authority, err := json.Marshal(def.Authority)
	if err != nil {
		return DefinitionRecord{}, fmt.Errorf("marshal authority: %w", err)
	}
	policies := make([]any, 5)
	for i, policy := range []balance.PolicyRule{def.Floor, def.Cap, def.Expiry, def.Rollover, def.Correction} {
		policies[i], err = json.Marshal(policy)
		if err != nil {
			return DefinitionRecord{}, fmt.Errorf("marshal policy %d: %w", i, err)
		}
	}
	row := tx.QueryRow(ctx, `
		INSERT INTO accumulator_definition (
			row_id, tenant_id, definition_id, version, revision, name, unit, currency,
			subject, period, dimensions, entry_types, authority, floor_policy,
			cap_policy, expiry_policy, rollover_policy, correction_policy)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb,$12::jsonb,$13,$14::jsonb,
			$15::jsonb,$16::jsonb,$17::jsonb,$18::jsonb)
		ON CONFLICT DO NOTHING
		RETURNING row_id`,
		uuid.New(), tenant, def.ID, def.Version, int64(revision), def.Name, def.Unit,
		optionalText(def.Currency), def.Subject, string(def.Period), string(dimensions),
		string(entryTypes), string(authority), policies[0], policies[1], policies[2], policies[3], policies[4])
	var record DefinitionRecord
	if err := row.Scan(&record.RowID); err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return DefinitionRecord{}, codedError{code: CodeDuplicateRevision, err: ErrDuplicateRevision,
				text: fmt.Sprintf("%s: definition %s/%s revision %d", CodeDuplicateRevision, def.ID, def.Version, revision)}
		}
		return DefinitionRecord{}, fmt.Errorf("insert accumulator definition %s/%s: %w", def.ID, def.Version, err)
	}
	record.TenantID, record.Revision, record.Definition = tenant, revision, def
	return record, nil
}

// Publish is the descriptive alias used by callers that treat a definition
// row as a publication rather than a generic insert.
func (s Store) Publish(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, def balance.AccumulatorDefinition, revision uint64) (DefinitionRecord, error) {
	return s.PutDefinition(ctx, tx, tenant, def, revision)
}

func (s Store) LoadDefinition(ctx context.Context, q dbport.Querier, tenant uuid.UUID, definitionID, version string) (DefinitionRecord, error) {
	if tenant == uuid.Nil || definitionID == "" || version == "" {
		return DefinitionRecord{}, invalid("definition", "tenant, id and version are required")
	}
	row := q.QueryRow(ctx, definitionColumns+`
		FROM accumulator_definition
		WHERE tenant_id=$1 AND definition_id=$2 AND version=$3`, tenant, definitionID, version)
	record, err := scanDefinition(row)
	if errors.Is(err, dbport.ErrNoRows) {
		return DefinitionRecord{}, codedError{code: CodeDefinitionNotFound, err: ErrDefinitionNotFound,
			text: fmt.Sprintf("%s: %s/%s", CodeDefinitionNotFound, definitionID, version)}
	}
	if err != nil {
		return DefinitionRecord{}, fmt.Errorf("load accumulator definition %s/%s: %w", definitionID, version, err)
	}
	return record, nil
}

const definitionColumns = `SELECT tenant_id,row_id,definition_id,version,revision,name,unit,currency,
	subject,period,dimensions::text,entry_types::text,authority,floor_policy::text,
	cap_policy::text,expiry_policy::text,rollover_policy::text,correction_policy::text `

func scanDefinition(src interface{ Scan(...any) error }) (DefinitionRecord, error) {
	var (
		r                                                      DefinitionRecord
		id, version, period, dimensions, entryTypes, authority string
		floor, cap, expiry, rollover, correction               *string
		revision                                               int64
		currency                                               *string
		name, unit, subject                                    string
	)
	if err := src.Scan(&r.TenantID, &r.RowID, &id, &version, &revision, &name, &unit, &currency,
		&subject, &period, &dimensions, &entryTypes, &authority, &floor, &cap, &expiry, &rollover, &correction); err != nil {
		return DefinitionRecord{}, err
	}
	var d balance.AccumulatorDefinition
	if err := json.Unmarshal([]byte(dimensions), &d.Dimensions); err != nil {
		return DefinitionRecord{}, fmt.Errorf("decode dimensions: %w", err)
	}
	if err := json.Unmarshal([]byte(entryTypes), &d.EntryTypes); err != nil {
		return DefinitionRecord{}, fmt.Errorf("decode entry types: %w", err)
	}
	if err := json.Unmarshal([]byte(authority), &d.Authority); err != nil {
		return DefinitionRecord{}, fmt.Errorf("decode authority: %w", err)
	}
	policies := []*string{floor, cap, expiry, rollover, correction}
	out := []*balance.PolicyRule{&d.Floor, &d.Cap, &d.Expiry, &d.Rollover, &d.Correction}
	for i := range policies {
		if policies[i] != nil {
			if err := json.Unmarshal([]byte(*policies[i]), out[i]); err != nil {
				return DefinitionRecord{}, fmt.Errorf("decode policy %d: %w", i, err)
			}
		}
	}
	d.ID, d.Version, d.Name, d.Unit, d.Currency, d.Subject, d.Period = id, version, name, unit, currencyValue(currency), subject, balance.PeriodKind(period)
	r.Definition, r.Revision = d, uint64(revision)
	return r, nil
}

func (s Store) Post(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, req balance.PostRequest) (balance.PostReceipt, error) {
	if tenant == uuid.Nil {
		return balance.PostReceipt{}, invalid("tenant_id", "is required")
	}
	if req.ExpectedHead < 0 {
		return balance.PostReceipt{}, invalid("expected_head", "cannot be negative")
	}
	if req.Entry.IdempotencyKey == "" {
		return balance.PostReceipt{}, invalid("idempotency_key", "is required")
	}
	lineage, err := hasCorrectionLineage(ctx, tx)
	if err != nil {
		return balance.PostReceipt{}, err
	}
	if req.Entry.SupersedesDigest != "" {
		if !lineage {
			return balance.PostReceipt{}, invalid("supersedes_digest", "correction lineage storage is not installed")
		}
	}
	if err := lock(ctx, tx, "key:"+tenant.String()+":"+req.Entry.IdempotencyKey); err != nil {
		return balance.PostReceipt{}, err
	}
	if prior, found, err := s.loadEntryByKey(ctx, tx, tenant, req.Entry.IdempotencyKey, lineage); err != nil {
		return balance.PostReceipt{}, err
	} else if found {
		if prior.receipt.Digest != req.Entry.Digest() {
			return balance.PostReceipt{}, codedError{code: CodeIdempotencyConflict, err: ErrIdempotencyConflict,
				text: fmt.Sprintf("%s: %s", CodeIdempotencyConflict, req.Entry.IdempotencyKey)}
		}
		receipt := prior.receipt
		receipt.Replay = true
		return receipt, nil
	}
	def, err := s.LoadDefinition(ctx, tx, tenant, req.Entry.DefinitionID, req.Entry.DefinitionVersion)
	if err != nil {
		return balance.PostReceipt{}, err
	}
	if err := req.Entry.Validate(def.Definition); err != nil {
		return balance.PostReceipt{}, err
	}
	var parentRef uuid.UUID
	if req.Entry.SupersedesDigest != "" {
		rows, queryErr := tx.Query(ctx, entryColumnsLineage+` FROM balance_entry b JOIN accumulator_definition d ON d.tenant_id=b.tenant_id AND d.definition_id=b.definition_id AND d.revision=b.definition_version WHERE b.tenant_id=$1 AND b.account_id=$2 ORDER BY b.event_sequence`, tenant, req.Entry.AccountID)
		if queryErr != nil {
			return balance.PostReceipt{}, fmt.Errorf("load correction parent: %w", queryErr)
		}
		for rows.Next() {
			parent, scanErr := scanEntryLineage(rows)
			if scanErr != nil {
				rows.Close()
				return balance.PostReceipt{}, scanErr
			}
			if parent.Digest() == req.Entry.SupersedesDigest && parent.DefinitionID == req.Entry.DefinitionID && parent.DefinitionVersion == req.Entry.DefinitionVersion {
				parentRef = parent.rowID
				break
			}
		}
		if rows.Err() != nil {
			rows.Close()
			return balance.PostReceipt{}, fmt.Errorf("load correction parent: %w", rows.Err())
		}
		rows.Close()
		if parentRef == uuid.Nil {
			return balance.PostReceipt{}, codedError{code: "BALANCE_INVALID", err: ErrInvalid, text: "correction parent not found"}
		}
	}
	amountProjection := req.Entry.Amount.String()
	if err := lock(ctx, tx, "account:"+tenant.String()+":"+req.Entry.AccountID); err != nil {
		return balance.PostReceipt{}, err
	}
	var count, max, min int64
	if err := tx.QueryRow(ctx, `SELECT count(*), COALESCE(max(event_sequence),0), COALESCE(min(event_sequence),0) FROM balance_entry WHERE tenant_id=$1 AND account_id=$2`, tenant, req.Entry.AccountID).Scan(&count, &max, &min); err != nil {
		return balance.PostReceipt{}, fmt.Errorf("read balance head: %w", err)
	}
	if count != max || (count > 0 && min != 1) {
		return balance.PostReceipt{}, codedError{code: CodeSequenceGap, err: ErrSequenceGap, text: fmt.Sprintf("%s: account %s", CodeSequenceGap, req.Entry.AccountID)}
	}
	if req.ExpectedHead != max {
		return balance.PostReceipt{}, codedError{code: CodeStaleCAS, err: ErrStaleCAS, text: fmt.Sprintf("%s: expected %d actual %d", CodeStaleCAS, req.ExpectedHead, max)}
	}
	dimensions, err := json.Marshal(req.Entry.Dimensions)
	if err != nil {
		return balance.PostReceipt{}, fmt.Errorf("marshal dimensions: %w", err)
	}
	insertSQL := `
		INSERT INTO balance_entry (row_id,tenant_id,account_id,definition_id,definition_version,unit,currency,subject,period,dimensions,kind,amount,entry_type,source_transaction_id,idempotency_key,effective_at,authorized_at,event_sequence,supersedes_digest,supersedes_row_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11,$12::numeric,$13,$14,$15,$16,$17,$18,$19,$20)
		RETURNING row_id, recorded_at`
	args := []any{uuid.New(), tenant, req.Entry.AccountID, req.Entry.DefinitionID, int64(def.Revision), req.Entry.Unit,
		optionalText(req.Entry.Currency), req.Entry.Subject, req.Entry.Period, string(dimensions), string(req.Entry.Kind), amountProjection, req.Entry.EntryType,
		optionalText(req.Entry.SourceTransactionID), req.Entry.IdempotencyKey, optionalInstant(req.Entry.EffectiveAt), optionalInstant(req.Entry.AuthorizedAt), max + 1, optionalText(req.Entry.SupersedesDigest), optionalParentUUID(parentRef)}
	if lineage {
		insertSQL = `INSERT INTO balance_entry (row_id,tenant_id,account_id,definition_id,definition_version,unit,currency,subject,period,dimensions,kind,amount,entry_type,source_transaction_id,idempotency_key,effective_at,authorized_at,event_sequence,supersedes_digest,supersedes_row_id,amount_exact,amount_scale,amount_rounding,effective_at_submicro,authorized_at_submicro) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11,$12::numeric,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25) RETURNING row_id, recorded_at`
		args = append(args, req.Entry.Amount.String(), req.Entry.Amount.Scale(), req.Entry.Amount.Rounding().String(), submicro(req.Entry.EffectiveAt), submicro(req.Entry.AuthorizedAt))
	} else if req.Entry.SupersedesDigest == "" {
		insertSQL = `INSERT INTO balance_entry (row_id,tenant_id,account_id,definition_id,definition_version,unit,currency,subject,period,dimensions,kind,amount,entry_type,source_transaction_id,idempotency_key,effective_at,authorized_at,event_sequence) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11,$12::numeric,$13,$14,$15,$16,$17,$18) RETURNING row_id, recorded_at`
		args = args[:18]
	}
	row := tx.QueryRow(ctx, insertSQL, args...)
	var rowID uuid.UUID
	var recordedAt time.Time
	if err := row.Scan(&rowID, &recordedAt); err != nil {
		if isUnique(err) {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.ConstraintName == "balance_entry_one_correction_per_parent" {
				return balance.PostReceipt{}, codedError{code: CodeCorrectionConflict, err: ErrCorrectionConflict, text: CodeCorrectionConflict}
			}
			return balance.PostReceipt{}, codedError{code: CodeIdempotencyConflict, err: ErrIdempotencyConflict, text: fmt.Sprintf("%s: %s", CodeIdempotencyConflict, req.Entry.IdempotencyKey)}
		}
		return balance.PostReceipt{}, fmt.Errorf("append balance entry: %w", err)
	}
	posted := req.Entry
	posted.RecordedAt = values.NewInstant(recordedAt.UTC())
	return balance.PostReceipt{Entry: posted, Head: max + 1, Digest: posted.Digest()}, nil
}

type storedReceipt struct {
	receipt       balance.PostReceipt
	eventSequence int64
}
type storedEntry struct {
	balance.BalanceEntry
	TenantID      uuid.UUID
	eventSequence int64
	rowID         uuid.UUID
}

func (e storedEntry) Digest() string { return e.BalanceEntry.Digest() }

func (s Store) loadEntryByKey(ctx context.Context, q dbport.Querier, tenant uuid.UUID, key string, lineage bool) (storedReceipt, bool, error) {
	columns := entryColumns
	if lineage {
		columns = entryColumnsLineage
	}
	row := q.QueryRow(ctx, columns+` FROM balance_entry b JOIN accumulator_definition d ON d.tenant_id=b.tenant_id AND d.definition_id=b.definition_id AND d.revision=b.definition_version WHERE b.tenant_id=$1 AND b.idempotency_key=$2`, tenant, key)
	var e storedEntry
	var err error
	if lineage {
		e, err = scanEntryLineage(row)
	} else {
		e, err = scanEntry(row)
	}
	if errors.Is(err, dbport.ErrNoRows) {
		return storedReceipt{}, false, nil
	}
	if err != nil {
		return storedReceipt{}, false, fmt.Errorf("load idempotent balance entry: %w", err)
	}
	return storedReceipt{receipt: balance.PostReceipt{Entry: e.BalanceEntry, Head: e.eventSequence, Digest: e.Digest()}, eventSequence: e.eventSequence}, true, nil
}

const entryColumns = `SELECT b.row_id,b.tenant_id,b.account_id,b.definition_id,d.version,b.unit,b.currency,b.subject,b.period,b.dimensions::text,b.kind,b.amount::text,b.entry_type,b.source_transaction_id,b.idempotency_key,b.effective_at,b.recorded_at,b.authorized_at,b.event_sequence`
const entryColumnsLineage = entryColumns + `,b.supersedes_digest,b.amount_exact,b.amount_scale,b.amount_rounding,b.effective_at_submicro,b.authorized_at_submicro`

func scanEntry(src interface{ Scan(...any) error }) (storedEntry, error) {
	return scanEntryWithLineage(src, false)
}
func scanEntryLineage(src interface{ Scan(...any) error }) (storedEntry, error) {
	return scanEntryWithLineage(src, true)
}
func scanEntryWithLineage(src interface{ Scan(...any) error }, lineage bool) (storedEntry, error) {
	var e storedEntry
	var amount, dimensions string
	var version, kind string
	var sourceTransaction *string
	var supersedesDigest *string
	var amountExact, amountRounding *string
	var amountScale, effectiveSubmicro, authorizedSubmicro *int32
	var effective, recorded, authorized *time.Time
	var sequence int64
	args := []any{&e.rowID, &e.TenantID, &e.AccountID, &e.DefinitionID, &version, &e.Unit, &e.Currency, &e.Subject, &e.Period, &dimensions, &kind, &amount, &e.EntryType, &sourceTransaction, &e.IdempotencyKey, &effective, &recorded, &authorized, &sequence}
	if lineage {
		args = append(args, &supersedesDigest, &amountExact, &amountScale, &amountRounding, &effectiveSubmicro, &authorizedSubmicro)
	}
	if err := src.Scan(args...); err != nil {
		return storedEntry{}, err
	}
	e.DefinitionVersion = version
	e.eventSequence = sequence
	e.RecordedAt = instantFromPtr(recorded)
	e.EffectiveAt = instantFromPtr(effective)
	e.AuthorizedAt = instantFromPtr(authorized)
	e.Kind = balance.EntryKind(kind)
	if sourceTransaction != nil {
		e.SourceTransactionID = *sourceTransaction
	}
	if supersedesDigest != nil {
		e.SupersedesDigest = *supersedesDigest
	}
	if err := json.Unmarshal([]byte(dimensions), &e.Dimensions); err != nil {
		return storedEntry{}, fmt.Errorf("decode dimensions: %w", err)
	}
	decimalText, scale, rounding := amount, int32(4), values.RoundingExactRequired
	if amountExact != nil || amountScale != nil || amountRounding != nil {
		if amountExact == nil || amountScale == nil || amountRounding == nil {
			return storedEntry{}, errors.New("balancestore: partial exact amount metadata")
		}
		var err error
		decimalText, scale, rounding = *amountExact, *amountScale, values.RoundingExactRequired
		rounding, err = parseRounding(*amountRounding)
		if err != nil {
			return storedEntry{}, err
		}
	}
	decimal, err := values.NewDecimal(decimalText, scale, rounding)
	if err != nil {
		return storedEntry{}, fmt.Errorf("decode amount: %w", err)
	}
	if amountExact != nil {
		projection, err := values.NewDecimal(amount, scale, rounding)
		if err != nil || projection.String() != decimal.String() {
			return storedEntry{}, errors.New("balancestore: exact amount metadata disagrees with numeric projection")
		}
	}
	e.Amount = decimal
	e.EffectiveAt = restoreSubmicro(effective, effectiveSubmicro)
	e.AuthorizedAt = restoreSubmicro(authorized, authorizedSubmicro)
	return e, nil
}

func (s Store) Entries(ctx context.Context, q dbport.Querier, tenant uuid.UUID, accountID string) ([]balance.BalanceEntry, error) {
	lineage, err := hasCorrectionLineage(ctx, q)
	if err != nil {
		return nil, err
	}
	columns := entryColumns
	if lineage {
		columns = entryColumnsLineage
	}
	rows, err := q.Query(ctx, columns+` FROM balance_entry b JOIN accumulator_definition d ON d.tenant_id=b.tenant_id AND d.definition_id=b.definition_id AND d.revision=b.definition_version WHERE b.tenant_id=$1 AND b.account_id=$2 ORDER BY b.event_sequence`, tenant, accountID)
	if err != nil {
		return nil, fmt.Errorf("list balance entries: %w", err)
	}
	defer rows.Close()
	var out []balance.BalanceEntry
	for rows.Next() {
		var e storedEntry
		var scanErr error
		if lineage {
			e, scanErr = scanEntryLineage(rows)
		} else {
			e, scanErr = scanEntry(rows)
		}
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, e.BalanceEntry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s Store) Head(ctx context.Context, q dbport.Querier, tenant uuid.UUID, accountID string) (int64, error) {
	var head int64
	if err := q.QueryRow(ctx, `SELECT COALESCE(max(event_sequence),0) FROM balance_entry WHERE tenant_id=$1 AND account_id=$2`, tenant, accountID).Scan(&head); err != nil {
		return 0, fmt.Errorf("read balance head: %w", err)
	}
	return head, nil
}

func (s Store) EntryRef(ctx context.Context, q dbport.Querier, tenant uuid.UUID, accountID, idempotencyKey string) (uuid.UUID, error) {
	var id uuid.UUID
	err := q.QueryRow(ctx, `SELECT row_id FROM balance_entry WHERE tenant_id=$1 AND account_id=$2 AND idempotency_key=$3`, tenant, accountID, idempotencyKey).Scan(&id)
	if errors.Is(err, dbport.ErrNoRows) {
		return uuid.Nil, codedError{code: CodeEntryNotFound, err: ErrEntryNotFound, text: CodeEntryNotFound}
	}
	return id, err
}

func (s Store) AppendLifecycle(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, rec LifecycleRecord) (LifecycleRecord, error) {
	if tenant == uuid.Nil || rec.EntryRef == uuid.Nil {
		return LifecycleRecord{}, invalid("entry_ref", "tenant and entry reference are required")
	}
	if rec.EventSequence <= 0 {
		return LifecycleRecord{}, invalid("event_sequence", "must be positive")
	}
	if rec.Entry.Operation == "" {
		return LifecycleRecord{}, invalid("operation", "is required")
	}
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM balance_entry WHERE tenant_id=$1 AND row_id=$2)`, tenant, rec.EntryRef).Scan(&exists); err != nil {
		return LifecycleRecord{}, fmt.Errorf("check lifecycle entry reference: %w", err)
	}
	if !exists {
		return LifecycleRecord{}, codedError{code: CodeEntryNotFound, err: ErrEntryNotFound, text: CodeEntryNotFound}
	}
	if err := lock(ctx, tx, "lifecycle:"+tenant.String()+":"+rec.EntryRef.String()); err != nil {
		return LifecycleRecord{}, err
	}
	var max int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(max(event_sequence),0) FROM balance_lifecycle_entry WHERE tenant_id=$1 AND entry_ref=$2`, tenant, rec.EntryRef).Scan(&max); err != nil {
		return LifecycleRecord{}, err
	}
	if rec.EventSequence != max+1 {
		code, base := CodeSequenceGap, ErrSequenceGap
		if rec.EventSequence == max {
			code, base = CodeDuplicateEvent, ErrDuplicateEvent
		}
		return LifecycleRecord{}, codedError{code: code, err: base, text: fmt.Sprintf("%s: expected %d got %d", code, max+1, rec.EventSequence)}
	}
	sources, err := json.Marshal(rec.Entry.SourceEntries)
	if err != nil {
		return LifecycleRecord{}, err
	}
	policyVersion, err := optionalVersion(rec.Entry.PolicyVersion)
	if err != nil {
		return LifecycleRecord{}, err
	}
	row := tx.QueryRow(ctx, `INSERT INTO balance_lifecycle_entry (row_id,tenant_id,entry_ref,operation,reason,policy_id,policy_version,source_period,target_period,source_entries,event_sequence) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11) RETURNING row_id,recorded_at`, uuid.New(), tenant, rec.EntryRef, string(rec.Entry.Operation), optionalText(rec.Entry.Reason), optionalText(rec.Entry.PolicyID), policyVersion, optionalText(rec.Entry.SourcePeriod.ID), optionalText(rec.Entry.TargetPeriod.ID), string(sources), rec.EventSequence)
	if err := row.Scan(&rec.RowID, &rec.RecordedAt); err != nil {
		if isUnique(err) {
			return LifecycleRecord{}, codedError{code: CodeDuplicateEvent, err: ErrDuplicateEvent, text: CodeDuplicateEvent}
		}
		return LifecycleRecord{}, err
	}
	rec.TenantID, rec.RecordedAt = tenant, rec.RecordedAt.UTC()
	return rec, nil
}

func (s Store) Lifecycle(ctx context.Context, q dbport.Querier, tenant, entryRef uuid.UUID) ([]LifecycleRecord, error) {
	rows, err := q.Query(ctx, `SELECT row_id,entry_ref,operation,reason,policy_id,policy_version,source_period,target_period,source_entries,event_sequence,recorded_at FROM balance_lifecycle_entry WHERE tenant_id=$1 AND entry_ref=$2 ORDER BY event_sequence`, tenant, entryRef)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LifecycleRecord
	for rows.Next() {
		var r LifecycleRecord
		var operation string
		var reason, policyID, sourcePeriod, targetPeriod, sourceEntries *string
		var policyVersion *int64
		if err := rows.Scan(&r.RowID, &r.EntryRef, &operation, &reason, &policyID, &policyVersion, &sourcePeriod, &targetPeriod, &sourceEntries, &r.EventSequence, &r.RecordedAt); err != nil {
			return nil, err
		}
		r.TenantID = tenant
		r.Entry.Operation = balance.LifecycleOperation(operation)
		if reason != nil {
			r.Entry.Reason = *reason
		}
		if policyID != nil {
			r.Entry.PolicyID = *policyID
		}
		if policyVersion != nil {
			r.Entry.PolicyVersion = strconv.FormatInt(*policyVersion, 10)
		}
		if sourcePeriod != nil {
			r.Entry.SourcePeriod.ID = *sourcePeriod
		}
		if targetPeriod != nil {
			r.Entry.TargetPeriod.ID = *targetPeriod
		}
		if sourceEntries != nil {
			if err := json.Unmarshal([]byte(*sourceEntries), &r.Entry.SourceEntries); err != nil {
				return nil, err
			}
		}
		r.RecordedAt = r.RecordedAt.UTC()
		out = append(out, r)
	}
	return out, rows.Err()
}

func lock(ctx context.Context, tx dbport.Execer, key string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, key)
	return err
}
func isUnique(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
func optionalText(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func currencyValue(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
func optionalInstant(i values.Instant) any {
	if !i.IsSet() {
		return nil
	}
	t := i.Time()
	return time.Unix(t.Unix(), int64(t.Nanosecond()/1000*1000)).UTC()
}

func submicro(i values.Instant) any {
	if !i.IsSet() {
		return nil
	}
	_, nanos := i.Unix()
	return nanos % 1000
}

func hasCorrectionLineage(ctx context.Context, q dbport.Querier) (bool, error) {
	var installed bool
	if err := q.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM pg_attribute
		WHERE attrelid = to_regclass('balance_entry')
		  AND attname IN ('supersedes_digest','supersedes_row_id')
		  AND NOT attisdropped
		GROUP BY attrelid HAVING count(*) = 2)`).Scan(&installed); err != nil {
		return false, fmt.Errorf("inspect balance correction lineage storage: %w", err)
	}
	return installed, nil
}

func restoreSubmicro(base *time.Time, remainder *int32) values.Instant {
	if base == nil {
		return values.Instant{}
	}
	nanos := base.Nanosecond() / 1000 * 1000
	if remainder != nil {
		nanos += int(*remainder)
	}
	return values.NewInstant(time.Unix(base.Unix(), int64(nanos)).UTC())
}

func parseRounding(text string) (values.RoundingMode, error) {
	for _, mode := range []values.RoundingMode{values.RoundingExactRequired, values.RoundingTowardZero, values.RoundingAwayFromZero, values.RoundingHalfEven, values.RoundingHalfUp, values.RoundingHalfAwayFromZero, values.RoundingFloor, values.RoundingCeiling} {
		if mode.String() == text {
			return mode, nil
		}
	}
	return 0, fmt.Errorf("balancestore: invalid exact amount rounding %q", text)
}

func optionalParentUUID(id uuid.UUID) any {
	if id == uuid.Nil {
		return nil
	}
	return id
}
func instantFromPtr(t *time.Time) values.Instant {
	if t == nil {
		return values.Instant{}
	}
	return values.NewInstant(t.UTC())
}
func optionalVersion(s string) (any, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return nil, invalid("policy_version", "must be a positive integer")
	}
	return n, nil
}
