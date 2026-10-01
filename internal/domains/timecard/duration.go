// WTIME-009: duration timesheets recorded by project, cost code, grant and
// an optional, independently-coded capitalization/research taxonomy, with
// the DCAA total-time-accounting and grant reporting profiles that govern
// government-contract and grant-funded work.
package timecard

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/labor"
)

// ErrDurationRejected is the WTIME-009 seeded-defect sentinel.
var ErrDurationRejected = errors.New("WTIME_009_REJECTED")

// ErrDCAAIncompleteDay is the DCAA total-time-accounting seeded defect: a
// day short of the standard with only WORKED entries and no declared
// leave, indirect or uncompensated time. Total time accounting means every
// hour is entered, not only the billable ones.
var ErrDCAAIncompleteDay = errors.New("timecard: DCAA day is missing non-billable hours")

// HourType is the closed vocabulary of a DCAA total-time-accounting day:
// every hour a worker spends is one of these, never left uncounted.
type HourType string

const (
	HourWorked          HourType = "WORKED"
	HourLeave           HourType = "LEAVE"
	HourIndirect        HourType = "INDIRECT"
	HourUncompensatedOT HourType = "UNCOMPENSATED_OT"
)

func (h HourType) Valid() bool {
	switch h {
	case HourWorked, HourLeave, HourIndirect, HourUncompensatedOT:
		return true
	default:
		return false
	}
}

// Taxonomy codes one duration line under up to two independent axes: ASC
// 350-40 capitalization and IRC 41 research-activity credit. Neither
// implies or excludes the other; a line may carry both, one or neither.
type Taxonomy struct {
	Capitalization   string // ASC 350-40 project code, optional
	ResearchActivity string // IRC 41 activity code, optional
}

// DurationLine is one WTIME-009 duration-timesheet entry: hours reported
// per day and project rather than clock punches.
type DurationLine struct {
	ID       string
	WorkDate time.Time
	Project  labor.Dimension
	CostCode string
	Grant    string // grant/award id, optional
	Taxonomy Taxonomy
	HourType HourType
	Minutes  int
}

func (l DurationLine) Validate() error {
	if strings.TrimSpace(l.ID) == "" {
		return fmt.Errorf("%w: line id is required", ErrDurationRejected)
	}
	if l.WorkDate.IsZero() {
		return fmt.Errorf("%w: work date is required", ErrDurationRejected)
	}
	if err := l.Project.Validate(); err != nil {
		return fmt.Errorf("%w: project: %v", ErrDurationRejected, err)
	}
	if strings.TrimSpace(l.CostCode) == "" {
		return fmt.Errorf("%w: cost code is required", ErrDurationRejected)
	}
	if !l.HourType.Valid() {
		return fmt.Errorf("%w: hour type is not declared", ErrDurationRejected)
	}
	if l.Minutes <= 0 {
		return fmt.Errorf("%w: minutes must be positive", ErrDurationRejected)
	}
	return nil
}

// EntryEvidence is when a line was actually keyed, kept distinct from the
// work date it describes so a late, back-filled entry is observable
// instead of erased.
type EntryEvidence struct {
	Line      DurationLine
	EnteredAt time.Time
}

func (e EntryEvidence) lateBy(threshold time.Duration) bool {
	if threshold <= 0 {
		return false
	}
	return e.EnteredAt.Sub(e.Line.WorkDate) > threshold
}

// DCAAProfile is the total-time-accounting profile for a government
// contractor. Every threshold is data, injected per rule pack: nothing
// here is a hard-coded per-jurisdiction constant.
type DCAAProfile struct {
	Enabled            bool
	StandardDayMinutes int
	LateEntryThreshold time.Duration
}

// DayEntry is one day's DCAA-checked duration record.
type DayEntry struct {
	WorkDate     time.Time
	Entries      []EntryEvidence
	TotalMinutes int
	LateEntries  []string // line IDs entered after the threshold
}

// RecordDCAADay validates and totals one day's duration entries. All hour
// types count toward the total, including leave, indirect and
// uncompensated overtime; a day that falls short of the standard with only
// WORKED entries is rejected rather than silently accepted as a partial,
// billable-only record.
func RecordDCAADay(profile DCAAProfile, entries []EntryEvidence) (DayEntry, error) {
	if len(entries) == 0 {
		return DayEntry{}, fmt.Errorf("%w: at least one entry is required", ErrDurationRejected)
	}
	day := entries[0].Line.WorkDate
	total := 0
	onlyWorked := true
	var late []string
	for i, e := range entries {
		if err := e.Line.Validate(); err != nil {
			return DayEntry{}, fmt.Errorf("%w: entry %d: %v", ErrDurationRejected, i, err)
		}
		if !e.Line.WorkDate.Equal(day) {
			return DayEntry{}, fmt.Errorf("%w: entry %d is not on this day", ErrDurationRejected, i)
		}
		if e.EnteredAt.IsZero() {
			return DayEntry{}, fmt.Errorf("%w: entry %d entry time is required", ErrDurationRejected, i)
		}
		total += e.Line.Minutes
		if e.Line.HourType != HourWorked {
			onlyWorked = false
		}
		if profile.Enabled && e.lateBy(profile.LateEntryThreshold) {
			late = append(late, e.Line.ID)
		}
	}
	if profile.Enabled && profile.StandardDayMinutes > 0 && total < profile.StandardDayMinutes && onlyWorked {
		return DayEntry{}, fmt.Errorf("%w: %d of %d standard minutes recorded with no leave, indirect or uncompensated entry", ErrDCAAIncompleteDay, total, profile.StandardDayMinutes)
	}
	sort.Strings(late)
	return DayEntry{WorkDate: day, Entries: append([]EntryEvidence(nil), entries...), TotalMinutes: total, LateEntries: late}, nil
}

// DurationCorrection routes a duration-line change through a reason and a
// separate supervisor approval; the original line is retained by the
// caller's own append-only history, never overwritten in place.
type DurationCorrection struct {
	Original              DurationLine
	Corrected             DurationLine
	Reason                string
	SupervisorApprovalRef string
}

func (c DurationCorrection) Validate() error {
	if err := c.Original.Validate(); err != nil {
		return fmt.Errorf("%w: original: %v", ErrDurationRejected, err)
	}
	if err := c.Corrected.Validate(); err != nil {
		return fmt.Errorf("%w: corrected: %v", ErrDurationRejected, err)
	}
	if c.Original.ID != c.Corrected.ID {
		return fmt.Errorf("%w: correction must retain the line id", ErrDurationRejected)
	}
	if strings.TrimSpace(c.Reason) == "" {
		return fmt.Errorf("%w: correction reason is required", ErrDurationRejected)
	}
	if strings.TrimSpace(c.SupervisorApprovalRef) == "" {
		return fmt.Errorf("%w: supervisor approval is required", ErrDurationRejected)
	}
	if c.Original.Minutes == c.Corrected.Minutes && c.Original.HourType == c.Corrected.HourType {
		return fmt.Errorf("%w: correction changes nothing", ErrDurationRejected)
	}
	return nil
}

// CorrectDurationLine applies a reasoned, supervisor-approved correction
// and returns the corrected line. The caller appends it, and the original,
// to an append-only correction history.
func CorrectDurationLine(c DurationCorrection) (DurationLine, error) {
	if err := c.Validate(); err != nil {
		return DurationLine{}, err
	}
	return c.Corrected, nil
}

// FloorCheckObservation is an independent physical-presence spot check for
// one day, used to reconcile declared duration entries.
type FloorCheckObservation struct {
	WorkDate        time.Time
	ObservedMinutes int
	ObserverRef     string
}

// FloorCheckFinding is the DCAA floor-check comparison for one day.
type FloorCheckFinding struct {
	WorkDate        time.Time
	DeclaredMinutes int
	ObservedMinutes int
	VarianceMinutes int
}

// FloorCheckReport compares a declared day against an independent
// observation. It never adjusts the declared record: any variance is
// reported evidence for a supervisor to resolve.
func FloorCheckReport(day DayEntry, obs FloorCheckObservation) (FloorCheckFinding, error) {
	if !day.WorkDate.Equal(obs.WorkDate) {
		return FloorCheckFinding{}, fmt.Errorf("%w: floor check day does not match the declared day", ErrDurationRejected)
	}
	if strings.TrimSpace(obs.ObserverRef) == "" {
		return FloorCheckFinding{}, fmt.Errorf("%w: floor check requires an observer", ErrDurationRejected)
	}
	if obs.ObservedMinutes < 0 {
		return FloorCheckFinding{}, fmt.Errorf("%w: observed minutes cannot be negative", ErrDurationRejected)
	}
	return FloorCheckFinding{WorkDate: day.WorkDate, DeclaredMinutes: day.TotalMinutes, ObservedMinutes: obs.ObservedMinutes, VarianceMinutes: day.TotalMinutes - obs.ObservedMinutes}, nil
}

// GrantReportKind is the 2 CFR 200.430(i) reporting choice for grant-funded
// time.
type GrantReportKind string

const (
	GrantPeriodActivityReport    GrantReportKind = "PERIOD_ACTIVITY_REPORT"
	GrantSemiAnnualCertification GrantReportKind = "SEMIANNUAL_CERTIFICATION"
)

// RequiredGrantReport decides which 2 CFR 200.430(i) report a period's
// entries support. Semi-annual certification is only available when every
// entry in the period charges the same single award; any split across
// awards falls back to a period activity report.
func RequiredGrantReport(entries []DurationLine) (GrantReportKind, error) {
	if len(entries) == 0 {
		return "", fmt.Errorf("%w: at least one entry is required", ErrDurationRejected)
	}
	award := ""
	sole := true
	for i, e := range entries {
		if err := e.Validate(); err != nil {
			return "", fmt.Errorf("%w: entry %d: %v", ErrDurationRejected, i, err)
		}
		if strings.TrimSpace(e.Grant) == "" {
			return "", fmt.Errorf("%w: entry %d has no grant or award", ErrDurationRejected, i)
		}
		if award == "" {
			award = e.Grant
		} else if award != e.Grant {
			sole = false
		}
	}
	if sole {
		return GrantSemiAnnualCertification, nil
	}
	return GrantPeriodActivityReport, nil
}

// ValidateAuthorizedCoding is the WTIME-009 security boundary: an entry can
// only code hours to a grant or project the caller declares authorized for
// this worker. An entry outside the set is rejected rather than silently
// costed to it.
func ValidateAuthorizedCoding(entries []DurationLine, authorizedGrants, authorizedProjects map[string]bool) error {
	for i, e := range entries {
		if e.Grant != "" && !authorizedGrants[e.Grant] {
			return fmt.Errorf("%w: entry %d codes an unauthorized grant %q", ErrDurationRejected, i, e.Grant)
		}
		if !authorizedProjects[e.Project.Value] {
			return fmt.Errorf("%w: entry %d codes an unauthorized project %q", ErrDurationRejected, i, e.Project.Value)
		}
	}
	return nil
}

// Digest returns a stable digest of a day's entries, for GOLDEN pinning.
func (d DayEntry) Digest() string {
	entries := append([]EntryEvidence(nil), d.Entries...)
	sort.Slice(entries, func(i, j int) bool { return entries[i].Line.ID < entries[j].Line.ID })
	var b strings.Builder
	fmt.Fprintf(&b, "%s|%d|", d.WorkDate.UTC().Format(time.RFC3339), d.TotalMinutes)
	for _, e := range entries {
		fmt.Fprintf(&b, "%s:%s:%s:%d;", e.Line.ID, e.Line.HourType, e.Line.CostCode, e.Line.Minutes)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return "sha256:" + hex.EncodeToString(sum[:])
}
