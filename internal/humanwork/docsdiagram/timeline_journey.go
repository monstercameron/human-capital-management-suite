package docsdiagram

import (
	"math"
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type tlPeriod struct {
	label   string
	events  []string
	section int
}

type timeline struct {
	title    string
	sections []string
	periods  []*tlPeriod
	accTitle string
	accDescr string
}

func parseTimeline(headerLine int, body []srcLine) (*timeline, error) {
	tl := &timeline{}
	section := -1
	events := 0
	for _, ln := range body {
		s := ln.text
		if t, ok := titleRest(s); ok {
			tl.title = t
			continue
		}
		if rest, ok := keywordRest(s, "section"); ok {
			tl.sections = append(tl.sections, cleanLabel(rest))
			section = len(tl.sections) - 1
			if len(tl.sections) > MaxElements {
				return nil, tooLarge("sections", MaxElements)
			}
			continue
		}
		if key, value, ok := accStatement(s); ok {
			if key == "accTitle" {
				tl.accTitle = value
			} else {
				tl.accDescr = value
			}
			continue
		}
		parts := strings.Split(s, ":")
		if strings.HasPrefix(s, ":") {
			if len(tl.periods) == 0 {
				return nil, syntaxErr(ln.n, "event continuation before any period")
			}
		} else {
			if len(tl.periods) >= MaxElements {
				return nil, tooLarge("periods", MaxElements)
			}
			tl.periods = append(tl.periods, &tlPeriod{label: cleanLabel(parts[0]), section: section})
		}
		p := tl.periods[len(tl.periods)-1]
		for _, e := range parts[1:] {
			if e = cleanLabel(e); e != "" {
				p.events = append(p.events, e)
				events++
			}
		}
		if events > MaxElements {
			return nil, tooLarge("events", MaxElements)
		}
	}
	if len(tl.periods) == 0 {
		return nil, syntaxErr(headerLine, "timeline has no periods")
	}
	return tl, nil
}

func renderTimeline(headerLine int, rest string, body []srcLine, rc rctx) (*drawing, error) {
	_ = rest
	tl, err := parseTimeline(headerLine, body)
	if err != nil {
		return nil, err
	}
	loc := rc.loc
	const (
		margin = 16.0
		colW   = 150.0
		gap    = 14.0
		secH   = 26.0
	)
	hasSections := len(tl.sections) > 0
	top := margin
	if hasSections {
		top += secH + 8
	}
	periodLines := make([][]string, len(tl.periods))
	headH := 0.0
	for i, p := range tl.periods {
		periodLines[i] = wrapLabel(p.label, 18)
		headH = math.Max(headH, float64(len(periodLines[i]))*nodeLine+14)
	}
	axisY := top + headH + 18
	var bands, heads, cards, links []ui.Node
	bottom := axisY
	for i, p := range tl.periods {
		x := margin + float64(i)*(colW+gap)
		cls := seriesClass(i)
		if p.section >= 0 {
			cls = seriesClass(p.section)
		}
		heads = append(heads, rect("dg-period "+cls, x, top, colW, headH, 6), label(x, top, colW, headH, periodLines[i], "", "dg-label-strong"))
		cx := x + colW/2
		y := axisY + 14
		for _, e := range p.events {
			lines := wrapLabel(e, 20)
			h := float64(len(lines))*smallLine + 12
			cards = append(cards, rect("dg-event "+cls, x+8, y, colW-16, h, 4), label(x+8, y, colW-16, h, lines, "", "dg-label-small"))
			y += h + 8
		}
		if len(p.events) > 0 {
			links = append(links, line("dg-event-link", cx, top+headH, cx, y-8))
		}
		bottom = math.Max(bottom, y)
	}
	width := margin*2 + float64(len(tl.periods))*(colW+gap) - gap
	if hasSections {
		for si, s := range tl.sections {
			first, last := -1, -1
			for i, p := range tl.periods {
				if p.section == si {
					if first < 0 {
						first = i
					}
					last = i
				}
			}
			if first < 0 {
				continue
			}
			x0 := margin + float64(first)*(colW+gap)
			x1 := margin + float64(last)*(colW+gap) + colW
			bands = append(bands, rect("dg-section-head "+seriesClass(si), x0, margin, x1-x0, secH, 6),
				label(x0, margin, x1-x0, secH, []string{truncateRunes(s, 60)}, "", "dg-label-small dg-label-strong"))
		}
	}
	axis := line("dg-axis dg-timeline-axis", margin, axisY, width-margin, axisY)
	height := bottom + margin
	name := tl.accTitle
	if name == "" {
		name = loc.t("timeline")
	}
	desc := tl.accDescr
	labels := make([]string, len(tl.periods))
	rows := make([][]string, len(tl.periods))
	for i, p := range tl.periods {
		labels[i] = plainLabel(p.label)
		sec := ""
		if p.section >= 0 {
			sec = tl.sections[p.section]
		}
		rows[i] = []string{sec, plainLabel(p.label), strings.Join(p.events, loc.listJoin)}
	}
	if desc == "" {
		desc = loc.t("tl.summary", "n", strconv.Itoa(len(tl.periods)), "list", joinSummary(labels, loc.listJoin, 8, loc))
	}
	head := []string{loc.t("col.period"), loc.t("col.events")}
	if hasSections {
		head = append([]string{loc.t("col.section")}, head...)
	} else {
		for i := range rows {
			rows[i] = rows[i][1:]
		}
	}
	return &drawing{
		kind: KindTimeline, title: tl.title, name: name, desc: desc, width: width, height: height,
		body:     []ui.Node{group("dg-sections", bands...), axis, group("dg-links", links...), group("dg-periods", heads...), group("dg-events", cards...)},
		fallback: fallbackTable(loc.t("data"), head, rows),
	}, nil
}

type jTask struct {
	name    string
	score   int
	actors  []string
	section int
}

type journey struct {
	title    string
	sections []string
	tasks    []jTask
	accTitle string
	accDescr string
}

func parseJourney(headerLine int, body []srcLine) (*journey, error) {
	j := &journey{}
	section := -1
	for _, ln := range body {
		s := ln.text
		if t, ok := titleRest(s); ok {
			j.title = t
			continue
		}
		if rest, ok := keywordRest(s, "section"); ok {
			if len(j.sections) >= MaxElements {
				return nil, tooLarge("sections", MaxElements)
			}
			j.sections = append(j.sections, cleanLabel(rest))
			section = len(j.sections) - 1
			continue
		}
		if key, value, ok := accStatement(s); ok {
			if key == "accTitle" {
				j.accTitle = value
			} else {
				j.accDescr = value
			}
			continue
		}
		parts := strings.SplitN(s, ":", 3)
		if len(parts) < 2 {
			return nil, syntaxErr(ln.n, "task needs ': score'")
		}
		score, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil || score < 0 || score > 5 {
			return nil, syntaxErr(ln.n, "score must be a whole number from 0 to 5")
		}
		t := jTask{name: cleanLabel(parts[0]), score: score, section: section}
		if len(parts) == 3 {
			for _, a := range strings.Split(parts[2], ",") {
				if a = cleanLabel(a); a != "" {
					t.actors = append(t.actors, a)
				}
			}
		}
		if len(j.tasks) >= MaxElements {
			return nil, tooLarge("tasks", MaxElements)
		}
		j.tasks = append(j.tasks, t)
	}
	if len(j.tasks) == 0 {
		return nil, syntaxErr(headerLine, "journey has no tasks")
	}
	return j, nil
}

func renderJourney(headerLine int, rest string, body []srcLine, rc rctx) (*drawing, error) {
	_ = rest
	j, err := parseJourney(headerLine, body)
	if err != nil {
		return nil, err
	}
	loc := rc.loc
	const (
		margin = 16.0
		colW   = 124.0
		secH   = 26.0
		step   = 26.0
		faceR  = 13.0
	)
	top := margin
	if len(j.sections) > 0 {
		top += secH + 10
	}
	plotTop := top + faceR
	plotBottom := plotTop + 5*step
	var bands, grid, faces, names []ui.Node
	var pts []point
	nameH := 0.0
	nameLines := make([][]string, len(j.tasks))
	for i, t := range j.tasks {
		nameLines[i] = wrapLabel(t.name, 16)
		h := float64(len(nameLines[i])) * smallLine
		if len(t.actors) > 0 {
			h += smallLine
		}
		nameH = math.Max(nameH, h)
	}
	for s := 0; s <= 5; s++ {
		y := plotBottom - float64(s)*step
		grid = append(grid, line("dg-grid", margin, y, margin+float64(len(j.tasks))*colW, y))
	}
	for i, t := range j.tasks {
		cx := margin + float64(i)*colW + colW/2
		cy := plotBottom - float64(t.score)*step
		pts = append(pts, point{cx, cy})
		faces = append(faces, group("dg-face dg-score-"+strconv.Itoa(max(t.score, 1)),
			circle("dg-face-dot", cx, cy, faceR),
			label(cx-faceR, cy-faceR, 2*faceR, 2*faceR, []string{strconv.Itoa(t.score)}, "", "dg-label-tiny dg-label-strong"),
		))
		lines := append([]string{}, nameLines[i]...)
		names = append(names, label(cx-colW/2+4, plotBottom+faceR+6, colW-8, float64(len(lines))*smallLine+2, lines, "", "dg-label-small"))
		if len(t.actors) > 0 {
			actors := truncateRunes(strings.Join(t.actors, loc.listJoin), 24)
			names = append(names, label(cx-colW/2+4, plotBottom+faceR+8+float64(len(lines))*smallLine, colW-8, smallLine+2, []string{actors}, "", "dg-label-small dg-label-muted"))
		}
	}
	for si, s := range j.sections {
		first, last := -1, -1
		for i, t := range j.tasks {
			if t.section == si {
				if first < 0 {
					first = i
				}
				last = i
			}
		}
		if first < 0 {
			continue
		}
		x0 := margin + float64(first)*colW + 3
		x1 := margin + float64(last+1)*colW - 3
		bands = append(bands, rect("dg-section-head "+seriesClass(si), x0, margin, x1-x0, secH, 6),
			label(x0, margin, x1-x0, secH, []string{truncateRunes(s, 60)}, "", "dg-label-small dg-label-strong"))
	}
	width := margin*2 + float64(len(j.tasks))*colW
	height := plotBottom + faceR + 10 + nameH + margin
	name := j.accTitle
	if name == "" {
		name = loc.t("journey")
	}
	sum := 0
	rows := make([][]string, len(j.tasks))
	for i, t := range j.tasks {
		sum += t.score
		sec := ""
		if t.section >= 0 {
			sec = j.sections[t.section]
		}
		rows[i] = []string{sec, t.name, strconv.Itoa(t.score), strings.Join(t.actors, loc.listJoin)}
	}
	desc := j.accDescr
	if desc == "" {
		desc = loc.t("journey.summary", "n", strconv.Itoa(len(j.tasks)), "s", strconv.Itoa(max(len(j.sections), 1)),
			"avg", loc.number(float64(sum)/float64(len(j.tasks)), 1))
	}
	return &drawing{
		kind: KindJourney, title: j.title, name: name, desc: desc, width: width, height: height,
		body: []ui.Node{
			group("dg-sections", bands...), group("dg-grid-lines", grid...),
			el("polyline", "dg-journey-path", attrs{"points": pointsAttr(pts)}),
			group("dg-faces", faces...), group("dg-names", names...),
		},
		fallback: fallbackTable(loc.t("data"),
			[]string{loc.t("col.section"), loc.t("col.task"), loc.t("col.score"), loc.t("col.actors")}, rows),
	}, nil
}
