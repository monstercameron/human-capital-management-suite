package app

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// TestMemoryEvidenceSinkRecordsTenantAndReadsJourneysBackTenantScoped proves
// the test double honours the [EvidenceStore] contract the durable store
// implements: capability decisions keep the tenant key they were recorded in,
// execution evidence keeps its storage tenant and the OBS-024 packing, and a
// journey read returns only its own tenant's records for the intent, the
// instance and its nodes, in recording order.
func TestMemoryEvidenceSinkRecordsTenantAndReadsJourneysBackTenantScoped(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 9, 15, 9, 0, 0, 0, time.UTC)
	acmeID, globexID := uuid.New(), uuid.New()
	sink := NewMemoryEvidenceSink()

	gate, err := sink.RecordInvocation(ctx, capability.InvocationEvidence{
		CapabilityID: "workflow.execution_authority_gate", CapabilityVersion: 1,
		SubjectRef: "intent-1", Tenant: "acme", Decision: EvidenceKindGateAdmitted, OccurredAt: at,
	})
	if err != nil || gate == "" {
		t.Fatalf("RecordInvocation = %q, %v", gate, err)
	}
	node, err := sink.RecordExecutionEvidence(ctx, acmeID, "APPROVAL_COMPLETED", "inst-1", "approve", "wi-1", "sha256:d", at)
	if err != nil || node == "" || node == gate {
		t.Fatalf("RecordExecutionEvidence = %q, %v", node, err)
	}
	// Another tenant's records for the same subjects, and an unrelated one.
	if _, err := sink.RecordInvocation(ctx, capability.InvocationEvidence{SubjectRef: "intent-1", Tenant: "globex", Decision: "X", OccurredAt: at}); err != nil {
		t.Fatal(err)
	}
	if _, err := sink.RecordExecutionEvidence(ctx, globexID, "TERMINAL_WRITTEN", "inst-1", "end", "ev", "sha256:e", at); err != nil {
		t.Fatal(err)
	}
	if _, err := sink.RecordInvocation(ctx, capability.InvocationEvidence{SubjectRef: "intent-2", Tenant: "acme", Decision: "X", OccurredAt: at}); err != nil {
		t.Fatal(err)
	}
	// A record that names no tenant belongs to none.
	if _, err := sink.RecordInvocation(ctx, capability.InvocationEvidence{SubjectRef: "intent-1", Decision: "X", OccurredAt: at}); err != nil {
		t.Fatal(err)
	}

	records := sink.Records()
	if sink.Len() != 6 || records[0].Tenant != "acme" || records[1].TenantID != acmeID {
		t.Fatalf("records = %+v", records)
	}
	exec := records[1]
	if exec.CapabilityID != ExecutionEvidenceCapabilityID || exec.Decision != "APPROVAL_COMPLETED" ||
		exec.SubjectRef != "inst-1|approve" || exec.ReasonCode != "wi-1|sha256:d" || !exec.OccurredAt.Equal(at) {
		t.Fatalf("execution evidence packing = %+v", exec)
	}

	ids, err := sink.JourneyEvidenceIDs(ctx, "acme", acmeID, "intent-1", "inst-1")
	if err != nil {
		t.Fatalf("JourneyEvidenceIDs: %v", err)
	}
	if len(ids) != 2 || ids[0] != gate || ids[1] != node {
		t.Fatalf("acme journey evidence = %v, want [%s %s]", ids, gate, node)
	}
	if ids, _ := sink.JourneyEvidenceIDs(ctx, "initech", uuid.New(), "intent-1", "inst-1"); len(ids) != 0 {
		t.Fatalf("an unrelated tenant read %v", ids)
	}
	if ids, _ := sink.JourneyEvidenceIDs(ctx, "acme", acmeID, "", ""); len(ids) != 0 {
		t.Fatalf("an empty journey matched %v", ids)
	}
}

// TestServiceEvidenceRecordersNameTheCallersTenant proves every decision the
// service records directly - the execution-authority gate, a cancellation
// disposition and a proposal decision - carries the tenant the caller acted
// in, so a durable store scopes the row instead of refusing it (WF-RUN-035).
func TestServiceEvidenceRecordersNameTheCallersTenant(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 9, 15, 11, 0, 0, 0, time.UTC)
	sink := NewMemoryEvidenceSink()
	svc := &IntentService{evidence: sink, clock: func() values.Instant { return values.NewInstant(at) }}

	if _, err := svc.recordGateEvidence(ctx, "acme", EvidenceKindGateAdmitted, "intent-g", ""); err != nil {
		t.Fatalf("recordGateEvidence: %v", err)
	}
	svc.recordCancellationEvidence(ctx, "globex", "intent-c", "CANCELLATION_PENDING", "reason:late")
	svc.recordProposalDecisionEvidence(ctx, "initech", "intent-p", true, "INVOKED", "")

	want := []struct{ tenant, subject, capability string }{
		{"acme", "intent-g", "workflow.execution_authority_gate"},
		{"globex", "intent-c", "intent.cancellation_disposition"},
		{"initech", "intent-p", proposalDecisionCapabilityApprove},
	}
	records := sink.Records()
	if len(records) != len(want) {
		t.Fatalf("records = %+v, want %d", records, len(want))
	}
	for i, w := range want {
		if records[i].Tenant != w.tenant || records[i].SubjectRef != w.subject || records[i].CapabilityID != w.capability || !records[i].OccurredAt.Equal(at) {
			t.Errorf("records[%d] = %+v, want tenant %s subject %s capability %s", i, records[i], w.tenant, w.subject, w.capability)
		}
	}
}

// memoryEvidence returns the in-memory test double a test cell was composed
// with, failing the test when the cell holds any other store.
func memoryEvidence(t *testing.T, cell *Cell) *MemoryEvidenceSink {
	t.Helper()
	sink, ok := cell.Evidence.(*MemoryEvidenceSink)
	if !ok {
		t.Fatalf("cell evidence is %T, want the in-memory test double", cell.Evidence)
	}
	return sink
}

// TestExecutionEvidenceOfPacksTheOBS024Vocabulary pins the packing every
// store shares.
func TestExecutionEvidenceOfPacksTheOBS024Vocabulary(t *testing.T) {
	at := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	got := ExecutionEvidenceOf("TASK_SUBMITTED", "i", "n", "r", "d", at)
	want := capability.InvocationEvidence{
		CapabilityID: ExecutionEvidenceCapabilityID, CapabilityVersion: 1,
		SubjectRef: "i|n", Decision: "TASK_SUBMITTED", ReasonCode: "r|d", OccurredAt: at,
	}
	if got != want {
		t.Fatalf("ExecutionEvidenceOf = %+v, want %+v", got, want)
	}
}
