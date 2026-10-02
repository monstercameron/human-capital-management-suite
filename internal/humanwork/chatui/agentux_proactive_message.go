package chatui

import (
	"net/url"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type AgentAnnouncementSource struct {
	Title string
	Href  string
}

type AgentAnnouncementMessage struct {
	AgentName, OwnerName, Text string
	Scheduled                  bool
	PostedAt                   time.Time
	Sources                    []AgentAnnouncementSource
	identityRendered           bool
}

// RenderAgentAnnouncementMessage uses the same identity and Sources
// components as an agent reply, but deliberately has no private marker and no
// line saying who it was posted for: the agent's name and badge say enough.
func RenderAgentAnnouncementMessage(model Model, message AgentAnnouncementMessage) ui.Node {
	name := personaAgentName(model, message.AgentName)
	envelope := agentReplyEnvelope{Body: strings.TrimSpace(message.Text)}
	for _, source := range message.Sources {
		href := strings.TrimSpace(source.Href)
		parsed, err := url.Parse(href)
		if err != nil || parsed.IsAbs() || parsed.Host != "" || !validAgentDocumentHref(href) {
			href = ""
		}
		if title := strings.TrimSpace(source.Title); title != "" {
			envelope.Sources = append(envelope.Sources, agentReplySource{Title: title, Href: href})
		}
	}
	children := []ui.Node{}
	class := "agent-announcement-message"
	if !message.identityRendered {
		class = "message agent-reply-row " + class
		children = append(children, renderAgentReplyIdentity(model, name))
	}
	children = append(children, html.Div(html.Props{Class: "agent-reply-answer", Dir: "auto"}, markdownMessageBody(model, envelope.Body)...))
	children = append(children, renderAgentReplySources(model, envelope)...)
	return html.Article(html.Props{Class: class, Raw: map[string]any{"data-agent-announcement": "true"}}, children...)
}
