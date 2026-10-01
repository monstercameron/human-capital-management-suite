// Package agentclient serves the productui Agents page from the composed
// agent runtime (internal/agentsystem). It is the read adapter only: it maps
// the signed-in user's own tasks onto the page contract and never exposes
// ledger entries, tainted content, failure detail or another user's data.
// agentsystem does not import productui; this package joins the two.
package agentclient

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var (
	// ErrUnavailable means no agent runtime is composed for this client.
	ErrUnavailable = errors.New("agentclient: agent runtime unavailable")
	// ErrInvalid means the request named no tenant or no principal.
	ErrInvalid = errors.New("agentclient: tenant and principal are required")
	// ErrNoPrincipal means the catalog request did not carry the verified
	// human principal that owns the snapshot.
	ErrNoPrincipal = errors.New("agentclient: verified human principal is required")
)

// TaskReader is the owner-scoped read side of one tenant's agent runtime.
// *agentsystem.Runner satisfies it.
type TaskReader interface {
	UserTasks(ctx context.Context, userID string) ([]agentrun.AgentTask, error)
	BudgetUsage(taskID string) (used, limit agentbudget.Limits, ok bool)
}

// Runners binds the runtime to one tenant.
type Runners interface {
	Runner(ctx context.Context, tenant values.TenantId) (TaskReader, error)
}

// SettingReader reports whether the tenant has enabled agent work.
type SettingReader interface {
	AgentsEnabled(context.Context, values.TenantId) (bool, error)
}

// PersonaCatalog is the read port for the signed-in user's persona catalog.
// Implementations must apply the current AGENT2-005 discovery gate before
// returning summaries; this adapter only supplies the verified principal and
// never accepts authority claims from the page request.
type PersonaCatalog interface {
	List(context.Context, *trust.Principal) ([]productui.AgentSummary, error)
}

type platformRunners struct{ platform *agentsystem.Platform }

func (p platformRunners) Runner(ctx context.Context, tenant values.TenantId) (TaskReader, error) {
	runner, err := p.platform.ForTenant(ctx, tenant)
	if err != nil {
		return nil, err
	}
	return runner, nil
}

// Client implements productui.AgentClient.
type Client struct {
	runners    Runners
	settings   SettingReader
	controller Controller
	catalog    PersonaCatalog
}

var _ productui.AgentClient = (*Client)(nil)

// New builds a client over any tenant runner source.
func New(runners Runners) *Client { return &Client{runners: runners} }

// NewWithControls configures task affordances only when the current tenant
// setting and server-side controller are both available.
func NewWithControls(runners Runners, settings SettingReader, controller Controller) *Client {
	return &Client{runners: runners, settings: settings, controller: controller}
}

// NewWithControlsAndCatalog configures task controls and the verified-principal
// persona catalog for a client.
func NewWithControlsAndCatalog(runners Runners, settings SettingReader, controller Controller, catalog PersonaCatalog) *Client {
	return &Client{runners: runners, settings: settings, controller: controller, catalog: catalog}
}

// FromPlatform builds a client over the composed agent platform.
func FromPlatform(platform *agentsystem.Platform) *Client {
	if platform == nil {
		return &Client{}
	}
	return New(platformRunners{platform: platform})
}

// FromPlatformWithControls binds the read client, tenant setting and control
// endpoint from one composed runtime.
func FromPlatformWithControls(platform *agentsystem.Platform, settings SettingReader, controller Controller) *Client {
	if platform == nil {
		return &Client{}
	}
	return NewWithControls(platformRunners{platform: platform}, settings, controller)
}

// FromPlatformWithControlsAndCatalog binds the read client, tenant setting,
// control endpoint and verified-principal persona catalog from one composed
// runtime.
func FromPlatformWithControlsAndCatalog(platform *agentsystem.Platform, settings SettingReader, controller Controller, catalog PersonaCatalog) *Client {
	if platform == nil {
		return &Client{}
	}
	return NewWithControlsAndCatalog(platformRunners{platform: platform}, settings, controller, catalog)
}

// Snapshot lists the principal's own tasks in the tenant. TenantID and
// Principal must be server-derived from the verified credential; the runtime
// filters by owner and this adapter filters again, so a store that returned
// another user's or tenant's task still discloses nothing.
func (c *Client) Snapshot(ctx context.Context, req productui.AgentSnapshotRequest) (productui.AgentSnapshot, error) {
	if c == nil || c.runners == nil {
		return productui.AgentSnapshot{}, ErrUnavailable
	}
	tenant, principal := strings.TrimSpace(req.TenantID), strings.TrimSpace(req.Principal)
	if tenant == "" || principal == "" {
		return productui.AgentSnapshot{}, ErrInvalid
	}
	if c.catalog != nil && ctx == nil {
		return productui.AgentSnapshot{}, ErrNoPrincipal
	}
	runner, err := c.runners.Runner(ctx, values.TenantId(tenant))
	if err != nil {
		return productui.AgentSnapshot{}, fmt.Errorf("agentclient: bind tenant runtime: %w", err)
	}
	if runner == nil {
		return productui.AgentSnapshot{}, ErrUnavailable
	}
	tasks, err := runner.UserTasks(ctx, principal)
	if err != nil {
		return productui.AgentSnapshot{}, fmt.Errorf("agentclient: list tasks: %w", err)
	}
	own := make([]agentrun.AgentTask, 0, len(tasks))
	for _, task := range tasks {
		if task.UserID == principal && task.TenantID == tenant {
			own = append(own, task)
		}
	}
	sort.SliceStable(own, func(i, j int) bool { return own[i].UpdatedAt.After(own[j].UpdatedAt) })
	snapshot := productui.AgentSnapshot{
		Availability: productui.AgentsAvailable,
		// The runtime has no agent registry or threads yet; both stay empty
		// rather than being invented from task data.
		Agents:  []productui.AgentSummary{},
		Threads: []productui.AgentThread{},
		Tasks:   make([]productui.AgentTask, 0, len(own)),
	}
	if c.catalog != nil {
		verified, err := verifiedCatalogPrincipal(ctx, tenant, principal)
		if err != nil {
			return productui.AgentSnapshot{}, err
		}
		agents, err := c.catalog.List(ctx, verified)
		if err != nil {
			return productui.AgentSnapshot{}, fmt.Errorf("agentclient: list persona catalog: %w", err)
		}
		snapshot.Agents = cloneAgentSummaries(agents)
	}
	controlsEnabled := false
	if c.settings != nil && c.controller != nil {
		controlsEnabled, _ = c.settings.AgentsEnabled(ctx, values.TenantId(tenant))
	}
	for _, task := range own {
		projected := projectTask(ctx, task, runner)
		if controlsEnabled {
			policy := c.policyForTask(ctx, runner, task, principal)
			projected.Actions = productui.AgentTaskActionPolicy{ConfirmPlan: policy.ConfirmPlan, Pause: policy.Pause, Resume: policy.Resume, Cancel: policy.Cancel}
		}
		snapshot.Tasks = append(snapshot.Tasks, projected)
	}
	return snapshot, nil
}

func verifiedCatalogPrincipal(ctx context.Context, tenant, subject string) (*trust.Principal, error) {
	if ctx == nil {
		return nil, ErrNoPrincipal
	}
	verified, ok := trust.FromContext(ctx)
	if !ok || verified == nil || verified.SubjectKind() != trust.SubjectKindHuman {
		return nil, ErrNoPrincipal
	}
	if verified.Tenant().String() != tenant || verified.Subject() != subject {
		return nil, ErrNoPrincipal
	}
	return verified, nil
}

func cloneAgentSummaries(in []productui.AgentSummary) []productui.AgentSummary {
	out := make([]productui.AgentSummary, len(in))
	for i, item := range in {
		out[i] = item
		out[i].Skills = append([]string(nil), item.Skills...)
	}
	return out
}

// TaskState maps the runtime state machine onto the page's six groups.
func TaskState(state agentrun.TaskState) productui.AgentTaskState {
	switch state {
	case agentrun.StateRunning:
		return productui.AgentTaskRunning
	case agentrun.StateAwaitingApproval:
		return productui.AgentTaskAwaitingApproval
	case agentrun.StateWaiting:
		return productui.AgentTaskWaiting
	case agentrun.StateAwaitingPlanConfirmation:
		return productui.AgentTaskAwaitingPlanConfirmation
	case agentrun.StateDrafting:
		return productui.AgentTaskDrafting
	case agentrun.StatePaused:
		return productui.AgentTaskPaused
	case agentrun.StateCompleted:
		return productui.AgentTaskCompleted
	case agentrun.StateCancelled:
		return productui.AgentTaskCancelled
	case agentrun.StateExpired:
		return productui.AgentTaskExpired
	case agentrun.StateFailed:
		return productui.AgentTaskFailed
	default:
		return productui.AgentTaskUnknown
	}
}

func projectTask(ctx context.Context, task agentrun.AgentTask, budget TaskReader) productui.AgentTask {
	result := productui.AgentTask{
		ID: task.ID, Version: task.Version, Title: taskTitle(task.Goal), Goal: task.Goal, State: TaskState(task.State),
		PlanRevision: strconv.FormatUint(task.Plan.Revision, 10),
		Steps:        make([]productui.AgentTaskStep, 0, len(task.Plan.Steps)),
	}
	if task.State == agentrun.StateCompleted {
		result.AnswerText = task.Ledger.AnswerText
	}
	for _, step := range task.Plan.Steps {
		result.Steps = append(result.Steps, productui.AgentTaskStep{
			Name: stepName(step), State: strings.ToLower(string(step.State)), Tier: "T" + strconv.Itoa(int(step.Tier)),
		})
		if step.State == agentrun.StepAwaitingApproval {
			result.Approvals = append(result.Approvals, productui.AgentApproval{ID: task.ID + "/" + step.ID, Digest: step.ApprovalDigest, Summary: stepName(step)})
		}
	}
	if task.CurrentStep >= 0 && task.CurrentStep < len(task.Plan.Steps) && result.State != productui.AgentTaskCompleted && result.State != productui.AgentTaskFailed {
		result.LiveStep = stepName(task.Plan.Steps[task.CurrentStep])
	}
	if used, limit, ok := budget.BudgetUsage(task.ID); ok {
		result.BudgetUsed = strconv.FormatInt(used.Steps, 10)
		result.BudgetLimit = strconv.FormatInt(limit.Steps, 10)
	}
	return result
}

func stepName(step agentrun.PlanStep) string {
	if name := strings.TrimSpace(step.SkillID); name != "" {
		return name
	}
	return step.ID
}

const maxTitleRunes = 80

// taskTitle is the first line of the goal, bounded for a list row.
func taskTitle(goal string) string {
	title := strings.TrimSpace(goal)
	if index := strings.IndexAny(title, "\r\n"); index >= 0 {
		title = strings.TrimSpace(title[:index])
	}
	if utf8.RuneCountInString(title) <= maxTitleRunes {
		return title
	}
	runes := []rune(title)
	return strings.TrimSpace(string(runes[:maxTitleRunes-1])) + "…"
}
