package chatui

import (
	"strconv"
	"unicode"
	"unicode/utf16"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// The composer's tool row, left to right under the text: an Add menu (+), a
// Mention button (@), the emoji picker, the GIF picker where one is set up, and
// a Formatting toggle (Aa) that shows or hides the formatting row above the
// row. At the right sit the muted Enter / Shift+Enter hint and Send.
//
// Nothing here waits for a focus or blur event or for a timer. The Add menu
// opens and closes through the shared disclosure code (which works inside the
// click and key events themselves), and every other control acts inside its own
// click.

const (
	// composerFormatDefaultWidth is the viewport width at which the formatting
	// row is shown until the person chooses otherwise.
	composerFormatDefaultWidth = 800
	// composerHintMinWidth is the viewport width below which the Enter hint
	// is left out.
	composerHintMinWidth = 600

	composerFormatShown  = "shown"
	composerFormatHidden = "hidden"
	composerFormatAuto   = "auto"
)

// composerAddItem is one thing the Add menu can add to a conversation.
type composerAddItem struct {
	kind, iconName, nameKey, noteKey string
}

// composerVoiceAvailable is whether this conversation records voice messages:
// direct and group conversations, as the voice control has always been.
func composerVoiceAvailable(m Model) bool {
	kind := m.selected().Kind
	return kind == DirectMessage || kind == GroupChat
}

// composerLocationAvailable is whether this deployment shares locations.
func composerLocationAvailable(m Model) bool {
	return m.ChatFeatures == nil || m.ChatFeatures.Locations
}

// composerChannelWidget is whether the channel's poll and to-do list apply
// here: they belong to public and private channels, as their header buttons do.
func composerChannelWidget(m Model) bool {
	kind := m.selected().Kind
	return m.SelectedID != "" && (kind == PublicChannel || kind == PrivateChannel)
}

// composerAddItems lists what can be added to this conversation, in the order
// the menu shows it. An item is left out, never greyed, when it is not
// available here.
//
// The attachment item is offered when its upload owner is composed.
func composerAddItems(m Model) []composerAddItem {
	var items []composerAddItem
	if m.Chatattach001 != nil && m.Chatattach001.Choose != nil {
		items = append(items, composerAddItem{"attachment", "attach", "", ""})
	}
	if composerChannelWidget(m) && m.Callbacks.OpenChannelPoll != nil {
		items = append(items, composerAddItem{"poll", "poll", keyComposerAddPoll, keyComposerAddPollNote})
	}
	if composerChannelWidget(m) && m.Callbacks.OpenChannelTodo != nil {
		items = append(items, composerAddItem{"todo", "checklist", keyComposerAddTodo, keyComposerAddTodoNote})
	}
	if composerLocationAvailable(m) {
		items = append(items, composerAddItem{"location", "pin", keyComposerAddLocation, keyComposerAddLocNote})
	}
	if composerVoiceAvailable(m) {
		items = append(items, composerAddItem{"voice", "", keyComposerAddVoice, keyComposerAddVoiceNote})
	}
	return items
}

// composerAddControl is the + button and the menu it opens. The menu is a
// shared disclosure: the page's one click listener opens it and moves focus to
// its first item, so it needs no state of its own.
func composerAddControl(m Model, target string, disabled bool) ui.Node {
	items := composerAddItems(m)
	if len(items) == 0 {
		return nil
	}
	menuID := target + "-add-menu"
	label := composerText(m, keyComposerAdd)
	rows := make([]ui.Node, 0, len(items))
	for _, item := range items {
		glyph := voiceMicrophone()
		if item.iconName != "" {
			glyph = icon(item.iconName)
		}
		name, note := composerText(m, item.nameKey), composerText(m, item.noteKey)
		if item.kind == "attachment" {
			name, note = Chatattach001Text(m.Locale, "attach"), Chatattach001Text(m.Locale, "note")
		}
		noteID := menuID + "-" + item.kind + "-note"
		rows = append(rows, html.Button(html.Props{ID: menuID + "-" + item.kind, Class: "menu-item composer-add-item", Type: "button", Role: "menuitem", Title: note,
			Data: map[string]string{"action": "composer-add", "id": target, "extra": item.kind},
			Aria: map[string]string{"label": name, "describedby": noteID}},
			html.Span(html.Props{Class: "composer-add-icon", Aria: map[string]string{"hidden": "true"}}, glyph),
			html.Span(html.Props{Class: "composer-add-text"},
				html.Span(html.Props{Class: "composer-add-name", Dir: "auto", Text: name}),
				html.Span(html.Props{ID: noteID, Class: "composer-add-note", Dir: "auto", Text: note}))))
	}
	return html.Div(html.Props{Class: "composer-add", Data: map[string]string{"chat-disclosure": "true"}},
		html.Button(html.Props{Class: "tool-button composer-add-trigger", Type: "button", Disabled: disabled, Title: label,
			Data: map[string]string{"chat-disclosure-toggle": "true"},
			Aria: map[string]string{"label": label, "expanded": "false", "haspopup": "menu", "controls": menuID}}, icon("plus")),
		html.Div(html.Props{ID: menuID, Class: "composer-add-menu", Role: "menu", Hidden: true, Dir: agentReplyDirection(m.Locale),
			Data: map[string]string{"chat-disclosure-body": "true"}, Aria: map[string]string{"label": label}}, rows...))
}

// composerMentionButton is the @ button: it puts an @ at the caret and so opens
// the same suggestion list typing one does.
func composerMentionButton(m Model, target string, disabled bool) ui.Node {
	label := composerText(m, keyComposerMention)
	return html.Button(html.Props{Class: "tool-button composer-mention-button", Type: "button", Disabled: disabled, Title: label,
		Data: map[string]string{"action": "composer-mention", "id": target}, Aria: map[string]string{"label": label}},
		html.Span(html.Props{Class: "composer-glyph", Dir: "ltr", Aria: map[string]string{"hidden": "true"}, Text: "@"}))
}

// composerFormatState is how the formatting row is set: what the viewer chose
// this session, else what they chose before, else automatic (shown from
// composerFormatDefaultWidth up).
func composerFormatState(m Model, local localUI) string {
	for _, choice := range []string{local.formatRow, m.ComposerFormatRow} {
		if choice == composerFormatShown || choice == composerFormatHidden {
			return choice
		}
	}
	return composerFormatAuto
}

// composerFormatVisible is whether the formatting row is on screen.
func composerFormatVisible(m Model, local localUI) bool {
	switch composerFormatState(m, local) {
	case composerFormatShown:
		return true
	case composerFormatHidden:
		return false
	}
	return viewportAtLeast(composerFormatDefaultWidth)
}

// composerFormatToggle is the Aa button.
func composerFormatToggle(m Model, target string, disabled bool, local localUI) ui.Node {
	return html.Button(html.Props{Class: "tool-button composer-format-toggle", Type: "button", Disabled: disabled, Title: composerText(m, keyComposerFormatTip),
		Data: map[string]string{"action": "composer-format-toggle", "id": target},
		Aria: map[string]string{"label": composerText(m, keyComposerFormat), "pressed": boolString(composerFormatVisible(m, local)), "controls": target + "-format-row"}},
		html.Span(html.Props{Class: "composer-glyph", Dir: "ltr", Aria: map[string]string{"hidden": "true"}, Text: "Aa"}))
}

// composerFormatRow is the formatting row. It is always in the tree; the
// composer's data-format-row attribute and the styles decide whether it shows.
func composerFormatRow(m Model, target string, disabled bool) ui.Node {
	return html.Div(html.Props{ID: target + "-format-row", Class: "composer-format-row"}, formatToolbar(m, target, disabled))
}

// composerToolsClick handles the clicks that belong to the tool row. It runs
// first in the workspace's one delegated click handler and reports whether the
// click was one of its own.
func composerToolsClick(e ui.MouseEvent, m Model, local localStore) bool {
	// A click anywhere outside the Add control closes an open Add menu.
	dismissComposerAddMenu(e)
	action, id, extra := eventAction(e)
	if action != "command-pick" && local.get().commandMenu.Open {
		// A click anywhere but a command row closes the "/" list.
		local.update(func(u *localUI) { u.commandMenu = composerCommandMenu{} })
	}
	switch action {
	case "command-pick":
		if index, err := strconv.Atoi(extra); err == nil {
			commandMenuPick(m, local, index-1)
		}
		return true
	case "composer-add":
		composerAddChoose(m, local, extra)
		return true
	case "composer-mention":
		insertComposerMention(id)
		return true
	case "composer-format-toggle":
		composerFormatChoose(m, local)
		return true
	}
	return false
}

// composerAddChoose runs one Add menu item. The menu closes first, focus going
// back to the + button, so the surface the item opens can take focus itself.
func composerAddChoose(m Model, local localStore, kind string) {
	closeComposerAddMenu(true)
	switch kind {
	case "poll", "todo":
		// The same surfaces the header's Channel poll and To-do list buttons
		// open. Choosing one that is already open leaves it open.
		action := "open-" + kind
		next, opening := chatTrayToggle(local.get().tray, action)
		if !opening {
			return
		}
		rememberComposerLayerOpener(kind)
		local.update(func(u *localUI) { u.tray = next })
		if kind == "todo" && m.Callbacks.OpenChannelTodo != nil {
			m.Callbacks.OpenChannelTodo()
		}
		if kind == "poll" && m.Callbacks.OpenChannelPoll != nil {
			m.Callbacks.OpenChannelPoll(m.SelectedID)
		}
	case "location":
		clickComposerControl("[data-chatmap-action=toggle]")
	case "voice":
		clickComposerControl("[data-chatvoice-action=toggle]")
	}
}

// composerFormatChoose flips the formatting row. The choice shows at once from
// the local state; the callback, where the client has one, keeps it for the
// viewer.
func composerFormatChoose(m Model, local localStore) {
	next := composerFormatShown
	if composerFormatVisible(m, local.get()) {
		next = composerFormatHidden
	}
	local.update(func(u *localUI) { u.formatRow = next })
	if m.Callbacks.SetComposerFormatRow != nil {
		m.Callbacks.SetComposerFormatRow(next == composerFormatShown)
	}
}

// composerInsertAt writes text over the UTF-16 selection [start,end) of a
// composer and returns the new value and the caret after the insert.
func composerInsertAt(value, insert string, start, end int) (string, int) {
	units := utf16.Encode([]rune(value))
	start = max(0, min(start, len(units)))
	end = max(start, min(end, len(units)))
	added := utf16.Encode([]rune(insert))
	out := make([]uint16, 0, len(units)-(end-start)+len(added))
	out = append(out, units[:start]...)
	out = append(out, added...)
	out = append(out, units[end:]...)
	return string(utf16.Decode(out)), start + len(added)
}

// composerMentionInsert is the text and caret after the @ button is pressed
// with the selection [start,end). An @ typed against a letter reads as part of
// an address, not a mention, so one is preceded by a space in that case.
func composerMentionInsert(value string, start, end int) (string, int) {
	units := utf16.Encode([]rune(value))
	insert := "@"
	start = max(0, min(start, len(units)))
	if start > 0 {
		prev := rune(units[start-1])
		if unicode.IsLetter(prev) || unicode.IsDigit(prev) || prev == '_' || prev == '@' {
			insert = " @"
		}
	}
	return composerInsertAt(value, insert, start, end)
}

// composerHintSends is how many messages the person sends before the
// "Enter to send" hint has taught what it says and goes.
const composerHintSends = 3

// composerHintVisible is whether the Enter / Shift+Enter hint is drawn: until
// the viewer has sent three messages, counted as the larger of what they sent
// this session and what they have in the conversation on screen.
func composerHintVisible(m Model, local localUI) bool {
	sent := local.sentCount
	own := 0
	for _, message := range m.Messages {
		if m.CurrentUser != "" && message.AuthorID == m.CurrentUser {
			own++
		}
	}
	return max(sent, own) < composerHintSends
}

// composerToolRow is the row under the text, left to right: Add (+), Mention
// (@), the emoji picker, the GIF picker where one is set up, the Formatting
// toggle (Aa) and the writing styles; at the right the hint and Send. The
// location and voice openers come last in the tree and are drawn over the +
// button (see ChatComposerToolsStyles): the Add menu presses them.
func composerToolRow(m Model, h handlers, id string, disabled, canSend bool) ui.Node {
	var hint ui.Node
	if composerHintVisible(m, h.local) {
		hint = html.Span(html.Props{ID: "composer-help", Class: "composer-help kbd-hint", Text: m.t(KeyComposeHint)})
	}
	return html.Div(html.Props{Class: "composer-toolbar"},
		html.Div(html.Props{Class: "composer-tools"},
			composerAddControl(m, id, disabled),
			composerMentionButton(m, id, disabled),
			chatEmojiComposerPicker(m, h.local, id, disabled),
			giphyPickerControl(m, id, disabled),
			composerFormatToggle(m, id, disabled, h.local),
			chattoneToolbar(m, id, disabled),
			chatvoiceComposer(m, disabled),
			chatmapComposerControl(m, id, disabled),
		),
		hint,
		html.Button(html.Props{Class: "send-button", Type: "submit", Disabled: !canSend, Aria: map[string]string{"label": m.t(KeySend), "disabled": boolString(!canSend)}, Title: m.t(KeySend)}, icon("send"), html.Span(html.Props{Class: "send-label", Text: m.t(KeySend)})),
	)
}
