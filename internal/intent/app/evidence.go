package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
)

// MemoryEvidenceSink records every capability-gateway decision this process
// made, in order.
//
// P1A has no evidence table (the migration tree stops at ledger, projection
// and outbox), so the shipped sink keeps the records in memory and hands each
// one a deterministic identifier derived from its content and its ordinal. It
// is a real sink, not a stub: the gateway refuses to complete an invocation
// whose evidence cannot be recorded, and this one can be read back and
// asserted on. A durable sink replaces it by satisfying the same
// capability.EvidenceSink port.
type MemoryEvidenceSink struct {
	mu      sync.Mutex
	records []EvidenceRecord
}

var _ capability.EvidenceSink = (*MemoryEvidenceSink)(nil)

// NewMemoryEvidenceSink returns an empty sink.
func NewMemoryEvidenceSink() *MemoryEvidenceSink { return &MemoryEvidenceSink{} }

// RecordInvocation implements capability.EvidenceSink.
func (s *MemoryEvidenceSink) RecordInvocation(_ context.Context, evt capability.InvocationEvidence) (string, error) {
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
		Decision:          evt.Decision,
		ReasonCode:        evt.ReasonCode,
		OccurredAt:        evt.OccurredAt,
		Purpose:           evt.Purpose,
		IdempotencyKey:    evt.IdempotencyKey,
		Deadline:          evt.Deadline,
		EffectClass:       string(evt.EffectClass),
	})
	return id, nil
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
