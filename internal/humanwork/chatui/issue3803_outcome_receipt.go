package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// PersonaOutcomeReceipt is how one agent run ended, as the server sent it to
// every member of the conversation (#3803). It holds no answer, reason or task
// name; Outcome is one of succeeded, refused, failed, cancelled and expired.
type PersonaOutcomeReceipt struct {
	ID        string
	PostID    string
	ThreadID  string
	InvokerID string
	PersonaID string
	AgentName string
	Outcome   string
}

const (
	keyOutcomeRegion    = "issue3803.region"
	keyOutcomeSucceeded = "issue3803.succeeded"
	keyOutcomeRefused   = "issue3803.refused"
	keyOutcomeFailed    = "issue3803.failed"
	keyOutcomeCancelled = "issue3803.cancelled"
	keyOutcomeExpired   = "issue3803.expired"
	keyOutcomeNote      = "issue3803.note"
	keyOutcomeSomeone   = "issue3803.someone"
)

var issue3803OutcomeCopy = map[string]map[string]string{
	"en-US": {
		keyOutcomeRegion:    "Agent outcome",
		keyOutcomeSucceeded: "{agent} finished the request from {person}.",
		keyOutcomeRefused:   "{agent} could not take the request from {person}.",
		keyOutcomeFailed:    "{agent} could not finish the request from {person}.",
		keyOutcomeCancelled: "The request from {person} to {agent} was stopped before it finished.",
		keyOutcomeExpired:   "{agent} ran out of time on the request from {person}.",
		keyOutcomeNote:      "This is the agent’s own result. Steps that follow elsewhere may still be open.",
		keyOutcomeSomeone:   "a teammate",
	},
	"de-DE": {
		keyOutcomeRegion:    "Ergebnis des Agenten",
		keyOutcomeSucceeded: "{agent} hat die Anfrage von {person} abgeschlossen.",
		keyOutcomeRefused:   "{agent} konnte die Anfrage von {person} nicht annehmen.",
		keyOutcomeFailed:    "{agent} konnte die Anfrage von {person} nicht abschließen.",
		keyOutcomeCancelled: "Die Anfrage von {person} an {agent} wurde vor dem Abschluss gestoppt.",
		keyOutcomeExpired:   "{agent} hat für die Anfrage von {person} die Zeit überschritten.",
		keyOutcomeNote:      "Das ist das eigene Ergebnis des Agenten. Folgeschritte an anderer Stelle können noch offen sein.",
		keyOutcomeSomeone:   "einem Teammitglied",
	},
	"ar": {
		keyOutcomeRegion:    "نتيجة الوكيل",
		keyOutcomeSucceeded: "أنهى {agent} الطلب المقدَّم من {person}.",
		keyOutcomeRefused:   "تعذّر على {agent} قبول الطلب المقدَّم من {person}.",
		keyOutcomeFailed:    "تعذّر على {agent} إكمال الطلب المقدَّم من {person}.",
		keyOutcomeCancelled: "أُوقف طلب {person} إلى {agent} قبل اكتماله.",
		keyOutcomeExpired:   "انتهت المهلة قبل أن يُكمل {agent} الطلب المقدَّم من {person}.",
		keyOutcomeNote:      "هذه نتيجة الوكيل نفسه. قد تبقى خطوات لاحقة في أماكن أخرى مفتوحة.",
		keyOutcomeSomeone:   "زميل",
	},
}

// issue3803OutcomeStyles keeps the receipt calm: it is text, draws no control,
// and holds in a narrow column and in right-to-left text.
const issue3803OutcomeStyles = `.persona-outcome-receipt{min-width:0}.persona-outcome-receipt .persona-outcome-text{margin:6px 0;overflow-wrap:anywhere}.persona-outcome-receipt .persona-outcome-note{margin:4px 0 0;font-size:.8125rem;color:var(--hcm-color-text-muted);overflow-wrap:anywhere}`

// personaOutcomeKey is the sentence of an outcome word, empty for a word the
// page does not know (it then draws nothing rather than guessing).
func personaOutcomeKey(outcome string) string {
	switch outcome {
	case "succeeded":
		return keyOutcomeSucceeded
	case "refused":
		return keyOutcomeRefused
	case "failed":
		return keyOutcomeFailed
	case "cancelled":
		return keyOutcomeCancelled
	case "expired":
		return keyOutcomeExpired
	}
	return ""
}

// personaOutcomeAsker is the name the asker is shown by: their own name for the
// reader, the room's name for them otherwise, and "a teammate" when the page
// cannot name them (never their identifier).
func personaOutcomeAsker(m Model, id string) string {
	id = strings.TrimSpace(id)
	if id != "" && id == m.CurrentUser {
		if name := strings.TrimSpace(m.CurrentUserName); name != "" {
			return name
		}
	}
	for _, member := range m.Members {
		if member.ID == id && !member.Agent {
			if name := strings.TrimSpace(member.Name); name != "" {
				return name
			}
		}
	}
	return laneText(m, issue3803OutcomeCopy, keyOutcomeSomeone)
}

// personaOutcomeRows draws the receipts of the runs asked under one message.
// The asker's own run already has its card in this place, which updates in
// place when the run ends, so the asker gets no second row; everyone else in
// the conversation reads the outcome here. A conversation with an agent, whose
// replies are the agent's own messages, has none.
func personaOutcomeRows(m Model, postID string) []ui.Node {
	if strings.TrimSpace(postID) == "" || m.selected().Agent || len(m.PersonaOutcomes) == 0 {
		return nil
	}
	rows := []ui.Node{}
	for _, receipt := range m.PersonaOutcomes {
		key := personaOutcomeKey(receipt.Outcome)
		if receipt.PostID != postID || key == "" || strings.TrimSpace(receipt.ID) == "" {
			continue
		}
		if receipt.InvokerID == m.CurrentUser && personaOutcomeHasOwnCard(m, postID) {
			continue
		}
		rows = append(rows, html.WithKey(renderPersonaOutcome(m, receipt, key), "agent-outcome:"+receipt.ID))
	}
	return rows
}

func personaOutcomeHasOwnCard(m Model, postID string) bool {
	for _, invocation := range m.PersonaInvocations {
		if invocation.PostID == postID {
			return true
		}
	}
	return false
}

func renderPersonaOutcome(m Model, receipt PersonaOutcomeReceipt, key string) ui.Node {
	name := agentReplyAuthor(m, receipt.PostID, receipt.AgentName)
	if name == "" {
		name = personaProgressText(m, "chat.agent.name", "Agent")
	}
	sentence := laneTextf(m, issue3803OutcomeCopy, key, map[string]string{"agent": name, "person": personaOutcomeAsker(m, receipt.InvokerID)})
	children := []ui.Node{
		renderAgentReplyIdentity(m, name, storedAgentIcon(m, []string{receipt.PersonaID}, name)),
		html.P(html.Props{Class: "persona-outcome-text", Dir: "auto", Text: sentence}),
	}
	if receipt.Outcome == "succeeded" {
		children = append(children, html.P(html.Props{Class: "persona-outcome-note", Dir: "auto", Text: laneText(m, issue3803OutcomeCopy, keyOutcomeNote)}))
	}
	return html.Div(html.Props{Class: "agent-reply-row persona-outcome-receipt", Role: "status", Aria: map[string]string{"label": laneText(m, issue3803OutcomeCopy, keyOutcomeRegion)}, Data: map[string]string{"agent-outcome": receipt.Outcome, "agent-outcome-id": receipt.ID}}, children...)
}
