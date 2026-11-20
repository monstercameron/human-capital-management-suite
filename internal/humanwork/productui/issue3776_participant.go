package productui

import (
	"sort"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
)

// Issue 3776: the Ask view introduces the agent a person is about to ask, the
// way a coworker's card would: its picture and name, what it can use, and the
// answers it has given lately. Everything on the card comes from the snapshot
// the page was built from. Nothing is invented: a skill appears only when an
// agent in the roster declares it, the documents line only when the document
// hub is available, and an answer only when a finished task has one.

const issue3776RecentAnswers = 3

var issue3776Copy = map[string][3]string{
	"region":      {"About the agent you are asking", "Über den Agenten, den Sie fragen", "عن الوكيل الذي تسأله"},
	"can_use":     {"What it can use", "Was er nutzen kann", "ما يمكنه استخدامه"},
	"documents":   {"Documents you can read", "Dokumente, die Sie lesen dürfen", "المستندات التي يمكنك قراءتها"},
	"nothing":     {"Nothing beyond its general knowledge is set up for it yet.", "Über sein Allgemeinwissen hinaus ist noch nichts eingerichtet.", "لم يُعدّ له شيء بعد غير معرفته العامة."},
	"recent":      {"Recent answers", "Letzte Antworten", "آخر الإجابات"},
	"no_answers":  {"No answers yet. Ask a question below and the answer will be kept here.", "Noch keine Antworten. Stellen Sie unten eine Frage, die Antwort wird hier aufbewahrt.", "لا توجد إجابات بعد. اطرح سؤالًا أدناه وستُحفظ الإجابة هنا."},
	"open_answer": {"Open answer: {request}", "Antwort öffnen: {request}", "فتح الإجابة: {request}"},
}

func issue3776Text(locale LocaleContext, key string, replacements ...string) string {
	text := agentuxCopyText(issue3776Copy, locale, key)
	if len(replacements) > 0 {
		text = strings.NewReplacer(replacements...).Replace(text)
	}
	return text
}

// issue3776Skills is every distinct skill an agent in the roster declares, in
// the order first seen.
func issue3776Skills(snapshot AgentSnapshot) []string {
	seen := map[string]bool{}
	skills := []string{}
	for _, agent := range snapshot.roster() {
		for _, skill := range agent.Skills {
			if skill = strings.TrimSpace(skill); skill != "" && !seen[skill] {
				seen[skill] = true
				skills = append(skills, skill)
			}
		}
	}
	return skills
}

// issue3776RecentTasks is the newest finished tasks that have an answer to show.
func issue3776RecentTasks(snapshot AgentSnapshot) []AgentTask {
	recent := make([]AgentTask, 0, len(snapshot.Tasks))
	for _, task := range snapshot.Tasks {
		if agentTaskTone(task) == "completed" && agentTaskResultPreview(LocaleContext{}, task) != "" {
			recent = append(recent, task)
		}
	}
	sort.SliceStable(recent, func(i, j int) bool { return recent[i].UpdatedAt.After(recent[j].UpdatedAt) })
	if len(recent) > issue3776RecentAnswers {
		recent = recent[:issue3776RecentAnswers]
	}
	return recent
}

// issue3776Participant is the card above the Ask form.
func issue3776Participant(view View, locale LocaleContext, snapshot AgentSnapshot) ui.Node {
	identity := generalAgentIcon()
	can := []ui.Node{html.H3(html.Props{}, ui.Text(issue3776Text(locale, "can_use")))}
	skills := issue3776Skills(snapshot)
	if snapshot.DocumentHubAvailable {
		skills = append([]string{issue3776Text(locale, "documents")}, skills...)
	}
	if len(skills) > 0 {
		can = append(can, agentSkills(locale, skills))
	} else {
		can = append(can, html.P(html.Props{Class: "muted"}, ui.Text(issue3776Text(locale, "nothing"))))
	}
	answers := []ui.Node{html.H3(html.Props{}, ui.Text(issue3776Text(locale, "recent")))}
	if recent := issue3776RecentTasks(snapshot); len(recent) > 0 {
		items := make([]ui.Node, 0, len(recent))
		for _, task := range recent {
			request := agentTaskRequest(task)
			items = append(items, html.Li(html.Props{Class: "agents-participant-answer"},
				html.A(html.Props{Class: "agents-participant-answer-link", Href: statefulHref(view, PageAgents, "task", task.ID) + "#agents-task-title",
					Aria: map[string]string{"label": issue3776Text(locale, "open_answer", "{request}", request)}},
					html.Strong(html.Props{Dir: "auto"}, ui.Text(request)),
					html.Span(html.Props{Class: "muted", Dir: "auto"}, ui.Text(agentTaskResultPreview(locale, task))))))
		}
		answers = append(answers, html.Ul(html.Props{Class: "agents-participant-answers"}, items...))
	} else {
		answers = append(answers, html.P(html.Props{Class: "muted", Role: "status"}, ui.Text(issue3776Text(locale, "no_answers"))))
	}
	return html.Section(html.Props{Class: "agents-participant", Role: "region", Aria: map[string]string{"label": issue3776Text(locale, "region")}, Raw: map[string]any{"data-agent-participant": "true"}},
		html.Div(html.Props{Class: "agents-participant-id", Dir: string(locale.Direction)},
			agenticon.NodeFor(identity, "agents-participant"),
			html.Div(html.Props{},
				html.H2(html.Props{Dir: "auto"}, ui.Text(locale.Text("agents.general_agent"))),
				html.P(html.Props{Class: "muted", Dir: "auto", Raw: map[string]any{"data-agent-general-purpose": "true", "data-purpose-plain": locale.Text("agents.general_agent_purpose"), "data-purpose-documents": locale.Text("agents.general_agent_purpose_documents")}}, ui.Text(issue3974GeneralPurpose(locale, snapshot))))),
		html.Div(html.Props{Class: "agents-participant-can"}, can...),
		html.Div(html.Props{Class: "agents-participant-recent"}, answers...),
	)
}

const agentUX3776Styles = `.agents-participant{display:grid;gap:12px;padding:16px;border:1px solid var(--hcm-color-border);border-radius:var(--hcm-radius-surface);background:var(--hcm-color-surface);box-sizing:border-box;max-width:100%;min-width:0}.agents-participant h2{margin:0;font-size:1.0625rem}.agents-participant h3{margin:0 0 6px;font-size:.8125rem;color:var(--hcm-color-text-muted)}.agents-participant p{margin:0}.agents-participant-id{display:flex;align-items:center;gap:12px;min-width:0}.agents-participant-answers{display:grid;gap:6px;margin:0;padding:0;list-style:none}.agents-participant-answer-link{display:grid;gap:2px;min-height:44px;padding:6px 8px;border-radius:var(--hcm-radius-control);color:inherit;text-decoration:none;overflow-wrap:anywhere}.agents-participant-answer-link:hover,.agents-participant-answer-link:focus-visible{background:color-mix(in srgb,var(--hcm-color-text) 6%,transparent)}.agents-participant-answer-link span{display:-webkit-box;-webkit-box-orient:vertical;-webkit-line-clamp:2;overflow:hidden}`
