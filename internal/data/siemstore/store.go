// Package siemstore persists tenant-ordered security feed events and enqueues
// a durable dispatch reference in the shared outbox in the same transaction.
package siemstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/outbox"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

const (
	OutboxSchemaRef = "hcmnext.security.siem.event-reference@1"
	OutboxSchemaID  = "hcmnext.security.siem.event-reference"
	SchemaVersion   = 1
)

var (
	ErrInvalid          = errors.New("siemstore: invalid request")
	ErrIdentityConflict = errors.New("siemstore: source identity conflicts with persisted event")
	ErrInvalidCursor    = errors.New("siemstore: cursor does not match tenant feed")
)

// DB is the transaction-opening capability used by Store.
type DB interface{ dbport.Beginner }

type Store struct{ db DB }

func New(db DB) *Store { return &Store{db: db} }

// AppendRequest is a minimized source event. It carries no raw event payload.
type AppendRequest struct {
	Type           string
	OccurredAt     time.Time
	SourceRef      string
	EvidenceDigest string
	RuleID         string
	RuleVersion    int
}

// Record is one immutable, tenant-sequenced SIEM feed event.
type Record struct {
	Tenant         uuid.UUID
	Sequence       uint64
	Type           string
	OccurredAt     time.Time
	SourceRef      string
	EvidenceDigest string
	RuleID         string
	RuleVersion    int
	PreviousDigest string
	Digest         string
}

// Page contains an ordered immutable page after the supplied cursor.
type Page struct {
	Tenant uuid.UUID
	From   uint64
	Events []Record
	Next   uint64
	Digest string
}

func (r AppendRequest) validate() error {
	if !validType(r.Type) || r.OccurredAt.IsZero() || !safeText(r.SourceRef, 512) || !validDigest(r.EvidenceDigest) {
		return ErrInvalid
	}
	if r.Type == "SECURITY_ALERT_RULE" {
		if !safeText(r.RuleID, 128) || r.RuleVersion <= 0 {
			return ErrInvalid
		}
	} else if r.RuleID != "" || r.RuleVersion != 0 {
		return ErrInvalid
	}
	return nil
}

func validType(kind string) bool {
	switch kind {
	case "SECURITY_ALERT_RULE", "DLP_SIGNAL", "ACCESS_SIGNAL", "ADMIN_ACTION":
		return true
	default:
		return false
	}
}

func safeText(value string, max int) bool {
	return value != "" && strings.TrimSpace(value) == value && len(value) <= max &&
		!strings.ContainsAny(value, "|\r\n\x00")
}

func validDigest(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

type Executor interface {
	dbport.Execer
	dbport.Querier
}

func registerOutboxSchema(ctx context.Context, tx Executor, tenant uuid.UUID) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO payload_schema (
			tenant_id, schema_ref, schema_id, schema_version,
			message_full_name, wire_format, canonicalization_profile)
		VALUES ($1,$2,$3,$4,'google.protobuf.StringValue','PROTOBUF','ARTIFACT_REFERENCE')
		ON CONFLICT DO NOTHING`, tenant, OutboxSchemaRef, OutboxSchemaID, SchemaVersion)
	if err != nil {
		return fmt.Errorf("siemstore: register outbox reference schema: %w", err)
	}
	var schemaID, fullName, format, profile string
	var version int
	if err := tx.QueryRow(ctx, `SELECT schema_id,schema_version,message_full_name,wire_format,canonicalization_profile
		FROM payload_schema WHERE tenant_id=$1 AND schema_ref=$2`, tenant, OutboxSchemaRef).
		Scan(&schemaID, &version, &fullName, &format, &profile); err != nil {
		return fmt.Errorf("siemstore: verify outbox reference schema: %w", err)
	}
	if schemaID != OutboxSchemaID || version != SchemaVersion || fullName != "google.protobuf.StringValue" || format != "PROTOBUF" || profile != "ARTIFACT_REFERENCE" {
		return fmt.Errorf("%w: outbox reference schema was registered with different metadata", ErrInvalid)
	}
	return nil
}

// Append persists one event, advances the tenant's chain head, and creates
// the provider dispatch intent atomically. A source replay returns its first
// immutable record, while a changed replay under the same identity refuses.
func (s *Store) Append(ctx context.Context, tenant uuid.UUID, request AppendRequest) (Record, error) {
	if s == nil || s.db == nil || tenant == uuid.Nil || request.validate() != nil {
		return Record{}, ErrInvalid
	}
	request.OccurredAt = request.OccurredAt.UTC()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Record{}, fmt.Errorf("siemstore: append begin: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		return Record{}, err
	}
	if err := registerOutboxSchema(ctx, tx, tenant); err != nil {
		return Record{}, err
	}
	identity := sourceIdentity(request)
	if existing, err := readByIdentity(ctx, tx, tenant, identity); err == nil {
		if !sameRequest(existing, request) {
			return Record{}, fmt.Errorf("%w: %s", ErrIdentityConflict, identity)
		}
		if err := tx.Commit(ctx); err != nil {
			return Record{}, fmt.Errorf("siemstore: append replay commit: %w", err)
		}
		committed = true
		return existing, nil
	} else if !errors.Is(err, dbport.ErrNoRows) {
		return Record{}, fmt.Errorf("siemstore: find source replay: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO security_siem_feed_head (tenant_id) VALUES ($1) ON CONFLICT DO NOTHING`, tenant); err != nil {
		return Record{}, fmt.Errorf("siemstore: initialize tenant feed: %w", err)
	}
	var previousSequence int64
	var previousDigest *string
	if err := tx.QueryRow(ctx, `SELECT last_sequence,last_digest FROM security_siem_feed_head WHERE tenant_id=$1 FOR UPDATE`, tenant).Scan(&previousSequence, &previousDigest); err != nil {
		return Record{}, fmt.Errorf("siemstore: lock tenant feed head: %w", err)
	}
	if previousSequence < 0 {
		return Record{}, fmt.Errorf("%w: persisted head sequence is negative", ErrInvalid)
	}
	previous := ""
	if previousDigest != nil {
		previous = *previousDigest
	}
	sequence := uint64(previousSequence + 1)
	record := Record{Tenant: tenant, Sequence: sequence, Type: request.Type, OccurredAt: request.OccurredAt,
		SourceRef: request.SourceRef, EvidenceDigest: request.EvidenceDigest, RuleID: request.RuleID,
		RuleVersion: request.RuleVersion, PreviousDigest: previous}
	record.Digest = recordDigest(record)
	if _, err := tx.Exec(ctx, `
		INSERT INTO security_siem_feed_event (
			tenant_id, sequence, source_identity, event_type, occurred_at, source_ref,
			evidence_digest, rule_id, rule_version, previous_digest, event_digest)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, tenant, sequence, identity, request.Type,
		request.OccurredAt, request.SourceRef, request.EvidenceDigest, nullableString(request.RuleID), nullableInt(request.RuleVersion),
		nullableString(previous), record.Digest); err != nil {
		return Record{}, fmt.Errorf("siemstore: insert feed event: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE security_siem_feed_head SET last_sequence=$2,last_digest=$3,updated_at=now() WHERE tenant_id=$1`, tenant, sequence, record.Digest); err != nil {
		return Record{}, fmt.Errorf("siemstore: update feed head: %w", err)
	}
	wirePayload, err := proto.Marshal(wrapperspb.String(fmt.Sprintf("%d", sequence)))
	if err != nil {
		return Record{}, fmt.Errorf("siemstore: encode outbox reference: %w", err)
	}
	if _, err := outbox.Enqueue(ctx, tx, outbox.EnqueueRequest{
		Tenant: tenant, EffectIdentity: "siem:" + identity, OrderingKey: "security-siem-feed",
		Criticality: outbox.CriticalityP1, SchemaRef: OutboxSchemaRef, Payload: wirePayload,
	}); err != nil {
		return Record{}, fmt.Errorf("siemstore: enqueue delivery intent: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Record{}, fmt.Errorf("siemstore: append commit: %w", err)
	}
	committed = true
	return record, nil
}

// ReadPage validates the cursor anchor in the tenant transaction and reads
// at most limit ordered records. The caller signs the returned page for the
// governed endpoint after this durable source read succeeds.
func (s *Store) ReadPage(ctx context.Context, tenant uuid.UUID, after uint64, digest string, limit int) (Page, error) {
	if s == nil || s.db == nil || tenant == uuid.Nil || limit <= 0 || limit > 1000 ||
		(after == 0 && digest != "") || (after > 0 && !validDigest(digest)) {
		return Page{}, ErrInvalidCursor
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Page{}, fmt.Errorf("siemstore: read page begin: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		return Page{}, err
	}
	var head int64
	err = tx.QueryRow(ctx, `SELECT last_sequence FROM security_siem_feed_head WHERE tenant_id=$1`, tenant).Scan(&head)
	if errors.Is(err, dbport.ErrNoRows) {
		head, err = 0, nil
	}
	if err != nil {
		return Page{}, fmt.Errorf("siemstore: read feed head: %w", err)
	}
	if head < 0 || after > uint64(head) {
		return Page{}, ErrInvalidCursor
	}
	if after > 0 {
		var anchor string
		if err := tx.QueryRow(ctx, `SELECT event_digest FROM security_siem_feed_event WHERE tenant_id=$1 AND sequence=$2`, tenant, after).Scan(&anchor); err != nil || anchor != digest {
			return Page{}, ErrInvalidCursor
		}
	}
	rows, err := tx.Query(ctx, `
		SELECT sequence,event_type,occurred_at,source_ref,evidence_digest,COALESCE(rule_id,''),
			COALESCE(rule_version,0),COALESCE(previous_digest,''),event_digest
		FROM security_siem_feed_event WHERE tenant_id=$1 AND sequence>$2
		ORDER BY sequence LIMIT $3`, tenant, after, limit)
	if err != nil {
		return Page{}, fmt.Errorf("siemstore: query feed page: %w", err)
	}
	page := Page{Tenant: tenant, From: after, Next: after, Digest: digest}
	for rows.Next() {
		var record Record
		var sequence int64
		if err := rows.Scan(&sequence, &record.Type, &record.OccurredAt, &record.SourceRef, &record.EvidenceDigest,
			&record.RuleID, &record.RuleVersion, &record.PreviousDigest, &record.Digest); err != nil {
			rows.Close()
			return Page{}, fmt.Errorf("siemstore: scan feed event: %w", err)
		}
		if sequence <= 0 {
			rows.Close()
			return Page{}, fmt.Errorf("%w: persisted sequence is invalid", ErrInvalid)
		}
		record.Tenant, record.Sequence = tenant, uint64(sequence)
		page.Events = append(page.Events, record)
		page.Next, page.Digest = record.Sequence, record.Digest
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Page{}, fmt.Errorf("siemstore: read feed events: %w", err)
	}
	rows.Close()
	if err := tx.Commit(ctx); err != nil {
		return Page{}, fmt.Errorf("siemstore: read page commit: %w", err)
	}
	committed = true
	return page, nil
}

func readByIdentity(ctx context.Context, q dbport.Querier, tenant uuid.UUID, identity string) (Record, error) {
	var record Record
	var sequence int64
	err := q.QueryRow(ctx, `
		SELECT sequence,event_type,occurred_at,source_ref,evidence_digest,COALESCE(rule_id,''),
			COALESCE(rule_version,0),COALESCE(previous_digest,''),event_digest
		FROM security_siem_feed_event WHERE tenant_id=$1 AND source_identity=$2`, tenant, identity).
		Scan(&sequence, &record.Type, &record.OccurredAt, &record.SourceRef, &record.EvidenceDigest,
			&record.RuleID, &record.RuleVersion, &record.PreviousDigest, &record.Digest)
	if err != nil {
		return Record{}, err
	}
	if sequence <= 0 {
		return Record{}, fmt.Errorf("%w: persisted sequence is invalid", ErrInvalid)
	}
	record.Tenant, record.Sequence = tenant, uint64(sequence)
	return record, nil
}

func sameRequest(record Record, request AppendRequest) bool {
	return record.Type == request.Type && record.OccurredAt.Equal(request.OccurredAt) &&
		record.SourceRef == request.SourceRef && record.EvidenceDigest == request.EvidenceDigest &&
		record.RuleID == request.RuleID && record.RuleVersion == request.RuleVersion
}

func sourceIdentity(request AppendRequest) string {
	value := fmt.Sprintf("siem-source/v%d|%s|%s|%s|%s|%d", SchemaVersion, request.Type,
		request.SourceRef, request.EvidenceDigest, request.RuleID, request.RuleVersion)
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func recordDigest(record Record) string {
	value := fmt.Sprintf("siem/v%d|%d|%s|%s|%s|%s|%s|%s|%d|%s", SchemaVersion,
		record.Sequence, record.Tenant.String(), record.Type, record.OccurredAt.UTC().Format(time.RFC3339Nano),
		record.SourceRef, record.EvidenceDigest, record.RuleID, record.RuleVersion, record.PreviousDigest)
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableInt(value int) any {
	if value == 0 {
		return nil
	}
	return value
}
