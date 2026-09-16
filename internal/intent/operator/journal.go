package operator

import (
	"context"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// MemoryJournal is an in-process [Journal] and [ObligationStore]. A gateway
// recomposed over the same journal sees every receipt and every outstanding
// bypass obligation the previous one recorded, which is what a restart over
// durable storage looks like.
type MemoryJournal struct {
	mu       sync.Mutex
	receipts map[string]Receipt
	// obligations holds the bypass obligations of the same actions
	// (WF-RUN-039); its methods live in obligation.go.
	obligations memoryObligations
}

var _ ObligationStore = (*MemoryJournal)(nil)

// NewMemoryJournal returns an empty journal.
func NewMemoryJournal() *MemoryJournal { return &MemoryJournal{receipts: map[string]Receipt{}} }

func journalKey(tenant values.TenantId, key string) string { return string(tenant) + "\x1f" + key }

// Lookup implements Journal.
func (j *MemoryJournal) Lookup(_ context.Context, tenant values.TenantId, key string) (Receipt, bool, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	r, ok := j.receipts[journalKey(tenant, key)]
	return r, ok, nil
}

// Begin implements Journal.
func (j *MemoryJournal) Begin(_ context.Context, pending Receipt) (Receipt, bool, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	k := journalKey(pending.Tenant, pending.IdempotencyKey)
	if existing, ok := j.receipts[k]; ok {
		return existing, true, nil
	}
	j.receipts[k] = pending
	return pending, false, nil
}

// Complete implements Journal.
func (j *MemoryJournal) Complete(_ context.Context, final Receipt) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	k := journalKey(final.Tenant, final.IdempotencyKey)
	prior, ok := j.receipts[k]
	if !ok || prior.Outcome != OutcomePending || prior.RequestDigest != final.RequestDigest {
		return refuse(CodeJournal, final.Kind, "no matching pending receipt to complete")
	}
	j.receipts[k] = final
	return nil
}

// Receipts returns every recorded receipt.
func (j *MemoryJournal) Receipts() []Receipt {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make([]Receipt, 0, len(j.receipts))
	for _, r := range j.receipts {
		out = append(out, r)
	}
	return out
}

// Abort implements Journal.
func (j *MemoryJournal) Abort(_ context.Context, pending Receipt) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	k := journalKey(pending.Tenant, pending.IdempotencyKey)
	prior, ok := j.receipts[k]
	if !ok || prior.Outcome != OutcomePending || prior.RequestDigest != pending.RequestDigest {
		return refuse(CodeJournal, pending.Kind, "no matching pending receipt to abort")
	}
	delete(j.receipts, k)
	return nil
}
