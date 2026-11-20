package chatui

import (
	"strconv"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Issue 3936: a reversible Unread filter for the conversation list.
//
// The filter is a view over the rows the sidebar already draws; it changes no
// section, no read state and no notification setting, and it is the viewer's own
// (kept in the page session, never sent anywhere). A row is unread the way
// railRow already says so: it has mentions, or unread messages and is not muted.
// A muted conversation without a mention is left out and the list says so.
// Because the rows and the counts come from the same conversations the sidebar
// is given, they follow the viewer's current membership and visibility.
//
// A row that becomes read while the filter is on (read here, or on another
// device) stays in the list until the filter is turned off and on again, so the
// row under the person's pointer or keyboard focus never moves or vanishes. The
// open conversation always stays.

const (
	issue3936Toggle   = "toggle-unread-filter"
	issue3936ToggleID = "issue3936-toggle"

	keyIssue3936Label     = "chat.issue3936.label"
	keyIssue3936Hint      = "chat.issue3936.hint"
	keyIssue3936Count     = "chat.issue3936.count"
	keyIssue3936Mention   = "chat.issue3936.mention"
	keyIssue3936Showing   = "chat.issue3936.showing"
	keyIssue3936Empty     = "chat.issue3936.empty"
	keyIssue3936Muted     = "chat.issue3936.muted"
	keyIssue3936ShowAll   = "chat.issue3936.show_all"
	keyIssue3936Loading   = "chat.issue3936.loading"
	keyIssue3936Unavail   = "chat.issue3936.unavailable"
	keyIssue3936CountNone = "chat.issue3936.count_none"
)

var issue3936Copy = map[string]map[string]string{
	"en-US": {
		keyIssue3936Label:     "Unread",
		keyIssue3936Hint:      "Show only conversations with unread messages or mentions",
		keyIssue3936Count:     "{n} unread",
		keyIssue3936CountNone: "none unread",
		keyIssue3936Mention:   "including mentions of you",
		keyIssue3936Showing:   "Unread filter on: {n} unread. Muted conversations without a mention are left out.",
		keyIssue3936Empty:     "You are all caught up. No conversation has unread messages or mentions.",
		keyIssue3936Muted:     "Muted conversations without a mention are not shown here.",
		keyIssue3936ShowAll:   "Show all conversations",
		keyIssue3936Loading:   "Loading your conversations",
		keyIssue3936Unavail:   "Your conversations could not be loaded, so unread ones cannot be listed. Show all conversations to try again.",
	},
	"de-DE": {
		keyIssue3936Label:     "Ungelesen",
		keyIssue3936Hint:      "Nur Unterhaltungen mit ungelesenen Nachrichten oder Erwähnungen zeigen",
		keyIssue3936Count:     "{n} ungelesen",
		keyIssue3936CountNone: "nichts ungelesen",
		keyIssue3936Mention:   "darunter Erwähnungen von Ihnen",
		keyIssue3936Showing:   "Filter „Ungelesen“ aktiv: {n} ungelesen. Stummgeschaltete Unterhaltungen ohne Erwähnung fehlen.",
		keyIssue3936Empty:     "Alles gelesen. Keine Unterhaltung hat ungelesene Nachrichten oder Erwähnungen.",
		keyIssue3936Muted:     "Stummgeschaltete Unterhaltungen ohne Erwähnung werden hier nicht angezeigt.",
		keyIssue3936ShowAll:   "Alle Unterhaltungen zeigen",
		keyIssue3936Loading:   "Ihre Unterhaltungen werden geladen",
		keyIssue3936Unavail:   "Ihre Unterhaltungen konnten nicht geladen werden, daher lassen sich ungelesene nicht auflisten. Zeigen Sie alle Unterhaltungen, um es erneut zu versuchen.",
	},
	"ar": {
		keyIssue3936Label:     "غير المقروءة",
		keyIssue3936Hint:      "إظهار المحادثات التي فيها رسائل غير مقروءة أو إشارات فقط",
		keyIssue3936Count:     "{n} غير مقروءة",
		keyIssue3936CountNone: "لا توجد غير مقروءة",
		keyIssue3936Mention:   "بما في ذلك إشارات إليك",
		keyIssue3936Showing:   "مرشح غير المقروءة مفعّل: {n} غير مقروءة. المحادثات المكتومة بلا إشارة غير مشمولة.",
		keyIssue3936Empty:     "لقد اطلعت على كل شيء. لا توجد محادثة فيها رسائل غير مقروءة أو إشارات.",
		keyIssue3936Muted:     "المحادثات المكتومة بلا إشارة لا تظهر هنا.",
		keyIssue3936ShowAll:   "إظهار كل المحادثات",
		keyIssue3936Loading:   "جارٍ تحميل محادثاتك",
		keyIssue3936Unavail:   "تعذر تحميل محادثاتك، لذلك لا يمكن عرض غير المقروءة. أظهر كل المحادثات للمحاولة مرة أخرى.",
	},
}

// issue3936Unread says whether a row is unread the way railRow draws it.
func issue3936Unread(c Conversation) bool {
	return (c.Unread > 0 && !c.Muted) || c.Mentions > 0
}

// issue3936Sections is the sections the sidebar draws, before any filter.
func issue3936Sections(m Model) []SidebarSection {
	sections := m.Sections
	if len(sections) == 0 && len(m.Conversations) > 0 {
		sections = defaultSections(m)
	}
	return chatside001Sections(m, chatbug084Sections(m, sections))
}

// issue3936Tally counts the unread conversations of the list, whether any has a
// mention, and how many muted ones are left out.
func issue3936Tally(sections []SidebarSection) (unread int, mention bool, mutedOut int) {
	for _, section := range sections {
		for _, c := range section.Chats {
			switch {
			case issue3936Unread(c):
				unread++
				mention = mention || c.Mentions > 0
			case c.Unread > 0 && c.Muted:
				mutedOut++
			}
		}
	}
	return unread, mention, mutedOut
}

// issue3936Snapshot is the set of conversations unread right now, taken when the
// filter is turned on.
func issue3936Snapshot(m Model) map[string]bool {
	out := map[string]bool{}
	for _, section := range issue3936Sections(m) {
		for _, c := range section.Chats {
			if issue3936Unread(c) {
				out[c.ID] = true
			}
		}
	}
	return out
}

// issue3936View is the sections as the filtered list shows them: every section
// open (the stored fold is untouched), only the rows that are unread, were
// unread when the filter was turned on, or are open, and no empty section.
func issue3936View(m Model, kept map[string]bool, sections []SidebarSection) (view []SidebarSection, rows int) {
	for _, section := range sections {
		chats := make([]Conversation, 0, len(section.Chats))
		for _, c := range section.Chats {
			if issue3936Unread(c) || kept[c.ID] || c.ID == m.SelectedID {
				chats = append(chats, c)
			}
		}
		if len(chats) == 0 {
			continue
		}
		section.Chats, section.Collapsed = chats, false
		rows += len(chats)
		view = append(view, section)
	}
	return view, rows
}

// issue3936Control is the Unread toggle above the list: a pressed-state button
// with the unread count and a mark when any of it is a mention, and a polite
// status line that says what the list now shows.
func issue3936Control(m Model, h handlers, sections []SidebarSection) ui.Node {
	on := h.local.unreadOnly
	unread, mention, _ := issue3936Tally(sections)
	count := laneText(m, issue3936Copy, keyIssue3936CountNone)
	if unread > 0 {
		count = laneTextf(m, issue3936Copy, keyIssue3936Count, map[string]string{"n": m.n(unread)})
	}
	label := laneText(m, issue3936Copy, keyIssue3936Label)
	name := label + ", " + count
	children := []ui.Node{html.Span(html.Props{Class: "issue3936-name", Text: label})}
	if unread > 0 {
		class := "issue3936-count"
		if mention {
			class += " has-mention"
			name += ", " + laneText(m, issue3936Copy, keyIssue3936Mention)
		}
		children = append(children, html.Span(html.Props{Class: class, Data: map[string]string{"unread-count": strconv.Itoa(unread)}, Aria: map[string]string{"hidden": "true"}, Text: m.n(unread)}))
	}
	class := "issue3936-toggle"
	if on {
		class += " on"
	}
	button := html.Button(html.Props{ID: issue3936ToggleID, Class: class, Type: "button", Title: laneText(m, issue3936Copy, keyIssue3936Hint),
		Data: map[string]string{"action": issue3936Toggle},
		Aria: map[string]string{"pressed": boolString(on), "label": name}}, children...)
	status := ""
	if on && m.State != StateLoading && m.State != StateError {
		status = laneTextf(m, issue3936Copy, keyIssue3936Showing, map[string]string{"n": m.n(unread)})
	}
	return html.Div(html.Props{Class: "issue3936-filter"},
		html.WithKey(button, "toggle"),
		html.WithKey(html.Span(html.Props{Class: "sr-only", Role: "status", Aria: map[string]string{"live": "polite"}, Text: status}), "status"))
}

// issue3936Note is what stands in the filtered list instead of rows when there
// are none: loading, could not load, or all caught up, each with a way back.
// With rows it is the one line saying muted conversations are left out.
func issue3936Note(m Model, mutedOut int, rows int) ui.Node {
	back := html.Button(html.Props{Class: "button secondary small issue3936-back", Type: "button",
		Data: map[string]string{"action": issue3936Toggle}, Text: laneText(m, issue3936Copy, keyIssue3936ShowAll)})
	switch {
	case rows > 0:
		if mutedOut == 0 {
			return nil
		}
		return html.P(html.Props{Class: "rail-empty issue3936-muted", Text: laneText(m, issue3936Copy, keyIssue3936Muted)})
	case m.State == StateLoading:
		return html.Div(html.Props{Class: "issue3936-note", Role: "status"},
			html.P(html.Props{Class: "rail-empty", Text: laneText(m, issue3936Copy, keyIssue3936Loading)}))
	case m.State == StateError:
		return html.Div(html.Props{Class: "issue3936-note", Role: "alert"},
			html.P(html.Props{Class: "rail-empty", Text: laneText(m, issue3936Copy, keyIssue3936Unavail)}), back)
	}
	children := []ui.Node{html.P(html.Props{Class: "rail-empty", Text: laneText(m, issue3936Copy, keyIssue3936Empty)})}
	if mutedOut > 0 {
		children = append(children, html.P(html.Props{Class: "rail-empty issue3936-muted", Text: laneText(m, issue3936Copy, keyIssue3936Muted)}))
	}
	return html.Div(html.Props{Class: "issue3936-note", Role: "status"}, append(children, back)...)
}

// issue3936Click turns the filter on and off. It reports whether it consumed the
// click. Turning it on remembers which conversations are unread right now.
func issue3936Click(e ui.MouseEvent, m Model, local localStore) bool {
	if action, _, _ := eventAction(e); action != issue3936Toggle {
		return false
	}
	if local.get().unreadOnly {
		local.update(func(u *localUI) { u.unreadOnly, u.unreadKept = false, nil })
		// The "Show all conversations" button in the list vanishes with the
		// filter; focus goes to the toggle instead of falling to the page.
		focusField(issue3936ToggleID)
		return true
	}
	kept := issue3936Snapshot(m)
	local.update(func(u *localUI) { u.unreadOnly, u.unreadKept = true, kept })
	return true
}

// Issue3936Styles lays the toggle out as a full-width row with a 44px target on
// phones; logical properties keep it right in right-to-left.
const Issue3936Styles = `.issue3936-filter{padding:0 .75rem .25rem}` +
	`.issue3936-toggle{display:flex;align-items:center;justify-content:space-between;gap:.5rem;width:100%;min-height:2rem;padding:.25rem .5rem;border:1px solid var(--line,transparent);border-radius:.5rem;background:none;color:inherit;font:inherit;text-align:start;cursor:pointer}` +
	`.issue3936-toggle.on{background:var(--accent-soft,rgba(0,0,0,.08));font-weight:600}` +
	`.issue3936-toggle:focus-visible{outline:2px solid var(--hcm-color-focus);outline-offset:2px}` +
	`.issue3936-count{min-width:1.25rem;padding:0 .375rem;border-radius:999px;border:1px solid currentColor;text-align:center;font-size:.75rem;font-variant-numeric:tabular-nums}` +
	`.issue3936-count.has-mention{background:var(--accent,currentColor);color:var(--accent-contrast,#fff);border-color:transparent}` +
	`.issue3936-note{padding:.5rem .75rem;display:grid;gap:.5rem;justify-items:start}` +
	`.issue3936-muted{font-size:.8125rem}` +
	`@media (max-width:640px){.issue3936-toggle,.issue3936-back{min-height:44px}}`
