package docsdiagram

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type ganttTask struct {
	name, id   string
	section    int
	done       bool
	active     bool
	crit       bool
	milestone  bool
	start, end time.Time
	resolved   bool
	startSpec  string
	endSpec    string
	prev       int // previous task index for implicit starts, -1 for none
	line       int
}

type gantt struct {
	title        string
	dateLayout   string
	hasTime      bool
	axisFormat   string
	tickInterval string
	weekends     bool
	sections     []string
	tasks        []*ganttTask
	accTitle     string
	accDescr     string
}

// dayjsLayout converts a Mermaid (dayjs) date format to a Go layout.
func dayjsLayout(format string) (string, bool, bool) {
	tokens := []struct{ from, to string }{
		{"YYYY", "2006"}, {"YY", "06"}, {"MMMM", "January"}, {"MMM", "Jan"}, {"MM", "01"}, {"M", "1"},
		{"DD", "02"}, {"D", "2"}, {"HH", "15"}, {"H", "15"}, {"hh", "03"}, {"h", "3"}, {"mm", "04"}, {"ss", "05"},
		{"A", "PM"}, {"a", "pm"},
	}
	var b strings.Builder
	hasTime := false
	for i := 0; i < len(format); {
		matched := false
		for _, t := range tokens {
			if strings.HasPrefix(format[i:], t.from) {
				b.WriteString(t.to)
				if strings.ContainsAny(t.from, "Hhms") {
					hasTime = true
				}
				i += len(t.from)
				matched = true
				break
			}
		}
		if matched {
			continue
		}
		c := format[i]
		isLetter := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		if (isLetter && c != 'T') || (c >= '0' && c <= '9') {
			return "", false, false
		}
		b.WriteByte(c)
		i++
	}
	return b.String(), hasTime, true
}

func parseGantt(headerLine int, body []srcLine) (*gantt, error) {
	g := &gantt{dateLayout: "2006-01-02"}
	section := -1
	for _, ln := range body {
		s := ln.text
		word, rest := splitWord(s)
		switch strings.ToLower(word) {
		case "title":
			g.title = cleanLabel(rest)
			continue
		case "dateformat":
			layout, hasTime, ok := dayjsLayout(rest)
			if !ok {
				return nil, syntaxErr(ln.n, "unsupported dateFormat %q", truncateRunes(rest, 30))
			}
			g.dateLayout, g.hasTime = layout, hasTime
			continue
		case "axisformat":
			g.axisFormat = rest
			continue
		case "tickinterval":
			g.tickInterval = strings.ToLower(rest)
			continue
		case "excludes":
			if strings.Contains(strings.ToLower(rest), "weekends") {
				g.weekends = true
			}
			continue
		case "includes", "todaymarker", "weekday", "displaymode", "inclusiveenddates", "topaxis", "click", "weekend":
			continue
		case "section":
			if len(g.sections) >= MaxElements {
				return nil, tooLarge("sections", MaxElements)
			}
			g.sections = append(g.sections, cleanLabel(rest))
			section = len(g.sections) - 1
			continue
		}
		if key, value, ok := accStatement(s); ok {
			if key == "accTitle" {
				g.accTitle = value
			} else {
				g.accDescr = value
			}
			continue
		}
		colon := strings.IndexByte(s, ':')
		if colon < 0 {
			return nil, syntaxErr(ln.n, "task %q needs ':' and a date or duration", truncateRunes(s, 40))
		}
		if len(g.tasks) >= MaxElements {
			return nil, tooLarge("tasks", MaxElements)
		}
		t := &ganttTask{name: cleanLabel(s[:colon]), section: section, prev: len(g.tasks) - 1, line: ln.n}
		if err := t.meta(ln.n, s[colon+1:]); err != nil {
			return nil, err
		}
		g.tasks = append(g.tasks, t)
	}
	if len(g.tasks) == 0 {
		return nil, syntaxErr(headerLine, "gantt chart has no tasks")
	}
	if err := g.resolve(); err != nil {
		return nil, err
	}
	return g, nil
}

func (t *ganttTask) meta(line int, s string) error {
	var items []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		switch strings.ToLower(p) {
		case "done":
			t.done = true
		case "active":
			t.active = true
		case "crit":
			t.crit = true
		case "milestone":
			t.milestone = true
		case "":
		default:
			items = append(items, p)
		}
	}
	switch len(items) {
	case 1:
		t.endSpec = items[0]
	case 2:
		t.startSpec, t.endSpec = items[0], items[1]
	case 3:
		t.id, t.startSpec, t.endSpec = items[0], items[1], items[2]
	case 0:
		return syntaxErr(line, "task needs a date or duration")
	default:
		return syntaxErr(line, "task has too many fields")
	}
	if len(items) == 2 {
		// `id, 5d` is an id plus a duration after the previous task.
		if _, ok := parseDuration(items[1]); ok && !strings.HasPrefix(strings.ToLower(items[0]), "after ") && !looksLikeDate(items[0]) {
			t.id, t.startSpec = items[0], ""
		}
	}
	return nil
}

func looksLikeDate(s string) bool {
	return s != "" && s[0] >= '0' && s[0] <= '9'
}

// parseDuration reads 3d, 2w, 12h, 30m (minutes), 45s, 1.5d.
func parseDuration(s string) (time.Duration, bool) {
	s = strings.TrimSpace(s)
	units := []struct {
		suffix string
		d      time.Duration
	}{{"ms", time.Millisecond}, {"w", 7 * 24 * time.Hour}, {"d", 24 * time.Hour}, {"h", time.Hour}, {"m", time.Minute}, {"s", time.Second}}
	for _, u := range units {
		if !strings.HasSuffix(s, u.suffix) {
			continue
		}
		v, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(s, u.suffix)), 64)
		if err != nil || v < 0 || v > 20*365*24 {
			return 0, false
		}
		d := time.Duration(v * float64(u.d))
		if d > 20*365*24*time.Hour {
			return 0, false
		}
		return d, true
	}
	return 0, false
}

// resolve computes every task's dates, iterating because `after` may name
// a task declared later.
func (g *gantt) resolve() error {
	byID := map[string]*ganttTask{}
	for _, t := range g.tasks {
		if t.id != "" {
			byID[t.id] = t
		}
	}
	for pass := 0; pass <= len(g.tasks); pass++ {
		progress, pending := false, 0
		for _, t := range g.tasks {
			if t.resolved {
				continue
			}
			ok, err := g.resolveTask(t, byID)
			if err != nil {
				return err
			}
			if ok {
				progress = true
			} else {
				pending++
			}
		}
		if pending == 0 {
			return g.checkSpan()
		}
		if !progress {
			break
		}
	}
	for _, t := range g.tasks {
		if !t.resolved {
			return syntaxErr(t.line, "task %q refers to a task that does not exist or forms a cycle", truncateRunes(t.name, 40))
		}
	}
	return g.checkSpan()
}

func (g *gantt) checkSpan() error {
	lo, hi := g.span()
	if hi.Sub(lo) > 20*365*24*time.Hour {
		return tooLarge("years in the time span", 20)
	}
	return nil
}

func (g *gantt) resolveTask(t *ganttTask, byID map[string]*ganttTask) (bool, error) {
	spec := strings.TrimSpace(t.startSpec)
	switch {
	case spec == "":
		if t.prev < 0 {
			return false, syntaxErr(t.line, "the first task needs a start date")
		}
		p := g.tasks[t.prev]
		if !p.resolved {
			return false, nil
		}
		t.start = p.end
	case strings.HasPrefix(strings.ToLower(spec), "after "):
		var latest time.Time
		for _, ref := range strings.Fields(spec[6:]) {
			r, ok := byID[ref]
			if !ok {
				return false, syntaxErr(t.line, "unknown task id %q", truncateRunes(ref, 30))
			}
			if !r.resolved {
				return false, nil
			}
			if r.end.After(latest) {
				latest = r.end
			}
		}
		t.start = latest
	default:
		d, err := time.Parse(g.dateLayout, spec)
		if err != nil {
			return false, syntaxErr(t.line, "start %q does not match the date format", truncateRunes(spec, 30))
		}
		t.start = d
	}
	end := strings.TrimSpace(t.endSpec)
	switch {
	case strings.HasPrefix(strings.ToLower(end), "until "):
		r, ok := byID[strings.TrimSpace(end[6:])]
		if !ok {
			return false, syntaxErr(t.line, "unknown task id in until")
		}
		if !r.resolved {
			return false, nil
		}
		t.end = r.start
	default:
		if dur, ok := parseDuration(end); ok {
			t.end = g.addDuration(t.start, dur)
		} else if d, err := time.Parse(g.dateLayout, end); err == nil {
			t.end = d
		} else {
			return false, syntaxErr(t.line, "end %q is neither a date nor a duration", truncateRunes(end, 30))
		}
	}
	if t.milestone {
		t.end = t.start
	}
	if t.end.Before(t.start) {
		return false, syntaxErr(t.line, "task %q ends before it starts", truncateRunes(t.name, 40))
	}
	t.resolved = true
	return true, nil
}

// addDuration adds a duration, skipping Saturdays and Sundays for whole
// days when weekends are excluded.
func (g *gantt) addDuration(start time.Time, d time.Duration) time.Time {
	if !g.weekends || d%(24*time.Hour) != 0 {
		return start.Add(d)
	}
	days := int(d / (24 * time.Hour))
	t := start
	for days > 0 {
		t = t.AddDate(0, 0, 1)
		if wd := t.Add(-time.Hour).Weekday(); wd != time.Saturday && wd != time.Sunday {
			days--
		}
	}
	return t
}

func (g *gantt) span() (time.Time, time.Time) {
	lo, hi := g.tasks[0].start, g.tasks[0].end
	for _, t := range g.tasks {
		if t.start.Before(lo) {
			lo = t.start
		}
		if t.end.After(hi) {
			hi = t.end
		}
	}
	return lo, hi
}

// strftime formats the subset of d3 time format directives Mermaid users write.
func strftime(format string, t time.Time, loc locale) string {
	var b strings.Builder
	for i := 0; i < len(format); i++ {
		if format[i] != '%' || i+1 >= len(format) {
			b.WriteByte(format[i])
			continue
		}
		i++
		switch format[i] {
		case 'Y':
			b.WriteString(strconv.Itoa(t.Year()))
		case 'y':
			b.WriteString(pad2(t.Year() % 100))
		case 'm':
			b.WriteString(pad2(int(t.Month())))
		case 'd':
			b.WriteString(pad2(t.Day()))
		case 'e':
			b.WriteString(strconv.Itoa(t.Day()))
		case 'b', 'B', 'h':
			b.WriteString(loc.months[t.Month()-1])
		case 'a', 'A':
			b.WriteString(t.Weekday().String()[:3])
		case 'H':
			b.WriteString(pad2(t.Hour()))
		case 'I':
			h := t.Hour() % 12
			if h == 0 {
				h = 12
			}
			b.WriteString(pad2(h))
		case 'M':
			b.WriteString(pad2(t.Minute()))
		case 'S':
			b.WriteString(pad2(t.Second()))
		case 'p':
			if t.Hour() < 12 {
				b.WriteString("AM")
			} else {
				b.WriteString("PM")
			}
		case 'j':
			b.WriteString(strconv.Itoa(t.YearDay()))
		case '%':
			b.WriteByte('%')
		default:
			b.WriteByte('%')
			b.WriteByte(format[i])
		}
	}
	return b.String()
}

func pad2(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

// ganttTicks picks tick instants and a default label format for a span.
func ganttTicks(lo, hi time.Time, interval string) ([]time.Time, string) {
	span := hi.Sub(lo)
	day := 24 * time.Hour
	unit, count := "", 1
	if interval != "" {
		for _, u := range []string{"month", "week", "day", "hour"} {
			if idx := strings.Index(interval, u); idx > 0 {
				if n, err := strconv.Atoi(strings.TrimSpace(interval[:idx])); err == nil && n > 0 {
					unit, count = u, n
				}
			}
		}
	}
	if unit == "" {
		switch {
		case span <= 2*day:
			unit, count = "hour", 3
		case span <= 12*day:
			unit, count = "day", 1
		case span <= 26*day:
			unit, count = "day", 2
		case span <= 100*day:
			unit, count = "week", 1
		case span <= 2*365*day:
			unit, count = "month", 1
		default:
			unit, count = "month", 3
		}
	}
	format := "%b %e"
	var t time.Time
	switch unit {
	case "hour":
		format = "%H:%M"
		t = lo.Truncate(time.Hour)
	case "day":
		t = time.Date(lo.Year(), lo.Month(), lo.Day(), 0, 0, 0, 0, lo.Location())
	case "week":
		t = time.Date(lo.Year(), lo.Month(), lo.Day(), 0, 0, 0, 0, lo.Location())
		for t.Weekday() != time.Monday {
			t = t.AddDate(0, 0, -1)
		}
	default:
		format = "%b %Y"
		t = time.Date(lo.Year(), lo.Month(), 1, 0, 0, 0, 0, lo.Location())
	}
	var ticks []time.Time
	for !t.After(hi) && len(ticks) < 400 {
		if !t.Before(lo) {
			ticks = append(ticks, t)
		}
		switch unit {
		case "hour":
			t = t.Add(time.Duration(count) * time.Hour)
		case "day":
			t = t.AddDate(0, 0, count)
		case "week":
			t = t.AddDate(0, 0, 7*count)
		default:
			t = t.AddDate(0, count, 0)
		}
	}
	return ticks, format
}

func renderGantt(headerLine int, rest string, body []srcLine, rc rctx) (*drawing, error) {
	_ = rest
	g, err := parseGantt(headerLine, body)
	if err != nil {
		return nil, err
	}
	loc := rc.loc
	first, last := g.span()
	lo, hi := g.axisRange(first, last)
	spanDays := hi.Sub(lo).Hours() / 24
	plotW := clamp(spanDays*12, 420, 640)
	const (
		margin  = 12.0
		rowH    = 28.0
		headH   = 26.0
		axisH   = 26.0
		barH    = 16.0
		nameCol = 24
	)
	nameLines := make([][]string, len(g.tasks))
	colW := 80.0
	for i, t := range g.tasks {
		nameLines[i] = wrapLabel(t.name, nameCol)
		if len(nameLines[i]) > 2 {
			nameLines[i] = []string{nameLines[i][0], truncateRunes(nameLines[i][1]+" "+nameLines[i][2], nameCol)}
		}
		colW = math.Max(colW, maxLineWidth(nameLines[i], smallFont)+16)
	}
	for _, s := range g.sections {
		colW = math.Max(colW, math.Min(textWidth(s, smallFont)+16, 240))
	}
	colW = math.Min(colW, 240)
	left := margin + colW
	xOf := func(t time.Time) float64 { return left + t.Sub(lo).Hours()/24/spanDays*plotW }
	// Rows: tasks grouped by section in declaration order.
	type row struct {
		header  bool
		section int
		task    int
		y, h    float64
	}
	var rows []row
	y := margin
	current := -2
	for i, t := range g.tasks {
		if t.section != current {
			current = t.section
			if t.section >= 0 && g.sections[t.section] != "" {
				rows = append(rows, row{header: true, section: t.section, y: y, h: headH})
				y += headH
			}
		}
		h := rowH
		if len(nameLines[i]) > 1 {
			h += smallLine
		}
		rows = append(rows, row{section: t.section, task: i, y: y, h: h})
		y += h
	}
	plotBottom := y
	height := plotBottom + axisH + margin
	width := left + plotW + margin
	var bands, grid, bars, labels []ui.Node
	// Section bands.
	bandStart, bandSection, bandIdx := margin, -2, 0
	flush := func(end float64) {
		if bandSection == -2 {
			return
		}
		cls := "dg-section-band"
		if bandIdx%2 == 1 {
			cls += " dg-section-band-alt"
		}
		bands = append(bands, rect(cls, margin, bandStart, width-2*margin, end-bandStart, 0))
		bandIdx++
	}
	for _, r := range rows {
		if r.section != bandSection {
			flush(r.y)
			bandStart, bandSection = r.y, r.section
		}
	}
	flush(plotBottom)
	ticks, format := ganttTicks(lo, hi, g.tickInterval)
	if g.axisFormat != "" {
		format = g.axisFormat
	}
	lastRight := -1.0
	for _, t := range ticks {
		x := xOf(t)
		grid = append(grid, line("dg-grid", x, margin, x, plotBottom))
		txt := strftime(format, t, loc)
		w := textWidth(txt, smallFont) + 6
		if x-w/2 > lastRight+4 && x+w/2 <= width {
			labels = append(labels, label(x-w/2, plotBottom+5, w, smallLine+2, []string{txt}, "", "dg-label-small dg-label-muted"))
			lastRight = x + w/2
		}
	}
	grid = append(grid, line("dg-axis", left, plotBottom, left+plotW, plotBottom))
	for _, r := range rows {
		if r.header {
			labels = append(labels, label(margin+6, r.y, width-2*margin-12, r.h, []string{truncateRunes(g.sections[r.section], 80)}, "start", "dg-label-small dg-label-strong"))
			continue
		}
		t := g.tasks[r.task]
		labels = append(labels, label(margin+6, r.y, colW-10, r.h, nameLines[r.task], "start", "dg-label-small"))
		cy := r.y + r.h/2
		if t.milestone {
			x := xOf(t.start)
			s := 7.0
			bars = append(bars, el("polygon", "dg-milestone "+taskClass(t), attrs{"points": pointsAttr([]point{{x, cy - s}, {x + s, cy}, {x, cy + s}, {x - s, cy}})}))
			// The name sits beside the diamond, on whichever side has room.
			name := truncateRunes(plainLabel(t.name), 40)
			w := textWidth(name, smallFont) + 8
			if x+s+4+w <= width-margin {
				labels = append(labels, label(x+s+4, cy-smallLine/2-1, w, smallLine+2, []string{name}, "start", "dg-label-small dg-milestone-label"))
			} else {
				labels = append(labels, label(x-s-4-w, cy-smallLine/2-1, w, smallLine+2, []string{name}, "end", "dg-label-small dg-milestone-label"))
			}
			continue
		}
		x0, x1 := xOf(t.start), xOf(t.end)
		bars = append(bars, rect("dg-task "+taskClass(t), x0, cy-barH/2, math.Max(x1-x0, 2), barH, 3))
	}
	name := g.accTitle
	if name == "" {
		name = loc.t("gantt")
	}
	desc := g.accDescr
	dateFmt := "2006-01-02"
	if g.hasTime {
		dateFmt = "2006-01-02 15:04"
	}
	sections := 0
	for _, s := range g.sections {
		if s != "" {
			sections++
		}
	}
	if desc == "" {
		desc = loc.t("gantt.summary", "n", strconv.Itoa(len(g.tasks)), "s", strconv.Itoa(max(sections, 1)),
			"start", first.Format(dateFmt), "end", last.Format(dateFmt))
	}
	tableRows := make([][]string, len(g.tasks))
	for i, t := range g.tasks {
		sec := ""
		if t.section >= 0 {
			sec = g.sections[t.section]
		}
		tableRows[i] = []string{sec, t.name, t.start.Format(dateFmt), t.end.Format(dateFmt), taskStatus(t, loc)}
	}
	return &drawing{
		kind: KindGantt, title: g.title, name: name, desc: desc, width: width, height: height,
		body: []ui.Node{group("dg-bands", bands...), group("dg-grid-lines", grid...), group("dg-tasks", bars...), group("dg-labels", labels...)},
		fallback: fallbackTable(loc.t("data"),
			[]string{loc.t("col.section"), loc.t("col.task"), loc.t("col.start"), loc.t("col.end"), loc.t("col.status")}, tableRows),
	}, nil
}

func taskClass(t *ganttTask) string {
	cls := []string{}
	if t.done {
		cls = append(cls, "dg-task-done")
	}
	if t.active {
		cls = append(cls, "dg-task-active")
	}
	if t.crit {
		cls = append(cls, "dg-task-crit")
	}
	if len(cls) == 0 {
		cls = append(cls, "dg-task-planned")
	}
	return strings.Join(cls, " ")
}

func taskStatus(t *ganttTask, loc locale) string {
	var parts []string
	if t.milestone {
		parts = append(parts, loc.t("status.milestone"))
	}
	if t.done {
		parts = append(parts, loc.t("status.done"))
	}
	if t.active {
		parts = append(parts, loc.t("status.active"))
	}
	if t.crit {
		parts = append(parts, loc.t("status.crit"))
	}
	if len(parts) == 0 {
		parts = append(parts, loc.t("status.planned"))
	}
	return strings.Join(parts, loc.listJoin)
}

// axisRange pads the span from the first start to the last end (which
// includes every milestone date) so nothing sits on the plot border.
func (g *gantt) axisRange(first, last time.Time) (time.Time, time.Time) {
	span := last.Sub(first)
	pad := span / 25
	if pad < 12*time.Hour {
		pad = 12 * time.Hour
	}
	if span < 2*24*time.Hour && span > 0 {
		pad = span / 25
	}
	return first.Add(-pad), last.Add(pad)
}
