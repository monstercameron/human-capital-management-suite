package search

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

var (
	// ErrIndexStale reports a record older than the query watermark. Its
	// message carries the machine-readable UNAVAILABLE_STALE code.
	ErrIndexStale = errors.New("search: index record is UNAVAILABLE_STALE")
	// ErrIndexDenied refuses an over-clearance or cross-tenant read without
	// revealing whether the document exists.
	ErrIndexDenied = errors.New("search: index read denied")
	// ErrIndexGone reports a tombstoned or deleted source.
	ErrIndexGone = errors.New("search: indexed source is gone")
	// ErrIndexHeld reports a record under legal hold.
	ErrIndexHeld = errors.New("search: indexed source is under legal hold")
	// ErrIndexQuarantined reports a quarantined source.
	ErrIndexQuarantined = errors.New("search: indexed source is quarantined")
	// ErrIndexInvalid identifies a malformed index write.
	ErrIndexInvalid = errors.New("search: invalid index write")
)

// isUnavailableStale reports the machine-readable stale code.
func isUnavailableStale(err error) bool {
	return errors.Is(err, ErrIndexStale) && strings.Contains(err.Error(), "UNAVAILABLE_STALE")
}

// IndexRecord is the rebuildable index copy of one source record. It carries
// the source watermark, classification, ACL epoch, retention/hold state and
// embedding model version so restriction, deletion and hold propagate.
type IndexRecord struct {
	DocID           string
	Tenant          string
	SourceWatermark values.Instant
	Classification  dlp.DataClass
	ACLEpoch        int64
	ModelVersion    string
	Hold            bool
	Quarantined     bool
	Tombstoned      bool
	TombstoneReason string
}

func (r IndexRecord) Validate() error {
	if strings.TrimSpace(r.DocID) == "" || strings.TrimSpace(r.Tenant) == "" {
		return fmt.Errorf("%w: document and tenant are required", ErrIndexInvalid)
	}
	if err := r.SourceWatermark.Validate(); err != nil {
		return fmt.Errorf("%w: source watermark: %v", ErrIndexInvalid, err)
	}
	if strings.TrimSpace(r.ModelVersion) == "" {
		return fmt.Errorf("%w: embedding model version is required", ErrIndexInvalid)
	}
	return nil
}

// Tombstone is the idempotent deletion marker for one document.
type Tombstone struct {
	Tenant    string
	DocID     string
	Watermark values.Instant
	Reason    string
}

// SourceSnapshot is one source journal entry consumed by Rebuild.
type SourceSnapshot struct {
	Record    IndexRecord
	Tombstone *Tombstone
}

// GetRequest is one authorized index read.
type GetRequest struct {
	Tenant       string
	DocID        string
	MinWatermark values.Instant
	Clearance    []dlp.DataClass
	ACLEpoch     int64
}

// RebuildReport accounts for one rebuild.
type RebuildReport struct {
	Applied           int
	SkippedTombstoned int
	SkippedStale      int
	Resurrected       int
}

// Index is the SEARCH-002 rebuildable copy. It is never authority: every
// write honors source watermarks and tombstones, and every read enforces
// watermark, clearance, epoch, hold and quarantine state.
type Index struct {
	mu      sync.RWMutex
	tenant  string
	records map[string]IndexRecord
}

// NewIndex builds an empty index copy.
func NewIndex() *Index { return &Index{records: map[string]IndexRecord{}} }

// authorizeWrite pins the owning tenant on first write and refuses every
// foreign write after. Callers must hold the write lock.
func (x *Index) authorizeWrite(tenant string) error {
	if strings.TrimSpace(tenant) == "" {
		return fmt.Errorf("%w: tenant is required", ErrIndexInvalid)
	}
	if x.tenant == "" {
		x.tenant = tenant
		return nil
	}
	if tenant != x.tenant {
		return fmt.Errorf("%w: cross-tenant index access", ErrIndexDenied)
	}
	return nil
}

// authorizeRead refuses foreign reads without mutating the index. Callers
// may hold either lock.
func (x *Index) authorizeRead(tenant string) error {
	if x.tenant != "" && tenant != x.tenant {
		return fmt.Errorf("%w: cross-tenant index access", ErrIndexDenied)
	}
	return nil
}

// Upsert applies one source write. Older watermarks never regress the copy,
// and tombstoned documents stay tombstoned unless the source advances past
// the tombstone with a live record.
func (x *Index) Upsert(record IndexRecord) error {
	if err := record.Validate(); err != nil {
		return err
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	if err := x.authorizeWrite(record.Tenant); err != nil {
		return err
	}
	if current, ok := x.records[record.DocID]; ok {
		if record.SourceWatermark.Compare(current.SourceWatermark) <= 0 {
			return nil
		}
		record.Tombstoned = false
		record.TombstoneReason = ""
	}
	x.records[record.DocID] = record
	return nil
}

// ApplyTombstone marks one document gone. Tombstones are idempotent: the
// highest watermark wins and repeats change nothing.
func (x *Index) ApplyTombstone(tomb Tombstone) error {
	if strings.TrimSpace(tomb.DocID) == "" {
		return fmt.Errorf("%w: tombstone needs a document", ErrIndexInvalid)
	}
	if err := tomb.Watermark.Validate(); err != nil {
		return fmt.Errorf("%w: tombstone watermark: %v", ErrIndexInvalid, err)
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	if err := x.authorizeWrite(tomb.Tenant); err != nil {
		return err
	}
	current, ok := x.records[tomb.DocID]
	if !ok {
		x.records[tomb.DocID] = IndexRecord{DocID: tomb.DocID, Tenant: tomb.Tenant, Tombstoned: true, TombstoneReason: tomb.Reason, SourceWatermark: tomb.Watermark}
		return nil
	}
	if tomb.Watermark.Compare(current.SourceWatermark) <= 0 {
		return nil
	}
	current.Tombstoned = true
	current.TombstoneReason = tomb.Reason
	current.SourceWatermark = tomb.Watermark
	x.records[tomb.DocID] = current
	return nil
}

// Get serves one authorized read. Stale copies fail with UNAVAILABLE_STALE;
// restricted, deleted, held and quarantined sources are never retrievable.
func (x *Index) Get(req GetRequest) (IndexRecord, error) {
	x.mu.RLock()
	defer x.mu.RUnlock()
	if err := x.authorizeRead(req.Tenant); err != nil {
		return IndexRecord{}, err
	}
	record, ok := x.records[req.DocID]
	if !ok || record.Tenant == "" {
		return IndexRecord{}, fmt.Errorf("%w: document is not indexed", ErrIndexDenied)
	}
	if record.Tombstoned {
		return IndexRecord{}, fmt.Errorf("%w: %s", ErrIndexGone, record.TombstoneReason)
	}
	cleared := false
	for _, class := range req.Clearance {
		if class == record.Classification {
			cleared = true
			break
		}
	}
	if !cleared {
		return IndexRecord{}, fmt.Errorf("%w: classification is not cleared", ErrIndexDenied)
	}
	if record.Hold {
		return IndexRecord{}, fmt.Errorf("%w: record is under legal hold", ErrIndexHeld)
	}
	if record.Quarantined {
		return IndexRecord{}, fmt.Errorf("%w: record is quarantined", ErrIndexQuarantined)
	}
	if record.SourceWatermark.Compare(req.MinWatermark) < 0 {
		return IndexRecord{}, fmt.Errorf("%w: have %s want %s", ErrIndexStale, record.SourceWatermark, req.MinWatermark)
	}
	if req.ACLEpoch < record.ACLEpoch {
		return IndexRecord{}, fmt.Errorf("%w: ACL epoch %d supersedes the reader epoch %d", ErrIndexStale, record.ACLEpoch, req.ACLEpoch)
	}
	return record, nil
}

// Rebuild converges a fresh copy from the source journal. Tombstoned sources
// are never reintroduced: resurrection is counted and must stay zero.
func (x *Index) Rebuild(journal []SourceSnapshot) (RebuildReport, error) {
	var report RebuildReport
	for _, snap := range journal {
		if snap.Tombstone != nil {
			if err := x.ApplyTombstone(*snap.Tombstone); err != nil {
				return report, err
			}
			report.SkippedTombstoned++
			continue
		}
		if err := snap.Record.Validate(); err != nil {
			return report, err
		}
		if current, ok := x.records[snap.Record.DocID]; ok && current.Tombstoned &&
			snap.Record.SourceWatermark.Compare(current.SourceWatermark) <= 0 {
			report.SkippedTombstoned++
			continue
		}
		if current, ok := x.records[snap.Record.DocID]; ok && !current.Tombstoned &&
			snap.Record.SourceWatermark.Compare(current.SourceWatermark) <= 0 {
			report.SkippedStale++
			continue
		}
		if err := x.Upsert(snap.Record); err != nil {
			return report, err
		}
		report.Applied++
	}
	return report, nil
}

// Journal exports the live source view: records plus tombstone markers, in
// document order, for failover rebuilds.
func (x *Index) Journal() []SourceSnapshot {
	x.mu.RLock()
	defer x.mu.RUnlock()
	docs := make([]string, 0, len(x.records))
	for doc := range x.records {
		docs = append(docs, doc)
	}
	sort.Strings(docs)
	out := make([]SourceSnapshot, 0, len(docs))
	for _, doc := range docs {
		record := x.records[doc]
		if record.Tombstoned {
			out = append(out, SourceSnapshot{Tombstone: &Tombstone{Tenant: record.Tenant, DocID: doc, Watermark: record.SourceWatermark, Reason: record.TombstoneReason}})
			continue
		}
		if record.Tenant == "" {
			continue
		}
		out = append(out, SourceSnapshot{Record: record})
	}
	return out
}
