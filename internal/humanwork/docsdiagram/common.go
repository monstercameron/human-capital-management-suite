package docsdiagram

import (
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type attrs = map[string]any

// el builds one element; attribute values are numbers or plain strings.
func el(tag, class string, a attrs, children ...ui.Node) ui.Node {
	return html.Tag(tag, html.Props{Class: class, Raw: a}, children...)
}

// num formats a coordinate with at most one decimal.
func num(v float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return "0"
	}
	r := math.Round(v*10) / 10
	if r == 0 {
		return "0"
	}
	return strconv.FormatFloat(r, 'f', -1, 64)
}

func ceilTo(v, step float64) float64 { return math.Ceil(v/step) * step }

func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }

// Text metrics. Labels are laid out from an estimate of the rendered width:
// 0.58em for most glyphs, 1em for East Asian wide glyphs, less for spaces and
// narrow punctuation. The label boxes centre their text, so an estimate that
// is slightly off shifts nothing.
const (
	nodeFont    = 14.0
	nodeLine    = 18.0
	smallFont   = 12.0
	smallLine   = 15.0
	wrapChars   = 20
	glyphFactor = 0.58
)

func runeWidth(r rune) float64 {
	switch {
	case r == ' ' || r == '.' || r == ',' || r == ':' || r == ';' || r == '\'' || r == '|' || r == '!' || r == 'i' || r == 'l':
		return 0.3
	case isWide(r):
		return 1.0
	case unicode.IsUpper(r) || r == 'm' || r == 'w' || r == 'W' || r == 'M':
		return 0.7
	}
	return glyphFactor
}

func isWide(r rune) bool {
	return unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hangul, r) || unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) || (r >= 0xFF00 && r <= 0xFFEF) || (r >= 0x1F300 && r <= 0x1FAFF)
}

func textWidth(s string, font float64) float64 {
	w := 0.0
	for _, r := range s {
		w += runeWidth(r)
	}
	return w * font
}

func maxLineWidth(lines []string, font float64) float64 {
	w := 0.0
	for _, l := range lines {
		w = math.Max(w, textWidth(l, font))
	}
	return w
}

// truncateRunes shortens s to n runes, marking the cut with an ellipsis.
func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

// wrapLabel splits a label into lines of about limit characters, honouring
// explicit breaks (<br>, <br/>, \n) and splitting over-long words.
func wrapLabel(s string, limit int) []string {
	s = truncateRunes(s, maxLabelRunes)
	var out []string
	for _, para := range splitBreaks(s) {
		words := strings.Fields(para)
		line := ""
		for _, w := range words {
			for utf8.RuneCountInString(w) > limit {
				if line != "" {
					out = append(out, line)
					line = ""
				}
				r := []rune(w)
				out = append(out, string(r[:limit]))
				w = string(r[limit:])
			}
			switch {
			case line == "":
				line = w
			case utf8.RuneCountInString(line)+1+utf8.RuneCountInString(w) <= limit:
				line += " " + w
			default:
				out = append(out, line)
				line = w
			}
		}
		if line != "" || len(words) == 0 {
			out = append(out, line)
		}
	}
	// Drop blank lines at the ends; keep at least one line.
	for len(out) > 1 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	for len(out) > 1 && out[0] == "" {
		out = out[1:]
	}
	if len(out) == 0 {
		out = []string{""}
	}
	return out
}

func splitBreaks(s string) []string {
	lower := asciiLower(s)
	var parts []string
	for {
		idx := strings.Index(lower, "<br")
		if idx < 0 {
			break
		}
		end := strings.IndexByte(lower[idx:], '>')
		if end < 0 {
			break
		}
		parts = append(parts, s[:idx])
		s = s[idx+end+1:]
		lower = lower[idx+end+1:]
	}
	parts = append(parts, s)
	var out []string
	for _, p := range parts {
		out = append(out, strings.Split(p, "\n")...)
	}
	return out
}

// plainLabel is the one-line form of a label for summaries and fallbacks.
func plainLabel(s string) string {
	return strings.Join(strings.Fields(strings.Join(splitBreaks(s), " ")), " ")
}

// cleanLabel resolves Mermaid label decoration: surrounding quotes, markdown
// string backticks and the #name; entity codes.
func cleanLabel(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}
	if len(s) >= 2 && s[0] == '`' && s[len(s)-1] == '`' {
		s = s[1 : len(s)-1]
	}
	if strings.Contains(s, "#") {
		s = decodeEntities(s)
	}
	return strings.TrimSpace(s)
}

var namedEntities = map[string]string{
	"quot": "\"", "amp": "&", "lt": "<", "gt": ">", "apos": "'", "nbsp": " ", "hash": "#", "semi": ";",
}

func decodeEntities(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '#' {
			if end := strings.IndexByte(s[i:], ';'); end > 1 && end <= 10 {
				name := s[i+1 : i+end]
				if v, ok := namedEntities[strings.ToLower(name)]; ok {
					b.WriteString(v)
					i += end
					continue
				}
				if code, err := strconv.Atoi(name); err == nil && code > 0 && code < 0x110000 {
					b.WriteRune(rune(code))
					i += end
					continue
				}
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// label draws text centred (or aligned) in a box through a foreignObject.
// align is "", "start" or "end"; extra adds classes to the text block.
func label(x, y, w, h float64, lines []string, align, extra string) ui.Node {
	class := "dg-label"
	if align != "" {
		class += " dg-label-" + align
	}
	if extra != "" {
		class += " " + extra
	}
	children := make([]ui.Node, 0, len(lines))
	for _, l := range lines {
		children = append(children, html.Tag("span", html.Props{}, ui.Text(l)))
	}
	return el("foreignObject", "dg-fo", attrs{"x": num(x), "y": num(y), "width": num(math.Max(w, 1)), "height": num(math.Max(h, 1))},
		html.Div(html.Props{Class: class, Raw: map[string]any{"dir": "auto"}}, children...))
}

func rect(class string, x, y, w, h, r float64) ui.Node {
	a := attrs{"x": num(x), "y": num(y), "width": num(math.Max(w, 0)), "height": num(math.Max(h, 0))}
	if r > 0 {
		a["rx"] = num(r)
		a["ry"] = num(r)
	}
	return el("rect", class, a)
}

func line(class string, x1, y1, x2, y2 float64) ui.Node {
	return el("line", class, attrs{"x1": num(x1), "y1": num(y1), "x2": num(x2), "y2": num(y2)})
}

func circle(class string, cx, cy, r float64) ui.Node {
	return el("circle", class, attrs{"cx": num(cx), "cy": num(cy), "r": num(r)})
}

func path(class, d string, extra attrs) ui.Node {
	a := attrs{"d": d}
	for k, v := range extra {
		a[k] = v
	}
	return el("path", class, a)
}

func group(class string, children ...ui.Node) ui.Node {
	return el("g", class, nil, children...)
}

type point struct{ x, y float64 }

func pointsAttr(ps []point) string {
	parts := make([]string, len(ps))
	for i, p := range ps {
		parts[i] = num(p.x) + "," + num(p.y)
	}
	return strings.Join(parts, " ")
}

func seriesClass(i int) string { return "series-" + strconv.Itoa(i%8) }

// arrowMarker defines a filled arrowhead under an id unique to its diagram.
func arrowMarker(id, class string) ui.Node {
	return el("marker", "", attrs{
		"id": id, "viewBox": "0 0 10 10", "refX": "9", "refY": "5",
		"markerWidth": "7", "markerHeight": "7", "markerUnits": "userSpaceOnUse", "orient": "auto-start-reverse",
	}, path(class, "M0,0 L10,5 L0,10 z", nil))
}

// fallbackTable builds a plain HTML table for the text alternative.
func fallbackTable(caption string, head []string, rows [][]string) ui.Node {
	headCells := make([]ui.Node, len(head))
	for i, h := range head {
		headCells[i] = html.Tag("th", html.Props{Raw: map[string]any{"scope": "col"}}, ui.Text(h))
	}
	bodyRows := make([]ui.Node, len(rows))
	for i, r := range rows {
		cells := make([]ui.Node, len(r))
		for j, c := range r {
			cells[j] = html.Tag("td", html.Props{Raw: map[string]any{"dir": "auto"}}, ui.Text(c))
		}
		bodyRows[i] = html.Tag("tr", html.Props{}, cells...)
	}
	return html.Tag("table", html.Props{},
		html.Tag("caption", html.Props{}, ui.Text(caption)),
		html.Tag("thead", html.Props{}, html.Tag("tr", html.Props{}, headCells...)),
		html.Tag("tbody", html.Props{}, bodyRows...),
	)
}

// fallbackList builds a plain list for the text alternative.
func fallbackList(caption string, ordered bool, items []string) ui.Node {
	lis := make([]ui.Node, len(items))
	for i, it := range items {
		lis[i] = html.Tag("li", html.Props{Raw: map[string]any{"dir": "auto"}}, ui.Text(it))
	}
	tag := "ul"
	if ordered {
		tag = "ol"
	}
	return html.Div(html.Props{},
		html.P(html.Props{}, ui.Text(caption)),
		html.Tag(tag, html.Props{}, lis...),
	)
}

// parseNumber reads a plain decimal number (Mermaid data never uses locale
// separators).
func parseNumber(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > 1e15 {
		return 0, false
	}
	return v, true
}

// splitTopLevel splits on sep outside double quotes.
func splitTopLevel(s string, sep byte) []string {
	var parts []string
	inQuote := false
	start := 0
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '"':
			inQuote = !inQuote
		case !inQuote && s[i] == sep:
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	return append(parts, s[start:])
}

// titleRest reads a `title ...` statement.
func titleRest(s string) (string, bool) {
	rest, ok := keywordRest(s, "title")
	if !ok {
		return "", false
	}
	return cleanLabel(rest), true
}

// accStatement recognises accTitle/accDescr lines, which the renderer uses
// for the accessible name and description.
func accStatement(s string) (key, value string, ok bool) {
	for _, k := range []string{"accTitle", "accDescr"} {
		if rest, found := keywordRest(s, k); found {
			rest = strings.TrimSpace(strings.TrimPrefix(rest, ":"))
			rest = strings.TrimSuffix(strings.TrimPrefix(rest, "{"), "}")
			return k, strings.TrimSpace(rest), true
		}
	}
	return "", "", false
}

// joinSummary lists up to max items then counts the rest.
func joinSummary(items []string, sep string, max int, loc locale) string {
	if len(items) <= max {
		return strings.Join(items, sep)
	}
	return strings.Join(items[:max], sep) + sep + loc.more(len(items)-max)
}

// asciiLower lowercases A-Z only, so byte offsets found in the result are
// valid in the input even when the input is not valid UTF-8.
func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}
