package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATBUG-048 and CHATBUG-025: every setting in Manage channel is the same
// disclosure row, a 12 px label with its current value at the right and a
// chevron, under one 11 px group caption. Before, the rows came in three sizes,
// each group had its own caption style and the Status block was a box of its
// own. These helpers are the one shape the panel's sections are built from.

// manageCaption is the caption over a group of rows.
func manageCaption(text string) ui.Node {
	return html.H4(html.Props{Class: "manage-caption", Text: text})
}

// manageSummary is the closed row: the label, the current value and the
// chevron. props carries the button's own wiring (the Chat disclosure toggle
// for a row that opens its body in the page, or a click handler for a section
// that draws its body on demand).
func manageSummary(props html.Props, label string, value ui.Node) ui.Node {
	props.Class = "manage-row-summary " + props.Class
	var current ui.Node
	if value != nil {
		current = html.Span(html.Props{Class: "manage-row-value"}, value)
	}
	return html.Button(props, html.Span(html.Props{Text: label}), current, icon("chevron-down"))
}

// manageRow is a closed row that opens its body in place. The Chat disclosure
// handler flips the body, so the row costs no state.
func manageRow(class, label string, value ui.Node, body ...ui.Node) ui.Node {
	summary := chatPolishDisclosureLabel(html.Props{Class: "manage-row-summary"},
		html.Span(html.Props{Text: label}),
		manageValue(value))
	return chatPolishDisclosure(html.Props{Class: "manage-row " + class}, summary, html.Div(html.Props{Class: "manage-row-body"}, body...))
}

func manageValue(value ui.Node) ui.Node {
	if value == nil {
		return html.Span(html.Props{Class: "manage-row-value"})
	}
	return html.Span(html.Props{Class: "manage-row-value"}, value)
}

// manageNote is a sentence under a row's body. Notes appear only where they
// apply, so a row never explains a case the conversation is not in.
func manageNote(text string) ui.Node {
	return html.P(html.Props{Class: "manage-note", Text: text})
}

// manageStatic is a row with a value and nothing to open, for a setting the
// person may see and not change.
func manageStatic(class, label string, value ui.Node) ui.Node {
	return html.Div(html.Props{Class: "manage-row manage-row-static " + class},
		html.Div(html.Props{Class: "manage-row-summary"}, html.Span(html.Props{Text: label}), manageValue(value)))
}
