package app

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/google/uuid"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"google.golang.org/protobuf/proto"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/intentcontrol"
	datalogger "github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projection/critical"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/protomap"
	ledgerport "github.com/monstercameron/human-capital-management-suite/internal/ledger"
)

const proposalRevisionStreamKind = "TRANSACTION"

// recordProposalRevision appends the typed proposal event and materializes its
// projection in the caller's transaction. The canonical material bytes stay
// in ProposalRevision.proposal; the versioned full-proposal snapshot is carried
// in the same event so execution can reconstruct fields outside that digest.
func recordProposalRevision(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, proposal intent.ProposalRevision, digester intent.Digester) error {
	if tx == nil {
		return fmt.Errorf("app: proposal revision ledger transaction is required")
	}
	if tenantID == uuid.Nil {
		return fmt.Errorf("app: proposal revision ledger tenant is required")
	}
	if digester == nil {
		return fmt.Errorf("app: proposal revision ledger digester is required")
	}
	fullPayload, err := intentcontrol.EncodeFullProposal(proposal)
	if err != nil {
		return fmt.Errorf("app: encode the complete proposal revision payload: %w", err)
	}
	event, err := protomap.ProposalToProto(proposal)
	if err != nil {
		return fmt.Errorf("app: encode the proposal revision event: %w", err)
	}
	event.Proposal.CanonicalDigest = proposal.MaterialDigest.ToProto()
	event.FullProposalPayload = fullPayload
	eventPayload, err := protomap.MarshalDeterministic(event)
	if err != nil {
		return fmt.Errorf("app: marshal the proposal revision event: %w", err)
	}

	streamKey := "intent:" + proposal.IntentID
	idempotencyKey := fmt.Sprintf("proposal-revision:%s:%d", proposal.IntentID, proposal.Revision)
	// Re-simulation can mint new provenance (proposal ID, created_at and
	// refreshed control snapshots) while preserving the same material revision.
	// Reuse the original ledger bytes only after decoding and verifying that
	// their complete proposal still names this identity and exact material.
	// The appender below still performs its ordinary digest comparison, so a
	// different material payload remains an idempotency conflict.
	events, err := datalogger.NewReader().ReadStream(ctx, tx, tenantID, streamKey)
	if err != nil {
		return fmt.Errorf("app: read proposal revision ledger stream: %w", err)
	}
	for _, recorded := range events {
		if recorded.IdempotencyKey != idempotencyKey {
			continue
		}
		if recorded.SchemaRef != critical.SchemaRefProposalRevision || recorded.PayloadState != datalogger.PayloadPresent {
			return fmt.Errorf("app: existing proposal revision ledger event is unavailable or has an unexpected schema")
		}
		var message intentsv1.ProposalRevision
		if err := proto.Unmarshal(recorded.Payload, &message); err != nil {
			return fmt.Errorf("app: decode existing proposal revision ledger event: %w", err)
		}
		if message.GetProposal() == nil {
			return fmt.Errorf("app: existing proposal revision ledger event has no material payload")
		}
		stored, err := intentcontrol.DecodeFullProposal(message.GetFullProposalPayload(), fullProposalVerifier{digester})
		if err != nil {
			return fmt.Errorf("app: verify existing proposal revision ledger snapshot: %w", err)
		}
		outer, outerErr := protomap.ProposalFromProto(&message)
		if outerErr != nil {
			return fmt.Errorf("app: decode existing proposal revision identity: %w", outerErr)
		}
		outerPayload, payloadErr := protomap.PayloadFromProto(message.GetProposal())
		if payloadErr != nil {
			return fmt.Errorf("app: decode existing proposal revision material payload: %w", payloadErr)
		}
		if !proto.Equal(message.GetProposal().GetCanonicalDigest(), stored.MaterialDigest.ToProto()) ||
			!reflect.DeepEqual(outerPayload, stored.MaterialPayload()) {
			return fmt.Errorf("app: existing proposal revision ledger payload disagrees with its full snapshot")
		}
		if message.GetIntentId() != proposal.IntentID || message.GetRevision() != proposal.Revision ||
			stored.IntentID != proposal.IntentID || stored.Revision != proposal.Revision ||
			stored.ProposalRevisionID != outer.ProposalRevisionID ||
			stored.ProposalRevisionID != proposal.ProposalRevisionID ||
			!reflect.DeepEqual(stored.MaterialDigest, outer.MaterialDigest) ||
			!reflect.DeepEqual(stored.MaterialDigest, proposal.MaterialDigest) ||
			!bytes.Equal(stored.MaterialPayload().WireBytes, proposal.MaterialPayload().WireBytes) {
			// Keep the caller's new payload. Append will return the ledger's
			// typed idempotency conflict instead of treating changed material
			// as a replay.
			break
		}
		// These bytes are the event the ledger originally accepted. Passing
		// them through Append preserves its strict replay digest check and
		// lets critical.Apply repair a lagging projection from ledger truth.
		eventPayload = recorded.Payload
		break
	}
	if err := datalogger.EnsureStream(ctx, tx, tenantID, streamKey, proposalRevisionStreamKind, proposal.IntentID); err != nil {
		return fmt.Errorf("app: register proposal revision ledger stream: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO payload_schema (
			tenant_id, schema_ref, schema_id, schema_version,
			message_full_name, wire_format, canonicalization_profile)
		VALUES ($1, $2, 'hcmnext.intents.v1.ProposalRevision', 1,
			'hcmnext.intents.v1.ProposalRevision', 'PROTOBUF', 'LEDGER_EVENT')
		ON CONFLICT (tenant_id, schema_ref) DO NOTHING`, tenantID, critical.SchemaRefProposalRevision); err != nil {
		return fmt.Errorf("app: register proposal revision ledger schema: %w", err)
	}

	var expectedHead int64
	if err := tx.QueryRow(ctx, `
		SELECT head_sequence FROM stream_head
		WHERE tenant_id = $1 AND stream_key = $2`, tenantID, streamKey).Scan(&expectedHead); err != nil {
		return fmt.Errorf("app: read proposal revision stream head: %w", err)
	}
	registry, err := ledgerport.NewLedgerEventDigestRegistry()
	if err != nil {
		return fmt.Errorf("app: build proposal revision ledger digester: %w", err)
	}
	appender := ledgerport.NewAppenderWithClock(registry, func() time.Time { return proposal.CreatedAt.Time() })
	correlationID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(idempotencyKey))
	appended, err := appender.Append(ctx, tx, datalogger.AppendRequest{
		Tenant: tenantID, StreamKey: streamKey, ExpectedHead: expectedHead,
		AssertionClass: datalogger.TransactionFact, SourceRef: "hcmnext:intent-cell",
		SchemaRef: critical.SchemaRefProposalRevision, Payload: eventPayload,
		OccurredAt: proposal.CreatedAt.Time(), EffectiveAt: proposal.CreatedAt.Time(),
		CorrelationID: correlationID, IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		return fmt.Errorf("app: append proposal revision to ledger: %w", err)
	}
	if _, err := critical.Apply(ctx, tx, critical.ProtoMapper{}, critical.ApplyRequest{
		Tenant: tenantID, StreamKey: streamKey, Sequence: appended.Sequence,
		Digest: appended.Digest, SchemaRef: critical.SchemaRefProposalRevision, Payload: eventPayload,
	}); err != nil {
		return fmt.Errorf("app: project proposal revision from ledger: %w", err)
	}
	return nil
}
