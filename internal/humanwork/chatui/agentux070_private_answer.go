package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// AGENTUX-070: an agent's answer in a channel is visible to the channel unless
// something makes it private. One of those is the asker's own word: a "Private
// answer" switch in the composer, shown only while the draft asks an agent in a
// channel whose answers are public by default. The switch writes the same words
// the server reads in a typed question ("keep this private"), so the choice
// travels in the question the server already judges, and the server alone
// decides what is delivered; the page only says, before sending, who will see
// the answer.

// agentux070PrivatePhrase is the sentence the switch adds to the question. The
// server recognises it by its phrase list and takes it out before the question
// reaches the model.
const agentux070PrivatePhrase = "Keep this private."

// agentux070Applies reports whether the switch is offered: the draft asks an
// agent, in a public channel, and at least one agent asked answers there by
// default. An agent set to answer privately, or a channel that requires it, has
// nothing for the asker to choose.
func agentux070Applies(m Model, refs []ChatReference) bool {
	if m.selected().Kind != PublicChannel {
		return false
	}
	for _, reference := range refs {
		if reference.Kind != "AGENT_MENTION" {
			continue
		}
		for _, candidate := range m.ResolvedPersonaMentions {
			if candidate.Reference.ID == reference.ID && candidate.ReplyPlacement != PersonaReplyPrivateAlways {
				return true
			}
		}
	}
	return false
}

// agentux070Body is the question as it is sent: with the asker's word added when
// the switch is on and the question does not already say it.
func agentux070Body(body string, private bool) string {
	if !private || strings.Contains(strings.ToLower(body), "keep this private") {
		return body
	}
	return strings.TrimRight(body, " \t\r\n") + " " + agentux070PrivatePhrase
}

// agentux070Row is the switch and the one line saying who will see the answer.
func agentux070Row(m Model, local localUI, refs []ChatReference) ui.Node {
	if !agentux070Applies(m, refs) {
		return nil
	}
	line := agentux070Format(m, "line.public", map[string]string{"channel": chatux003ChannelName(m)})
	if local.privateAnswer {
		line = agentux070Text(m, "line.private")
	}
	return html.Div(html.Props{Class: "composer-private-answer", Data: map[string]string{"private-answer": boolString(local.privateAnswer)}},
		html.Button(html.Props{Class: "composer-private-switch", Type: "button", Role: "switch", Data: map[string]string{"action": "agent-private-answer"},
			Aria: map[string]string{"checked": boolString(local.privateAnswer)}, Text: agentux070Text(m, "toggle")}),
		html.Span(html.Props{Class: "composer-private-line", Role: "status", Dir: "auto", Text: line}))
}

// agentux070Click turns the switch over. It reports whether the press was on it.
func agentux070Click(e ui.MouseEvent, local localStore) bool {
	if action, _, _ := eventAction(e); action != "agent-private-answer" {
		return false
	}
	e.PreventDefault()
	local.update(func(u *localUI) { u.privateAnswer = !u.privateAnswer })
	return true
}

func agentux070Text(m Model, key string) string {
	copy := map[string][3]string{
		"toggle":       {"Private answer", "Private Antwort", "إجابة خاصة"},
		"line.public":  {"Everyone in {channel} will see the answer.", "Alle in {channel} sehen die Antwort.", "سيرى الجميع في {channel} الإجابة."},
		"line.private": {"Only you will see the answer.", "Nur Sie sehen die Antwort.", "أنت وحدك سترى الإجابة."},
	}
	return chatbug039Text("agentux070."+key, copy[key][chatbug039LocaleIndex(m.Locale)], copy[key][0])
}

func agentux070Format(m Model, key string, values map[string]string) string {
	text := agentux070Text(m, key)
	for name, value := range values {
		text = strings.ReplaceAll(text, "{"+name+"}", value)
	}
	return text
}

// chatUX070Styles lays the switch and its line in one quiet row under the draft.
const chatUX070Styles = `.composer-private-answer{display:flex;flex-wrap:wrap;align-items:center;gap:4px 12px;margin:0;padding:2px 8px 4px;font-size:.75rem;color:var(--muted)}` +
	`.composer-private-switch{display:inline-flex;align-items:center;gap:6px;min-height:24px;padding:0 10px;border:1px solid var(--line);border-radius:999px;background:transparent;color:inherit;font:inherit;cursor:pointer}` +
	`.composer-private-switch[aria-checked="true"]{border-color:var(--accent);background:var(--soft);color:var(--text)}` +
	`.composer-private-line{min-width:0;overflow-wrap:anywhere}`
