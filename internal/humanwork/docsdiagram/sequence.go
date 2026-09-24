package docsdiagram

import (
	"math"
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type seqParticipant struct {
	id, label string
	actor     bool
}

type seqEventKind int

const (
	evMessage seqEventKind = iota
	evNote
	evBlockStart
	evBlockElse
	evBlockEnd
	evActivate
	evDeactivate
)

type seqHead int

const (
	headNone seqHead = iota
	headFilled
	headCross
	headAsync
)

type seqEvent struct {
	kind       seqEventKind
	from, to   int
	text       string
	dotted     bool
	head       seqHead
	both       bool
	activate   bool // + on the target
	deactivate bool // - on the source
	noteSide   string
	block      string
	number     int
}

type sequence struct {
	parts    []seqParticipant
	index    map[string]int
	events   []seqEvent
	title    string
	accTitle string
	accDescr string
	messages int
}

var seqArrows = []struct {
	tok    string
	dotted bool
	head   seqHead
	both   bool
}{
	{"<<-->>", true, headFilled, true},
	{"<<->>", false, headFilled, true},
	{"-->>", true, headFilled, false},
	{"->>", false, headFilled, false},
	{"--x", true, headCross, false},
	{"--)", true, headAsync, false},
	{"-->", true, headNone, false},
	{"-x", false, headCross, false},
	{"-)", false, headAsync, false},
	{"->", false, headNone, false},
}

var seqBlockKinds = map[string]bool{"loop": true, "alt": true, "opt": true, "par": true, "critical": true, "break": true, "rect": true}
var seqElseKinds = map[string]string{"else": "alt", "and": "par", "option": "critical"}

func parseSequence(body []srcLine) (*sequence, error) {
	sq := &sequence{index: map[string]int{}}
	var stack []string
	autonumber := false
	next, step := 1, 1
	for _, ln := range body {
		s := ln.text
		word, rest := splitWord(s)
		lw := strings.ToLower(word)
		if lw == "create" {
			s = rest
			word, rest = splitWord(s)
			lw = strings.ToLower(word)
		}
		switch {
		case lw == "participant" || lw == "actor":
			id, lbl := rest, rest
			if i := strings.Index(rest, " as "); i >= 0 {
				id, lbl = strings.TrimSpace(rest[:i]), strings.TrimSpace(rest[i+4:])
			}
			id = cleanLabel(id)
			if id == "" {
				return nil, syntaxErr(ln.n, "participant needs a name")
			}
			idx, err := sq.participant(id)
			if err != nil {
				return nil, err
			}
			sq.parts[idx].label = cleanLabel(lbl)
			sq.parts[idx].actor = lw == "actor"
		case lw == "autonumber":
			autonumber = true
			f := strings.Fields(rest)
			if len(f) > 0 {
				if v, err := strconv.Atoi(f[0]); err == nil {
					next = v
				}
			}
			if len(f) > 1 {
				if v, err := strconv.Atoi(f[1]); err == nil && v != 0 {
					step = v
				}
			}
			if strings.EqualFold(rest, "off") {
				autonumber = false
			}
		case lw == "title" || strings.HasPrefix(lw, "title:"):
			sq.title = cleanLabel(strings.TrimPrefix(strings.TrimSpace(s[5:]), ":"))
		case lw == "destroy" || lw == "link" || lw == "links" || lw == "properties" || lw == "details":
		case lw == "activate" || lw == "deactivate":
			idx, err := sq.participant(cleanLabel(rest))
			if err != nil {
				return nil, err
			}
			kind := evActivate
			if lw == "deactivate" {
				kind = evDeactivate
			}
			sq.events = append(sq.events, seqEvent{kind: kind, from: idx})
		case lw == "note":
			ev, err := sq.note(ln.n, rest)
			if err != nil {
				return nil, err
			}
			sq.events = append(sq.events, ev)
		case lw == "box":
			stack = append(stack, "box")
		case seqBlockKinds[lw]:
			stack = append(stack, lw)
			text := rest
			if lw == "rect" {
				text = ""
			}
			sq.events = append(sq.events, seqEvent{kind: evBlockStart, block: lw, text: cleanLabel(text)})
		case seqElseKinds[lw] != "":
			if len(stack) == 0 || stack[len(stack)-1] != seqElseKinds[lw] {
				return nil, syntaxErr(ln.n, "%s outside %s", lw, seqElseKinds[lw])
			}
			sq.events = append(sq.events, seqEvent{kind: evBlockElse, block: lw, text: cleanLabel(rest)})
		case lw == "end":
			if len(stack) == 0 {
				return nil, syntaxErr(ln.n, "end without a block")
			}
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if top != "box" {
				sq.events = append(sq.events, seqEvent{kind: evBlockEnd, block: top})
			}
		default:
			if key, value, ok := accStatement(s); ok {
				if key == "accTitle" {
					sq.accTitle = value
				} else {
					sq.accDescr = value
				}
				continue
			}
			ev, err := sq.message(ln.n, s)
			if err != nil {
				return nil, err
			}
			if autonumber {
				ev.number = next
				next += step
			}
			sq.events = append(sq.events, ev)
			sq.messages++
			if sq.messages > MaxElements {
				return nil, tooLarge("messages", MaxElements)
			}
		}
		if len(sq.events) > 4*MaxElements {
			return nil, tooLarge("statements", 4*MaxElements)
		}
	}
	for len(stack) > 0 {
		if top := stack[len(stack)-1]; top != "box" {
			sq.events = append(sq.events, seqEvent{kind: evBlockEnd, block: top})
		}
		stack = stack[:len(stack)-1]
	}
	if len(sq.parts) == 0 {
		return nil, ErrEmpty
	}
	return sq, nil
}

func (sq *sequence) participant(id string) (int, error) {
	if idx, ok := sq.index[id]; ok {
		return idx, nil
	}
	if len(sq.parts) >= MaxElements {
		return 0, tooLarge("participants", MaxElements)
	}
	sq.index[id] = len(sq.parts)
	sq.parts = append(sq.parts, seqParticipant{id: id, label: id})
	return len(sq.parts) - 1, nil
}

func (sq *sequence) note(line int, rest string) (seqEvent, error) {
	colon := strings.IndexByte(rest, ':')
	if colon < 0 {
		return seqEvent{}, syntaxErr(line, "note needs a ':' before its text")
	}
	where, text := strings.TrimSpace(rest[:colon]), strings.TrimSpace(rest[colon+1:])
	ev := seqEvent{kind: evNote, text: cleanLabel(text)}
	lower := strings.ToLower(where)
	var who string
	switch {
	case strings.HasPrefix(lower, "right of "):
		ev.noteSide, who = "right", where[9:]
	case strings.HasPrefix(lower, "left of "):
		ev.noteSide, who = "left", where[8:]
	case strings.HasPrefix(lower, "over "):
		ev.noteSide, who = "over", where[5:]
	default:
		return seqEvent{}, syntaxErr(line, "note position must be left of, right of or over")
	}
	names := strings.Split(who, ",")
	if len(names) > 2 {
		return seqEvent{}, syntaxErr(line, "note spans at most two participants")
	}
	var err error
	if ev.from, err = sq.participant(cleanLabel(names[0])); err != nil {
		return seqEvent{}, err
	}
	ev.to = ev.from
	if len(names) == 2 {
		if ev.to, err = sq.participant(cleanLabel(names[1])); err != nil {
			return seqEvent{}, err
		}
	}
	if strings.TrimSpace(names[0]) == "" {
		return seqEvent{}, syntaxErr(line, "note needs a participant")
	}
	return ev, nil
}

func (sq *sequence) message(line int, s string) (seqEvent, error) {
	pos, tok := -1, -1
	for i := 0; i < len(s) && pos < 0; i++ {
		if s[i] != '-' && s[i] != '<' {
			continue
		}
		for t, a := range seqArrows {
			if strings.HasPrefix(s[i:], a.tok) {
				pos, tok = i, t
				break
			}
		}
	}
	if pos < 0 {
		return seqEvent{}, syntaxErr(line, "unrecognised statement %q", truncateRunes(s, 40))
	}
	a := seqArrows[tok]
	from := strings.TrimSpace(s[:pos])
	rest := strings.TrimSpace(s[pos+len(a.tok):])
	ev := seqEvent{kind: evMessage, dotted: a.dotted, head: a.head, both: a.both}
	if strings.HasPrefix(rest, "+") {
		ev.activate = true
		rest = strings.TrimSpace(rest[1:])
	} else if strings.HasPrefix(rest, "-") {
		ev.deactivate = true
		rest = strings.TrimSpace(rest[1:])
	}
	to, text, _ := strings.Cut(rest, ":")
	to = cleanLabel(to)
	if from == "" || to == "" {
		return seqEvent{}, syntaxErr(line, "message needs a sender and a receiver")
	}
	var err error
	if ev.from, err = sq.participant(cleanLabel(from)); err != nil {
		return seqEvent{}, err
	}
	if ev.to, err = sq.participant(to); err != nil {
		return seqEvent{}, err
	}
	ev.text = cleanLabel(text)
	return ev, nil
}

// Sequence layout constants.
const (
	seqMargin     = 16.0
	seqBoxMinW    = 84.0
	seqActorH     = 34.0
	seqGap        = 28.0
	seqSelfW      = 34.0
	seqSelfH      = 22.0
	seqNoteWrap   = 28
	seqMsgWrap    = 40
	seqFrameTab   = 22.0
	seqActivation = 10.0
)

type seqFrame struct {
	kind   string
	text   string
	top    float64
	elses  []seqElse
	x0, x1 float64
	have   bool
	depth  int
}

type seqElse struct {
	y    float64
	text string
}

func renderSequence(headerLine int, rest string, body []srcLine, rc rctx) (*drawing, error) {
	_ = headerLine
	_ = rest
	sq, err := parseSequence(body)
	if err != nil {
		return nil, err
	}
	loc := rc.loc
	n := len(sq.parts)
	boxW := make([]float64, n)
	boxLines := make([][]string, n)
	boxH := 0.0
	for i, p := range sq.parts {
		lines := wrapLabel(p.label, 18)
		boxLines[i] = lines
		boxW[i] = math.Max(maxLineWidth(lines, nodeFont)+24, seqBoxMinW)
		h := float64(len(lines))*nodeLine + 16
		if p.actor {
			h += seqActorH
		}
		boxH = math.Max(boxH, h)
	}
	// Minimum distances between neighbouring lifelines.
	gap := make([]float64, n) // gap[i] = distance from i-1 to i
	for i := 1; i < n; i++ {
		gap[i] = (boxW[i-1]+boxW[i])/2 + seqGap
	}
	leftOver, rightOver := 0.0, 0.0
	msgLines := make([][]string, len(sq.events))
	for ei, ev := range sq.events {
		switch ev.kind {
		case evMessage:
			lines := wrapLabel(ev.text, seqMsgWrap)
			if ev.text == "" {
				lines = nil
			}
			msgLines[ei] = lines
			need := maxLineWidth(lines, smallFont) + 24
			if ev.number > 0 {
				need += 20
			}
			a, b := min(ev.from, ev.to), max(ev.from, ev.to)
			if a == b {
				need += seqSelfW + 8
				if a+1 < n {
					gap[a+1] = math.Max(gap[a+1], need+boxW[a+1]/2)
				} else {
					rightOver = math.Max(rightOver, need-boxW[a]/2)
				}
				continue
			}
			spanned := 0.0
			for k := a + 1; k <= b; k++ {
				spanned += gap[k]
			}
			if spanned < need {
				gap[b] += need - spanned
			}
		case evNote:
			lines := wrapLabel(ev.text, seqNoteWrap)
			msgLines[ei] = lines
			w := maxLineWidth(lines, smallFont) + 20
			switch ev.noteSide {
			case "right":
				if ev.from+1 < n {
					gap[ev.from+1] = math.Max(gap[ev.from+1], w+14+boxW[ev.from+1]/2)
				} else {
					rightOver = math.Max(rightOver, w+14-boxW[ev.from]/2)
				}
			case "left":
				if ev.from > 0 {
					gap[ev.from] = math.Max(gap[ev.from], w+14+boxW[ev.from-1]/2)
				} else {
					leftOver = math.Max(leftOver, w+14-boxW[0]/2)
				}
			default:
				a, b := min(ev.from, ev.to), max(ev.from, ev.to)
				spanned := 0.0
				for k := a + 1; k <= b; k++ {
					spanned += gap[k]
				}
				if half := (w - spanned) / 2; half > boxW[a]/2 {
					if a == 0 {
						leftOver = math.Max(leftOver, half-boxW[0]/2)
					}
					if b == n-1 {
						rightOver = math.Max(rightOver, half-boxW[b]/2)
					}
				}
			}
		}
	}
	cx := make([]float64, n)
	cx[0] = seqMargin + leftOver + boxW[0]/2
	for i := 1; i < n; i++ {
		cx[i] = cx[i-1] + gap[i]
	}
	width := cx[n-1] + boxW[n-1]/2 + rightOver + seqMargin

	var lifelines, frames, frameLabels, activations, arrows, notes, labels []ui.Node
	markerID := func(k string) string { return rc.id + "-" + k }
	y := seqMargin + boxH + 22
	active := make([][]float64, n) // start y per open activation
	level := func(i int) int { return len(active[i]) }
	closeActivation := func(i int, at float64) {
		if len(active[i]) == 0 {
			return
		}
		start := active[i][len(active[i])-1]
		active[i] = active[i][:len(active[i])-1]
		x := cx[i] - seqActivation/2 + float64(len(active[i]))*4
		activations = append(activations, rect("dg-activation", x, start, seqActivation, math.Max(at-start, 8), 0))
	}
	var stack []*seqFrame
	var done []*seqFrame
	extend := func(x0, x1 float64) {
		for _, f := range stack {
			if !f.have {
				f.x0, f.x1, f.have = x0, x1, true
				continue
			}
			f.x0, f.x1 = math.Min(f.x0, x0), math.Max(f.x1, x1)
		}
	}
	for ei, ev := range sq.events {
		switch ev.kind {
		case evMessage:
			lines := msgLines[ei]
			lh := float64(len(lines)) * smallLine
			y += lh + 8
			a, b := ev.from, ev.to
			if a == b {
				x := cx[a] + float64(level(a))*4 + seqActivation/2*boolf(level(a) > 0)
				d := "M" + num(x) + "," + num(y) + " H" + num(x+seqSelfW) + " V" + num(y+seqSelfH) + " H" + num(x+2)
				arrows = append(arrows, path(msgClass(ev), d, markerAttrs(ev, markerID)))
				if len(lines) > 0 {
					w := maxLineWidth(lines, smallFont) + 8
					labels = append(labels, label(x+seqSelfW+6, y-lh/2+seqSelfH/2-2, w, lh+4, lines, "start", "dg-label-small"))
				}
				extend(cx[a]-boxW[a]/2, math.Max(cx[a]+boxW[a]/2, x+seqSelfW+8+maxLineWidth(lines, smallFont)+8))
				if ev.number > 0 {
					labels = append(labels, seqNumber(x, y, ev.number))
				}
				y += seqSelfH
			} else {
				dir := 1.0
				if b < a {
					dir = -1
				}
				x1 := cx[a] + dir*seqActivation/2*boolf(level(a) > 0)
				x2 := cx[b] - dir*(seqActivation/2*boolf(level(b) > 0 || ev.activate)+1)
				arrows = append(arrows, path(msgClass(ev), "M"+num(x1)+","+num(y)+" H"+num(x2), markerAttrs(ev, markerID)))
				if len(lines) > 0 {
					lo, hi := math.Min(cx[a], cx[b]), math.Max(cx[a], cx[b])
					labels = append(labels, label(lo+4, y-lh-4, hi-lo-8, lh+2, lines, "", "dg-label-small"))
				}
				extend(math.Min(cx[a]-boxW[a]/2, cx[b]-boxW[b]/2), math.Max(cx[a]+boxW[a]/2, cx[b]+boxW[b]/2))
				if ev.number > 0 {
					labels = append(labels, seqNumber(x1, y, ev.number))
				}
			}
			if ev.activate {
				active[b] = append(active[b], y)
			}
			if ev.deactivate {
				closeActivation(a, y)
			}
			y += 16
		case evActivate:
			active[ev.from] = append(active[ev.from], y-6)
		case evDeactivate:
			closeActivation(ev.from, y-6)
		case evNote:
			lines := msgLines[ei]
			w := maxLineWidth(lines, smallFont) + 20
			h := float64(len(lines))*smallLine + 12
			var x float64
			switch ev.noteSide {
			case "right":
				x = cx[ev.from] + 12
			case "left":
				x = cx[ev.from] - 12 - w
			default:
				lo, hi := math.Min(cx[ev.from], cx[ev.to]), math.Max(cx[ev.from], cx[ev.to])
				if hi-lo+40 > w {
					w = hi - lo + 40
				}
				x = (lo+hi)/2 - w/2
			}
			y += 6
			notes = append(notes, rect("dg-note", x, y, w, h, 3), label(x, y, w, h, lines, "", "dg-label-small"))
			extend(math.Min(x-4, cx[ev.from]-boxW[ev.from]/2), math.Max(x+w+4, cx[ev.from]+boxW[ev.from]/2))
			y += h + 12
		case evBlockStart:
			y += 8
			f := &seqFrame{kind: ev.block, text: ev.text, top: y, depth: len(stack)}
			stack = append(stack, f)
			y += seqFrameTab + 14
		case evBlockElse:
			if len(stack) > 0 {
				y += 4
				f := stack[len(stack)-1]
				f.elses = append(f.elses, seqElse{y: y, text: ev.text})
				y += 22
			}
		case evBlockEnd:
			if len(stack) == 0 {
				continue
			}
			y += 6
			f := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if !f.have {
				f.x0, f.x1, f.have = cx[0]-boxW[0]/2, cx[n-1]+boxW[n-1]/2, true
			}
			pad := math.Max(18-float64(f.depth)*6, 6)
			f.x0 -= pad
			f.x1 += pad
			tab := f.kind
			tabW := textWidth(tab, smallFont) + 18
			textW := textWidth(f.text, smallFont) + 30
			if need := tabW + textW; f.x1-f.x0 < need {
				f.x1 = f.x0 + need
			}
			f.x0 = math.Max(f.x0, 2)
			shape, texts := drawFrame(f, y, tabW)
			frames = append(frames, shape)
			frameLabels = append(frameLabels, texts)
			done = append(done, f)
			extend(f.x0, f.x1)
			y += 10
		}
	}
	for i := range active {
		for len(active[i]) > 0 {
			closeActivation(i, y)
		}
	}
	for _, f := range done {
		width = math.Max(width, f.x1+seqMargin)
	}
	y += 10
	bottomTop := y
	height := bottomTop + boxH + seqMargin
	var parts []ui.Node
	for i, p := range sq.parts {
		lifelines = append(lifelines, line("dg-lifeline", cx[i], seqMargin+boxH, cx[i], bottomTop))
		parts = append(parts, seqActorBox(p, cx[i], seqMargin, boxW[i], boxH, boxLines[i]), seqActorBox(p, cx[i], bottomTop, boxW[i], boxH, boxLines[i]))
	}
	bodyNodes := []ui.Node{
		group("dg-frames", frames...),
		group("dg-lifelines", lifelines...),
		group("dg-participants", parts...),
		group("dg-activations", activations...),
		group("dg-messages", arrows...),
		group("dg-frame-labels", frameLabels...),
		group("dg-notes", notes...),
		group("dg-message-labels", labels...),
	}
	defs := []ui.Node{
		arrowMarker(markerID("arrow"), "dg-arrowhead"),
		el("marker", "", attrs{"id": markerID("open"), "viewBox": "0 0 10 10", "refX": "9", "refY": "5", "markerWidth": "8", "markerHeight": "8", "markerUnits": "userSpaceOnUse", "orient": "auto-start-reverse"},
			path("dg-arrowhead-open", "M1,1 L9,5 L1,9", nil)),
		el("marker", "", attrs{"id": markerID("cross"), "viewBox": "0 0 10 10", "refX": "8", "refY": "5", "markerWidth": "9", "markerHeight": "9", "markerUnits": "userSpaceOnUse", "orient": "auto"},
			path("dg-arrowhead-cross", "M1,1 L9,9 M9,1 L1,9", nil)),
	}
	name := sq.accTitle
	if name == "" {
		name = loc.t("sequence")
	}
	desc := sq.accDescr
	if desc == "" {
		names := make([]string, n)
		for i, p := range sq.parts {
			names[i] = plainLabel(p.label)
		}
		desc = loc.t("seq.summary", "list", joinSummary(names, loc.listJoin, 6, loc), "n", strconv.Itoa(sq.messages))
	}
	return &drawing{
		kind: KindSequence, title: sq.title, name: name, desc: desc,
		width: width, height: height, body: bodyNodes, defs: defs,
		fallback: sq.fallback(loc),
	}, nil
}

func boolf(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

func msgClass(ev seqEvent) string {
	if ev.dotted {
		return "dg-msg dg-msg-dotted"
	}
	return "dg-msg"
}

func markerAttrs(ev seqEvent, id func(string) string) attrs {
	a := attrs{}
	switch ev.head {
	case headFilled:
		a["marker-end"] = "url(#" + id("arrow") + ")"
		if ev.both {
			a["marker-start"] = "url(#" + id("arrow") + ")"
		}
	case headCross:
		a["marker-end"] = "url(#" + id("cross") + ")"
	case headAsync:
		a["marker-end"] = "url(#" + id("open") + ")"
	}
	return a
}

func seqNumber(x, y float64, n int) ui.Node {
	s := strconv.Itoa(n)
	r := math.Max(8, textWidth(s, 10)/2+4)
	return group("dg-seqnum", circle("dg-seqnum-dot", x, y, r), label(x-r, y-r, 2*r, 2*r, []string{s}, "", "dg-label-tiny dg-seqnum-text"))
}

func seqActorBox(p seqParticipant, cx, top, w, h float64, lines []string) ui.Node {
	if !p.actor {
		return group("dg-participant", rect("dg-participant-box", cx-w/2, top, w, h, 4), label(cx-w/2, top, w, h, lines, "", ""))
	}
	// Stick figure above the name.
	hy := top + 7
	parts := []ui.Node{
		circle("dg-actor-figure", cx, hy, 6),
		path("dg-actor-figure", "M"+num(cx)+","+num(hy+6)+" V"+num(hy+20)+" M"+num(cx-10)+","+num(hy+11)+" H"+num(cx+10)+
			" M"+num(cx)+","+num(hy+20)+" L"+num(cx-8)+","+num(hy+30)+" M"+num(cx)+","+num(hy+20)+" L"+num(cx+8)+","+num(hy+30), nil),
		label(cx-w/2, top+seqActorH, w, h-seqActorH, lines, "", ""),
	}
	return group("dg-participant dg-participant-actor", parts...)
}

func drawFrame(f *seqFrame, bottom, tabW float64) (ui.Node, ui.Node) {
	w := f.x1 - f.x0
	shapes := []ui.Node{rect("dg-frame", f.x0, f.top, w, bottom-f.top, 2)}
	texts := []ui.Node{
		path("dg-frame-tab", "M"+num(f.x0)+","+num(f.top)+" H"+num(f.x0+tabW)+" V"+num(f.top+seqFrameTab-6)+" L"+num(f.x0+tabW-6)+","+num(f.top+seqFrameTab)+" H"+num(f.x0)+" Z", nil),
		label(f.x0, f.top, tabW-4, seqFrameTab, []string{f.kind}, "", "dg-label-small dg-frame-kind"),
	}
	condition := func(x, y float64, text string) {
		t := "[" + truncateRunes(plainLabel(text), 80) + "]"
		tw := textWidth(t, smallFont) + 10
		texts = append(texts, rect("dg-frame-label-bg", x, y+2, tw, seqFrameTab-4, 3), label(x, y, tw, seqFrameTab, []string{t}, "", "dg-label-small"))
	}
	if f.text != "" {
		condition(f.x0+tabW+4, f.top, f.text)
	}
	for _, e := range f.elses {
		shapes = append(shapes, line("dg-frame-divider", f.x0, e.y, f.x0+w, e.y))
		if e.text != "" {
			condition(f.x0+6, e.y, e.text)
		}
	}
	return group("dg-frame-group", shapes...), group("dg-frame-text", texts...)
}

func (sq *sequence) fallback(loc locale) ui.Node {
	var items []string
	name := func(i int) string { return plainLabel(sq.parts[i].label) }
	for _, ev := range sq.events {
		switch ev.kind {
		case evMessage:
			s := ""
			if ev.number > 0 {
				s = strconv.Itoa(ev.number) + ". "
			}
			s += loc.t("flow.connects", "from", name(ev.from), "to", name(ev.to))
			if ev.text != "" {
				s += ": " + plainLabel(ev.text)
			}
			items = append(items, s)
		case evNote:
			who := name(ev.from)
			if ev.to != ev.from {
				who += loc.listJoin + name(ev.to)
			}
			items = append(items, loc.t("seq.note", "who", who, "text", plainLabel(ev.text)))
		case evBlockStart, evBlockElse:
			items = append(items, loc.t("seq.block", "kind", ev.block, "text", plainLabel(ev.text)))
		case evBlockEnd:
			items = append(items, loc.t("seq.end", "kind", ev.block))
		}
	}
	return fallbackList(loc.t("seq.list"), false, items)
}
