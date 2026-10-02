package chatui

import "strings"

// AGENT-027, page half. The server accepts "/ask @Agent question" and rewrites
// it into the message a typed mention sends (internal/application/
// agent_ask_command.go). The "/" list offers the command where an agent can be
// asked, and the line is read here first for the four reasons the server
// refuses it, so the person is told in their own words and the draft stays
// for them to fix; the line is otherwise sent as typed.

// Reasons an ask line is refused; the names are the server's reason codes.
const (
	agent027NeedsAgent    = "ask_needs_agent"
	agent027NeedsQuestion = "ask_needs_question"
	agent027OneAgent      = "ask_one_agent"
	agent027NotOwnWords   = "ask_not_own_words"
)

var agent027Copy = map[string][3]string{
	agent027NeedsAgent: {
		"Say which agent to ask: /ask @Agent your question.",
		"Nennen Sie den Agenten, den Sie fragen möchten: /ask @Agent Ihre Frage.",
		"حدّد الوكيل الذي تريد سؤاله: /ask @الوكيل سؤالك.",
	},
	agent027NeedsQuestion: {
		"Write your question after the agent's name.",
		"Schreiben Sie Ihre Frage hinter den Namen des Agenten.",
		"اكتب سؤالك بعد اسم الوكيل.",
	},
	agent027OneAgent: {
		"Ask one agent at a time. Remove the extra name.",
		"Fragen Sie immer nur einen Agenten. Entfernen Sie den zusätzlichen Namen.",
		"اسأل وكيلًا واحدًا في كل مرة. احذف الاسم الإضافي.",
	},
	agent027NotOwnWords: {
		"Forwarded text cannot be sent as a command. Write the question in your own words.",
		"Weitergeleiteter Text kann nicht als Befehl gesendet werden. Formulieren Sie die Frage selbst.",
		"لا يمكن إرسال نص تمت إعادة توجيهه كأمر. اكتب السؤال بكلماتك.",
	},
}

// agent027Text is the sentence for one refusal reason in the page's language.
func agent027Text(m Model, reason string) string {
	copy := agent027Copy[reason]
	return chatbug039Text(reason, copy[chatbug039LocaleIndex(m.Locale)], copy[0])
}

// agent027AskAvailable says whether an agent can be asked in the open
// conversation: it is a conversation with an agent, or agents are placed in it
// for this person.
func agent027AskAvailable(m Model) bool {
	return m.selected().Agent || len(personaMentionCandidates(m, "")) > 0
}

// agent027AskRefusal reads the text after "/ask" the way the server does and
// returns the reason it would be refused, or "" when it can be sent.
func agent027AskRefusal(m Model, args string) string {
	rest := strings.TrimSpace(args)
	named := 0
	for _, persona := range personaMentionCandidates(m, "") {
		display := strings.TrimSpace(persona.Reference.Display)
		if display == "" {
			continue
		}
		if mention := "@" + display; strings.Contains(rest, mention) {
			named++
			rest = strings.TrimSpace(strings.Replace(rest, mention, "", 1))
		}
	}
	switch {
	case named > 1:
		return agent027OneAgent
	case named == 0 && !m.selected().Agent:
		return agent027NeedsAgent
	case strings.TrimLeft(rest, "@ ") == "":
		return agent027NeedsQuestion
	}
	return ""
}

// agent027AskCheck is the command's Check: it says why a refused line was not
// sent, and lets a good one through to be sent as typed.
func agent027AskCheck(rt composerCommandRuntime, args string) bool {
	if reason := agent027AskRefusal(rt.Model, args); reason != "" {
		rt.Notice(agent027Text(rt.Model, reason))
		return false
	}
	return true
}
