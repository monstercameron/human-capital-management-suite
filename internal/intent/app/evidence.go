package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// EvidenceStore is the one evidence port a cell and its execution driver
// record on, and the journey reads back from (WF-RUN-035).
//
// It is the capability gateway's [capability.EvidenceSink], the execution
// driver's OBS-024 port (internal/workflow/execute.ExecutionEvidence, which
// this interface satisfies structurally because this package must not import
// the driver), and a tenant-scoped read of the evidence one journey produced.
// A served composition wires the durable PostgreSQL store
// (internal/data/evidencestore); [MemoryEvidenceSink] is the test double.
type EvidenceStore interface {
	capability.EvidenceSink
	// RecordExecutionEvidence records one OBS-024 execution-evidence entry
	// for the storage tenant the run committed under.
	RecordExecutionEvidence(ctx context.Context, tenantID uuid.UUID, kind, instanceID, nodeID, refID, digest string, occurredAt time.Time) (string, error)
	// JourneyEvidenceIDs returns, in recording order, the ids of the evidence
	// recorded in tenant (whose storage identity is tenantID, or uuid.Nil
	// when the composition maps none) whose subject is intentID, instanceID
	// or a node of instanceID ("<instanceID>|<nodeID>").
	JourneyEvidenceIDs(ctx context.Context, tenant values.TenantId, tenantID uuid.UUID, intentID, instanceID string) ([]string, error)
}

// ExecutionEvidenceCapabilityID is the capability id an OBS-024 execution
// evidence entry is recorded under. Every [EvidenceStore] packs the richer
// execution vocabulary into the capability evidence fields the same way:
// Decision carries the kind, SubjectRef "<instanceID>|<nodeID>" and ReasonCode
// "<refID>|<digest>" (see [ExecutionEvidenceOf]).
const ExecutionEvidenceCapabilityID = "workflow.execution.evidence"

// ExecutionEvidenceOf packs one OBS-024 execution-evidence entry into the
// capability evidence shape every store records.
func ExecutionEvidenceOf(kind, instanceID, nodeID, refID, digest string, occurredAt time.Time) capability.InvocationEvidence {
	return capability.InvocationEvidence{
		CapabilityID:      ExecutionEvidenceCapabilityID,
		CapabilityVersion: 1,
		SubjectRef:        instanceID + "|" + nodeID,
		Decision:          kind,
		ReasonCode:        refID + "|" + digest,
		OccurredAt:        occurredAt,
	}
}

// MemoryEvidenceSink records every evidence decision this process made, in
// order, in memory.
//
// It is the test double of [EvidenceStore]: it hands each record a
// deterministic identifier derived from its content and its ordinal, can be
// read back and asserted on, and loses everything on restart. A served
// composition never wires it (internal/application's composition test fails
// if it does); the durable store is internal/data/evidencestore.
type MemoryEvidenceSink struct {
	mu      sync.Mutex
	records []EvidenceRecord
}

var _ EvidenceStore = (*MemoryEvidenceSink)(nil)

// NewMemoryEvidenceSink returns an empty sink.
func NewMemoryEvidenceSink() *MemoryEvidenceSink { return &MemoryEvidenceSink{} }

// RecordInvocation implements capability.EvidenceSink.
func (s *MemoryEvidenceSink) RecordInvocation(_ context.Context, evt capability.InvocationEvidence) (string, error) {
	return s.record(evt, uuid.Nil), nil
}

// RecordExecutionEvidence implements [EvidenceStore].
func (s *MemoryEvidenceSink) RecordExecutionEvidence(_ context.Context, tenantID uuid.UUID, kind, instanceID, nodeID, refID, digest string, occurredAt time.Time) (string, error) {
	return s.record(ExecutionEvidenceOf(kind, instanceID, nodeID, refID, digest, occurredAt), tenantID), nil
}

// RecordExecutionEvidenceTx records on the caller's transaction the way the
// workflow driver's atomic half (internal/workflow/execute.ExecutionEvidenceTx,
// satisfied here structurally because this package must not import the
// driver) requires. Memory has no transactions, so tx is only witnessed,
// never used: the entry joins the caller's unit of work by sharing its fate
// in the test, exactly as the durable store shares the advance transaction's
// fate in production.
func (s *MemoryEvidenceSink) RecordExecutionEvidenceTx(_ context.Context, _ dbport.Tx, tenantID uuid.UUID, kind, instanceID, nodeID, refID, digest string, occurredAt time.Time) (string, error) {
	return s.record(ExecutionEvidenceOf(kind, instanceID, nodeID, refID, digest, occurredAt), tenantID), nil
}

func (s *MemoryEvidenceSink) record(evt capability.InvocationEvidence, tenantID uuid.UUID) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	ordinal := len(s.records) + 1
	sum := sha256.Sum256(fmt.Appendf(nil, "%d|%s|%d|%s|%s|%s",
		ordinal, evt.CapabilityID, evt.CapabilityVersion, evt.SubjectRef, evt.Decision, evt.ReasonCode))
	id := "ev:capability:" + hex.EncodeToString(sum[:12])

	s.records = append(s.records, EvidenceRecord{
		EvidenceID:        id,
		CapabilityID:      evt.CapabilityID,
		CapabilityVersion: evt.CapabilityVersion,
		SubjectRef:        evt.SubjectRef,
		Tenant:            evt.Tenant,
		TenantID:          tenantID,
		Decision:          evt.Decision,
		ReasonCode:        evt.ReasonCode,
		OccurredAt:        evt.OccurredAt,
		Purpose:           evt.Purpose,
		IdempotencyKey:    evt.IdempotencyKey,
		Deadline:          evt.Deadline,
		EffectClass:       string(evt.EffectClass),
	})
	return id
}

// JourneyEvidenceIDs implements [EvidenceStore]. A record belongs to the
// tenant when its recorded tenant key equals tenant or its recorded storage
// tenant equals tenantID; a record that names neither belongs to no tenant.
func (s *MemoryEvidenceSink) JourneyEvidenceIDs(_ context.Context, tenant values.TenantId, tenantID uuid.UUID, intentID, instanceID string) ([]string, error) {
	var out []string
	for _, rec := range s.Records() {
		inTenant := (rec.Tenant != "" && rec.Tenant == string(tenant)) ||
			(rec.TenantID != uuid.Nil && rec.TenantID == tenantID)
		if inTenant && journeyEvidenceMatches(rec.SubjectRef, intentID, instanceID) {
			out = append(out, rec.EvidenceID)
		}
	}
	return out, nil
}

// Records returns a copy of everything recorded so far.
func (s *MemoryEvidenceSink) Records() []EvidenceRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]EvidenceRecord, len(s.records))
	copy(out, s.records)
	return out
}

// Len returns how many decisions were recorded.
func (s *MemoryEvidenceSink) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.records)
}
