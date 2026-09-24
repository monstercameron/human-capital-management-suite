package docsdiagram

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

type fcShape int

const (
	shapeRect fcShape = iota
	shapeRound
	shapeStadium
	shapeSubroutine
	shapeCylinder
	shapeCircle
	shapeDoubleCircle
	shapeDiamond
	shapeHexagon
	shapeLeanRight
	shapeLeanLeft
	shapeTrapezoid
	shapeTrapezoidAlt
	shapeAsym
)

type edgeStroke int

const (
	strokeSolid edgeStroke = iota
	strokeDotted
	strokeThick
	strokeInvisible
)

type edgeEnd int

const (
	endNone edgeEnd = iota
	endArrow
	endCircle
	endCross
)

type fcNode struct {
	id       string
	label    string
	shape    fcShape
	defined  bool // a shape or label was given somewhere
	group    int  // innermost subgraph, -1 for none
	mentions int
}

type fcEdge struct {
	from, to   int
	label      string
	stroke     edgeStroke
	start, end edgeEnd
	minlen     int
}

type fcGroup struct {
	id, title string
	parent    int
}

type flowchart struct {
	dir      string
	nodes    []*fcNode
	edges    []fcEdge
	groups   []*fcGroup
	accTitle string
	accDescr string
}

// rawEdge keeps endpoint ids until subgraph references are resolved.
type rawEdge struct {
	from, to string
	fcEdge
	line int
}

type fcParser struct {
	fc        *flowchart
	index     map[string]int
	groupIdx  map[string]int
	stack     []int
	raw       []rawEdge
	memberSet map[string]bool
}

func parseFlowchart(header srcLine, rest string, body []srcLine) (*flowchart, error) {
	dir := strings.ToUpper(strings.TrimSpace(strings.TrimSuffix(rest, ";")))
	switch dir {
	case "", "TD", "TB":
		dir = "TB"
	case "BT", "LR", "RL":
	default:
		return nil, syntaxErr(header.n, "unknown direction %q", truncateRunes(rest, 20))
	}
	p := &fcParser{
		fc:        &flowchart{dir: dir},
		index:     map[string]int{},
		groupIdx:  map[string]int{},
		memberSet: map[string]bool{},
	}
	for _, ln := range body {
		for _, stmt := range splitStatements(ln.text) {
			if err := p.statement(ln.n, stmt); err != nil {
				return nil, err
			}
		}
	}
	if err := p.finish(); err != nil {
		return nil, err
	}
	return p.fc, nil
}

// splitStatements splits a line on ';' outside quotes and brackets.
func splitStatements(s string) []string {
	var out []string
	depth := 0
	inQuote := false
	start := 0
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '"':
			inQuote = !inQuote
		case inQuote:
		case c == '[' || c == '(' || c == '{':
			depth++
		case c == ']' || c == ')' || c == '}':
			if depth > 0 {
				depth--
			}
		case c == ';' && depth == 0:
			if t := strings.TrimSpace(s[start:i]); t != "" {
				out = append(out, t)
			}
			start = i + 1
		}
	}
	if t := strings.TrimSpace(s[start:]); t != "" {
		out = append(out, t)
	}
	return out
}

func (p *fcParser) statement(line int, s string) error {
	word, rest := splitWord(s)
	switch strings.ToLower(word) {
	case "classdef", "class", "style", "linkstyle", "click", "direction", "callback":
		return nil
	case "subgraph":
		return p.openGroup(line, rest)
	case "end":
		if rest != "" {
			break
		}
		if len(p.stack) == 0 {
			return syntaxErr(line, "end without subgraph")
		}
		p.stack = p.stack[:len(p.stack)-1]
		return nil
	}
	if key, value, ok := accStatement(s); ok {
		if key == "accTitle" {
			p.fc.accTitle = value
		} else {
			p.fc.accDescr = value
		}
		return nil
	}
	return p.chain(line, s)
}

func (p *fcParser) openGroup(line int, rest string) error {
	if len(p.fc.groups) >= MaxElements {
		return tooLarge("subgraphs", MaxElements)
	}
	id, title := rest, rest
	if i := strings.IndexByte(rest, '['); i >= 0 && strings.HasSuffix(rest, "]") {
		id = strings.TrimSpace(rest[:i])
		title = cleanLabel(rest[i+1 : len(rest)-1])
	} else {
		title = cleanLabel(rest)
		id = title
	}
	if id == "" {
		id = "subgraph-" + itoa(len(p.fc.groups)+1)
	}
	parent := -1
	if len(p.stack) > 0 {
		parent = p.stack[len(p.stack)-1]
	}
	if _, dup := p.groupIdx[id]; dup {
		return syntaxErr(line, "subgraph %q is declared twice", truncateRunes(id, 40))
	}
	p.groupIdx[id] = len(p.fc.groups)
	p.stack = append(p.stack, len(p.fc.groups))
	p.fc.groups = append(p.fc.groups, &fcGroup{id: id, title: title, parent: parent})
	return nil
}

// chain parses `group (edge group)*` where a group is `node (& node)*`.
func (p *fcParser) chain(line int, s string) error {
	sc := &fcScanner{s: s, line: line}
	left, err := p.nodeGroup(sc)
	if err != nil {
		return err
	}
	for {
		sc.skipSpace()
		if sc.done() {
			return nil
		}
		edge, err := sc.edge()
		if err != nil {
			return err
		}
		right, err := p.nodeGroup(sc)
		if err != nil {
			return err
		}
		for _, a := range left {
			for _, b := range right {
				if len(p.raw) >= MaxElements {
					return tooLarge("edges", MaxElements)
				}
				p.raw = append(p.raw, rawEdge{from: a, to: b, fcEdge: edge, line: line})
			}
		}
		left = right
	}
}

func (p *fcParser) nodeGroup(sc *fcScanner) ([]string, error) {
	var ids []string
	for {
		sc.skipSpace()
		id, label, shape, hasShape, err := sc.node()
		if err != nil {
			return nil, err
		}
		if err := p.mention(id, label, shape, hasShape); err != nil {
			return nil, err
		}
		ids = append(ids, id)
		sc.skipSpace()
		if sc.peek() != '&' {
			return ids, nil
		}
		sc.i++
	}
}

func (p *fcParser) mention(id, label string, shape fcShape, hasShape bool) error {
	idx, ok := p.index[id]
	if !ok {
		if len(p.fc.nodes) >= MaxElements {
			return tooLarge("nodes", MaxElements)
		}
		idx = len(p.fc.nodes)
		p.index[id] = idx
		p.fc.nodes = append(p.fc.nodes, &fcNode{id: id, label: id, group: -1})
	}
	n := p.fc.nodes[idx]
	n.mentions++
	if hasShape {
		n.shape = shape
		n.label = label
		n.defined = true
	}
	if len(p.stack) > 0 && !p.memberSet[id] {
		p.memberSet[id] = true
		n.group = p.stack[len(p.stack)-1]
	}
	return nil
}

// finish resolves edges whose endpoint names a subgraph and drops the
// placeholder nodes those references created.
func (p *fcParser) finish() error {
	groupRef := map[int]int{} // node index -> group index
	for idx, n := range p.fc.nodes {
		if g, ok := p.groupIdx[n.id]; ok && !n.defined && n.group != g {
			groupRef[idx] = g
		}
	}
	// Members per group including nested groups, in node order.
	members := make([][]int, len(p.fc.groups))
	for idx, n := range p.fc.nodes {
		if _, isRef := groupRef[idx]; isRef {
			continue
		}
		for g := n.group; g >= 0; g = p.fc.groups[g].parent {
			members[g] = append(members[g], idx)
		}
	}
	remap := make([]int, len(p.fc.nodes))
	var kept []*fcNode
	for idx, n := range p.fc.nodes {
		if _, isRef := groupRef[idx]; isRef {
			remap[idx] = -1
			continue
		}
		remap[idx] = len(kept)
		kept = append(kept, n)
	}
	resolve := func(id string, first bool) int {
		idx := p.index[id]
		if g, isRef := groupRef[idx]; isRef {
			m := members[g]
			if len(m) == 0 {
				return -1
			}
			if first {
				return remap[m[0]]
			}
			return remap[m[len(m)-1]]
		}
		return remap[idx]
	}
	for _, r := range p.raw {
		from, to := resolve(r.from, false), resolve(r.to, true)
		if from < 0 || to < 0 {
			continue
		}
		e := r.fcEdge
		e.from, e.to = from, to
		p.fc.edges = append(p.fc.edges, e)
	}
	p.fc.nodes = kept
	return nil
}

type fcScanner struct {
	s    string
	i    int
	line int
}

func (sc *fcScanner) done() bool { return sc.i >= len(sc.s) }

func (sc *fcScanner) peek() byte {
	if sc.i >= len(sc.s) {
		return 0
	}
	return sc.s[sc.i]
}

func (sc *fcScanner) at(off int) byte {
	if sc.i+off >= len(sc.s) || sc.i+off < 0 {
		return 0
	}
	return sc.s[sc.i+off]
}

func (sc *fcScanner) skipSpace() {
	for sc.i < len(sc.s) && (sc.s[sc.i] == ' ' || sc.s[sc.i] == '\t') {
		sc.i++
	}
}

func (sc *fcScanner) errf(format string, args ...any) error {
	return syntaxErr(sc.line, format, args...)
}

func isIDRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// node reads an id and its optional shape.
func (sc *fcScanner) node() (id, label string, shape fcShape, hasShape bool, err error) {
	start := sc.i
	for sc.i < len(sc.s) {
		r, size := utf8.DecodeRuneInString(sc.s[sc.i:])
		if isIDRune(r) {
			sc.i += size
			continue
		}
		// A dash joins an id only when a name character follows, so that
		// A-->B still reads as an edge.
		if r == '-' {
			next, _ := utf8.DecodeRuneInString(sc.s[sc.i+1:])
			if sc.i+1 < len(sc.s) && isIDRune(next) {
				sc.i++
				continue
			}
		}
		break
	}
	id = sc.s[start:sc.i]
	if id == "" {
		return "", "", 0, false, sc.errf("expected a node name near %q", truncateRunes(sc.s[sc.i:], 20))
	}
	label, shape, hasShape, err = sc.shape()
	if err != nil {
		return "", "", 0, false, err
	}
	// :::className suffix
	if strings.HasPrefix(sc.s[sc.i:], ":::") {
		sc.i += 3
		for sc.i < len(sc.s) {
			r, size := utf8.DecodeRuneInString(sc.s[sc.i:])
			if !isIDRune(r) && r != '-' {
				break
			}
			sc.i += size
		}
	}
	if hasShape && label == "" {
		label = " "
	}
	return id, label, shape, hasShape, nil
}

var shapeOpeners = []struct {
	open, close string
	shape       fcShape
}{
	{"(((", ")))", shapeDoubleCircle},
	{"((", "))", shapeCircle},
	{"([", "])", shapeStadium},
	{"(", ")", shapeRound},
	{"[[", "]]", shapeSubroutine},
	{"[(", ")]", shapeCylinder},
	{"[/", "/]", shapeLeanRight},
	{"[\\", "\\]", shapeLeanLeft},
	{"[", "]", shapeRect},
	{"{{", "}}", shapeHexagon},
	{"{", "}", shapeDiamond},
	{">", "]", shapeAsym},
}

func (sc *fcScanner) shape() (string, fcShape, bool, error) {
	rest := sc.s[sc.i:]
	if strings.HasPrefix(rest, "@{") {
		end := strings.IndexByte(rest, '}')
		if end < 0 {
			return "", 0, false, sc.errf("unclosed @{ shape")
		}
		body := rest[2:end]
		sc.i += end + 1
		label := ""
		if i := strings.Index(body, "label:"); i >= 0 {
			v := strings.TrimSpace(body[i+6:])
			if j := indexOutsideQuotes(v, ','); j >= 0 {
				v = v[:j]
			}
			label = cleanLabel(v)
		}
		shape := shapeRect
		if strings.Contains(body, "diamond") || strings.Contains(body, "decision") {
			shape = shapeDiamond
		} else if strings.Contains(body, "circle") {
			shape = shapeCircle
		} else if strings.Contains(body, "rounded") || strings.Contains(body, "stadium") {
			shape = shapeRound
		}
		return label, shape, label != "", nil
	}
	for _, op := range shapeOpeners {
		if !strings.HasPrefix(rest, op.open) {
			continue
		}
		inner := rest[len(op.open):]
		shape := op.shape
		var text string
		var consumed int
		if strings.HasPrefix(strings.TrimLeft(inner, " "), "\"") {
			lead := len(inner) - len(strings.TrimLeft(inner, " "))
			q := inner[lead+1:]
			endQ := strings.IndexByte(q, '"')
			if endQ < 0 {
				return "", 0, false, sc.errf("unclosed quote in node text")
			}
			text = q[:endQ]
			after := q[endQ+1:]
			trimmed := strings.TrimLeft(after, " ")
			closeLen, alt, ok := matchCloser(trimmed, op.close, shape)
			if !ok {
				return "", 0, false, sc.errf("expected %q after node text", op.close)
			}
			shape = alt
			consumed = len(op.open) + lead + 1 + endQ + 1 + (len(after) - len(trimmed)) + closeLen
		} else {
			idx, closeLen, alt := findCloser(inner, op.close, shape)
			if idx < 0 {
				return "", 0, false, sc.errf("expected %q to close node text", op.close)
			}
			shape = alt
			text = inner[:idx]
			consumed = len(op.open) + idx + closeLen
		}
		sc.i += consumed
		return cleanLabel(text), shape, true, nil
	}
	return "", 0, false, nil
}

// matchCloser checks for the closer at the start of s. The slanted shapes
// accept either slant as the closer, which selects the trapezoid variants.
func matchCloser(s, closer string, shape fcShape) (int, fcShape, bool) {
	if strings.HasPrefix(s, closer) {
		return len(closer), shape, true
	}
	switch shape {
	case shapeLeanRight:
		if strings.HasPrefix(s, "\\]") {
			return 2, shapeTrapezoid, true
		}
	case shapeLeanLeft:
		if strings.HasPrefix(s, "/]") {
			return 2, shapeTrapezoidAlt, true
		}
	}
	return 0, shape, false
}

func findCloser(s, closer string, shape fcShape) (int, int, fcShape) {
	idx := strings.Index(s, closer)
	alt := ""
	altShape := shape
	switch shape {
	case shapeLeanRight:
		alt, altShape = "\\]", shapeTrapezoid
	case shapeLeanLeft:
		alt, altShape = "/]", shapeTrapezoidAlt
	}
	if alt != "" {
		if j := strings.Index(s, alt); j >= 0 && (idx < 0 || j < idx) {
			return j, len(alt), altShape
		}
	}
	return idx, len(closer), shape
}

func indexOutsideQuotes(s string, c byte) int {
	inQuote := false
	for i := 0; i < len(s); i++ {
		if s[i] == '"' {
			inQuote = !inQuote
		} else if !inQuote && s[i] == c {
			return i
		}
	}
	return -1
}

func isEdgeBody(c byte) bool { return c == '-' || c == '=' || c == '.' }

// edge reads one link operator with its optional label.
func (sc *fcScanner) edge() (fcEdge, error) {
	e := fcEdge{minlen: 1}
	if strings.HasPrefix(sc.s[sc.i:], "~~~") {
		for sc.peek() == '~' {
			sc.i++
		}
		e.stroke = strokeInvisible
		return e, sc.pipeLabel(&e)
	}
	switch c := sc.peek(); {
	case c == '<' && isEdgeBody(sc.at(1)):
		e.start = endArrow
		sc.i++
	case (c == 'o' || c == 'x') && (sc.at(1) == '-' || sc.at(1) == '=') && isEdgeBody(sc.at(2)):
		if c == 'o' {
			e.start = endCircle
		} else {
			e.start = endCross
		}
		sc.i++
	}
	bodyStart := sc.i
	for sc.i < len(sc.s) && isEdgeBody(sc.s[sc.i]) {
		sc.i++
	}
	body := sc.s[bodyStart:sc.i]
	if len(body) < 2 || body[0] == '.' {
		return e, sc.errf("expected a link such as --> near %q", truncateRunes(sc.s[bodyStart:], 20))
	}
	head := sc.headEnd()
	// `-- text -->` form: an opener with no head followed by label text.
	if head == endNone && (body == "--" || body == "==" || body == "-.") && (sc.peek() == ' ' || sc.peek() == '\t') {
		return e, sc.inlineLabel(&e, body)
	}
	e.end = head
	if err := classifyBody(&e, body, head != endNone); err != nil {
		return e, sc.errf("%s", err.Error())
	}
	return e, sc.pipeLabel(&e)
}

func (sc *fcScanner) headEnd() edgeEnd {
	switch sc.peek() {
	case '>':
		sc.i++
		return endArrow
	case 'o', 'x':
		next, _ := utf8.DecodeRuneInString(sc.s[min(sc.i+1, len(sc.s)):])
		if sc.i+1 >= len(sc.s) || !isIDRune(next) {
			c := sc.peek()
			sc.i++
			if c == 'o' {
				return endCircle
			}
			return endCross
		}
	}
	return endNone
}

type bodyError string

func (b bodyError) Error() string { return string(b) }

func classifyBody(e *fcEdge, body string, headed bool) error {
	dashes := strings.Count(body, "-")
	eqs := strings.Count(body, "=")
	dots := strings.Count(body, ".")
	switch {
	case eqs > 0 && dashes == 0 && dots == 0:
		e.stroke = strokeThick
		e.minlen = lengthFrom(eqs, headed)
	case dots > 0 && eqs == 0:
		e.stroke = strokeDotted
		e.minlen = dots
	case eqs == 0 && dots == 0:
		e.stroke = strokeSolid
		e.minlen = lengthFrom(dashes, headed)
	default:
		return bodyError("unrecognised link " + truncateRunes(body, 20))
	}
	if e.minlen < 1 {
		e.minlen = 1
	}
	if e.minlen > 8 {
		e.minlen = 8
	}
	return nil
}

func lengthFrom(n int, headed bool) int {
	if headed {
		return n - 1
	}
	return n - 2
}

// inlineLabel reads `text -->` after an opener such as `--`.
func (sc *fcScanner) inlineLabel(e *fcEdge, opener string) error {
	rest := sc.s[sc.i:]
	var closerAt int
	switch opener {
	case "==":
		closerAt = strings.Index(rest, "==")
	case "-.":
		closerAt = strings.Index(rest, ".-")
	default:
		closerAt = strings.Index(rest, "--")
	}
	if closerAt < 0 {
		return sc.errf("link label is not closed with a link")
	}
	text := strings.TrimSpace(rest[:closerAt])
	sc.i += closerAt
	start := sc.i
	for sc.i < len(sc.s) && isEdgeBody(sc.s[sc.i]) {
		sc.i++
	}
	body := sc.s[start:sc.i]
	head := sc.headEnd()
	e.end = head
	if opener == "-." {
		body = "." + strings.TrimLeft(body, ".")
	}
	if err := classifyBody(e, body, head != endNone); err != nil {
		return sc.errf("%s", err.Error())
	}
	if opener == "-." {
		e.stroke = strokeDotted
	}
	e.label = cleanLabel(text)
	return nil
}

func (sc *fcScanner) pipeLabel(e *fcEdge) error {
	sc.skipSpace()
	if sc.peek() != '|' {
		return nil
	}
	end := strings.IndexByte(sc.s[sc.i+1:], '|')
	if end < 0 {
		return sc.errf("link label is missing its closing |")
	}
	e.label = cleanLabel(sc.s[sc.i+1 : sc.i+1+end])
	sc.i += end + 2
	return nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
