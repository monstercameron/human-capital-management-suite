package chatui

import (
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATUX-024: a conversation with an agent that holds no message said "Start
// the conversation" and its box said "Ask Assistant a follow-up": nothing told
// a person opening the agent for the first time what it is for or what to ask.
// The empty conversation now names the agent, says what it is for, and offers
// three example questions. Pressing one puts it in the message box for the
// person to change and send themselves; it is never sent for them.
//
// An agent's definition may hold example questions of its own; those are shown
// when it has them. For an agent without, the examples are chosen by what it
// can read: questions about policy for an agent that reads policy documents,
// questions about the conversation's documents for any other.
//
// Every example is a whole sentence a person could send as it stands. The
// built-in sets once ended in a stem to be finished ("Summarise the policy
// on"), which on the page read as a sentence with its topic missing; an example
// that does not end where a sentence ends is left out.

// chatux024Persona is the open agent conversation's own agent in the resolved
// directory, when the directory has arrived.
func chatux024Persona(m Model, c Conversation) (ResolvedPersonaMention, bool) {
	if strings.TrimSpace(c.AgentID) == "" {
		return ResolvedPersonaMention{}, false
	}
	for _, persona := range m.ResolvedPersonaMentions {
		if persona.Reference.ID == c.AgentID {
			return persona, true
		}
	}
	return ResolvedPersonaMention{}, false
}

// chatux024ExampleCount is how many example questions the empty conversation
// offers.
const chatux024ExampleCount = 3

// chatux024Whole reports whether an example is a whole sentence: it ends in a
// question mark, a full stop or an exclamation mark, in any of the scripts the
// product is written in.
func chatux024Whole(example string) bool {
	last, _ := utf8.DecodeLastRuneInString(strings.TrimSpace(example))
	return last != utf8.RuneError && strings.ContainsRune("?.!؟。？！", last)
}

// chatux024Examples is the example questions for the agent: the ones its
// definition holds, or, when it holds none that can be shown, the built-in set
// for what it can read. At most three, each a whole sentence, none twice.
func chatux024Examples(m Model, c Conversation) []string {
	set := "general"
	var examples []string
	add := func(candidates ...string) {
		for _, candidate := range candidates {
			candidate = strings.Join(strings.Fields(candidate), " ")
			if len(examples) == chatux024ExampleCount || !chatux024Whole(candidate) {
				continue
			}
			repeated := false
			for _, held := range examples {
				repeated = repeated || held == candidate
			}
			if !repeated {
				examples = append(examples, candidate)
			}
		}
	}
	if persona, known := chatux024Persona(m, c); known {
		for _, class := range persona.DataClasses {
			if class == "POLICY_DOCUMENT" {
				set = "policy"
			}
		}
		add(persona.Examples...)
	}
	if len(examples) == 0 {
		add(chatux024Text(m.Locale, set+"_1"), chatux024Text(m.Locale, set+"_2"), chatux024Text(m.Locale, set+"_3"))
	}
	return examples
}

// chatux024EmptyAgent is the empty conversation with an agent.
func chatux024EmptyAgent(m Model, c Conversation) []ui.Node {
	name := displayName(m, c)
	children := []ui.Node{
		agentDMAvatar(name, "avatar agent-dm-avatar chatux024-avatar", conversationAgentIcon(m, c)),
		html.H2(html.Props{Text: strings.ReplaceAll(chatux024Text(m.Locale, "ask"), "{name}", name)}),
	}
	if purpose := agentConversationPurpose(m, c); purpose != "" {
		children = append(children, html.P(html.Props{Class: "chatux024-purpose", Dir: "auto", Text: purpose}))
	}
	examples := make([]ui.Node, 0, 3)
	for _, example := range chatux024Examples(m, c) {
		examples = append(examples, html.Li(html.Props{}, html.Button(html.Props{Class: "chatux024-example", Type: "button", Dir: "auto", Data: map[string]string{"action": "agent-example", "extra": example}, Text: example})))
	}
	label := chatux024Text(m.Locale, "examples")
	children = append(children,
		html.H3(html.Props{ID: "chatux024-examples", Class: "chatux024-examples-heading", Text: label}),
		html.Ul(html.Props{Class: "chatux024-examples", Aria: map[string]string{"labelledby": "chatux024-examples"}}, examples...),
		html.P(html.Props{Class: "chatux024-note", Text: chatux024Text(m.Locale, "note")}))
	return []ui.Node{html.Div(html.Props{Class: "state-panel chatux024-empty", Data: map[string]string{"agent-empty": c.AgentID}}, children...)}
}

// chatux024Placeholder is the message box's placeholder in a conversation with
// an agent: "Ask <name>" until the agent has answered there, and "Ask <name> a
// follow-up" from then on.
func chatux024Placeholder(m Model, name string) string {
	for _, message := range m.Messages {
		if messageIsAgent(m, message) {
			return strings.ReplaceAll(agentReplyFallback(m.Locale, "chat.agent.follow_up", "Ask {name} a follow-up"), "{name}", name)
		}
	}
	return strings.ReplaceAll(chatux024Text(m.Locale, "ask"), "{name}", name)
}

// chatux024UseExample puts an example question in the message box, with the
// caret at its end, and tells the model about the draft. It does not send.
func chatux024UseExample(m Model, example string) {
	if strings.TrimSpace(example) == "" || m.SelectedID == "" {
		return
	}
	if m.Callbacks.DraftChanged != nil {
		m.Callbacks.DraftChanged(m.SelectedID, example)
	}
	replaceComposerText("chat-composer", example, len(utf16.Encode([]rune(example))))
}

func chatux024Text(locale, key string) string {
	copy := map[string][3]string{
		"ask":       {"Ask {name}", "{name} fragen", "اسأل {name}"},
		"examples":  {"Try asking", "Fragen Sie zum Beispiel", "جرّب أن تسأل"},
		"note":      {"Pressing a question puts it in the message box. Nothing is sent until you send it.", "Ein Klick setzt die Frage in das Nachrichtenfeld. Gesendet wird erst, wenn Sie senden.", "الضغط على سؤال يضعه في مربع الرسالة. لا يُرسل شيء حتى ترسله أنت."},
		"policy_1":  {"What does our time off policy say about carrying hours over?", "Was sagt unsere Urlaubsrichtlinie zum Übertragen von Stunden?", "ماذا تقول سياسة الإجازات عن ترحيل الساعات؟"},
		"policy_2":  {"Which policies can you read here?", "Welche Richtlinien können Sie hier lesen?", "ما السياسات التي يمكنك قراءتها هنا؟"},
		"policy_3":  {"What should I know before I take time off?", "Was sollte ich wissen, bevor ich Urlaub nehme?", "ما الذي ينبغي أن أعرفه قبل أخذ إجازة؟"},
		"general_1": {"Which documents can you read in this conversation?", "Welche Dokumente können Sie in dieser Unterhaltung lesen?", "ما المستندات التي يمكنك قراءتها في هذه المحادثة؟"},
		"general_2": {"Which company holidays are still to come this year?", "Welche Betriebsfeiertage stehen dieses Jahr noch an?", "ما العطل الرسمية المتبقية هذا العام؟"},
		"general_3": {"What can you help me with here?", "Wobei können Sie mir hier helfen?", "بماذا يمكنك مساعدتي هنا؟"},
	}
	return chatbug039Text(key, copy[key][chatbug039LocaleIndex(locale)], copy[key][0])
}

// chatUX024Styles lays the examples out as a short column of quiet buttons.
const chatUX024Styles = `.chatux024-empty{gap:8px}` +
	`.chatux024-avatar{width:48px;height:48px}` +
	`.chatux024-purpose{max-width:52ch;margin:0;color:var(--muted)}` +
	`.chatux024-examples-heading{margin:10px 0 0;font-size:.8125rem;font-weight:600;color:var(--muted)}` +
	`.chatux024-examples{display:grid;gap:6px;margin:0;padding:0;list-style:none;width:min(100%,460px)}` +
	`.chatux024-example{display:block;width:100%;min-height:36px;padding:6px 12px;border:1px solid var(--line);border-radius:var(--hcm-radius-control);background:var(--surface);color:var(--ink);font:inherit;font-size:.875rem;text-align:start;cursor:pointer;overflow-wrap:anywhere}` +
	`.chatux024-example:hover,.chatux024-example:focus-visible{border-color:var(--accent);color:var(--accent)}` +
	`.chatux024-note{max-width:52ch;margin:4px 0 0;color:var(--muted);font-size:.75rem}` +
	`@media(pointer:coarse){.chatux024-example{min-height:44px}}`
