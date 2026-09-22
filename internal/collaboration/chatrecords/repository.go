package chatrecords

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
)

var (
	ErrInvalid         = errors.New("chatrecords: invalid request")
	ErrConflict        = errors.New("chatrecords: append conflict")
	ErrHeld            = errors.New("chatrecords: record is under legal hold")
	ErrUnauthorized    = errors.New("chatrecords: unauthorized")
	ErrPrivateEvidence = errors.New("chatrecords: private evidence requires a scoped reference")
)

type MemoryRepository struct {
	mu      sync.RWMutex
	records map[string]Record
	events  map[string][]AuditEvent
	outbox  map[string][]OutboxEvent
	holds   map[string][]Hold
	exports map[string]Export
	reports map[string][]Report
	actions map[string][]CaseAction
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{records: map[string]Record{}, events: map[string][]AuditEvent{}, outbox: map[string][]OutboxEvent{}, holds: map[string][]Hold{}, exports: map[string]Export{}, reports: map[string][]Report{}, actions: map[string][]CaseAction{}}
}

func (r *MemoryRepository) Append(_ context.Context, record Record, event AuditEvent, outbox OutboxEvent) error {
	if record.TenantID == "" || record.RecordID == "" || record.Kind == "" || event.TenantID != record.TenantID || event.EventID == "" || event.Action == "" || outbox.EventID != event.EventID {
		return ErrInvalid
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if old, ok := r.records[key(record.TenantID, record.RecordID)]; ok {
		if old.Revision >= record.Revision {
			return ErrConflict
		}
	}
	r.records[key(record.TenantID, record.RecordID)] = cloneRecord(record)
	r.events[record.TenantID] = append(r.events[record.TenantID], event)
	r.outbox[record.TenantID] = append(r.outbox[record.TenantID], outbox)
	return nil
}
func (r *MemoryRepository) List(_ context.Context, tenant string) ([]Record, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []Record{}
	for _, v := range r.records {
		if v.TenantID == tenant {
			out = append(out, cloneRecord(v))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RecordID < out[j].RecordID })
	return out, nil
}
func (r *MemoryRepository) Events(_ context.Context, tenant string) ([]AuditEvent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := append([]AuditEvent(nil), r.events[tenant]...)
	return out, nil
}
func (r *MemoryRepository) PutHold(_ context.Context, h Hold) error {
	if h.TenantID == "" || h.HoldID == "" || h.Reason == "" || h.PlacedBy == "" {
		return ErrInvalid
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, x := range r.holds[h.TenantID] {
		if x.HoldID == h.HoldID {
			return ErrConflict
		}
	}
	r.holds[h.TenantID] = append(r.holds[h.TenantID], h)
	return nil
}
func (r *MemoryRepository) Holds(_ context.Context, tenant string) ([]Hold, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]Hold(nil), r.holds[tenant]...), nil
}
func (r *MemoryRepository) PutExport(_ context.Context, e Export) error {
	if e.TenantID == "" || e.ExportID == "" || e.Digest == "" {
		return ErrInvalid
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.exports[e.TenantID+":"+e.ExportID]; ok {
		return ErrConflict
	}
	r.exports[e.TenantID+":"+e.ExportID] = e
	return nil
}
func (r *MemoryRepository) PutReport(_ context.Context, p Report) error {
	if p.TenantID == "" || p.ReportID == "" || p.TargetID == "" || p.ReporterID == "" || p.Reason == "" {
		return ErrInvalid
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reports[p.TenantID] = append(r.reports[p.TenantID], p)
	return nil
}
func (r *MemoryRepository) Reports(_ context.Context, tenant string) ([]Report, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]Report(nil), r.reports[tenant]...), nil
}
func (r *MemoryRepository) PutCaseAction(_ context.Context, tenant string, a CaseAction) error {
	if tenant == "" || a.CaseID == "" || a.ActorID == "" || a.Action == "" || a.Reason == "" {
		return ErrInvalid
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.actions[tenant] = append(r.actions[tenant], a)
	return nil
}
func (r *MemoryRepository) Snapshot(_ context.Context, tenant string) (Snapshot, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rec := []Record{}
	for _, x := range r.records {
		if x.TenantID == tenant {
			rec = append(rec, cloneRecord(x))
		}
	}
	sort.Slice(rec, func(i, j int) bool { return rec[i].RecordID < rec[j].RecordID })
	ev := append([]AuditEvent(nil), r.events[tenant]...)
	ob := append([]OutboxEvent(nil), r.outbox[tenant]...)
	s := Snapshot{TenantID: tenant, Records: rec, Events: ev, Outbox: ob}
	s.Watermark = uint64(len(ev))
	s.Digest = digest(s)
	return s, nil
}
func (r *MemoryRepository) Restore(_ context.Context, s Snapshot) error {
	if s.TenantID == "" || s.Digest == "" || digest(s) != s.Digest {
		return ErrInvalid
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, x := range s.Records {
		if x.TenantID != s.TenantID {
			return ErrInvalid
		}
		r.records[key(s.TenantID, x.RecordID)] = cloneRecord(x)
	}
	r.events[s.TenantID] = append([]AuditEvent(nil), s.Events...)
	r.outbox[s.TenantID] = append([]OutboxEvent(nil), s.Outbox...)
	return nil
}
func (r *MemoryRepository) Reconcile(_ context.Context, tenant string) (ReconcileResult, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	x := ReconcileResult{Records: 0, Events: len(r.events[tenant]), Outbox: len(r.outbox[tenant])}
	for _, v := range r.records {
		if v.TenantID == tenant {
			x.Records++
		}
	}
	seen := map[uint64]bool{}
	ids := map[string]bool{}
	for _, e := range r.events[tenant] {
		if seen[e.Sequence] {
			x.DuplicateSequences++
		}
		seen[e.Sequence] = true
		ids[e.EventID] = true
	}
	for _, o := range r.outbox[tenant] {
		if !ids[o.EventID] {
			x.MissingOutbox++
		}
	}
	x.Ready = x.MissingOutbox == 0 && x.DuplicateSequences == 0 && x.Events == x.Outbox
	return x, nil
}

func key(t, id string) string     { return t + ":" + id }
func cloneRecord(r Record) Record { r.HoldIDs = append([]string(nil), r.HoldIDs...); return r }
func digest(s Snapshot) string {
	s.Digest = ""
	b, _ := json.Marshal(s)
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}

var _ Repository = (*MemoryRepository)(nil)
var _ = fmt.Sprintf
