package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATUX-027: every setting in Conversation details is one disclosure row. The
// row is a button (label, current value, chevron); an open row shows its
// content in an inset block directly under it. Which row is open lives in the
// client's local state (localUI.detailGroups), one key per row, and rows of one
// group close each other, so opening a row never depends on hooks that change
// count between renders. Components that draw their own row from hooks (the
// channel status, the translation setting, the reading language) are handed a
// sectionWrap and put their content into the same shape.

const (
	// chatux027ManageGroup is the group of rows under Manage channel: one open at a time.
	chatux027ManageGroup = "manage"
	// chatux027PersonalGroup holds the rows that are about the reader, not the channel.
	chatux027PersonalGroup = "me"

	chatux027KeyAttention    = "chat.ux027.attention"
	chatux027KeyRoleAdd      = "chat.ux027.role_add"
	chatux027KeyAddMilestone = "chat.ux027.milestone_add"
)

var chatux027Copy = map[string]map[string]string{
	"en-US": {chatux027KeyAttention: "Needs attention", chatux027KeyRoleAdd: "Add a label", chatux027KeyAddMilestone: "Add milestone"},
	"de-DE": {chatux027KeyAttention: "Prüfen", chatux027KeyRoleAdd: "Bezeichnung hinzufügen", chatux027KeyAddMilestone: "Meilenstein hinzufügen"},
	"ar":    {chatux027KeyAttention: "يحتاج إلى انتباه", chatux027KeyRoleAdd: "إضافة تسمية", chatux027KeyAddMilestone: "إضافة مرحلة"},
}

// sectionContent is what a component that draws its own row hands to the
// panel: the label and current value of the row, what is inside when it is
// open, and whether the content holds an error (the row then says so, and a
// group with nothing chosen yet opens it).
type sectionContent struct {
	Label  string
	Value  ui.Node
	Body   ui.Node
	Failed bool
}

// sectionWrap turns a component's content into the panel's row.
type sectionWrap func(sectionContent) ui.Node

// sectionSpec is one row of the panel.
type sectionSpec struct {
	// ID names the row; Group is the set of rows that close each other ("" for a row of its own).
	ID, Group string
	// Class goes on the row's wrapper, for tests and scripts that find a section by name.
	Class  string
	Label  string
	Value  ui.Node
	Body   ui.Node
	Open   bool
	Failed bool
	// Action is the delegated click action; the group's own toggle (Manage channel) keeps "details-group".
	Action string
	// BodyID overrides the id of the open block.
	BodyID string
	// Plain is a block that holds other rows (Manage channel): no inset of its own.
	Plain bool
	// Data is more attributes for the wrapper.
	Data map[string]string
}

func chatux027Key(group, id string) string { return "s:" + group + ":" + id }

// chatux027IsOpen reports whether a row is open. A row the person has not
// touched is closed, unless it asks to open first (a failed load, the filter
// settings the header opened to) and nothing in its group has been chosen.
func chatux027IsOpen(u localUI, group, id string, openFirst bool) bool {
	if value, ok := u.detailGroups[chatux027Key(group, id)]; ok {
		return value
	}
	if !openFirst {
		return false
	}
	prefix := "s:" + group + ":"
	for key := range u.detailGroups {
		if strings.HasPrefix(key, prefix) {
			return false
		}
	}
	return true
}

// toggleSection opens a row, closing the others of its group, or closes it.
// wasOpen is what the page showed when the person pressed it.
func (u *localUI) toggleSection(group, id string, wasOpen bool) {
	next := make(map[string]bool, len(u.detailGroups)+1)
	prefix := "s:" + group + ":"
	for key, value := range u.detailGroups {
		if !wasOpen && group != "" && strings.HasPrefix(key, prefix) {
			value = false
		}
		next[key] = value
	}
	next[chatux027Key(group, id)] = !wasOpen
	u.detailGroups = next
}

// chatux027Click handles a row's button. It reports whether the click was one.
func chatux027Click(e ui.MouseEvent, local localStore) bool {
	action, id, extra := eventAction(e)
	if action != "manage-section" || id == "" {
		return false
	}
	cut := strings.LastIndex(extra, ":")
	if cut < 0 {
		return false
	}
	group, state := extra[:cut], extra[cut+1:]
	local.update(func(u *localUI) { u.toggleSection(group, id, state == "1") })
	return true
}

// chatux027Section draws one row and, under it, its block. The row is a button
// with its state and the block it controls; a row with nothing to open is a
// plain line in the same shape, without the button.
func chatux027Section(m Model, s sectionSpec) ui.Node {
	rowID := "chat-manage-" + s.ID + "-row"
	bodyID := s.BodyID
	if bodyID == "" {
		bodyID = "chat-manage-" + s.ID
	}
	value := s.Value
	if s.Failed {
		value = ui.Text(laneText(m, chatux027Copy, chatux027KeyAttention))
	}
	var current ui.Node
	if value != nil {
		current = html.Span(html.Props{Class: "manage-sec-value", Dir: "auto"}, value)
	} else {
		current = html.Span(html.Props{Class: "manage-sec-value"})
	}
	class := "manage-sec"
	if s.Class != "" {
		class += " " + s.Class
	}
	data := map[string]string{"manage-section": s.ID}
	for key, value := range s.Data {
		data[key] = value
	}
	if s.Failed {
		data["failed"] = "true"
	}
	label := html.Span(html.Props{Class: "manage-sec-label", Dir: "auto", Text: s.Label})
	if s.Body == nil {
		return html.Div(html.Props{Class: class + " manage-sec-static", Data: data},
			html.Div(html.Props{Class: "manage-sec-row", ID: rowID}, label, current))
	}
	action := s.Action
	if action == "" {
		action = "manage-section"
	}
	state := "0"
	if s.Open {
		state = "1"
	}
	rowData := map[string]string{"action": action, "id": s.ID, "extra": s.Group + ":" + state}
	row := html.Button(html.Props{Class: "manage-sec-row", ID: rowID, Type: "button", Data: rowData,
		Aria: map[string]string{"expanded": boolString(s.Open), "controls": bodyID}},
		label, current, html.Span(html.Props{Class: "manage-sec-chevron", Aria: map[string]string{"hidden": "true"}}, icon("chevron-down")))
	bodyClass := "manage-sec-body"
	if s.Plain {
		bodyClass += " manage-sec-plain"
	}
	block := html.Div(html.Props{ID: bodyID, Class: bodyClass, Role: "region", Hidden: !s.Open, Aria: map[string]string{"labelledby": rowID}}, s.Body)
	return html.Div(html.Props{Class: class, Data: data}, row, block)
}

// chatux027Wrap is the sectionWrap for a row of a group: the component that
// draws the content asks the panel for the row and gets the same shape as the
// rows the panel draws itself.
func chatux027Wrap(m Model, h handlers, group, id, class string) sectionWrap {
	return func(c sectionContent) ui.Node {
		open := chatux027IsOpen(h.local, group, id, c.Failed)
		return chatux027Section(m, sectionSpec{ID: id, Group: group, Class: class, Label: c.Label, Value: c.Value, Body: c.Body, Open: open, Failed: c.Failed})
	}
}

// chatux027GroupLabel is the quiet label over a group of rows.
func chatux027GroupLabel(text string) ui.Node {
	return html.H4(html.Props{Class: "manage-caption", Text: text})
}

// chatux027Help is the sentence that explains a setting. It is drawn inside an
// open row only.
func chatux027Help(text string) ui.Node {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return html.P(html.Props{Class: "manage-sec-help", Text: text})
}

// chatux027Error is an error shown inside the row it belongs to, in full, with
// the way to try again.
func chatux027Error(m Model, text string, retry ui.Handler, retryAction string) ui.Node {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	props := html.Props{Class: "button secondary small", Type: "button", Text: m.t(KeyRetry)}
	if retryAction != "" {
		props.Data = map[string]string{"action": retryAction}
	} else {
		props.OnClick = retry
	}
	return html.Div(html.Props{Class: "manage-sec-error", Role: "alert"},
		html.P(html.Props{Dir: "auto", Text: text}), html.Button(props))
}

// chatux027Actions is the one button row of a form in the panel: the main
// button first, then Cancel.
func chatux027Actions(children ...ui.Node) ui.Node {
	return html.Div(html.Props{Class: "manage-sec-actions"}, children...)
}

// chatux027CancelButton closes the open row of a form without saving.
func chatux027CancelButton(m Model, group, id string) ui.Node {
	return html.Button(html.Props{Class: "button secondary small", Type: "button", Text: m.t(KeyCancel),
		Data: map[string]string{"action": "manage-section", "id": id, "extra": group + ":1"}})
}
