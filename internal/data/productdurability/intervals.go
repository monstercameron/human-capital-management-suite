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

// ALIGN-034: effective intervals are half-open. A fact holds from its
// inclusive start until its exclusive end; an absent end means the fact is
// still current. Two facts for the same key may touch (one ends exactly
// when the next begins) but never overlap, so every instant resolves to at
// most one effective fact.

// Interval errors.
var (
	ErrIntervalInvalid = errors.New("productdurability: effective interval is invalid")
	ErrIntervalOverlap = errors.New("productdurability: effective interval overlaps the current fact")
)

// Interval is one half-open [Start, End) span. A zero End means the fact
// is open: it holds for all time at or after Start.
type Interval struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// Contains reports whether t falls inside the half-open span.
func (iv Interval) Contains(t time.Time) bool {
	if t.Before(iv.Start) {
		return false
	}
	return iv.End.IsZero() || t.Before(iv.End)
}

func (iv Interval) validate() error {
	if iv.Start.IsZero() {
		return fmt.Errorf("%w: start is required", ErrIntervalInvalid)
	}
	if !iv.End.IsZero() && !iv.End.After(iv.Start) {
		return fmt.Errorf("%w: end must be after start", ErrIntervalInvalid)
	}
	return nil
}

func endOrInfinity(iv Interval) time.Time {
	if iv.End.IsZero() {
		return time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC)
	}
	return iv.End
}

// Overlaps reports whether two spans share any instant. Touching spans
// (one ends exactly when the other begins) do not overlap.
func (iv Interval) Overlaps(other Interval) bool {
	return iv.Start.Before(endOrInfinity(other)) && other.Start.Before(endOrInfinity(iv))
}

// IntervalSet holds the non-overlapping effective history of named keys.
// The zero value is not usable; build one with NewIntervalSet.
type IntervalSet struct {
	mu    sync.Mutex
	spans map[string][]Interval
}

// NewIntervalSet builds an empty set.
func NewIntervalSet() *IntervalSet {
	return &IntervalSet{spans: make(map[string][]Interval)}
}

func intervalSlot(tenant values.TenantId, name string) string {
	return tenant.String() + "\x00" + name
}

// Add records one effective span for a key. An overlapping span is
// refused; callers supersede a fact by ending it first (or by adding a
// touching successor), never by covering it.
func (s *IntervalSet) Add(tenant values.TenantId, name string, iv Interval) error {
	if err := tenant.Validate(); err != nil {
		return fmt.Errorf("%w: tenant: %v", ErrIntervalInvalid, err)
	}
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("%w: key name is required", ErrIntervalInvalid)
	}
	iv.Start = iv.Start.UTC()
	iv.End = iv.End.UTC()
	if err := iv.validate(); err != nil {
		return err
	}
	slot := intervalSlot(tenant, name)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.spans[slot] {
		if iv.Overlaps(existing) {
			return fmt.Errorf("%w: %s", ErrIntervalOverlap, name)
		}
	}
	s.spans[slot] = append(s.spans[slot], iv)
	return nil
}

// EffectiveAt returns the fact holding at t, if any.
func (s *IntervalSet) EffectiveAt(tenant values.TenantId, name string, t time.Time) (Interval, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, iv := range s.spans[intervalSlot(tenant, name)] {
		if iv.Contains(t) {
			return iv, true
		}
	}
	return Interval{}, false
}

// Digest returns the content digest over the sorted spans of every key.
func (s *IntervalSet) Digest() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	slots := make([]string, 0, len(s.spans))
	for slot := range s.spans {
		slots = append(slots, slot)
	}
	sort.Strings(slots)
	h := sha256.New()
	for _, slot := range slots {
		spans := append([]Interval(nil), s.spans[slot]...)
		sort.Slice(spans, func(i, j int) bool { return spans[i].Start.Before(spans[j].Start) })
		for _, iv := range spans {
			fmt.Fprintf(h, "%s|%s|%s;", slot,
				iv.Start.UTC().Format(time.RFC3339Nano), iv.End.UTC().Format(time.RFC3339Nano))
		}
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}
