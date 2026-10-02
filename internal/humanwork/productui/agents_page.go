package productui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
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
	TasksLoading           bool
	TasksLoadFailed        bool
	DocumentHubAvailable   bool
}

type AgentSummary struct {
	Icon         agenticon.Value
	IconRevision int64
	ID           string
	Name         string
	Description  string
	Status       string
	Skills       []string
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

type AgentDocumentUsageState string

const (
	AgentDocumentUsageUnknown AgentDocumentUsageState = "UNKNOWN"
	AgentDocumentUsageNone    AgentDocumentUsageState = "NONE"
	AgentDocumentUsageUsed    AgentDocumentUsageState = "USED"
)

type AgentTask struct {
	Icon                      agenticon.Value
	IconRevision              int64
	ID                        string
	Version                   uint64
	AgentID                   string
	Title                     string
	Goal                      string
	AnswerText                string
	State                     AgentTaskState
	LiveStep                  string
	BudgetUsed                string
	BudgetLimit               string
	PlanRevision              string
	PlanDiff                  string
	PlanChanges               []AgentPlanChange
	Steps                     []AgentTaskStep
	Checkpoints               []AgentCheckpoint
	Artifacts                 []AgentArtifact
	Approvals                 []AgentApproval
	SubmittedIntents          []AgentIntentStatus
	Actions                   AgentTaskActionPolicy
	CreatedAt                 time.Time
	UpdatedAt                 time.Time
	ResultPreview             string
	FailureReason             string
	Retryable                 bool
	AnsweringAgentID          string
	AnsweringAgentDisplayName string
	AnsweringAgentVersion     string
	Documents                 []AgentTaskDocumentReference
	UsedDocuments             []AgentTaskDocumentReference
	DocumentUsageState        AgentDocumentUsageState
	DocumentOmissions         []AgentTaskDocumentOmission
}

// AgentTaskDocumentReference is the display-safe reference carried by the
// owner-scoped task projection. The document service still authorizes the
// destination when its link is opened.
type AgentTaskDocumentReference struct {
	DocumentID    string
	Label         string
	SectionAnchor string
}

// AgentTaskDocumentOmission explains only what the task service elected to
// disclose. An empty label is deliberately rendered as an aggregate count.
type AgentTaskDocumentOmission struct {
	Label  string
	Reason string
}

// AgentTaskActionPolicy is the server-derived affordance projection for one
// task. The control RPC rechecks every action and version before mutation.
type AgentTaskActionPolicy struct {
	ConfirmPlan  bool
	Pause        bool
	Resume       bool
	Cancel       bool
	ExtendBudget bool
}

type AgentTaskStep struct {
	Name          string
	State         string
	Tier          string
	Detail        string
	StartedAt     time.Time
	FinishedAt    time.Time
	FailureReason string
}

// AgentCheckpoint is one point where the task's progress was saved. The
// server sends Kind (and Step for a finished step) with At as an RFC 3339
// instant; Label is finished text for a caller that has no kind.
type AgentCheckpoint struct {
	Label string
	At    string
	Kind  string
	Step  string
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
	// AGENT2-018: the same route also serves the person's own access view.
	if AgentAccessViewRequested(view.Query) {
		return BuildAgentAccessPage(view)
	}
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
	actions := []ui.Node(nil)
	if view.AgentsProjection != nil && view.AgentsProjection.ViewerIsAdmin {
		actions = append(actions, AgentPageNavigation(view, AgentPageAsk))
	}
	return ProductPageFrame(ProductPageFrameProps{
		Class: "agent-page-frame", Dir: string(locale.Direction), Aria: map[string]string{"labelledby": "agents-page-title"},
		Breadcrumbs: agentUXR7Breadcrumb(locale), Title: locale.Text("agents.page_title"), TitleID: "agents-page-title", Actions: actions,
		Body: []ui.Node{html.Div(html.Props{Class: "agents-page agents-page-unavailable"},
			html.P(html.Props{Class: "agents-page-subtitle"}, ui.Text(locale.Text("agents.page_subtitle"))),
			ui.CreateElement(EmptyState, EmptyStateProps{
				Title: locale.Text("agents.unavailable_title"), Description: locale.Text("agents.load_failed_detail"), Role: "status", Class: "agents-unavailable",
			}),
			html.Button(html.Props{Class: "button secondary", Type: "button", Raw: map[string]any{"data-agent-page-retry": "true"}}, ui.Text(locale.Text("agents.try_again"))),
		)},
	})
}

func renderAgentsPage(view View, locale LocaleContext, snapshot AgentSnapshot) ui.Node {
	children := make([]ui.Node, 0, 4)
	actions := []ui.Node(nil)
	if view.AgentsProjection != nil && view.AgentsProjection.ViewerIsAdmin {
		actions = append(actions, AgentPageNavigation(view, AgentPageAsk))
	}
	children = append(children,
		agentsViewTabs(view, locale, "ask"),
		html.P(html.Props{Class: "agents-page-subtitle"}, ui.Text(locale.Text("agents.page_subtitle"))),
	)
	main := []ui.Node{tagAgentComposerForView(view, locale, snapshot)}
	if len(snapshot.Threads) > 0 {
		main = append(main, tagAgentThreads(locale, snapshot))
	}
	main = append(main, tagAgentTasks(view, locale, snapshot))
	children = append(children, html.Div(html.Props{Class: "agents-layout"},
		html.Section(html.Props{Class: "agents-main", Role: "region", Aria: map[string]string{"label": locale.Text("agents.workspace")}}, main...),
	))
	if !snapshot.StartAvailable {
		for _, page := range []PageID{PageChatSettings, PageConfigurationCenter} {
			if view.Allows(page, "view") {
				children = append(children, ui.CreateElement(ActionLink, ActionLinkProps{Label: map[PageID]string{PageChatSettings: locale.Text("page.chat_settings.label"), PageConfigurationCenter: locale.Text("page.configuration_center.label")}[page], Href: statefulHref(view, page), Class: "button secondary", Navigate: view.Navigate}))
			}
		}
	}
	raw := map[string]any{}
	if snapshot.SelectedTask != nil {
		raw["data-has-task-detail"] = "true"
	}
	return ProductPageFrame(ProductPageFrameProps{
		Class: "agent-page-frame", Dir: string(locale.Direction), Aria: map[string]string{"labelledby": "agents-page-title"},
		Breadcrumbs: agentUXR7Breadcrumb(locale), Title: locale.Text("agents.page_title"), TitleID: "agents-page-title", Actions: actions,
		Body: []ui.Node{html.Div(html.Props{Class: "agents-page", Raw: raw}, children...)},
	})
}

func tagAgentsSidebar(locale LocaleContext, snapshot AgentSnapshot) ui.Node {
	items := make([]ui.Node, 0, len(snapshot.Agents))
	// An agent with no stored icon wears its own fallback, never a glyph another
	// agent in this list has.
	siblings := make([]string, 0, len(snapshot.Agents))
	for _, agent := range snapshot.Agents {
		siblings = append(siblings, agent.ID)
	}
	for _, agent := range snapshot.Agents {
		label := agent.Name
		if strings.TrimSpace(label) == "" {
			label = agent.ID
		}
		items = append(items, html.Li(html.Props{Class: "agents-agent-item", Raw: map[string]any{"data-agent-id": agent.ID}},
			agenticon.NodeFor(agent.Icon, agent.ID, siblings...), html.Strong(html.Props{}, ui.Text(label)),
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
	snapshot = agentUX020SettledTasks(snapshot)
	groups := map[string][]AgentTask{"active": {}, "completed": {}, "failed": {}}
	selectedListed := false
	for _, task := range snapshot.Tasks {
		category := agentTaskCategory(task.State)
		groups[category] = append(groups[category], task)
		selectedListed = selectedListed || snapshot.SelectedTask != nil && snapshot.SelectedTask.ID == task.ID
	}
	if snapshot.SelectedTask != nil && !selectedListed {
		category := agentTaskCategory(snapshot.SelectedTask.State)
		groups[category] = append([]AgentTask{*snapshot.SelectedTask}, groups[category]...)
	}
	selected := "active"
	if snapshot.SelectedTask != nil {
		selected = agentTaskCategory(snapshot.SelectedTask.State)
	} else if len(groups[selected]) == 0 {
		if len(groups["completed"]) > 0 {
			selected = "completed"
		} else if len(groups["failed"]) > 0 {
			selected = "failed"
		}
	}
	rows := make([]ui.Node, 0, len(snapshot.Tasks))
	for _, category := range []string{"active", "completed", "failed"} {
		for index, task := range groups[category] {
			isSelected := snapshot.SelectedTask != nil && snapshot.SelectedTask.ID == task.ID
			task.Icon = agentUX074TaskIcon(snapshot.Agents, task)
			rows = append(rows, agentTaskRow(view, locale, task, isSelected, category, index, category != selected || index >= agentTaskFirstPage))
		}
	}
	listing := []ui.Node{html.Div(html.Props{Class: "agents-tasks-heading"}, html.Div(html.Props{},
		html.H2(html.Props{}, ui.Text(locale.Text("agents.tasks"))),
		html.P(html.Props{Class: "muted agents-chat-history-note"}, ui.Text(locale.Text("agents.chat_history_note"))),
	))}
	if len(snapshot.Tasks) > 0 {
		filters := make([]ui.Node, 0, 3)
		for _, category := range []string{"active", "completed", "failed"} {
			filters = append(filters, html.Button(html.Props{Type: "button", Class: "agents-task-filter", Role: "tab", Raw: map[string]any{
				"data-agent-task-filter": category,
				"aria-selected":          fmt.Sprint(category == selected),
				"aria-controls":          "agents-task-list",
				"tabindex":               map[bool]string{true: "0", false: "-1"}[category == selected],
			}},
				html.Span(html.Props{Class: "agents-task-filter-label"}, ui.Text(locale.Text("agents.filter."+category+"_label"))),
				html.Span(html.Props{Class: "agents-task-filter-count", Aria: map[string]string{"label": locale.Text("agents.task_count")}}, ui.Text(locale.FormatNumber(fmt.Sprint(len(groups[category])), 0))),
			))
		}
		listing = append(listing, html.Div(html.Props{Class: "agents-task-filters", Role: "tablist", Aria: map[string]string{"label": locale.Text("agents.task_filters")}}, filters...))
	}
	if snapshot.TasksLoading {
		listing = append(listing, AgentLoadingFrame(AgentLoadingProps{Locale: locale, Shape: AgentLoadingRows, Rows: 4, Status: locale.Text("agents.tasks_loading"), RetryRaw: map[string]any{"data-agent-tasks-retry": "true"}}))
	}
	if snapshot.TasksLoadFailed {
		listing = append(listing, html.Div(html.Props{Class: "agents-tasks-failed", Role: "alert"},
			html.P(html.Props{}, ui.Text(locale.Text("agents.tasks_load_failed"))),
			html.Button(html.Props{Class: "button secondary", Type: "button", Raw: map[string]any{"data-agent-tasks-retry": "true"}}, ui.Text(locale.Text("agents.try_again"))),
		))
	}
	if len(rows) > 0 && !snapshot.TasksLoadFailed {
		listing = append(listing, html.Ul(html.Props{ID: "agents-task-list", Class: "agents-task-list", Role: "tabpanel"}, rows...))
	}
	for _, category := range []string{"active", "completed", "failed"} {
		if snapshot.TasksLoadFailed {
			break
		}
		key := "agents.no_tasks_" + category
		if category == "active" && len(snapshot.Tasks) == 0 {
			key = "agents.no_tasks"
		}
		listing = append(listing, html.P(html.Props{Class: "agents-tasks-empty", Hidden: category != selected || len(groups[category]) > 0, Role: "status", Raw: map[string]any{"data-agent-task-empty": category}}, ui.Text(locale.Text(key))))
	}
	if len(snapshot.Tasks) > 0 {
		listing = append(listing, html.Button(html.Props{Type: "button", Class: "button secondary agents-task-more", Hidden: len(groups[selected]) <= agentTaskFirstPage, Raw: map[string]any{"data-agent-task-more": "true"}}, ui.Text(locale.Text("agents.show_older"))))
	}
	listing = append(listing, html.P(html.Props{ID: "agents-tasks-announcement", Class: "agents-tasks-announcement", Role: "status", Raw: map[string]any{
		"aria-live":              "polite",
		"data-msg-task-answered": locale.Text("agents.task_answered_announcement", map[string]string{"agent": "__AGENT__"}),
		"data-msg-task-failed":   locale.Text("agents.task_failed_announcement", map[string]string{"agent": "__AGENT__"}),
		"data-msg-agent":         locale.Text("agents.agent_identity"),
	}}))
	selectedTaskID := ""
	if snapshot.SelectedTask != nil {
		selectedTaskID = snapshot.SelectedTask.ID
	}
	content := []ui.Node{html.Div(html.Props{Class: "agents-task-listing"}, listing...)}
	if snapshot.SelectedTask != nil {
		content = append(content, html.Div(html.Props{ID: "agents-task-detail-" + snapshot.SelectedTask.ID, Class: "agents-task-detail-pane"}, tagAgentTaskViewWithRetry(view, locale, *snapshot.SelectedTask, snapshot.StartAvailable)))
	}
	return html.Section(html.Props{ID: "agents-tasks", Class: "agents-tasks", Role: "region", Aria: map[string]string{"label": locale.Text("agents.tasks")}, Raw: map[string]any{"data-selected-task-filter": selected, "data-selected-task-id": selectedTaskID, "data-has-task-detail": fmt.Sprint(snapshot.SelectedTask != nil), "aria-busy": fmt.Sprint(snapshot.TasksLoading)}}, content...)
}

func agentTaskCategory(state AgentTaskState) string {
	switch state {
	case AgentTaskCompleted:
		return "completed"
	case AgentTaskFailed, AgentTaskCancelled, AgentTaskExpired, AgentTaskUnknown:
		return "failed"
	default:
		return "active"
	}
}

func agentTaskRow(view View, locale LocaleContext, task AgentTask, selected bool, category string, index int, hidden bool) ui.Node {
	request := strings.TrimSpace(task.Title)
	if request == "" {
		request = strings.TrimSpace(task.Goal)
	}
	preview := agentTaskResultPreview(locale, task)
	if preview == "" {
		preview = strings.TrimSpace(task.LiveStep)
	}
	if task.State == AgentTaskCompleted {
		if answer := strings.TrimSpace(task.AnswerText); answer != "" {
			preview = answer
		}
	}
	failureReason := ""
	if category == "failed" {
		preview = locale.Text("agents.task_failed_reassurance")
		failureReason = localizedAgentFailureReason(locale, task.FailureReason)
	}
	children := []ui.Node{
		html.Div(html.Props{Class: "agents-task-row-heading"},
			html.Span(html.Props{Class: "agents-task-state-icon", Raw: map[string]any{"data-tone": category, "aria-hidden": "true"}}, ui.Text(agentTaskStatusGlyph(category))),
			html.Strong(html.Props{Class: "agents-task-request"}, html.Tag("bdi", html.Props{}, ui.Text(request))),
			html.Span(html.Props{Class: "agents-task-chevron", Raw: map[string]any{"aria-hidden": "true"}}, ui.Text("›")),
			agentTaskRowTimes(locale, task, time.Now()),
		),
	}
	// The second line of a row says who answered and what: the agent, with the
	// icon it wears in the choices above, then the answer excerpt or the reason
	// the task stopped. A task is two lines at page width, not a narrow stack.
	agentText := agentTaskAgentName(locale, task)
	if category == "completed" {
		agentText = locale.Text("agents.answered_by", map[string]string{"agent": agentTaskAgentName(locale, task)})
	} else if category == "active" {
		agentText = locale.Text("agents.agent_active", map[string]string{
			"agent":  agentTaskAgentName(locale, task),
			"status": agentTaskStateLabel(locale, task.State),
		})
	}
	agent := []ui.Node{}
	if task.Icon.Valid() {
		agent = append(agent, agenticon.Node(task.Icon))
	}
	summary := []ui.Node{html.Span(html.Props{Class: "agents-task-agent muted"}, append(agent, ui.Text(agentText))...)}
	if preview != "" {
		props := html.Props{Class: "agents-task-preview"}
		if category != "failed" {
			props.Raw = map[string]any{"dir": "auto"}
		}
		summary = append(summary, html.Span(props, ui.Text(preview)))
	}
	if failureReason != "" {
		summary = append(summary, html.Span(html.Props{Class: "agents-task-failure-reason"}, ui.Text(failureReason)))
	}
	children = append(children, html.P(html.Props{Class: "agents-task-summary"}, summary...))
	detailID := "agents-task-detail-" + task.ID
	rowClass := "agents-task-row"
	if selected {
		// The route remains expanded semantically; layout is now owned by the
		// adjacent detail pane rather than by this row.
		rowClass += " is-expanded"
	}
	// The browser enhances this ordinary link into an in-page disclosure. Keeping
	// the href preserves the script-free route without letting page navigation
	// race the delegated disclosure handler.
	rowChildren := []ui.Node{html.A(html.Props{Class: "agents-task-link", Aria: map[string]string{
		"label": locale.Text("agents.open_named_task", map[string]string{"request": request}), "expanded": fmt.Sprint(selected), "controls": detailID,
	}, Raw: map[string]any{"data-agent-task-row-link": task.ID}, Href: statefulHref(view, PageAgents, "task", task.ID) + "#agents-task-title"}, children...)}
	// Documents and the retry control are links and buttons of their own, so they
	// sit beside the row link rather than inside it, and only when there are any.
	metaChildren := []ui.Node{}
	if documents := agentTaskDocumentLinks(view, locale, task, true); documents != nil {
		metaChildren = append(metaChildren, html.Div(html.Props{Class: "agents-task-source-meta"}, documents))
	}
	if category == "failed" {
		metaChildren = append(metaChildren, html.Div(html.Props{Class: "agents-task-row-actions"}, askAgainButton(locale, task)))
	}
	if len(metaChildren) > 0 {
		rowChildren = append(rowChildren, html.Div(html.Props{Class: "agents-task-row-meta"}, metaChildren...))
	}
	return html.Li(html.Props{Class: rowClass, Hidden: hidden, Raw: map[string]any{"data-task-id": task.ID, "data-task-category": category, "data-task-index": index, "data-selected": fmt.Sprint(selected)}}, rowChildren...)
}

func agentTaskStatusGlyph(category string) string {
	if category == "completed" {
		return "✓"
	}
	if category == "failed" {
		return "!"
	}
	return "•"
}

func agentTaskResultPreview(_ LocaleContext, task AgentTask) string {
	// ResultPreview is projection-owned answer text. The UI must not attempt to
	// repair server copy: doing so can turn a plan or a transport preamble into
	// a misleading answer. The task projection owns choosing the answer field.
	return strings.TrimSpace(task.ResultPreview)

}

func tagAgentComposer(locale LocaleContext, snapshot AgentSnapshot) ui.Node {
	return tagAgentComposerForView(View{}, locale, snapshot)
}

func tagAgentComposerForView(view View, locale LocaleContext, snapshot AgentSnapshot) ui.Node {
	helper := locale.Text("agents.composer_help")
	if !snapshot.StartAvailable {
		copy := [3]string{"Task creation is unavailable for your current access.", "Die Aufgabenerstellung ist für Ihren Zugriff nicht verfügbar.", "إنشاء المهام غير متاح لصلاحياتك الحالية."}
		if snapshot.StartUnavailableReason == "model_unavailable" {
			copy = [3]string{"Agents cannot answer right now. Contact the person who manages agents in your workspace.", "Agenten können gerade nicht antworten. Kontaktieren Sie die Person, die Ihre Agenten verwaltet.", "لا يمكن للوكلاء الإجابة الآن. تواصل مع الشخص الذي يدير الوكلاء في مساحة عملك."}
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
	children := make([]ui.Node, 0, 8)
	if len(snapshot.Agents) > 0 {
		choices := make([]ui.Node, 0, len(snapshot.Agents)+1)
		choices = append(choices, agentChoice(locale, "agents-choice-general", "", locale.Text("agents.general_agent"), locale.Text("agents.general_agent_purpose"), true))
		// Each agent wears its stored icon; one without a stored icon takes its
		// fallback among the agents listed here, as Chat does among the agents it
		// knows, so the same agent has the same picture on both pages.
		peers := agentUX074AgentIDs(snapshot.Agents)
		for index, agent := range snapshot.Agents {
			name := strings.TrimSpace(agent.Name)
			if name == "" {
				name = locale.Text("agents.agent_identity")
			}
			purpose := strings.TrimSpace(agent.Description)
			if purpose == "" {
				purpose = locale.Text("agents.agent_purpose_unavailable")
			}
			id := fmt.Sprintf("agents-choice-%d", index)
			choices = append(choices, agentChoice(locale, id, agent.ID, name, purpose, false, agentUX074IconValue(agent.Icon, agent.ID, peers)))
		}
		children = append(children, html.Fieldset(html.Props{Class: "agents-agent-choices"}, html.Legend(html.Props{}, ui.Text(locale.Text("agents.who_should_answer"))), html.Div(html.Props{}, choices...)))
	} else {
		children = append(children, html.Fieldset(html.Props{Class: "agents-agent-choices"}, html.Legend(html.Props{}, ui.Text(locale.Text("agents.who_should_answer"))), html.Div(html.Props{}, agentChoice(locale, "agents-choice-general", "", locale.Text("agents.general_agent"), locale.Text("agents.general_agent_purpose"), true))))
		children = append(children, html.P(html.Props{Class: "agents-general-agent muted", Role: "status"}, ui.Text(agentUXR7Text(locale, "other_agents"))))
	}
	inputProps := html.Props{ID: "agents-composer-input", Name: "prompt", Rows: 3, Placeholder: locale.Text("agents.composer_placeholder")}
	if !snapshot.StartAvailable {
		inputProps.Raw = map[string]any{"aria-describedby": "agents-composer-help"}
	}
	children = append(children,
		html.Label(html.Props{For: "agents-composer-input"}, ui.Text(locale.Text("agents.composer_label"))),
		html.Textarea(inputProps),
		html.Div(html.Props{Class: "agents-document-picker-mount", Raw: map[string]any{"data-agent-request-document-mount": "true"}},
			map[bool]ui.Node{true: AgentRequestDocumentPicker(locale), false: nil}[snapshot.DocumentHubAvailable],
		),
		// The client writes progress and refusals here. Its localized messages
		// ride on the node, so the browser needs no catalog of its own.
		html.P(html.Props{ID: "agents-composer-status", Class: "muted", Role: "status", Raw: map[string]any{
			"aria-live": "polite", "data-msg-working": locale.Text("agents.start_working"), "data-msg-done": locale.Text("agents.start_done"),
			"data-msg-empty": locale.Text("agents.start_empty"), "data-msg-disabled": locale.Text("agents.start_disabled"),
			"data-msg-denied": locale.Text("agents.start_denied"), "data-msg-failed": locale.Text("agents.start_failed"),
		}}),
	)
	if !snapshot.StartAvailable {
		children = append(children, html.P(html.Props{ID: "agents-composer-help", Class: "muted", Raw: map[string]any{"data-start-unavailable-reason": snapshot.StartUnavailableReason}}, ui.Text(helper)))
	}
	children = append(children,
		html.P(html.Props{Class: "agents-composer-actions-help muted"}, ui.Text(locale.Text("agents.actions_help"))),
		html.Div(html.Props{Class: "agents-composer-actions"},
			html.Button(html.Props{Class: "button primary", Type: "button", Disabled: !snapshot.StartAvailable, Raw: map[string]any{"data-agent-action": "quick-answer"}}, ui.Text(locale.Text("agents.quick_answer"))),
			html.Button(html.Props{Class: "button secondary", Type: "button", Disabled: !snapshot.StartAvailable, Raw: map[string]any{"data-agent-action": "long-task"}}, ui.Text(locale.Text("agents.start_task"))),
		),
	)
	return html.Form(html.Props{Class: "agents-composer", Raw: map[string]any{"aria-label": locale.Text("agents.composer")}}, children...)
}

func agentChoice(locale LocaleContext, id, value, name, purpose string, checked bool, identity ...agenticon.Value) ui.Node {
	return html.Label(html.Props{Class: "agents-agent-choice", For: id},
		html.Input(html.Props{ID: id, Type: "radio", Name: "agent", Checked: checked, Raw: map[string]any{"value": value}}),
		html.Span(html.Props{Dir: string(locale.Direction)}, agenticon.NodeFor(integrate1IconValue(identity), choiceSeed(id, value)), html.Strong(html.Props{Dir: "auto"}, ui.Text(name)), agentUXR7Description(locale, purpose)),
	)
}

func tagAgentTaskView(view View, locale LocaleContext, task AgentTask) ui.Node {
	return tagAgentTaskViewWithRetry(view, locale, task, false)
}

func tagAgentTaskViewWithRetry(view View, locale LocaleContext, task AgentTask, retryAvailable bool) ui.Node {
	task.State = AgentTaskSettledState(task)
	if task.State == AgentTaskCompleted && strings.TrimSpace(task.AnswerText) == "" {
		task.AnswerText = task.ResultPreview
	}
	displaySteps := append([]AgentTaskStep(nil), task.Steps...)
	for index := range displaySteps {
		if strings.EqualFold(strings.TrimSpace(displaySteps[index].State), "failed") && strings.TrimSpace(displaySteps[index].Detail) == "" {
			reason := strings.TrimSpace(displaySteps[index].FailureReason)
			if reason == "" {
				reason = strings.TrimSpace(task.FailureReason)
			}
			displaySteps[index].Detail = localizedAgentFailureReason(locale, reason)
			if displaySteps[index].Detail == "" {
				displaySteps[index].Detail = locale.Text("agents.step_failed_generic")
			}
		}
	}
	steps := agentTaskStepItems(locale, displaySteps)
	checkpoints := make([]ui.Node, 0, len(task.Checkpoints))
	for _, checkpoint := range task.Checkpoints {
		checkpoints = append(checkpoints, agentTaskCheckpointItem(locale, checkpoint, time.Now()))
	}
	artifacts := make([]ui.Node, 0, len(task.Artifacts))
	for _, artifact := range task.Artifacts {
		artifacts = append(artifacts, agentTaskArtifactItem(view, locale, artifact))
	}
	approvals := make([]ui.Node, 0, len(task.Approvals))
	for _, approval := range task.Approvals {
		approvals = append(approvals, agentTaskApprovalCard(locale, approval))
	}
	intents := make([]ui.Node, 0, len(task.SubmittedIntents))
	for _, intent := range task.SubmittedIntents {
		intents = append(intents, agentTaskIntentItem(locale, intent))
	}
	request := agentTaskRequest(task)
	heading := []ui.Node{
		html.H1(html.Props{ID: "agents-task-title", Raw: map[string]any{"tabindex": "-1", "dir": "auto"}}, ui.Text(request)),
		html.Span(html.Props{Class: "agents-task-status", Raw: map[string]any{"data-tone": agentTaskCategory(task.State)}}, ui.Text(agentTaskStateLabel(locale, task.State))),
	}
	children := []ui.Node{
		softwareLink(view.Navigate, html.Props{Class: "agents-back-link", Raw: map[string]any{"data-agent-back-tasks": "true", "data-agent-back-task-id": task.ID}}, statefulHref(view, PageAgents)+"#agents-tasks", html.Span(html.Props{Class: "agents-detail-back-mobile"}, ui.Text(locale.Text("agents.back_to_tasks"))), html.Span(html.Props{Class: "agents-detail-close-desktop"}, ui.Text("× "+agentUXR7Text(locale, "close")))),
		html.Div(html.Props{Class: "agents-task-heading"}, heading...),
		agentTaskDetailMeta(locale, task),
	}
	if task.State == AgentTaskFailed {
		children = append(children, failedTaskBlock(locale, task))
	} else if answer := answerBlock(locale, task.AnswerText); answer != nil {
		children = append(children, answer)
	}
	var plan ui.Node
	if len(steps) > 1 {
		planKey := "agents.what_agent_did"
		if task.State == AgentTaskAwaitingPlanConfirmation || task.State == AgentTaskDrafting {
			planKey = "agents.proposed_plan"
		}
		plan = html.Section(html.Props{Class: "agents-task-plan"}, html.H3(html.Props{}, ui.Text(locale.Text(planKey))), html.Ol(html.Props{}, steps...))
	}
	planFirst := task.State == AgentTaskAwaitingPlanConfirmation || task.State == AgentTaskDrafting
	// What a new plan revision changes is read with the plan it changes: next
	// to the plan while it waits for confirmation, after the detail otherwise.
	var revision ui.Node
	planChanges := agentTaskPlanChanges(locale, task.PlanChanges)
	if strings.TrimSpace(task.PlanDiff) != "" || planChanges != nil {
		parts := []ui.Node{html.H3(html.Props{}, ui.Text(locale.Text("agents.plan_revision", map[string]string{"revision": task.PlanRevision})))}
		if planChanges != nil {
			parts = append(parts, planChanges)
		}
		if strings.TrimSpace(task.PlanDiff) != "" {
			parts = append(parts, html.Pre(html.Props{}, ui.Text(task.PlanDiff)))
		}
		revision = html.Section(html.Props{Class: "agents-plan-diff"}, parts...)
	}
	if goal := agentTaskFullGoal(locale, task); goal != nil {
		children = append(children, goal)
	}
	if planFirst && plan != nil {
		children = append(children, plan)
	}
	if planFirst && revision != nil {
		children = append(children, revision)
	}
	if documents := agentTaskDocumentLinks(view, locale, task, false); documents != nil {
		children = append(children, documents)
	}
	if omissions := agentTaskOmissions(locale, task.DocumentOmissions); omissions != nil {
		children = append(children, omissions)
	}
	if next := taskNextActions(view, locale, task, retryAvailable); next != nil {
		children = append(children, next)
	}
	if planFirst {
		if actions := taskActions(view, locale, task); actions != nil {
			children = append(children, actions)
		}
	}
	if !planFirst {
		if actions := taskActions(view, locale, task); actions != nil {
			children = append(children, actions)
		}
	}
	children = append(children, taskControlStatus(locale))
	if !planFirst && plan != nil {
		children = append(children, plan)
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
	if !planFirst && revision != nil {
		children = append(children, revision)
	}
	return html.Section(html.Props{Class: "agents-task-view", Role: "region", Aria: map[string]string{"label": locale.Text("agents.task_view"), "labelledby": "agents-task-title"}, Raw: map[string]any{"data-task-view": task.ID, "data-task-request": request}}, children...)
}

func agentTaskRequest(task AgentTask) string {
	if request := strings.TrimSpace(task.Title); request != "" {
		return request
	}
	if request := strings.TrimSpace(task.Goal); request != "" {
		return request
	}
	return "Task"
}

func failedTaskBlock(locale LocaleContext, task AgentTask) ui.Node {
	detail := localizedAgentFailureReason(locale, task.FailureReason)
	if detail == "" {
		if task.Retryable {
			detail = locale.Text("agents.task_failed_detail")
		} else {
			detail = locale.Text("agents.task_failed_detail_no_retry")
		}
	}
	return html.Section(html.Props{Class: "agents-task-answer agents-task-failure", Role: "alert"},
		html.H2(html.Props{}, ui.Text(locale.Text("agents.task_failed_title"))),
		html.P(html.Props{Raw: map[string]any{"dir": "auto"}}, ui.Text(detail)),
	)
}

func agentTaskAgentName(locale LocaleContext, task AgentTask) string {
	if strings.EqualFold(strings.TrimSpace(task.AnsweringAgentID), "general-agent") {
		return locale.Text("agents.general_agent")
	}
	if name := strings.TrimSpace(task.AnsweringAgentDisplayName); name != "" {
		return name
	}
	return locale.Text("agents.general_agent")
}

func agentTaskDetailAgentLine(locale LocaleContext, task AgentTask) ui.Node {
	key := "agents.agent_active"
	switch agentTaskCategory(task.State) {
	case "completed":
		key = "agents.answered_by"
	case "failed":
		key = "agents.agent_failed"
	}
	return html.P(html.Props{Class: "agents-task-agent muted"}, agenticon.NodeFor(task.Icon, task.AgentID), ui.Text(locale.Text(key, map[string]string{
		"agent":  agentTaskAgentName(locale, task),
		"status": agentTaskStateLabel(locale, task.State),
	})))
}

func agentTaskDetailMeta(locale LocaleContext, task AgentTask) ui.Node {
	at := task.UpdatedAt
	if at.IsZero() {
		at = task.CreatedAt
	}
	when := locale.Text("agents.time_now")
	if !at.IsZero() {
		when = agentTaskDateTimeLabel(locale, at, time.Now())
	}
	duration := locale.Text("agents.detail_took_under_minute")
	if !task.CreatedAt.IsZero() && !task.UpdatedAt.IsZero() && task.UpdatedAt.Sub(task.CreatedAt) >= time.Minute {
		duration = locale.Text("agents.detail_took_minutes", map[string]string{"count": locale.FormatNumber(fmt.Sprint(int(task.UpdatedAt.Sub(task.CreatedAt).Round(time.Minute)/time.Minute)), 0)})
	}
	return html.P(html.Props{Class: "agents-task-meta muted"}, ui.Text(locale.Text("agents.detail_meta", map[string]string{
		"agent": agentTaskAgentName(locale, task), "time": when, "duration": duration,
	})))
}

func askAgainButton(locale LocaleContext, task AgentTask) ui.Node {
	return html.Button(html.Props{Class: "button secondary", Type: "button", Raw: map[string]any{
		"data-agent-ask-again": "true", "data-agent-retry": "true", "data-agent-retry-prompt": agentTaskRequest(task), "data-agent-retry-persona": agentTaskPersonaSelection(task),
	}}, ui.Text(locale.Text("agents.ask_again")))
}

func taskNextActions(view View, locale LocaleContext, task AgentTask, retryAvailable bool) ui.Node {
	request := agentTaskRequest(task)
	followUpClass := "button primary"
	if task.State == AgentTaskFailed {
		followUpClass = "button secondary"
	}
	followUp := html.Button(html.Props{Class: followUpClass, Type: "button", Raw: map[string]any{
		"data-agent-follow-up": "true", "data-agent-follow-up-context": request, "data-agent-follow-up-href": statefulHref(view, PageAgents) + "#agents-composer-input",
	}}, ui.Text(locale.Text("agents.ask_follow_up")))
	children := make([]ui.Node, 0, 3)
	switch task.State {
	case AgentTaskCompleted:
		children = append(children, followUp)
	case AgentTaskFailed:
		children = append(children, askAgainButton(locale, task))
		if strings.TrimSpace(task.AnsweringAgentID) != "" {
			children = append(children, ui.CreateElement(ActionLink, ActionLinkProps{Label: locale.Text("agents.ask_in_chat"), Href: statefulHref(view, PageChat), Class: "button secondary", Navigate: view.Navigate}))
		}
	case AgentTaskAwaitingPlanConfirmation:
		return nil
	default:
		children = append(children, followUp)
	}
	return html.Div(html.Props{Class: "agents-task-next-actions"}, children...)
}

func agentTaskPersonaSelection(task AgentTask) string {
	id := strings.TrimSpace(task.AnsweringAgentID)
	if id == "general-agent" {
		return ""
	}
	return id
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
	return html.Section(html.Props{Class: "agents-task-answer"},
		html.Div(html.Props{Class: "agents-task-answer-heading"},
			html.H2(html.Props{}, ui.Text(locale.Text("agents.answer"))),
			html.Button(html.Props{Class: "button secondary compact", Type: "button", Raw: map[string]any{"data-agent-copy-answer": "true", "data-agent-answer": answer}}, ui.Text(locale.Text("agents.copy_answer"))),
		),
		html.P(html.Props{Raw: map[string]any{"dir": "auto"}}, ui.Text(answer)),
		html.Span(html.Props{Class: "muted", Role: "status", Raw: map[string]any{"aria-live": "polite", "data-agent-copy-status": "true", "data-msg-copied": locale.Text("agents.copied"), "data-msg-failed": locale.Text("agents.copy_failed")}}),
	)
}

func taskActionButton(locale LocaleContext, taskID, action, key string, version uint64) ui.Node {
	return html.Button(html.Props{Class: "button secondary", Type: "button", Aria: map[string]string{"label": locale.Text(key)}, Raw: map[string]any{"data-task-action": action, "data-task-id": taskID, "data-task-version": version}}, ui.Text(locale.Text(key)))
}

func taskActions(view View, locale LocaleContext, task AgentTask) ui.Node {
	// A task whose state the page does not know is shown as ended: it offers no
	// control that assumes it is still running.
	if task.Version == 0 || !knownAgentTaskState(task.State) || task.State == AgentTaskUnknown {
		return nil
	}
	buttons := make([]ui.Node, 0, 4)
	if task.Actions.ConfirmPlan && task.State == AgentTaskAwaitingPlanConfirmation {
		buttons = append(buttons,
			html.Button(html.Props{Class: "button primary", Type: "button", Aria: map[string]string{"label": locale.Text("agents.start_plan")}, Raw: map[string]any{"data-task-action": "confirm-plan", "data-task-id": task.ID, "data-task-version": task.Version}}, ui.Text(locale.Text("agents.start_plan"))),
			html.Button(html.Props{Class: "button secondary", Type: "button", Raw: map[string]any{"data-agent-follow-up": "true", "data-agent-follow-up-context": agentTaskRequest(task), "data-agent-follow-up-href": statefulHref(view, PageAgents) + "#agents-composer-input"}}, ui.Text(locale.Text("agents.change_request"))),
		)
	}
	if task.Actions.Pause && (task.State == AgentTaskRunning || task.State == AgentTaskAwaitingApproval || task.State == AgentTaskAwaitingInput || task.State == AgentTaskWaiting) {
		buttons = append(buttons, taskActionButton(locale, task.ID, "pause", "agents.pause", task.Version))
	}
	if task.Actions.Resume && task.State == AgentTaskPaused {
		buttons = append(buttons, taskActionButton(locale, task.ID, "resume", "agents.resume", task.Version))
	}
	// AGENT2-017: a task paused at its budget ceiling offers one click to add the
	// policy's allowance; resuming stays a separate click.
	if task.Actions.ExtendBudget && task.State == AgentTaskPaused {
		buttons = append(buttons, taskActionButton(locale, task.ID, "extend-budget", "agents.extend_budget", task.Version))
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

func agentStepPresentation(locale LocaleContext, step AgentTaskStep) (string, string) {
	normalized := strings.ToLower(strings.TrimSpace(step.Name))
	normalized = strings.NewReplacer(".", "_", "-", "_", " ", "_").Replace(normalized)
	for strings.Contains(normalized, "__") {
		normalized = strings.ReplaceAll(normalized, "__", "_")
	}
	name := strings.TrimSpace(step.Name)
	switch normalized {
	case "agent_read_own_worker_state", "read_own_worker_state":
		name = locale.Text("agents.step.own_record")
	case "agent_summarize_request", "summarize_request", "summarise_request":
		name = locale.Text("agents.step.private_draft")
	case "agent_read_handbook", "read_handbook":
		name = locale.Text("agents.step.read_handbook")
	case "read_information":
		name = locale.Text("agents.step.read_information")
	case "prepare_answer":
		name = locale.Text("agents.step.prepare_answer")
	case "prepare_draft":
		name = locale.Text("agents.step.prepare_draft")
	case "send_message":
		name = locale.Text("agents.step.send_message")
	case "submit_request":
		name = locale.Text("agents.step.submit_request")
	case "verify_result":
		name = locale.Text("agents.step.verify_result")
	case "ask_for_information":
		name = locale.Text("agents.step.ask_information")
	case "wait_for_an_update":
		name = locale.Text("agents.step.wait_update")
	case "work_on_request":
		name = locale.Text("agents.step.generic")
	}
	if name == "" {
		name = locale.Text("agents.step.generic")
	}
	return name, agentStepTierLabel(locale, step.Tier)
}

func legacyAgentStepLabel(name string) string {
	normalized := strings.ToLower(strings.TrimSpace(name))
	normalized = strings.NewReplacer(".", "_", "-", "_", " ", "_").Replace(normalized)
	switch normalized {
	case "agent_read_own_worker_state", "read_own_worker_state":
		return "Read own worker state"
	case "agent_summarize_request", "summarize_request", "summarise_request":
		return "Summarize request"
	default:
		return strings.TrimSpace(name)
	}
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
