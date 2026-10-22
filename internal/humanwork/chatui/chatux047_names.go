package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
)

// CHATUX-047: names and pictures arrive with the rows they belong to. The server
// sends a message's author and a direct conversation's peer in the same answer
// as the message or the conversation (Post.author, Conversation.peer), so the
// first frame after a list answer already reads every name; the people and agent
// directory reads that follow only refine it. Where a name is truly not known
// yet the page shows the shared loading bar, never "..." and never the word
// "Conversation" standing in for a person.

// AuthorAgent is what an answer said about the agent that wrote a message: the
// agent's stored icon, and its id as the agent directory knows it. The name is
// Message.Author.
type AuthorAgent struct {
	AgentID      string
	Icon         agenticon.Value
	IconRevision int64
}

// chatux047NameBar is the shared loading bar standing where a name will be. The
// extra class gives it the width of the name it stands for.
func chatux047NameBar(extra string) ui.Node {
	return chatux037Bar("chatux047-name " + extra)
}

// chatux047Title is the text of a heading: the name, or the loading bar while
// the name is not known.
func chatux047Title(title string) ui.Node {
	if strings.TrimSpace(title) == "" {
		return chatux047NameBar("heading")
	}
	return ui.Text(title)
}

// chatux047MessagePhoto is the photograph a message's author wears: the one the
// session already holds, else the one the answer carried with the message.
func chatux047MessagePhoto(m Model, msg Message) string {
	if photo := strings.TrimSpace(m.PhotoURLs[msg.AuthorID]); photo != "" {
		return photo
	}
	return strings.TrimSpace(msg.Avatar)
}

// chatux047MessageAvatar is the avatar of a person's message.
func chatux047MessageAvatar(m Model, msg Message, class string) ui.Node {
	return personAvatarWithPhoto(msg.Author, class, chatux047MessagePhoto(m, msg))
}

// chatux047NamedAgent adopts the name and icon an answer gave for an announcement
// whose author is not a person the model knows. It only fills the identity the
// announcement path already draws; the badge and the trust come from there.
func chatux047NamedAgent(message Message) (Message, bool) {
	agent := message.AuthorAgent
	if agent == nil || strings.TrimSpace(message.Author) == "" {
		return message, false
	}
	id := strings.TrimSpace(agent.AgentID)
	if id == "" {
		id = message.AuthorID
	}
	message.PersonaActor = &PersonaActor{PersonaID: message.AuthorID, AgentID: id, Trusted: true, Icon: agent.Icon, IconRevision: agent.IconRevision}
	return message, true
}

// ChatUX047Styles sizes the loading bar to the name it stands for.
const ChatUX047Styles = `.chatux047-name.chatux037-bar{display:inline-block;inline-size:7em;max-inline-size:100%;block-size:.75em;vertical-align:middle}` +
	`.conversation-heading h1 .chatux047-name{inline-size:9em;block-size:.7em}` +
	`.message-author .chatux047-name{inline-size:6.5em;block-size:.7em}` +
	`.chat-row-name.chatux047-name-slot{display:flex;align-items:center;min-inline-size:0}` +
	`.chat-row-name.chatux047-name-slot .chatux047-name{inline-size:70%;max-inline-size:120px}` +
	// The agent count in the header line is text in that line: its own padding and
	// minimum height doubled the space after the separator and made the line taller
	// (so the title moved up) once the count arrived.
	`.conversation-topic .conversation-agent-count{min-height:0;padding:0;line-height:inherit;vertical-align:baseline}`

// chatux047PersonHeading is the heading of a direct conversation with a person:
// the name as a button that opens the person, or the loading bar while the name
// is not known (a button labelled "View" with no name would say nothing).
func chatux047PersonHeading(m Model, peer, title string) ui.Node {
	if strings.TrimSpace(title) == "" {
		return html.H1(html.Props{Class: "conversation-person-heading", Aria: map[string]string{"busy": "true"}}, chatux047NameBar("heading"))
	}
	return html.H1(html.Props{Class: "conversation-person-heading"}, personButton(m, peer, title, "conversation-person-name", ui.Text(title)))
}

// chatux047HeaderAvatar is the avatar beside that heading, opening the person
// once there is a name to open them by.
func chatux047HeaderAvatar(m Model, peer, title string, c Conversation) ui.Node {
	avatar := conversationHeaderAvatar(m, c)
	if strings.TrimSpace(title) == "" {
		return avatar
	}
	return personButton(m, peer, title, "person-avatar-button", avatar)
}
