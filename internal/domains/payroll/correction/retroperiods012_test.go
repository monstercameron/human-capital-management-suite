// WF-CAP-012 RED: corrections operate on one run; nothing enumerates
// the periods an effective-dated change touches.
package correction_test

import (
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/payroll/correction"
)

func cap012Change() correction.RetroChange {
	return correction.RetroChange{
		EmployeeID:    "e-42",
		FactKind:      "hourly_rate",
		EffectiveDate: "2026-02-01",
		KnownAt:       time.Date(2026, 3, 20, 9, 0, 0, 0, time.UTC),
	}
}

func cap012Periods() []correction.PayrollPeriod {
	run := func(id, period, date string) correction.PeriodRun {
		return correction.PeriodRun{RunID: id, PeriodID: period, EffectiveDate: date, RuleVersionID: "rv1"}
	}
	return []correction.PayrollPeriod{
		{PeriodID: "2026-01", StartDate: "2026-01-01", EndDate: "2026-01-31", Closed: true,
			Runs: []correction.PeriodRun{run("r1", "2026-01", "2026-01-31")}},
		{PeriodID: "2026-02", StartDate: "2026-02-01", EndDate: "2026-02-28", Closed: true,
			Runs: []correction.PeriodRun{run("r2", "2026-02", "2026-02-28")}},
		{PeriodID: "2026-03", StartDate: "2026-03-01", EndDate: "2026-03-31", Closed: true,
			Runs: []correction.PeriodRun{run("r3a", "2026-03", "2026-03-15"), run("r3b", "2026-03", "2026-03-31")}},
		{PeriodID: "2026-04", StartDate: "2026-04-01", EndDate: "2026-04-30", Closed: false,
			Runs: []correction.PeriodRun{run("r4", "2026-04", "2026-04-15")}},
	}
}

func cap012Facts() correction.FactHistory {
	return correction.FactHistory{
		EmployeeID: "e-42",
		FactKind:   "hourly_rate",
		Versions: []correction.FactVersion{
			{Version: "v1", EffectiveFrom: "2026-01-01", EffectiveTo: "2026-03-10",
				KnownAt: time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC), Value: "25.00"},
			{Version: "v2", EffectiveFrom: "2026-03-10", EffectiveTo: "",
				KnownAt: time.Date(2026, 3, 11, 9, 0, 0, 0, time.UTC), Value: "27.50"},
		},
	}
}

// TestTodo_WF_CAP_012 is the PRIMARY test: payroll.resolve_retro_periods
// returns the affected periods and runs with the fact versions effective
// and known in each.
func TestTodo_WF_CAP_012(t *testing.T) {
	t.Run("names every closed period the change touches", func(t *testing.T) {
		got, err := correction.ResolveRetroPeriods(cap012Change(), cap012Periods(), cap012Facts())
		if err != nil {
			t.Fatalf("ResolveRetroPeriods: %v", err)
		}
		if len(got) != 2 || got[0].PeriodID != "2026-02" || got[1].PeriodID != "2026-03" {
			t.Fatalf("affected = %v, want [2026-02 2026-03]", periodIDs(got))
		}
		if len(got[0].Runs) != 1 || got[0].Runs[0].RunID != "r2" {
			t.Fatalf("2026-02 runs = %+v, want [r2]", got[0].Runs)
		}
		if len(got[1].Runs) != 2 || got[1].Runs[0].RunID != "r3a" || got[1].Runs[1].RunID != "r3b" {
			t.Fatalf("2026-03 runs = %+v, want [r3a r3b]", got[1].Runs)
		}
	})

	t.Run("carries the fact versions effective and known in each", func(t *testing.T) {
		got, err := correction.ResolveRetroPeriods(cap012Change(), cap012Periods(), cap012Facts())
		if err != nil {
			t.Fatalf("ResolveRetroPeriods: %v", err)
		}
		if got[0].EffectiveFactVersion != "v1" || got[0].KnownFactVersion != "v2" {
			t.Fatalf("2026-02 versions = %s/%s, want v1/v2",
				got[0].EffectiveFactVersion, got[0].KnownFactVersion)
		}
		if got[1].EffectiveFactVersion != "v2" || got[1].KnownFactVersion != "v2" {
			t.Fatalf("2026-03 versions = %s/%s, want v2/v2",
				got[1].EffectiveFactVersion, got[1].KnownFactVersion)
		}
	})

	t.Run("open periods and periods before the change are untouched", func(t *testing.T) {
		change := cap012Change()
		change.EffectiveDate = "2026-04-01"
		change.KnownAt = time.Date(2026, 4, 10, 9, 0, 0, 0, time.UTC)
		got, err := correction.ResolveRetroPeriods(change, cap012Periods(), cap012Facts())
		if err != nil {
			t.Fatalf("ResolveRetroPeriods: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("affected = %v, want none (only the open period overlaps)", periodIDs(got))
		}
	})

	t.Run("a period with no effective fact version is skipped", func(t *testing.T) {
		facts := correction.FactHistory{EmployeeID: "e-42", FactKind: "hourly_rate",
			Versions: []correction.FactVersion{
				{Version: "v1", EffectiveFrom: "2026-03-01", EffectiveTo: "",
					KnownAt: time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC), Value: "25.00"},
			}}
		got, err := correction.ResolveRetroPeriods(cap012Change(), cap012Periods(), facts)
		if err != nil {
			t.Fatalf("ResolveRetroPeriods: %v", err)
		}
		if len(got) != 1 || got[0].PeriodID != "2026-03" {
			t.Fatalf("affected = %v, want [2026-03]", periodIDs(got))
		}
	})

	t.Run("malformed input fails closed", func(t *testing.T) {
		bad := cap012Change()
		bad.EffectiveDate = "02/01/2026"
		if _, err := correction.ResolveRetroPeriods(bad, cap012Periods(), cap012Facts()); !errors.Is(err, correction.ErrRetroInvalid) {
			t.Fatalf("bad date err = %v, want ErrRetroInvalid", err)
		}
		future := cap012Change()
		future.EffectiveDate = "2026-05-01"
		if _, err := correction.ResolveRetroPeriods(future, cap012Periods(), cap012Facts()); !errors.Is(err, correction.ErrRetroInvalid) {
			t.Fatalf("future change err = %v, want ErrRetroInvalid", err)
		}
		periods := cap012Periods()
		periods[1].EndDate = "2026-01-01"
		if _, err := correction.ResolveRetroPeriods(cap012Change(), periods, cap012Facts()); !errors.Is(err, correction.ErrRetroInvalid) {
			t.Fatalf("inverted period err = %v, want ErrRetroInvalid", err)
		}
	})
}

func periodIDs(periods []correction.AffectedPeriod) []string {
	var out []string
	for _, p := range periods {
		out = append(out, p.PeriodID)
	}
	return out
}

// TestTodo_WF_CAP_012_Property proves no affected period is omitted: the
// resolver agrees exactly with an independent naive oracle on generated
// calendars, histories and changes.
func TestTodo_WF_CAP_012_Property(t *testing.T) {
	for seed := int64(1); seed <= 200; seed++ {
		rng := rand.New(rand.NewSource(seed)) //nolint:gosec // deterministic property seed
		change, periods, facts := cap012Generate(rng)
		got, err := correction.ResolveRetroPeriods(change, periods, facts)
		if err != nil {
			t.Fatalf("seed %d: ResolveRetroPeriods: %v", seed, err)
		}
		want := cap012Oracle(change, periods, facts)
		if len(got) != len(want) {
			t.Fatalf("seed %d: affected %v, oracle %v", seed, periodIDs(got), periodIDs(want))
		}
		for i := range want {
			if !reflect.DeepEqual(got[i], want[i]) {
				t.Fatalf("seed %d: period %d = %+v, oracle %+v", seed, i, got[i], want[i])
			}
		}
		if !sort.SliceIsSorted(periodIDs(got), func(i, j int) bool {
			return periodIDs(got)[i] < periodIDs(got)[j]
		}) {
			t.Fatalf("seed %d: output not ordered: %v", seed, periodIDs(got))
		}
	}
}

func cap012Generate(rng *rand.Rand) (correction.RetroChange, []correction.PayrollPeriod, correction.FactHistory) {
	// Calendar: 1..6 consecutive months of 2026 starting at a random
	// month, each randomly closed, each with 0..3 runs.
	startMonth := 1 + rng.Intn(6)
	nPeriods := 1 + rng.Intn(6)
	var periods []correction.PayrollPeriod
	monthEnd := map[int]string{1: "31", 2: "28", 3: "31", 4: "30", 5: "31", 6: "30", 7: "31", 8: "31", 9: "30", 10: "31", 11: "30", 12: "31"}
	for i := 0; i < nPeriods; i++ {
		m := startMonth + i
		if m > 12 {
			break
		}
		id := fmt.Sprintf("2026-%02d", m)
		var runs []correction.PeriodRun
		for r := 0; r < rng.Intn(4); r++ {
			runs = append(runs, correction.PeriodRun{
				RunID:         fmt.Sprintf("r-%s-%d", id, r),
				PeriodID:      id,
				EffectiveDate: fmt.Sprintf("%s-%s", id, monthEnd[m]),
				RuleVersionID: "rv1",
			})
		}
		periods = append(periods, correction.PayrollPeriod{
			PeriodID:  id,
			StartDate: id + "-01",
			EndDate:   id + "-" + monthEnd[m],
			Closed:    rng.Intn(2) == 0,
			Runs:      runs,
		})
	}
	// Facts: 1..3 contiguous versions from January, known the next day.
	nFacts := 1 + rng.Intn(3)
	var versions []correction.FactVersion
	for v := 0; v < nFacts; v++ {
		from := fmt.Sprintf("2026-%02d-01", 1+v*2)
		to := ""
		if v+1 < nFacts {
			to = fmt.Sprintf("2026-%02d-01", 1+(v+1)*2)
		}
		versions = append(versions, correction.FactVersion{
			Version:       fmt.Sprintf("v%d", v+1),
			EffectiveFrom: from,
			EffectiveTo:   to,
			KnownAt:       time.Date(2026, time.Month(1+v*2), 2, 9, 0, 0, 0, time.UTC),
			Value:         fmt.Sprintf("%d.00", 20+v),
		})
	}
	// Change: effective on the first of a random month at or before the
	// last generated period, known mid June (after every version).
	effMonth := 1 + rng.Intn(startMonth+nFacts)
	if effMonth > 6 {
		effMonth = 6
	}
	change := correction.RetroChange{
		EmployeeID:    "e-42",
		FactKind:      "hourly_rate",
		EffectiveDate: fmt.Sprintf("2026-%02d-01", effMonth),
		KnownAt:       time.Date(2026, 6, 15, 9, 0, 0, 0, time.UTC),
	}
	return change, periods, correction.FactHistory{EmployeeID: "e-42", FactKind: "hourly_rate", Versions: versions}
}

// cap012Oracle is the independent naive restatement: a closed period
// overlapping [effective, known-as-of] that settles under a known fact
// version is affected.
func cap012Oracle(change correction.RetroChange, periods []correction.PayrollPeriod, facts correction.FactHistory) []correction.AffectedPeriod {
	asOf := change.KnownAt.UTC().Format("2006-01-02")
	var out []correction.AffectedPeriod
	for _, p := range periods {
		if !p.Closed || p.EndDate < change.EffectiveDate || p.StartDate > asOf {
			continue
		}
		eff := ""
		for _, v := range facts.Versions {
			if v.EffectiveFrom <= p.EndDate && (v.EffectiveTo == "" || p.EndDate < v.EffectiveTo) {
				if eff == "" || v.EffectiveFrom > effFrom(facts, eff) {
					eff = v.Version
				}
			}
		}
		if eff == "" {
			continue
		}
		known := ""
		var knownAt time.Time
		for _, v := range facts.Versions {
			if !v.KnownAt.After(change.KnownAt) && (known == "" || v.KnownAt.After(knownAt)) {
				known, knownAt = v.Version, v.KnownAt
			}
		}
		runs := append([]correction.PeriodRun(nil), p.Runs...)
		sort.Slice(runs, func(i, j int) bool { return runs[i].RunID < runs[j].RunID })
		out = append(out, correction.AffectedPeriod{
			PeriodID:             p.PeriodID,
			StartDate:            p.StartDate,
			EndDate:              p.EndDate,
			Runs:                 runs,
			EffectiveFactVersion: eff,
			KnownFactVersion:     known,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PeriodID < out[j].PeriodID })
	return out
}

func effFrom(facts correction.FactHistory, version string) string {
	for _, v := range facts.Versions {
		if v.Version == version {
			return v.EffectiveFrom
		}
	}
	return ""
}
