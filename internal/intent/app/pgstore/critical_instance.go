package pgstore

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	datalogger "github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projection/critical"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/lifecycle"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/protomap"
	ledgerport "github.com/monstercameron/human-capital-management-suite/internal/ledger"
)

// applyInstanceLifecycle writes the next authoritative IntentInstance snapshot
// to the intent stream and materializes it through the critical projection in
// the same transaction. The existing event is the source for fields that the
// relational projection does not own.
func applyInstanceLifecycle(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, intentID uuid.UUID, nextVersion uint64, dimensions lifecycle.Dimensions, recordedAt time.Time) error {
	streamKey := StreamKey(intentID.String())
	events, err := ledgerport.NewReader().ReadStream(ctx, tx, tenant, streamKey)
	if err != nil {
		return fmt.Errorf("pgstore: read intent stream before lifecycle projection: %w", err)
	}
	var snapshot *intentsv1.IntentInstance
	for _, event := range events {
		if event.SchemaRef != critical.SchemaRefIntentInstance || len(event.Payload) == 0 {
			continue
		}
		var decoded intentsv1.IntentInstance
		if err := proto.Unmarshal(event.Payload, &decoded); err != nil {
			return fmt.Errorf("pgstore: decode intent snapshot at sequence %d: %w", event.Sequence, err)
		}
		snapshot = &decoded
	}
	if snapshot == nil || snapshot.GetIntentId() != intentID.String() {
		return fmt.Errorf("pgstore: intent stream %s has no matching IntentInstance snapshot", streamKey)
	}
	protoDimensions, err := protomap.DimensionsToProto(dimensions)
	if err != nil {
		return fmt.Errorf("pgstore: encode intent lifecycle dimensions: %w", err)
	}
	snapshot.Lifecycle = protoDimensions
	snapshot.InstanceVersion = nextVersion
	snapshot.RecordedAt = timestamppb.New(recordedAt.UTC())
	snapshot.LastTransitionAt = timestamppb.New(recordedAt.UTC())
	payload, err := proto.MarshalOptions{Deterministic: true}.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("pgstore: marshal intent lifecycle snapshot: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO payload_schema (
			tenant_id, schema_ref, schema_id, schema_version,
			message_full_name, wire_format, canonicalization_profile)
		VALUES ($1, $2, 'hcmnext.intents.v1.IntentInstance', 1,
			'hcmnext.intents.v1.IntentInstance', 'PROTOBUF', 'LEDGER_EVENT')
		ON CONFLICT (tenant_id, schema_ref) DO NOTHING`, tenant, critical.SchemaRefIntentInstance); err != nil {
		return fmt.Errorf("pgstore: ensure intent lifecycle schema: %w", err)
	}
	var expectedHead int64
	if err := tx.QueryRow(ctx, `SELECT head_sequence FROM stream_head WHERE tenant_id = $1 AND stream_key = $2`, tenant, streamKey).Scan(&expectedHead); err != nil {
		return fmt.Errorf("pgstore: read intent stream head before lifecycle append: %w", err)
	}
	key := fmt.Sprintf("intent-lifecycle:%s:%d", intentID, nextVersion)
	registry, err := ledgerport.NewLedgerEventDigestRegistry()
	if err != nil {
		return fmt.Errorf("pgstore: build intent lifecycle digester: %w", err)
	}
	appender := ledgerport.NewAppenderWithClock(registry, func() time.Time { return recordedAt.UTC() })
	receipt, err := appender.Append(ctx, tx, datalogger.AppendRequest{
		Tenant: tenant, StreamKey: streamKey, ExpectedHead: expectedHead,
		AssertionClass: datalogger.TransactionFact, SourceRef: "hcmnext:intent-lifecycle",
		SchemaRef: critical.SchemaRefIntentInstance, Payload: payload,
		OccurredAt: recordedAt.UTC(), EffectiveAt: recordedAt.UTC(),
		CorrelationID: uuid.NewSHA1(uuid.NameSpaceOID, []byte(key)), IdempotencyKey: key,
	})
	if err != nil {
		return fmt.Errorf("pgstore: append intent lifecycle snapshot: %w", err)
	}
	if _, err := critical.Apply(ctx, tx, critical.ProtoMapper{}, critical.ApplyRequest{
		Tenant: tenant, StreamKey: streamKey, Sequence: receipt.Sequence,
		Digest: receipt.Digest, SchemaRef: critical.SchemaRefIntentInstance, Payload: payload,
	}); err != nil {
		return fmt.Errorf("pgstore: project intent lifecycle snapshot: %w", err)
	}
	return nil
}
