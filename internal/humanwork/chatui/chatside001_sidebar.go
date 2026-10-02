package chatui

import (
	"strings"
	"unicode/utf8"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATSIDE-001: sections of one's own. Favorites is drawn from the starred
// conversations and is never stored as a section; a conversation is in one
// section at a time, so a favorite is shown in Favorites only and goes back to
// the section it sits in when it is un-starred. The order of the sidebar is one
// rule: Favorites first, then the person's sections in the order they arranged
// (the ones they made and Channels and Direct messages alike), and Archived
// always last.

const (
	chatside001Favorites  = "favorites"
	chatside001NameMax    = 40
	chatside001CustomMax  = 20
	chatside001MenuPrefix = "section:"
	chatside001NameField  = "chat-rename-section"
)

// chatside001UI is the sidebar's own transient state: the section being
// renamed, the conversation that is waiting for the name of a new section, and
// the plain message under the open name field.
type chatside001UI struct {
	renaming, moveTo, err string
	// nameForm is the open name field, drawn by the workspace so the section
	// heading it replaces needs no handlers of its own.
	nameForm ui.Node
}

func chatside001IsCustom(id string) bool {
	return id != "channels" && id != "direct" && id != chatside001Favorites && id != "all"
}

// chatside001Starred is the set of starred conversation ids. The conversation
// list is the truth: the copies held in sections may be older.
func chatside001Starred(m Model) map[string]bool {
	out := map[string]bool{}
	for _, c := range m.Conversations {
		if c.Starred {
			out[c.ID] = true
		}
	}
	if len(m.Conversations) == 0 {
		for _, section := range m.Sections {
			for _, c := range section.Chats {
				if c.Starred {
					out[c.ID] = true
				}
			}
		}
	}
	return out
}

// chatside001Sections arranges the sections for the sidebar: Favorites first
// once it holds something, then the sections exactly as the person arranged
// them, never regrouped by kind (a new section starts at the top and the person
// moves it). Archived is not a section: it is the list at the foot of the rail.
// A favorite leaves the section it sits in while it is a favorite.
func chatside001Sections(m Model, sections []SidebarSection) []SidebarSection {
	starred := chatside001Starred(m)
	favorites := SidebarSection{ID: chatside001Favorites, Name: laneText(m, chatside001Copy, keyChatside001Favorites), Collapsed: m.FavoritesCollapsed}
	out := make([]SidebarSection, 0, len(sections)+1)
	out = append(out, SidebarSection{})
	for _, section := range sections {
		kept := make([]Conversation, 0, len(section.Chats))
		for _, c := range section.Chats {
			if starred[c.ID] {
				c.Starred = true
				favorites.Chats = append(favorites.Chats, c)
				continue
			}
			kept = append(kept, c)
		}
		section.Chats = kept
		out = append(out, section)
	}
	if len(favorites.Chats) > 0 {
		out[0] = favorites
		return out
	}
	return out[1:]
}

// Chatside001Neighbor is the index in sections of the section id would trade
// places with when moved by delta (-1 up, 1 down), or -1 when it is already at
// that end of the person's order.
func Chatside001Neighbor(sections []SidebarSection, id string, delta int) int {
	for i, section := range sections {
		if section.ID == id {
			if i+delta < 0 || i+delta >= len(sections) {
				return -1
			}
			return i + delta
		}
	}
	return -1
}

// chatside001UnreadTotal is the unread count of a whole section, shown on a
// collapsed heading; mention reports whether any of it is a mention.
func chatside001UnreadTotal(section SidebarSection) (total int, mention bool) {
	for _, c := range section.Chats {
		n := c.Unread
		if c.Mentions > n {
			n = c.Mentions
		}
		total += n
		mention = mention || c.Mentions > 0
	}
	return total, mention
}

// chatside001NameError is the plain refusal for a section name, or "" when it
// is fine. exceptID is the section being renamed, so it may keep its own name.
func chatside001NameError(m Model, name, exceptID string) string {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return laneText(m, chatside001Copy, keyChatside001ErrEmpty)
	case utf8.RuneCountInString(name) > chatside001NameMax:
		return laneText(m, chatside001Copy, keyChatside001ErrLong)
	case strings.EqualFold(name, "favorites") || strings.EqualFold(name, laneText(m, chatside001Copy, keyChatside001Favorites)):
		return laneText(m, chatside001Copy, keyChatside001ErrReserved)
	}
	custom := 0
	for _, section := range m.Sections {
		if !chatside001IsCustom(section.ID) {
			continue
		}
		custom++
		if section.ID != exceptID && strings.EqualFold(strings.TrimSpace(section.Name), name) {
			return laneText(m, chatside001Copy, keyChatside001ErrDuplicate)
		}
	}
	if exceptID == "" && custom >= chatside001CustomMax {
		return laneText(m, chatside001Copy, keyChatside001ErrLimit)
	}
	return ""
}

// chatside001SectionMenuButton is the three dots on a section heading. It is the
// row menu's own trigger, so it opens the same menu the same way.
func chatside001SectionMenuButton(m Model, section SidebarSection, name string) ui.Node {
	if section.ID == chatside001Favorites {
		return nil
	}
	id := chatside001MenuPrefix + section.ID
	label := laneTextf(m, chatside001Copy, keyChatside001SectionMore, map[string]string{"name": name})
	return html.Button(html.Props{Class: "rail-row-more section-more", Type: "button", Disabled: m.Callbacks.OpenRailMenu == nil,
		Data: map[string]string{"action": "rail-menu", "id": id},
		Aria: map[string]string{"label": label, "haspopup": "menu", "expanded": boolString(m.RailMenuID == id)}, Title: label}, icon("more-vertical"))
}

// chatside001Menu is the menu behind a section heading's three dots: Rename,
// Move up, Move down and Delete section for a section the person made, and the
// moves alone for the built-in ones.
func chatside001Menu(m Model) (ui.Node, bool) {
	if !strings.HasPrefix(m.RailMenuID, chatside001MenuPrefix) {
		return nil, false
	}
	id := strings.TrimPrefix(m.RailMenuID, chatside001MenuPrefix)
	name := id
	switch id {
	case "channels":
		name = m.t(KeySectionChannels)
	case "direct":
		name = m.t(KeySectionDirect)
	default:
		for _, section := range m.Sections {
			if section.ID == id {
				name = section.Name
			}
		}
	}
	item := func(action, class string, disabled bool, label string) ui.Node {
		return html.Button(html.Props{Class: class, Type: "button", Role: "menuitem", Disabled: disabled, Data: map[string]string{"action": action, "id": id}, Text: label})
	}
	var items []ui.Node
	if chatside001IsCustom(id) {
		items = append(items, item("side-rename", "menu-item", m.Callbacks.RenameSection == nil, laneText(m, chatside001Copy, keyChatside001Rename)))
	}
	if Chatside001Neighbor(m.Sections, id, -1) >= 0 {
		items = append(items, item("section-up", "menu-item", m.Callbacks.ReorderSection == nil, m.t(KeyMoveSectionUp)))
	}
	if Chatside001Neighbor(m.Sections, id, 1) >= 0 {
		items = append(items, item("section-down", "menu-item", m.Callbacks.ReorderSection == nil, m.t(KeyMoveSectionDown)))
	}
	if chatside001IsCustom(id) {
		items = append(items, item("section-remove", "menu-item danger", m.Callbacks.RemoveSection == nil, laneText(m, chatside001Copy, keyChatside001Delete)))
	}
	// CHATBUG-090: the section menu is the same anchored layer as the row menu;
	// as a plain div it sat in the top-left corner of the page.
	return anchoredChatLayer(html.Props{Class: "rail-row-menu", Role: "menu", Aria: map[string]string{"label": laneTextf(m, chatside001Copy, keyChatside001SectionMore, map[string]string{"name": name})}}, "rail-menu", items...), true
}

// chatside001Header is the heading of a section: its name (a button that folds
// it), the unread total while it is folded, and its menu. While the section is
// being renamed the heading is the name field.
func chatside001Header(m Model, section SidebarSection, name string, menu ui.Node) []ui.Node {
	if m.side.renaming == section.ID && m.side.nameForm != nil {
		return []ui.Node{m.side.nameForm}
	}
	chevron := "chevron-down"
	if section.Collapsed {
		chevron = "chevron-right"
	}
	title := []ui.Node{icon(chevron), html.Span(html.Props{Text: name}), html.Span(html.Props{Class: "section-count", Text: m.n(len(section.Chats))})}
	if section.Collapsed {
		if total, mention := chatside001UnreadTotal(section); total > 0 {
			class := "chat-badge section-unread"
			if mention {
				class += " mention"
			}
			title = append(title, html.Span(html.Props{Class: class, Text: m.n(total), Aria: map[string]string{"label": laneTextf(m, chatside001Copy, keyChatside001UnreadIn, map[string]string{"n": m.n(total), "name": name})}}))
		}
	}
	controls := []ui.Node{html.Button(html.Props{Class: "section-title", Type: "button", Disabled: m.Callbacks.ToggleSection == nil, Data: map[string]string{"action": "toggle-section", "id": section.ID}, Aria: map[string]string{"expanded": boolString(!section.Collapsed)}}, title...)}
	// The heading carries one control of its own, the three dots: the menu holds
	// Rename, Move up, Move down and Delete section, and Channels adds its own
	// plus beside it.
	if more := chatside001SectionMenuButton(m, section, name); more != nil {
		controls = append(controls, more)
	}
	if menu != nil {
		controls = append(controls, menu)
	}
	return controls
}

// chatside001CreateError is the refusal shown under the add menu's New section
// field. The renaming and move-to-new-section fields show their own.
func chatside001CreateError(m Model) ui.Node {
	if m.side.err == "" || m.side.renaming != "" || m.side.moveTo != "" {
		return nil
	}
	return html.P(html.Props{Class: "section-name-error", Role: "alert", Text: m.side.err})
}

// chatside001NameForm is the small name field used to rename a section and to
// name a new one: one line, Enter saves, Escape or Cancel puts it away, and a
// refusal is said in words under the field.
func chatside001NameForm(m Model, h handlers, current string) ui.Node {
	children := []ui.Node{
		html.Label(html.Props{Class: "sr-only", For: chatside001NameField, Text: m.t(KeySectionName)}),
		html.Input(html.Props{ID: chatside001NameField, Class: "chat-input", Type: "text", MaxLength: 80, AutoFocus: true, AutoComplete: "off", Placeholder: m.t(KeySectionName), OnKeyDown: h.sideNameKey,
			Data: map[string]string{"chat-value": current}, Aria: map[string]string{"invalid": boolString(m.side.err != "")}}),
	}
	if m.side.err != "" {
		children = append(children, html.P(html.Props{Class: "section-name-error", Role: "alert", Text: m.side.err}))
	}
	children = append(children, html.Div(html.Props{Class: "section-create-actions"},
		html.Button(html.Props{Class: "button secondary small", Type: "button", Data: map[string]string{"action": "side-name-cancel"}, Text: m.t(KeyCancel)}),
		html.Button(html.Props{Class: "button small", Type: "submit", Text: m.t(KeyWidgetSave)})))
	return html.Form(html.Props{Class: "section-create-form section-rename-form", OnSubmit: h.sideNameSubmit}, children...)
}

// chatside001NamePrompt is the name field for a new section made from a row's
// menu: it sits at the top of the list until the name is saved or put away.
func chatside001NamePrompt(m Model) ui.Node {
	if m.side.moveTo == "" || m.side.nameForm == nil {
		return nil
	}
	return html.Div(html.Props{Class: "section-new-prompt"}, html.P(html.Props{Class: "section-new-title", Text: m.t(KeyNewSection)}), m.side.nameForm)
}

// chatside001OpenName is the name field the workspace draws, if one is open.
func chatside001OpenName(m Model, h handlers) ui.Node {
	switch {
	case m.side.renaming != "":
		current := ""
		for _, section := range m.Sections {
			if section.ID == m.side.renaming {
				current = section.Name
			}
		}
		return chatside001NameForm(m, h, current)
	case m.side.moveTo != "":
		return chatside001NameForm(m, h, "")
	}
	return nil
}

// chatside001NameSubmitted saves the open name field: a rename, or a new
// section that the waiting conversation moves into.
func chatside001NameSubmitted(m Model, local localStore) {
	state := local.get().side
	name := strings.TrimSpace(domValue(chatside001NameField))
	except := state.renaming
	if msg := chatside001NameError(m, name, except); msg != "" {
		local.update(func(u *localUI) { u.side.err = msg })
		return
	}
	switch {
	case state.renaming != "" && m.Callbacks.RenameSection != nil:
		m.Callbacks.RenameSection(state.renaming, name)
	case state.moveTo != "" && m.Callbacks.CreateSectionFor != nil:
		m.Callbacks.CreateSectionFor(name, state.moveTo)
	default:
		return
	}
	local.update(func(u *localUI) { u.side = chatside001UI{} })
}

// chatside001CreateSubmitted saves the add menu's New section form with the
// same rules; it reports whether the section was sent.
func chatside001CreateSubmitted(m Model, local localStore, name string) bool {
	name = strings.TrimSpace(name)
	if msg := chatside001NameError(m, name, ""); msg != "" {
		local.update(func(u *localUI) { u.side.err = msg })
		return false
	}
	if m.Callbacks.CreateSection == nil {
		return false
	}
	m.Callbacks.CreateSection(name)
	if local.get().side.err != "" {
		local.update(func(u *localUI) { u.side.err = "" })
	}
	return true
}

// chatside001NameKey puts the name field away on Escape.
func chatside001NameKey(e ui.KeyboardEvent, local localStore) {
	if e.GetKey() != "Escape" {
		return
	}
	e.PreventDefault()
	e.StopPropagation()
	local.update(func(u *localUI) { u.side = chatside001UI{} })
}

// chatside001Click handles the sidebar's own steps and reports whether it
// consumed the click: Add to favorites, Rename, Move to a new section and the
// name field's Cancel.
func chatside001Click(e ui.MouseEvent, m Model, local localStore) bool {
	action, id, extra := eventAction(e)
	closeMenu := func() {
		if m.Callbacks.OpenRailMenu != nil {
			m.Callbacks.OpenRailMenu("")
		}
	}
	switch action {
	case "side-fav":
		if m.Callbacks.SetFavorite != nil {
			m.Callbacks.SetFavorite(id, extra == "1")
		}
		closeMenu()
		return true
	case "side-rename":
		local.update(func(u *localUI) { u.side = chatside001UI{renaming: id} })
		closeMenu()
		focusField(chatside001NameField)
		return true
	case "side-move-new":
		local.update(func(u *localUI) { u.side = chatside001UI{moveTo: id} })
		closeMenu()
		focusField(chatside001NameField)
		return true
	case "side-name-cancel":
		local.update(func(u *localUI) { u.side = chatside001UI{} })
		return true
	case "open-section-create", "cancel-section-create":
		if local.get().side.err != "" {
			local.update(func(u *localUI) { u.side.err = "" })
		}
	}
	return false
}

// chatside001FavoriteItem is the row menu's Add to favorites or Remove from
// favorites.
func chatside001FavoriteItem(m Model, c Conversation) ui.Node {
	key, flag := keyChatside001AddFavorite, "1"
	if chatside001Starred(m)[c.ID] {
		key, flag = keyChatside001RemoveFavorite, "0"
	}
	return html.Button(html.Props{Class: "menu-item", Type: "button", Role: "menuitem", Disabled: m.Callbacks.SetFavorite == nil,
		Data: map[string]string{"action": "side-fav", "id": c.ID, "extra": flag}, Text: laneText(m, chatside001Copy, key)})
}

// chatside001NewSectionItem is the row menu's "Move to a new section".
func chatside001NewSectionItem(m Model, c Conversation) ui.Node {
	return html.Button(html.Props{Class: "menu-item", Type: "button", Role: "menuitem", Disabled: m.Callbacks.CreateSectionFor == nil,
		Data: map[string]string{"action": "side-move-new", "id": c.ID}, Text: laneText(m, chatside001Copy, keyChatside001MoveNew)})
}

// chatside001HeaderStar is the star in the conversation header: it favorites the
// open conversation and, pressed again, puts it back.
func chatside001HeaderStar(m Model, c Conversation, actions []ui.Node) []ui.Node {
	on := chatside001Starred(m)[c.ID]
	key, flag, glyph := keyChatside001AddFavorite, "1", "star"
	if on {
		key, flag, glyph = keyChatside001RemoveFavorite, "0", "star-filled"
	}
	label := laneText(m, chatside001Copy, key)
	star := html.Button(html.Props{Class: "icon-button chat-favorite-toggle", Type: "button", Title: label, Disabled: m.Callbacks.SetFavorite == nil,
		Data: map[string]string{"action": "side-fav", "id": c.ID, "extra": flag}, Aria: map[string]string{"label": label, "pressed": boolString(on)}}, icon(glyph))
	return append([]ui.Node{star}, actions...)
}
