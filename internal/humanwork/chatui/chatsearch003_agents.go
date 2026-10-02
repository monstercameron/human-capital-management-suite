package chatui

import (
	"net/url"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
)

// CHATSEARCH-003: agents, a person's own agent tasks and their own
// announcements are results of the Chat search. They are not in a
// conversation, so a result is a link to the page that holds the record.

// chatsearch003Names are the result kind and the group heading of each agent
// kind, in English, German and Arabic.
var chatsearch003Names = map[chatsearch.Kind][2][3]string{
	chatsearch.Agent:             {{"Agent", "Agent", "وكيل"}, {"Agents", "Agenten", "الوكلاء"}},
	chatsearch.AgentTask:         {{"Agent task", "Agentenaufgabe", "مهمة وكيل"}, {"Your agent tasks", "Deine Agentenaufgaben", "مهام الوكلاء الخاصة بك"}},
	chatsearch.AgentAnnouncement: {{"Scheduled announcement", "Geplante Ankündigung", "إعلان مجدول"}, {"Your scheduled announcements", "Deine geplanten Ankündigungen", "إعلاناتك المجدولة"}},
}

// chatsearch003Name is the kind label (heading false) or group heading of an
// agent kind, and false for every other kind.
func chatsearch003Name(locale string, kind chatsearch.Kind, heading bool) (string, bool) {
	names, ok := chatsearch003Names[kind]
	if !ok {
		return "", false
	}
	i := 0
	switch lower := strings.ToLower(locale); {
	case strings.HasPrefix(lower, "de"):
		i = 1
	case strings.HasPrefix(lower, "ar"):
		i = 2
	}
	part := 0
	if heading {
		part = 1
	}
	return chatbug039Text(string(kind), names[part][i], names[part][0]), true
}

// ChatSearchAgentHref is where an agent result leads: the Agents page for an
// agent, the task on that page for a task, and the agent operations page for
// an announcement. It is "" for a result of any other kind.
func ChatSearchAgentHref(row chatsearch.Row) string {
	switch row.Kind {
	case chatsearch.Agent:
		return "/workspace/app/agents"
	case chatsearch.AgentTask:
		if row.Target.ItemID == "" {
			return "/workspace/app/agents"
		}
		return "/workspace/app/agents?" + url.Values{"task": {row.Target.ItemID}}.Encode() + "#agents-task-title"
	case chatsearch.AgentAnnouncement:
		return "/workspace/app/agent-operations"
	}
	return ""
}

// chatsearch003Result draws an agent result: the kind, "Only you" on a record
// only its owner finds, and the name or text, all inside one link. An agent's
// first line is its name and the rest its purpose.
func chatsearch003Result(locale string, row chatsearch.Row, words string) ui.Node {
	kind := chatsearchKind(locale, row.Kind)
	line := []ui.Node{html.Strong(html.Props{Class: "chatsearch-kind", Text: kind})}
	if row.Private {
		line = append(line, html.Span(html.Props{Class: "chatsearch-private", Text: chatsearchText(locale, "only")}))
	}
	title, rest, _ := strings.Cut(chatDisplayText(row.Text), "\n")
	body := []ui.Node{html.Div(html.Props{Class: "chatsearch-meta"}, line...), html.Div(html.Props{Class: "chatsearch-text", Dir: "auto"}, highlightText(title, words)...)}
	if rest = strings.TrimSpace(rest); rest != "" {
		body = append(body, html.Div(html.Props{Class: "chatsearch-text chatsearch-agent-purpose", Dir: "auto"}, highlightText(rest, words)...))
	}
	label := chatsearchText(locale, "open") + ": " + kind + " · " + title
	link := html.A(html.Props{Class: "chatsearch-open chatsearch-agent-link", Href: ChatSearchAgentHref(row), Aria: map[string]string{"label": label}}, html.Div(html.Props{Class: "chatsearch-plain"}, body...))
	return html.Div(html.Props{Class: "chatsearch-result chatsearch-agent-result", Data: map[string]string{"chatsearch-action": "visit", "kind": string(row.Kind)}}, link)
}

const chatsearch003Styles = `
.chatsearch-agent-result .chatsearch-agent-link{display:block;min-height:44px;padding:.5rem .75rem;border:1px solid var(--hcm-color-border);border-radius:var(--hcm-radius-control);color:var(--hcm-color-text);background:var(--hcm-color-surface);text-align:start;text-decoration:none;cursor:pointer}
.chatsearch-agent-result .chatsearch-agent-link:hover{border-color:var(--hcm-color-brand-primary)}
.chatsearch-agent-result .chatsearch-agent-purpose{color:var(--hcm-color-text-muted,var(--hcm-color-text));overflow-wrap:anywhere}
`
