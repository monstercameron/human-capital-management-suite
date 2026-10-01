package productui

import (
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// PersonaChatAvailability is an explicit server projection. The zero value
// and unavailable value both fail closed; the renderer never fabricates a
// persona, thread, task or approval when the agent service is not ready.
type PersonaChatAvailability string

const (
	PersonaChatAvailable   PersonaChatAvailability = "available"
	PersonaChatUnavailable PersonaChatAvailability = "unavailable"
)

type PersonaThreadReply struct {
	ThreadID     string
	ReplyToPost  string
	ReplyID      string
	AgentName    string
	Body         string
	InvokerID    string
	InvokerLabel string
}

// PersonaProgress is intentionally a status projection rather than a model
// transcript. It is rendered only to the invoker and disappears when ResultReady
// becomes true.
type PersonaProgress struct {
	InvocationID string
	InvokerID    string
	AgentName    string
	Activity     string
	Announcement string
	CurrentStep  int
	TotalSteps   int
	Visible      bool
}

type PersonaChatFailure struct {
	InvocationID string
	InvokerID    string
	Message      string
	Heading      string
	RetryLabel   string
	Retryable    bool
}

// PersonaChatProjection is the typed, server-owned projection consumed by
// chat. Task is the same AgentTask used by the agents page, so the hand-off
// card and task list cannot drift into a second task renderer.
type PersonaChatProjection struct {
	Availability PersonaChatAvailability
	ViewerID     string
	InvokerID    string
	Reply        *PersonaThreadReply
	Progress     *PersonaProgress
	Task         *AgentTask
	TaskRevision string
	Approval     *PersonaApprovalCard
	Failure      *PersonaChatFailure
	ResultReady  bool
}

// RenderPersonaChat renders the thread-owned persona surface. It does not
// render html.Main and it accepts only already-authorized typed projections.
// In particular, invoker-only status, task and approval data are omitted for a
// colleague rather than redacted after rendering.
func RenderPersonaChat(view View, projection PersonaChatProjection, now time.Time) ui.Node {
	locale := view.Locale
	if locale.Resolved == "" {
		locale = ResolveProductLocale(DefaultProductLocale)
	}
	if projection.Availability != PersonaChatAvailable || strings.TrimSpace(projection.ViewerID) == "" || strings.TrimSpace(projection.InvokerID) == "" {
		return personaChatUnavailable(locale)
	}
	children := make([]ui.Node, 0, 5)
	if projection.Reply != nil {
		children = append(children, personaThreadReply(locale, *projection.Reply))
	}
	if projection.ViewerID == projection.InvokerID {
		if projection.Progress != nil && projection.Progress.Visible && !projection.ResultReady && projection.Progress.InvokerID == projection.InvokerID {
			children = append(children, personaProgressStatus(locale, *projection.Progress))
		}
		if projection.Task != nil {
			children = append(children, personaTaskHandoff(view, locale, *projection.Task, projection.TaskRevision))
		}
		if projection.Approval != nil {
			children = append(children, RenderPersonaApprovalCard(view, *projection.Approval, now))
		}
		if projection.Failure != nil && projection.Failure.InvokerID == projection.InvokerID {
			children = append(children, personaFailure(locale, *projection.Failure))
		}
	}
	if len(children) == 0 {
		return personaChatUnavailable(locale)
	}
	return html.Section(html.Props{Class: "persona-chat-surface", Role: "region", Aria: map[string]string{"label": locale.Text("agents.threads")}}, children...)
}

func personaChatUnavailable(locale LocaleContext) ui.Node {
	return html.Section(html.Props{Class: "persona-chat-unavailable", Role: "status", Aria: map[string]string{"live": "polite"}},
		html.Strong(html.Props{}, ui.Text(locale.Text("agents.unavailable_title"))),
		html.P(html.Props{}, ui.Text(locale.Text("agents.unavailable_detail"))))
}

func personaThreadReply(locale LocaleContext, reply PersonaThreadReply) ui.Node {
	agent := strings.TrimSpace(reply.AgentName)
	if agent == "" {
		agent = locale.Text("agents.agent_identity")
	}
	children := []ui.Node{html.Strong(html.Props{Class: "persona-thread-agent"}, ui.Text(agent)), html.Span(html.Props{Class: "agents-acting-for"}, ui.Text(locale.Text("agents.acting_for_you")))}
	if reply.Body != "" {
		children = append(children, html.P(html.Props{Class: "persona-thread-body"}, ui.Text(reply.Body)))
	}
	return html.Article(html.Props{Class: "persona-thread-reply", Data: map[string]string{"agent-thread-id": reply.ThreadID, "agent-reply-to": reply.ReplyToPost, "agent-reply-id": reply.ReplyID}}, children...)
}

func personaProgressStatus(locale LocaleContext, progress PersonaProgress) ui.Node {
	agent := strings.TrimSpace(progress.AgentName)
	if agent == "" {
		agent = locale.Text("agents.agent_identity")
	}
	activity := strings.TrimSpace(progress.Activity)
	if activity == "" {
		activity = locale.Text("agents.live_step")
	}
	current, total := progress.CurrentStep, progress.TotalSteps
	if current < 1 {
		current = 1
	}
	if total < current {
		total = current
	}
	label := progress.Announcement
	if strings.TrimSpace(label) == "" {
		label = locale.Text("agents.live_step") + ": " + agent + " — " + activity + " (" + formatPersonaNumber(current) + "/" + formatPersonaNumber(total) + ")"
	}
	return html.Div(html.Props{Class: "persona-chat-progress", Role: "status", Aria: map[string]string{"live": "polite", "atomic": "true"}, Data: map[string]string{"agent-progress": "true", "agent-invocation-id": progress.InvocationID, "reduced-motion": "respect"}}, ui.Text(label))
}

func personaTaskHandoff(view View, locale LocaleContext, task AgentTask, revision string) ui.Node {
	// tagAgentTasks is the agents page's canonical task renderer. The wrapper
	// adds only thread identity and revision metadata for in-place updates.
	return html.Section(html.Props{Class: "persona-task-handoff", Role: "region", Aria: map[string]string{"label": locale.Text("agents.tasks")}, Data: map[string]string{"agent-task-card": task.ID, "agent-task-revision": revision}},
		tagAgentTasks(view, locale, AgentSnapshot{Availability: AgentsAvailable, Tasks: []AgentTask{task}}))
}

func personaFailure(locale LocaleContext, failure PersonaChatFailure) ui.Node {
	heading := failure.Heading
	if heading == "" {
		heading = locale.Text("agents.unavailable_title")
	}
	children := []ui.Node{html.Strong(html.Props{}, ui.Text(heading))}
	if failure.Message != "" {
		children = append(children, html.P(html.Props{}, ui.Text(failure.Message)))
	}
	if failure.Retryable {
		retryLabel := failure.RetryLabel
		if retryLabel == "" {
			retryLabel = locale.Text("agents.start_task")
		}
		children = append(children, html.Button(html.Props{Class: "button secondary persona-retry", Type: "button", Data: map[string]string{"agent-action": "retry", "agent-invocation-id": failure.InvocationID}, Aria: map[string]string{"label": retryLabel}}, ui.Text(retryLabel)))
	}
	return html.Div(html.Props{Class: "persona-chat-failure", Role: "alert", Aria: map[string]string{"live": "assertive"}, Data: map[string]string{"agent-failure": "typed"}}, children...)
}

func formatPersonaNumber(value int) string {
	return strconv.Itoa(value)
}
