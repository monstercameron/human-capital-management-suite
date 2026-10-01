package pgstore

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	datalogger "github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
)

// AppendJourneyFailure appends one authorized approval-start failure to the
// intent's existing ledger stream. The intent lookup precedes stream access so
// an unknown or cross-tenant identifier cannot create a forged stream.
func (s *Store) AppendJourneyFailure(ctx context.Context, event app.JourneyFailureEvent) error {
	if err := event.Validate(); err != nil {
		return fmt.Errorf("pgstore: validate journey failure: %w", err)
	}
	intentID, err := uuid.Parse(strings.TrimSpace(event.IntentID))
	if err != nil {
		return fmt.Errorf("pgstore: validate journey failure intent: %w", err)
	}
	if _, err := s.LoadIntent(ctx, event.Tenant, intentID.String()); err != nil {
		return fmt.Errorf("pgstore: authorize journey failure intent: %w", err)
	}
	tenantID := TenantID(event.Tenant)
	streamKey := StreamKey(intentID.String())
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("pgstore: begin journey failure: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := ensureJourneyFailureSchema(ctx, tx, tenantID); err != nil {
		return err
	}
	var head int64
	if err := tx.QueryRow(ctx, `SELECT head_sequence FROM stream_head WHERE tenant_id = $1 AND stream_key = $2`, tenantID, streamKey).Scan(&head); err != nil {
		return fmt.Errorf("pgstore: read journey failure stream head: %w", err)
	}
	payload, err := journeyFailurePayload(event)
	if err != nil {
		return err
	}
	receipt, err := s.appender.Append(ctx, tx, datalogger.AppendRequest{
		Tenant: tenantID, StreamKey: streamKey, ExpectedHead: head,
		AssertionClass: datalogger.TransactionFact, SourceRef: "hcmnext:journey",
		SchemaRef: app.JourneyFailureSchemaRef, Payload: payload,
		OccurredAt: event.OccurredAt.UTC(), EffectiveAt: event.OccurredAt.UTC(),
		CorrelationID:  uuid.NewSHA1(uuid.NameSpaceOID, []byte(event.IdempotencyKey)),
		IdempotencyKey: event.IdempotencyKey,
	})
	if err != nil {
		return fmt.Errorf("pgstore: append journey failure: %w", err)
	}
	if receipt.Sequence <= 0 {
		return fmt.Errorf("pgstore: append journey failure returned invalid sequence")
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("pgstore: commit journey failure: %w", err)
	}
	return nil
}

func ensureJourneyFailureSchema(ctx context.Context, tx dbport.Tx, tenant uuid.UUID) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO payload_schema (
			tenant_id, schema_ref, schema_id, schema_version,
			message_full_name, wire_format, canonicalization_profile)
		VALUES ($1, $2, $2, 1,
			'google.protobuf.Struct', 'PROTOBUF', 'LEDGER_EVENT')
		ON CONFLICT (tenant_id, schema_ref) DO NOTHING`, tenant, app.JourneyFailureSchemaRef)
	if err != nil {
		return fmt.Errorf("pgstore: ensure journey failure schema: %w", err)
	}
	return nil
}

func journeyFailurePayload(event app.JourneyFailureEvent) ([]byte, error) {
	values, err := structpb.NewStruct(map[string]any{
		"actor":       event.Actor,
		"intent_id":   event.IntentID,
		"reason_ref":  event.ReasonRef,
		"revision_id": event.RevisionID,
	})
	if err != nil {
		return nil, fmt.Errorf("pgstore: build journey failure payload: %w", err)
	}
	payload, err := (proto.MarshalOptions{Deterministic: true}).Marshal(values)
	if err != nil {
		return nil, fmt.Errorf("pgstore: marshal journey failure payload: %w", err)
	}
	return payload, nil
}
