// Package worktimerules computes rolling working-time windows and the
// jurisdiction-specific limits (minors, travel, EU/UK) that read them. The
// package is pure: no database, no clock (every function takes its instant
// as an argument), no network, and no package-level mutable state. Every
// threshold is data supplied by the caller as a rule set; this package
// ships none as constants.
//
// windows.go implements WTIME-007: a ledger of intervals is reduced into
// hours per day, week, 14 days and an arbitrary reference period,
// consecutive days worked and the end of the last rest period. A ledger
// carries a revision computed from its own contents so a caller can record
// exactly what it read (CheckRevision) and detect that another writer moved
// the ledger from underneath a decision that is about to be recorded.
package worktimerules

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"time"
)

var (
	// ErrInvalidLedger reports a ledger entry that cannot be reduced: a
	// missing identity, a zero or inverted interval, or an unknown kind.
	ErrInvalidLedger = errors.New("worktimerules: invalid ledger entry")
	// ErrMissingScope reports a window request with no tenant or worker to
	// scope the read to. Every read is scoped; there is no "all workers".
	ErrMissingScope = errors.New("worktimerules: tenant and worker scope are required")
	// ErrRevisionMismatch reports that the ledger a decision is about to be
	// recorded against no longer matches the revision the caller expected.
	// This is the concurrency guard: two sessions may each observe 46 hours
	// and each try to record a decision against it, but only the session
	// that still holds the current revision may proceed; the other must
	// reread the ledger.
	ErrRevisionMismatch = errors.New("worktimerules: ledger revision mismatch")
)

// IntervalKind classifies one ledger entry.
type IntervalKind string

const (
	KindWork   IntervalKind = "WORK"   // time that counts toward worked hours
	KindRest   IntervalKind = "REST"   // an off-duty rest period
	KindSchool IntervalKind = "SCHOOL" // a school session hour, for minors
)

func (k IntervalKind) valid() bool {
	switch k {
	case KindWork, KindRest, KindSchool:
		return true
	}
	return false
}

// LedgerEntry is one immutable, sourced interval. TenantID and WorkerID
// scope every read; HirerID additionally scopes the AWR qualifying-week
// counter in awr.go and may be empty when it does not apply.
type LedgerEntry struct {
	ID       string
	TenantID string
	WorkerID string
	HirerID  string
	Kind     IntervalKind
	Start    time.Time
	End      time.Time
	// Source names the system of record for this entry (a punch, a
	// schedule, an import). It is required so a forged or unsourced entry
	// can be rejected rather than silently trusted.
	Source string
}

func (e LedgerEntry) validate() error {
	if e.ID == "" || e.TenantID == "" || e.WorkerID == "" {
		return fmt.Errorf("%w: id, tenant_id and worker_id are required", ErrInvalidLedger)
	}
	if e.Source == "" {
		return fmt.Errorf("%w: entry %s: source is required", ErrInvalidLedger, e.ID)
	}
	if !e.Kind.valid() {
		return fmt.Errorf("%w: entry %s: unknown kind %q", ErrInvalidLedger, e.ID, e.Kind)
	}
	if e.Start.IsZero() || e.End.IsZero() || !e.End.After(e.Start) {
		return fmt.Errorf("%w: entry %s: interval must be non-empty", ErrInvalidLedger, e.ID)
	}
	return nil
}

func (e LedgerEntry) duration() time.Duration { return e.End.Sub(e.Start) }

// Ledger is an unordered collection of intervals for any number of tenants
// and workers. Every read into this package scopes down to one tenant and
// worker; the ledger itself may be shared storage.
type Ledger struct {
	Entries []LedgerEntry
}

// Validate checks every entry. A caller may skip this for a ledger it
// trusts (e.g. loaded from its own store) but every exported computation
// in this package validates its own scoped subset regardless.
func (l Ledger) Validate() error {
	for _, e := range l.Entries {
		if err := e.validate(); err != nil {
			return err
		}
	}
	return nil
}

// forScope returns only the entries belonging to the given tenant and
// worker, sorted by start time then ID for deterministic reduction. This is
// the single choke point that enforces tenant/worker isolation: nothing in
// this package reduces a ledger without going through it first.
func (l Ledger) forScope(tenantID, workerID string) ([]LedgerEntry, error) {
	if tenantID == "" || workerID == "" {
		return nil, ErrMissingScope
	}
	out := make([]LedgerEntry, 0, len(l.Entries))
	for _, e := range l.Entries {
		if e.TenantID != tenantID || e.WorkerID != workerID {
			continue
		}
		if err := e.validate(); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Start.Equal(out[j].Start) {
			return out[i].Start.Before(out[j].Start)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// Revision is a stable digest of exactly the entries scoped to tenantID and
// workerID. Two ledgers with the same scoped entries produce the same
// revision regardless of what other tenants' or workers' entries they also
// hold, and regardless of entry order.
func (l Ledger) Revision(tenantID, workerID string) (string, error) {
	scoped, err := l.forScope(tenantID, workerID)
	if err != nil {
		return "", err
	}
	return revisionOf(scoped), nil
}

func revisionOf(entries []LedgerEntry) string {
	h := sha256.New()
	fmt.Fprintf(h, "worktimerules.ledger\x00%d", len(entries))
	for _, e := range entries {
		fmt.Fprintf(h, "\x00%s|%s|%s|%s|%s|%d|%d|%s",
			e.ID, e.TenantID, e.WorkerID, e.HirerID, e.Kind,
			e.Start.UTC().UnixNano(), e.End.UTC().UnixNano(), e.Source)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// CheckRevision reports ErrRevisionMismatch when the ledger's current
// revision for the given scope no longer equals expected. A decision that
// read a window at one revision must call this immediately before
// recording anything that depends on that window; if it fails, the caller
// rereads the ledger and recomputes rather than recording a stale decision.
func CheckRevision(ledger Ledger, tenantID, workerID, expected string) error {
	current, err := ledger.Revision(tenantID, workerID)
	if err != nil {
		return err
	}
	if expected == "" || current != expected {
		return fmt.Errorf("%w: expected %s, ledger is at %s", ErrRevisionMismatch, expected, current)
	}
	return nil
}

// WindowOptions parameterizes a window read. ReferencePeriod is a caller
// supplied rolling duration (e.g. 17 weeks for a UK AWR average, 26 weeks
// for a EU reference period); zero skips computing it.
type WindowOptions struct {
	TenantID        string
	WorkerID        string
	AsOf            time.Time
	ReferencePeriod time.Duration
}

func (o WindowOptions) validate() error {
	if o.TenantID == "" || o.WorkerID == "" {
		return ErrMissingScope
	}
	if o.AsOf.IsZero() {
		return fmt.Errorf("%w: as_of is required", ErrInvalidLedger)
	}
	return nil
}

// Windows is the reduced answer for one worker as of one instant, plus the
// ledger revision it was computed from.
type Windows struct {
	Revision string
	AsOf     time.Time

	DayMinutes             int // trailing 24h
	WeekMinutes            int // trailing 7 days
	Fortnight14DaysMinutes int // trailing 14 days
	ReferencePeriodMinutes int // trailing ReferencePeriod, 0 if not requested

	ConsecutiveDaysWorked int

	LastRestEnd    time.Time
	HasLastRestEnd bool

	SchoolWeekMinutes int // trailing 7 days of KindWork while a SCHOOL entry overlaps
}

// Explain returns an audit-safe one-line summary: figures and the revision
// they were read at, never raw ledger contents.
func (w Windows) Explain() string {
	return fmt.Sprintf("worktimerules windows as_of=%s revision=%s day=%dm week=%dm fortnight=%dm reference=%dm consecutive_days=%d school_week=%dm",
		w.AsOf.UTC().Format(time.RFC3339), w.Revision, w.DayMinutes, w.WeekMinutes,
		w.Fortnight14DaysMinutes, w.ReferencePeriodMinutes, w.ConsecutiveDaysWorked, w.SchoolWeekMinutes)
}

// Version is the worktimerules contract version (ARCH-GO-009). A change to
// the meaning or shape of Windows, or of any rule-set struct in this
// package, must advance this value.
func Version() int { return 1 }

// rollingWorkMinutes sums KindWork minutes clipped to (asOf-window, asOf].
func rollingWorkMinutes(entries []LedgerEntry, asOf time.Time, window time.Duration) int {
	if window <= 0 {
		return 0
	}
	from := asOf.Add(-window)
	total := 0
	for _, e := range entries {
		if e.Kind != KindWork {
			continue
		}
		total += overlapMinutes(e.Start, e.End, from, asOf)
	}
	return total
}

func overlapMinutes(start, end, from, to time.Time) int {
	if start.After(to) || !end.After(from) {
		return 0
	}
	clippedStart := start
	if clippedStart.Before(from) {
		clippedStart = from
	}
	clippedEnd := end
	if clippedEnd.After(to) {
		clippedEnd = to
	}
	if !clippedEnd.After(clippedStart) {
		return 0
	}
	return int(clippedEnd.Sub(clippedStart).Minutes())
}

// schoolWeekMinutes sums worked minutes in the trailing 7 days that overlap
// any SCHOOL entry, minute for minute.
func schoolWeekMinutes(entries []LedgerEntry, asOf time.Time) int {
	from := asOf.Add(-7 * 24 * time.Hour)
	var school []LedgerEntry
	for _, e := range entries {
		if e.Kind == KindSchool {
			school = append(school, e)
		}
	}
	total := 0
	for _, e := range entries {
		if e.Kind != KindWork {
			continue
		}
		ws, we := e.Start, e.End
		if ws.Before(from) {
			ws = from
		}
		if we.After(asOf) {
			we = asOf
		}
		if !we.After(ws) {
			continue
		}
		for _, s := range school {
			total += overlapMinutes(ws, we, s.Start, s.End)
		}
	}
	return total
}

// consecutiveDaysWorked counts calendar days, ending at asOf's UTC calendar
// day and going backward, that contain at least one KindWork minute, until
// the first day with none.
func consecutiveDaysWorked(entries []LedgerEntry, asOf time.Time) int {
	dayHasWork := func(dayStart time.Time) bool {
		dayEnd := dayStart.Add(24 * time.Hour)
		for _, e := range entries {
			if e.Kind != KindWork {
				continue
			}
			if overlapMinutes(e.Start, e.End, dayStart, dayEnd) > 0 {
				return true
			}
		}
		return false
	}
	day := time.Date(asOf.Year(), asOf.Month(), asOf.Day(), 0, 0, 0, 0, asOf.Location())
	count := 0
	for {
		if !dayHasWork(day) {
			return count
		}
		count++
		day = day.Add(-24 * time.Hour)
		if count > 3660 { // ten years is an unambiguous runaway guard, not a policy cap
			return count
		}
	}
}

// lastRestEnd returns the end of the most recent REST entry that ends at or
// before asOf.
func lastRestEnd(entries []LedgerEntry, asOf time.Time) (time.Time, bool) {
	var best time.Time
	found := false
	for _, e := range entries {
		if e.Kind != KindRest {
			continue
		}
		if e.End.After(asOf) {
			continue
		}
		if !found || e.End.After(best) {
			best = e.End
			found = true
		}
	}
	return best, found
}

// ComputeWindows reduces the ledger's entries for one tenant/worker scope
// into Windows as of the given instant. It never reads any other tenant's
// or worker's entries, even when they are present in the same ledger.
func ComputeWindows(ledger Ledger, opts WindowOptions) (Windows, error) {
	if err := opts.validate(); err != nil {
		return Windows{}, err
	}
	entries, err := ledger.forScope(opts.TenantID, opts.WorkerID)
	if err != nil {
		return Windows{}, err
	}
	w := Windows{
		Revision:               revisionOf(entries),
		AsOf:                   opts.AsOf,
		DayMinutes:             rollingWorkMinutes(entries, opts.AsOf, 24*time.Hour),
		WeekMinutes:            rollingWorkMinutes(entries, opts.AsOf, 7*24*time.Hour),
		Fortnight14DaysMinutes: rollingWorkMinutes(entries, opts.AsOf, 14*24*time.Hour),
		ReferencePeriodMinutes: rollingWorkMinutes(entries, opts.AsOf, opts.ReferencePeriod),
		ConsecutiveDaysWorked:  consecutiveDaysWorked(entries, opts.AsOf),
		SchoolWeekMinutes:      schoolWeekMinutes(entries, opts.AsOf),
	}
	if end, ok := lastRestEnd(entries, opts.AsOf); ok {
		w.LastRestEnd, w.HasLastRestEnd = end, true
	}
	return w, nil
}
