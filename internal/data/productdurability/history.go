package productdurability

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ALIGN-033: material history is append-oriented. The journal accepts
// appends and reads; it offers no update and no delete, so a recorded fact
// can only be superseded by a newer fact, never rewritten. Each entry
// hash-chains its predecessor, so tampering with any entry breaks every
// later digest.

// History errors.
var (
	ErrHistoryInvalid  = errors.New("productdurability: history entry is invalid")
	ErrHistoryTampered = errors.New("productdurability: history chain is broken")
)

// HistoryEntry is one immutable material fact. PrevDigest chains the
// previous entry of the same stream; Digest covers tenant, stream,
// sequence, payload, timestamp, and predecessor.
type HistoryEntry struct {
	Tenant        values.TenantId `json:"tenant"`
	Stream        string          `json:"stream"`
	Sequence      uint64          `json:"sequence"`
	PayloadDigest string          `json:"payload_digest"`
	RecordedAt    time.Time       `json:"recorded_at"`
	PrevDigest    string          `json:"prev_digest"`
	Digest        string          `json:"digest"`
}

func (e HistoryEntry) computeDigest() string {
	raw := strings.Join([]string{
		e.Tenant.String(), e.Stream,
		fmt.Sprint(e.Sequence), e.PayloadDigest,
		e.RecordedAt.UTC().Format(time.RFC3339Nano), e.PrevDigest,
	}, "\x00")
	sum := sha256.Sum256([]byte(raw))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// HistoryJournal is the append-only material journal. The zero value is
// not usable; build one with NewHistoryJournal.
type HistoryJournal struct {
	mu      sync.Mutex
	entries []HistoryEntry
}

// NewHistoryJournal builds an empty journal.
func NewHistoryJournal() *HistoryJournal {
	return &HistoryJournal{}
}

// Append records one material fact at the tail of its stream. Sequences
// are assigned by the journal, never by the caller.
func (j *HistoryJournal) Append(tenant values.TenantId, stream, payloadDigest string, at time.Time) (HistoryEntry, error) {
	if err := tenant.Validate(); err != nil {
		return HistoryEntry{}, fmt.Errorf("%w: tenant: %v", ErrHistoryInvalid, err)
	}
	if strings.TrimSpace(stream) == "" || strings.TrimSpace(payloadDigest) == "" {
		return HistoryEntry{}, fmt.Errorf("%w: stream and payload digest are required", ErrHistoryInvalid)
	}
	if at.IsZero() {
		return HistoryEntry{}, fmt.Errorf("%w: recorded time is required", ErrHistoryInvalid)
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	var prev string
	var sequence uint64 = 1
	for i := len(j.entries) - 1; i >= 0; i-- {
		if j.entries[i].Tenant == tenant && j.entries[i].Stream == stream {
			prev = j.entries[i].Digest
			sequence = j.entries[i].Sequence + 1
			if !at.After(j.entries[i].RecordedAt) && !at.Equal(j.entries[i].RecordedAt) {
				return HistoryEntry{}, fmt.Errorf("%w: entry predates the stream tail", ErrHistoryInvalid)
			}
			break
		}
	}
	entry := HistoryEntry{
		Tenant: tenant, Stream: stream, Sequence: sequence,
		PayloadDigest: payloadDigest, RecordedAt: at.UTC(), PrevDigest: prev,
	}
	entry.Digest = entry.computeDigest()
	j.entries = append(j.entries, entry)
	return entry, nil
}

// Verify replays the chain and reports the first break. A journal that was
// only ever appended to always verifies.
func (j *HistoryJournal) Verify() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.verifyLocked()
}

func (j *HistoryJournal) verifyLocked() error {
	heads := make(map[string]HistoryEntry)
	for _, entry := range j.entries {
		if entry.Digest == "" || entry.Digest != entry.computeDigest() {
			return fmt.Errorf("%w: %s/%d", ErrHistoryTampered, entry.Stream, entry.Sequence)
		}
		slot := entry.Tenant.String() + "\x00" + entry.Stream
		prev, ok := heads[slot]
		if !ok {
			if entry.Sequence != 1 || entry.PrevDigest != "" {
				return fmt.Errorf("%w: %s does not start at sequence 1", ErrHistoryTampered, entry.Stream)
			}
		} else {
			if entry.Sequence != prev.Sequence+1 || entry.PrevDigest != prev.Digest {
				return fmt.Errorf("%w: %s/%d does not chain its predecessor", ErrHistoryTampered, entry.Stream, entry.Sequence)
			}
		}
		heads[slot] = entry
	}
	return nil
}

// Replay returns the entries of one tenant stream in sequence order.
func (j *HistoryJournal) Replay(tenant values.TenantId, stream string) ([]HistoryEntry, error) {
	if err := tenant.Validate(); err != nil {
		return nil, fmt.Errorf("%w: tenant: %v", ErrHistoryInvalid, err)
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	var out []HistoryEntry
	for _, entry := range j.entries {
		if entry.Tenant == tenant && entry.Stream == stream {
			out = append(out, entry)
		}
	}
	return out, nil
}

// Streams reports the tenant streams present in the journal, in sorted
// order. It names streams only, never payloads or digests.
func (j *HistoryJournal) Streams() []string {
	j.mu.Lock()
	defer j.mu.Unlock()
	set := make(map[string]bool)
	for _, entry := range j.entries {
		set[entry.Tenant.String()+"\x00"+entry.Stream] = true
	}
	out := make([]string, 0, len(set))
	for slot := range set {
		out = append(out, slot)
	}
	sort.Strings(out)
	return out
}
