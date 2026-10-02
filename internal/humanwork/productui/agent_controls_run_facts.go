package productui

import (
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// agentOpsRunHealthFacts are the operating facts of one run that the owner
// dashboard shows when the server projects them: how long the run waited to
// start, how many tool requests were refused, and how many sources it cited.
// They are counts and durations only. The refusal codes, the prompt and the
// sources themselves stay out of the page.
func agentOpsRunHealthFacts(locale LocaleContext, run AgentControlRun) []ui.Node {
	facts := make([]ui.Node, 0, 3)
	if wait := strings.TrimSpace(run.QueueLag); wait != "" {
		facts = append(facts, agentOpsRunFact(agentOpsRunHealthText(locale, "queue_wait"), agentRunDurationLabel(locale, wait)))
	}
	if refused := agentOpsCount(run.Denials); refused > 0 {
		facts = append(facts, agentOpsRunFact(agentOpsRunHealthText(locale, "tool_refusals"), locale.FormatNumber(strconv.Itoa(refused), 0)))
	}
	if cited := agentOpsCount(run.Citations); cited > 0 {
		facts = append(facts, agentOpsRunFact(agentOpsRunHealthText(locale, "sources_cited"), locale.FormatNumber(strconv.Itoa(cited), 0)))
	}
	return facts
}

// agentOpsCount counts the entries that say something; the server may send
// empty strings for steps that had nothing to report.
func agentOpsCount(values []string) int {
	count := 0
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			count++
		}
	}
	return count
}

func agentOpsRunHealthText(locale LocaleContext, key string) string {
	copy := map[string][3]string{
		"queue_wait":    {"Waited to start", "Wartezeit bis zum Start", "مدة الانتظار قبل البدء"},
		"tool_refusals": {"Tool requests refused", "Abgelehnte Werkzeuganfragen", "طلبات الأدوات المرفوضة"},
		"sources_cited": {"Sources cited", "Zitierte Quellen", "المصادر المستشهد بها"},
	}
	values, ok := copy[key]
	if !ok {
		return key
	}
	index := 0
	switch locale.Resolved {
	case "de-DE":
		index = 1
	case "ar":
		index = 2
	}
	return values[index]
}
