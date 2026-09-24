package docsdiagram

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type xySeries struct {
	name   string
	line   bool
	values []float64
}

type xyChart struct {
	title      string
	xTitle     string
	yTitle     string
	categories []string
	xRange     bool
	xMin, xMax float64
	yRange     bool
	yMin, yMax float64
	// A second y-axis statement declares a right-hand axis for the line
	// series of a bar-and-line chart, so a rate is not read on a count axis.
	y2           bool
	y2Title      string
	y2Range      bool
	y2Min, y2Max float64
	series       []xySeries
	accTitle     string
	accDescr     string
}

func parseXYChart(headerLine int, rest string, body []srcLine) (*xyChart, error) {
	if strings.Contains(strings.ToLower(rest), "horizontal") {
		return nil, fmt.Errorf("%w: horizontal xychart", ErrUnsupported)
	}
	xc := &xyChart{}
	points, yAxes := 0, 0
	for _, ln := range body {
		s := ln.text
		word, rest := splitWord(s)
		switch strings.ToLower(word) {
		case "title":
			xc.title = cleanLabel(rest)
		case "x-axis":
			title, remainder := axisTitle(rest)
			xc.xTitle = title
			switch {
			case strings.HasPrefix(remainder, "["):
				cats, err := bracketList(ln.n, remainder)
				if err != nil {
					return nil, err
				}
				if len(cats) > MaxDataPoints {
					return nil, tooLarge("categories", MaxDataPoints)
				}
				xc.categories = cats
			case remainder != "":
				lo, hi, err := axisRange(ln.n, remainder)
				if err != nil {
					return nil, err
				}
				xc.xRange, xc.xMin, xc.xMax = true, lo, hi
			}
		case "y-axis":
			title, remainder := axisTitle(rest)
			var lo, hi float64
			if remainder != "" {
				var err error
				if lo, hi, err = axisRange(ln.n, remainder); err != nil {
					return nil, err
				}
			}
			if yAxes++; yAxes >= 2 {
				xc.y2, xc.y2Title = true, title
				xc.y2Range, xc.y2Min, xc.y2Max = remainder != "", lo, hi
				continue
			}
			xc.yTitle = title
			if remainder != "" {
				xc.yRange, xc.yMin, xc.yMax = true, lo, hi
			}
		case "bar", "line":
			name, remainder := axisTitle(rest)
			items, err := bracketList(ln.n, remainder)
			if err != nil {
				return nil, err
			}
			values := make([]float64, len(items))
			for i, it := range items {
				v, ok := parseNumber(it)
				if !ok {
					return nil, syntaxErr(ln.n, "value %q is not a number", truncateRunes(it, 20))
				}
				values[i] = v
			}
			points += len(values)
			if points > MaxDataPoints {
				return nil, tooLarge("data points", MaxDataPoints)
			}
			if len(xc.series) >= 8*4 {
				return nil, tooLarge("series", 32)
			}
			xc.series = append(xc.series, xySeries{name: name, line: strings.EqualFold(word, "line"), values: values})
		default:
			if key, value, ok := accStatement(s); ok {
				if key == "accTitle" {
					xc.accTitle = value
				} else {
					xc.accDescr = value
				}
				continue
			}
			return nil, syntaxErr(ln.n, "unrecognised statement %q", truncateRunes(s, 40))
		}
	}
	if len(xc.series) == 0 {
		return nil, syntaxErr(headerLine, "chart has no bar or line series")
	}
	n := 0
	for _, s := range xc.series {
		n = max(n, len(s.values))
		if len(s.values) == 0 {
			return nil, syntaxErr(headerLine, "a series has no values")
		}
	}
	if len(xc.categories) > 0 && n > len(xc.categories) {
		return nil, syntaxErr(headerLine, "a series has %d values for %d categories", n, len(xc.categories))
	}
	if len(xc.categories) == 0 {
		xc.categories = make([]string, n)
		for i := range xc.categories {
			v := float64(i + 1)
			if xc.xRange {
				v = xc.xMin
				if n > 1 {
					v += (xc.xMax - xc.xMin) * float64(i) / float64(n-1)
				}
			}
			xc.categories[i] = strconv.FormatFloat(math.Round(v*100)/100, 'f', -1, 64)
		}
	}
	if xc.yRange && xc.yMax <= xc.yMin || xc.y2Range && xc.y2Max <= xc.y2Min {
		return nil, syntaxErr(headerLine, "y-axis range must increase")
	}
	return xc, nil
}

// xyScale resolves one value axis: its range, tick values, formatted tick
// labels and the widest label. A declared range is kept as written.
func xyScale(loc locale, lo, hi float64, declared bool, dMin, dMax float64) (float64, float64, []float64, []string, float64) {
	var ticks []float64
	if declared {
		lo, hi = dMin, dMax
		_, _, ticks = niceTicks(lo, hi, 5, false)
	} else {
		lo, hi, ticks = niceTicks(lo, hi, 5, true)
	}
	step := 1.0
	if len(ticks) > 1 {
		step = ticks[1] - ticks[0]
	}
	dec := decimalsFor(step)
	labels := make([]string, len(ticks))
	width := 0.0
	for i, t := range ticks {
		labels[i] = loc.number(t, dec)
		width = math.Max(width, textWidth(labels[i], smallFont))
	}
	return lo, hi, ticks, labels, width
}

// axisTitle splits an optional leading title (quoted, or one bare word that
// is not a range or list) from the rest.
func axisTitle(s string) (string, string) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "\"") {
		if end := strings.IndexByte(s[1:], '"'); end >= 0 {
			return s[1 : end+1], strings.TrimSpace(s[end+2:])
		}
		return cleanLabel(s), ""
	}
	if s == "" || strings.HasPrefix(s, "[") {
		return "", s
	}
	word, rest := splitWord(s)
	if _, isNum := parseNumber(word); isNum || strings.Contains(word, "-->") {
		return "", s
	}
	if strings.HasPrefix(rest, "-->") {
		return "", s
	}
	return word, rest
}

func bracketList(line int, s string) ([]string, error) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "[") || !strings.HasSuffix(s, "]") {
		return nil, syntaxErr(line, "expected a [list]")
	}
	inner := strings.TrimSpace(s[1 : len(s)-1])
	if inner == "" {
		return nil, nil
	}
	parts := splitTopLevel(inner, ',')
	for i, p := range parts {
		parts[i] = cleanLabel(p)
	}
	return parts, nil
}

func axisRange(line int, s string) (float64, float64, error) {
	lo, hi, ok := strings.Cut(s, "-->")
	if !ok {
		return 0, 0, syntaxErr(line, "expected min --> max")
	}
	a, okA := parseNumber(lo)
	b, okB := parseNumber(hi)
	if !okA || !okB {
		return 0, 0, syntaxErr(line, "axis range must be numbers")
	}
	return a, b, nil
}

// niceTicks returns rounded tick values covering [lo, hi]; when expand is
// set the range grows to whole steps.
func niceTicks(lo, hi float64, target int, expand bool) (float64, float64, []float64) {
	if hi == lo {
		if hi == 0 {
			hi = 1
		} else {
			lo, hi = lo-math.Abs(lo)*0.5, hi+math.Abs(hi)*0.5
		}
	}
	step := niceStep((hi - lo) / float64(target))
	if expand {
		lo = math.Floor(lo/step+1e-9) * step
		hi = math.Ceil(hi/step-1e-9) * step
	}
	var ticks []float64
	for v := math.Ceil(lo/step-1e-9) * step; v <= hi+step*1e-9 && len(ticks) < 50; v += step {
		ticks = append(ticks, math.Round(v/step)*step)
	}
	return lo, hi, ticks
}

func niceStep(raw float64) float64 {
	if raw <= 0 || math.IsNaN(raw) || math.IsInf(raw, 0) {
		return 1
	}
	exp := math.Pow(10, math.Floor(math.Log10(raw)))
	f := raw / exp
	switch {
	case f <= 1:
		return exp
	case f <= 2:
		return 2 * exp
	case f <= 2.5:
		return 2.5 * exp
	case f <= 5:
		return 5 * exp
	}
	return 10 * exp
}

func decimalsFor(step float64) int {
	d := 0
	for d < 6 && math.Abs(step*math.Pow(10, float64(d))-math.Round(step*math.Pow(10, float64(d)))) > 1e-9 {
		d++
	}
	return d
}

func renderXYChart(headerLine int, rest string, body []srcLine, rc rctx) (*drawing, error) {
	xc, err := parseXYChart(headerLine, rest, body)
	if err != nil {
		return nil, err
	}
	loc := rc.loc
	bars := 0
	for _, s := range xc.series {
		if !s.line {
			bars++
		}
	}
	// The right-hand axis only exists for lines drawn beside bars.
	secondary := xc.y2 && bars > 0 && bars < len(xc.series)
	onRight := func(s xySeries) bool { return secondary && s.line }
	dataMin, dataMax := math.Inf(1), math.Inf(-1)
	min1, max1, min2, max2 := math.Inf(1), math.Inf(-1), math.Inf(1), math.Inf(-1)
	for _, s := range xc.series {
		for _, v := range s.values {
			dataMin, dataMax = math.Min(dataMin, v), math.Max(dataMax, v)
			if onRight(s) {
				min2, max2 = math.Min(min2, v), math.Max(max2, v)
			} else {
				min1, max1 = math.Min(min1, v), math.Max(max1, v)
			}
		}
	}
	lo, hi := math.Min(0, min1), max1
	if bars == 0 {
		lo = min1
	}
	lo, hi, ticks, tickLabels, labelW := xyScale(loc, lo, hi, xc.yRange, xc.yMin, xc.yMax)
	var lo2, hi2, labelW2 float64
	var ticks2 []float64
	var tickLabels2 []string
	if secondary {
		lo2, hi2, ticks2, tickLabels2, labelW2 = xyScale(loc, math.Min(0, min2), max2, xc.y2Range, xc.y2Min, xc.y2Max)
	}
	n := len(xc.categories)
	band := math.Max(40, float64(bars)*14+18)
	plotW := math.Max(320, float64(n)*band)
	band = plotW / float64(n)
	const plotH = 220.0
	// Two or more series always get a key, named or not: a line drawn over
	// bars with no key reads as a second measure of the same thing.
	legend := len(xc.series) > 1
	for _, s := range xc.series {
		if s.name != "" {
			legend = true
		}
	}
	top := 14.0
	if xc.yTitle != "" || secondary && xc.y2Title != "" {
		top += 28
	}
	if legend {
		top += 24
	}
	left := labelW + 16
	chars := int((band - 4) / (glyphFactor * smallFont))
	stride := 1
	if chars < 4 {
		chars = 4
		stride = int(math.Ceil(textWidth(strings.Repeat("m", 6), smallFont) / band))
	}
	catLines := make([][]string, n)
	maxLines := 1
	for i, c := range xc.categories {
		lines := wrapLabel(c, chars)
		if len(lines) > 2 {
			lines = []string{lines[0], truncateRunes(lines[1]+" "+lines[2], chars)}
		}
		catLines[i] = lines
		maxLines = max(maxLines, len(lines))
	}
	bottom := float64(maxLines)*smallLine + 14
	if xc.xTitle != "" {
		bottom += 20
	}
	right := 16.0
	if secondary {
		right = labelW2 + 16
	}
	width := left + plotW + right
	height := top + plotH + bottom + 8
	yOf := func(v float64) float64 { return top + plotH - (clamp(v, lo, hi)-lo)/(hi-lo)*plotH }
	yOf2 := func(v float64) float64 { return top + plotH - (clamp(v, lo2, hi2)-lo2)/(hi2-lo2)*plotH }
	var grid, axes, tickNodes, marks, points, legendNodes []ui.Node
	for i, t := range ticks {
		y := yOf(t)
		grid = append(grid, line("dg-grid", left, y, left+plotW, y))
		tickNodes = append(tickNodes, label(0, y-smallLine/2, left-8, smallLine, []string{tickLabels[i]}, "end", "dg-label-small dg-label-muted"))
	}
	base := yOf(math.Max(lo, math.Min(0, hi)))
	axes = append(axes, line("dg-axis", left, base, left+plotW, base), line("dg-axis", left, top, left, top+plotH))
	if secondary {
		axes = append(axes, line("dg-axis", left+plotW, top, left+plotW, top+plotH))
		for i, t := range ticks2 {
			y := yOf2(t)
			axes = append(axes, line("dg-tick", left+plotW, y, left+plotW+4, y))
			tickNodes = append(tickNodes, label(left+plotW+8, y-smallLine/2, labelW2+4, smallLine, []string{tickLabels2[i]}, "start", "dg-label-small dg-label-muted"))
		}
	}
	for i := range xc.categories {
		x := left + band*float64(i)
		axes = append(axes, line("dg-tick", x+band/2, top+plotH, x+band/2, top+plotH+4))
		if i%stride == 0 {
			tickNodes = append(tickNodes, label(x+band/2-math.Max(band, 44)/2, top+plotH+6, math.Max(band, 44), float64(len(catLines[i]))*smallLine+2, catLines[i], "", "dg-label-small"))
		}
	}
	barW := math.Min(28, band*0.72/math.Max(float64(bars), 1))
	barIdx := 0
	for si, s := range xc.series {
		cls := seriesClass(si)
		if s.line {
			var ps []point
			yv := yOf
			if onRight(s) {
				yv = yOf2
			}
			for i, v := range s.values {
				ps = append(ps, point{left + band*(float64(i)+0.5), yv(v)})
			}
			marks = append(marks, el("polyline", "dg-line "+cls, attrs{"points": pointsAttr(ps)}))
			for _, p := range ps {
				points = append(points, circle("dg-point "+cls, p.x, p.y, 3.5))
			}
			continue
		}
		for i, v := range s.values {
			x := left + band*float64(i) + band/2 - barW*float64(bars)/2 + barW*float64(barIdx)
			y := yOf(v)
			y0, y1 := math.Min(y, base), math.Max(y, base)
			marks = append(marks, rect("dg-bar "+cls, x+1, y0, barW-2, math.Max(y1-y0, 0.5), 1.5))
		}
		barIdx++
	}
	names := make([]string, len(xc.series))
	for si, s := range xc.series {
		names[si] = s.name
		switch {
		case names[si] != "":
		case onRight(s) && xc.y2Title != "":
			names[si] = xc.y2Title
		case bars == 1 && !s.line && xc.yTitle != "":
			// The only bar series is what the left axis measures.
			names[si] = xc.yTitle
		default:
			names[si] = loc.t("xy.series", "n", strconv.Itoa(si+1))
		}
	}
	var titles []ui.Node
	if xc.yTitle != "" {
		titles = append(titles, label(4, top-30, width-8, 18, []string{truncateRunes(xc.yTitle, 80)}, "start", "dg-label-small dg-label-muted"))
	}
	if secondary && xc.y2Title != "" {
		titles = append(titles, label(4, top-30, width-8, 18, []string{truncateRunes(xc.y2Title, 80)}, "end", "dg-label-small dg-label-muted"))
	}
	if xc.xTitle != "" {
		titles = append(titles, label(left, height-26, plotW, 18, []string{truncateRunes(xc.xTitle, 80)}, "", "dg-label-small dg-label-muted"))
	}
	if legend {
		x := left
		for si, s := range xc.series {
			cls := seriesClass(si)
			var sw ui.Node
			if s.line {
				sw = group("", line("dg-line "+cls, x, 19, x+16, 19), circle("dg-point "+cls, x+8, 19, 3))
			} else {
				sw = rect("dg-legend-swatch "+cls, x+2, 13, 12, 12, 2)
			}
			txt := truncateRunes(names[si], 30)
			w := textWidth(txt, smallFont) + 8
			legendNodes = append(legendNodes, group("dg-legend-item", sw, label(x+20, 11, w, smallLine+2, []string{txt}, "start", "dg-label-small")))
			x += 20 + w + 14
		}
		width = math.Max(width, x+16)
	}
	name := xc.accTitle
	if name == "" {
		name = loc.t("xy")
	}
	desc := xc.accDescr
	if desc == "" {
		parts := make([]string, len(xc.series))
		for si, s := range xc.series {
			kind := loc.t("xy.bar")
			if onRight(s) {
				kind = loc.t("xy.line.right")
			} else if s.line {
				kind = loc.t("xy.line")
			}
			parts[si] = names[si] + " (" + kind + ")"
		}
		desc = loc.t("xy.summary", "n", strconv.Itoa(n), "s", strconv.Itoa(len(xc.series)), "list", strings.Join(parts, loc.listJoin),
			"min", loc.number(dataMin, 4), "max", loc.number(dataMax, 4))
	}
	head := append([]string{loc.t("col.category")}, names...)
	rows := make([][]string, n)
	for i, c := range xc.categories {
		row := []string{plainLabel(c)}
		for _, s := range xc.series {
			if i < len(s.values) {
				row = append(row, loc.number(s.values[i], 4))
			} else {
				row = append(row, "")
			}
		}
		rows[i] = row
	}
	return &drawing{
		kind: KindXYChart, title: xc.title, name: name, desc: desc, width: width, height: height,
		body: []ui.Node{
			group("dg-grid-lines", grid...), group("dg-marks", marks...), group("dg-axes", axes...),
			group("dg-points", points...), group("dg-ticks", tickNodes...), group("dg-titles", titles...), group("dg-legend", legendNodes...),
		},
		fallback: fallbackTable(loc.t("data"), head, rows),
	}, nil
}
