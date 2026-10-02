package chatui

import (
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
)

// CHATBUG-079: a private answer is delivered to the page apart from the record
// that says the run finished, and after it. In between, the card used to read
// "Finding an answer in your policy documents…", which on a reload made an
// answer from the day before look like a run that had just started. A finished
// run is never drawn as working: it keeps the answer's room with the quiet
// placeholder, and if the text does not arrive it points at the saved copy.

// chatbug079StoredAnswerRow is the row under a question whose answer is stored
// but not on the page yet.
func chatbug079StoredAnswerRow(model Model, projection PersonaProgressProjection, postID string, now time.Time) ui.Node {
	href := strings.TrimSpace(projection.PrivateReplyHref)
	if href == "" || projection.AnswerDue.IsZero() || now.Before(projection.AnswerDue) {
		return chatbug040Placeholder(model, postID, "loading-answer")
	}
	name := agentReplyAuthor(model, postID, projection.AgentName)
	note := personaProgressText(model, "chat.agent.only_visible", "Only visible to you")
	saved := strings.ReplaceAll(chatbug079Text(model.Locale, "saved"), "{name}", name)
	return html.Article(html.Props{Class: "chat-ephemeral agent-reply-row chatbug079-saved", Dir: agentReplyDirection(model.Locale), Data: map[string]string{"agent-reply-state": "answered-saved", "ephemeral-thread": postID}},
		chatux003Header(model, name, chatux003AgentID(model, PersonaPostActor{}, name), agenticon.Value{}, time.Time{}, note),
		html.P(html.Props{Class: "agent-reply-answer chatbug079-saved-line", Dir: "auto"},
			html.Span(html.Props{Text: saved + " "}),
			html.A(html.Props{Class: "agent-reply-open", Href: href, Text: chatbug079Text(model.Locale, "open")})))
}

// chatbug079Text is the wording of the saved-copy row. {name} is the agent.
func chatbug079Text(locale, key string) string {
	copy := map[string][3]string{
		"saved": {"{name} answered. The answer is saved in your conversation with {name}.", "{name} hat geantwortet. Die Antwort ist in Ihrer Unterhaltung mit {name} gespeichert.", "أجاب {name}. الإجابة محفوظة في محادثتك مع {name}."},
		"open":  {"Open the answer", "Antwort öffnen", "فتح الإجابة"},
	}
	return chatbug039Text(key, copy[key][chatbug039LocaleIndex(locale)], copy[key][0])
}
