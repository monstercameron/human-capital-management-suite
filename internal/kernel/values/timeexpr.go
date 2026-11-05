package values

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// Time-expression errors. All are matchable with errors.Is.
var (
	ErrTimeExprInvalid  = errors.New("values: invalid time expression")
	ErrTimeExprAnchor   = errors.New("values: time expression anchor is unset")
	ErrTimeExprCalendar = errors.New("values: time expression calendar dataset mismatch")
)

// TimeExprOffsetKind names how a TimeExpr offsets its anchor.
type TimeExprOffsetKind string

// The declared offset kinds.
const (
	TimeExprOffsetUnspecified  TimeExprOffsetKind = ""
	TimeExprOffsetBusinessDays TimeExprOffsetKind = "BUSINESS_DAYS"
	TimeExprOffsetCalendarDays TimeExprOffsetKind = "CALENDAR_DAYS"
	TimeExprOffsetDuration     TimeExprOffsetKind = "DURATION"
)

// Valid reports whether k names a declared offset kind.
func (k TimeExprOffsetKind) Valid() bool {
	switch k {
	case TimeExprOffsetBusinessDays, TimeExprOffsetCalendarDays, TimeExprOffsetDuration:
		return true
	default:
		return false
	}
}

// ParseTimeExprOffsetKind decodes a wire token. The empty token is rejected.
func ParseTimeExprOffsetKind(token string) (TimeExprOffsetKind, error) {
	k := TimeExprOffsetKind(token)
	if !k.Valid() {
		return TimeExprOffsetUnspecified, fmt.Errorf("%w: offset kind %q", ErrTimeExprInvalid, token)
	}
	return k, nil
}

// StatuteRef is an opaque rule-pack statute reference carried on a TimeExpr.
// Resolution records it in evidence; it never interprets law. Release may be
// empty when the pack is unversioned.
type StatuteRef struct {
	RulePack string
	Release  string
	Statute  string
}

// Validate reports whether the reference names a pack and a statute.
func (s StatuteRef) Validate() error {
	if s.RulePack == "" || s.Statute == "" {
		return fmt.Errorf("%w: statute reference names no rule pack and statute", ErrTimeExprInvalid)
	}
	return nil
}

// String returns "pack@release:statute", omitting "@release" when unversioned.
func (s StatuteRef) String() string {
	if s.Release == "" {
		return s.RulePack + ":" + s.Statute
	}
	return s.RulePack + "@" + s.Release + ":" + s.Statute
}

// TimeExpr is one deadline expression relative to run data: an anchor mapping
// the runtime evaluates to an instant, an offset in business days, calendar
// days or seconds, the pinned calendar dataset and zone day-based offsets
// resolve against, and an optional rule-pack statute reference recording the
// authority behind the deadline. WAIT, TASK, APPROVAL and SIGNAL all accept
// this one shape so timers and SLA escalation share its resolver.
type TimeExpr struct {
	// AnchorPath is the run-data field path the runtime evaluates to the
	// anchor instant (for example "run.start_date"). Resolution takes the
	// evaluated instant; it never reads run data itself.
	AnchorPath string
	// OffsetKind selects which offset field applies.
	OffsetKind TimeExprOffsetKind
	// OffsetDays applies to BUSINESS_DAYS and CALENDAR_DAYS. It counts whole
	// days exclusive of the anchor date and may be negative.
	OffsetDays int
	// OffsetSeconds applies to DURATION and must be non-zero.
	OffsetSeconds int64
	// Calendar pins the business-calendar dataset BUSINESS_DAYS resolves
	// against. It is required for day-based offsets.
	Calendar CalendarRef
	// Zone is the zone day-based offsets convert through. It is required
	// for day-based offsets.
	Zone ZoneRef
	// WakeLocalTime, when set, is the wall-clock time of the resolved date;
	// when unset the deadline is the start of the resolved date.
	WakeLocalTime LocalTime
	// Disambiguation is the DST gap/fold policy for the resolved date. It
	// is required for day-based offsets.
	Disambiguation Disambiguation
	// Statute, when set, records the rule-pack authority behind the
	// deadline. It is evidence, never an input to the arithmetic.
	Statute *StatuteRef
}

// Validate reports whether the expression is complete and unambiguous.
func (e TimeExpr) Validate() error {
	if e.AnchorPath == "" {
		return fmt.Errorf("%w: anchor path is empty", ErrTimeExprInvalid)
	}
	switch e.OffsetKind {
	case TimeExprOffsetBusinessDays, TimeExprOffsetCalendarDays:
		if err := e.Calendar.Validate(); err != nil {
			return fmt.Errorf("%w: %v", ErrTimeExprInvalid, err)
		}
		if err := e.Zone.Validate(); err != nil {
			return fmt.Errorf("%w: %v", ErrTimeExprInvalid, err)
		}
		if !e.Disambiguation.Valid() {
			return fmt.Errorf("%w: day-based offset declares no disambiguation policy", ErrTimeExprInvalid)
		}
	case TimeExprOffsetDuration:
		if e.OffsetSeconds == 0 {
			return fmt.Errorf("%w: duration offset is zero", ErrTimeExprInvalid)
		}
	default:
		return fmt.Errorf("%w: offset kind %q", ErrTimeExprInvalid, string(e.OffsetKind))
	}
	if e.Statute != nil {
		if err := e.Statute.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// CalendarDataset answers working-day questions for one pinned calendar
// version. The runtime supplies it; AddBusinessDays and ResolveTimeExpr
// refuse a dataset whose version is not the expression's pin.
type CalendarDataset interface {
	CalendarVersion() string
	IsWorkingDay(LocalDate) (bool, error)
}

// AddBusinessDays moves date by n working days exclusive of date itself: the
// result of +1 is the next working day strictly after date. A zero offset
// returns date unchanged. Every day the walk lands on must answer
// IsWorkingDay; the first dataset error aborts the walk.
func AddBusinessDays(date LocalDate, n int, cal CalendarDataset) (LocalDate, error) {
	if cal == nil {
		return LocalDate{}, fmt.Errorf("%w: business-day offset without a calendar dataset", ErrTimeExprCalendar)
	}
	if n == 0 {
		return date, nil
	}
	step := 1
	if n < 0 {
		step = -1
		n = -n
	}
	got := date
	for counted := 0; counted < n; {
		got = got.AddDays(step)
		working, err := cal.IsWorkingDay(got)
		if err != nil {
			return LocalDate{}, fmt.Errorf("%w: %v", ErrTimeExprCalendar, err)
		}
		if working {
			counted++
		}
	}
	return got, nil
}

// TimeExprEvidence records how a TimeExprResolution's instant was derived.
// Inputs is a flat string map; Steps is an ordered narrative.
type TimeExprEvidence struct {
	Inputs map[string]string `json:"inputs,omitempty"`
	Steps  []string          `json:"steps,omitempty"`
}

// TimeExprResolution is a resolved deadline: the fire-at instant, the local
// date day-based offsets landed on (unset for DURATION), and the evidence.
type TimeExprResolution struct {
	FireAt       Instant
	ResolvedDate LocalDate
	Evidence     TimeExprEvidence
}

// ResolveTimeExpr resolves expr against an already-evaluated anchor instant.
// The caller supplies the anchor (evaluating AnchorPath against run data is
// the runtime's job) and the pinned calendar dataset. A dataset whose
// version is not the expression's pin, a version-skewed DatasetVersions, or
// an unset anchor is refused outright: a deadline never resolves by guess.
func ResolveTimeExpr(expr TimeExpr, anchor Instant, cals CalendarDataset, dataset DatasetVersions) (TimeExprResolution, error) {
	if err := expr.Validate(); err != nil {
		return TimeExprResolution{}, err
	}
	if !anchor.IsSet() {
		return TimeExprResolution{}, fmt.Errorf("%w: anchor %q evaluated to nothing", ErrTimeExprAnchor, expr.AnchorPath)
	}
	if err := dataset.Validate(); err != nil {
		return TimeExprResolution{}, err
	}
	evidence := TimeExprEvidence{Inputs: map[string]string{
		"anchor_path": expr.AnchorPath,
		"anchor":      anchor.String(),
		"offset_kind": string(expr.OffsetKind),
	}}
	if expr.Statute != nil {
		evidence.Inputs["statute"] = expr.Statute.String()
	}
	if expr.OffsetKind == TimeExprOffsetDuration {
		fireAt := NewInstant(anchor.Time().Add(time.Duration(expr.OffsetSeconds) * time.Second))
		evidence.Inputs["offset_seconds"] = fmt.Sprintf("%d", expr.OffsetSeconds)
		evidence.Inputs["fire_at"] = fireAt.String()
		evidence.Steps = append(evidence.Steps, "duration offset applies directly to the anchor instant; no calendar or zone is consulted")
		return TimeExprResolution{FireAt: fireAt, Evidence: evidence}, nil
	}
	if cals == nil {
		return TimeExprResolution{}, fmt.Errorf("%w: day-based offset without a calendar dataset", ErrTimeExprCalendar)
	}
	if cals.CalendarVersion() != expr.Calendar.Version {
		return TimeExprResolution{}, fmt.Errorf("%w: dataset is %s, expression pins %s",
			ErrTimeExprCalendar, cals.CalendarVersion(), expr.Calendar.Version)
	}
	if dataset.CalendarVersion != expr.Calendar.Version {
		return TimeExprResolution{}, fmt.Errorf("%w: versions in force are %s, expression pins %s",
			ErrTimeExprCalendar, dataset.CalendarVersion, expr.Calendar.Version)
	}
	if dataset.TzdbVersion != expr.Zone.TzdbVersion {
		return TimeExprResolution{}, fmt.Errorf("%w: tzdb in force is %s, expression pins %s",
			ErrTimeExprCalendar, dataset.TzdbVersion, expr.Zone.TzdbVersion)
	}
	loc, err := expr.Zone.Location()
	if err != nil {
		return TimeExprResolution{}, err
	}
	inZone := anchor.Time().In(loc)
	anchorDate, err := NewLocalDate(inZone.Date())
	if err != nil {
		return TimeExprResolution{}, err
	}
	evidence.Inputs["zone"] = expr.Zone.String()
	evidence.Inputs["calendar"] = expr.Calendar.String()
	evidence.Inputs["anchor_date"] = anchorDate.String()
	evidence.Inputs["offset_days"] = fmt.Sprintf("%d", expr.OffsetDays)
	var resolved LocalDate
	switch expr.OffsetKind {
	case TimeExprOffsetBusinessDays:
		resolved, err = AddBusinessDays(anchorDate, expr.OffsetDays, cals)
		if err != nil {
			return TimeExprResolution{}, err
		}
		evidence.Steps = append(evidence.Steps, fmt.Sprintf("added %d business days exclusive of the anchor date", expr.OffsetDays))
	case TimeExprOffsetCalendarDays:
		resolved = anchorDate.AddDays(expr.OffsetDays)
		evidence.Steps = append(evidence.Steps, fmt.Sprintf("added %d calendar days to the anchor date", expr.OffsetDays))
	}
	evidence.Inputs["resolved_date"] = resolved.String()
	var zoned ZonedDateTime
	if expr.WakeLocalTime.IsSet() {
		evidence.Inputs["local_time"] = expr.WakeLocalTime.String()
		zoned, err = NewZonedDateTime(resolved, expr.WakeLocalTime, expr.Zone, expr.Disambiguation)
	} else {
		zoned, err = resolved.AtStartOfDay(expr.Zone, expr.Disambiguation)
	}
	if err != nil {
		return TimeExprResolution{}, err
	}
	evidence.Inputs["disambiguation"] = expr.Disambiguation.String()
	evidence.Inputs["fire_at"] = zoned.Instant().String()
	evidence.Steps = append(evidence.Steps, fmt.Sprintf("resolved %s in %s", resolved, expr.Zone))
	return TimeExprResolution{FireAt: zoned.Instant(), ResolvedDate: resolved, Evidence: evidence}, nil
}

// Resolver adapts the expression to the one TimerResolver timers and SLA
// escalation share: anchor and calendar dataset close over, DatasetVersions
// arrives per call so PIN replays and RECALCULATE resolutions use the same
// function with different versions in force. The versions in force are
// substituted for the expression pin before resolving (mirroring how a WAIT
// node re-resolves a wall-clock condition), so the caller must supply a
// calendar dataset matching the versions it passes; anything else is still
// refused rather than guessed.
func (e TimeExpr) Resolver(anchor Instant, cals CalendarDataset) TimerResolver {
	return func(dataset DatasetVersions) (Instant, error) {
		sub := e
		sub.Zone.TzdbVersion = dataset.TzdbVersion
		sub.Calendar.Version = dataset.CalendarVersion
		res, err := ResolveTimeExpr(sub, anchor, cals, dataset)
		if err != nil {
			return Instant{}, err
		}
		return res.FireAt, nil
	}
}

// deadlineDefaultProfile prefixes every DeadlineDefault digest.
const deadlineDefaultProfile = "hcmnext.values.DeadlineDefault/v1"

// DeadlineDefault is a declared non-response payload: the schema it conforms
// to and its canonical JSON bytes. A node carrying one completes its
// deadline outcome route (TASK/APPROVAL EXPIRED, SIGNAL TIMED_OUT) with this
// payload; the route's output digest is Digest, never a guess.
type DeadlineDefault struct {
	Schema        string
	CanonicalJSON string
}

// Validate reports whether the default names a schema and carries bytes.
func (d DeadlineDefault) Validate() error {
	if d.Schema == "" || d.CanonicalJSON == "" {
		return fmt.Errorf("%w: deadline default names no schema and payload", ErrTimeExprInvalid)
	}
	return nil
}

// Digest returns the profile-prefixed sha256 hex over schema and payload.
func (d DeadlineDefault) Digest() string {
	h := sha256.New()
	h.Write([]byte(deadlineDefaultProfile))
	h.Write([]byte{0})
	h.Write([]byte(d.Schema))
	h.Write([]byte{0})
	h.Write([]byte(d.CanonicalJSON))
	return deadlineDefaultProfile + ":" + hex.EncodeToString(h.Sum(nil))
}
