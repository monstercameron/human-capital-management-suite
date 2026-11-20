package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// Issue 3977: continue a question with another agent. An agent's private answer
// card offers "Continue with another agent" (in its "..." menu). It opens a short review
// inside the card: which agent (only agents the person has their own
// conversation with right now), what to carry (their question, and the answer
// text only when they tick it) and a preview of the draft and of who will see
// it. "Open draft" then puts the text into the unsent draft of the person's own
// conversation with that agent, after any draft already there, and opens it.
// Nothing is sent, and no source content, attachments or history come along.
// The agent is checked again at that moment against the conversation list the
// page holds now; the service checks the agent and the conversation again when
// the message is sent.

const (
	issue3977Open     = "agent-handoff"
	issue3977Pick     = "agent-handoff-pick"
	issue3977Question = "agent-handoff-question"
	issue3977Answer   = "agent-handoff-answer"
	issue3977Go       = "agent-handoff-go"
	issue3977Cancel   = "agent-handoff-cancel"
)

// issue3977State is the review a card is showing, kept with the page's local state.
type issue3977State struct {
	Card     string // answer card (ephemeral message id) that is open, "" when none
	Target   string // conversation id of the chosen agent
	Question bool
	Answer   bool
	Notice   string // copy key suffix of the reason the last attempt did nothing
}

// issue3977Source is what a card can carry, read from the answer as the card
// shows it.
type issue3977Source struct {
	Question, Answer string
	AgentID, Agent   string
}

func issue3977Read(m Model, messageID string) (issue3977Source, bool) {
	for _, message := range m.EphemeralMessages {
		if message.ID != messageID {
			continue
		}
		marked, _ := chat.SplitUngrounded(message.Body)
		body, _ := chatux003Reason(marked)
		envelope := parseAgentReplyEnvelope(body)
		source := issue3977Source{Question: strings.TrimSpace(envelope.QuestionText)}
		source.Answer = strings.TrimSpace(agentUX075CleanAnswer(envelope.Body, envelope.Sources))
		name := ""
		for _, invocation := range m.PersonaInvocations {
			if invocation.Projection.DurablePostID == messageID {
				name = strings.TrimSpace(invocation.Projection.AgentName)
			}
		}
		actor := m.PersonaPostActors[messageID]
		if strings.TrimSpace(actor.Display) != "" {
			name = strings.TrimSpace(actor.Display)
		}
		source.Agent = name
		source.AgentID = chatux003AgentID(m, actor, name)
		if source.AgentID == "" && actor.Actor.valid() {
			source.AgentID = actor.Actor.AgentID
		}
		return source, source.Question != "" || source.Answer != ""
	}
	return issue3977Source{}, false
}

// issue3977Agents are the agents the person can continue with: the agents they
// have their own, joined conversation with, other than the one that answered
// and the conversation they are in.
func issue3977Agents(m Model, source issue3977Source) []Conversation {
	var out []Conversation
	seen := map[string]bool{}
	for _, room := range m.Conversations {
		if room.Kind != DirectMessage || !room.Agent || !room.Joined || room.ID == "" || room.ID == m.SelectedID {
			continue
		}
		if source.AgentID != "" && room.AgentID == source.AgentID {
			continue
		}
		if source.AgentID == "" && source.Agent != "" && strings.EqualFold(strings.TrimSpace(room.Name), source.Agent) {
			continue
		}
		key := room.AgentID
		if key == "" {
			key = room.ID
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, room)
	}
	return out
}

func issue3977Has(rooms []Conversation, id string) (Conversation, bool) {
	for _, room := range rooms {
		if room.ID == id && id != "" {
			return room, true
		}
	}
	return Conversation{}, false
}

// issue3977Draft is the draft the review previews and opens: the question, then,
// only when chosen, the answer text under a plain label.
func issue3977Draft(m Model, state issue3977State, source issue3977Source) string {
	var parts []string
	if state.Question && source.Question != "" {
		parts = append(parts, source.Question)
	}
	if state.Answer && source.Answer != "" {
		parts = append(parts, issue3977Text(m, "answer_label")+"\n"+source.Answer)
	}
	return strings.Join(parts, "\n\n")
}

// issue3977Offered reports whether the card offers the action at all.
func issue3977Offered(m Model, messageID string) bool {
	if m.Callbacks.SelectConversation == nil {
		return false
	}
	_, ok := issue3977Read(m, messageID)
	return ok
}

// issue3977Step decides what one press does to the review. effect is "start"
// when the review opened, "open" when the draft should be handed off and
// "close" when the review ended.
func issue3977Step(m Model, state issue3977State, action, id string) (next issue3977State, effect string, taken bool) {
	switch action {
	case issue3977Open:
		source, ok := issue3977Read(m, id)
		if !ok || m.Callbacks.SelectConversation == nil {
			return state, "", true
		}
		next = issue3977State{Card: id, Question: source.Question != "", Answer: source.Question == ""}
		if rooms := issue3977Agents(m, source); len(rooms) == 1 {
			next.Target = rooms[0].ID
		}
		return next, "start", true
	case issue3977Pick:
		source, _ := issue3977Read(m, state.Card)
		if _, ok := issue3977Has(issue3977Agents(m, source), id); ok && state.Card != "" {
			state.Target, state.Notice = id, ""
		}
		return state, "", true
	case issue3977Question:
		source, _ := issue3977Read(m, state.Card)
		if state.Card != "" && source.Question != "" {
			state.Question, state.Notice = !state.Question, ""
		}
		return state, "", true
	case issue3977Answer:
		source, _ := issue3977Read(m, state.Card)
		if state.Card != "" && source.Answer != "" {
			state.Answer, state.Notice = !state.Answer, ""
		}
		return state, "", true
	case issue3977Cancel:
		return issue3977State{}, "close", true
	case issue3977Go:
		if state.Card == "" {
			return state, "", true
		}
		source, ok := issue3977Read(m, state.Card)
		if !ok {
			state.Notice = "gone"
			return state, "", true
		}
		_, there := issue3977Has(issue3977Agents(m, source), state.Target)
		switch {
		case state.Target == "":
			state.Notice = "choose"
		case !there || m.Callbacks.SelectConversation == nil:
			// The agent or the person's conversation with it went away since the
			// review opened: nothing moves, and the question stays on the card.
			state.Target, state.Notice = "", "unavailable"
		case strings.TrimSpace(issue3977Draft(m, state, source)) == "":
			state.Notice = "nothing"
		default:
			return issue3977State{}, "open", true
		}
		return state, "", true
	}
	return state, "", false
}

// issue3977Merge puts the carried text after a draft that is already there.
func issue3977Merge(existing, carried string) string {
	if strings.TrimSpace(existing) == "" {
		return carried
	}
	return strings.TrimRight(existing, "\n ") + "\n\n" + carried
}

// issue3977Click handles the review's presses. It reports whether the press was one.
func issue3977Click(e ui.Event, m Model, local localStore, drafts *browserDrafts) bool {
	action, id, _ := eventAction(e)
	if !strings.HasPrefix(action, issue3977Open) {
		return false
	}
	before := local.get().handoff
	next, effect, taken := issue3977Step(m, before, action, id)
	if !taken {
		return false
	}
	if action != issue3977Pick && action != issue3977Question && action != issue3977Answer {
		e.PreventDefault()
	}
	if effect == "open" {
		issue3977Handoff(m, local, drafts, before)
		return true
	}
	local.update(func(u *localUI) { u.handoff = next })
	switch effect {
	case "start":
		chatux028CloseMenu(m)
		chatux026Focus(`.agent-handoff input[type="radio"], .agent-handoff [data-action="` + issue3977Cancel + `"]`)
	case "close":
		chatux026Focus(`[data-action="menu"][data-id="agent-card:` + chatux026Quoted(before.Card) + `"], [data-ephemeral-id="` + chatux026Quoted(before.Card) + `"] .agent-follow-up`)
	}
	return true
}

// issue3977Handoff carries the chosen text into the unsent draft of the person's
// own conversation with the agent and opens it. It sends nothing.
func issue3977Handoff(m Model, local localStore, drafts *browserDrafts, state issue3977State) {
	source, _ := issue3977Read(m, state.Card)
	carried := issue3977Draft(m, state, source)
	from := m.SelectedID
	current := domDraftValue("chat-composer", from)
	if current == "" {
		current = m.Draft
	}
	existing := drafts.selectConversation(from, current, state.Target)
	if existing == "" {
		existing = m.Preferences.Drafts[state.Target]
	}
	text := issue3977Merge(existing, carried)
	drafts.set(state.Target, text)
	if m.Callbacks.DraftChanged != nil {
		m.Callbacks.DraftChanged(from, current)
		m.Callbacks.DraftChanged(state.Target, text)
	}
	local.update(func(u *localUI) { u.handoff = issue3977State{} })
	m.Callbacks.SelectConversation(state.Target)
	FocusComposerFor(state.Target)
}

// issue3977MenuItem is the card's "Continue with another agent" menu item.
func issue3977MenuItem(m Model, messageID string) []ui.Node {
	if !issue3977Offered(m, messageID) {
		return nil
	}
	return []ui.Node{html.Button(html.Props{Class: "menu-item agent-handoff-menu", Type: "button", Role: "menuitem", Data: map[string]string{"action": issue3977Open, "id": messageID}},
		icon("send"), html.Span(html.Props{Text: issue3977Text(m, "open")}))}
}

func issue3977CancelButton(m Model) ui.Node {
	return html.Button(html.Props{Class: "button secondary small agent-handoff-cancel", Type: "button", Data: map[string]string{"action": issue3977Cancel}, Text: agentReplyFallback(m.Locale, KeyCancel, "Cancel")})
}

// issue3977Panel is the review, drawn under the card's action row while open.
func issue3977Panel(m Model, local localUI, messageID string) ui.Node {
	state := local.handoff
	if state.Card == "" || state.Card != messageID {
		return nil
	}
	title := issue3977Text(m, "open")
	source, ok := issue3977Read(m, messageID)
	if !ok {
		return html.Div(html.Props{Class: "agent-handoff", Role: "group", Aria: map[string]string{"label": title}, Data: map[string]string{"agent-handoff": messageID}},
			html.P(html.Props{Role: "status", Text: issue3977Text(m, "gone")}), issue3977CancelButton(m))
	}
	rooms := issue3977Agents(m, source)
	children := []ui.Node{html.H4(html.Props{Class: "agent-handoff-title", Text: title}),
		html.P(html.Props{Class: "agent-handoff-note", Text: issue3977Text(m, "note")})}
	if len(rooms) == 0 {
		children = append(children, html.P(html.Props{Class: "agent-handoff-empty", Role: "status", Text: issue3977Text(m, "none")}))
		if chat3759MayPlaceAgents(m, m.selected()) {
			children = append(children, chat3759AddAgentLink(m))
		}
		children = append(children, issue3977CancelButton(m))
		return html.Div(html.Props{Class: "agent-handoff", Role: "group", Aria: map[string]string{"label": title}, Data: map[string]string{"agent-handoff": messageID, "state": "empty"}}, children...)
	}
	agents := []ui.Node{html.Legend(html.Props{Text: issue3977Text(m, "agent_legend")})}
	for _, room := range rooms {
		id := "agent-handoff-agent-" + room.ID
		details := []ui.Node{html.Strong(html.Props{Dir: "auto", Text: displayName(m, room)})}
		if purpose := strings.TrimSpace(room.AgentPurpose); purpose != "" {
			details = append(details, html.Span(html.Props{Class: "agent-handoff-purpose", Dir: "auto", Text: purpose}))
		}
		details = append(details, html.Span(html.Props{Class: "agent-handoff-where", Dir: "auto", Text: issue3977Format(m, "where", map[string]string{"agent": displayName(m, room)})}))
		agents = append(agents, html.Label(html.Props{Class: "agent-handoff-choice", For: id},
			html.Input(html.Props{ID: id, Type: "radio", Name: "agent-handoff-agent", Value: room.ID, Checked: state.Target == room.ID, Data: map[string]string{"action": issue3977Pick, "id": room.ID}}),
			html.Span(html.Props{Class: "agent-handoff-choice-text"}, details...)))
	}
	children = append(children, html.Tag("fieldset", html.Props{Class: "agent-handoff-agents"}, agents...))
	carry := []ui.Node{html.Legend(html.Props{Text: issue3977Text(m, "carry_legend")})}
	if source.Question != "" {
		carry = append(carry, html.Label(html.Props{Class: "agent-handoff-choice", For: "agent-handoff-q"},
			html.Input(html.Props{ID: "agent-handoff-q", Type: "checkbox", Checked: state.Question, Data: map[string]string{"action": issue3977Question}}),
			html.Span(html.Props{Class: "agent-handoff-choice-text"}, html.Strong(html.Props{Text: issue3977Text(m, "carry_question")}), html.Span(html.Props{Class: "agent-handoff-purpose", Dir: "auto", Text: source.Question}))))
	}
	if source.Answer != "" {
		carry = append(carry, html.Label(html.Props{Class: "agent-handoff-choice", For: "agent-handoff-a"},
			html.Input(html.Props{ID: "agent-handoff-a", Type: "checkbox", Checked: state.Answer, Data: map[string]string{"action": issue3977Answer}}),
			html.Span(html.Props{Class: "agent-handoff-choice-text"}, html.Strong(html.Props{Text: issue3977Text(m, "carry_answer")}), html.Span(html.Props{Class: "agent-handoff-purpose", Text: issue3977Text(m, "carry_answer_hint")}))))
	}
	children = append(children, html.Tag("fieldset", html.Props{Class: "agent-handoff-carry"}, carry...))
	// The preview: who will see it and the draft as it will be opened.
	draft := issue3977Draft(m, state, source)
	preview := []ui.Node{html.H5(html.Props{Text: issue3977Text(m, "preview")})}
	if room, found := issue3977Has(rooms, state.Target); found {
		preview = append(preview, html.P(html.Props{Class: "agent-handoff-audience", Dir: "auto", Text: issue3977Format(m, "audience", map[string]string{"agent": displayName(m, room)})}))
		if strings.TrimSpace(m.Preferences.Drafts[room.ID]) != "" {
			preview = append(preview, html.P(html.Props{Class: "agent-handoff-existing", Dir: "auto", Text: issue3977Text(m, "existing")}))
		}
	} else {
		preview = append(preview, html.P(html.Props{Class: "agent-handoff-audience", Text: issue3977Text(m, "audience_none")}))
	}
	if strings.TrimSpace(draft) == "" {
		preview = append(preview, html.P(html.Props{Class: "agent-handoff-draft empty", Text: issue3977Text(m, "draft_empty")}))
	} else {
		preview = append(preview, html.Div(html.Props{Class: "agent-handoff-draft", Dir: "auto", Role: "group", Aria: map[string]string{"label": issue3977Text(m, "draft_label")}, Text: draft}))
	}
	children = append(children, html.Div(html.Props{Class: "agent-handoff-preview"}, preview...))
	if state.Notice != "" {
		children = append(children, html.P(html.Props{Class: "agent-handoff-notice", Role: "alert", Dir: "auto", Text: issue3977Text(m, "notice_"+state.Notice)}))
	}
	children = append(children, html.Div(html.Props{Class: "agent-handoff-actions"},
		html.Button(html.Props{Class: "button small agent-handoff-go", Type: "button", Disabled: strings.TrimSpace(draft) == "" || state.Target == "", Data: map[string]string{"action": issue3977Go}, Text: issue3977Text(m, "go")}),
		issue3977CancelButton(m)))
	return html.Div(html.Props{Class: "agent-handoff", Role: "group", Aria: map[string]string{"label": title}, Data: map[string]string{"agent-handoff": messageID}}, children...)
}

func issue3977Text(m Model, key string) string {
	table := map[string][3]string{
		"open":               {"Continue with another agent", "Mit einem anderen Agenten fortfahren", "المتابعة مع وكيل آخر"},
		"note":               {"Pick an agent and what to bring along. It opens as a draft you can edit; nothing is sent until you send it.", "Wählen Sie einen Agenten und was mitgenommen wird. Es öffnet sich als Entwurf, den Sie bearbeiten können. Gesendet wird erst, wenn Sie senden.", "اختر وكيلًا وما تريد نقله. يُفتح كمسودة يمكنك تعديلها، ولن يُرسل شيء حتى ترسله أنت."},
		"none":               {"No other agent is available to you right now. Your question stays here.", "Derzeit steht Ihnen kein anderer Agent zur Verfügung. Ihre Frage bleibt hier erhalten.", "لا يتوفر لك وكيل آخر الآن. يبقى سؤالك هنا."},
		"agent_legend":       {"Continue with", "Fortfahren mit", "المتابعة مع"},
		"where":              {"Its answer would appear in your private conversation with {agent}. Only you can see it.", "Die Antwort erscheint in Ihrer privaten Unterhaltung mit {agent}. Nur Sie sehen sie.", "ستظهر الإجابة في محادثتك الخاصة مع {agent}. أنت وحدك من يراها."},
		"carry_legend":       {"What to bring", "Was mitgenommen wird", "ما سيُنقل"},
		"carry_question":     {"Your question", "Ihre Frage", "سؤالك"},
		"carry_answer":       {"The text of this answer", "Der Text dieser Antwort", "نص هذه الإجابة"},
		"carry_answer_hint":  {"Sources, documents and files are never copied.", "Quellen, Dokumente und Dateien werden nie kopiert.", "لا تُنسخ المصادر أو المستندات أو الملفات أبدًا."},
		"answer_label":       {"Earlier answer:", "Frühere Antwort:", "إجابة سابقة:"},
		"preview":            {"Preview", "Vorschau", "معاينة"},
		"audience":           {"Opens in your private conversation with {agent}. Only you and {agent} can see it, and it is not sent until you send it.", "Öffnet sich in Ihrer privaten Unterhaltung mit {agent}. Nur Sie und {agent} sehen es, und es wird erst gesendet, wenn Sie senden.", "يُفتح في محادثتك الخاصة مع {agent}. لا يراه سواك و{agent}، ولا يُرسل حتى ترسله أنت."},
		"audience_none":      {"Choose an agent to see where the draft will open.", "Wählen Sie einen Agenten, um zu sehen, wo der Entwurf geöffnet wird.", "اختر وكيلًا لترى أين ستُفتح المسودة."},
		"existing":           {"You already have an unsent draft there. This text is added after it.", "Dort liegt bereits ein nicht gesendeter Entwurf. Dieser Text wird danach angefügt.", "لديك مسودة غير مرسلة هناك. تُضاف هذه النصوص بعدها."},
		"draft_label":        {"Draft that will open", "Entwurf, der geöffnet wird", "المسودة التي ستُفتح"},
		"draft_empty":        {"Nothing selected to bring along.", "Nichts zum Mitnehmen ausgewählt.", "لم يُحدَّد شيء لنقله."},
		"go":                 {"Open draft", "Entwurf öffnen", "فتح المسودة"},
		"gone":               {"This answer is no longer available to continue from.", "Diese Antwort steht nicht mehr zum Fortfahren bereit.", "لم تعد هذه الإجابة متاحة للمتابعة منها."},
		"notice_gone":        {"This answer is no longer available to continue from.", "Diese Antwort steht nicht mehr zum Fortfahren bereit.", "لم تعد هذه الإجابة متاحة للمتابعة منها."},
		"notice_choose":      {"Choose an agent first.", "Wählen Sie zuerst einen Agenten.", "اختر وكيلًا أولًا."},
		"notice_nothing":     {"Choose something to bring along first.", "Wählen Sie zuerst aus, was mitgenommen wird.", "اختر شيئًا لنقله أولًا."},
		"notice_unavailable": {"That agent can no longer be reached from here, so nothing was moved. Your question is still on this card; choose another agent.", "Dieser Agent ist von hier aus nicht mehr erreichbar, daher wurde nichts verschoben. Ihre Frage steht weiter auf dieser Karte; wählen Sie einen anderen Agenten.", "لم يعد بالإمكان الوصول إلى هذا الوكيل من هنا، لذا لم يُنقل شيء. سؤالك لا يزال على هذه البطاقة؛ اختر وكيلًا آخر."},
	}
	return chatbug039Text("issue3977."+key, table[key][chatbug039LocaleIndex(m.Locale)], table[key][0])
}

func issue3977Format(m Model, key string, values map[string]string) string {
	text := issue3977Text(m, key)
	for name, value := range values {
		text = strings.ReplaceAll(text, "{"+name+"}", value)
	}
	return text
}

const issue3977Styles = `.agent-handoff{flex:1 0 100%;display:grid;gap:8px;margin-block-start:8px;padding:12px;border:1px solid var(--line);border-radius:var(--hcm-radius-control);background:var(--surface);min-width:0}` +
	`.agent-handoff fieldset{margin:0;padding:0;border:0;display:grid;gap:6px;min-width:0}.agent-handoff legend{padding:0;font-weight:600;font-size:.8125rem}` +
	`.agent-handoff-title,.agent-handoff h5{margin:0;font-size:.9375rem}.agent-handoff p{margin:0;font-size:.8125rem;line-height:1.4;overflow-wrap:anywhere}` +
	`.agent-handoff-choice{display:flex;align-items:flex-start;gap:8px;min-height:44px;padding:4px 0;cursor:pointer}.agent-handoff-choice input{flex:none;margin-block-start:4px}` +
	`.agent-handoff-choice-text{display:grid;gap:2px;min-width:0}.agent-handoff-purpose,.agent-handoff-where{color:var(--muted);font-size:.8125rem;overflow-wrap:anywhere}` +
	`.agent-handoff-draft{padding:8px;border:1px solid var(--line);border-radius:var(--hcm-radius-control);background:var(--soft);white-space:pre-wrap;overflow-wrap:anywhere;max-height:12rem;overflow:auto;font-size:.875rem}` +
	`.agent-handoff-notice{color:var(--danger,var(--ink))}.agent-handoff-actions{display:flex;flex-wrap:wrap;gap:8px}` +
	`@media (pointer:coarse),(max-width:480px){.agent-handoff-actions .button{min-height:44px}.agent-handoff-actions{flex-direction:column}.agent-handoff-actions .button{width:100%}}`
