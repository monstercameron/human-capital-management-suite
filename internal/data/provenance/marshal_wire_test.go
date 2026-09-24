package provenance

import (
	"testing"
	"time"

	"github.com/google/uuid"
	provenancev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/provenance/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestMarshalOutboxPayloadUsesTypedRecord(t *testing.T) {
	publishedAt := time.Date(2026, time.September, 24, 12, 30, 0, 0, time.UTC)
	eventID := uuid.MustParse("b75d8f63-1160-4e40-a768-9db6dfe94529")
	record := Record{
		RecordID:        uuid.MustParse("dd50f855-c5bf-456c-8ef1-ff18d98258db"),
		IntentRef:       "intent:1",
		SourceKind:      SourceLedgerEvent,
		SourceRef:       "ledger:1",
		StreamKey:       "intent:1",
		Sequence:        7,
		EventID:         eventID,
		SourceAuthority: "authority:1",
		PrincipalRef:    "principal:1",
		EvidenceIDs:     []string{"evidence:1", "evidence:2"},
		Digests:         []Digest{{Kind: "LEDGER_EVENT", Algorithm: "sha256", Digest: "abc123"}},
		PublishedAt:     publishedAt,
	}
	encoded, err := marshalOutboxPayload(record)
	if err != nil {
		t.Fatalf("marshalOutboxPayload: %v", err)
	}
	got := &provenancev1.Record{}
	if err := proto.Unmarshal(encoded, got); err != nil {
		t.Fatalf("unmarshal typed record: %v", err)
	}
	if name := string(proto.MessageName(got)); name != "hcmnext.provenance.v1.Record" {
		t.Fatalf("message name = %q, want hcmnext.provenance.v1.Record", name)
	}
	want := &provenancev1.Record{
		RecordId:        record.RecordID.String(),
		IntentRef:       record.IntentRef,
		SourceKind:      string(record.SourceKind),
		SourceRef:       record.SourceRef,
		StreamKey:       record.StreamKey,
		Sequence:        record.Sequence,
		EventId:         eventID.String(),
		SourceAuthority: record.SourceAuthority,
		PrincipalRef:    record.PrincipalRef,
		EvidenceIds:     record.EvidenceIDs,
		Digests:         []*provenancev1.Digest{{Kind: "LEDGER_EVENT", Algorithm: "sha256", Digest: "abc123"}},
		PublishedAt:     timestamppb.New(publishedAt),
	}
	if !proto.Equal(got, want) {
		t.Fatalf("decoded record = %v, want %v", got, want)
	}
}
