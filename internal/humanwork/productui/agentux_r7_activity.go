package productui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type AgentRunHistoryFilter struct {
	Agent, Version, Outcome string
	Page                    int
}
type AgentRunHistoryPage struct {
	Runs               []AgentControlRun
	Total, Page, Pages int
}

func FilterAgentRunHistory(runs []AgentControlRun, filter AgentRunHistoryFilter) AgentRunHistoryPage {
	matched := make([]AgentControlRun, 0, len(runs))
	for _, run := range runs {
		if (filter.Agent == "" || run.Name == filter.Agent) && (filter.Version == "" || run.Version == filter.Version) && (filter.Outcome == "" || run.State == filter.Outcome) {
			matched = append(matched, run)
		}
	}
	sort.SliceStable(matched, func(i, j int) bool { return agentRunStart(matched[i]).After(agentRunStart(matched[j])) })
	pages := max(1, (len(matched)+24)/25)
	page := min(max(1, filter.Page), pages)
	start := min((page-1)*25, len(matched))
	end := min(start+25, len(matched))
	return AgentRunHistoryPage{Runs: matched[start:end], Total: len(matched), Page: page, Pages: pages}
}

func agentRunStart(run AgentControlRun) time.Time {
	raw := run.Started
	if raw == "" {
		raw = run.Since
	}
	at, _ := time.Parse(time.RFC3339Nano, raw)
	return at
}

type AgentRunFailureWarning struct{ AgentID, Name, Version, Since, RollbackVersion string }

func AgentFailureStreak(runs []AgentControlRun, agentID, name string) (AgentRunFailureWarning, bool) {
	ordered := make([]AgentControlRun, 0, len(runs))
	for _, run := range runs {
		matches := agentID != "" && run.AgentID == agentID
		if agentID == "" || run.AgentID == "" {
			matches = name != "" && run.Name == name
		}
		if matches {
			ordered = append(ordered, run)
		}
	}
	sort.SliceStable(ordered, func(i, j int) bool { return agentRunStart(ordered[i]).After(agentRunStart(ordered[j])) })
	seen := map[string]bool{}
	for _, newest := range ordered {
		if seen[newest.Version] {
			continue
		}
		seen[newest.Version] = true
		recent := make([]AgentControlRun, 0, 5)
		for _, run := range ordered {
			if run.Version == newest.Version {
				recent = append(recent, run)
			}
			if len(recent) == 5 {
				break
			}
		}
		if len(recent) < 5 {
			continue
		}
		failed := true
		for _, run := range recent {
			if !strings.EqualFold(run.State, "FAILED") {
				failed = false
				break
			}
		}
		if !failed {
			continue
		}
		warning := AgentRunFailureWarning{AgentID: agentID, Name: name, Version: newest.Version, Since: recent[4].Started}
		if warning.Since == "" {
			warning.Since = recent[4].Since
		}
		current, _ := strconv.Atoi(newest.Version)
		for _, run := range ordered {
			v, _ := strconv.Atoi(run.Version)
			if v < current && strings.EqualFold(run.State, "COMPLETED") {
				warning.RollbackVersion = run.Version
				break
			}
		}
		return warning, true
	}
	return AgentRunFailureWarning{}, false
}

func agentFailureWarning(locale LocaleContext, warning AgentRunFailureWarning, version, rollbackHref, setupHref string) ui.Node {
	children := []ui.Node{html.P(html.Props{}, ui.Text(agentUXR7Text(locale, "failure_streak", "{agent}", warning.Name, "{version}", personaAdminLocalizedNumber(locale, warning.Version), "{time}", agentOperationsFormatInstant(locale, warning.Since))))}
	if version != "" {
		children = append(children, html.A(html.Props{Class: "button secondary", Href: rollbackHref}, ui.Text(agentUXR7Text(locale, "rollback", "{version}", personaAdminLocalizedNumber(locale, version)))))
	}
	children = append(children, html.A(html.Props{Class: "button secondary", Href: setupHref}, ui.Text(agentUXR7Text(locale, "pause"))))
	return html.Div(html.Props{Class: "agent-run-warning", Role: "alert"}, children...)
}

func agentRunLocation(locale LocaleContext, run AgentControlRun) ui.Node {
	label := strings.TrimSpace(run.Location)
	if strings.HasPrefix(label, "A person's private conversation") {
		label = agentUXR7Text(locale, "dm", "{person}", run.RequestedBy)
	}
	if strings.HasPrefix(label, "Your conversation with ") {
		label = agentUXR7Text(locale, "your_conversation", "{agent}", strings.TrimPrefix(label, "Your conversation with "))
	}
	if label == "" {
		label = agentControlsText(locale, "hidden_conversation")
	}
	content := ui.Node(html.Tag("bdi", html.Props{}, ui.Text(label)))
	if strings.HasPrefix(label, "#") {
		content = html.Tag("bdi", html.Props{Dir: "ltr"}, ui.Text(label))
	}
	if run.ConversationID != "" {
		return html.A(html.Props{Href: personaAdminConversationHref(run.ConversationID)}, content)
	}
	return content
}

func agentRunOutcome(locale LocaleContext, run AgentControlRun) ui.Node {
	tone := "neutral"
	if run.State == "FAILED" {
		tone = "danger"
	} else if run.State == "COMPLETED" {
		tone = "success"
	}
	children := []ui.Node{html.Span(html.Props{Class: "status agent-run-outcome", Raw: map[string]any{"data-tone": tone}}, ui.Text(agentControlLocalizedState(locale, run.State)))}
	if run.State == "FAILED" {
		reason, _ := agentOpsFailureCopy(locale, run.FailureGate)
		if reason == "" {
			reason, _ = agentOpsFailureCopy(locale, run.Failure)
		}
		if reason == "" {
			reason = locale.Text("agents.failure_unknown")
		}
		children = append(children, html.Small(html.Props{Class: "agent-run-failure"}, ui.Text(reason)))
	}
	return html.Div(html.Props{Class: "agent-run-outcome-cell"}, children...)
}

func RenderAgentRunHistory(locale LocaleContext, runs []AgentControlRun, filter AgentRunHistoryFilter) ui.Node {
	page := FilterAgentRunHistory(runs, filter)
	fields := []ui.Node{}
	for _, field := range []struct{ key, all, value string }{{"agent", "all_agents", filter.Agent}, {"version", "all_versions", filter.Version}, {"outcome", "all_outcomes", filter.Outcome}} {
		values := map[string]bool{}
		for _, run := range runs {
			v := run.Name
			if field.key == "version" {
				v = run.Version
			}
			if field.key == "outcome" {
				v = run.State
			}
			if v != "" {
				values[v] = true
			}
		}
		ordered := []string{}
		for v := range values {
			ordered = append(ordered, v)
		}
		sort.Strings(ordered)
		options := []ui.Node{html.Option(html.Props{Value: "", Selected: field.value == ""}, ui.Text(agentUXR7Text(locale, field.all)))}
		for _, v := range ordered {
			label := v
			if field.key == "agent" {
				label = agentControlDisplayName(locale, "run", v, "")
			}
			if field.key == "version" {
				label = personaAdminLocalizedNumber(locale, v)
			}
			if field.key == "outcome" {
				label = agentControlLocalizedState(locale, v)
			}
			options = append(options, html.Option(html.Props{Value: v, Selected: field.value == v}, ui.Text(label)))
		}
		id := "agent-history-" + field.key
		label := agentUXR7Text(locale, "agent_version")
		if field.key == "version" {
			label = agentUXR7Text(locale, "version")
		}
		if field.key == "outcome" {
			label = agentControlsText(locale, "outcome")
		}
		fields = append(fields, html.Div(html.Props{Class: "persona-admin-editor-field"}, html.Label(html.Props{For: id}, ui.Text(label)), html.Select(html.Props{ID: id, Raw: map[string]any{"data-agent-history-filter": field.key}}, options...)))
	}
	head := []ui.Node{}
	for _, label := range []string{agentControlsText(locale, "started"), agentUXR7Text(locale, "agent_version"), agentUXR7Text(locale, "asked_by"), agentControlsText(locale, "where"), agentControlsText(locale, "duration"), agentControlsText(locale, "outcome")} {
		head = append(head, html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, ui.Text(label)))
	}
	rows, cards := []ui.Node{}, []ui.Node{}
	for _, run := range page.Runs {
		name := agentControlNameVersion(locale, agentControlDisplayName(locale, "run", run.Name, run.ID), personaAdminLocalizedNumber(locale, run.Version))
		at := agentOperationsFormatInstant(locale, run.Started)
		rows = append(rows, html.Tr(html.Props{Raw: map[string]any{"data-control-run": run.ID}}, html.Td(html.Props{}, html.Time(html.Props{Title: run.Started, Raw: map[string]any{"datetime": run.Started}}, ui.Text(at))), html.Td(html.Props{}, html.Details(html.Props{Class: "agent-run-details"}, html.Summary(html.Props{Raw: map[string]any{"data-chevron": "›"}}, ui.Text(name)), agentOpsRecentRunDetails(locale, run))), html.Td(html.Props{}, html.Tag("bdi", html.Props{}, ui.Text(run.RequestedBy))), html.Td(html.Props{}, agentRunLocation(locale, run)), html.Td(html.Props{}, ui.Text(agentRunDurationLabel(locale, run.Duration))), html.Td(html.Props{}, agentRunOutcome(locale, run))))
		cards = append(cards, agentUXR7RecentRunCard(locale, run))
	}
	nodes := []ui.Node{html.H3(html.Props{Class: "agent-operations-recent-title"}, ui.Text(agentUXR7Text(locale, "recent", "{count}", locale.FormatNumber(strconv.Itoa(page.Total), 0)))), html.Div(html.Props{Class: "agent-history-filters"}, fields...)}
	if page.Total == 0 {
		nodes = append(nodes, html.P(html.Props{Role: "status"}, ui.Text(agentUXR7Text(locale, "no_runs_match"))))
	}
	nodes = append(nodes, html.Div(html.Props{Class: "agent-history-desktop"}, html.Table(html.Props{Class: "agent-history-table"}, html.Thead(html.Props{}, html.Tr(html.Props{}, head...)), html.Tbody(html.Props{}, rows...))), html.Div(html.Props{Class: "agent-history-mobile"}, cards...), html.Nav(html.Props{Class: "agent-history-pagination", Aria: map[string]string{"label": agentUXR7Text(locale, "recent", "{count}", fmt.Sprint(page.Total))}}, html.Button(html.Props{Type: "button", Class: "button secondary", Disabled: page.Page <= 1, Raw: map[string]any{"data-agent-history-page": page.Page - 1}}, ui.Text(agentUXR7Text(locale, "previous"))), html.Span(html.Props{Role: "status"}, ui.Text(agentUXR7Text(locale, "page", "{page}", locale.FormatNumber(fmt.Sprint(page.Page), 0), "{total}", locale.FormatNumber(fmt.Sprint(page.Pages), 0)))), html.Button(html.Props{Type: "button", Class: "button secondary", Disabled: page.Page >= page.Pages, Raw: map[string]any{"data-agent-history-page": page.Page + 1}}, ui.Text(agentUXR7Text(locale, "next")))))
	return html.Section(html.Props{ID: "agent-run-history", Class: "agent-run-history", Raw: map[string]any{"data-history-page": page.Page}}, nodes...)
}

func agentUXR7RecentRunCard(locale LocaleContext, run AgentControlRun) ui.Node {
	name := agentControlNameVersion(locale, agentControlDisplayName(locale, "run", run.Name, run.ID), personaAdminLocalizedNumber(locale, run.Version))
	children := []ui.Node{
		html.H4(html.Props{}, ui.Text(name)),
		html.Time(html.Props{Title: run.Started, Raw: map[string]any{"datetime": run.Started}}, ui.Text(agentOperationsFormatInstant(locale, run.Started))),
		agentOpsRunFact(agentUXR7Text(locale, "asked_by"), run.RequestedBy),
		html.P(html.Props{}, agentRunLocation(locale, run)),
		agentOpsRunFact(agentControlsText(locale, "duration"), agentRunDurationLabel(locale, run.Duration)),
		agentRunOutcome(locale, run),
	}
	if run.State == "FAILED" {
		_, next := agentOpsFailureCopy(locale, run.FailureGate)
		if next == "" {
			_, next = agentOpsFailureCopy(locale, run.Failure)
		}
		if next != "" {
			children = append(children, html.P(html.Props{}, ui.Text(next)))
		}
	}
	children = append(children, html.Details(html.Props{Class: "agent-operations-technical"}, html.Summary(html.Props{Raw: map[string]any{"data-chevron": "›"}}, ui.Text(agentControlsText(locale, "technical"))), agentOpsRecentRunDetails(locale, run)))
	return html.Article(html.Props{Class: "card persona-admin-card agent-operations-run agent-operations-run-finished", Raw: map[string]any{"data-control-run": run.ID}}, children...)
}

func agentRunDurationLabel(locale LocaleContext, raw string) string {
	duration, err := time.ParseDuration(raw)
	if err != nil {
		return raw
	}
	if duration < time.Second {
		return agentUXR7Text(locale, "under_second")
	}
	count, key := int(duration.Round(time.Second)/time.Second), "duration_seconds"
	if duration >= time.Minute {
		count, key = int(duration/time.Minute), "duration_minutes"
	}
	if duration >= time.Hour {
		count, key = int(duration/time.Hour), "duration_hours"
	}
	return agentUXR7Text(locale, key, "{count}", locale.FormatNumber(strconv.Itoa(count), 0))
}
