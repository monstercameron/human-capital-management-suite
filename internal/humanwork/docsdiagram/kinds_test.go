package docsdiagram

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

func lines(t *testing.T, src string) []srcLine {
	t.Helper()
	ls, err := prepare(src)
	if err != nil {
		t.Fatal(err)
	}
	return ls
}

func TestParseSequence(t *testing.T) {
	sq, err := parseSequence(lines(t, `x
    title: Leave flow
    autonumber 10 5
    participant E as Employee
    actor "M" as Manager
    create participant H
    box Aqua Team
      participant X
    end
    E->>+M: Ask
    M-->>-E: Answer
    E->H: plain
    E-->H: dotted open
    E-xH: lost
    E--xH: lost dotted
    E-)H: async
    E--)H: async dotted
    E<<->>H: both
    E<<-->>H: both dotted
    activate H
    deactivate H
    Note left of E: Left
    Note over E,M: Spanning
    par One
      E->>M: a
    and Two
      E->>H: b
    end
    critical Must
      E->>M: c
    option Fallback
      E->>M: d
    end
    rect rgb(0,0,0)
      E->>M: e
    end
    opt Maybe
      E->>M: f
    accTitle: Named
    accDescr: Described`)[1:])
	if err != nil {
		t.Fatal(err)
	}
	if sq.title != "Leave flow" || sq.accTitle != "Named" || sq.accDescr != "Described" {
		t.Errorf("titles: %q %q %q", sq.title, sq.accTitle, sq.accDescr)
	}
	if len(sq.parts) != 4 || sq.parts[0].label != "Employee" || !sq.parts[1].actor || sq.parts[1].label != "Manager" || sq.parts[3].id != "X" {
		t.Errorf("participants: %+v", sq.parts)
	}
	var msgs []seqEvent
	blocks := 0
	for _, ev := range sq.events {
		switch ev.kind {
		case evMessage:
			msgs = append(msgs, ev)
		case evBlockStart:
			blocks++
		}
	}
	if sq.messages != 16 || len(msgs) != 16 || blocks != 4 {
		t.Fatalf("messages=%d blocks=%d", sq.messages, blocks)
	}
	if msgs[0].number != 10 || msgs[1].number != 15 || !msgs[0].activate || !msgs[1].deactivate || !msgs[1].dotted {
		t.Errorf("first messages: %+v %+v", msgs[0], msgs[1])
	}
	heads := []seqHead{headNone, headNone, headCross, headCross, headAsync, headAsync, headFilled, headFilled}
	for i, h := range heads {
		if msgs[2+i].head != h {
			t.Errorf("message %d head = %d, want %d", 2+i, msgs[2+i].head, h)
		}
	}
	if !msgs[8].both || !msgs[9].dotted {
		t.Error("bidirectional arrows not parsed")
	}
	// The unclosed opt block is closed at the end.
	if last := sq.events[len(sq.events)-1]; last.kind != evBlockEnd || last.block != "opt" {
		t.Errorf("last event %+v", last)
	}
}

func TestRenderSequenceLayout(t *testing.T) {
	out := renderHTML(t, `sequenceDiagram
    participant A as Alpha
    participant B as Beta
    participant C as Gamma
    A->>C: A long message that spans two lifelines and needs room
    C->>C: Self call with a label
    Note left of A: Left note
    Note right of C: Right note
    Note over A,C: Across everyone
    activate B
    B->>A: unfinished activation`, Options{})
	if strings.Count(out, `class="dg-participant"`) != 6 || strings.Count(out, `class="dg-note"`) != 3 || strings.Count(out, `class="dg-activation"`) != 1 {
		t.Errorf("unexpected structure")
	}
	if !strings.Contains(out, "Sequence diagram between Alpha, Beta, Gamma with 3 messages.") {
		t.Error("summary missing")
	}
	if !strings.Contains(out, "Alpha → Gamma: A long message") {
		t.Error("fallback list missing message")
	}
}

func TestParsePie(t *testing.T) {
	pc, err := parsePie(1, "title Staff mix", lines(t, "x\n showData\n \"A: colon\" : 1.5\n \"B\" : 0\n accTitle: T\n accDescr: D")[1:])
	if err != nil {
		t.Fatal(err)
	}
	if pc.title != "Staff mix" || !pc.showData || len(pc.slices) != 2 || pc.slices[0].label != "A: colon" || pc.slices[0].value != 1.5 {
		t.Errorf("pie = %+v", pc)
	}
	full := renderHTML(t, "pie\n \"Only\" : 3", Options{})
	if strings.Count(full, " A") < 2 {
		t.Error("a single full slice needs two arcs")
	}
	if !strings.Contains(full, "100%") {
		t.Error("full slice percent missing")
	}
}

func TestParseXYChart(t *testing.T) {
	xc, err := parseXYChart(1, "", lines(t, `x
    title "Sales"
    x-axis "Month" [Jan, "Feb, late", Mar]
    y-axis Revenue
    bar "Plan" [1, 2, 3]
    line [3, 2]`)[1:])
	if err != nil {
		t.Fatal(err)
	}
	if xc.title != "Sales" || xc.xTitle != "Month" || xc.yTitle != "Revenue" || len(xc.categories) != 3 || xc.categories[1] != "Feb, late" {
		t.Errorf("chart = %+v", xc)
	}
	if xc.series[0].name != "Plan" || xc.series[0].line || !xc.series[1].line {
		t.Errorf("series = %+v", xc.series)
	}
	num, err := parseXYChart(1, "", lines(t, "x\n x-axis 1 --> 5\n line [1, 2, 3, 4, 5]")[1:])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(num.categories, ",") != "1,2,3,4,5" {
		t.Errorf("numeric categories = %v", num.categories)
	}
	auto, err := parseXYChart(1, "", lines(t, "x\n bar [4, -2]")[1:])
	if err != nil || strings.Join(auto.categories, ",") != "1,2" {
		t.Errorf("auto categories = %v %v", auto, err)
	}
	neg := renderHTML(t, "xychart-beta\n bar [4, -2]\n line [1, 1]", Options{})
	if strings.Count(neg, `class="dg-bar`) != 2 || !strings.Contains(neg, "-2") {
		t.Error("negative bar missing")
	}
	many := renderHTML(t, "xychart-beta\n x-axis ["+strings.Repeat("Category name,", 39)+"Last]\n bar ["+strings.Repeat("1,", 39)+"2]", Options{})
	if strings.Count(many, `class="dg-bar`) != 40 {
		t.Error("dense chart lost bars")
	}
}

func TestNiceTicks(t *testing.T) {
	cases := []struct {
		lo, hi         float64
		wantLo, wantHi float64
		wantStep       float64
	}{
		{0, 97, 0, 100, 20},
		{3, 3, 1, 5, 1},
		{0, 0, 0, 1, 0.2},
		{-12, 48, -20, 60, 20},
		{0.1, 0.9, 0, 1, 0.2},
	}
	for _, c := range cases {
		lo, hi, ticks := niceTicks(c.lo, c.hi, 5, true)
		if lo != c.wantLo || hi != c.wantHi || len(ticks) < 2 || math.Abs(ticks[1]-ticks[0]-c.wantStep) > 1e-9 {
			t.Errorf("niceTicks(%v,%v) = %v %v %v", c.lo, c.hi, lo, hi, ticks)
		}
	}
	if niceStep(-1) != 1 || niceStep(2.2) != 2.5 || decimalsFor(0.25) != 2 || decimalsFor(5) != 0 {
		t.Error("step helpers wrong")
	}
}

func TestParseGantt(t *testing.T) {
	g, err := parseGantt(1, lines(t, `x
    dateFormat YYYY-MM-DD HH:mm
    axisFormat %d/%m
    tickInterval 1day
    excludes weekends
    todayMarker off
    section Prep
    Kickoff :k, 2026-01-02 09:00, 2026-01-02 17:00
    Draft :d, after k, 3d
    Review :r, after d q, 1w
    Queue :q, 2026-01-01 00:00, 1d
    Wrap :until r
    section Close
    Sign :milestone, crit, done, s, after r, 2d`)[1:])
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]*ganttTask{}
	for _, tk := range g.tasks {
		byID[tk.id] = tk
	}
	if !g.hasTime || !g.weekends || g.axisFormat != "%d/%m" || g.tickInterval != "1day" {
		t.Errorf("settings: %+v", g)
	}
	// Draft starts Friday 17:00 and three working days skip the weekend.
	if got := byID["d"].end.Format("2006-01-02 15:04"); got != "2026-01-07 17:00" {
		t.Errorf("draft end = %s", got)
	}
	if !byID["r"].start.Equal(byID["d"].end) {
		t.Error("after with two ids should take the later end")
	}
	wrap := g.tasks[4]
	if !wrap.start.Equal(byID["q"].end) || !wrap.end.Equal(byID["r"].start) {
		t.Errorf("until task %v..%v", wrap.start, wrap.end)
	}
	if s := byID["s"]; !s.milestone || !s.crit || !s.done || !s.end.Equal(s.start) {
		t.Errorf("milestone = %+v", s)
	}
	if taskStatus(byID["s"], localeFor("en")) != "milestone, done, critical" {
		t.Errorf("status = %s", taskStatus(byID["s"], localeFor("en")))
	}
}

func TestGanttDurationsAndFormats(t *testing.T) {
	durs := map[string]time.Duration{"3d": 72 * time.Hour, "2w": 14 * 24 * time.Hour, "12h": 12 * time.Hour, "30m": 30 * time.Minute, "45s": 45 * time.Second, "1.5d": 36 * time.Hour, "250ms": 250 * time.Millisecond}
	for in, want := range durs {
		if got, ok := parseDuration(in); !ok || got != want {
			t.Errorf("parseDuration(%s) = %v %v", in, got, ok)
		}
	}
	for _, bad := range []string{"", "d", "-1d", "3x", "999999999d"} {
		if _, ok := parseDuration(bad); ok {
			t.Errorf("parseDuration(%q) accepted", bad)
		}
	}
	layouts := map[string]string{"YYYY-MM-DD": "2006-01-02", "DD/MM/YY": "02/01/06", "D MMM YYYY": "2 Jan 2006", "YYYY-MM-DDTHH:mm:ss": "2006-01-02T15:04:05", "h:mm A": "3:04 PM"}
	for in, want := range layouts {
		if got, _, ok := dayjsLayout(in); !ok || got != want {
			t.Errorf("dayjsLayout(%s) = %s", in, got)
		}
	}
	day := time.Date(2026, 3, 9, 14, 5, 7, 0, time.UTC)
	if got := strftime("%Y-%m-%d %e %b %a %H:%M:%S %I%p %y %j %% %q", day, localeFor("de")); got != "2026-03-09 9 März Mon 14:05:07 02PM 26 68 % %q" {
		t.Errorf("strftime = %s", got)
	}
	for _, c := range []struct {
		span   time.Duration
		format string
	}{{20 * time.Hour, "%H:%M"}, {10 * 24 * time.Hour, "%b %e"}, {20 * 24 * time.Hour, "%b %e"}, {60 * 24 * time.Hour, "%b %e"}, {300 * 24 * time.Hour, "%b %Y"}, {1000 * 24 * time.Hour, "%b %Y"}} {
		lo := time.Date(2026, 1, 7, 0, 0, 0, 0, time.UTC)
		ticks, format := ganttTicks(lo, lo.Add(c.span), "")
		if format != c.format || len(ticks) < 2 || len(ticks) > 40 {
			t.Errorf("span %v: %d ticks, format %s", c.span, len(ticks), format)
		}
	}
	lo := time.Date(2026, 1, 7, 0, 0, 0, 0, time.UTC)
	if ticks, _ := ganttTicks(lo, lo.AddDate(0, 6, 0), "2week"); len(ticks) != 13 {
		t.Errorf("tickInterval 2week gave %d ticks", len(ticks))
	}
	out := renderHTML(t, "gantt\n dateFormat YYYY-MM-DD HH:mm\n A : 2026-01-01 08:00, 6h\n B : 2h", Options{})
	if !strings.Contains(out, "2026-01-01 14:00") {
		t.Error("time-of-day fallback dates missing")
	}
}

func TestTimelineAndJourney(t *testing.T) {
	tl, err := parseTimeline(1, lines(t, "x\n title T\n 2019 : A : B\n      : C\n 2020")[1:])
	if err != nil {
		t.Fatal(err)
	}
	if len(tl.periods) != 2 || len(tl.periods[0].events) != 3 || len(tl.periods[1].events) != 0 {
		t.Errorf("timeline = %+v", tl.periods)
	}
	out := renderHTML(t, "timeline\n 2019 : A\n 2020 : B", Options{})
	if strings.Contains(out, ">Section<") || !strings.Contains(out, "Timeline with 2 periods") {
		t.Error("sectionless timeline should drop the section column")
	}
	j, err := parseJourney(1, lines(t, "x\n Task one: 0\n section S\n Task two: 4: A, B")[1:])
	if err != nil {
		t.Fatal(err)
	}
	if len(j.tasks) != 2 || j.tasks[0].section != -1 || j.tasks[1].section != 0 || len(j.tasks[1].actors) != 2 {
		t.Errorf("journey = %+v", j)
	}
	jo := renderHTML(t, "journey\n title Day\n section S\n  A: 1: Me\n  B: 2\n  C: 3\n  D: 4", Options{Locale: "ar"})
	for s := 1; s <= 4; s++ {
		if !strings.Contains(jo, "dg-score-"+itoa(s)) {
			t.Errorf("score class %d missing", s)
		}
	}
	if !strings.Contains(jo, "رحلة") {
		t.Error("Arabic summary missing")
	}
}

func TestLabelHelpers(t *testing.T) {
	if got := truncateRunes("abcdef", 4); got != "abc…" {
		t.Errorf("truncate = %s", got)
	}
	if got := decodeEntities("#amp; #9829; #bogus; #"); got != "& ♥ #bogus; #" {
		t.Errorf("decode = %q", got)
	}
	if k, v, ok := accStatement("accDescr { multi }"); !ok || k != "accDescr" || v != "multi" {
		t.Errorf("accStatement = %q %q %v", k, v, ok)
	}
	if _, ok := keywordRest("titles x", "title"); ok {
		t.Error("keywordRest matched a longer word")
	}
	if num(math.NaN()) != "0" || num(-0.04) != "0" || num(12.345) != "12.3" {
		t.Error("num formatting wrong")
	}
	if widthClass(10) != "docs-diagram-w-160" || widthClass(5000) != "docs-diagram-w-2400" || widthClass(641) != "docs-diagram-w-720" {
		t.Error("width buckets wrong")
	}
}

// Regression: a milestone dated after every task's end (the seeded October
// cohort plan) must sit inside the time axis and be drawn with its name.
func TestGanttTrailingMilestoneIsInsideTheAxis(t *testing.T) {
	src := `gantt
    title October cohort onboarding
    dateFormat YYYY-MM-DD
    section Before day one
    Background checks        :done,    bg, 2026-09-14, 10d
    Accounts and equipment   :active,  eq, 2026-09-21, 7d
    section First weeks
    Orientation              :         or, 2026-10-05, 2d
    Unit shadowing           :         sh, after or, 10d
    EHR training             :         ehr, after or, 5d
    section Check-ins
    Day 30 check-in          :milestone, m1, 2026-11-04, 0d`
	out := renderHTML(t, src, Options{})
	vb := regexp.MustCompile(`viewBox="0 0 ([0-9.]+) [0-9.]+"`).FindStringSubmatch(out)
	poly := regexp.MustCompile(`<polygon class="dg-milestone[^"]*" points="([0-9.]+),`).FindStringSubmatch(out)
	if vb == nil || poly == nil {
		t.Fatalf("milestone or viewBox missing")
	}
	width, _ := strconv.ParseFloat(vb[1], 64)
	x, _ := strconv.ParseFloat(poly[1], 64)
	axis := regexp.MustCompile(`<line class="dg-axis" x1="([0-9.]+)" x2="([0-9.]+)"`).FindStringSubmatch(out)
	if axis == nil {
		t.Fatal("axis line missing")
	}
	x1, _ := strconv.ParseFloat(axis[1], 64)
	x2, _ := strconv.ParseFloat(axis[2], 64)
	if !(x > x1+7 && x < x2-7) || x2 > width {
		t.Errorf("milestone at %v is not inside the axis %v..%v (width %v)", x, x1, x2, width)
	}
	if !strings.Contains(out, `dg-milestone-label"`) || strings.Count(out, "Day 30 check-in") < 3 {
		t.Error("milestone name should be drawn beside the diamond and listed")
	}
	if !strings.Contains(out, "Nov") {
		t.Error("axis ticks stop before the milestone month")
	}
	// The summary reports the real span, not the padded axis.
	if !strings.Contains(out, "from 2026-09-14 to 2026-11-04.") {
		t.Error("summary span wrong")
	}
	lone := renderHTML(t, "gantt\n Launch :milestone, 2026-01-01, 0d", Options{})
	if !strings.Contains(lone, `<polygon class="dg-milestone`) || !strings.Contains(lone, "dg-milestone-label") {
		t.Error("a chart holding only a milestone must still draw it")
	}
}

// A lone unnamed series needs no key; two or more series always get one,
// because an unkeyed line over bars reads as the same measure (docs M8).
func TestXYChartLegendForNamedOrMultipleSeries(t *testing.T) {
	lone := renderHTML(t, "xychart-beta\n x-axis [a, b]\n bar [1, 2]", Options{})
	if strings.Contains(lone, `class="dg-legend-item"`) {
		t.Error("a single unnamed series drew a legend")
	}
	unnamed := renderHTML(t, "xychart-beta\n x-axis [a, b]\n bar [1, 2]\n line [2, 1]", Options{})
	if strings.Count(unnamed, `class="dg-legend-item"`) != 2 {
		t.Error("two unnamed series should both be keyed")
	}
	if !strings.Contains(unnamed, "<th scope=\"col\">Series 1</th>") {
		t.Error("fallback table should still name unnamed series")
	}
	named := renderHTML(t, "xychart-beta\n x-axis [a, b]\n bar \"Hires\" [1, 2]\n line [2, 1]", Options{})
	if strings.Count(named, `class="dg-legend-item"`) != 2 {
		t.Error("a named series should bring the legend for every series")
	}
	single := renderHTML(t, "xychart-beta\n bar \"Hires\" [1, 2]", Options{})
	if strings.Count(single, `class="dg-legend-item"`) != 1 {
		t.Error("a single named series should be keyed")
	}
}
