package wire

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
)

func validIntentInstance() *intentsv1.IntentInstance {
	createdAt := time.Date(2026, time.September, 3, 9, 30, 0, 0, time.UTC)
	return &intentsv1.IntentInstance{
		IntentId:       "intent-1",
		TenantId:       "tenant-1",
		IdempotencyKey: "idem-1",
		Definition: &intentsv1.DefinitionReference{
			IntentTypeId: "employee_data.legal_name.change",
			Version:      3,
		},
		CanonicalRequestDigest: &intentsv1.CanonicalDigestReference{
			AlgorithmId: "sha256",
			Digest:      "abc123",
		},
		Lifecycle: &intentsv1.LifecycleDimensions{
			Request:     intentsv1.RequestState_REQUEST_STATE_SUBMITTED,
			Execution:   intentsv1.ExecutionState_EXECUTION_STATE_NOT_PLANNED,
			Business:    intentsv1.BusinessState_BUSINESS_STATE_NOT_STARTED,
			Consistency: intentsv1.ConsistencyState_CONSISTENCY_STATE_NOT_APPLICABLE,
			Obligation:  intentsv1.ObligationState_OBLIGATION_STATE_NOT_APPLICABLE,
		},
		InstanceVersion:  7,
		CreatedAt:        timestamppb.New(createdAt),
		RecordedAt:       timestamppb.New(createdAt.Add(time.Second)),
		LastTransitionAt: timestamppb.New(createdAt.Add(2 * time.Second)),
	}
}

// TestDecodeIntentInstanceProjection proves the happy path fills every
// projection field, including the enum-to-string state mappings and the
// timestamp conversions.
func TestDecodeIntentInstanceProjection(t *testing.T) {
	createdAt := time.Date(2026, time.September, 3, 9, 30, 0, 0, time.UTC)
	encoded, err := proto.Marshal(validIntentInstance())
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	got, err := DecodeIntentInstanceProjection(encoded)
	if err != nil {
		t.Fatalf("DecodeIntentInstanceProjection: %v", err)
	}
	want := IntentInstanceProjection{
		IntentID:               "intent-1",
		DefinitionRef:          "employee_data.legal_name.change",
		DefinitionVersion:      3,
		RequestDigest:          "abc123",
		RequestDigestAlgorithm: "sha256",
		IdempotencyKey:         "idem-1",
		RequestState:           "SUBMITTED",
		ExecutionState:         "NOT_PLANNED",
		BusinessState:          "NOT_STARTED",
		ConsistencyState:       "NOT_APPLICABLE",
		ObligationState:        "NOT_APPLICABLE",
		InstanceVersion:        7,
		CreatedAt:              createdAt,
		RecordedAt:             createdAt.Add(time.Second),
		LastTransitionAt:       createdAt.Add(2 * time.Second),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("projection mismatch:\n got %+v\nwant %+v", got, want)
	}
}

// TestDecodeIntentInstanceProjectionRejectsMalformed proves garbage bytes are
// a decode error, not a zero projection.
func TestDecodeIntentInstanceProjectionRejectsMalformed(t *testing.T) {
	if _, err := DecodeIntentInstanceProjection([]byte{0xff, 0xff, 0xff}); err == nil {
		t.Fatal("DecodeIntentInstanceProjection accepted malformed wire bytes")
	}
}

// TestDecodeIntentInstanceProjectionRequiredFields proves every structural
// requirement the read model depends on is enforced.
func TestDecodeIntentInstanceProjectionRequiredFields(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*intentsv1.IntentInstance)
		want   string
	}{
		{"missing definition", func(m *intentsv1.IntentInstance) { m.Definition = nil }, "definition reference is required"},
		{"definition without type id", func(m *intentsv1.IntentInstance) { m.Definition.IntentTypeId = "" }, "definition reference is required"},
		{"missing digest", func(m *intentsv1.IntentInstance) { m.CanonicalRequestDigest = nil }, "canonical request digest is required"},
		{"digest without value", func(m *intentsv1.IntentInstance) { m.CanonicalRequestDigest.Digest = "" }, "canonical request digest is required"},
		{"digest without algorithm", func(m *intentsv1.IntentInstance) { m.CanonicalRequestDigest.AlgorithmId = "" }, "canonical request digest is required"},
		{"missing idempotency key", func(m *intentsv1.IntentInstance) { m.IdempotencyKey = "" }, "idempotency key is required"},
		{"missing lifecycle", func(m *intentsv1.IntentInstance) { m.Lifecycle = nil }, "lifecycle dimensions are required"},
		{"zero instance version", func(m *intentsv1.IntentInstance) { m.InstanceVersion = 0 }, "instance_version must be at least 1"},
		{"missing created_at", func(m *intentsv1.IntentInstance) { m.CreatedAt = nil }, "created_at, recorded_at and last_transition_at are required"},
		{"missing recorded_at", func(m *intentsv1.IntentInstance) { m.RecordedAt = nil }, "created_at, recorded_at and last_transition_at are required"},
		{"missing last_transition_at", func(m *intentsv1.IntentInstance) { m.LastTransitionAt = nil }, "created_at, recorded_at and last_transition_at are required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := proto.Clone(validIntentInstance()).(*intentsv1.IntentInstance)
			tc.mutate(msg)
			encoded, err := proto.Marshal(msg)
			if err != nil {
				t.Fatalf("marshal fixture: %v", err)
			}
			_, err = DecodeIntentInstanceProjection(encoded)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

// TestDecodeIntentInstanceProjectionUnmappedStates proves every one of the
// five state mappers rejects an enum value with no declared mapping, so an
// upstream schema change cannot be persisted silently.
func TestDecodeIntentInstanceProjectionUnmappedStates(t *testing.T) {
	msg := proto.Clone(validIntentInstance()).(*intentsv1.IntentInstance)
	msg.Lifecycle.Request = intentsv1.RequestState(999)
	msg.Lifecycle.Execution = intentsv1.ExecutionState(999)
	msg.Lifecycle.Business = intentsv1.BusinessState(999)
	msg.Lifecycle.Consistency = intentsv1.ConsistencyState(999)
	msg.Lifecycle.Obligation = intentsv1.ObligationState(999)
	encoded, err := proto.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	_, err = DecodeIntentInstanceProjection(encoded)
	if err == nil || !strings.Contains(err.Error(), "request_state") {
		t.Errorf("error = %v, want the request_state mapping rejection", err)
	}
}

// TestStateMappersCoverDeclaredEnums pins each declared enum value to its
// storage text.
func TestStateMappersCoverDeclaredEnums(t *testing.T) {
	requests := map[intentsv1.RequestState]string{
		intentsv1.RequestState_REQUEST_STATE_DRAFT:       "DRAFT",
		intentsv1.RequestState_REQUEST_STATE_PREFLIGHTED: "PREFLIGHTED",
		intentsv1.RequestState_REQUEST_STATE_SIMULATED:   "SIMULATED",
		intentsv1.RequestState_REQUEST_STATE_SUBMITTED:   "SUBMITTED",
		intentsv1.RequestState_REQUEST_STATE_APPROVED:    "APPROVED",
		intentsv1.RequestState_REQUEST_STATE_REJECTED:    "REJECTED",
		intentsv1.RequestState_REQUEST_STATE_WITHDRAWN:   "WITHDRAWN",
		intentsv1.RequestState_REQUEST_STATE_CANCELLED:   "CANCELLED",
		intentsv1.RequestState_REQUEST_STATE_SUPERSEDED:  "SUPERSEDED",
		intentsv1.RequestState_REQUEST_STATE_CLOSED:      "CLOSED",
		intentsv1.RequestState_REQUEST_STATE_REOPENED:    "REOPENED",
	}
	for got, want := range requests {
		if text, err := requestStateText(got); err != nil || text != want {
			t.Errorf("requestStateText(%v) = %q, %v; want %q", got, text, err, want)
		}
	}
	executions := map[intentsv1.ExecutionState]string{
		intentsv1.ExecutionState_EXECUTION_STATE_NOT_PLANNED:     "NOT_PLANNED",
		intentsv1.ExecutionState_EXECUTION_STATE_SCHEDULED:       "SCHEDULED",
		intentsv1.ExecutionState_EXECUTION_STATE_REVALIDATING:    "REVALIDATING",
		intentsv1.ExecutionState_EXECUTION_STATE_EXECUTING:       "EXECUTING",
		intentsv1.ExecutionState_EXECUTION_STATE_COMMITTED:       "COMMITTED",
		intentsv1.ExecutionState_EXECUTION_STATE_BLOCKED:         "BLOCKED",
		intentsv1.ExecutionState_EXECUTION_STATE_REPAIR_REQUIRED: "REPAIR_REQUIRED",
	}
	for got, want := range executions {
		if text, err := executionStateText(got); err != nil || text != want {
			t.Errorf("executionStateText(%v) = %q, %v; want %q", got, text, err, want)
		}
	}
	businesses := map[intentsv1.BusinessState]string{
		intentsv1.BusinessState_BUSINESS_STATE_NOT_STARTED:  "NOT_STARTED",
		intentsv1.BusinessState_BUSINESS_STATE_IN_PROGRESS:  "IN_PROGRESS",
		intentsv1.BusinessState_BUSINESS_STATE_COMPLETED:    "COMPLETED",
		intentsv1.BusinessState_BUSINESS_STATE_NOT_ACHIEVED: "NOT_ACHIEVED",
		intentsv1.BusinessState_BUSINESS_STATE_CORRECTED:    "CORRECTED",
		intentsv1.BusinessState_BUSINESS_STATE_UNKNOWN:      "UNKNOWN",
	}
	for got, want := range businesses {
		if text, err := businessStateText(got); err != nil || text != want {
			t.Errorf("businessStateText(%v) = %q, %v; want %q", got, text, err, want)
		}
	}
	consistencies := map[intentsv1.ConsistencyState]string{
		intentsv1.ConsistencyState_CONSISTENCY_STATE_NOT_APPLICABLE:      "NOT_APPLICABLE",
		intentsv1.ConsistencyState_CONSISTENCY_STATE_PENDING_OBSERVATION: "PENDING_OBSERVATION",
		intentsv1.ConsistencyState_CONSISTENCY_STATE_CONSISTENT:          "CONSISTENT",
		intentsv1.ConsistencyState_CONSISTENCY_STATE_DEGRADED:            "DEGRADED",
		intentsv1.ConsistencyState_CONSISTENCY_STATE_REPAIRING:           "REPAIRING",
		intentsv1.ConsistencyState_CONSISTENCY_STATE_UNKNOWN:             "UNKNOWN",
	}
	for got, want := range consistencies {
		if text, err := consistencyStateText(got); err != nil || text != want {
			t.Errorf("consistencyStateText(%v) = %q, %v; want %q", got, text, err, want)
		}
	}
	obligations := map[intentsv1.ObligationState]string{
		intentsv1.ObligationState_OBLIGATION_STATE_NOT_APPLICABLE: "NOT_APPLICABLE",
		intentsv1.ObligationState_OBLIGATION_STATE_PENDING:        "PENDING",
		intentsv1.ObligationState_OBLIGATION_STATE_SATISFIED:      "SATISFIED",
		intentsv1.ObligationState_OBLIGATION_STATE_OVERDUE:        "OVERDUE",
		intentsv1.ObligationState_OBLIGATION_STATE_WAIVED:         "WAIVED",
		intentsv1.ObligationState_OBLIGATION_STATE_UNKNOWN:        "UNKNOWN",
	}
	for got, want := range obligations {
		if text, err := obligationStateText(got); err != nil || text != want {
			t.Errorf("obligationStateText(%v) = %q, %v; want %q", got, text, err, want)
		}
	}
}

func validProposalRevision() *intentsv1.ProposalRevision {
	return &intentsv1.ProposalRevision{
		ProposalRevisionId: "proposal-1",
		IntentId:           "intent-1",
		Revision:           2,
		MaterialProposalDigest: &intentsv1.CanonicalDigestReference{
			AlgorithmId: "sha256",
			Digest:      "material-abc",
		},
		Proposal: &intentsv1.TypedPayload{
			Schema:            &intentsv1.SchemaReference{SchemaId: "s1", Version: 2},
			ProtobufWireBytes: []byte("inline-payload"),
			CanonicalDigest:   &intentsv1.CanonicalDigestReference{Digest: "proposal-xyz"},
		},
		CreatedBy: &intentsv1.PrincipalReference{PrincipalId: "principal-1"},
		CreatedAt: timestamppb.New(time.Date(2026, time.September, 3, 10, 0, 0, 0, time.UTC)),
	}
}

// TestDecodeProposalRevisionProjection proves the revision happy path fills
// every projection field, including the schema@version join and the byte-exact
// inline payload.
func TestDecodeProposalRevisionProjection(t *testing.T) {
	encoded, err := proto.Marshal(validProposalRevision())
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	got, err := DecodeProposalRevisionProjection(encoded)
	if err != nil {
		t.Fatalf("DecodeProposalRevisionProjection: %v", err)
	}
	want := ProposalRevisionProjection{
		IntentID:        "intent-1",
		Revision:        2,
		ProposalDigest:  "proposal-xyz",
		MaterialDigest:  "material-abc",
		DigestAlgorithm: "sha256",
		SchemaRef:       "s1@2",
		Payload:         []byte("inline-payload"),
		ProducedBy:      "principal-1",
		ProducedAt:      time.Date(2026, time.September, 3, 10, 0, 0, 0, time.UTC),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("projection mismatch:\n got %+v\nwant %+v", got, want)
	}
}

func TestDecodeProposalRevisionProjectionPreservesFullProposalSnapshot(t *testing.T) {
	msg := validProposalRevision()
	full := []byte(`{"schema_version":1,"proposal_revision_id":"proposal-1"}`)
	msg.FullProposalPayload = full
	encoded, err := proto.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal ProposalRevision with full snapshot: %v", err)
	}
	got, err := DecodeProposalRevisionProjection(encoded)
	if err != nil {
		t.Fatalf("DecodeProposalRevisionProjection: %v", err)
	}
	if got.SchemaRef != FullProposalSnapshotSchemaRef {
		t.Fatalf("schema_ref = %q, want %q", got.SchemaRef, FullProposalSnapshotSchemaRef)
	}
	if !bytes.Equal(got.Payload, full) {
		t.Fatalf("payload = %q, want exact full snapshot %q", got.Payload, full)
	}
}

// TestDecodeProposalRevisionProjectionRejectsMalformed proves garbage bytes are
// a decode error.
func TestDecodeProposalRevisionProjectionRejectsMalformed(t *testing.T) {
	if _, err := DecodeProposalRevisionProjection([]byte{0x00, 0x00, 0x00}); err == nil {
		t.Fatal("DecodeProposalRevisionProjection accepted malformed wire bytes")
	}
}

// TestDecodeProposalRevisionProjectionRequiredFields proves every structural
// requirement is enforced.
func TestDecodeProposalRevisionProjectionRequiredFields(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*intentsv1.ProposalRevision)
		want   string
	}{
		{"zero revision", func(m *intentsv1.ProposalRevision) { m.Revision = 0 }, "revision must be at least 1"},
		{"missing material digest", func(m *intentsv1.ProposalRevision) { m.MaterialProposalDigest = nil }, "material proposal digest is required"},
		{"material digest without value", func(m *intentsv1.ProposalRevision) { m.MaterialProposalDigest.Digest = "" }, "material proposal digest is required"},
		{"material digest without algorithm", func(m *intentsv1.ProposalRevision) { m.MaterialProposalDigest.AlgorithmId = "" }, "material proposal digest is required"},
		{"missing proposal", func(m *intentsv1.ProposalRevision) { m.Proposal = nil }, "inline proposal payload is required"},
		{"proposal without bytes", func(m *intentsv1.ProposalRevision) { m.Proposal.ProtobufWireBytes = nil }, "inline proposal payload is required"},
		{"proposal without schema", func(m *intentsv1.ProposalRevision) { m.Proposal.Schema = nil }, "proposal payload schema is required"},
		{"schema without id", func(m *intentsv1.ProposalRevision) { m.Proposal.Schema.SchemaId = "" }, "proposal payload schema is required"},
		{"missing canonical digest", func(m *intentsv1.ProposalRevision) { m.Proposal.CanonicalDigest = nil }, "proposal payload canonical digest is required"},
		{"canonical digest without value", func(m *intentsv1.ProposalRevision) { m.Proposal.CanonicalDigest.Digest = "" }, "proposal payload canonical digest is required"},
		{"missing created by", func(m *intentsv1.ProposalRevision) { m.CreatedBy = nil }, "created_by principal is required"},
		{"created by without principal", func(m *intentsv1.ProposalRevision) { m.CreatedBy.PrincipalId = "" }, "created_by principal is required"},
		{"missing created at", func(m *intentsv1.ProposalRevision) { m.CreatedAt = nil }, "created_at is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := proto.Clone(validProposalRevision()).(*intentsv1.ProposalRevision)
			tc.mutate(msg)
			encoded, err := proto.Marshal(msg)
			if err != nil {
				t.Fatalf("marshal fixture: %v", err)
			}
			_, err = DecodeProposalRevisionProjection(encoded)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}
