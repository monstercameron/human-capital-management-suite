// Package commit composes the correctness-bearing ledger write set that must
// share the transaction opened by internal/transaction/commit.  It deliberately
// does not open, commit, or roll back the caller's transaction.
package commit

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	datalogger "github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/data/outbox"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projection"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projection/critical"
	"github.com/monstercameron/human-capital-management-suite/internal/data/provenance"
)

// Failpoint is called after each correctness-bearing phase. Returning an error
// causes every phase in this composition to roll back to its savepoint.
type Failpoint func(stage string) error

// Request describes one event and all of its synchronous, correctness-bearing
// companions. The stream, payload schema, and projection checkpoint should be
// registered by the caller's transaction before Commit is called, just as they
// are for the lower-level append APIs.
type Request struct {
	Append     datalogger.AppendRequest
	Projection string
	Outbox     outbox.OutboxSpec
	Provenance provenance.PublishRequest
	Failpoint  Failpoint
	RecordedAt time.Time
	// CriticalMapper decodes IntentInstance/ProposalRevision events for the
	// critical projection. Nil selects critical.ProtoMapper, the one decode
	// path the projection and RevisionStore.Materialize share. It is ignored
	// for events whose schema is not a critical schema.
	CriticalMapper critical.Mapper
}

// Receipt names every durable result produced by Commit. OutboxIDs includes
// the business outbox message and the provenance publication message.
type Receipt struct {
	Ledger                datalogger.AppendReceipt
	Projection            projection.ApplyResult
	Critical              critical.ApplyResult
	Outbox                outbox.Record
	Provenance            provenance.Record
	OutboxIDs             []uuid.UUID
	ProvenanceEndpointIDs []uuid.UUID
}

// Appender is the only ledger capability this package needs.
type Appender interface {
	Append(context.Context, dbport.Tx, datalogger.AppendRequest) (datalogger.AppendReceipt, error)
}

// isCriticalSchema reports whether the event carries a payload the
// critical projection owns. Only those events reach critical.Apply; every
// other schema keeps the previous checkpoint-only behavior untouched.
func isCriticalSchema(schemaRef string) bool {
	return schemaRef == critical.SchemaRefIntentInstance ||
		schemaRef == critical.SchemaRefProposalRevision
}

func criticalMapperFor(req Request) critical.Mapper {
	if req.CriticalMapper != nil {
		return req.CriticalMapper
	}
	return critical.ProtoMapper{}
}

// applyCritical projects the just-appended event into intent_instance or
// proposal_revision in the same transaction, under the critical
// checkpoint's CAS guard. A replayed sequence is a no-op; a stale version
// or a conflicting revision fails the whole commit through the savepoint.
func applyCritical(ctx context.Context, tx dbport.Tx, req Request, ledgerReceipt datalogger.AppendReceipt) (critical.ApplyResult, error) {
	return critical.Apply(ctx, tx, criticalMapperFor(req), critical.ApplyRequest{
		Tenant:    req.Append.Tenant,
		StreamKey: req.Append.StreamKey,
		Sequence:  ledgerReceipt.Sequence,
		Digest:    ledgerReceipt.Digest,
		SchemaRef: req.Append.SchemaRef,
		Payload:   req.Append.Payload,
	})
}

// Commit appends the event, advances the caller's checkpoint plus the
// critical intent/proposal projection when the event is a critical one,
// enqueues the business effect, and records/publishes the provenance root
// in one caller-owned PostgreSQL transaction. The savepoint makes a failed
// composition atomic even when the caller keeps the outer transaction open
// to inspect or roll it back.
func Commit(ctx context.Context, tx dbport.Tx, appender Appender, req Request) (receipt Receipt, err error) {
	if tx == nil {
		return Receipt{}, errors.New("ledger commit: transaction is required")
	}
	if appender == nil {
		return Receipt{}, errors.New("ledger commit: appender is required")
	}
	if req.Projection == "" {
		return Receipt{}, errors.New("ledger commit: projection is required")
	}
	if _, err := tx.Exec(ctx, `SAVEPOINT ledger_commit_composition`); err != nil {
		return Receipt{}, fmt.Errorf("ledger commit: create savepoint: %w", err)
	}
	defer func() {
		if err != nil {
			_, _ = tx.Exec(ctx, `ROLLBACK TO SAVEPOINT ledger_commit_composition`)
		}
		_, _ = tx.Exec(ctx, `RELEASE SAVEPOINT ledger_commit_composition`)
	}()

	if err = fail(req.Failpoint, "before-event"); err != nil {
		return Receipt{}, err
	}
	receipt.Ledger, err = appender.Append(ctx, tx, req.Append)
	if err != nil {
		return Receipt{}, fmt.Errorf("ledger commit: append event: %w", err)
	}
	if err = fail(req.Failpoint, "after-event"); err != nil {
		return Receipt{}, err
	}

	if req.Projection == critical.ProjectionName {
		// The caller's checkpoint is the critical checkpoint: critical.Apply
		// already advances it CAS-guarded, so a second generic advance would
		// turn the row write into a replay no-op. Do it once and derive both
		// receipts from the one application.
		criticalRes, applyErr := applyCritical(ctx, tx, req, receipt.Ledger)
		if applyErr != nil {
			return Receipt{}, fmt.Errorf("ledger commit: apply critical projection: %w", applyErr)
		}
		receipt.Projection = projection.ApplyResult{Checkpoint: criticalRes.Checkpoint, Applied: criticalRes.Applied}
		receipt.Critical = criticalRes
	} else {
		receipt.Projection, err = projection.Apply(ctx, tx, projection.ApplyRequest{
			Tenant:         req.Append.Tenant,
			ProjectionName: req.Projection,
			StreamKey:      req.Append.StreamKey,
			Sequence:       receipt.Ledger.Sequence,
			Digest:         receipt.Ledger.Digest,
		})
		if err != nil {
			return Receipt{}, fmt.Errorf("ledger commit: apply critical projection: %w", err)
		}
		if isCriticalSchema(req.Append.SchemaRef) {
			criticalRes, applyErr := applyCritical(ctx, tx, req, receipt.Ledger)
			if applyErr != nil {
				return Receipt{}, fmt.Errorf("ledger commit: apply critical projection: %w", applyErr)
			}
			receipt.Critical = criticalRes
		}
	}
	if err = fail(req.Failpoint, "after-projection"); err != nil {
		return Receipt{}, err
	}

	receipt.Outbox, err = outbox.Enqueue(ctx, tx, outbox.EnqueueRequest{
		Tenant:         req.Append.Tenant,
		OutboxID:       receipt.Ledger.EventID,
		EffectIdentity: req.Outbox.EffectIdentity,
		OrderingKey:    req.Outbox.OrderingKey,
		SchemaRef:      req.Outbox.SchemaRef,
		Payload:        req.Outbox.Payload,
	})
	if err != nil {
		return Receipt{}, fmt.Errorf("ledger commit: enqueue business outbox: %w", err)
	}
	if err = fail(req.Failpoint, "after-outbox"); err != nil {
		return Receipt{}, err
	}

	provReq, err := bindProvenance(req.Provenance, req.Append, receipt.Ledger, req.RecordedAt)
	if err != nil {
		return Receipt{}, err
	}
	if err := ensureProvenanceSchema(ctx, tx, req.Append.Tenant); err != nil {
		return Receipt{}, err
	}
	receipt.Provenance, err = provenance.Publish(ctx, tx, provReq)
	if err != nil {
		return Receipt{}, fmt.Errorf("ledger commit: publish provenance root: %w", err)
	}
	if err = fail(req.Failpoint, "after-provenance"); err != nil {
		return Receipt{}, err
	}

	receipt.OutboxIDs = []uuid.UUID{receipt.Outbox.OutboxID, provenanceOutboxID(receipt.Provenance)}
	receipt.ProvenanceEndpointIDs = []uuid.UUID{receipt.Provenance.RecordID}
	return receipt, nil
}

func bindProvenance(req provenance.PublishRequest, appendReq datalogger.AppendRequest, event datalogger.AppendReceipt, recordedAt time.Time) (provenance.PublishRequest, error) {
	if req.Tenant == uuid.Nil {
		req.Tenant = appendReq.Tenant
	}
	if req.Tenant != appendReq.Tenant {
		return provenance.PublishRequest{}, fmt.Errorf("ledger commit: provenance tenant %s differs from event tenant %s", req.Tenant, appendReq.Tenant)
	}
	if req.SourceKind == "" {
		req.SourceKind = provenance.SourceLedgerEvent
	}
	if req.SourceKind != provenance.SourceLedgerEvent {
		return provenance.PublishRequest{}, fmt.Errorf("ledger commit: provenance root for an event must use source kind %s", provenance.SourceLedgerEvent)
	}
	if req.SourceRef == "" {
		req.SourceRef = provenance.LedgerEventSourceRef(event.StreamKey, event.Sequence)
	}
	if req.StreamKey == "" {
		req.StreamKey = event.StreamKey
	}
	if req.Sequence == 0 {
		req.Sequence = event.Sequence
	}
	if req.EventID == uuid.Nil {
		req.EventID = event.EventID
	}
	if req.PublishedAt.IsZero() {
		if recordedAt.IsZero() {
			req.PublishedAt = event.RecordedAt
		} else {
			req.PublishedAt = recordedAt.UTC()
		}
	}
	if len(req.Digests) == 0 {
		req.Digests = []provenance.Digest{{Kind: "ledger_event", Algorithm: event.DigestAlgorithm, Digest: event.Digest}}
	}
	if len(req.EvidenceIDs) == 0 {
		req.EvidenceIDs = []string{event.EventID.String()}
	}
	return req, nil
}

func provenanceOutboxID(record provenance.Record) uuid.UUID { return record.RecordID }

func ensureProvenanceSchema(ctx context.Context, tx dbport.Tx, tenant uuid.UUID) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO payload_schema (
			tenant_id, schema_ref, schema_id, schema_version,
			message_full_name, wire_format, canonicalization_profile)
		VALUES ($1, $2, $2, 1, $2, 'PROTOBUF', 'EVIDENCE_MANIFEST')
		ON CONFLICT (tenant_id, schema_ref) DO NOTHING`, tenant, provenance.OutboxSchemaRef)
	if err != nil {
		return fmt.Errorf("ledger commit: register provenance outbox schema: %w", err)
	}
	return nil
}

func fail(f Failpoint, stage string) error {
	if f == nil {
		return nil
	}
	if err := f(stage); err != nil {
		return fmt.Errorf("ledger commit: failpoint %s: %w", stage, err)
	}
	return nil
}

// Version is the package contract version used by architecture tooling.
func Version() int { return 1 }

// Explain returns bounded operational metadata without payload material.
func Explain() string {
	return fmt.Sprintf("ledger.commit v%d: event, critical projection, outbox and provenance in one caller-owned transaction", Version())
}
