package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
)

func RenderFilterBlockedDraft(m Model, draft string, span chatfilter.Span) ui.Node {
	var children []ui.Node
	if span.Start >= 0 && span.End <= len(draft) && span.Start < span.End {
		children = []ui.Node{ui.Text(draft[:span.Start]), html.Mark(html.Props{}, ui.Text(draft[span.Start:span.End])), ui.Text(draft[span.End:])}
	} else {
		children = []ui.Node{ui.Text(draft)}
	}
	return html.Div(html.Props{Class: "chatfilter-blocked", Role: "alert", Dir: "auto"}, html.P(html.Props{Text: chatfilterText(m, "blocked")}), html.P(html.Props{}, children...))
}
func RenderFilterMaskedText(m Model, body string) ui.Node {
	parts := strings.Split(body, "[removed word]")
	var nodes []ui.Node
	for i, part := range parts {
		if i > 0 {
			nodes = append(nodes, html.Span(html.Props{Class: "chatfilter-removed", Text: chatfilterText(m, "removed")}))
		}
		nodes = append(nodes, ui.Text(part))
	}
	return html.Span(html.Props{Dir: "auto"}, nodes...)
}

func filterMessageBody(m Model, body string) []ui.Node {
	return markdownMessageBody(m, strings.ReplaceAll(body, "[removed word]", chatfilterText(m, "removed")))
}
