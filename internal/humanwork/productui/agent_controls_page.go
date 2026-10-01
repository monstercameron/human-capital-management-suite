package productui

import (
	"fmt"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func AgentControlsMount(locale LocaleContext) ui.Node {
	return html.Section(html.Props{ID: "agent-controls", Dir: string(locale.Direction), Raw: map[string]any{"data-locale": locale.Resolved}, Aria: map[string]string{"labelledby": "agent-controls-title"}},
		html.H2(html.Props{ID: "agent-controls-title"}, ui.Text(agentControlsText(locale, "title"))),
		html.P(html.Props{Role: "status"}, ui.Text(agentControlsText(locale, "loading"))))
}

// RenderAgentControls renders text as text nodes, including source references.
// It never consumes private prompts or memory bodies.
func RenderAgentControls(locale LocaleContext, snapshot AgentControlsSnapshot, message string) ui.Node {
	text := func(key string) string { return agentControlsText(locale, key) }
	children := []ui.Node{html.H2(html.Props{ID: "agent-controls-title"}, ui.Text(text("title"))),
		html.P(html.Props{ID: "agent-controls-status", Role: "status", Raw: map[string]any{"tabindex": "-1", "data-msg-invalid": text("invalid")}, Aria: map[string]string{"live": "polite", "atomic": "true"}}, ui.Text(text(message))),
		html.Button(html.Props{Type: "button", Raw: map[string]any{"data-owner-refresh": "true"}}, ui.Text(text("refresh")))}
	if !snapshot.Available {
		if message != "denied" {
			children = append(children, html.P(html.Props{}, ui.Text(text("unavailable"))))
		}
		return html.Div(html.Props{Dir: string(locale.Direction)}, children...)
	}
	children = append(children, html.H3(html.Props{}, ui.Text(text("schedules"))))
	children = append(children, html.Label(html.Props{For: "agent-controls-reason"}, ui.Text(text("reason"))), html.Input(html.Props{ID: "agent-controls-reason", Type: "text", Aria: map[string]string{"describedby": "agent-controls-status"}}),
		html.Label(html.Props{For: "agent-controls-incident"}, ui.Text(text("incident"))), html.Input(html.Props{ID: "agent-controls-incident", Type: "text"}))
	if snapshot.CanDraft {
		children = append(children, agentScheduleForm(locale))
	}
	if len(snapshot.Schedules) == 0 {
		children = append(children, html.P(html.Props{}, ui.Text(text("empty"))))
	}
	for _, schedule := range snapshot.Schedules {
		items := []ui.Node{html.H4(html.Props{}, ui.Text(schedule.ID)),
			agentControlsFacts(locale, []string{"state", "version", "installation", "recurrence", "zone", "calendar", "dst", "destination", "misfire", "overlap", "budget"}, []string{schedule.State, schedule.Version, schedule.Installation, schedule.Recurrence, schedule.Zone, schedule.Calendar, schedule.DST, schedule.Destination, schedule.Misfire, schedule.Overlap, schedule.Budget}),
			html.P(html.Props{}, ui.Text(text("occurrences")+": "+strings.Join(schedule.Occurrences, "; ")))}
		items = append(items, agentControlActions(locale, "schedule", schedule.ID, schedule.Revision, schedule.Actions))
		if len(schedule.OccurrenceKeys) > 0 {
			options := []ui.Node{}
			for i, key := range schedule.OccurrenceKeys {
				label := key
				if i < len(schedule.Occurrences) {
					label = schedule.Occurrences[i]
				}
				options = append(options, html.Option(html.Props{Value: key}, ui.Text(label)))
			}
			items = append(items, html.Label(html.Props{For: "agent-occurrence-" + schedule.ID}, ui.Text(text("occurrence"))), html.Select(html.Props{ID: "agent-occurrence-" + schedule.ID, Raw: map[string]any{"data-owner-occurrence": schedule.ID}}, options...))
		}
		children = append(children, html.Article(html.Props{Class: "agents-task-card", Raw: map[string]any{"data-control-schedule": schedule.ID}}, items...))
	}
	children = append(children, html.H3(html.Props{}, ui.Text(text("runs"))))
	if len(snapshot.Runs) == 0 {
		children = append(children, html.P(html.Props{}, ui.Text(text("empty"))))
	}
	for _, run := range snapshot.Runs {
		children = append(children, html.Article(html.Props{Class: "agents-task-card", Raw: map[string]any{"data-control-run": run.ID}},
			html.H4(html.Props{}, ui.Text(run.ID)),
			agentControlsFacts(locale, []string{"state", "version", "installation", "cause", "lag", "spend", "failure", "denials", "citations", "evals", "incident"}, []string{run.State, run.Version, run.Installation, run.Cause, run.QueueLag, run.Spend, run.Failure, strings.Join(run.Denials, "; "), strings.Join(run.Citations, "; "), strings.Join(run.Evals, "; "), run.Incident}),
			agentControlActions(locale, "run", run.ID, run.Revision, run.Actions)))
	}
	children = append(children, html.H3(html.Props{}, ui.Text(text("memory"))))
	if snapshot.CanExport {
		children = append(children, agentControlActions(locale, "memory", "export", 0, []string{"export"}))
	}
	if len(snapshot.Memory) == 0 {
		children = append(children, html.P(html.Props{}, ui.Text(text("empty"))))
	}
	for _, item := range snapshot.Memory {
		held := text("no")
		if item.Held {
			held = text("yes")
		}
		children = append(children, html.Article(html.Props{Class: "agents-task-card", Raw: map[string]any{"data-control-memory": item.ID}},
			html.H4(html.Props{}, ui.Text(item.ID)), agentControlsFacts(locale, []string{"source", "audience", "purpose", "class", "expires", "held"}, []string{item.Source, item.Audience, item.Purpose, item.Class, item.Expires, held}),
			agentControlActions(locale, "memory", item.ID, item.Revision, item.Actions)))
	}
	return html.Div(html.Props{Dir: string(locale.Direction), Style: map[string]string{"overflow-wrap": "anywhere", "min-width": "0"}}, children...)
}

func agentControlsFacts(locale LocaleContext, keys, values []string) ui.Node {
	items := make([]ui.Node, 0, len(keys)*2)
	for i, key := range keys {
		value := values[i]
		if key == "state" || key == "dst" || key == "misfire" || key == "overlap" {
			if localized := agentControlsText(locale, key+"_"+strings.ToLower(value)); localized != "" {
				value = localized
			}
		}
		items = append(items, html.P(html.Props{}, html.Strong(html.Props{}, ui.Text(agentControlsText(locale, key)+": ")), ui.Text(value)))
	}
	return html.Div(html.Props{}, items...)
}

func agentControlActions(locale LocaleContext, kind, id string, revision uint64, actions []string) ui.Node {
	items := []ui.Node{}
	for _, action := range actions {
		if !validAgentControlAction(kind, action) {
			continue
		}
		items = append(items, html.Button(html.Props{Type: "button", Class: "button-secondary", Raw: map[string]any{"data-owner-action": action, "data-owner-kind": kind, "data-owner-id": id, "data-owner-revision": fmt.Sprint(revision)}}, ui.Text(agentControlsText(locale, action))))
	}
	return html.Div(html.Props{Class: "agents-task-actions", Style: map[string]string{"display": "flex", "flex-wrap": "wrap", "gap": "0.5rem"}}, items...)
}

func validAgentControlAction(kind, action string) bool {
	switch kind {
	case "schedule":
		return action == "publish" || action == "pause" || action == "skip" || action == "resume" || action == "dry_run" || action == "retire"
	case "run":
		return action == "pause" || action == "quarantine"
	case "memory":
		return action == "export" || action == "delete" || action == "revoke" || action == "hold"
	}
	return false
}

func agentScheduleForm(locale LocaleContext) ui.Node {
	items := []ui.Node{}
	for _, key := range []string{"id", "revision", "version", "installation", "recurrence", "zone", "calendar", "destination", "max_cost", "max_input", "max_output"} {
		id := "agent-schedule-" + key
		inputType := "text"
		if key == "revision" || strings.HasPrefix(key, "max_") {
			inputType = "number"
		}
		items = append(items, html.Div(html.Props{}, html.Label(html.Props{For: id}, ui.Text(agentControlsText(locale, key))), html.Input(html.Props{ID: id, Name: key, Type: inputType, Aria: map[string]string{"describedby": "agent-controls-status"}, Style: map[string]string{"max-width": "100%", "box-sizing": "border-box"}})))
	}
	for _, field := range []struct {
		key     string
		choices []string
	}{{"misfire", []string{"SKIP", "CATCH_UP_ONCE", "REVIEW"}}, {"overlap", []string{"SKIP", "QUEUE", "REFUSE"}}, {"dst", []string{"REJECT", "EARLIER", "LATER"}}} {
		options := []ui.Node{}
		for _, choice := range field.choices {
			options = append(options, html.Option(html.Props{Value: choice}, ui.Text(agentControlsText(locale, field.key+"_"+strings.ToLower(choice)))))
		}
		items = append(items, html.Label(html.Props{For: "agent-schedule-" + field.key}, ui.Text(agentControlsText(locale, field.key))), html.Select(html.Props{ID: "agent-schedule-" + field.key}, options...))
	}
	items = append(items, html.P(html.Props{ID: "agent-schedule-help"}, ui.Text(agentControlsText(locale, "draft_help"))),
		html.Button(html.Props{Type: "button", Raw: map[string]any{"data-owner-draft": "preview"}}, ui.Text(agentControlsText(locale, "preview"))),
		html.Button(html.Props{Type: "button", Raw: map[string]any{"data-owner-draft": "draft"}}, ui.Text(agentControlsText(locale, "draft"))))
	return html.Fieldset(html.Props{ID: "agent-schedule-draft", Style: map[string]string{"min-inline-size": "0"}, Aria: map[string]string{"describedby": "agent-schedule-help"}}, append([]ui.Node{html.Legend(html.Props{}, ui.Text(agentControlsText(locale, "draft")))}, items...)...)
}
