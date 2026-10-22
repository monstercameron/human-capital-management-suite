package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/yuin/goldmark/ast"
)

// CHATUX-039 (lane W3): a message that copies an answer's sources as text
// ("Sources", then one link per line) used to draw a bulleted list. In the
// narrow thread panel the long first link wrapped as an inline block, so the
// first item lost its bullet and its version landed on a bullet of its own. A
// list of nothing but links that follows a paragraph reading "Sources" is drawn
// as the same chips the conversation draws under an answer, one per source.

var chatux031SourceHeadings = map[string]bool{"sources": true, "quellen": true, "المصادر": true, "المراجع": true}

// chatux031SourceList reports whether list is the sources of the paragraph
// before it: that paragraph is only the word "Sources" (any of the three
// languages, with an optional colon or emphasis) and every item is one link.
func chatux031SourceList(list *ast.List, source []byte) bool {
	if list.IsOrdered() || list.ChildCount() == 0 {
		return false
	}
	heading, ok := list.PreviousSibling().(*ast.Paragraph)
	if !ok || !chatux031SourceHeadings[strings.ToLower(strings.Trim(strings.TrimSpace(string(heading.Text(source))), ":*_  "))] {
		return false
	}
	for item := list.FirstChild(); item != nil; item = item.NextSibling() {
		block := item.FirstChild()
		if block == nil || item.ChildCount() != 1 || block.ChildCount() != 1 {
			return false
		}
		if _, isLink := block.FirstChild().(*ast.Link); !isLink {
			return false
		}
	}
	return true
}

// chatux031SourceChips draws the chips for a list chatux031SourceList accepted.
func chatux031SourceChips(m Model, list *ast.List, source []byte) ui.Node {
	items := make([]ui.Node, 0, list.ChildCount())
	for item := list.FirstChild(); item != nil; item = item.NextSibling() {
		items = append(items, html.Li(html.Props{Class: "agent-reply-source"}, append([]ui.Node{icon("document")}, markdownChildren(m, item.FirstChild(), source)...)...))
	}
	return html.Ul(html.Props{Class: "md-source-chips"}, items...)
}
