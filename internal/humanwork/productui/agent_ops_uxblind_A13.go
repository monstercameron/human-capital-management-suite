package productui

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// AgentOpsAudience is resolved by the server before this projection reaches
// the product UI. The component never infers an audience from a display name
// or from the contents of a task.
type AgentOpsAudience string

const (
	AgentOpsAudienceUser     AgentOpsAudience = "user"
	AgentOpsAudienceOwner    AgentOpsAudience = "owner"
	AgentOpsAudienceOperator AgentOpsAudience = "operator"
)

type AgentOpsTaskStatus string

const (
	AgentOpsTaskRunning   AgentOpsTaskStatus = "running"
	AgentOpsTaskWaiting   AgentOpsTaskStatus = "waiting"
	AgentOpsTaskPaused    AgentOpsTaskStatus = "paused"
	AgentOpsTaskFailed    AgentOpsTaskStatus = "failed"
	AgentOpsTaskCompleted AgentOpsTaskStatus = "completed"
)

const AgentOpsDefaultStallAfter = 5 * time.Minute

// AgentOpsStep is structured telemetry only. It deliberately has no prompt,
// result, document, draft, argument, or goal field.
type AgentOpsStep struct {
	StepID        string
	Name          string
	LatencyMillis int64
	Retries       int
	DenialReason  string
	WakeLagMillis int64
	SpendCents    int64
	StartedAt     time.Time
}

// AgentOpsTelemetryEvent is the content-free structured event emitted by the
// task projection boundary. The task and tenant identifiers stay attached to
// every step metric; prompt, result, document and draft content have no field
// in this event.
type AgentOpsTelemetryEvent struct {
	TenantID      string
	TaskID        string
	StepID        string
	StepName      string
	LatencyMillis int64
	Retries       int
	DenialReason  string
	WakeLagMillis int64
	SpendCents    int64
	StartedAt     time.Time
}

// AgentOpsTelemetryEvents converts structured step telemetry without carrying
// the task's private goal, documents or drafts into the operational stream.
func AgentOpsTelemetryEvents(task AgentOpsTask) []AgentOpsTelemetryEvent {
	events := make([]AgentOpsTelemetryEvent, 0, len(task.Steps))
	for _, step := range task.Steps {
		events = append(events, AgentOpsTelemetryEvent{
			TenantID: task.TenantID, TaskID: task.TaskID, StepID: step.StepID,
			StepName: step.Name, LatencyMillis: step.LatencyMillis, Retries: step.Retries,
			DenialReason: step.DenialReason, WakeLagMillis: step.WakeLagMillis,
			SpendCents: step.SpendCents, StartedAt: step.StartedAt,
		})
	}
	return events
}

// AgentOpsTask is the server-side input projection. Goal, Documents and
// Drafts are accepted only so callers can prove that redaction happens at
// this boundary; they are never copied into AgentOpsTaskView or telemetry.
type AgentOpsTask struct {
	TenantID       string
	TaskID         string
	UserID         string
	OwnerID        string
	AgentVersion   string
	Status         AgentOpsTaskStatus
	LastActivityAt time.Time
	LoopCount      int
	BudgetCents    int64
	SpendCents     int64
	Goal           string
	Documents      []string
	Drafts         []string
	Steps          []AgentOpsStep
}

type AgentOpsViewer struct {
	Audience AgentOpsAudience
	ID       string
	TenantID string
}

type AgentOpsStepView struct {
	StepID        string
	Name          string
	LatencyMillis int64
	Retries       int
	DenialReason  string
	WakeLagMillis int64
	SpendCents    int64
}

type AgentOpsTaskView struct {
	TenantID      string
	TaskID        string
	UserID        string
	AgentVersion  string
	Status        AgentOpsTaskStatus
	StepCount     int
	LatencyMillis int64
	Retries       int
	Denials       int
	WakeLagMillis int64
	SpendCents    int64
	BudgetCents   int64
	Stalled       bool
	Looping       bool
	OverBudget    bool
	AlertReasons  []string
	Trace         []AgentOpsStepView
	PauseAllowed  bool
}

type AgentOpsSummary struct {
	TaskCount     int
	FlaggedCount  int
	StepCount     int
	Retries       int
	Denials       int
	SpendCents    int64
	WakeLagMillis int64
}

type AgentOpsProjection struct {
	Audience AgentOpsAudience
	Tasks    []AgentOpsTaskView
	Summary  AgentOpsSummary
}

// ProjectAgentOps applies the audience boundary and returns a safe
// presentation model. User views contain their own step metadata; owner views
// contain aggregates; operator views contain only flagged tasks and safe
// operational identifiers. No branch copies task content.
func ProjectAgentOps(tasks []AgentOpsTask, viewer AgentOpsViewer, now time.Time) AgentOpsProjection {
	projection := AgentOpsProjection{Audience: viewer.Audience}
	for _, task := range tasks {
		if !agentOpsViewerMaySeeTask(task, viewer) {
			continue
		}
		alerts := agentOpsAlerts(task, now)
		if viewer.Audience == AgentOpsAudienceOperator && len(alerts) == 0 {
			continue
		}
		view := agentOpsTaskView(task, viewer.Audience, alerts)
		projection.Tasks = append(projection.Tasks, view)
		projection.Summary.TaskCount++
		if len(alerts) > 0 {
			projection.Summary.FlaggedCount++
		}
		projection.Summary.StepCount += view.StepCount
		projection.Summary.Retries += view.Retries
		projection.Summary.Denials += view.Denials
		projection.Summary.SpendCents += view.SpendCents
		projection.Summary.WakeLagMillis += view.WakeLagMillis
	}
	return projection
}

func agentOpsViewerMaySeeTask(task AgentOpsTask, viewer AgentOpsViewer) bool {
	if strings.TrimSpace(viewer.ID) == "" || (viewer.TenantID != "" && viewer.TenantID != task.TenantID) {
		return false
	}
	switch viewer.Audience {
	case AgentOpsAudienceUser:
		return task.UserID == viewer.ID
	case AgentOpsAudienceOwner:
		return task.OwnerID == viewer.ID
	case AgentOpsAudienceOperator:
		return true
	default:
		return false
	}
}

func agentOpsTaskView(task AgentOpsTask, audience AgentOpsAudience, alerts []string) AgentOpsTaskView {
	view := AgentOpsTaskView{
		TenantID:     task.TenantID,
		TaskID:       task.TaskID,
		AgentVersion: task.AgentVersion,
		Status:       task.Status,
		StepCount:    len(task.Steps),
		SpendCents:   task.SpendCents,
		BudgetCents:  task.BudgetCents,
		AlertReasons: append([]string(nil), alerts...),
		Stalled:      containsAgentOpsAlert(alerts, "stalled"),
		Looping:      containsAgentOpsAlert(alerts, "looping"),
		OverBudget:   containsAgentOpsAlert(alerts, "over-budget"),
	}
	if audience == AgentOpsAudienceUser {
		view.UserID = task.UserID
		view.Trace = make([]AgentOpsStepView, 0, len(task.Steps))
	}
	var stepSpendCents int64
	for _, step := range task.Steps {
		view.LatencyMillis += step.LatencyMillis
		view.Retries += step.Retries
		view.WakeLagMillis += step.WakeLagMillis
		stepSpendCents += step.SpendCents
		if step.DenialReason != "" {
			view.Denials++
		}
		if audience == AgentOpsAudienceUser {
			view.Trace = append(view.Trace, AgentOpsStepView{
				StepID: step.StepID, Name: step.Name, LatencyMillis: step.LatencyMillis,
				Retries: step.Retries, DenialReason: step.DenialReason,
				WakeLagMillis: step.WakeLagMillis, SpendCents: step.SpendCents,
			})
		}
	}
	if view.SpendCents == 0 {
		view.SpendCents = stepSpendCents
	}
	view.PauseAllowed = audience == AgentOpsAudienceOperator && len(alerts) > 0
	return view
}

func agentOpsAlerts(task AgentOpsTask, now time.Time) []string {
	alerts := make([]string, 0, 3)
	if (task.Status == AgentOpsTaskRunning || task.Status == AgentOpsTaskWaiting) && !task.LastActivityAt.IsZero() && now.Sub(task.LastActivityAt) >= AgentOpsDefaultStallAfter {
		alerts = append(alerts, "stalled")
	}
	if task.LoopCount >= 3 {
		alerts = append(alerts, "looping")
	}
	if task.BudgetCents > 0 && task.SpendCents >= task.BudgetCents {
		alerts = append(alerts, "over-budget")
	}
	return alerts
}

func containsAgentOpsAlert(alerts []string, want string) bool {
	for _, alert := range alerts {
		if alert == want {
			return true
		}
	}
	return false
}

// String is a deterministic, content-free diagnostic representation useful
// for logs and tests. It is intentionally built from the already-redacted
// projection rather than from AgentOpsTask.
func (projection AgentOpsProjection) String() string {
	var b strings.Builder
	b.WriteString(string(projection.Audience))
	for _, task := range projection.Tasks {
		b.WriteByte('|')
		b.WriteString(task.TenantID)
		b.WriteByte(':')
		b.WriteString(task.TaskID)
		b.WriteByte(':')
		b.WriteString(string(task.Status))
		b.WriteByte(':')
		b.WriteString(strings.Join(task.AlertReasons, ","))
		b.WriteByte(':')
		b.WriteString(strconv.FormatInt(task.SpendCents, 10))
	}
	return b.String()
}

var ErrAgentOpsUnauthorized = errors.New("agent operations: unauthorized")

type AgentOpsAuditRecord struct {
	EventType string
	TenantID  string
	TaskID    string
	ActorID   string
	Reason    string
	At        time.Time
}

type AgentOpsPauseCommand struct {
	TenantID string
	TaskID   string
	ActorID  string
	Reason   string
	Audit    AgentOpsAuditRecord
}

// AgentOpsPauseController is the application boundary. Its implementation
// must persist the pause and audit record atomically; the UI package only
// supplies the already-authorized, content-free command.
type AgentOpsPauseController interface {
	Pause(context.Context, AgentOpsPauseCommand) error
}

func PauseAgentOpsTask(ctx context.Context, viewer AgentOpsViewer, task AgentOpsTask, reason string, now time.Time, controller AgentOpsPauseController) error {
	if viewer.Audience != AgentOpsAudienceOperator || strings.TrimSpace(viewer.ID) == "" || (viewer.TenantID != "" && viewer.TenantID != task.TenantID) || len(agentOpsAlerts(task, now)) == 0 || controller == nil {
		return ErrAgentOpsUnauthorized
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "operator requested pause"
	}
	return controller.Pause(ctx, AgentOpsPauseCommand{
		TenantID: task.TenantID,
		TaskID:   task.TaskID,
		ActorID:  viewer.ID,
		Reason:   reason,
		Audit: AgentOpsAuditRecord{
			EventType: "agent.task.paused",
			TenantID:  task.TenantID,
			TaskID:    task.TaskID,
			ActorID:   viewer.ID,
			Reason:    reason,
			At:        now,
		},
	})
}

type AgentOpsCopy struct {
	Title       string
	Description string
	Empty       string
	Tenant      string
	Task        string
	Step        string
	Name        string
	Status      string
	Spend       string
	Alerts      string
	Trace       string
	Pause       string
	Latency     string
	Retries     string
	Denials     string
	WakeLag     string
}

type AgentOpsDashboardProps struct {
	Projection AgentOpsProjection
	Copy       AgentOpsCopy
	OnPause    func(string)
}

func (copy AgentOpsCopy) withDefaults() AgentOpsCopy {
	defaults := AgentOpsCopy{
		Title: "Agent task operations", Description: "Structured task health without private task content.",
		Empty: "No tasks require attention.", Tenant: "Tenant", Task: "Task", Step: "Step", Name: "Name", Status: "Status", Spend: "Spend",
		Alerts: "Alerts", Trace: "Step trace", Pause: "Pause task", Latency: "Latency",
		Retries: "Retries", Denials: "Denials", WakeLag: "Wake lag",
	}
	if copy.Title == "" {
		copy.Title = defaults.Title
	}
	if copy.Description == "" {
		copy.Description = defaults.Description
	}
	if copy.Empty == "" {
		copy.Empty = defaults.Empty
	}
	if copy.Tenant == "" {
		copy.Tenant = defaults.Tenant
	}
	if copy.Task == "" {
		copy.Task = defaults.Task
	}
	if copy.Step == "" {
		copy.Step = defaults.Step
	}
	if copy.Name == "" {
		copy.Name = defaults.Name
	}
	if copy.Status == "" {
		copy.Status = defaults.Status
	}
	if copy.Spend == "" {
		copy.Spend = defaults.Spend
	}
	if copy.Alerts == "" {
		copy.Alerts = defaults.Alerts
	}
	if copy.Trace == "" {
		copy.Trace = defaults.Trace
	}
	if copy.Pause == "" {
		copy.Pause = defaults.Pause
	}
	if copy.Latency == "" {
		copy.Latency = defaults.Latency
	}
	if copy.Retries == "" {
		copy.Retries = defaults.Retries
	}
	if copy.Denials == "" {
		copy.Denials = defaults.Denials
	}
	if copy.WakeLag == "" {
		copy.WakeLag = defaults.WakeLag
	}
	return copy
}

// AgentOpsDashboard renders only the safe projection. Copy is supplied by the
// locale-aware caller so this component does not create a second catalogue.
func AgentOpsDashboard(props AgentOpsDashboardProps) ui.Node {
	copy := props.Copy.withDefaults()
	rows := make([]ui.Node, 0, len(props.Projection.Tasks))
	for _, task := range props.Projection.Tasks {
		alerts := strings.Join(task.AlertReasons, ", ")
		action := ui.Node(nil)
		if props.OnPause != nil && task.PauseAllowed {
			taskID := task.TaskID
			action = html.Button(html.Props{Class: "button secondary", Type: "button", Data: map[string]string{"agent-ops-action": "pause", "agent-task-id": taskID}, OnClick: ui.UseEvent(func(ui.MouseEvent) { props.OnPause(taskID) })}, ui.Text(copy.Pause))
		}
		cells := []ui.Node{
			html.Td(html.Props{}, ui.Text(task.TenantID)),
			html.Th(html.Props{Raw: map[string]any{"scope": "row"}}, ui.Text(task.TaskID)),
			html.Td(html.Props{}, ui.Text(string(task.Status))),
			html.Td(html.Props{}, ui.Text(strconv.FormatInt(task.SpendCents, 10))),
			html.Td(html.Props{}, ui.Text(alerts)),
			html.Td(html.Props{}, ui.Text(strconv.FormatInt(task.LatencyMillis, 10))),
			html.Td(html.Props{}, ui.Text(strconv.Itoa(task.Retries))),
			html.Td(html.Props{}, ui.Text(strconv.Itoa(task.Denials))),
			html.Td(html.Props{}, ui.Text(strconv.FormatInt(task.WakeLagMillis, 10))),
			html.Td(html.Props{}, action),
		}
		row := html.Tr(html.Props{Data: map[string]string{"agent-task-id": task.TaskID, "agent-tenant-id": task.TenantID}}, cells...)
		rows = append(rows, row)
		if props.Projection.Audience == AgentOpsAudienceUser && len(task.Trace) > 0 {
			rows = append(rows, html.Tr(html.Props{Data: map[string]string{"agent-task-id": task.TaskID, "agent-tenant-id": task.TenantID}},
				html.Td(html.Props{ColSpan: 10}, agentOpsTrace(task, copy))))
		}
	}
	if len(rows) == 0 {
		return html.Section(html.Props{Class: "agent-ops-dashboard", Aria: map[string]string{"label": copy.Title}}, html.H2(html.Props{}, ui.Text(copy.Title)), html.P(html.Props{Class: "muted"}, ui.Text(copy.Empty)))
	}
	return html.Section(html.Props{Class: "agent-ops-dashboard", Aria: map[string]string{"label": copy.Title}},
		html.H2(html.Props{}, ui.Text(copy.Title)),
		html.P(html.Props{Class: "muted"}, ui.Text(copy.Description)),
		html.Table(html.Props{Class: "data-table agent-ops-table"},
			html.Thead(html.Props{}, html.Tr(html.Props{},
				html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, ui.Text(copy.Tenant)),
				html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, ui.Text(copy.Task)),
				html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, ui.Text(copy.Status)),
				html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, ui.Text(copy.Spend)),
				html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, ui.Text(copy.Alerts)),
				html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, ui.Text(copy.Latency)),
				html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, ui.Text(copy.Retries)),
				html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, ui.Text(copy.Denials)),
				html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, ui.Text(copy.WakeLag)),
				html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, ui.Text(copy.Pause)),
			)),
			html.Tbody(html.Props{}, rows...),
		),
	)
}

func agentOpsTrace(task AgentOpsTaskView, copy AgentOpsCopy) ui.Node {
	rows := make([]ui.Node, 0, len(task.Trace))
	for _, step := range task.Trace {
		rows = append(rows, html.Tr(html.Props{Data: map[string]string{"agent-step-id": step.StepID}},
			html.Th(html.Props{Raw: map[string]any{"scope": "row"}}, ui.Text(step.StepID)),
			html.Td(html.Props{}, ui.Text(step.Name)),
			html.Td(html.Props{}, ui.Text(strconv.FormatInt(step.LatencyMillis, 10))),
			html.Td(html.Props{}, ui.Text(strconv.Itoa(step.Retries))),
			html.Td(html.Props{}, ui.Text(step.DenialReason)),
			html.Td(html.Props{}, ui.Text(strconv.FormatInt(step.WakeLagMillis, 10))),
			html.Td(html.Props{}, ui.Text(strconv.FormatInt(step.SpendCents, 10))),
		))
	}
	return html.Details(html.Props{Class: "agent-ops-trace"},
		html.Summary(html.Props{}, ui.Text(copy.Trace)),
		html.Table(html.Props{Class: "data-table agent-ops-trace-table"},
			html.Thead(html.Props{}, html.Tr(html.Props{},
				html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, ui.Text(copy.Step)),
				html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, ui.Text(copy.Name)),
				html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, ui.Text(copy.Latency)),
				html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, ui.Text(copy.Retries)),
				html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, ui.Text(copy.Denials)),
				html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, ui.Text(copy.WakeLag)),
				html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, ui.Text(copy.Spend)),
			)),
			html.Tbody(html.Props{}, rows...),
		),
	)
}
