package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Issue 3969: a private answer is not a private question. The Private answer
// switch only changes who sees the answer; the question is still a channel
// message. So the composer says that in plain words, and offers a separate
// route: "Ask this agent privately" moves the unsent text into the person's own
// conversation with the agent, where nothing is sent until they send it. The
// channel never receives the question, and nothing else (no messages, thread
// history or files) is carried along. Cancelling puts the text back in the
// channel draft.

const (
	issue3969AskAction    = "agent-ask-private"
	issue3969CancelAction = "agent-ask-private-cancel"
	issue3969HelpID       = "composer-private-ask-help"
)

// issue3969Origin remembers where a question was moved from, so the private
// conversation can say so and Cancel can put the text back.
type issue3969Origin struct {
	From     string // conversation the text came from
	FromName string // its name as the composer shows it
	Text     string // the channel draft exactly as it was
}

// issue3969Agent is the agent the draft asks and that the person may ask
// privately: the first mentioned agent that answers to the channel by default.
func issue3969Agent(m Model, refs []ChatReference) (ChatReference, bool) {
	for _, reference := range refs {
		if reference.Kind != "AGENT_MENTION" {
			continue
		}
		for _, candidate := range m.ResolvedPersonaMentions {
			if candidate.Reference.ID == reference.ID && candidate.ReplyPlacement != PersonaReplyPrivateAlways {
				return candidate.Reference, true
			}
		}
	}
	return ChatReference{}, false
}

// issue3969DirectRoom is the person's own conversation with the agent, when the
// conversation list holds one they have joined.
func issue3969DirectRoom(m Model, agentID string) (Conversation, bool) {
	if strings.TrimSpace(agentID) == "" {
		return Conversation{}, false
	}
	for _, room := range m.Conversations {
		if room.Kind == DirectMessage && room.Agent && room.AgentID == agentID && room.Joined {
			return room, true
		}
	}
	return Conversation{}, false
}

// issue3969Question is the unsent text without the agent's own @name: in the
// private conversation the agent is already the one being asked.
func issue3969Question(text string, agent ChatReference) string {
	text = strings.TrimSpace(text)
	token := "@" + strings.TrimSpace(agent.Display)
	if len(token) > 1 {
		if at := strings.Index(strings.ToLower(text), strings.ToLower(token)); at >= 0 {
			text = strings.TrimSpace(text[:at] + " " + strings.TrimSpace(text[at+len(token):]))
		}
	}
	return strings.Join(strings.Fields(text), " ")
}

// issue3969Move carries the unsent text into the private conversation with
// agentID for review. It sends nothing. It reports whether the text moved; when
// the private conversation cannot be opened, the channel draft is left as it
// is and the composer says why.
func issue3969Move(m Model, local localStore, drafts *browserDrafts, clearMention func(), agentID, raw string) bool {
	agent, ok := issue3969Agent(m, []ChatReference{{Kind: "AGENT_MENTION", ID: agentID}})
	room, direct := issue3969DirectRoom(m, agentID)
	if !ok || !direct || m.Callbacks.SelectConversation == nil || m.SelectedID == "" || room.ID == m.SelectedID {
		local.update(func(u *localUI) { u.composerNotice = issue3969Text(m, "unavailable") })
		return false
	}
	text := issue3969Question(raw, agent)
	from := m.SelectedID
	origin := issue3969Origin{From: from, FromName: chatux003ChannelName(m), Text: raw}
	// The channel keeps nothing: the text lives in one place, the private draft.
	existing := drafts.selectConversation(from, "", room.ID)
	if strings.TrimSpace(existing) != "" && text != "" {
		text = existing + "\n" + text
	} else if text == "" {
		text = existing
	}
	drafts.set(room.ID, text)
	if clearMention != nil {
		clearMention()
	}
	if m.Callbacks.DraftChanged != nil {
		m.Callbacks.DraftChanged(from, "")
		m.Callbacks.DraftChanged(room.ID, text)
	}
	local.update(func(u *localUI) {
		u.composerNotice = ""
		u.privateAnswer = false
		u.privateAsk = map[string]issue3969Origin{room.ID: origin}
	})
	m.Callbacks.SelectConversation(room.ID)
	FocusComposerFor(room.ID)
	return true
}

// issue3969Cancel puts the question back in the channel draft and returns to
// the channel; the private draft is dropped.
func issue3969Cancel(m Model, local localStore, drafts *browserDrafts) bool {
	origin, ok := local.get().privateAsk[m.SelectedID]
	if !ok || m.Callbacks.SelectConversation == nil {
		return false
	}
	drafts.set(m.SelectedID, "")
	drafts.set(origin.From, origin.Text)
	if m.Callbacks.DraftChanged != nil {
		m.Callbacks.DraftChanged(m.SelectedID, "")
		m.Callbacks.DraftChanged(origin.From, origin.Text)
	}
	local.update(func(u *localUI) { delete(u.privateAsk, m.SelectedID) })
	m.Callbacks.SelectConversation(origin.From)
	FocusComposerFor(origin.From)
	return true
}

// issue3969Click handles the two buttons. It reports whether the press was on one.
func issue3969Click(e ui.MouseEvent, m Model, local localStore, drafts *browserDrafts, clearMention func()) bool {
	action, id, _ := eventAction(e)
	switch action {
	case issue3969AskAction:
		e.PreventDefault()
		raw := domDraftValue("chat-composer", m.SelectedID)
		if raw == "" {
			raw = m.Draft
		}
		issue3969Move(m, local, drafts, clearMention, id, raw)
		return true
	case issue3969CancelAction:
		e.PreventDefault()
		issue3969Cancel(m, local, drafts)
		return true
	}
	return false
}

// issue3969Notes is what sits in the Private answer row besides the switch: the
// plain statement that the question stays in the channel, and the way to ask
// privately instead.
func issue3969Notes(m Model, refs []ChatReference) []ui.Node {
	agent, ok := issue3969Agent(m, refs)
	if !ok {
		return nil
	}
	channel := chatux003ChannelName(m)
	notes := []ui.Node{html.Span(html.Props{Class: "composer-private-note", Dir: "auto",
		Text: issue3969Format(m, "question_visible", map[string]string{"channel": channel})})}
	_, direct := issue3969DirectRoom(m, agent.ID)
	help := issue3969Format(m, "ask_help", map[string]string{"agent": agent.Display})
	if !direct {
		help = issue3969Text(m, "unavailable")
	}
	notes = append(notes,
		html.Button(html.Props{Class: "composer-private-ask", Type: "button", Disabled: !direct || m.Callbacks.SelectConversation == nil,
			Data: map[string]string{"action": issue3969AskAction, "id": agent.ID},
			Aria: map[string]string{"describedby": issue3969HelpID, "label": issue3969Text(m, "ask") + ": " + agent.Display}, Text: issue3969Text(m, "ask")}),
		html.Span(html.Props{ID: issue3969HelpID, Class: "composer-private-help", Dir: "auto", Text: help}))
	return notes
}

// issue3969Banner sits above the private conversation's composer while it holds
// a question moved from a channel: it says nothing has been sent, that nothing
// else came along, and offers the way back.
func issue3969Banner(m Model, local localUI) ui.Node {
	origin, ok := local.privateAsk[m.SelectedID]
	if !ok || strings.TrimSpace(m.Draft) == "" {
		return nil
	}
	children := []ui.Node{html.Span(html.Props{Class: "composer-private-line", Role: "status", Dir: "auto",
		Text: issue3969Format(m, "moved", map[string]string{"channel": origin.FromName})})}
	if m.Callbacks.SelectConversation != nil {
		children = append(children, html.Button(html.Props{Class: "composer-private-switch", Type: "button",
			Data: map[string]string{"action": issue3969CancelAction}, Text: issue3969Text(m, "cancel")}))
	}
	return html.Div(html.Props{Class: "composer-private-answer composer-private-moved", Data: map[string]string{"private-ask": "moved"}}, children...)
}

func issue3969Text(m Model, key string) string {
	copy := map[string][3]string{
		"question_visible": {"Your question will still be visible to everyone in {channel}.", "Ihre Frage bleibt für alle in {channel} sichtbar.", "سيظل سؤالك مرئيًا للجميع في {channel}."},
		"ask":              {"Ask this agent privately", "Diesen Agenten privat fragen", "اسأل هذا الوكيل بشكل خاص"},
		"ask_help": {"Moves your unsent text to your private conversation with {agent} so you can review it there. Nothing is sent, and no messages or files from this channel come with it.",
			"Verschiebt Ihren nicht gesendeten Text in Ihre private Unterhaltung mit {agent}, damit Sie ihn dort prüfen können. Es wird nichts gesendet, und keine Nachrichten oder Dateien aus diesem Kanal werden mitgenommen.",
			"ينقل نصك غير المرسل إلى محادثتك الخاصة مع {agent} لتراجعه هناك. لن يُرسل شيء، ولن تُنقل أي رسائل أو ملفات من هذه القناة."},
		"unavailable": {"A private conversation with this agent is not available, so nothing was moved or sent.", "Eine private Unterhaltung mit diesem Agenten ist nicht verfügbar. Es wurde nichts verschoben oder gesendet.", "المحادثة الخاصة مع هذا الوكيل غير متاحة، لذا لم يُنقل أو يُرسل شيء."},
		"moved": {"Not sent yet. This question was moved from {channel}; review it, then send. No messages or files from the channel came with it.",
			"Noch nicht gesendet. Diese Frage wurde aus {channel} hierher verschoben. Prüfen Sie sie und senden Sie sie dann. Es wurden keine Nachrichten oder Dateien aus dem Kanal mitgenommen.",
			"لم يُرسل بعد. نُقل هذا السؤال من {channel}؛ راجعه ثم أرسله. لم تُنقل أي رسائل أو ملفات من القناة."},
		"cancel": {"Back to channel", "Zurück zum Kanal", "العودة إلى القناة"},
	}
	return chatbug039Text("issue3969."+key, copy[key][chatbug039LocaleIndex(m.Locale)], copy[key][0])
}

func issue3969Format(m Model, key string, values map[string]string) string {
	text := issue3969Text(m, key)
	for name, value := range values {
		text = strings.ReplaceAll(text, "{"+name+"}", value)
	}
	return text
}

// chatIssue3969Styles keeps the explanation readable beside the switch.
const chatIssue3969Styles = `.composer-private-note,.composer-private-help{flex-basis:100%;min-width:0;overflow-wrap:anywhere}` +
	`.composer-private-ask{display:inline-flex;align-items:center;min-height:24px;padding:0 10px;border:1px solid var(--line);border-radius:999px;background:transparent;color:inherit;font:inherit;cursor:pointer}` +
	`.composer-private-ask:disabled{cursor:not-allowed;opacity:.6}` +
	`@media (pointer:coarse){.composer-private-ask,.composer-private-moved .composer-private-switch{min-height:44px}}`
