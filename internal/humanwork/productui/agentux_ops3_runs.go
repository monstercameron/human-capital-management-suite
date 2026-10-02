package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func agentOpsRunFinished(state string) bool {
	switch strings.ToUpper(strings.TrimSpace(state)) {
	case "COMPLETED", "FAILED", "CANCELLED", "EXPIRED", "STOPPED_RESPONDING":
		return true
	default:
		return false
	}
}

func agentOpsFailureCopy(locale LocaleContext, code string) (string, string) {
	key := map[string][2]string{
		"AUTHORITY":                   {"failure_context_unavailable", "action_context_unavailable"},
		"GRANT":                       {"failure_out_of_scope", "action_out_of_scope"},
		"INSTALLATION":                {"failure_context_unavailable", "action_context_unavailable"},
		"AUDIENCE":                    {"failure_out_of_scope", "action_out_of_scope"},
		"BUDGET":                      {"failure_out_of_scope", "action_out_of_scope"},
		"MODEL_ROUTE":                 {"failure_model_unavailable", "action_model_unavailable"},
		"MODEL_CALL":                  {"failure_model_unavailable", "action_model_unavailable"},
		"MODEL_OUTPUT":                {"failure_model_incomplete", "action_model_incomplete"},
		"TOOL_SCOPE":                  {"failure_tool", "action_tool"},
		"TOOL_CALL":                   {"failure_tool", "action_tool"},
		"OUTPUT_GROUNDING":            {"failure_output", "action_output"},
		"OUTPUT_SCHEMA":               {"failure_output", "action_output"},
		"DELIVERY_AUDIENCE":           {"failure_delivery", "action_delivery"},
		"DELIVERY_WRITE":              {"failure_delivery", "action_delivery"},
		"DEADLINE":                    {"failure_out_of_scope", "action_out_of_scope"},
		"STOPPED":                     {"failure_out_of_scope", "action_out_of_scope"},
		"CONTEXT_UNAVAILABLE":         {"failure_context_unavailable", "action_context_unavailable"},
		"MODEL_UNAVAILABLE":           {"failure_model_unavailable", "action_model_unavailable"},
		"MODEL_REFUSED_OR_INCOMPLETE": {"failure_model_incomplete", "action_model_incomplete"},
		"TOOL_EXECUTION_FAILED":       {"failure_tool", "action_tool"},
		"OUTPUT_REJECTED":             {"failure_output", "action_output"},
		"DELIVERY_FAILED":             {"failure_delivery", "action_delivery"},
		"OUT_OF_SCOPE":                {"failure_out_of_scope", "action_out_of_scope"},
	}[strings.ToUpper(strings.TrimSpace(code))]
	if key[0] == "" {
		return "", ""
	}
	return agentControlsText(locale, key[0]), agentControlsText(locale, key[1])
}

func agentOpsRecentRun(locale LocaleContext, run AgentControlRun) ui.Node {
	text := func(key string) string { return agentControlsText(locale, key) }
	name := agentControlNameVersion(locale, agentControlDisplayName(locale, "run", run.Name, run.ID), run.Version)
	requester := strings.TrimSpace(run.RequestedBy)
	if requester == "" {
		requester = text("workspace_member")
	}
	location := strings.TrimSpace(run.Location)
	if location == "" {
		location = text("hidden_conversation")
	}
	started := strings.TrimSpace(run.Started)
	if started == "" {
		started = run.Since
	}
	started = agentOperationsFormatInstant(locale, started)
	facts := []ui.Node{
		agentOpsRunFact(text("who_asked"), requester),
		html.P(html.Props{Class: "agent-operations-run-fact"}, html.Strong(html.Props{}, ui.Text(text("where")+": ")), agentRunLocation(locale, run)),
		agentOpsRunFact(text("started"), started),
		agentOpsRunFact(text("duration"), agentRunDurationLabel(locale, run.Duration)),
		agentRunOutcome(locale, run),
	}
	failureKey := run.Failure
	if strings.TrimSpace(run.FailureGate) != "" {
		failureKey = strings.ToLower(strings.TrimSpace(run.FailureGate))
	}
	if reason, action := agentOpsFailureCopy(locale, failureKey); reason != "" {
		facts = append(facts,
			html.Div(html.Props{Class: "agent-operations-failure"},
				agentOpsRunFact(text("failure_reason"), reason),
				agentOpsRunFact(text("suggested_action"), action),
			),
		)
	}
	facts = append(facts, agentOpsRecentRunDetails(locale, run))
	return html.Article(html.Props{Class: "card persona-admin-card agent-operations-run agent-operations-run-finished", Raw: map[string]any{"data-control-run": run.ID}},
		append([]ui.Node{html.H4(html.Props{}, ui.Text(name))}, facts...)...,
	)
}

func agentOpsRecentRunDetails(locale LocaleContext, run AgentControlRun) ui.Node {
	// The cost is shown when the server projects it; otherwise the page says
	// it has none and where to ask.
	cost := html.P(html.Props{}, html.Strong(html.Props{}, ui.Text(agentUXR7Text(locale, "cost")+": ")), ui.Text(agentUXR7Text(locale, "cost_unavailable")+" "), html.A(html.Props{Href: agentSetupHref(locale)}, ui.Text(agentUXR7Text(locale, "open_setup"))))
	if spend := strings.TrimSpace(run.Spend); spend != "" {
		cost = agentOpsRunFact(agentUXR7Text(locale, "cost"), spend)
	}
	items := []ui.Node{
		cost,
		agentOpsRunFact(agentUXR7Text(locale, "asked_by"), run.RequestedBy),
		html.P(html.Props{}, html.Strong(html.Props{}, ui.Text(agentControlsText(locale, "where")+": ")), agentRunLocation(locale, run)),
		agentOpsRunFact(agentControlsText(locale, "started"), agentOperationsFormatInstant(locale, run.Started)),
		agentOpsRunFact(agentControlsText(locale, "duration"), agentRunDurationLabel(locale, run.Duration)),
		agentRunOutcome(locale, run),
	}
	items = append(items, agentOpsRunHealthFacts(locale, run)...)
	if run.State == "FAILED" {
		_, next := agentOpsFailureCopy(locale, run.FailureGate)
		if next == "" {
			_, next = agentOpsFailureCopy(locale, run.Failure)
		}
		if next != "" {
			items = append(items, html.P(html.Props{}, ui.Text(next)))
		}
	}
	return html.Div(html.Props{Class: "agent-operations-run-details"}, items...)
}

func agentOpsTechnicalLabel(locale LocaleContext, key string) string {
	values := map[string][3]string{
		"gate":     {"Gate", "Gate", "البوابة"},
		"owner":    {"Owner", "Owner", "المالك"},
		"location": {"Location", "Location", "الموضع"},
	}
	index := 0
	if locale.Resolved == "de-DE" {
		index = 1
	} else if locale.Resolved == "ar" {
		index = 2
	}
	return values[key][index]
}

func agentOpsRunFact(label, value string) ui.Node {
	if strings.TrimSpace(value) == "" {
		value = "—"
	}
	return html.P(html.Props{Class: "agent-operations-run-fact"}, html.Strong(html.Props{}, ui.Text(label+": ")), ui.Text(value))
}
