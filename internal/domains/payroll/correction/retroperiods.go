// WF-CAP-012: resolve which closed payroll periods a retroactive change
// affects.
//
// A back-dated change must name the periods and runs to correct before
// the payroll system recalculates. The resolver below is the NATIVE part
// of that contract; recalculation itself stays delegated. Given an
// effective-dated change, the period calendar and the bitemporal fact
// history, it returns every affected closed period with its runs and the
// fact versions effective and known in each.
package correction

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ErrRetroInvalid is returned for a malformed retro resolution request.
// It is matchable with errors.Is.
var ErrRetroInvalid = errors.New("correction: invalid retro period resolution")

// RetroChange is one back-dated fact change: effective from EffectiveDate,
// learned at KnownAt.
type RetroChange struct {
	EmployeeID    string
	FactKind      string
	EffectiveDate string
	KnownAt       time.Time
}

// PeriodRun is one immutable pay run inside a payroll period.
type PeriodRun struct {
	RunID         string
	PeriodID      string
	EffectiveDate string
	RuleVersionID string
}

// PayrollPeriod is one calendar pay period with its runs.
type PayrollPeriod struct {
	PeriodID  string
	StartDate string
	EndDate   string
	Closed    bool
	Runs      []PeriodRun
}

// FactVersion is one version of a bitemporal fact: effective over
// [EffectiveFrom, EffectiveTo), with an empty EffectiveTo meaning open,
// and learned at KnownAt.
type FactVersion struct {
	Version       string
	EffectiveFrom string
	EffectiveTo   string
	KnownAt       time.Time
	Value         string
}

// FactHistory is the versioned bitemporal fact the change touches.
type FactHistory struct {
	EmployeeID string
	FactKind   string
	Versions   []FactVersion
}

// AffectedPeriod is one closed period the change touches, with its runs
// and the fact versions effective and known in it: EffectiveFactVersion
// is the version the closed period settled under, KnownFactVersion the
// latest version known when the change was learned.
type AffectedPeriod struct {
	PeriodID             string
	StartDate            string
	EndDate              string
	Runs                 []PeriodRun
	EffectiveFactVersion string
	KnownFactVersion     string
}

func validDay(s string) bool {
	if len(s) != len("2006-01-02") {
		return false
	}
	day, err := time.Parse("2006-01-02", s)
	if err != nil {
		return false
	}
	return day.Format("2006-01-02") == s
}

// ResolveRetroPeriods returns the closed periods a back-dated change
// affects, ordered by period id: the closed periods overlapping the
// change's effective date whose runs settled under a known fact version.
// Each carries its runs ordered by run id. Malformed input fails closed.
func ResolveRetroPeriods(change RetroChange, periods []PayrollPeriod, facts FactHistory) ([]AffectedPeriod, error) {
	if strings.TrimSpace(change.EmployeeID) == "" || strings.TrimSpace(change.FactKind) == "" {
		return nil, fmt.Errorf("%w: employee and fact kind are required", ErrRetroInvalid)
	}
	if !validDay(change.EffectiveDate) {
		return nil, fmt.Errorf("%w: effective date %q is not a day", ErrRetroInvalid, change.EffectiveDate)
	}
	if change.KnownAt.IsZero() {
		return nil, fmt.Errorf("%w: known time is required", ErrRetroInvalid)
	}
	asOf := change.KnownAt.UTC().Format("2006-01-02")
	if change.EffectiveDate > asOf {
		return nil, fmt.Errorf("%w: effective date %s is after knowledge %s", ErrRetroInvalid, change.EffectiveDate, asOf)
	}
	if facts.EmployeeID != change.EmployeeID || facts.FactKind != change.FactKind {
		return nil, fmt.Errorf("%w: fact history is for another employee or kind", ErrRetroInvalid)
	}
	for _, v := range facts.Versions {
		if !validDay(v.EffectiveFrom) || (v.EffectiveTo != "" && !validDay(v.EffectiveTo)) {
			return nil, fmt.Errorf("%w: version %q has a malformed effective range", ErrRetroInvalid, v.Version)
		}
		if v.EffectiveTo != "" && v.EffectiveTo <= v.EffectiveFrom {
			return nil, fmt.Errorf("%w: version %q has an inverted effective range", ErrRetroInvalid, v.Version)
		}
		if v.KnownAt.IsZero() {
			return nil, fmt.Errorf("%w: version %q has no known time", ErrRetroInvalid, v.Version)
		}
	}
	seen := make(map[string]bool, len(periods))
	var out []AffectedPeriod
	for _, p := range periods {
		if strings.TrimSpace(p.PeriodID) == "" || seen[p.PeriodID] {
			return nil, fmt.Errorf("%w: duplicate or blank period id %q", ErrRetroInvalid, p.PeriodID)
		}
		seen[p.PeriodID] = true
		if !validDay(p.StartDate) || !validDay(p.EndDate) || p.EndDate < p.StartDate {
			return nil, fmt.Errorf("%w: period %q has a malformed range", ErrRetroInvalid, p.PeriodID)
		}
		if !p.Closed || p.EndDate < change.EffectiveDate || p.StartDate > asOf {
			continue
		}
		eff := effectiveVersion(facts.Versions, p.EndDate)
		if eff == "" {
			// The fact did not exist when this period settled; the
			// change cannot touch it.
			continue
		}
		runs := append([]PeriodRun(nil), p.Runs...)
		sort.Slice(runs, func(i, j int) bool { return runs[i].RunID < runs[j].RunID })
		out = append(out, AffectedPeriod{
			PeriodID:             p.PeriodID,
			StartDate:            p.StartDate,
			EndDate:              p.EndDate,
			Runs:                 runs,
			EffectiveFactVersion: eff,
			KnownFactVersion:     knownVersion(facts.Versions, change.KnownAt),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PeriodID < out[j].PeriodID })
	return out, nil
}

// effectiveVersion returns the version effective on day: the covering
// version with the latest effective-from. Day comparisons are string
// comparisons over validated YYYY-MM-DD days.
func effectiveVersion(versions []FactVersion, day string) string {
	best := ""
	bestFrom := ""
	for _, v := range versions {
		if v.EffectiveFrom > day || (v.EffectiveTo != "" && day >= v.EffectiveTo) {
			continue
		}
		if best == "" || v.EffectiveFrom > bestFrom {
			best, bestFrom = v.Version, v.EffectiveFrom
		}
	}
	return best
}

// knownVersion returns the latest version known at or before knownAt.
func knownVersion(versions []FactVersion, knownAt time.Time) string {
	best := ""
	var bestAt time.Time
	for _, v := range versions {
		if v.KnownAt.After(knownAt) {
			continue
		}
		if best == "" || v.KnownAt.After(bestAt) {
			best, bestAt = v.Version, v.KnownAt
		}
	}
	return best
}
