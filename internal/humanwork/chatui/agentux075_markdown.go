package chatui

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/yuin/goldmark/ast"
	extast "github.com/yuin/goldmark/extension/ast"
)

// AGENTUX-075: an agent's answer is formatted text. The message renderer in
// markdown.go already draws paragraphs, headings, lists, emphasis, code and
// links; this file adds the pieces it lacked (tables and struck-through text)
// and removes the emphasis marks an answer left open, so that raw Markdown
// characters never show in an agent's answer.

// agentUX075MarkdownNode draws the nodes the table and strikethrough extensions
// add. It reports false for any other node.
func agentUX075MarkdownNode(m Model, node ast.Node, source []byte) ([]ui.Node, bool) {
	switch n := node.(type) {
	case *extast.Table:
		var head, body []ui.Node
		for row := n.FirstChild(); row != nil; row = row.NextSibling() {
			switch row.(type) {
			case *extast.TableHeader:
				head = append(head, html.Tr(html.Props{}, agentUX075Cells(m, row, source, "th")...))
			case *extast.TableRow:
				body = append(body, html.Tr(html.Props{}, agentUX075Cells(m, row, source, "td")...))
			}
		}
		table := []ui.Node{}
		if len(head) > 0 {
			table = append(table, html.Thead(html.Props{}, head...))
		}
		if len(body) > 0 {
			table = append(table, html.Tbody(html.Props{}, body...))
		}
		// The wrapper scrolls a wide table sideways inside the message, so the page
		// itself never scrolls.
		return []ui.Node{html.Div(html.Props{Class: "md-table-wrap", Dir: "auto"}, html.Table(html.Props{Class: "md-table"}, table...))}, true
	case *extast.Strikethrough:
		return []ui.Node{html.Tag("del", html.Props{}, markdownChildren(m, node, source)...)}, true
	}
	return nil, false
}

func agentUX075Cells(m Model, row ast.Node, source []byte, tag string) []ui.Node {
	var cells []ui.Node
	for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
		tableCell, ok := cell.(*extast.TableCell)
		if !ok {
			continue
		}
		props := html.Props{}
		switch tableCell.Alignment {
		case extast.AlignRight:
			props.Class = "md-align-end"
		case extast.AlignCenter:
			props.Class = "md-align-center"
		}
		children := markdownChildren(m, cell, source)
		if tag == "th" {
			props.Raw = map[string]any{"scope": "col"}
			cells = append(cells, html.Th(props, children...))
			continue
		}
		cells = append(cells, html.Td(props, children...))
	}
	return cells
}

// agentUX075AnswerBody is the text of an agent's own message prepared for
// drawing; any other message is returned as it is.
func agentUX075AnswerBody(actor *PersonaActor, body string, sources []agentReplySource) string {
	if !actor.valid() {
		return body
	}
	return agentUX075CleanAnswer(body, sources)
}

var agentUX075CiteLabel = regexp.MustCompile(`^\[\d+\]$`)
var agentUX075Numbered = regexp.MustCompile(`^\d+[.)]$`)

// agentUX075LinkProps adds what a citation marker needs to a link: the document
// title as its tooltip and its accessible name. Any other link is unchanged.
func agentUX075LinkProps(props html.Props, title, label string) html.Props {
	if strings.TrimSpace(title) == "" || !agentUX075CiteLabel.MatchString(label) {
		return props
	}
	props.Class = "agent-cite"
	props.Title = title
	props.Aria = map[string]string{"label": title}
	return props
}

// agentUX075CleanAnswer is the one shaping of an agent's answer before it is
// drawn: a source named in brackets after the last line is dropped when it has
// its own place in Sources, a citation set on lines of its own becomes a small
// numbered marker that stays in its sentence, and the emphasis the answer left
// open is closed.
func agentUX075CleanAnswer(body string, sources []agentReplySource) string {
	body = agentUX075DropTrailingSourceName(body, sources)
	body = agentUX075InlineCitations(body, sources)
	return agentUX075CloseOpenMarks(body)
}

// agentUX075DropTrailingSourceName removes "(2026 holiday guide)" from the end
// of the answer, on its own line or at the end of the last line, when a source
// of that title is listed under the answer.
func agentUX075DropTrailingSourceName(body string, sources []agentReplySource) string {
	trimmed := strings.TrimRight(body, " \t\r\n")
	if trimmed == "" || len(sources) == 0 {
		return body
	}
	for _, source := range sources {
		title := agentSourceBaseTitle(source.Title)
		if title == "" {
			continue
		}
		for _, wrap := range [][2]string{{"(", ")"}, {"[", "]"}} {
			for _, tail := range []string{wrap[0] + title + wrap[1], wrap[0] + title + wrap[1] + "."} {
				if len(trimmed) < len(tail) || !strings.EqualFold(trimmed[len(trimmed)-len(tail):], tail) {
					continue
				}
				rest := trimmed[:len(trimmed)-len(tail)]
				if strings.TrimSpace(rest) != "" && rest != strings.TrimRight(rest, " \t\r\n") {
					return strings.TrimRight(rest, " \t\r\n")
				}
			}
		}
	}
	return body
}

// agentUX075InlineCitations turns a citation that stands on lines of its own
// after a sentence ("...calendar year", a line break, the document's title as a
// link, a line break, ".") into a numbered marker at the end of that sentence,
// with the full title as its tooltip, and keeps the punctuation attached. The
// full titles are shown once, in Sources. A link in the middle of a sentence,
// a link that opens an answer, and the items of a list of links are left alone.
func agentUX075InlineCitations(body string, sources []agentReplySource) string {
	if len(sources) == 0 {
		return body
	}
	spans := agentMarkdownLinkParts.FindAllStringSubmatchIndex(body, -1)
	if len(spans) == 0 {
		return body
	}
	var out strings.Builder
	numbers := map[string]int{}
	last := 0
	for _, span := range spans {
		label, href := body[span[2]:span[3]], body[span[4]:span[5]]
		before := body[last:span[0]]
		if !strings.Contains(href, "/workspace/app/docs?document=") && !agentUX075IsSourceHref(sources, href) {
			continue
		}
		text := strings.TrimRightFunc(before, agentUX075IsSpace)
		gap := before[len(text):]
		onOwnLine := strings.Contains(gap, "\n")
		if strings.TrimSpace(out.String()+text) == "" || !(onOwnLine || strings.Contains(label, " · ")) {
			continue
		}
		// The link opens a list item ("- [title](...)"): it is the item, not a citation.
		if line := strings.TrimSpace(text[strings.LastIndex(text, "\n")+1:]); !onOwnLine && (line == "-" || line == "*" || agentUX075Numbered.MatchString(line)) {
			continue
		}
		number, seen := numbers[href]
		if !seen {
			number = len(numbers) + 1
			numbers[href] = number
		}
		out.WriteString(text)
		title := strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(label), `"`, "'"), "\n", " ")
		out.WriteString("[\\[" + strconv.Itoa(number) + "\\]](" + href + ` "` + title + `")`)
		last = span[1]
		// Punctuation that follows, after any space or line break (a model
		// sometimes writes a no-break or thin space before it), stays with the
		// sentence, directly after the marker.
		rest := body[last:]
		skipped := len(rest) - len(strings.TrimLeftFunc(rest, agentUX075IsSpace))
		punctuation := 0
		for _, r := range rest[skipped:] {
			if !strings.ContainsRune(".,;:!?…،؛؟。", r) {
				break
			}
			punctuation += utf8.RuneLen(r)
		}
		if punctuation > 0 {
			out.WriteString(rest[skipped : skipped+punctuation])
			last += skipped + punctuation
		}
	}
	out.WriteString(body[last:])
	return out.String()
}

// agentUX075IsSpace is any space a model may put around a citation: the usual
// ones, the no-break and thin spaces, and the zero-width marks.
func agentUX075IsSpace(r rune) bool {
	return unicode.IsSpace(r) || r == 0x200b || r == 0x2060 || r == 0xfeff
}

func agentUX075IsSourceHref(sources []agentReplySource, href string) bool {
	for _, source := range sources {
		if source.Href != "" && source.Href == href {
			return true
		}
	}
	return false
}

// agentUX075CloseOpenMarks removes the emphasis marks and backticks an answer
// opened and never closed, line by line, outside code blocks. A model that is
// cut off mid-emphasis would otherwise leave "**" in the text. Marks inside a
// word (snake_case, an address with underscores) are never touched: only "*",
// "**" and the backtick are considered, and a bullet's own "*" is not a mark.
func agentUX075CloseOpenMarks(body string) string {
	if !strings.ContainsAny(body, "*`") {
		return body
	}
	lines := strings.Split(body, "\n")
	fenced := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fenced = !fenced
			continue
		}
		if fenced || strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t") {
			continue
		}
		lines[i] = agentUX075CleanLine(line)
	}
	return strings.Join(lines, "\n")
}

func agentUX075CleanLine(line string) string {
	prefix := ""
	rest := line
	trimmed := strings.TrimLeft(line, " ")
	if strings.HasPrefix(trimmed, "* ") || strings.HasPrefix(trimmed, "- ") {
		cut := len(line) - len(trimmed) + 2
		prefix, rest = line[:cut], line[cut:]
	}
	rest = agentUX075CloseBackticks(rest)
	rest = agentUX075DropOpenStars(rest, "**")
	rest = agentUX075DropOpenStars(rest, "*")
	return prefix + rest
}

// agentUX075CloseBackticks removes the last backtick of an odd count.
func agentUX075CloseBackticks(line string) string {
	if strings.Count(line, "`")%2 == 0 {
		return line
	}
	at := strings.LastIndex(line, "`")
	return line[:at] + line[at+1:]
}

// agentUX075DropOpenStars removes the last mark of an odd number of marks. For
// the single "*" the doubled marks are set aside first, and a "*" with a space
// on both sides (a multiplication sign) is not a mark.
func agentUX075DropOpenStars(line, mark string) string {
	var positions []int
	for i := 0; i < len(line); {
		if !strings.HasPrefix(line[i:], "*") {
			i++
			continue
		}
		run := 0
		for i+run < len(line) && line[i+run] == '*' {
			run++
		}
		if mark == "**" && run >= 2 || mark == "*" && run%2 == 1 {
			start := i
			if mark == "*" {
				start = i + run - 1
			}
			spaced := start > 0 && line[start-1] == ' ' && start+len(mark) < len(line) && line[start+len(mark)] == ' '
			if !spaced {
				positions = append(positions, start)
			}
		}
		i += run
	}
	if len(positions)%2 == 0 {
		return line
	}
	at := positions[len(positions)-1]
	return line[:at] + line[at+len(mark):]
}
