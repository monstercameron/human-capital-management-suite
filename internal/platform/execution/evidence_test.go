package execution

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
)

// TestTodo_OBS_024_Golden proves capabilityEvidenceAdapter's packing is
// exact and byte-stable: [execute.ExecutionEvidence]'s five arguments
// (kind, instanceID, nodeID, refID, digest) decode back unchanged from the
// [app.EvidenceRecord] the underlying [app.MemoryEvidenceSink] stores, using
// exactly the "<instanceID>|<nodeID>" / "<refID>|<digest>" convention this
// package's own evidence.go documents. A reader (an inspector, a test) that
// does not know this convention can still recover CapabilityID and
// Decision unchanged.
func TestTodo_OBS_024_Golden(t *testing.T) {
	sink := app.NewMemoryEvidenceSink()
	at := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	tenantID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	adapter := NewCapabilityEvidenceAdapter(sink, func() time.Time { return at })

	evidenceID, err := adapter.RecordExecutionEvidence(context.Background(), tenantID,
		execute.EvidenceKindTerminalWritten, "instance-1", "end", "event:instance-1@1", "sha256:deadbeef", at)
	if err != nil {
		t.Fatalf("RecordExecutionEvidence: %v", err)
	}
	if evidenceID == "" {
		t.Fatal("RecordExecutionEvidence returned an empty evidence id")
	}

	records := sink.Records()
	if len(records) != 1 {
		t.Fatalf("sink.Records() = %d, want exactly 1", len(records))
	}
	rec := records[0]

	if rec.EvidenceID != evidenceID {
		t.Errorf("recorded EvidenceID = %q, want the returned id %q", rec.EvidenceID, evidenceID)
	}
	if rec.TenantID != tenantID {
		t.Errorf("TenantID = %s, want the storage tenant the run committed under %s", rec.TenantID, tenantID)
	}
	if rec.CapabilityID != "workflow.execution.evidence" {
		t.Errorf("CapabilityID = %q, want %q", rec.CapabilityID, "workflow.execution.evidence")
	}
	if rec.Decision != execute.EvidenceKindTerminalWritten {
		t.Errorf("Decision = %q, want the OBS-024 kind %q", rec.Decision, execute.EvidenceKindTerminalWritten)
	}
	wantSubject := "instance-1|end"
	if rec.SubjectRef != wantSubject {
		t.Errorf("SubjectRef = %q, want %q", rec.SubjectRef, wantSubject)
	}
	wantReason := "event:instance-1@1|sha256:deadbeef"
	if rec.ReasonCode != wantReason {
		t.Errorf("ReasonCode = %q, want %q", rec.ReasonCode, wantReason)
	}
	if !rec.OccurredAt.Equal(at) {
		t.Errorf("OccurredAt = %v, want %v", rec.OccurredAt, at)
	}

	// Round-trip the documented convention back apart.
	instanceID, nodeID, ok := strings.Cut(rec.SubjectRef, "|")
	if !ok || instanceID != "instance-1" || nodeID != "end" {
		t.Errorf("SubjectRef %q did not round-trip to instance-1/end", rec.SubjectRef)
	}
	refID, digest, ok := strings.Cut(rec.ReasonCode, "|")
	if !ok || refID != "event:instance-1@1" || digest != "sha256:deadbeef" {
		t.Errorf("ReasonCode %q did not round-trip to refID/digest", rec.ReasonCode)
	}
}

// TestTodo_OBS_024_Security proves the adapter never fabricates an evidence
// id or a record for a kind outside OBS-024's own enumerated vocabulary is
// still packed identically (the adapter itself enforces no allowlist — the
// vocabulary is a caller-side contract, execute.EvidenceKind* — so an
// unexpected kind must still be visible verbatim in Decision for whatever
// reads it back, never silently coerced to a different value that would
// hide a caller bug).
func TestTodo_OBS_024_Security(t *testing.T) {
	sink := app.NewMemoryEvidenceSink()
	adapter := NewCapabilityEvidenceAdapter(sink, nil)
	tenantID := uuid.New()

	for i, kind := range []string{
		execute.EvidenceKindGateRefused, execute.EvidenceKindGateAdmitted,
		execute.EvidenceKindApprovalCompleted, execute.EvidenceKindTaskSubmitted,
		execute.EvidenceKindTerminalWritten,
	} {
		id, err := adapter.RecordExecutionEvidence(context.Background(), tenantID, kind, "inst", "node", "ref", "digest", time.Time{})
		if err != nil {
			t.Fatalf("RecordExecutionEvidence(%s): %v", kind, err)
		}
		if id == "" {
			t.Fatalf("RecordExecutionEvidence(%s) returned an empty id", kind)
		}
		if got := sink.Records()[i].Decision; got != kind {
			t.Errorf("Records()[%d].Decision = %q, want %q", i, got, kind)
		}
	}
	if sink.Len() != 5 {
		t.Fatalf("sink.Len() = %d, want 5 (one per enumerated kind)", sink.Len())
	}
}

// plainCapabilitySink is a capability.EvidenceSink with no storage-tenant
// method, the shape the adapter packs onto.
type plainCapabilitySink struct {
	records []capability.InvocationEvidence
}

func (s *plainCapabilitySink) RecordInvocation(_ context.Context, evt capability.InvocationEvidence) (string, error) {
	s.records = append(s.records, evt)
	return "ev:plain", nil
}

// TestCapabilityEvidenceAdapterPacksOntoAPlainCapabilitySink proves the
// adapter's fallback path: a sink that cannot record a storage tenant
// receives the OBS-024 packing with the clock filling a zero instant, and
// names no tenant it was never given.
func TestCapabilityEvidenceAdapterPacksOntoAPlainCapabilitySink(t *testing.T) {
	at := time.Date(2026, 9, 15, 8, 0, 0, 0, time.UTC)
	sink := &plainCapabilitySink{}
	adapter := NewCapabilityEvidenceAdapter(sink, func() time.Time { return at })
	id, err := adapter.RecordExecutionEvidence(context.Background(), uuid.New(), execute.EvidenceKindTaskSubmitted, "i", "n", "r", "d", time.Time{})
	if err != nil || id != "ev:plain" || len(sink.records) != 1 {
		t.Fatalf("RecordExecutionEvidence = %q, %v, records %d", id, err, len(sink.records))
	}
	if want := app.ExecutionEvidenceOf(execute.EvidenceKindTaskSubmitted, "i", "n", "r", "d", at); sink.records[0] != want {
		t.Fatalf("packed evidence = %+v, want %+v", sink.records[0], want)
	}
}
