package chatui

import (
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Issue 3915: one place in Chat navigation that gathers the person's unsent
// conversation drafts.
//
// The list is a view over the draft state the composer already keeps (the
// server-backed projection in Preferences.Drafts, the tab's browserDrafts and
// the open composer's text). It stores nothing of its own, so a draft cleared,
// sent, revoked or dropped on an identity change leaves the list at the same
// moment it leaves the composer (#864). Only conversations the person can still
// open are listed or counted. The text is drawn only while the person has the
// list open, in their own sidebar; the closed entry carries a count of drafts,
// not their text, and nothing here reaches a notification or another person.
//
// Opening an entry selects its conversation and focuses its composer; it never
// sends. Discard asks twice (the second press names itself "Confirm discard")
// and clears only that conversation's draft.
//
// Drafts inside a thread are not listed: the thread reply box is not kept
// across a reload (#3904), and the list says so rather than promise it.

const (
	issue3915Toggle   = "drafts-toggle"
	issue3915OpenAct  = "drafts-open"
	issue3915Discards = "drafts-discard"
	issue3915ToggleID = "issue3915-toggle"
	issue3915PanelID  = "issue3915-panel"

	keyIssue3915Label    = "chat.issue3915.label"
	keyIssue3915Count    = "chat.issue3915.count"
	keyIssue3915CountNil = "chat.issue3915.count_none"
	keyIssue3915Empty    = "chat.issue3915.empty"
	keyIssue3915Open     = "chat.issue3915.open"
	keyIssue3915Discard  = "chat.issue3915.discard"
	keyIssue3915DiscardN = "chat.issue3915.discard_name"
	keyIssue3915Confirm  = "chat.issue3915.confirm"
	keyIssue3915ConfirmN = "chat.issue3915.confirm_name"
	keyIssue3915Done     = "chat.issue3915.discarded"
	keyIssue3915Threads  = "chat.issue3915.threads"
	keyIssue3915OpenNow  = "chat.issue3915.open_now"
)

var issue3915Copy = map[string]map[string]string{
	"en-US": {
		keyIssue3915Label:    "Drafts",
		keyIssue3915Count:    "Drafts, {n} unsent",
		keyIssue3915CountNil: "Drafts, none",
		keyIssue3915Empty:    "No unsent drafts. Text you start in a conversation and leave appears here.",
		keyIssue3915Open:     "Open draft in {name}",
		keyIssue3915OpenNow:  "open now",
		keyIssue3915Discard:  "Discard draft",
		keyIssue3915DiscardN: "Discard draft in {name}",
		keyIssue3915Confirm:  "Confirm discard",
		keyIssue3915ConfirmN: "Confirm discard of the draft in {name}, for good",
		keyIssue3915Done:     "Draft in {name} discarded.",
		keyIssue3915Threads:  "Drafts inside a thread are not listed here yet.",
	},
	"de-DE": {
		keyIssue3915Label:    "Entwürfe",
		keyIssue3915Count:    "Entwürfe, {n} ungesendet",
		keyIssue3915CountNil: "Entwürfe, keine",
		keyIssue3915Empty:    "Keine ungesendeten Entwürfe. Text, den Sie in einer Unterhaltung beginnen und stehen lassen, erscheint hier.",
		keyIssue3915Open:     "Entwurf in {name} öffnen",
		keyIssue3915OpenNow:  "gerade geöffnet",
		keyIssue3915Discard:  "Entwurf verwerfen",
		keyIssue3915DiscardN: "Entwurf verwerfen: {name}",
		keyIssue3915Confirm:  "Verwerfen bestätigen",
		keyIssue3915ConfirmN: "Verwerfen bestätigen: Entwurf in {name} endgültig",
		keyIssue3915Done:     "Entwurf in {name} verworfen.",
		keyIssue3915Threads:  "Entwürfe in Threads werden hier noch nicht aufgelistet.",
	},
	"ar": {
		keyIssue3915Label:    "المسودات",
		keyIssue3915Count:    "المسودات، {n} غير مرسلة",
		keyIssue3915CountNil: "المسودات، لا توجد",
		keyIssue3915Empty:    "لا توجد مسودات غير مرسلة. النص الذي تبدأ كتابته في محادثة وتتركه يظهر هنا.",
		keyIssue3915Open:     "فتح المسودة في {name}",
		keyIssue3915OpenNow:  "مفتوحة الآن",
		keyIssue3915Discard:  "تجاهل المسودة",
		keyIssue3915DiscardN: "تجاهل المسودة في {name}",
		keyIssue3915Confirm:  "تأكيد التجاهل",
		keyIssue3915ConfirmN: "تأكيد التجاهل: المسودة في {name} نهائيًا",
		keyIssue3915Done:     "تم تجاهل المسودة في {name}.",
		keyIssue3915Threads:  "المسودات داخل المحادثات المتفرعة غير مدرجة هنا بعد.",
	},
}

// issue3915Bodies is the text of every conversation draft the person holds, by
// conversation: the server-backed projection, then the tab's own drafts (a
// cleared one removes the entry) and the open composer. Blank text is no draft.
func issue3915Bodies(model Model, d *browserDrafts) map[string]string {
	out := map[string]string{}
	for id, body := range model.Preferences.Drafts {
		if strings.TrimSpace(body) != "" {
			out[id] = body
		}
	}
	if d != nil {
		for id, body := range d.values {
			if strings.TrimSpace(body) != "" {
				out[id] = body
			} else {
				delete(out, id)
			}
		}
	}
	if model.SelectedID != "" && strings.TrimSpace(model.Draft) != "" {
		out[model.SelectedID] = model.Draft
	}
	return out
}

// issue3915Entry is one conversation holding a draft the person can still open.
type issue3915Entry struct {
	Conversation Conversation
	Body         string
}

// issue3915Entries lists the drafts of conversations the person can still open,
// in the order the sidebar draws them, folded sections included. A draft whose
// conversation is gone from the person's list (access lost) is not an entry.
func issue3915Entries(m Model) []issue3915Entry {
	var out []issue3915Entry
	seen := map[string]bool{}
	known := map[string]Conversation{}
	for _, c := range m.Conversations {
		known[c.ID] = c
	}
	add := func(c Conversation) {
		if seen[c.ID] {
			return
		}
		seen[c.ID] = true
		direct := c.Kind == DirectMessage || c.Kind == GroupChat
		if !c.Joined && !direct {
			return
		}
		if body := m.railDraftBodies[c.ID]; strings.TrimSpace(body) != "" {
			out = append(out, issue3915Entry{Conversation: c, Body: body})
		}
	}
	for _, section := range issue3936Sections(m) {
		for _, c := range section.Chats {
			if live, ok := known[c.ID]; ok {
				add(live)
			}
		}
	}
	for _, c := range m.Conversations {
		add(c)
	}
	return out
}

// issue3915Excerpt is the first words of a draft on one line.
func issue3915Excerpt(body string) string {
	runes := []rune(strings.Join(strings.Fields(body), " "))
	if len(runes) > 80 {
		return string(runes[:80]) + "…"
	}
	return string(runes)
}

// issue3915Sidebar is the Drafts entry above the conversation list and, while
// open, the list of drafts under it.
func issue3915Sidebar(m Model, h handlers) ui.Node {
	entries := issue3915Entries(m)
	open := h.local.draftsOpen
	label := laneText(m, issue3915Copy, keyIssue3915Label)
	name := laneText(m, issue3915Copy, keyIssue3915CountNil)
	children := []ui.Node{icon("document"), html.Span(html.Props{Class: "chat-row-name", Text: label})}
	if n := len(entries); n > 0 {
		name = laneTextf(m, issue3915Copy, keyIssue3915Count, map[string]string{"n": m.n(n)})
		children = append(children, html.Span(html.Props{Class: "chat-count issue3915-count", Data: map[string]string{"drafts-count": strconv.Itoa(n)}, Aria: map[string]string{"hidden": "true"}, Text: m.n(n)}))
	}
	toggle := html.Button(html.Props{ID: issue3915ToggleID, Type: "button", Class: "chat-row issue3915-toggle", Data: map[string]string{"action": issue3915Toggle},
		Aria: map[string]string{"expanded": boolString(open), "controls": issue3915PanelID, "label": name}}, children...)
	var panel ui.Node
	if open {
		panel = html.Div(html.Props{ID: issue3915PanelID, Class: "issue3915-panel", Role: "region", Aria: map[string]string{"label": label}}, issue3915Body(m, h, entries)...)
	} else {
		panel = html.Div(html.Props{ID: issue3915PanelID, Class: "issue3915-panel", Hidden: true})
	}
	status := html.Span(html.Props{Class: "sr-only", Role: "status", Aria: map[string]string{"live": "polite"}, Text: h.local.draftsNote})
	return html.Div(html.Props{Class: "issue3915", Data: map[string]string{"drafts-sidebar": "true"}},
		html.WithKey(toggle, "toggle"), html.WithKey(panel, "panel"), html.WithKey(status, "status"))
}

func issue3915Body(m Model, h handlers, entries []issue3915Entry) []ui.Node {
	threads := html.P(html.Props{Class: "rail-empty issue3915-note", Text: laneText(m, issue3915Copy, keyIssue3915Threads)})
	if len(entries) == 0 {
		return []ui.Node{html.P(html.Props{Class: "rail-empty", Text: laneText(m, issue3915Copy, keyIssue3915Empty)}), threads}
	}
	items := make([]ui.Node, 0, len(entries))
	for _, e := range entries {
		c := e.Conversation
		cname := displayName(m, c)
		meta := []ui.Node{html.Span(html.Props{Class: "issue3915-kind", Text: m.t(kindKey(c.Kind))})}
		if c.ID == m.SelectedID {
			meta = append(meta, html.Span(html.Props{Class: "issue3915-kind", Text: "· " + laneText(m, issue3915Copy, keyIssue3915OpenNow)}))
		}
		discardText := laneText(m, issue3915Copy, keyIssue3915Discard)
		discardLabel := laneTextf(m, issue3915Copy, keyIssue3915DiscardN, map[string]string{"name": cname})
		class := "issue3915-discard"
		if h.local.draftsAsk == c.ID {
			discardText = laneText(m, issue3915Copy, keyIssue3915Confirm)
			discardLabel = laneTextf(m, issue3915Copy, keyIssue3915ConfirmN, map[string]string{"name": cname})
			class += " danger"
		}
		excerpt := issue3915Excerpt(e.Body)
		open := html.Button(html.Props{ID: issue3915OpenID(c.ID), Type: "button", Class: "issue3915-open", Disabled: m.Callbacks.SelectConversation == nil,
			Data: map[string]string{"action": issue3915OpenAct, "id": c.ID},
			Aria: map[string]string{"label": laneTextf(m, issue3915Copy, keyIssue3915Open, map[string]string{"name": cname}) + ": " + excerpt}},
			html.Span(html.Props{Class: "issue3915-name", Dir: "auto", Text: cname}),
			html.Span(html.Props{Class: "issue3915-meta"}, meta...),
			html.Span(html.Props{Class: "issue3915-excerpt", Dir: "auto", Text: excerpt}))
		discard := html.Button(html.Props{Type: "button", Class: class, Data: map[string]string{"action": issue3915Discards, "id": c.ID},
			Aria: map[string]string{"label": discardLabel}, Text: discardText})
		items = append(items, html.WithKey(html.Li(html.Props{Class: "issue3915-item", Data: map[string]string{"draft-conversation": c.ID}}, open, discard), "draft:"+c.ID))
	}
	return []ui.Node{html.Ul(html.Props{Class: "issue3915-list"}, items...), threads}
}

func issue3915OpenID(conversationID string) string { return "issue3915-open-" + conversationID }

// issue3915Click handles the entry's three buttons and reports whether the
// press was on one.
func issue3915Click(e ui.MouseEvent, m Model, local localStore, drafts *browserDrafts, clearMention func()) bool {
	action, id, _ := eventAction(e)
	switch action {
	case issue3915Toggle:
		e.PreventDefault()
		local.update(func(u *localUI) { u.draftsOpen, u.draftsAsk, u.draftsNote = !u.draftsOpen, "", "" })
		return true
	case issue3915OpenAct:
		e.PreventDefault()
		issue3915Open(m, local, drafts, id)
		return true
	case issue3915Discards:
		e.PreventDefault()
		issue3915Discard(m, local, drafts, id, clearMention)
		return true
	}
	return false
}

// issue3915Open goes to the conversation holding the draft and puts the caret
// in its composer. The draft being left is kept first, exactly as selecting a
// row does; nothing is sent.
func issue3915Open(m Model, local localStore, drafts *browserDrafts, id string) {
	if id == "" || m.Callbacks.SelectConversation == nil {
		return
	}
	local.update(func(u *localUI) { u.draftsAsk, u.draftsNote = "", "" })
	if id != m.SelectedID {
		current := domDraftValue("chat-composer", m.SelectedID)
		if current == "" {
			current = m.Draft
		}
		drafts.selectConversation(m.SelectedID, current, id)
		if m.Callbacks.DraftChanged != nil && m.SelectedID != "" {
			m.Callbacks.DraftChanged(m.SelectedID, current)
		}
		if strings.TrimSpace(m.Search) != "" && m.Callbacks.Search != nil {
			m.Callbacks.Search("")
		}
		m.Callbacks.SelectConversation(id)
	}
	if m.Callbacks.ToggleSidebar != nil && m.SidebarOpen {
		m.Callbacks.ToggleSidebar(false)
	}
	FocusComposerFor(id)
}

// issue3915Discard throws away one conversation's draft after a second press.
// Other drafts are untouched; focus moves to the entry that takes its place,
// or to the Drafts button when none is left.
func issue3915Discard(m Model, local localStore, drafts *browserDrafts, id string, clearMention func()) {
	if id == "" {
		return
	}
	if local.get().draftsAsk != id {
		local.update(func(u *localUI) { u.draftsAsk, u.draftsNote = id, "" })
		return
	}
	name, focus, index := id, issue3915ToggleID, -1
	var rest []string
	for _, e := range issue3915Entries(m) {
		if e.Conversation.ID == id {
			name, index = displayName(m, e.Conversation), len(rest)
			continue
		}
		rest = append(rest, e.Conversation.ID)
	}
	if len(rest) > 0 && index >= 0 {
		if index >= len(rest) {
			index = len(rest) - 1
		}
		focus = issue3915OpenID(rest[index])
	}
	drafts.set(id, "")
	delete(m.Preferences.Drafts, id)
	if m.Callbacks.DraftChanged != nil {
		m.Callbacks.DraftChanged(id, "")
	}
	if id == m.SelectedID {
		setDOMValue("chat-composer", "")
		if clearMention != nil {
			clearMention()
		}
	}
	note := laneTextf(m, issue3915Copy, keyIssue3915Done, map[string]string{"name": name})
	local.update(func(u *localUI) { u.draftsAsk, u.draftsNote = "", note })
	focusField(focus)
}

// Issue3915Styles lays the entry out as a sidebar row with a 44px target on
// phones; logical properties keep it right in right-to-left.
const Issue3915Styles = `.issue3915-count{margin-inline-start:auto}` +
	`.issue3915-toggle:focus-visible,.issue3915-open:focus-visible,.issue3915-discard:focus-visible{outline:2px solid var(--hcm-color-focus);outline-offset:2px}` +
	`.issue3915-panel{max-height:40vh;overflow:auto;padding-block:.25rem}` +
	`.issue3915-list{list-style:none;margin:0;padding:0;display:grid;gap:.25rem}` +
	`.issue3915-item{display:flex;align-items:stretch;gap:.25rem;border:1px solid var(--line,transparent);border-radius:.5rem}` +
	`.issue3915-open{flex:1 1 auto;min-width:0;display:grid;gap:.125rem;padding:.25rem .5rem;border:0;background:none;color:inherit;font:inherit;text-align:start;cursor:pointer}` +
	`.issue3915-name{font-weight:600;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}` +
	`.issue3915-meta,.issue3915-kind{font-size:.75rem;color:var(--hcm-color-text-muted)}` +
	`.issue3915-meta{display:flex;gap:.25rem}` +
	`.issue3915-excerpt{font-size:.8125rem;color:var(--hcm-color-text-muted);overflow:hidden;text-overflow:ellipsis;white-space:nowrap}` +
	`.issue3915-discard{flex:none;margin-inline:.25rem;padding:.25rem .5rem;border:0;background:none;color:inherit;font:inherit;font-size:.75rem;cursor:pointer;text-decoration:underline}` +
	`.issue3915-discard.danger{color:var(--hcm-color-danger,#b00020);font-weight:600}` +
	`.issue3915-note{font-size:.75rem}` +
	`@media (max-width:640px){.issue3915-toggle,.issue3915-open,.issue3915-discard{min-height:44px}}`
