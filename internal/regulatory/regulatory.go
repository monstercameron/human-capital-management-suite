package regulatory

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	ErrInvalidEvents     = errors.New("regulatory: invalid layoff events")
	ErrInvalidUnit       = errors.New("regulatory: invalid decisional unit")
	ErrObligationMissing = errors.New("regulatory: obligation is not in the pack")
	ErrDeadlineAnchor    = errors.New("regulatory: invalid deadline anchor")
)

// LayoffEvent is one separation at one site on one date.
type LayoffEvent struct {
	SiteID   string
	Date     values.LocalDate
	WorkerID string
}

func (e LayoffEvent) validate() error {
	if strings.TrimSpace(e.SiteID) == "" || strings.TrimSpace(e.WorkerID) == "" {
		return fmt.Errorf("%w: site and worker are required", ErrInvalidEvents)
	}
	if err := e.Date.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidEvents, err)
	}
	return nil
}

// WindowCount is one mini-WARN trigger's rolling result for one site: the
// events in the rolling window, whether the headcount threshold tripped, and
// the notice due date counted back from the earliest in-window layoff.
type WindowCount struct {
	ObligationID string
	WindowStart  values.LocalDate
	WindowEnd    values.LocalDate
	Count        int
	Threshold    int
	Tripped      bool
	NoticeDue    values.LocalDate
	HasNoticeDue bool
}

// AggregateLayoffWindow computes rolling site counts (regulatory.
// aggregate_layoff_window): for every mini-WARN trigger in the pack, the
// layoffs at the site inside the trigger's rolling window ending at asOf,
// whether the trigger's headcount threshold tripped, and the notice due
// date. Timing and thresholds come from the pack; the events come from the
// caller. An empty pack trigger list yields no windows, never a default.
func AggregateLayoffWindow(pack legal.RulePack, site string, events []LayoffEvent, asOf values.LocalDate) ([]WindowCount, error) {
	if strings.TrimSpace(site) == "" {
		return nil, fmt.Errorf("%w: site is required", ErrInvalidEvents)
	}
	if err := asOf.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidEvents, err)
	}
	for _, event := range events {
		if err := event.validate(); err != nil {
			return nil, err
		}
	}
	out := make([]WindowCount, 0, len(pack.MiniWARNTriggers))
	for _, trigger := range pack.MiniWARNTriggers {
		start := asOf.AddDays(-trigger.LayoffWindowDays)
		count := WindowCount{
			ObligationID: trigger.ID,
			WindowStart:  start,
			WindowEnd:    asOf,
			Threshold:    trigger.EmployeeThreshold,
		}
		var earliest values.LocalDate
		for _, event := range events {
			if event.SiteID != site {
				continue
			}
			if event.Date.Compare(start) < 0 || event.Date.Compare(asOf) > 0 {
				continue
			}
			count.Count++
			if !earliest.IsSet() || event.Date.Compare(earliest) < 0 {
				earliest = event.Date
			}
		}
		count.Tripped = count.Count >= trigger.EmployeeThreshold
		if earliest.IsSet() {
			count.NoticeDue = earliest.AddDays(-trigger.NoticeDays)
			count.HasNoticeDue = true
		}
		out = append(out, count)
	}
	return out, nil
}

// UnitMember is one worker in a decisional unit.
type UnitMember struct {
	WorkerID string
	JobTitle string
	Eligible bool
}

// DecisionalUnit is the employer-defined group a release decision covers.
type DecisionalUnit struct {
	UnitID  string
	Members []UnitMember
}

// Disclosure lists the decisional unit for a release: the eligible members
// in worker order, the pack-timed consideration due date and revocation end,
// and the digest sealing the listing.
type Disclosure struct {
	UnitID           string
	Members          []UnitMember
	ConsiderationDue values.LocalDate
	RevocationEnd    values.LocalDate
	Digest           string
}

// ReleaseDisclosure lists the decisional unit (regulatory.release_
// disclosure): the eligible members sorted by worker, with consideration and
// revocation timing resolved from the named pack notices against the anchor
// (the offer date). The consideration notice must count AFTER the anchor and
// the revocation notice must count AFTER the signing it follows; any other
// direction refuses rather than invert a protection.
func ReleaseDisclosure(pack legal.RulePack, unit DecisionalUnit, considerationNoticeID, revocationNoticeID string, anchor values.LocalDate) (Disclosure, error) {
	if strings.TrimSpace(unit.UnitID) == "" {
		return Disclosure{}, fmt.Errorf("%w: unit id is required", ErrInvalidUnit)
	}
	if err := anchor.Validate(); err != nil {
		return Disclosure{}, fmt.Errorf("%w: %v", ErrInvalidUnit, err)
	}
	consideration, err := noticeTiming(pack, considerationNoticeID)
	if err != nil {
		return Disclosure{}, err
	}
	revocation, err := noticeTiming(pack, revocationNoticeID)
	if err != nil {
		return Disclosure{}, err
	}
	if consideration.TimingDirection != "AFTER" || revocation.TimingDirection != "AFTER" {
		return Disclosure{}, fmt.Errorf("%w: consideration and revocation count after their anchors", ErrInvalidUnit)
	}
	members := make([]UnitMember, 0, len(unit.Members))
	for _, member := range unit.Members {
		if strings.TrimSpace(member.WorkerID) == "" {
			return Disclosure{}, fmt.Errorf("%w: member worker id is required", ErrInvalidUnit)
		}
		if member.Eligible {
			members = append(members, member)
		}
	}
	sort.Slice(members, func(i, j int) bool { return members[i].WorkerID < members[j].WorkerID })
	disclosure := Disclosure{
		UnitID:           unit.UnitID,
		Members:          members,
		ConsiderationDue: anchor.AddDays(consideration.TimingDays),
		RevocationEnd:    anchor.AddDays(revocation.TimingDays),
	}
	parts := []string{"regulatory.release_disclosure", pack.PackID, fmt.Sprint(pack.Version), pack.Jurisdiction.String(), unit.UnitID}
	for _, member := range members {
		parts = append(parts, member.WorkerID, member.JobTitle)
	}
	parts = append(parts, disclosure.ConsiderationDue.String(), disclosure.RevocationEnd.String())
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	disclosure.Digest = "sha256:" + hex.EncodeToString(sum[:])
	return disclosure, nil
}

func noticeTiming(pack legal.RulePack, id string) (legal.NoticeObligation, error) {
	for _, notice := range pack.Notices {
		if notice.ID == id {
			return notice, nil
		}
	}
	return legal.NoticeObligation{}, fmt.Errorf("%w: %s", ErrObligationMissing, id)
}

// StatuteRef is one pack deadline as a statute reference: the exact release
// it came from, the obligation and citation, and the resolved due date when
// the rule carries computable timing. A prose-only deadline (for example a
// final-pay "due immediately" rule) resolves to a reference with HasDue
// false and its description carried verbatim: the reference never invents a
// date the pack did not compute.
type StatuteRef struct {
	PackID       string
	PackVersion  uint32
	Jurisdiction legal.Jurisdiction
	ObligationID string
	Citation     string
	Direction    string
	Days         int
	Anchor       values.LocalDate
	Due          values.LocalDate
	HasDue       bool
	Description  string
}

// TimeExpr renders the TimeExpr consumption contract (WF-EXT-012): a stable
// expression naming the release, the obligation, the anchor and either the
// resolved due date or the prose description. TimeExpr parses this grammar;
// until it lands, the Due field is the directly usable date.
func (r StatuteRef) TimeExpr() string {
	if !r.HasDue {
		return fmt.Sprintf("statute_ref(pack=%q,version=%d,jurisdiction=%q,obligation=%q,description=%q)",
			r.PackID, r.PackVersion, r.Jurisdiction.String(), r.ObligationID, r.Description)
	}
	return fmt.Sprintf("statute_due(pack=%q,version=%d,jurisdiction=%q,obligation=%q,anchor=%q,direction=%q,days=%d,due=%q)",
		r.PackID, r.PackVersion, r.Jurisdiction.String(), r.ObligationID,
		r.Anchor.String(), r.Direction, r.Days, r.Due.String())
}

// DeadlineFor resolves one pack deadline to its statute reference against
// the anchor (the event the deadline counts from). Notice obligations
// resolve by their BEFORE/AFTER timing, mini-WARN triggers by their notice
// days before the anchor, and final-pay prose deadlines resolve as
// references with their description. An unknown obligation refuses: no rule,
// no deadline.
func DeadlineFor(pack legal.RulePack, obligationID string, anchor values.LocalDate) (StatuteRef, error) {
	if err := anchor.Validate(); err != nil {
		return StatuteRef{}, fmt.Errorf("%w: %v", ErrDeadlineAnchor, err)
	}
	base := StatuteRef{
		PackID: pack.PackID, PackVersion: pack.Version,
		Jurisdiction: pack.Jurisdiction, ObligationID: obligationID, Anchor: anchor,
	}
	for _, notice := range pack.Notices {
		if notice.ID != obligationID {
			continue
		}
		ref := base
		ref.Citation = notice.Citation.Section
		ref.Direction = notice.TimingDirection
		ref.Days = notice.TimingDays
		switch notice.TimingDirection {
		case "BEFORE":
			ref.Due = anchor.AddDays(-notice.TimingDays)
		case "AFTER":
			ref.Due = anchor.AddDays(notice.TimingDays)
		default:
			return StatuteRef{}, fmt.Errorf("%w: notice %q has direction %q", ErrObligationMissing, obligationID, notice.TimingDirection)
		}
		ref.HasDue = true
		return ref, nil
	}
	for _, trigger := range pack.MiniWARNTriggers {
		if trigger.ID != obligationID {
			continue
		}
		ref := base
		ref.Citation = trigger.Citation.Section
		ref.Direction = "BEFORE"
		ref.Days = trigger.NoticeDays
		ref.Due = anchor.AddDays(-trigger.NoticeDays)
		ref.HasDue = true
		return ref, nil
	}
	for _, deadline := range pack.FinalPayDeadlines {
		if deadline.ID != obligationID {
			continue
		}
		ref := base
		ref.Citation = deadline.Citation.Section
		ref.Description = deadline.DeadlineDescription
		return ref, nil
	}
	return StatuteRef{}, fmt.Errorf("%w: %s", ErrObligationMissing, obligationID)
}
