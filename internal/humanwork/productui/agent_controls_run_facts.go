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

// agentOpsRunOriginFacts are the trace facts of one run: what caused it, the
// evaluation outcome of the version it executed, and whether stop evidence
// links it to an incident. Causes and evaluations render only catalog words
// for known codes; the incident reference itself never reaches the page.
func agentOpsRunOriginFacts(locale LocaleContext, run AgentControlRun) []ui.Node {
	facts := make([]ui.Node, 0, 3)
	if cause := agentOpsRunCauseLabel(locale, run.Cause); cause != "" {
		facts = append(facts, agentOpsRunFact(agentControlsText(locale, "cause"), cause))
	}
	for _, status := range run.Evals {
		if label := agentOpsRunEvalLabel(locale, status); label != "" {
			facts = append(facts, agentOpsRunFact(agentControlsText(locale, "evals"), label))
			break
		}
	}
	if strings.TrimSpace(run.Incident) != "" {
		facts = append(facts, agentOpsRunFact(agentControlsText(locale, "incident"), agentOpsRunOriginText(locale, "incident_recorded")))
	}
	return facts
}

func agentOpsRunCauseLabel(locale LocaleContext, cause string) string {
	labels := map[string][3]string{
		"CHAT":            {"Chat", "Chat", "دردشة"},
		"PERSONA_MENTION": {"Mention", "Erwähnung", "إشارة"},
		"API":             {"API", "API", "API"},
		"EVENT":           {"Event", "Ereignis", "حدث"},
		"SCHEDULE":        {"Schedule", "Zeitplan", "جدول زمني"},
		"WORKFLOW":        {"Workflow", "Workflow", "سير عمل"},
	}
	values, ok := labels[strings.ToUpper(strings.TrimSpace(cause))]
	if !ok {
		return ""
	}
	return agentOpsRunOriginPick(locale, values)
}

func agentOpsRunEvalLabel(locale LocaleContext, status string) string {
	labels := map[string][3]string{
		"passed":  {"Passed", "Bestanden", "ناجح"},
		"expired": {"Expired", "Abgelaufen", "منتهي الصلاحية"},
	}
	values, ok := labels[strings.ToLower(strings.TrimSpace(status))]
	if !ok {
		return ""
	}
	return agentOpsRunOriginPick(locale, values)
}

func agentOpsRunOriginText(locale LocaleContext, key string) string {
	copy := map[string][3]string{
		"incident_recorded": {"Recorded", "Erfasst", "مسجّل"},
	}
	values, ok := copy[key]
	if !ok {
		return key
	}
	return agentOpsRunOriginPick(locale, values)
}

func agentOpsRunOriginPick(locale LocaleContext, values [3]string) string {
	switch locale.Resolved {
	case "de-DE":
		return values[1]
	case "ar":
		return values[2]
	default:
		return values[0]
	}
}
