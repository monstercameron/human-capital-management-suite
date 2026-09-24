// Package wire contains authored adapters around generated Protobuf messages.
//
// The adapters are deliberately small and one-way: they turn a declared wire
// payload into primitive projection data. Storage and domain packages consume
// that owned data shape and never import protobuf runtime mechanics directly.
package wire

import (
	"fmt"
	"time"

	"google.golang.org/protobuf/proto"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
)

// FullProposalSnapshotSchemaRef names the durable JSON envelope encoded by
// internal/data/intentcontrol.EncodeFullProposal. The enclosing ledger event
// is still the generated ProposalRevision message; this is the schema of the
// complete snapshot copied into proposal_revision.payload.
const FullProposalSnapshotSchemaRef = "hcmnext.intentcontrol.FullProposal@1"

// IntentInstanceProjection is the storage-neutral result of decoding the
// hcmnext.intents.v1.IntentInstance ledger payload.
type IntentInstanceProjection struct {
	IntentID               string
	DefinitionRef          string
	DefinitionVersion      int64
	RequestDigest          string
	RequestDigestAlgorithm string
	IdempotencyKey         string
	RequestState           string
	ExecutionState         string
	BusinessState          string
	ConsistencyState       string
	ObligationState        string
	InstanceVersion        int64
	CreatedAt              time.Time
	RecordedAt             time.Time
	LastTransitionAt       time.Time
}

// ProposalRevisionProjection is the storage-neutral result of decoding the
// hcmnext.intents.v1.ProposalRevision ledger payload.
type ProposalRevisionProjection struct {
	IntentID        string
	Revision        int64
	ProposalDigest  string
	MaterialDigest  string
	DigestAlgorithm string
	SchemaRef       string
	Payload         []byte
	ProducedBy      string
	ProducedAt      time.Time
}

// DecodeIntentInstanceProjection decodes the declared generated message and
// validates only the structural facts the critical read model persists.
func DecodeIntentInstanceProjection(payload []byte) (IntentInstanceProjection, error) {
	var msg intentsv1.IntentInstance
	if err := proto.Unmarshal(payload, &msg); err != nil {
		return IntentInstanceProjection{}, fmt.Errorf("decode IntentInstance: %w", err)
	}
	definition := msg.GetDefinition()
	if definition == nil || definition.GetIntentTypeId() == "" {
		return IntentInstanceProjection{}, fmt.Errorf("definition reference is required")
	}
	digestRef := msg.GetCanonicalRequestDigest()
	if digestRef == nil || digestRef.GetDigest() == "" || digestRef.GetAlgorithmId() == "" {
		return IntentInstanceProjection{}, fmt.Errorf("canonical request digest is required")
	}
	if msg.GetIdempotencyKey() == "" {
		return IntentInstanceProjection{}, fmt.Errorf("idempotency key is required")
	}
	lifecycle := msg.GetLifecycle()
	if lifecycle == nil {
		return IntentInstanceProjection{}, fmt.Errorf("lifecycle dimensions are required")
	}
	requestState, err := requestStateText(lifecycle.GetRequest())
	if err != nil {
		return IntentInstanceProjection{}, err
	}
	executionState, err := executionStateText(lifecycle.GetExecution())
	if err != nil {
		return IntentInstanceProjection{}, err
	}
	businessState, err := businessStateText(lifecycle.GetBusiness())
	if err != nil {
		return IntentInstanceProjection{}, err
	}
	consistencyState, err := consistencyStateText(lifecycle.GetConsistency())
	if err != nil {
		return IntentInstanceProjection{}, err
	}
	obligationState, err := obligationStateText(lifecycle.GetObligation())
	if err != nil {
		return IntentInstanceProjection{}, err
	}
	if msg.GetInstanceVersion() == 0 {
		return IntentInstanceProjection{}, fmt.Errorf("instance_version must be at least 1")
	}
	createdAt, recordedAt, lastTransitionAt := msg.GetCreatedAt(), msg.GetRecordedAt(), msg.GetLastTransitionAt()
	if createdAt == nil || recordedAt == nil || lastTransitionAt == nil {
		return IntentInstanceProjection{}, fmt.Errorf("created_at, recorded_at and last_transition_at are required")
	}
	return IntentInstanceProjection{
		IntentID: msg.GetIntentId(), DefinitionRef: definition.GetIntentTypeId(),
		DefinitionVersion: int64(definition.GetVersion()), RequestDigest: digestRef.GetDigest(),
		RequestDigestAlgorithm: digestRef.GetAlgorithmId(), IdempotencyKey: msg.GetIdempotencyKey(),
		RequestState: requestState, ExecutionState: executionState, BusinessState: businessState,
		ConsistencyState: consistencyState, ObligationState: obligationState,
		InstanceVersion: int64(msg.GetInstanceVersion()), CreatedAt: createdAt.AsTime(),
		RecordedAt: recordedAt.AsTime(), LastTransitionAt: lastTransitionAt.AsTime(),
	}, nil
}

// DecodeProposalRevisionProjection decodes the declared generated message and
// validates only the structural facts the critical read model persists.
func DecodeProposalRevisionProjection(payload []byte) (ProposalRevisionProjection, error) {
	var msg intentsv1.ProposalRevision
	if err := proto.Unmarshal(payload, &msg); err != nil {
		return ProposalRevisionProjection{}, fmt.Errorf("decode ProposalRevision: %w", err)
	}
	if msg.GetRevision() == 0 {
		return ProposalRevisionProjection{}, fmt.Errorf("revision must be at least 1")
	}
	material := msg.GetMaterialProposalDigest()
	if material == nil || material.GetDigest() == "" || material.GetAlgorithmId() == "" {
		return ProposalRevisionProjection{}, fmt.Errorf("material proposal digest is required")
	}
	proposal := msg.GetProposal()
	if proposal == nil || len(proposal.GetProtobufWireBytes()) == 0 {
		return ProposalRevisionProjection{}, fmt.Errorf("inline proposal payload is required")
	}
	schema := proposal.GetSchema()
	if schema == nil || schema.GetSchemaId() == "" {
		return ProposalRevisionProjection{}, fmt.Errorf("proposal payload schema is required")
	}
	proposalDigestRef := proposal.GetCanonicalDigest()
	if proposalDigestRef == nil || proposalDigestRef.GetDigest() == "" {
		return ProposalRevisionProjection{}, fmt.Errorf("proposal payload canonical digest is required")
	}
	createdBy := msg.GetCreatedBy()
	if createdBy == nil || createdBy.GetPrincipalId() == "" {
		return ProposalRevisionProjection{}, fmt.Errorf("created_by principal is required")
	}
	createdAt := msg.GetCreatedAt()
	if createdAt == nil {
		return ProposalRevisionProjection{}, fmt.Errorf("created_at is required")
	}
	rowSchemaRef := fmt.Sprintf("%s@%d", schema.GetSchemaId(), schema.GetVersion())
	rowPayload := proposal.GetProtobufWireBytes()
	if full := msg.GetFullProposalPayload(); len(full) > 0 {
		rowSchemaRef = FullProposalSnapshotSchemaRef
		rowPayload = full
	}
	return ProposalRevisionProjection{
		IntentID: msg.GetIntentId(), Revision: int64(msg.GetRevision()),
		ProposalDigest: proposalDigestRef.GetDigest(), MaterialDigest: material.GetDigest(),
		DigestAlgorithm: material.GetAlgorithmId(), SchemaRef: rowSchemaRef,
		Payload:    rowPayload,
		ProducedBy: createdBy.GetPrincipalId(), ProducedAt: createdAt.AsTime(),
	}, nil
}

func requestStateText(s intentsv1.RequestState) (string, error) {
	switch s {
	case intentsv1.RequestState_REQUEST_STATE_DRAFT:
		return "DRAFT", nil
	case intentsv1.RequestState_REQUEST_STATE_PREFLIGHTED:
		return "PREFLIGHTED", nil
	case intentsv1.RequestState_REQUEST_STATE_SIMULATED:
		return "SIMULATED", nil
	case intentsv1.RequestState_REQUEST_STATE_SUBMITTED:
		return "SUBMITTED", nil
	case intentsv1.RequestState_REQUEST_STATE_APPROVED:
		return "APPROVED", nil
	case intentsv1.RequestState_REQUEST_STATE_REJECTED:
		return "REJECTED", nil
	case intentsv1.RequestState_REQUEST_STATE_WITHDRAWN:
		return "WITHDRAWN", nil
	case intentsv1.RequestState_REQUEST_STATE_CANCELLED:
		return "CANCELLED", nil
	case intentsv1.RequestState_REQUEST_STATE_SUPERSEDED:
		return "SUPERSEDED", nil
	case intentsv1.RequestState_REQUEST_STATE_CLOSED:
		return "CLOSED", nil
	case intentsv1.RequestState_REQUEST_STATE_REOPENED:
		return "REOPENED", nil
	default:
		return "", fmt.Errorf("request state %v has no intent_instance.request_state mapping", s)
	}
}

func executionStateText(s intentsv1.ExecutionState) (string, error) {
	switch s {
	case intentsv1.ExecutionState_EXECUTION_STATE_NOT_PLANNED:
		return "NOT_PLANNED", nil
	case intentsv1.ExecutionState_EXECUTION_STATE_SCHEDULED:
		return "SCHEDULED", nil
	case intentsv1.ExecutionState_EXECUTION_STATE_REVALIDATING:
		return "REVALIDATING", nil
	case intentsv1.ExecutionState_EXECUTION_STATE_EXECUTING:
		return "EXECUTING", nil
	case intentsv1.ExecutionState_EXECUTION_STATE_COMMITTED:
		return "COMMITTED", nil
	case intentsv1.ExecutionState_EXECUTION_STATE_BLOCKED:
		return "BLOCKED", nil
	case intentsv1.ExecutionState_EXECUTION_STATE_REPAIR_REQUIRED:
		return "REPAIR_REQUIRED", nil
	default:
		return "", fmt.Errorf("execution state %v has no intent_instance.execution_state mapping", s)
	}
}

func businessStateText(s intentsv1.BusinessState) (string, error) {
	switch s {
	case intentsv1.BusinessState_BUSINESS_STATE_NOT_STARTED:
		return "NOT_STARTED", nil
	case intentsv1.BusinessState_BUSINESS_STATE_IN_PROGRESS:
		return "IN_PROGRESS", nil
	case intentsv1.BusinessState_BUSINESS_STATE_COMPLETED:
		return "COMPLETED", nil
	case intentsv1.BusinessState_BUSINESS_STATE_NOT_ACHIEVED:
		return "NOT_ACHIEVED", nil
	case intentsv1.BusinessState_BUSINESS_STATE_CORRECTED:
		return "CORRECTED", nil
	case intentsv1.BusinessState_BUSINESS_STATE_UNKNOWN:
		return "UNKNOWN", nil
	default:
		return "", fmt.Errorf("business state %v has no intent_instance.business_state mapping", s)
	}
}

func consistencyStateText(s intentsv1.ConsistencyState) (string, error) {
	switch s {
	case intentsv1.ConsistencyState_CONSISTENCY_STATE_NOT_APPLICABLE:
		return "NOT_APPLICABLE", nil
	case intentsv1.ConsistencyState_CONSISTENCY_STATE_PENDING_OBSERVATION:
		return "PENDING_OBSERVATION", nil
	case intentsv1.ConsistencyState_CONSISTENCY_STATE_CONSISTENT:
		return "CONSISTENT", nil
	case intentsv1.ConsistencyState_CONSISTENCY_STATE_DEGRADED:
		return "DEGRADED", nil
	case intentsv1.ConsistencyState_CONSISTENCY_STATE_REPAIRING:
		return "REPAIRING", nil
	case intentsv1.ConsistencyState_CONSISTENCY_STATE_UNKNOWN:
		return "UNKNOWN", nil
	default:
		return "", fmt.Errorf("consistency state %v has no intent_instance.consistency_state mapping", s)
	}
}

func obligationStateText(s intentsv1.ObligationState) (string, error) {
	switch s {
	case intentsv1.ObligationState_OBLIGATION_STATE_NOT_APPLICABLE:
		return "NOT_APPLICABLE", nil
	case intentsv1.ObligationState_OBLIGATION_STATE_PENDING:
		return "PENDING", nil
	case intentsv1.ObligationState_OBLIGATION_STATE_SATISFIED:
		return "SATISFIED", nil
	case intentsv1.ObligationState_OBLIGATION_STATE_OVERDUE:
		return "OVERDUE", nil
	case intentsv1.ObligationState_OBLIGATION_STATE_WAIVED:
		return "WAIVED", nil
	case intentsv1.ObligationState_OBLIGATION_STATE_UNKNOWN:
		return "UNKNOWN", nil
	default:
		return "", fmt.Errorf("obligation state %v has no intent_instance.obligation_state mapping", s)
	}
}
