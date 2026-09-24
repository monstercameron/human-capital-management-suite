package critical

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projection/wire"
)

// SchemaRefFullProposalSnapshot names the complete proposal JSON stored in
// proposal_revision.payload when a revision event carries the expanded
// snapshot alongside its canonical material payload.
const SchemaRefFullProposalSnapshot = wire.FullProposalSnapshotSchemaRef

// Schema references this package's default [Mapper] recognizes. They follow
// the "<message full name>@<schema version>" convention used by the ledger.
const (
	SchemaRefIntentInstance   = "hcmnext.intents.v1.IntentInstance@1"
	SchemaRefProposalRevision = "hcmnext.intents.v1.ProposalRevision@1"
)

// Mapper decodes a ledger event's typed payload into the read-model row it
// projects to. It is a port so callers can register additional schema versions
// without changing the projection transaction.
type Mapper interface {
	MapIntentInstance(schemaRef string, payload []byte) (row IntentInstanceRow, ok bool, err error)
	MapProposalRevision(schemaRef string, payload []byte) (row ProposalRevisionRow, ok bool, err error)
}

// ProtoMapper is the critical-projection adapter for the two declared
// generated intent payloads. The Protobuf runtime lives in the internal
// projection wire adapter; this
// storage package owns only row validation and transactional projection.
type ProtoMapper struct{}

var _ Mapper = ProtoMapper{}

func (ProtoMapper) MapIntentInstance(schemaRef string, payload []byte) (IntentInstanceRow, bool, error) {
	if schemaRef != SchemaRefIntentInstance {
		return IntentInstanceRow{}, false, nil
	}
	decoded, err := wire.DecodeIntentInstanceProjection(payload)
	if err != nil {
		return IntentInstanceRow{}, true, fmt.Errorf("critical: decode %s: %w", schemaRef, err)
	}
	intentID, err := uuid.Parse(decoded.IntentID)
	if err != nil {
		return IntentInstanceRow{}, true, fmt.Errorf("critical: %s: intent_id %q: %w", schemaRef, decoded.IntentID, err)
	}
	return IntentInstanceRow{
		IntentID: intentID, DefinitionRef: decoded.DefinitionRef, DefinitionVersion: decoded.DefinitionVersion,
		RequestDigest: decoded.RequestDigest, RequestDigestAlgorithm: decoded.RequestDigestAlgorithm,
		IdempotencyKey: decoded.IdempotencyKey, RequestState: decoded.RequestState,
		ExecutionState: decoded.ExecutionState, BusinessState: decoded.BusinessState,
		ConsistencyState: decoded.ConsistencyState, ObligationState: decoded.ObligationState,
		InstanceVersion: decoded.InstanceVersion, CreatedAt: decoded.CreatedAt,
		RecordedAt: decoded.RecordedAt, LastTransitionAt: decoded.LastTransitionAt,
	}, true, nil
}

func (ProtoMapper) MapProposalRevision(schemaRef string, payload []byte) (ProposalRevisionRow, bool, error) {
	if schemaRef != SchemaRefProposalRevision {
		return ProposalRevisionRow{}, false, nil
	}
	decoded, err := wire.DecodeProposalRevisionProjection(payload)
	if err != nil {
		return ProposalRevisionRow{}, true, fmt.Errorf("critical: decode %s: %w", schemaRef, err)
	}
	intentID, err := uuid.Parse(decoded.IntentID)
	if err != nil {
		return ProposalRevisionRow{}, true, fmt.Errorf("critical: %s: intent_id %q: %w", schemaRef, decoded.IntentID, err)
	}
	return ProposalRevisionRow{
		IntentID: intentID, Revision: decoded.Revision, ProposalDigest: decoded.ProposalDigest,
		MaterialDigest: decoded.MaterialDigest, DigestAlgorithm: decoded.DigestAlgorithm,
		SchemaRef: decoded.SchemaRef, Payload: decoded.Payload, ProducedBy: decoded.ProducedBy,
		ProducedAt: decoded.ProducedAt,
	}, true, nil
}
