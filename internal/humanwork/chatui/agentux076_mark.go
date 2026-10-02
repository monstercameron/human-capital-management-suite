package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// AGENTUX-076: a general-purpose agent may answer from general knowledge when no
// document covers a question. The server marks such an answer and the card says
// so in one quiet line under the answer, so a reader never takes it for a
// company document's word.

// agentux076NotFromDocuments is the line under an answer that no document backs.
func agentux076NotFromDocuments(model Model) ui.Node {
	return html.P(html.Props{Class: "agent-reply-why agent-reply-ungrounded", Dir: "auto", Data: map[string]string{"agent-answer-source": "none"}, Text: agentux076Text(model.Locale)})
}

func agentux076Text(locale string) string {
	copy := [3]string{"Not from your documents", "Nicht aus Ihren Dokumenten", "ليست من مستنداتك"}
	return chatbug039Text("agentux076.not_from_documents", copy[chatbug039LocaleIndex(locale)], copy[0])
}
