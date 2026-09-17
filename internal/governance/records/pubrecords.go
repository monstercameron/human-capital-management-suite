// Public-records schedule authority (PRIV-011).
//
// RECORDS-DISP-001's composed schedule gains a distinct public-records and
// government schedule-authority class alongside tax and contractual
// retention. A privacy-deletion request, a tax-retention rule and a
// public-records disposition schedule never resolve against one shared
// timer: deletion stays blocked while ANY class's hold or minimum applies,
// and the blocking authority is named in the response so a privacy deletion
// is never silently blocked and a governed record is never deleted early.
package records

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ScheduleAuthorityClass is the closed retention-authority vocabulary. Each
// class carries its own minimum and its own hold set; classes never share
// a disposition timer.
type ScheduleAuthorityClass string

const (
	AuthorityTax           ScheduleAuthorityClass = "TAX"
	AuthorityContractual   ScheduleAuthorityClass = "CONTRACTUAL"
	AuthorityPublicRecords ScheduleAuthorityClass = "PUBLIC_RECORDS"
)

// Valid reports whether c is a declared schedule-authority class.
func (c ScheduleAuthorityClass) Valid() bool {
	switch c {
	case AuthorityTax, AuthorityContractual, AuthorityPublicRecords:
		return true
	}
	return false
}

var (
	// ErrDispositionBlocked reports a deletion that cannot proceed while a
	// named schedule authority still applies. It is a verdict, not a bug.
	ErrDispositionBlocked = errors.New("records: disposition blocked by schedule authority")
	// ErrDispositionInvalid reports a malformed classified schedule.
	ErrDispositionInvalid = errors.New("records: invalid classified schedule")
)

// ClassifiedRetentionRule is one retention rule bound to its
// schedule-authority class and disposition authority (for example a
// NARA-approved disposition authority or a state public-records schedule).
type ClassifiedRetentionRule struct {
	Rule                 RetentionRule
	Class                ScheduleAuthorityClass
	DispositionAuthority string
}

func (r ClassifiedRetentionRule) validate() error {
	if err := r.Rule.validate(); err != nil {
		return err
	}
	if !r.Class.Valid() {
		return fmt.Errorf("%w: schedule-authority class %q is not declared", ErrDispositionInvalid, r.Class)
	}
	if strings.TrimSpace(r.DispositionAuthority) == "" {
		return fmt.Errorf("%w: %s rule %q names no disposition authority", ErrDispositionInvalid, r.Class, r.Rule.RecordSeries)
	}
	return nil
}

// DispositionHold is one class-scoped hold excepting a record from
// deletion. The Authority names who imposed it.
type DispositionHold struct {
	ID        string
	Class     ScheduleAuthorityClass
	Authority string
	Reason    string
}

func (h DispositionHold) validate() error {
	if strings.TrimSpace(h.ID) == "" || strings.TrimSpace(h.Authority) == "" || strings.TrimSpace(h.Reason) == "" {
		return fmt.Errorf("%w: hold is incomplete", ErrDispositionInvalid)
	}
	if !h.Class.Valid() {
		return fmt.Errorf("%w: hold class %q is not declared", ErrDispositionInvalid, h.Class)
	}
	return nil
}

// ClassMinimum is the composed minimum of one authority class with the
// disposition authority that imposed it.
type ClassMinimum struct {
	Class                ScheduleAuthorityClass
	MinimumDays          int
	DispositionAuthority string
}

// ClassifiedSchedule is the composed per-series schedule: every class's
// minimum applies independently, each naming its own authority.
type ClassifiedSchedule struct {
	RecordSeries  string
	Jurisdiction  string
	ClassMinima   []ClassMinimum
	AuthorityRefs []string
	Digest        string
}

// ComposeClassified composes classified rules into per-series schedules.
// Rules for one series and jurisdiction accumulate per class; the composed
// minimum of a class is the maximum of its rules' minima, and every
// contributing disposition authority is named.
func ComposeClassified(rules []ClassifiedRetentionRule) ([]ClassifiedSchedule, error) {
	for _, r := range rules {
		if err := r.validate(); err != nil {
			return nil, err
		}
	}
	type key struct{ series, jurisdiction string }
	groups := map[key]map[ScheduleAuthorityClass]*ClassMinimum{}
	order := []key{}
	for _, r := range rules {
		k := key{r.Rule.RecordSeries, r.Rule.Jurisdiction}
		classes, ok := groups[k]
		if !ok {
			classes = map[ScheduleAuthorityClass]*ClassMinimum{}
			groups[k] = classes
			order = append(order, k)
		}
		agg := classes[r.Class]
		if agg == nil {
			agg = &ClassMinimum{Class: r.Class}
			classes[r.Class] = agg
		}
		if r.Rule.MinimumDays > agg.MinimumDays {
			agg.MinimumDays = r.Rule.MinimumDays
		}
		if agg.DispositionAuthority == "" {
			agg.DispositionAuthority = r.DispositionAuthority
		} else if !strings.Contains(agg.DispositionAuthority, r.DispositionAuthority) {
			agg.DispositionAuthority += "+" + r.DispositionAuthority
		}
	}
	schedules := make([]ClassifiedSchedule, 0, len(order))
	for _, k := range order {
		classes := groups[k]
		minima := make([]ClassMinimum, 0, len(classes))
		refs := []string{}
		for _, agg := range classes {
			minima = append(minima, *agg)
			refs = append(refs, agg.DispositionAuthority)
		}
		sort.Slice(minima, func(i, j int) bool { return minima[i].Class < minima[j].Class })
		sort.Strings(refs)
		schedules = append(schedules, ClassifiedSchedule{
			RecordSeries: k.series, Jurisdiction: k.jurisdiction,
			ClassMinima: minima, AuthorityRefs: refs,
			Digest: classifiedDigest(k.series, k.jurisdiction, minima),
		})
	}
	sort.Slice(schedules, func(i, j int) bool {
		if schedules[i].RecordSeries != schedules[j].RecordSeries {
			return schedules[i].RecordSeries < schedules[j].RecordSeries
		}
		return schedules[i].Jurisdiction < schedules[j].Jurisdiction
	})
	return schedules, nil
}

func classifiedDigest(series, jurisdiction string, minima []ClassMinimum) string {
	parts := []string{series, jurisdiction}
	for _, m := range minima {
		parts = append(parts, fmt.Sprintf("%s=%d@%s", m.Class, m.MinimumDays, m.DispositionAuthority))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// DispositionVerdict is the blocking-aware answer. Status is ELIGIBLE only
// when every class's minimum has elapsed and no hold applies; otherwise
// every blocking authority is named.
type DispositionVerdict struct {
	Status            Status
	BlockingAuthority []string
	ScheduleDigest    string
	EligibleAt        *time.Time
}

// EvaluateDisposition resolves one record against its classified schedule.
// Cutoff is the record's cutoff instant; holds are the active class-scoped
// holds. Deletion stays blocked while any class's minimum is unmet or any
// hold applies, and the response names each blocking authority.
func EvaluateDisposition(schedule ClassifiedSchedule, cutoff time.Time, asOf time.Time, holds []DispositionHold) (DispositionVerdict, error) {
	if schedule.RecordSeries == "" {
		return DispositionVerdict{}, fmt.Errorf("%w: no classified schedule resolves this record", ErrDispositionInvalid)
	}
	if cutoff.IsZero() || asOf.IsZero() {
		return DispositionVerdict{}, fmt.Errorf("%w: cutoff and evaluation time are required", ErrDispositionInvalid)
	}
	for _, h := range holds {
		if err := h.validate(); err != nil {
			return DispositionVerdict{}, err
		}
	}
	blocked := []string{}
	latest := cutoff
	for _, m := range schedule.ClassMinima {
		eligible := cutoff.AddDate(0, 0, m.MinimumDays)
		if eligible.After(latest) {
			latest = eligible
		}
		if asOf.Before(eligible) {
			blocked = append(blocked, string(m.Class)+":"+m.DispositionAuthority)
		}
	}
	for _, h := range holds {
		blocked = append(blocked, string(h.Class)+":"+h.Authority)
	}
	sort.Strings(blocked)
	if len(blocked) > 0 {
		return DispositionVerdict{
			Status:            BlockedWithReasons,
			BlockingAuthority: blocked,
			ScheduleDigest:    schedule.Digest,
		}, fmt.Errorf("%w: %s", ErrDispositionBlocked, strings.Join(blocked, ", "))
	}
	at := latest
	return DispositionVerdict{Status: Eligible, ScheduleDigest: schedule.Digest, EligibleAt: &at}, nil
}
