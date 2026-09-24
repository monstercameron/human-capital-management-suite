package docsdiagram

import (
	"math"
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type pieSlice struct {
	label string
	value float64
}

type pieChart struct {
	title    string
	showData bool
	slices   []pieSlice
	accTitle string
	accDescr string
}

func parsePie(headerLine int, rest string, body []srcLine) (*pieChart, error) {
	pc := &pieChart{}
	for _, w := range strings.Fields(rest) {
		if strings.EqualFold(w, "showData") {
			pc.showData = true
		}
	}
	if i := strings.Index(asciiLower(rest), "title"); i >= 0 {
		pc.title = cleanLabel(rest[i+5:])
	}
	for _, ln := range body {
		s := ln.text
		if strings.EqualFold(s, "showData") {
			pc.showData = true
			continue
		}
		if t, ok := titleRest(s); ok {
			pc.title = t
			continue
		}
		if key, value, ok := accStatement(s); ok {
			if key == "accTitle" {
				pc.accTitle = value
			} else {
				pc.accDescr = value
			}
			continue
		}
		colon := strings.LastIndexByte(s, ':')
		if colon < 0 || !strings.HasPrefix(s, "\"") {
			return nil, syntaxErr(ln.n, "expected \"Label\" : value")
		}
		lbl := strings.TrimSpace(s[:colon])
		if len(lbl) < 2 || !strings.HasSuffix(lbl, "\"") {
			return nil, syntaxErr(ln.n, "slice label must be quoted")
		}
		v, ok := parseNumber(s[colon+1:])
		if !ok {
			return nil, syntaxErr(ln.n, "slice value %q is not a number", truncateRunes(strings.TrimSpace(s[colon+1:]), 20))
		}
		if v < 0 {
			return nil, syntaxErr(ln.n, "slice value must not be negative")
		}
		if len(pc.slices) >= MaxElements {
			return nil, tooLarge("slices", MaxElements)
		}
		pc.slices = append(pc.slices, pieSlice{label: cleanLabel(lbl), value: v})
	}
	if len(pc.slices) == 0 {
		return nil, syntaxErr(headerLine, "pie chart has no slices")
	}
	total := 0.0
	for _, s := range pc.slices {
		total += s.value
	}
	if total <= 0 {
		return nil, syntaxErr(headerLine, "pie chart values sum to zero")
	}
	return pc, nil
}

func renderPie(headerLine int, rest string, body []srcLine, rc rctx) (*drawing, error) {
	pc, err := parsePie(headerLine, rest, body)
	if err != nil {
		return nil, err
	}
	loc := rc.loc
	total := 0.0
	for _, s := range pc.slices {
		total += s.value
	}
	const (
		r      = 110.0
		inner  = 62.0
		margin = 16.0
		rowH   = 22.0
	)
	cx, cy := margin+r, margin+r
	var slices []ui.Node
	angle := -math.Pi / 2
	for i, s := range pc.slices {
		if s.value <= 0 {
			continue
		}
		sweep := s.value / total * 2 * math.Pi
		slices = append(slices, path("dg-slice "+seriesClass(i), donutPath(cx, cy, r, inner, angle, angle+sweep), nil))
		angle += sweep
	}
	// Legend to the right: swatch, label, value and percent.
	lx := cx + r + 28
	rows := make([][]string, len(pc.slices))
	labelColW, valueColW := 0.0, 0.0
	for i, s := range pc.slices {
		pct := s.value / total * 100
		detail := loc.percent(pct)
		if pc.showData {
			detail = loc.number(s.value, 2) + " · " + detail
		}
		rows[i] = []string{truncateRunes(plainLabel(s.label), 40), detail}
		labelColW = math.Max(labelColW, textWidth(rows[i][0], smallFont)+8)
		valueColW = math.Max(valueColW, textWidth(detail, smallFont)+8)
	}
	legendW := 18 + labelColW + 10 + valueColW
	legendH := float64(len(rows)) * rowH
	height := math.Max(2*r+2*margin, legendH+2*margin)
	ly := math.Max(margin, cy-legendH/2)
	var legend []ui.Node
	for i, row := range rows {
		y := ly + float64(i)*rowH
		legend = append(legend, group("dg-legend-item",
			rect("dg-legend-swatch "+seriesClass(i), lx, y+5, 12, 12, 2),
			label(lx+18, y, labelColW, rowH, []string{row[0]}, "start", "dg-label-small"),
			label(lx+18+labelColW+10, y, valueColW, rowH, []string{row[1]}, "end", "dg-label-small dg-label-muted"),
		))
	}
	var center []ui.Node
	if pc.showData {
		center = append(center, label(cx-inner, cy-smallLine, 2*inner, 2*smallLine, []string{loc.number(total, 2)}, "", "dg-label-strong"))
	}
	width := lx + legendW + margin
	name := pc.accTitle
	if name == "" {
		name = loc.t("pie")
	}
	desc := pc.accDescr
	if desc == "" {
		parts := make([]string, len(pc.slices))
		for i, s := range pc.slices {
			parts[i] = plainLabel(s.label) + " " + loc.percent(s.value/total*100)
		}
		desc = loc.t("pie.summary", "n", strconv.Itoa(len(pc.slices)), "total", loc.number(total, 2), "list", joinSummary(parts, loc.listJoin, 8, loc))
	}
	tableRows := make([][]string, len(pc.slices))
	for i, s := range pc.slices {
		tableRows[i] = []string{plainLabel(s.label), loc.number(s.value, 4), loc.percent(s.value / total * 100)}
	}
	return &drawing{
		kind: KindPie, title: pc.title, name: name, desc: desc, width: width, height: height,
		body:     []ui.Node{group("dg-slices", slices...), group("dg-center", center...), group("dg-legend", legend...)},
		fallback: fallbackTable(loc.t("data"), []string{loc.t("col.label"), loc.t("col.value"), loc.t("col.percent")}, tableRows),
	}, nil
}

// donutPath draws a ring sector; a full turn is split in two arcs because
// an SVG arc cannot start and end at the same point.
func donutPath(cx, cy, r, inner, a0, a1 float64) string {
	if a1-a0 >= 2*math.Pi-1e-6 {
		mid := a0 + math.Pi
		return donutPath(cx, cy, r, inner, a0, mid) + " " + donutPath(cx, cy, r, inner, mid, a1)
	}
	large := "0"
	if a1-a0 > math.Pi {
		large = "1"
	}
	p := func(rad, a float64) string { return num(cx+rad*math.Cos(a)) + "," + num(cy+rad*math.Sin(a)) }
	return "M" + p(r, a0) + " A" + num(r) + "," + num(r) + " 0 " + large + " 1 " + p(r, a1) +
		" L" + p(inner, a1) + " A" + num(inner) + "," + num(inner) + " 0 " + large + " 0 " + p(inner, a0) + " Z"
}
