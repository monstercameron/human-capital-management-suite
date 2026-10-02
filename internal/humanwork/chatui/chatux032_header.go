package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATUX-032. One header for every panel beside the conversation: thread,
// Conversation details, Person details, Saved and Moderation. The same height
// (52 px), the same title size, an optional subtitle on the title's own line and
// one close button, so opening another panel never changes what the top of the
// panel looks like. The look is in chatux032_styles.go; this is the markup.

// chatux032HeaderProps describes one panel header.
type chatux032HeaderProps struct {
	// Class is added after the shared classes; ID is the heading's id.
	Class, ID string
	Title     string
	// Subtitle sits on the title's line, in a quieter weight.
	Subtitle     string
	SubtitleData map[string]string
	// FocusHeading makes the heading focusable by script (tabindex -1) so a panel
	// can put initial focus on its title instead of on its close button.
	FocusHeading bool
	// Leading comes before the title (a back arrow); Actions come before Close.
	Leading, Actions []ui.Node
	Close            ui.Node
}

// chatux032Header draws the shared header.
func chatux032Header(p chatux032HeaderProps) ui.Node {
	heading := html.Props{ID: p.ID, Text: p.Title}
	if p.FocusHeading {
		heading.Raw = map[string]any{"tabindex": "-1"}
	}
	title := []ui.Node{html.H2(heading)}
	if p.Subtitle != "" {
		title = append(title, html.Span(html.Props{Class: "chat-panel-sub", Text: p.Subtitle, Data: p.SubtitleData}))
	}
	children := append([]ui.Node{}, p.Leading...)
	children = append(children, html.Div(html.Props{Class: "chat-panel-title"}, title...))
	controls := append([]ui.Node{}, p.Actions...)
	if p.Close != nil {
		controls = append(controls, p.Close)
	}
	children = append(children, html.Div(html.Props{Class: "side-heading-actions"}, controls...))
	class := "side-heading chat-panel-head"
	if p.Class != "" {
		class += " " + p.Class
	}
	return html.Header(html.Props{Class: class}, children...)
}

// chatux032CloseButton is the one close control of a panel: a plain X icon
// button. The class carries the size, the hover and the focus ring (keyboard
// focus only).
func chatux032CloseButton(label string, disabled bool, data map[string]string, extraClass string) ui.Node {
	class := "icon-button chat-panel-close"
	if extraClass != "" {
		class += " " + extraClass
	}
	return html.Button(html.Props{Class: class, Type: "button", Disabled: disabled, Title: label, Data: data, Aria: map[string]string{"label": label}}, icon("close"))
}
