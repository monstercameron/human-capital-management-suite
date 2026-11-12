package chatui

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// issue3718Marker matches the last line of output when it is only a list
// marker ("- " or "3. "). The first paragraph of a list item continues that
// line instead of opening a new block.
var issue3718Marker = regexp.MustCompile(`^[ \t]*(?:-|\d+\.) $`)

// Issue3718CopyText is the text Copy text puts on the clipboard. The Markdown
// profile's markup goes and its words stay, so a pasted message reads as it
// does on screen rather than as ** and backticks (#3718).
func Issue3718CopyText(body string) string {
	source := []byte(chatStripInternalMarkers(body))
	root := markdownParser.Parse(text.NewReader(source))
	var out strings.Builder
	issue3718Plain(&out, root, source)
	return strings.TrimSpace(out.String())
}

func issue3718Plain(out *strings.Builder, node ast.Node, source []byte) {
	switch n := node.(type) {
	case *ast.Text:
		out.Write(n.Segment.Value(source))
		if n.SoftLineBreak() || n.HardLineBreak() {
			out.WriteByte('\n')
		}
		return
	case *ast.String:
		out.Write(n.Value)
		return
	case *ast.RawHTML:
		for i := 0; i < n.Segments.Len(); i++ {
			seg := n.Segments.At(i)
			out.Write(seg.Value(source))
		}
		return
	case *ast.FencedCodeBlock:
		issue3718Break(out)
		issue3718Lines(out, n.Lines(), source)
		return
	case *ast.CodeBlock:
		issue3718Break(out)
		issue3718Lines(out, n.Lines(), source)
		return
	case *ast.ThematicBreak:
		issue3718Break(out)
		return
	case *ast.Link:
		var label strings.Builder
		issue3718Children(&label, n, source)
		out.WriteString(label.String())
		if dest := string(n.Destination); dest != "" && dest != label.String() {
			out.WriteString(" (" + dest + ")")
		}
		return
	case *ast.ListItem:
		// Items of one list sit on consecutive lines; a nested item starts on
		// the line after its parent's text.
		if s := out.String(); s != "" && !strings.HasSuffix(s, "\n") {
			out.WriteByte('\n')
		}
		out.WriteString(issue3718ItemMarker(n))
		issue3718Children(out, n, source)
		return
	}
	if node.Type() == ast.TypeBlock {
		issue3718Break(out)
	}
	issue3718Children(out, node, source)
}

// issue3718ItemMarker is the bullet or number that opens a list item's line.
func issue3718ItemMarker(item *ast.ListItem) string {
	if list, ok := item.Parent().(*ast.List); ok && list.IsOrdered() {
		index := list.Start
		for s := item.PreviousSibling(); s != nil; s = s.PreviousSibling() {
			index++
		}
		return strconv.Itoa(index) + ". "
	}
	return "- "
}

func issue3718Children(out *strings.Builder, node ast.Node, source []byte) {
	for c := node.FirstChild(); c != nil; c = c.NextSibling() {
		issue3718Plain(out, c, source)
	}
}

func issue3718Lines(out *strings.Builder, lines *text.Segments, source []byte) {
	for i := 0; i < lines.Len(); i++ {
		seg := lines.At(i)
		out.Write(seg.Value(source))
	}
}

// issue3718Break starts a block on its own line, with a blank line before it.
// Nothing is added when the output already ends with one, or when the last
// line is only a list marker that this block continues.
func issue3718Break(out *strings.Builder) {
	s := out.String()
	if s == "" || strings.HasSuffix(s, "\n\n") {
		return
	}
	last := s[strings.LastIndexByte(s, '\n')+1:]
	if issue3718Marker.MatchString(last) {
		return
	}
	if strings.HasSuffix(s, "\n") {
		out.WriteByte('\n')
		return
	}
	out.WriteString("\n\n")
}
