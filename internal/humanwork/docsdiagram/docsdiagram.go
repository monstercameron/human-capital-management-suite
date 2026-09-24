// Package docsdiagram renders the common subset of Mermaid diagram syntax as
// accessible inline SVG built from typed UI nodes.
//
// The output carries no inline style attribute and no script, so it renders
// under the product Content-Security-Policy. Every visual property comes from
// the classes that Stylesheet defines against the product theme tokens, which
// keeps a diagram legible in light and dark mode.
//
// Labels are drawn inside foreignObject elements as HTML text. GoWebComponents
// creates SVG elements in the SVG namespace by tag name, and its tag table
// omits text and title (names HTML shares), so an SVG text element built on
// the client would land in the HTML namespace and never paint. A foreignObject
// is in the table and its HTML children paint the same way on the server
// rendered page and on the client.
package docsdiagram

import (
	"errors"
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Limits bound the work one diagram may ask for. Input beyond them is refused
// with ErrTooLarge rather than drawn partially.
const (
	// MaxSourceBytes caps the Mermaid source length.
	MaxSourceBytes = 20 * 1024
	// MaxElements caps nodes, edges, participants, messages, slices, tasks
	// and events, each counted separately.
	MaxElements = 200
	// MaxDataPoints caps the numeric values of one chart across all series.
	MaxDataPoints = 500
	// maxLabelRunes caps how much of one label is drawn; the text fallback
	// keeps the full label.
	maxLabelRunes = 240
)

// Sentinel errors. A caller shows the source as a code block for any of them.
var (
	// ErrEmpty reports a source with no diagram statement.
	ErrEmpty = errors.New("docsdiagram: empty diagram")
	// ErrUnsupported reports a diagram kind this package does not draw.
	ErrUnsupported = errors.New("docsdiagram: unsupported diagram kind")
	// ErrSyntax reports a statement that could not be parsed.
	ErrSyntax = errors.New("docsdiagram: syntax error")
	// ErrTooLarge reports input beyond the package limits.
	ErrTooLarge = errors.New("docsdiagram: diagram too large")
)

// SyntaxError locates a parse failure. It matches ErrSyntax under errors.Is.
type SyntaxError struct {
	Line int
	Msg  string
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("docsdiagram: line %d: %s", e.Line, e.Msg)
}

// Is reports ErrSyntax as the class of every SyntaxError.
func (e *SyntaxError) Is(target error) bool { return target == ErrSyntax }

func syntaxErr(line int, format string, args ...any) error {
	return &SyntaxError{Line: line, Msg: fmt.Sprintf(format, args...)}
}

func tooLarge(what string, limit int) error {
	return fmt.Errorf("%w: more than %d %s", ErrTooLarge, limit, what)
}

// Options adjusts one rendering.
type Options struct {
	// Title is the accessible name. When empty, the diagram's own title or a
	// generated name is used.
	Title string
	// Locale selects number formatting and the generated summary wording
	// (en, de and ar are known; anything else reads as en).
	Locale string
}

// Kind names the diagram families Render draws.
type Kind string

// The supported kinds.
const (
	KindFlowchart Kind = "flowchart"
	KindSequence  Kind = "sequence"
	KindPie       Kind = "pie"
	KindXYChart   Kind = "xychart"
	KindGantt     Kind = "gantt"
	KindTimeline  Kind = "timeline"
	KindJourney   Kind = "journey"
)

// srcLine is one logical statement with its 1-based source line.
type srcLine struct {
	n    int
	text string
}

// drawing is what every kind renderer produces before the figure is built.
type drawing struct {
	kind     Kind
	title    string // diagram-declared title, shown as the caption
	name     string // generated accessible name used when no title exists
	desc     string
	width    float64
	height   float64
	body     []ui.Node
	defs     []ui.Node
	fallback ui.Node
}

// Render parses a Mermaid source block and returns an accessible inline SVG node.
func Render(source string, opts Options) (ui.Node, error) {
	id := idBase(source, opts)
	d, err := build(source, opts, id)
	if err != nil {
		return nil, err
	}
	return figure(d, opts, id), nil
}

// DetectKind reports the kind a source declares, or ErrUnsupported/ErrEmpty.
func DetectKind(source string) (Kind, error) {
	lines, err := prepare(source)
	if err != nil {
		return "", err
	}
	kind, _, err := headerKind(lines[0])
	return kind, err
}

func build(source string, opts Options, id string) (d *drawing, err error) {
	defer func() {
		// Parsers index slices built from untrusted input; a missed bound is
		// a bug, but it must surface as an error, never as a page crash.
		if r := recover(); r != nil {
			d, err = nil, fmt.Errorf("%w: internal failure: %v", ErrSyntax, r)
		}
	}()
	return buildUnguarded(source, opts, id)
}

// buildUnguarded is build without the panic guard, so tests and the fuzz
// target see a panic as the bug it is.
func buildUnguarded(source string, opts Options, id string) (d *drawing, err error) {
	lines, err := prepare(source)
	if err != nil {
		return nil, err
	}
	kind, rest, err := headerKind(lines[0])
	if err != nil {
		return nil, err
	}
	rc := rctx{loc: localeFor(opts.Locale), id: id}
	body := lines[1:]
	switch kind {
	case KindFlowchart:
		d, err = renderFlowchart(lines[0], rest, body, rc)
	case KindSequence:
		d, err = renderSequence(lines[0].n, rest, body, rc)
	case KindPie:
		d, err = renderPie(lines[0].n, rest, body, rc)
	case KindXYChart:
		d, err = renderXYChart(lines[0].n, rest, body, rc)
	case KindGantt:
		d, err = renderGantt(lines[0].n, rest, body, rc)
	case KindTimeline:
		d, err = renderTimeline(lines[0].n, rest, body, rc)
	default:
		d, err = renderJourney(lines[0].n, rest, body, rc)
	}
	if err != nil {
		return nil, err
	}
	if d.title == "" {
		d.title = frontMatterTitle(source)
	}
	return d, nil
}

// frontMatterTitle reads `title:` from a leading --- front matter block.
func frontMatterTitle(source string) string {
	source = strings.TrimPrefix(strings.ReplaceAll(source, "\r\n", "\n"), "\ufeff")
	trimmed := strings.TrimLeft(source, " \t\n")
	if !strings.HasPrefix(trimmed, "---") {
		return ""
	}
	for _, ln := range strings.Split(trimmed, "\n")[1:] {
		t := strings.TrimSpace(ln)
		if t == "---" {
			return ""
		}
		if rest, ok := strings.CutPrefix(t, "title:"); ok {
			return cleanLabel(rest)
		}
	}
	return ""
}

// prepare splits the source into non-empty statements, dropping comments,
// front matter and init directives.
func prepare(source string) ([]srcLine, error) {
	if len(source) > MaxSourceBytes {
		return nil, fmt.Errorf("%w: source longer than %d bytes", ErrTooLarge, MaxSourceBytes)
	}
	source = strings.TrimPrefix(source, "\ufeff")
	source = strings.ReplaceAll(source, "\r\n", "\n")
	source = strings.ReplaceAll(source, "\r", "\n")
	raw := strings.Split(source, "\n")
	var out []srcLine
	inFront := false
	for i, text := range raw {
		trimmed := strings.TrimSpace(text)
		if len(out) == 0 && !inFront && trimmed == "---" {
			inFront = true
			continue
		}
		if inFront {
			if trimmed == "---" {
				inFront = false
			}
			continue
		}
		if strings.HasPrefix(trimmed, "%%") {
			continue
		}
		if idx := commentIndex(trimmed); idx >= 0 {
			trimmed = strings.TrimSpace(trimmed[:idx])
		}
		if trimmed == "" {
			continue
		}
		out = append(out, srcLine{n: i + 1, text: trimmed})
	}
	if inFront {
		return nil, syntaxErr(len(raw), "front matter is not closed")
	}
	if len(out) == 0 {
		return nil, ErrEmpty
	}
	return out, nil
}

// commentIndex finds a trailing %% comment outside quotes.
func commentIndex(s string) int {
	inQuote := false
	for i := 0; i+1 < len(s); i++ {
		switch {
		case s[i] == '"':
			inQuote = !inQuote
		case !inQuote && s[i] == '%' && s[i+1] == '%':
			return i
		}
	}
	return -1
}

func headerKind(first srcLine) (Kind, string, error) {
	word, rest := splitWord(first.text)
	switch strings.ToLower(word) {
	case "flowchart", "graph", "flowchart-elk":
		return KindFlowchart, rest, nil
	case "sequencediagram":
		return KindSequence, rest, nil
	case "pie":
		return KindPie, rest, nil
	case "xychart-beta", "xychart":
		return KindXYChart, rest, nil
	case "gantt":
		return KindGantt, rest, nil
	case "timeline":
		return KindTimeline, rest, nil
	case "journey":
		return KindJourney, rest, nil
	}
	return "", "", fmt.Errorf("%w: %q", ErrUnsupported, truncateRunes(word, 40))
}

// splitWord returns the first whitespace-delimited word and the trimmed rest.
func splitWord(s string) (string, string) {
	s = strings.TrimSpace(s)
	idx := strings.IndexAny(s, " \t")
	if idx < 0 {
		return s, ""
	}
	return s[:idx], strings.TrimSpace(s[idx+1:])
}

// keywordRest reports whether s starts with keyword (case-insensitive) as a
// whole word, returning the trimmed remainder.
func keywordRest(s, keyword string) (string, bool) {
	if len(s) < len(keyword) || !strings.EqualFold(s[:len(keyword)], keyword) {
		return "", false
	}
	rest := s[len(keyword):]
	if rest != "" && rest[0] != ' ' && rest[0] != '\t' && rest[0] != ':' {
		return "", false
	}
	return strings.TrimSpace(rest), true
}

func idBase(source string, opts Options) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(source))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(opts.Title))
	return "dg" + strconv.FormatUint(uint64(h.Sum32()), 36)
}

// figure wraps a drawing in its accessible figure.
func figure(d *drawing, opts Options, id string) ui.Node {
	name := strings.TrimSpace(opts.Title)
	if name == "" {
		name = d.title
	}
	if name == "" {
		name = d.name
	}
	titleID, descID := id+"-title", id+"-desc"
	width := ceilTo(d.width, 1)
	height := ceilTo(d.height, 1)
	defs := append([]ui.Node{}, d.defs...)
	svgChildren := []ui.Node{
		el("title", "", attrs{"id": titleID}, ui.Text(name)),
		el("desc", "", attrs{"id": descID}, ui.Text(d.desc)),
	}
	if len(defs) > 0 {
		svgChildren = append(svgChildren, el("defs", "", nil, defs...))
	}
	svgChildren = append(svgChildren, d.body...)
	svg := el("svg", "docs-diagram-svg "+widthClass(width), attrs{
		"xmlns":            "http://www.w3.org/2000/svg",
		"viewBox":          "0 0 " + num(width) + " " + num(height),
		"width":            "100%",
		"role":             "img",
		"aria-labelledby":  titleID,
		"aria-describedby": descID,
		"focusable":        "false",
	}, svgChildren...)
	children := []ui.Node{}
	if d.title != "" {
		children = append(children, html.Tag("figcaption", html.Props{Class: "docs-diagram-caption"}, ui.Text(d.title)))
	}
	children = append(children,
		html.Div(html.Props{Class: "docs-diagram-canvas", Raw: map[string]any{"tabindex": "0", "role": "group", "aria-label": name}}, svg),
		html.Div(html.Props{Class: "docs-diagram-sr"}, d.fallback),
	)
	return html.Tag("figure", html.Props{Class: "docs-diagram docs-diagram-" + string(d.kind)}, children...)
}

// widthClass picks the width bucket that caps the drawing at its natural
// size and keeps its text legible on a narrow screen by letting it scroll.
func widthClass(w float64) string {
	bucket := int(ceilTo(w, 80))
	if bucket < 160 {
		bucket = 160
	}
	if bucket > maxWidthBucket {
		bucket = maxWidthBucket
	}
	return "docs-diagram-w-" + strconv.Itoa(bucket)
}

const maxWidthBucket = 2400
