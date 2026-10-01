package timecalc

import (
	"sort"
	"time"
)

// zones resolves IANA names once per calculation.
type zones map[string]*time.Location

func (z zones) load(name string) (*time.Location, error) {
	if loc, ok := z[name]; ok {
		return loc, nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil || name == "" || name == "Local" {
		return nil, reject(ErrInvalidRequest, "zone", "%q is not an IANA zone", name)
	}
	z[name] = loc
	return loc, nil
}

// calendar holds the workday boundaries of the request as Unix seconds.
// Boundaries are local wall-clock times, so a DST workday is 23 or 25 hours
// long and worked time is always measured between instants.
type calendar struct {
	loc        *time.Location
	bounds     []int64 // len = days+1
	periodDays int
}

func newCalendar(z zones, req Request, periodDays int) (calendar, error) {
	loc, err := z.load(req.Workweek.Zone)
	if err != nil {
		return calendar{}, err
	}
	d := req.PeriodStart
	first := time.Date(d.Year, d.Month, d.Day, req.Workweek.StartHour, req.Workweek.StartMinute, 0, 0, loc)
	if first.Year() != d.Year || first.Month() != d.Month || first.Day() != d.Day {
		return calendar{}, reject(ErrInvalidRequest, "period_start", "%04d-%02d-%02d is not a date", d.Year, d.Month, d.Day)
	}
	if first.Weekday() != req.Workweek.StartWeekday {
		return calendar{}, reject(ErrInvalidRequest, "period_start", "%s is not the workweek start day %s", first.Weekday(), req.Workweek.StartWeekday)
	}
	c := calendar{loc: loc, bounds: make([]int64, req.Days+1), periodDays: periodDays}
	for i := range c.bounds {
		c.bounds[i] = time.Date(d.Year, d.Month, d.Day+i, req.Workweek.StartHour, req.Workweek.StartMinute, 0, 0, loc).Unix()
	}
	return c, nil
}

func (c calendar) days() int { return len(c.bounds) - 1 }

// dayOf returns the workday containing instant t (bounds[0] <= t < end).
func (c calendar) dayOf(t int64) int {
	return sort.Search(len(c.bounds), func(i int) bool { return c.bounds[i] > t }) - 1
}

func (c calendar) dateOf(day int) string {
	return time.Unix(c.bounds[day], 0).In(c.loc).Format("2006-01-02")
}

// segment is the part of one interval inside one workday.
type segment struct {
	iv         int
	day        int
	start, end int64
}

func (s segment) secs() Seconds { return Seconds(s.end - s.start) }

// span is a half-open instant range in Unix seconds, tagged with its source.
type span struct {
	idx        int
	start, end int64
}

// orderSpans sorts intervals chronologically and refuses overlap: two
// intervals covering the same instant would be paid twice.
func orderSpans(ivs []Interval) ([]span, error) {
	spans := make([]span, len(ivs))
	for i, iv := range ivs {
		spans[i] = span{idx: i, start: iv.Start.Unix(), end: iv.End.Unix()}
	}
	sort.Slice(spans, func(a, b int) bool {
		if spans[a].start != spans[b].start {
			return spans[a].start < spans[b].start
		}
		return ivs[spans[a].idx].ID < ivs[spans[b].idx].ID
	})
	for i := 1; i < len(spans); i++ {
		if spans[i].start < spans[i-1].end {
			return nil, reject(ErrOverlap, "intervals."+ivs[spans[i].idx].ID, "overlaps %s", ivs[spans[i-1].idx].ID)
		}
	}
	return spans, nil
}

// segments splits ordered intervals at workday boundaries.
func (c calendar) segments(ivs []Interval, spans []span) ([]segment, error) {
	lo, hi := c.bounds[0], c.bounds[len(c.bounds)-1]
	out := make([]segment, 0, len(spans)+len(spans)/2)
	for _, sp := range spans {
		if sp.start < lo || sp.end > hi {
			return nil, reject(ErrOutsidePeriod, "intervals."+ivs[sp.idx].ID, "falls outside the calculation period")
		}
		for t := sp.start; t < sp.end; {
			day := c.dayOf(t)
			end := min(sp.end, c.bounds[day+1])
			out = append(out, segment{iv: sp.idx, day: day, start: t, end: end})
			t = end
		}
	}
	return out, nil
}

func overlap(aStart, aEnd, bStart, bEnd int64) int64 {
	s, e := max(aStart, bStart), min(aEnd, bEnd)
	if e > s {
		return e - s
	}
	return 0
}

// windowOverlap returns the seconds of [s, e) that fall inside the local
// daily window w in loc.
func windowOverlap(s, e int64, loc *time.Location, w Window) Seconds {
	y, m, d := time.Unix(s, 0).In(loc).Date()
	var total int64
	for i := -1; ; i++ {
		ws := time.Date(y, m, d+i, w.StartMinute/60, w.StartMinute%60, 0, 0, loc).Unix()
		if ws >= e {
			break
		}
		endDay := d + i
		if w.EndMinute <= w.StartMinute {
			endDay++
		}
		we := time.Date(y, m, endDay, w.EndMinute/60, w.EndMinute%60, 0, 0, loc).Unix()
		total += overlap(s, e, ws, we)
	}
	return Seconds(total)
}
