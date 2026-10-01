// awr.go implements the UK Agency Workers Regulations 2010 qualifying-week
// counter that WTIME-012 needs and WTIME-007 promises to serve as a window:
// weeks worked with one hirer in the same role, where a break of six weeks
// or less does not reset the count, and parity pay is due once the count
// reaches twelve.
package worktimerules

import (
	"fmt"
	"sort"
	"time"
)

// AWRBreakTolerance is the maximum gap between qualifying weeks that does
// not reset the counter under regulation 7. It is exposed as a var, not a
// const, so a caller building a rule set can override it explicitly, but
// this package never applies a different value on its own.
const AWRBreakTolerance = 6 * 7 * 24 * time.Hour

// AWRWeeksWithHirer counts the qualifying weeks a worker has worked for one
// hirer as of asOf, using only KindWork entries scoped to hirerID within
// the given tenant/worker. A qualifying week is any ISO week (Mon 00:00 to
// the following Mon 00:00, in UTC) containing at least one minute of work
// for that hirer. Consecutive qualifying weeks keep counting across a gap
// of breakTolerance or less; a longer gap resets the count to the weeks
// worked after the gap.
func AWRWeeksWithHirer(ledger Ledger, tenantID, workerID, hirerID string, asOf time.Time, breakTolerance time.Duration) (weeks int, parityDue bool, err error) {
	if hirerID == "" {
		return 0, false, fmt.Errorf("%w: hirer_id is required", ErrMissingScope)
	}
	entries, err := ledger.forScope(tenantID, workerID)
	if err != nil {
		return 0, false, err
	}
	var forHirer []LedgerEntry
	for _, e := range entries {
		if e.Kind == KindWork && e.HirerID == hirerID && !e.Start.After(asOf) {
			forHirer = append(forHirer, e)
		}
	}
	if len(forHirer) == 0 {
		return 0, false, nil
	}

	weekStart := func(t time.Time) time.Time {
		t = t.UTC()
		// ISO week starts Monday; Go's Weekday has Sunday=0.
		offset := (int(t.Weekday()) + 6) % 7
		d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -offset)
		return d
	}

	qualifying := map[int64]bool{}
	for _, e := range forHirer {
		start := weekStart(e.Start)
		end := weekStart(e.End.Add(-time.Nanosecond))
		for w := start; !w.After(end); w = w.AddDate(0, 0, 7) {
			qualifying[w.Unix()] = true
		}
	}
	weekKeys := make([]int64, 0, len(qualifying))
	for k := range qualifying {
		weekKeys = append(weekKeys, k)
	}
	sort.Slice(weekKeys, func(i, j int) bool { return weekKeys[i] < weekKeys[j] })

	if breakTolerance <= 0 {
		breakTolerance = AWRBreakTolerance
	}
	count := 0
	var prev time.Time
	for i, k := range weekKeys {
		wk := time.Unix(k, 0).UTC()
		if i == 0 {
			count = 1
			prev = wk
			continue
		}
		gap := wk.Sub(prev.AddDate(0, 0, 7))
		if gap <= breakTolerance {
			count++
		} else {
			count = 1
		}
		prev = wk
	}
	return count, count >= 12, nil
}
