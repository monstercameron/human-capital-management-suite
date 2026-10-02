package productui

import (
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func AgentControlsMount(locale LocaleContext) ui.Node {
	return AgentControlsMountForTab(locale, true)
}

func AgentControlsMountForTab(locale LocaleContext, active bool) ui.Node {
	return html.Section(html.Props{ID: "agent-controls", Class: "persona-admin-editor agent-operations-region", Dir: string(locale.Direction), Hidden: !active, Raw: map[string]any{"role": "tabpanel", "data-locale": locale.Resolved, "data-agent-operations-panel": "running"}, Aria: map[string]string{"labelledby": "agent-operations-tab-running", "busy": "true"}},
		html.H2(html.Props{ID: "agent-controls-title"}, ui.Text(agentControlsText(locale, "title"))),
		html.P(html.Props{Class: "muted"}, ui.Text(agentControlsText(locale, "region_help"))),
		html.P(html.Props{Role: "status", Aria: map[string]string{"live": "polite", "atomic": "true"}}, ui.Text(agentControlsText(locale, "loading"))))
}

// RenderAgentControls renders text as text nodes, including source references.
// It never consumes private prompts or memory bodies.
func RenderAgentControls(locale LocaleContext, snapshot AgentControlsSnapshot, message string) ui.Node {
	text := func(key string) string { return agentControlsText(locale, key) }
	header := []ui.Node{html.Div(html.Props{}, html.H2(html.Props{ID: "agent-controls-title"}, ui.Text(text("title"))), html.P(html.Props{Class: "muted"}, ui.Text(text("region_help"))))}
	if snapshot.Available && message != "denied" {
		header = append(header, html.Button(html.Props{Type: "button", Class: "button secondary", Raw: map[string]any{"data-owner-refresh": "true", "data-busy-label": text("working_short")}}, ui.Text(text("refresh"))))
	}
	children := []ui.Node{html.Div(html.Props{Class: "agent-operations-region-header"}, header...)}
	if message == "denied" {
		notice := text("denied")
		if strings.TrimSpace(snapshot.OwnerName) != "" {
			notice = strings.ReplaceAll(text("denied_owner"), "{owner}", snapshot.OwnerName)
		}
		children = append(children, html.P(html.Props{Class: "notice", Role: "note"}, ui.Text(notice)))
		return html.Div(html.Props{Dir: string(locale.Direction), Class: "agent-operations-region-content"}, children...)
	}
	if !snapshot.Available {
		cause := text("unavailable_cause")
		switch message {
		case "timed_out":
			cause = text("timed_out_cause")
		case "access_denied":
			cause = text("access_denied_cause")
		}
		help := text("unavailable_help")
		if contact := strings.TrimSpace(snapshot.TechnicalContact); contact != "" {
			help = strings.ReplaceAll(text("unavailable_contact"), "{contact}", contact)
		}
		children = append(children, html.Div(html.Props{Class: "agent-operations-alert", Role: "alert", Raw: map[string]any{"data-agent-controls-state": "unavailable"}},
			html.Div(html.Props{}, html.Strong(html.Props{}, ui.Text(text("unavailable"))), html.P(html.Props{}, ui.Text(cause)), html.P(html.Props{}, ui.Text(text("unavailable_fallback_before")), html.A(html.Props{Href: agentSetupHref(locale)}, ui.Text(text("unavailable_fallback_link"))), ui.Text(text("unavailable_fallback_after"))), html.P(html.Props{}, ui.Text(help))),
			html.Button(html.Props{Type: "button", Class: "button secondary", Raw: map[string]any{"data-owner-refresh": "true", "data-busy-label": text("working_short")}}, ui.Text(text("retry"))),
		))
		return html.Div(html.Props{Dir: string(locale.Direction), Class: "agent-operations-region-content"}, children...)
	}
	status := strings.ReplaceAll(text("updated"), "{time}", agentOperationsFormatInstant(locale, snapshot.UpdatedAt))
	if localized := text(message); message != "" && message != "done" && localized != "" {
		status = localized
	}
	children = append(children, html.P(html.Props{ID: "agent-controls-status", Class: "agent-operations-updated", Role: "status", Raw: map[string]any{"tabindex": "-1", "data-msg-invalid": text("invalid")}, Aria: map[string]string{"live": "polite", "atomic": "true"}}, ui.Text(status)))
	running := make([]AgentControlRun, 0, len(snapshot.Runs))
	finished := make([]AgentControlRun, 0, len(snapshot.Runs))
	for _, run := range snapshot.Runs {
		if agentOpsRunFinished(run.State) {
			finished = append(finished, run)
		} else {
			running = append(running, run)
		}
	}
	children = append(children, html.H3(html.Props{}, ui.Text(agentUXR7Text(locale, "running_now", "{count}", locale.FormatNumber(fmt.Sprint(len(running)), 0)))))
	for _, persona := range snapshot.Agents {
		warning, ok := AgentFailureStreak(snapshot.Runs, persona.ID, persona.Name)
		if ok {
			version := personaAdminRollbackBefore(persona, warning.Version)
			children = append(children, agentFailureWarning(locale, warning, version, personaAdminRollbackHref(persona.ID, version), agentSetupHref(locale)+"#persona-admin-"+safeAgentDOMToken(persona.ID)))
		}
		children = append(children, html.Article(html.Props{Class: "agent-owner-pause-row", Raw: map[string]any{"data-persona-id": persona.ID}}, html.Strong(html.Props{}, ui.Text(persona.Name)), personaAdminPauseButton(locale, persona, PersonaAdminSnapshot{CommandPermissionsAvailable: true, AllowedCommands: snapshot.AllowedCommands})))
	}
	if len(snapshot.Agents) == 0 {
		seen := map[string]bool{}
		for _, run := range snapshot.Runs {
			if seen[run.Name] {
				continue
			}
			seen[run.Name] = true
			if warning, ok := AgentFailureStreak(snapshot.Runs, run.AgentID, run.Name); ok {
				children = append(children, agentFailureWarning(locale, warning, "", personaAdminRollbackHref(run.AgentID, warning.RollbackVersion), agentSetupHref(locale)))
			}
		}
	}

	if agentControlsNeedsAuditReason(snapshot, running) {
		children = append(children, html.Div(html.Props{Class: "persona-admin-editor-grid"},
			html.Div(html.Props{Class: "persona-admin-editor-field"}, html.Label(html.Props{For: "agent-controls-reason"}, ui.Text(text("reason"))), html.Input(html.Props{ID: "agent-controls-reason", Type: "text", Aria: map[string]string{"describedby": "agent-controls-reason-help"}}), html.Small(html.Props{ID: "agent-controls-reason-help", Class: "muted"}, ui.Text(text("reason_help")))),
			html.Div(html.Props{Class: "persona-admin-editor-field"}, html.Label(html.Props{For: "agent-controls-incident"}, ui.Text(text("incident"))), html.Input(html.Props{ID: "agent-controls-incident", Type: "text", Aria: map[string]string{"describedby": "agent-controls-reason-help"}})),
		))
	}
	if len(running) == 0 {
		children = append(children, html.Div(html.Props{Class: "agent-operations-empty"}, html.P(html.Props{}, ui.Text(text("empty"))), html.P(html.Props{Class: "muted"}, ui.Text(text("empty_help")))))
	}
	for _, run := range running {
		name := agentControlDisplayName(locale, "run", run.Name, run.ID)
		displayName := agentControlNameVersion(locale, name, run.Version)
		facts := []ui.Node{html.P(html.Props{}, html.Strong(html.Props{}, ui.Text(text("state")+": ")), ui.Text(agentControlLocalizedState(locale, run.State)))}
		if strings.TrimSpace(run.Since) != "" {
			facts = append(facts, html.P(html.Props{}, html.Strong(html.Props{}, ui.Text(text("since")+": ")), ui.Text(agentOperationsFormatInstant(locale, run.Since))))
		}
		facts = append(facts,
			html.Details(html.Props{Class: "agent-operations-technical"}, html.Summary(html.Props{Raw: map[string]any{"data-chevron": "›"}}, ui.Text(text("technical"))), agentOpsRecentRunDetails(locale, run)),
			agentControlActions(locale, "run", run.ID, displayName, run.Revision, run.Actions),
		)
		children = append(children, html.Article(html.Props{Class: "card persona-admin-card agent-running-card", Raw: map[string]any{"data-control-run": run.ID}}, append([]ui.Node{html.H4(html.Props{}, ui.Text(displayName))}, facts...)...))
	}
	children = append(children, RenderAgentRunHistory(locale, finished, AgentRunHistoryFilter{Page: 1}))

	children = append(children, html.H3(html.Props{}, ui.Text(text("schedules"))))
	if snapshot.CanDraft {
		children = append(children, html.A(html.Props{Href: Path(PageAgentOperations) + "?tab=announcements", Class: "button secondary"}, ui.Text(agentUXR7Text(locale, "manage_announcements"))))
	}
	if len(snapshot.Schedules) == 0 {
		children = append(children, html.P(html.Props{Class: "muted"}, ui.Text(text("schedules_empty"))))
	}
	for _, schedule := range snapshot.Schedules {
		name := agentControlDisplayName(locale, "schedule", schedule.Name, schedule.ID)
		items := []ui.Node{html.H4(html.Props{}, ui.Text(name)),
			html.P(html.Props{}, html.Strong(html.Props{}, ui.Text(text("state")+": ")), ui.Text(agentControlLocalizedState(locale, schedule.State))),
			agentControlTechnicalDetails(locale, schedule.ID, []string{"version", "zone", "calendar", "dst", "misfire", "overlap"}, []string{schedule.Version, schedule.Zone, schedule.Calendar, schedule.DST, schedule.Misfire, schedule.Overlap}),
			html.P(html.Props{}, append([]ui.Node{ui.Text(text("occurrences") + ": ")}, agentControlOccurrenceTimes(locale, schedule.Occurrences)...)...)}
		items = append(items, agentControlActions(locale, "schedule", schedule.ID, name, schedule.Revision, schedule.Actions))
		if len(schedule.OccurrenceKeys) > 0 {
			options := []ui.Node{}
			for i, key := range schedule.OccurrenceKeys {
				label := text("occurrence") + " " + locale.FormatNumber(fmt.Sprint(i+1), 0)
				if i < len(schedule.Occurrences) {
					label = agentOperationsFormatInstant(locale, schedule.Occurrences[i])
				}
				options = append(options, html.Option(html.Props{Value: key}, ui.Text(label)))
			}
			items = append(items, html.Label(html.Props{For: "agent-occurrence-" + schedule.ID}, ui.Text(text("occurrence"))), html.Select(html.Props{ID: "agent-occurrence-" + schedule.ID, Raw: map[string]any{"data-owner-occurrence": schedule.ID}}, options...))
		}
		children = append(children, html.Article(html.Props{Class: "card persona-admin-card", Raw: map[string]any{"data-control-schedule": schedule.ID}}, items...))
	}
	children = append(children, html.H3(html.Props{}, ui.Text(text("memory"))))
	if snapshot.CanExport {
		children = append(children, agentControlActions(locale, "memory", "export", agentControlsText(locale, "memory_record"), 0, []string{"export"}))
	}
	if len(snapshot.Memory) == 0 {
		children = append(children, html.P(html.Props{Class: "muted"}, ui.Text(text("memory_empty"))))
	}
	for _, item := range snapshot.Memory {
		name := agentControlDisplayName(locale, "memory", item.Name, item.ID)
		held := text("no")
		if item.Held {
			held = text("yes")
		}
		children = append(children, html.Article(html.Props{Class: "card persona-admin-card", Raw: map[string]any{"data-control-memory": item.ID}},
			html.H4(html.Props{}, ui.Text(name)), agentControlTechnicalDetails(locale, item.ID, []string{"source", "audience", "purpose", "class", "expires", "held"}, []string{item.Source, item.Audience, item.Purpose, item.Class, item.Expires, held}),
			agentControlActions(locale, "memory", item.ID, name, item.Revision, item.Actions)))
	}
	return html.Div(html.Props{Dir: string(locale.Direction), Class: "agent-operations-region-content"}, children...)
}

func agentOperationsFormatInstant(locale LocaleContext, raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2 Jan 2006, 15:04 MST"} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return agentTaskDateTimeLabel(locale, parsed, time.Now())
		}
	}
	if parsed, err := time.Parse("2006-01-02", value); err == nil {
		return ChatDocDateLabel(locale.WithTimeZone("UTC"), parsed, time.Now())
	}
	return value
}

func agentControlLocalizedState(locale LocaleContext, state string) string {
	if localized := agentControlsText(locale, "state_"+strings.ToLower(state)); localized != "" {
		return localized
	}
	return state
}

func agentControlDisplayName(locale LocaleContext, kind, name, id string) string {
	if kind == "run" && (name == "General agent" || id == "general-agent") {
		return locale.Text("agents.general_agent")
	}
	if strings.TrimSpace(name) != "" {
		return name
	}
	return agentControlsText(locale, map[string]string{"schedule": "scheduled_agent", "run": "agent_run", "memory": "memory_record"}[kind])
}

func agentControlNameVersion(locale LocaleContext, name, version string) string {
	if strings.TrimSpace(version) == "" {
		return name
	}
	separator := ", version "
	if locale.Resolved == "de-DE" {
		separator = ", Version "
	} else if locale.Resolved == "ar" {
		separator = "، الإصدار "
	}
	return name + separator + personaAdminLocalizedNumber(locale, version)
}

func agentControlTechnicalDetails(locale LocaleContext, id string, keys, values []string) ui.Node {
	return html.Details(html.Props{Class: "agent-operations-technical", Raw: map[string]any{"data-control-record": id}},
		html.Summary(html.Props{Raw: map[string]any{"data-chevron": "›"}}, ui.Text(agentControlsText(locale, "technical"))),

		agentControlsFacts(locale, keys, values),
	)
}

func agentControlsFacts(locale LocaleContext, keys, values []string) ui.Node {
	items := make([]ui.Node, 0, len(keys)*2)
	for i, key := range keys {
		value := values[i]
		if key == "expires" {
			value = agentOperationsFormatInstant(locale, value)
		}
		if key == "state" || key == "dst" || key == "misfire" || key == "overlap" {
			if localized := agentControlsText(locale, key+"_"+strings.ToLower(value)); localized != "" {
				value = localized
			}
		}
		items = append(items, html.P(html.Props{}, html.Strong(html.Props{}, ui.Text(agentControlsText(locale, key)+": ")), ui.Text(value)))
	}
	return html.Div(html.Props{}, items...)
}

func agentControlActions(locale LocaleContext, kind, id, name string, revision uint64, actions []string) ui.Node {
	items := []ui.Node{}
	for _, action := range actions {
		if !validAgentControlAction(kind, action) {
			continue
		}
		verb := agentControlsText(locale, action)
		if kind == "run" {
			key := map[string]string{"pause": "pause_task", "resume": "resume_task", "stop": "cancel_task"}[action]
			if key != "" {
				verb = agentUXR7Text(locale, key)
			}
		}
		label := verb + " " + name
		raw := map[string]any{"data-owner-action": action, "data-owner-kind": kind, "data-owner-id": id, "data-owner-revision": fmt.Sprint(revision), "data-busy-label": agentControlsText(locale, "working_short")}
		if action != "export" && action != "dry_run" {
			raw["data-owner-confirm"] = label + "?"
			if locale.Resolved == "en-US" {
				raw["data-owner-confirm"] = label + " for everyone?"
			}
		}
		items = append(items, html.Div(html.Props{Class: "persona-admin-editor-field agent-control-action"},
			html.Button(html.Props{Type: "button", Class: "button secondary", Raw: raw}, ui.Text(label)),
			html.Small(html.Props{Class: "muted"}, ui.Text(agentControlsText(locale, "action_help"))),
		))
	}
	return html.Div(html.Props{Class: "persona-admin-actions agent-control-actions"}, items...)
}

func validAgentControlAction(kind, action string) bool {
	switch kind {
	case "schedule":
		return action == "publish" || action == "pause" || action == "skip" || action == "resume" || action == "dry_run" || action == "retire"
	case "run":
		return action == "pause" || action == "resume" || action == "stop" || action == "quarantine"
	case "memory":
		return action == "export" || action == "delete" || action == "revoke" || action == "hold"
	}
	return false
}

func agentControlsNeedsAuditReason(snapshot AgentControlsSnapshot, running []AgentControlRun) bool {
	if snapshot.CanExport {
		return true
	}
	for _, run := range running {
		for _, action := range run.Actions {
			if validAgentControlAction("run", action) {
				return true
			}
		}
	}
	for _, schedule := range snapshot.Schedules {
		for _, action := range schedule.Actions {
			if validAgentControlAction("schedule", action) {
				return true
			}
		}
	}
	for _, memory := range snapshot.Memory {
		for _, action := range memory.Actions {
			if validAgentControlAction("memory", action) {
				return true
			}
		}
	}
	return false
}

func agentControlOccurrenceTimes(locale LocaleContext, instants []string) []ui.Node {
	items := make([]ui.Node, 0, len(instants)*2)
	for i, raw := range instants {
		if i > 0 {
			items = append(items, ui.Text("; "))
		}
		items = append(items, html.Time(html.Props{Raw: map[string]any{"datetime": raw}}, ui.Text(agentOperationsFormatInstant(locale, raw))))
	}
	return items
}
