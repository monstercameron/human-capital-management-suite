package main

import "time"

// chatSeedSpan is one office day inside the seeded window.
type chatSeedSpan struct{ from, to time.Time }

// chatSeedTimeline is the working time between two moments: every weekday's
// 08:00 to 18:00 in the office zone, cut to the window. The seeder places posts
// on it instead of on the calendar, so a message can only land inside office
// hours on a weekday, and moving along it never reorders two moments.
type chatSeedTimeline struct {
	spans []chatSeedSpan
	total time.Duration
}

// chatSeedIsWeekend reports a Saturday or Sunday in the office zone.
func chatSeedIsWeekend(at time.Time, office *time.Location) bool {
	day := at.In(office).Weekday()
	return day == time.Saturday || day == time.Sunday
}

// newChatSeedTimeline lays the office days of [start, end] end to end.
func newChatSeedTimeline(start, end time.Time, office *time.Location) chatSeedTimeline {
	var line chatSeedTimeline
	first := start.In(office)
	for day := time.Date(first.Year(), first.Month(), first.Day(), 0, 0, 0, 0, office); !day.After(end); day = time.Date(day.Year(), day.Month(), day.Day()+1, 0, 0, 0, 0, office) {
		if chatSeedIsWeekend(day, office) {
			continue
		}
		span := chatSeedSpan{from: day.Add(chatSeedWorkdayStart), to: day.Add(chatSeedWorkdayEnd)}
		if span.from.Before(start) {
			span.from = start
		}
		if span.to.After(end) {
			span.to = end
		}
		if !span.to.After(span.from) {
			continue
		}
		line.spans = append(line.spans, span)
		line.total += span.to.Sub(span.from)
	}
	return line
}

// Empty reports a window with no office time in it.
func (l chatSeedTimeline) Empty() bool { return len(l.spans) == 0 }

// At is the moment a fraction of the way through the working time.
func (l chatSeedTimeline) At(fraction float64) time.Time {
	if l.Empty() {
		return time.Time{}
	}
	if fraction < 0 {
		fraction = 0
	}
	remaining := time.Duration(fraction * float64(l.total))
	for _, span := range l.spans {
		length := span.to.Sub(span.from)
		if remaining < length {
			return span.from.Add(remaining).UTC()
		}
		remaining -= length
	}
	return l.spans[len(l.spans)-1].to.UTC()
}

// Advance moves forward by working time: a step that would pass 18:00 carries
// into the next office morning, and one past the end of the window stops there.
func (l chatSeedTimeline) Advance(from time.Time, by time.Duration) time.Time {
	if l.Empty() {
		return from
	}
	for _, span := range l.spans {
		if !span.to.After(from) {
			continue
		}
		position := span.from
		if from.After(position) {
			position = from
		}
		left := span.to.Sub(position)
		if by < left {
			return position.Add(by).UTC()
		}
		by -= left
	}
	return l.spans[len(l.spans)-1].to.UTC()
}

// Back moves backward by working time from the end of the window, and stops at
// its first moment.
func (l chatSeedTimeline) Back(by time.Duration) time.Time {
	if l.Empty() {
		return time.Time{}
	}
	for i := len(l.spans) - 1; i >= 0; i-- {
		span := l.spans[i]
		length := span.to.Sub(span.from)
		if by <= length {
			return span.to.Add(-by).UTC()
		}
		by -= length
	}
	return l.spans[0].from.UTC()
}
