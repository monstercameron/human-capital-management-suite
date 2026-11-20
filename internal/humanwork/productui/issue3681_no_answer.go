package productui

import (
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/localize"
)

// Issue 3681: earlier runs that produced no answer stored the placeholder
// "No answer was produced." as their answer and were listed as Completed. The
// placeholder is not an answer: such a task is a failure, says why, and offers
// nothing to copy.

// agentNoAnswerFailure is the failure reason a placeholder-only task is given.
// localizedAgentFailureReason turns it into the plain sentence for the locale.
const agentNoAnswerFailure = "no_answer_produced"

func agentTextIsNoAnswerPlaceholder(text string) bool {
	text = strings.ToLower(strings.TrimRight(strings.TrimSpace(text), ". "))
	return text == "no answer was produced"
}

// agentUX3681Settle files a completed task whose only "answer" is the
// placeholder under Failed, with a reason and without the placeholder text.
func agentUX3681Settle(task AgentTask) AgentTask {
	task.State = agentTaskBaseState(task)
	if task.State != AgentTaskCompleted {
		return task
	}
	answer, preview := strings.TrimSpace(task.AnswerText), strings.TrimSpace(task.ResultPreview)
	placeholder := agentTextIsNoAnswerPlaceholder(answer) || agentTextIsNoAnswerPlaceholder(preview)
	if !placeholder || (answer != "" && !agentTextIsNoAnswerPlaceholder(answer)) || (preview != "" && !agentTextIsNoAnswerPlaceholder(preview)) {
		return task
	}
	task.State = AgentTaskFailed
	task.AnswerText, task.ResultPreview = "", ""
	if strings.TrimSpace(task.FailureReason) == "" {
		task.FailureReason = agentNoAnswerFailure
	}
	return task
}

func withIssue3681Messages(catalog map[string]map[string]localize.Message) map[string]map[string]localize.Message {
	for locale, text := range map[string]string{
		"en-US": "The agent produced no answer. Agents may not be set up for this workspace yet.",
		"de-DE": "Der Agent hat keine Antwort erzeugt. Möglicherweise sind Agenten für diesen Arbeitsbereich noch nicht eingerichtet.",
		"ar":    "لم ينتج الوكيل أي إجابة. قد لا يكون الوكلاء معدّين لمساحة العمل هذه بعد.",
	} {
		catalog[locale]["agents.failure_no_answer"] = localize.Message{Text: text}
	}
	return catalog
}
