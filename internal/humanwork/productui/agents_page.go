package productui

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// PageAgents is the personal agent workspace nested under Chat. The runtime
// client is deliberately an interface: the page can ship before the agent
// service is published and still has a truthful, accessible unavailable state.
const PageAgents PageID = "agents"

type AgentsAvailability string

const (
	AgentsAvailable   AgentsAvailability = "available"
	AgentsUnavailable AgentsAvailability = "unavailable"
)

type AgentClient interface {
	Snapshot(context.Context, AgentSnapshotRequest) (AgentSnapshot, error)
}

type AgentSnapshotRequest struct {
	TenantID  string
	Principal string
}

type AgentSnapshot struct {
	Availability           AgentsAvailability
	StartAvailable         bool
	StartUnavailableReason string
	Agents                 []AgentSummary
	Threads                []AgentThread
	Tasks                  []AgentTask
	SelectedTask           *AgentTask
}

type AgentSummary struct {
	ID          string
	Name        string
	Description string
	Status      string
	Skills      []string
}

type AgentThread struct {
	ID      string
	AgentID string
	Title   string
	Posts   []AgentPost
}

type AgentPost struct {
	ID      string
	Author  string
	Body    string
	At      string
	Skills  []string
	ForUser bool
}

type AgentTaskState string

const (
	AgentTaskRunning                  AgentTaskState = "running"
	AgentTaskAwaitingApproval         AgentTaskState = "awaiting_approval"
	AgentTaskAwaitingInput            AgentTaskState = "awaiting_input"
	AgentTaskDrafting                 AgentTaskState = "drafting"
	AgentTaskAwaitingPlanConfirmation AgentTaskState = "awaiting_plan_confirmation"
	AgentTaskWaiting                  AgentTaskState = "waiting"
	AgentTaskPaused                   AgentTaskState = "paused"
	AgentTaskCompleted                AgentTaskState = "completed"
	AgentTaskFailed                   AgentTaskState = "failed"
	AgentTaskCancelled                AgentTaskState = "cancelled"
	AgentTaskExpired                  AgentTaskState = "expired"
	AgentTaskUnknown                  AgentTaskState = "unknown"
)

type AgentTask struct {
	ID               string
	Version          uint64
	AgentID          string
	Title            string
	Goal             string
	AnswerText       string
	State            AgentTaskState
	LiveStep         string
	BudgetUsed       string
	BudgetLimit      string
	PlanRevision     string
	PlanDiff         string
	Steps            []AgentTaskStep
	Checkpoints      []AgentCheckpoint
	Artifacts        []AgentArtifact
	Approvals        []AgentApproval
	SubmittedIntents []AgentIntentStatus
	Actions          AgentTaskActionPolicy
}

// AgentTaskActionPolicy is the server-derived affordance projection for one
// task. The control RPC rechecks every action and version before mutation.
type AgentTaskActionPolicy struct {
	ConfirmPlan bool
	Pause       bool
	Resume      bool
	Cancel      bool
}

type AgentTaskStep struct {
	Name   string
	State  string
	Tier   string
	Detail string
}

type AgentCheckpoint struct {
	Label string
	At    string
}

type AgentArtifact struct {
	Name string
	Kind string
	Href string
}

type AgentApproval struct {
	ID      string
	Digest  string
	Sources []string
	Taint   string
	Summary string
}

type AgentIntentStatus struct {
	Name   string
	Status string
}

type agentsPageModuleRenderer struct{}

// Render resolves the route through the viewer's agents availability
// projection (UXBLIND-122), the same answer that decides the navigation entry.
func (agentsPageModuleRenderer) Render(view View) ui.Node {
	return BuildAgentsSurface(view)
}

// BuildAgentsPage renders one AgentClient answer. A nil client (component
// previews, a view without a server projection) shows the truthful
// unavailable state; it never fabricates agents or tasks.
func BuildAgentsPage(view View, client AgentClient) ui.Node {
	locale := view.Locale
	if locale.Resolved == "" {
		locale = ResolveProductLocale(DefaultProductLocale)
	}
	if client == nil {
		return agentsUnavailablePage(view, locale)
	}
	snapshot, err := client.Snapshot(context.Background(), AgentSnapshotRequest{TenantID: view.Tenant, Principal: view.Principal})
	// Availability is an explicit server projection. Treat the zero value as
	// unavailable too: a partially populated response must never make private
	// agent data appear available by accident.
	if err != nil || snapshot.Availability != AgentsAvailable {
		return agentsUnavailablePage(view, locale)
	}
	return renderAgentsPage(view, locale, snapshot)
}

func agentsUnavailablePage(view View, locale LocaleContext) ui.Node {
	return html.Section(html.Props{Class: "agents-page agents-page-unavailable", Aria: map[string]string{"labelledby": "agents-page-title"}},
		html.H1(html.Props{ID: "agents-page-title"}, ui.Text(locale.Text("agents.page_title"))),
		html.P(html.Props{Class: "agents-page-subtitle"}, ui.Text(locale.Text("agents.page_subtitle"))),
		ui.CreateElement(EmptyState, EmptyStateProps{
			Title: locale.Text("agents.unavailable_title"), Description: locale.Text("agents.unavailable_detail"), Role: "status", Class: "agents-unavailable",
		}),
	)
}

func renderAgentsPage(view View, locale LocaleContext, snapshot AgentSnapshot) ui.Node {
	var selectedTask ui.Node
	if snapshot.SelectedTask != nil {
		selectedTask = tagAgentTaskView(view, locale, *snapshot.SelectedTask)
	}
	children := []ui.Node{
		html.H1(html.Props{ID: "agents-page-title"}, ui.Text(locale.Text("agents.page_title"))),
		html.P(html.Props{Class: "agents-page-subtitle"}, ui.Text(locale.Text("agents.page_subtitle"))),
		html.Div(html.Props{Class: "agents-layout"},
			tagAgentsSidebar(locale, snapshot),
			html.Section(html.Props{Class: "agents-main", Role: "region", Aria: map[string]string{"label": locale.Text("agents.conversations")}},
				selectedTask,
				tagAgentComposer(locale, snapshot),
				tagAgentThreads(locale, snapshot),
				tagAgentTasks(view, locale, snapshot),
			),
		),
	}
	children = append(children, AgentControlsMount(locale), AgentRolloutPortableMount(locale, AgentRolloutSnapshot{Loading: true}, AgentPortableSnapshot{}))
	if view.Allows(PagePersonaAdmin, "view") {
		children = append(children, ui.CreateElement(ActionLink, ActionLinkProps{Label: locale.Text("page.personas.label"), Href: statefulHref(view, PagePersonaAdmin), Class: "button secondary", Navigate: view.Navigate}))
	}
	if !snapshot.StartAvailable {
		for _, page := range []PageID{PageChatSettings, PageConfigurationCenter} {
			if view.Allows(page, "view") {
				children = append(children, ui.CreateElement(ActionLink, ActionLinkProps{Label: map[PageID]string{PageChatSettings: locale.Text("page.chat_settings.label"), PageConfigurationCenter: locale.Text("page.configuration_center.label")}[page], Href: statefulHref(view, page), Class: "button secondary", Navigate: view.Navigate}))
			}
		}
	}
	return html.Section(html.Props{Class: "agents-page", Aria: map[string]string{"labelledby": "agents-page-title"}}, children...)
}

func tagAgentsSidebar(locale LocaleContext, snapshot AgentSnapshot) ui.Node {
	items := make([]ui.Node, 0, len(snapshot.Agents))
	for _, agent := range snapshot.Agents {
		label := agent.Name
		if strings.TrimSpace(label) == "" {
			label = agent.ID
		}
		items = append(items, html.Li(html.Props{Class: "agents-agent-item", Raw: map[string]any{"data-agent-id": agent.ID}},
			html.Strong(html.Props{}, ui.Text(label)),
			html.Span(html.Props{Class: "agents-agent-status"}, ui.Text(agent.Status)),
			html.P(html.Props{Class: "muted"}, ui.Text(agent.Description)),
			agentSkills(locale, agent.Skills),
		))
	}
	if len(items) == 0 {
		items = append(items, html.Li(html.Props{Class: "muted"}, ui.Text(locale.Text("agents.no_agents"))))
	}
	return html.Nav(html.Props{Class: "agents-sidebar", Aria: map[string]string{"label": locale.Text("agents.sidebar")}},
		html.H2(html.Props{}, ui.Text(locale.Text("agents.available"))),
		html.Ul(html.Props{Class: "agents-agent-list"}, items...),
	)
}

func tagAgentThreads(locale LocaleContext, snapshot AgentSnapshot) ui.Node {
	agentNames := make(map[string]string, len(snapshot.Agents))
	for _, agent := range snapshot.Agents {
		if name := strings.TrimSpace(agent.Name); name != "" {
			agentNames[agent.ID] = name
		}
	}
	threads := make([]ui.Node, 0, len(snapshot.Threads))
	for _, thread := range snapshot.Threads {
		posts := make([]ui.Node, 0, len(thread.Posts))
		for _, post := range thread.Posts {
			identity := strings.TrimSpace(post.Author)
			if identity == "" {
				identity = agentNames[thread.AgentID]
			}
			if identity == "" {
				identity = locale.Text("agents.agent_identity")
			}
			postChildren := []ui.Node{html.Strong(html.Props{Class: "agents-post-author"}, ui.Text(identity))}
			if post.ForUser {
				postChildren = append(postChildren, html.Span(html.Props{Class: "agents-acting-for"}, ui.Text(locale.Text("agents.acting_for_you"))))
			}
			postChildren = append(postChildren, html.P(html.Props{Class: "agents-post-body"}, ui.Text(post.Body)))
			if strings.TrimSpace(post.At) != "" {
				postChildren = append(postChildren, html.Span(html.Props{Class: "agents-post-at"}, ui.Text(post.At)))
			}
			if len(post.Skills) > 0 {
				postChildren = append(postChildren, html.Div(html.Props{Class: "agents-post-skills"},
					html.Span(html.Props{Class: "muted"}, ui.Text(locale.Text("agents.skills_used"))), agentSkills(locale, post.Skills)))
			}
			posts = append(posts, html.Li(html.Props{Class: "agents-post", Raw: map[string]any{"data-agent-post": post.ID}}, postChildren...))
		}
		threads = append(threads, html.Article(html.Props{Class: "agents-thread", Raw: map[string]any{"data-thread-id": thread.ID}},
			html.H2(html.Props{}, ui.Text(thread.Title)), html.Ul(html.Props{Class: "agents-post-list"}, posts...)))
	}
	if len(threads) == 0 {
		threads = append(threads, html.Section(html.Props{Class: "agents-empty-thread", Role: "status"}, ui.Text(locale.Text("agents.no_threads"))))
	}
	return html.Section(html.Props{Class: "agents-threads", Role: "region", Aria: map[string]string{"label": locale.Text("agents.threads")}}, threads...)
}

func tagAgentTasks(view View, locale LocaleContext, snapshot AgentSnapshot) ui.Node {
	groups := map[AgentTaskState][]AgentTask{}
	for _, task := range snapshot.Tasks {
		groups[task.State] = append(groups[task.State], task)
	}
	ordered := []AgentTaskState{AgentTaskRunning, AgentTaskDrafting, AgentTaskAwaitingPlanConfirmation, AgentTaskAwaitingApproval, AgentTaskAwaitingInput, AgentTaskWaiting, AgentTaskPaused, AgentTaskCompleted, AgentTaskFailed, AgentTaskCancelled, AgentTaskExpired, AgentTaskUnknown}
	sections := make([]ui.Node, 0, len(ordered))
	for _, state := range ordered {
		tasks := groups[state]
		if len(tasks) == 0 {
			continue
		}
		rows := make([]ui.Node, 0, len(tasks))
		for _, task := range tasks {
			rows = append(rows, html.Li(html.Props{Class: "agents-task-row", Raw: map[string]any{"data-task-id": task.ID}},
				RenderAgentTaskSummary(locale, task, statefulHref(view, PageAgents, "task", task.ID)),
			))
		}
		sections = append(sections, html.Section(html.Props{Class: "agents-task-group", Raw: map[string]any{"data-task-state": string(state)}},
			html.H3(html.Props{}, ui.Text(agentTaskStateLabel(locale, state))), html.Ul(html.Props{Class: "agents-task-list"}, rows...)))
	}
	if len(sections) == 0 {
		sections = append(sections, html.P(html.Props{Class: "muted"}, ui.Text(locale.Text("agents.no_tasks"))))
	}
	content := append([]ui.Node{html.H2(html.Props{}, ui.Text(locale.Text("agents.tasks")))}, sections...)
	return html.Section(html.Props{Class: "agents-tasks", Role: "region", Aria: map[string]string{"label": locale.Text("agents.tasks")}}, content...)
}

func tagAgentComposer(locale LocaleContext, snapshot AgentSnapshot) ui.Node {
	helper := locale.Text("agents.composer_help")
	if !snapshot.StartAvailable {
		copy := [3]string{"Task creation is unavailable for your current access.", "Die Aufgabenerstellung ist für Ihren Zugriff nicht verfügbar.", "إنشاء المهام غير متاح لصلاحياتك الحالية."}
		if snapshot.StartUnavailableReason == "model_unavailable" {
			copy = [3]string{"Connect a model provider to start agent tasks.", "Verbinden Sie einen Modellanbieter, um Agentenaufgaben zu starten.", "اربط مزود نموذج لبدء مهام الوكيل."}
		}
		if snapshot.StartUnavailableReason == "disabled" {
			copy = [3]string{"Enable agents in Chat settings to start tasks.", "Aktivieren Sie Agenten in den Chat-Einstellungen, um Aufgaben zu starten.", "فعّل الوكلاء في إعدادات الدردشة لبدء المهام."}
		}
		helper = copy[0]
		if strings.HasPrefix(locale.Resolved, "de") {
			helper = copy[1]
		}
		if strings.HasPrefix(locale.Resolved, "ar") {
			helper = copy[2]
		}
	}
	return html.Form(html.Props{Class: "agents-composer", Raw: map[string]any{"aria-label": locale.Text("agents.composer")}},
		html.Label(html.Props{For: "agents-composer-input"}, ui.Text(locale.Text("agents.composer_label"))),
		html.Textarea(html.Props{ID: "agents-composer-input", Name: "prompt", Rows: 3, Placeholder: locale.Text("agents.composer_placeholder"), Raw: map[string]any{"aria-describedby": "agents-composer-help"}}),
		html.P(html.Props{ID: "agents-composer-help", Class: "muted", Raw: map[string]any{"data-start-unavailable-reason": snapshot.StartUnavailableReason}}, ui.Text(helper)),
		// The client writes progress and refusals here. Its localized messages
		// ride on the node, so the browser needs no catalog of its own.
		html.P(html.Props{ID: "agents-composer-status", Class: "muted", Role: "status", Raw: map[string]any{
			"aria-live": "polite", "data-msg-working": locale.Text("agents.start_working"), "data-msg-done": locale.Text("agents.start_done"),
			"data-msg-empty": locale.Text("agents.start_empty"), "data-msg-disabled": locale.Text("agents.start_disabled"),
			"data-msg-denied": locale.Text("agents.start_denied"), "data-msg-failed": locale.Text("agents.start_failed"),
		}}),
		html.Div(html.Props{Class: "agents-composer-actions"},
			html.Button(html.Props{Class: "button secondary", Type: "button", Disabled: !snapshot.StartAvailable, Raw: map[string]any{"data-agent-action": "quick-answer"}}, ui.Text(locale.Text("agents.quick_answer"))),
			html.Button(html.Props{Class: "button primary", Type: "button", Disabled: !snapshot.StartAvailable, Raw: map[string]any{"data-agent-action": "long-task"}}, ui.Text(locale.Text("agents.start_task"))),
		),
	)
}

func tagAgentTaskView(view View, locale LocaleContext, task AgentTask) ui.Node {
	steps := make([]ui.Node, 0, len(task.Steps))
	for index, step := range task.Steps {
		steps = append(steps, html.Li(html.Props{Class: "agents-plan-step", Raw: map[string]any{"data-step-state": step.State, "data-step-tier": step.Tier}},
			html.Span(html.Props{Class: "agents-step-number"}, ui.Text(fmt.Sprintf("%d", index+1))),
			html.Strong(html.Props{}, ui.Text(agentStepName(step.Name))), html.Span(html.Props{Class: "status"}, ui.Text(agentStepStateLabel(locale, step.State))),
			html.Span(html.Props{Class: "agents-step-tier"}, ui.Text(agentStepTierLabel(locale, step.Tier))), html.P(html.Props{Class: "muted"}, ui.Text(step.Detail))))
	}
	checkpoints := make([]ui.Node, 0, len(task.Checkpoints))
	for _, checkpoint := range task.Checkpoints {
		checkpoints = append(checkpoints, html.Li(html.Props{}, html.Strong(html.Props{}, ui.Text(checkpoint.Label)), html.Span(html.Props{Class: "muted"}, ui.Text(checkpoint.At))))
	}
	artifacts := make([]ui.Node, 0, len(task.Artifacts))
	for _, artifact := range task.Artifacts {
		artifacts = append(artifacts, html.Li(html.Props{}, softwareLink(view.Navigate, html.Props{}, artifact.Href, ui.Text(artifact.Name)), html.Span(html.Props{Class: "muted"}, ui.Text(artifact.Kind))))
	}
	approvals := make([]ui.Node, 0, len(task.Approvals))
	for _, approval := range task.Approvals {
		sources := make([]ui.Node, 0, len(approval.Sources))
		for _, source := range approval.Sources {
			sources = append(sources, html.Li(html.Props{}, ui.Text(source)))
		}
		approvals = append(approvals, html.Article(html.Props{Class: "agents-approval-card", Raw: map[string]any{"data-approval-id": approval.ID}},
			html.H3(html.Props{}, ui.Text(approval.Summary)), html.Code(html.Props{Class: "agents-digest"}, ui.Text(approval.Digest)),
			html.Span(html.Props{Class: "status agents-taint"}, ui.Text(approval.Taint)), html.H4(html.Props{}, ui.Text(locale.Text("agents.sources"))), html.Ul(html.Props{}, sources...)))
	}
	intents := make([]ui.Node, 0, len(task.SubmittedIntents))
	for _, intent := range task.SubmittedIntents {
		intents = append(intents, html.Li(html.Props{}, html.Strong(html.Props{}, ui.Text(intent.Name)), html.Span(html.Props{Class: "status"}, ui.Text(intent.Status))))
	}
	children := []ui.Node{
		html.Div(html.Props{Class: "agents-task-heading"}, html.H2(html.Props{ID: "agents-task-title"}, ui.Text(task.Title)), html.Span(html.Props{Class: "status"}, ui.Text(agentTaskStateLabel(locale, task.State)))),
		html.P(html.Props{Class: "agents-task-goal"}, ui.Text(task.Goal)),
		answerBlock(locale, task.AnswerText),
	}
	if actions := taskActions(locale, task); actions != nil {
		children = append(children, actions)
	}
	children = append(children, taskControlStatus(locale))
	if len(steps) > 0 {
		planKey := "agents.confirmed_plan"
		if task.State == AgentTaskAwaitingPlanConfirmation || task.State == AgentTaskDrafting {
			planKey = "agents.proposed_plan"
		}
		children = append(children, html.Section(html.Props{Class: "agents-task-plan"}, html.H3(html.Props{}, ui.Text(locale.Text(planKey))), html.Ol(html.Props{}, steps...)))
	}
	if strings.TrimSpace(task.LiveStep) != "" {
		liveChildren := []ui.Node{html.H3(html.Props{}, ui.Text(locale.Text("agents.live_step"))), html.P(html.Props{}, ui.Text(task.LiveStep))}
		if strings.TrimSpace(task.BudgetUsed) != "" || strings.TrimSpace(task.BudgetLimit) != "" {
			liveChildren = append(liveChildren, html.P(html.Props{Class: "muted"}, ui.Text(locale.Text("agents.budget_used", map[string]string{"used": task.BudgetUsed, "limit": task.BudgetLimit}))))
		}
		children = append(children, html.Section(html.Props{Class: "agents-task-live"}, liveChildren...))
	}
	for _, detail := range []ui.Node{taskDetailList(locale, "agents.checkpoints", checkpoints), taskDetailList(locale, "agents.artifacts", artifacts), taskDetailList(locale, "agents.approvals", approvals), taskDetailList(locale, "agents.submitted_intents", intents)} {
		if detail != nil {
			children = append(children, detail)
		}
	}
	if strings.TrimSpace(task.PlanDiff) != "" {
		children = append(children, html.Section(html.Props{Class: "agents-plan-diff"}, html.H3(html.Props{}, ui.Text(locale.Text("agents.plan_revision", map[string]string{"revision": task.PlanRevision}))), html.Pre(html.Props{}, ui.Text(task.PlanDiff))))
	}
	return html.Section(html.Props{Class: "agents-task-view", Role: "region", Aria: map[string]string{"label": locale.Text("agents.task_view"), "labelledby": "agents-task-title"}, Raw: map[string]any{"data-task-view": task.ID}}, children...)
}

func taskControlStatus(locale LocaleContext) ui.Node {
	return html.P(html.Props{ID: "agents-task-control-status", Class: "muted", Role: "status", Raw: map[string]any{
		"aria-live":         "polite",
		"data-msg-working":  locale.Text("agents.control_working"),
		"data-msg-done":     locale.Text("agents.control_done"),
		"data-msg-conflict": locale.Text("agents.control_conflict"),
		"data-msg-denied":   locale.Text("agents.control_denied"),
		"data-msg-disabled": locale.Text("agents.control_disabled"),
		"data-msg-failed":   locale.Text("agents.control_failed"),
	}}, ui.Text(""))
}

func answerBlock(locale LocaleContext, answer string) ui.Node {
	if answer == "" {
		return nil
	}
	return html.Section(html.Props{Class: "agents-task-answer"}, html.H3(html.Props{}, ui.Text(locale.Text("agents.answer"))), html.P(html.Props{}, ui.Text(answer)))
}

func taskActionButton(locale LocaleContext, taskID, action, key string, version uint64) ui.Node {
	return html.Button(html.Props{Class: "button secondary", Type: "button", Aria: map[string]string{"label": locale.Text(key)}, Raw: map[string]any{"data-task-action": action, "data-task-id": taskID, "data-task-version": version}}, ui.Text(locale.Text(key)))
}

func taskActions(locale LocaleContext, task AgentTask) ui.Node {
	if task.Version == 0 || !knownAgentTaskState(task.State) {
		return nil
	}
	buttons := make([]ui.Node, 0, 4)
	if task.Actions.ConfirmPlan && task.State == AgentTaskAwaitingPlanConfirmation {
		buttons = append(buttons, taskActionButton(locale, task.ID, "confirm-plan", "agents.confirm_plan", task.Version))
	}
	if task.Actions.Pause && (task.State == AgentTaskRunning || task.State == AgentTaskAwaitingApproval || task.State == AgentTaskAwaitingInput || task.State == AgentTaskWaiting) {
		buttons = append(buttons, taskActionButton(locale, task.ID, "pause", "agents.pause", task.Version))
	}
	if task.Actions.Resume && task.State == AgentTaskPaused {
		buttons = append(buttons, taskActionButton(locale, task.ID, "resume", "agents.resume", task.Version))
	}
	if task.Actions.Cancel && task.State != AgentTaskCompleted && task.State != AgentTaskFailed && task.State != AgentTaskCancelled && task.State != AgentTaskExpired {
		buttons = append(buttons, taskActionButton(locale, task.ID, "cancel", "agents.cancel", task.Version))
	}
	if len(buttons) == 0 {
		return nil
	}
	return html.Div(html.Props{Class: "agents-task-actions"}, buttons...)
}

func knownAgentTaskState(state AgentTaskState) bool {
	switch state {
	case AgentTaskRunning, AgentTaskAwaitingApproval, AgentTaskAwaitingInput, AgentTaskDrafting,
		AgentTaskAwaitingPlanConfirmation, AgentTaskWaiting, AgentTaskPaused, AgentTaskCompleted,
		AgentTaskFailed, AgentTaskCancelled, AgentTaskExpired, AgentTaskUnknown:
		return true
	default:
		return false
	}
}

func taskDetailList(locale LocaleContext, key string, children []ui.Node) ui.Node {
	if len(children) == 0 {
		return nil
	}
	return html.Section(html.Props{Class: "agents-task-detail", Raw: map[string]any{"data-detail": key}}, html.H3(html.Props{}, ui.Text(locale.Text(key))), html.Ul(html.Props{}, children...))
}

func agentSkills(locale LocaleContext, skills []string) ui.Node {
	items := make([]ui.Node, 0, len(skills))
	for _, skill := range skills {
		items = append(items, html.Li(html.Props{Class: "agents-skill"}, ui.Text(skill)))
	}
	return html.Ul(html.Props{Class: "agents-skills", Aria: map[string]string{"label": locale.Text("agents.skills_used")}}, items...)
}

func agentTaskStateLabel(locale LocaleContext, state AgentTaskState) string {
	switch state {
	case AgentTaskRunning, AgentTaskAwaitingApproval, AgentTaskAwaitingInput, AgentTaskDrafting,
		AgentTaskAwaitingPlanConfirmation, AgentTaskWaiting, AgentTaskPaused, AgentTaskCompleted,
		AgentTaskFailed, AgentTaskCancelled, AgentTaskExpired:
		return locale.Text("agents.state." + string(state))
	default:
		return locale.Text("agents.state.unknown")
	}
}

func agentStepName(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "Step"
	}
	if dot := strings.LastIndex(trimmed, "."); dot >= 0 {
		trimmed = trimmed[dot+1:]
	}
	words := strings.FieldsFunc(trimmed, func(r rune) bool { return r == '_' || r == '-' })
	if len(words) == 0 {
		return "Step"
	}
	if strings.Contains(name, ".") || strings.ContainsAny(name, "_-") {
		words[0] = upperFirst(strings.ToLower(words[0]))
		return strings.Join(words, " ")
	}
	return trimmed
}

func agentStepStateLabel(locale LocaleContext, state string) string {
	key := strings.ToLower(strings.TrimSpace(state))
	key = strings.ReplaceAll(key, " ", "_")
	switch key {
	case "observed":
		return locale.Text("agents.step_state.observed")
	case "awaiting_approval":
		return locale.Text("agents.step_state.awaiting_approval")
	case "pending", "waiting":
		return locale.Text("agents.step_state.pending")
	case "running":
		return locale.Text("agents.step_state.running")
	case "completed", "complete":
		return locale.Text("agents.step_state.completed")
	case "failed", "failure":
		return locale.Text("agents.step_state.failed")
	default:
		return locale.Text("agents.state.unknown")
	}
}

func agentStepTierLabel(locale LocaleContext, tier string) string {
	switch strings.ToUpper(strings.TrimSpace(tier)) {
	case "T0", "0":
		return locale.Text("agents.tier.read")
	case "T1", "1":
		return locale.Text("agents.tier.private_draft")
	case "T2", "2":
		return locale.Text("agents.tier.communicate")
	case "T3", "3":
		return locale.Text("agents.tier.submit_governed")
	case "T4", "4":
		return locale.Text("agents.tier.external_write")
	default:
		return locale.Text("agents.tier.unknown")
	}
}

func upperFirst(value string) string {
	runes := []rune(value)
	if len(runes) == 0 {
		return value
	}
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}
