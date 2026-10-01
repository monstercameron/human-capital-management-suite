package crewshift

import (
	"fmt"
	"sort"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Status is the lifecycle state of one shift revision.
type Status string

const (
	StatusDraft     Status = "DRAFT"
	StatusPublished Status = "PUBLISHED"
	StatusCancelled Status = "CANCELLED"
)

// Valid reports whether s is one of the declared statuses.
func (s Status) Valid() bool {
	switch s {
	case StatusDraft, StatusPublished, StatusCancelled:
		return true
	}
	return false
}

// Interval is a half-open [Start, End) instant range. Every duration this
// package reports is End.Sub(Start): absolute instant arithmetic, never a
// naive difference of wall-clock hour-of-day fields. That is what keeps a
// daylight-saving transition from silently changing worked hours -- the
// transition changes the offset, not the elapsed real time between the two
// instants.
type Interval struct {
	Start time.Time
	End   time.Time
}

// Validate reports whether the interval is well formed and non-empty.
func (i Interval) Validate() error {
	if i.Start.IsZero() || i.End.IsZero() {
		return fmt.Errorf("%w: interval requires start and end", ErrInvalidShift)
	}
	if !i.End.After(i.Start) {
		return fmt.Errorf("%w: interval must be non-empty", ErrInvalidShift)
	}
	return nil
}

// Minutes returns the whole-minute elapsed instant duration.
func (i Interval) Minutes() int64 { return int64(i.End.Sub(i.Start) / time.Minute) }

// Overlaps reports whether the two half-open intervals share any instant.
func (i Interval) Overlaps(o Interval) bool {
	return i.Start.Before(o.End) && o.Start.Before(i.End)
}

// BreakKind is the closed set of planned break kinds a shift can carry.
type BreakKind string

const (
	BreakMeal BreakKind = "MEAL"
	BreakRest BreakKind = "REST"
)

// Valid reports whether k is a declared break kind.
func (k BreakKind) Valid() bool { return k == BreakMeal || k == BreakRest }

// BreakSegment is one planned break within a shift's bounds.
type BreakSegment struct {
	Kind     BreakKind
	Interval Interval
	Paid     bool
}

// Validate reports whether the segment is well formed and falls inside the
// supplied shift bounds.
func (b BreakSegment) Validate(shift Interval) error {
	if !b.Kind.Valid() {
		return fmt.Errorf("%w: break kind %q is not declared", ErrInvalidShift, b.Kind)
	}
	if err := b.Interval.Validate(); err != nil {
		return err
	}
	if b.Interval.Start.Before(shift.Start) || b.Interval.End.After(shift.End) {
		return fmt.Errorf("%w: break falls outside the shift", ErrInvalidShift)
	}
	return nil
}

// BreakPlan is the ordered, non-overlapping set of breaks planned for one
// shift.
type BreakPlan struct {
	Segments []BreakSegment
}

// Validate checks every segment against the shift bounds and against every
// other segment for overlap.
func (p BreakPlan) Validate(shift Interval) error {
	ordered := append([]BreakSegment(nil), p.Segments...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Interval.Start.Before(ordered[j].Interval.Start) })
	for i, seg := range ordered {
		if err := seg.Validate(shift); err != nil {
			return err
		}
		if i > 0 && seg.Interval.Start.Before(ordered[i-1].Interval.End) {
			return fmt.Errorf("%w: break segments overlap", ErrInvalidShift)
		}
	}
	return nil
}

// UnpaidMinutes sums every unpaid segment's elapsed minutes.
func (p BreakPlan) UnpaidMinutes() int64 {
	var total int64
	for _, seg := range p.Segments {
		if !seg.Paid {
			total += seg.Interval.Minutes()
		}
	}
	return total
}

// ProposalSource distinguishes a manually drafted shift from one an
// optimizer proposed. The distinction is informational only: both sources
// publish through the identical AuthorityPath, so recording the source can
// never widen what a proposal is allowed to do.
type ProposalSource string

const (
	SourceManual    ProposalSource = "MANUAL"
	SourceOptimizer ProposalSource = "OPTIMIZER"
)

// Valid reports whether s is a declared proposal source.
func (s ProposalSource) Valid() bool { return s == SourceManual || s == SourceOptimizer }

// Shift is one draft or published crew shift revision. A published shift's
// Revision, ApprovedBy, ApprovedAt and PublishedAt are set by Publish; a
// caller never assigns them directly.
type Shift struct {
	ID           string
	Tenant       values.TenantId
	Revision     int64
	Status       Status
	Source       ProposalSource
	WorkerRef    values.EntityRef
	RoleRef      values.EntityRef
	SiteRef      values.EntityRef
	ProjectRef   values.EntityRef
	HasWorkOrder bool
	WorkOrderRef values.EntityRef
	Timezone     string // IANA zone name; the shift's instants are planned in it
	Work         Interval
	Breaks       BreakPlan
	ApprovedBy   values.EntityRef
	ApprovedAt   time.Time
	PublishedAt  time.Time
	CreatedAt    time.Time
}

// Validate checks that the shift is internally well formed: every scoped
// reference belongs to the shift's tenant, the timezone is a real IANA zone,
// the work interval is non-empty, and the break plan fits inside it. It does
// not check publication rules -- that is Publish's job.
func (s Shift) Validate() error {
	if s.ID == "" {
		return fmt.Errorf("%w: shift id is required", ErrInvalidShift)
	}
	if err := s.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidShift, err)
	}
	if !s.Status.Valid() {
		return fmt.Errorf("%w: status %q is not declared", ErrInvalidShift, s.Status)
	}
	if !s.Source.Valid() {
		return fmt.Errorf("%w: source %q is not declared", ErrInvalidShift, s.Source)
	}
	for name, ref := range map[string]values.EntityRef{
		"worker": s.WorkerRef, "role": s.RoleRef, "site": s.SiteRef, "project": s.ProjectRef,
	} {
		if err := ref.Validate(); err != nil {
			return fmt.Errorf("%w: %s reference is invalid: %v", ErrInvalidShift, name, err)
		}
		if ref.Tenant != s.Tenant {
			return fmt.Errorf("%w: %s reference crosses tenant", ErrInvalidShift, name)
		}
	}
	if s.HasWorkOrder {
		if err := s.WorkOrderRef.Validate(); err != nil {
			return fmt.Errorf("%w: work order reference is invalid: %v", ErrInvalidShift, err)
		}
		if s.WorkOrderRef.Tenant != s.Tenant {
			return fmt.Errorf("%w: work order reference crosses tenant", ErrInvalidShift)
		}
	}
	if s.Timezone == "" {
		return fmt.Errorf("%w: timezone is required", ErrInvalidShift)
	}
	if _, err := time.LoadLocation(s.Timezone); err != nil {
		return fmt.Errorf("%w: timezone %q does not resolve: %v", ErrInvalidShift, s.Timezone, err)
	}
	if err := s.Work.Validate(); err != nil {
		return err
	}
	if err := s.Breaks.Validate(s.Work); err != nil {
		return err
	}
	if s.Status == StatusPublished {
		if s.ApprovedBy.Validate() != nil || s.ApprovedAt.IsZero() || s.PublishedAt.IsZero() {
			return fmt.Errorf("%w: a published shift requires an approver and approval/publication instants", ErrInvalidShift)
		}
	}
	return nil
}

// WorkedMinutes is the shift's paid elapsed minutes: the absolute instant
// span of the work interval, minus unpaid break minutes. Because Work.Start
// and Work.End are already-resolved instants, a shift that spans a
// daylight-saving transition reports the true elapsed time -- a spring-
// forward shift is shorter and a fall-back shift is longer, exactly as
// happened, never the naive clock-face difference.
func (s Shift) WorkedMinutes() int64 {
	total := s.Work.Minutes() - s.Breaks.UnpaidMinutes()
	if total < 0 {
		return 0
	}
	return total
}
