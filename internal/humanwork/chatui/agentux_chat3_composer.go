package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func bodyWithAgentMentions(body string, references []ChatReference) string {
	body = strings.TrimSpace(body)
	prefixes := make([]string, 0, len(references))
	for _, reference := range references {
		if reference.Kind != "AGENT_MENTION" || strings.TrimSpace(reference.Display) == "" {
			continue
		}
		mention := "@" + strings.TrimSpace(reference.Display)
		if strings.HasPrefix(strings.ToLower(body), strings.ToLower(mention)+" ") || strings.EqualFold(body, mention) {
			continue
		}
		prefixes = append(prefixes, mention)
	}
	if len(prefixes) == 0 {
		return body
	}
	return strings.Join(prefixes, " ") + " " + body
}

func composerAgentToken(name string) ui.Node {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	return html.Span(html.Props{Class: "composer-agent-token", Role: "status"}, html.Span(html.Props{Class: "mention-chip mention-chip-agent", Text: "@" + name}))
}
