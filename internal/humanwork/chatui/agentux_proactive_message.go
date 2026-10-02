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
// components as an agent reply, but deliberately has no private marker.
func RenderAgentAnnouncementMessage(model Model, message AgentAnnouncementMessage) ui.Node {
	name := personaAgentName(model, message.AgentName)
	owner := strings.TrimSpace(message.OwnerName)
	line := agentAnnouncementChatText(model.Locale, "scheduled")
	if !message.Scheduled {
		line = agentAnnouncementChatText(model.Locale, "once")
	}
	line = strings.NewReplacer("{owner}", owner, "{agent}", name).Replace(line)
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
	children = append(children, html.P(html.Props{Class: "muted agent-announcement-attribution", Dir: "auto"}, ui.Text(line)))
	return html.Article(html.Props{Class: class, Raw: map[string]any{"data-agent-announcement": "true"}}, children...)
}

func agentAnnouncementChatText(locale, key string) string {
	copy := map[string]map[string]string{
		"en-US": {"scheduled": "Posted on a schedule set by {owner}", "once": "Posted for {owner}"},
		"de-DE": {"scheduled": "Nach einem von {owner} festgelegten Zeitplan veröffentlicht", "once": "Für {owner} veröffentlicht"},
		"ar":    {"scheduled": "نُشر وفق جدول أعدّه {owner}", "once": "نُشر نيابة عن {owner}"},
	}
	language := "en-US"
	if strings.HasPrefix(strings.ToLower(locale), "de") {
		language = "de-DE"
	} else if strings.HasPrefix(strings.ToLower(locale), "ar") {
		language = "ar"
	}
	return chatbug039Text(key, copy[language][key], copy["en-US"][key])
}
