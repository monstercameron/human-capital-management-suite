// Package agentsystem is the composition root of the on-behalf-of agent
// system. It owns no policy of its own: it joins the model gateway
// (agentmodel), skill registry (agentskills), delegated credentials
// (agentdelegation), budgets (agentbudget), audit chain (agentaudit),
// egress evaluation (agentegress), system connections (agentconnect),
// prompt-injection containment (agentsecurity) and the durable task state
// machine (agentrun) behind interfaces, so every step of a task passes the
// same gate in the same order. There is no package-level state: everything
// lives on the values built by NewPlatform and Platform.ForTenant.
package agentsystem

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentaudit"
	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/agentconnect"
	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var (
	ErrNotConfigured  = errors.New("agentsystem: platform is not fully configured")
	ErrInvalid        = errors.New("agentsystem: invalid request")
	ErrDenied         = errors.New("agentsystem: step denied by the skill gate")
	ErrUnsupported    = errors.New("agentsystem: step type is not executable by the gate")
	ErrTenantDisabled = errors.New("agentsystem: tenant agents are disabled")
)

// Mode is the AGENT2-001 run mode. ON_BEHALF_OF steps run with the user's
// delegated credential; SPONSORED runs may never reach T3 or T4.
type Mode string

const (
	ModeOnBehalfOf Mode = "ON_BEHALF_OF"
	ModeSponsored  Mode = "SPONSORED"
)

func (m Mode) valid() bool { return m == ModeOnBehalfOf || m == ModeSponsored }

// GrantScoper hands out the tenant-scoped delegation grant store. The
// PostgreSQL store's ForTenant satisfies it directly.
type GrantScoper interface {
	ForTenant(context.Context, values.TenantId) (agentdelegation.GrantStore, error)
}

// TaskScoper hands out the tenant-scoped durable task store.
type TaskScoper interface {
	ForTenant(context.Context, values.TenantId) (agentrun.TaskStore, error)
}

// TaskWorkIdentity is resolved after current delegated authority succeeds.
type TaskWorkIdentity struct {
	TenantID string
	UserID   string
	TaskID   string
	Mode     Mode
	Provider string
	StepType agentrun.StepType
}

// StepAdmission bounds worker occupancy for an executable task checkpoint.
type StepAdmission interface {
	AcquireTaskStep(context.Context, TaskWorkIdentity) (context.Context, func(), error)
}

// ChildTaskAuthority resolves the admitted target's current installation and
// manifest authority for a delegated child at each executable boundary.
type ChildTaskAuthority interface {
	CheckChildTask(context.Context, agentrun.AgentTask, agentdelegation.Grant) error
}

// Config lists every collaborator of the platform. All fields except
// Connections and WebSearchAllowed are required.
type Config struct {
	Skills         *agentskills.Registry
	Budget         *agentbudget.Ledger
	Audit          agentaudit.Store
	Redactor       agentmodel.Redactor
	Egress         *agentegress.Evaluator
	Connections    *agentconnect.Registry
	Grants         GrantScoper
	Tasks          TaskScoper
	Authority      agentdelegation.AuthorityResolver
	Owner          ToolOwner
	ChildAuthority ChildTaskAuthority
	StepAdmission  StepAdmission
	TypedDispatch  agentmodel.TypedModelDispatcher

	// TokenSecret signs step credentials; Audience and Workload are the tool
	// gateway audience and the sender constraint every credential is bound to.
	TokenSecret []byte
	Audience    string
	Workload    string

	// ModelEstimate is the upper-bound reservation for one model call.
	ModelEstimate    agentbudget.Usage
	WebSearchAllowed func(agentskills.SkillRecord) bool
	// WakeGate reports whether a tenant has agents enabled. Disabled checkpoints
	// are paused and approvals voided; resumption after enabling is explicit.
	WakeGate func(ctx context.Context, tenant string) bool
	Clock    func() time.Time
}

// Platform is the composition root. Model and Quarantine are the only model
// entry points; both are SchemaFlux gateways from internal/agentmodel.
type Platform struct {
	cfg        Config
	model      *agentmodel.Gateway
	quarantine *agentmodel.Gateway
}

// NewPlatform validates the configuration and builds the model gateways.
func NewPlatform(cfg Config) (*Platform, error) {
	if cfg.Skills == nil || cfg.Budget == nil || cfg.Audit == nil || cfg.Redactor == nil ||
		cfg.Egress == nil || cfg.Grants == nil || cfg.Tasks == nil || cfg.Authority == nil || cfg.Owner == nil {
		return nil, ErrNotConfigured
	}
	if strings.TrimSpace(cfg.Audience) == "" || strings.TrimSpace(cfg.Workload) == "" || len(cfg.TokenSecret) < 16 {
		return nil, fmt.Errorf("%w: audience, workload and a 16 byte token secret are required", ErrNotConfigured)
	}
	if cfg.ModelEstimate.Steps <= 0 {
		return nil, fmt.Errorf("%w: model estimate needs a positive step count", ErrNotConfigured)
	}
	if cfg.Clock == nil {
		cfg.Clock = func() time.Time { return time.Now().UTC() }
	}
	base := agentmodel.Config{
		Skills: cfg.Skills, Budget: agentmodel.BudgetAdapter{Ledger: cfg.Budget}, Audit: cfg.Audit,
		Redactor: cfg.Redactor, Clock: cfg.Clock, WebSearchAllowed: cfg.WebSearchAllowed,
		TypedDispatch: cfg.TypedDispatch,
	}
	model, err := agentmodel.New(base)
	if err != nil {
		return nil, err
	}
	// Quarantined extraction sees untrusted content, so its gateway is built
	// with an empty tool projection and never any web search.
	base.WebSearchAllowed = nil
	base.Tools = func(agentskills.SkillRecord) []agentmodel.ToolDefinition { return nil }
	quarantine, err := agentmodel.New(base)
	if err != nil {
		return nil, err
	}
	return &Platform{cfg: cfg, model: model, quarantine: quarantine}, nil
}

// Model returns the typed model gateway shared by every agent step.
func (p *Platform) Model() *agentmodel.Gateway { return p.model }

// Runner is the tenant-scoped face of the platform.
type Runner struct {
	p          *Platform
	tenant     values.TenantId
	grants     agentdelegation.GrantStore
	tasks      agentrun.TaskStore
	Delegation *agentdelegation.Service
	Runtime    *agentrun.Runtime
}

// ForTenant binds durable stores, the delegation service and the task
// runtime to one tenant.
func (p *Platform) ForTenant(ctx context.Context, tenant values.TenantId) (*Runner, error) {
	if p == nil {
		return nil, ErrNotConfigured
	}
	if err := tenant.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	grants, err := p.cfg.Grants.ForTenant(ctx, tenant)
	if err != nil {
		return nil, err
	}
	tasks, err := p.cfg.Tasks.ForTenant(ctx, tenant)
	if err != nil {
		return nil, err
	}
	delegation, err := agentdelegation.NewService(agentdelegation.Config{
		Store: grants, Authority: p.cfg.Authority, Secret: p.cfg.TokenSecret, Now: p.cfg.Clock,
	})
	if err != nil {
		return nil, err
	}
	runtime, err := agentrun.NewRuntime(tasks)
	if err != nil {
		return nil, err
	}
	return &Runner{p: p, tenant: tenant, grants: grants, tasks: tasks, Delegation: delegation, Runtime: runtime}, nil
}

// StartRequest is the server-resolved input for a user-started task.
// UserAuthority must come from the current policy decision point.
type StartRequest struct {
	TaskID              string
	UserID              string
	AgentVersion        string
	InstallationID      string
	Purpose             string
	OrganizationScopeID string
	Goal                string
	Constraints         []string
	Steps               []agentrun.PlanStep
	UserAuthority       trust.AuthorityScope
	Limit               agentbudget.Limits
	Lifetime            time.Duration
}

// GrantID is the deterministic delegation grant of one task.
func GrantID(taskID string) string { return "grant:" + taskID }

// StartTask pins the plan's skills, mints the task's delegation grant, opens
// its budget and stores the plan. Steps that declare a lower tier than their
// skill are refused, so a plan cannot under-declare a side effect.
func (r *Runner) StartTask(ctx context.Context, req StartRequest) (agentrun.AgentTask, error) {
	plan, err := agentrun.NewPlan(req.Steps)
	if err != nil {
		return agentrun.AgentTask{}, err
	}
	now := r.p.cfg.Clock().UTC()
	skills, scopes, err := r.pinSkills(plan.Steps)
	if err != nil {
		return agentrun.AgentTask{}, err
	}
	lifetime := req.Lifetime
	if lifetime <= 0 || lifetime > agentrun.MaxTaskLifetime {
		lifetime = agentrun.MaxTaskLifetime
	}
	if _, err := r.Delegation.CreateGrant(agentdelegation.GrantRequest{
		GrantID: GrantID(req.TaskID), UserID: req.UserID, Tenant: r.tenant, AgentVersion: req.AgentVersion,
		InstallationID: req.InstallationID, TaskID: req.TaskID, PlanSkillSetDigest: skillSetDigest(skills),
		Purpose: req.Purpose, OrganizationScopeID: req.OrganizationScopeID, Skills: skillIDs(skills),
		SkillScopes: scopes, NotBefore: now, ExpiresAt: now.Add(lifetime), UserAuthority: req.UserAuthority,
	}); err != nil {
		return agentrun.AgentTask{}, err
	}
	if err := r.p.cfg.Budget.OpenTask(agentbudget.TaskSpec{ID: req.TaskID, TenantID: r.tenant.String(), UserID: req.UserID, Limit: req.Limit}); err != nil {
		_ = r.Delegation.RevokeGrant(GrantID(req.TaskID), "budget open failed")
		return agentrun.AgentTask{}, err
	}
	return r.Runtime.CreateTask(ctx, agentrun.CreateRequest{
		ID: req.TaskID, TenantID: r.tenant.String(), UserID: req.UserID, Goal: req.Goal,
		Constraints: req.Constraints, Plan: plan, Now: now, ExpiresAt: now.Add(lifetime),
	})
}

// Step executes exactly one checkpoint of a confirmed task through the gate.
func (r *Runner) Step(ctx context.Context, taskID string, mode Mode) (agentrun.AgentTask, error) {
	if !mode.valid() {
		return agentrun.AgentTask{}, fmt.Errorf("%w: unknown run mode %q", ErrInvalid, mode)
	}
	ctx, cancel := context.WithTimeout(ctx, stepLeaseTimeout)
	defer cancel()
	task, err := r.Runtime.GetTask(ctx, taskID)
	if err != nil {
		return agentrun.AgentTask{}, err
	}
	if r.p.cfg.WakeGate != nil && !r.p.cfg.WakeGate(ctx, r.tenant.String()) {
		return r.pauseAfterAuthorityDenial(ctx, task, errors.Join(ErrDenied, ErrTenantDisabled))
	}
	task, err = r.recheckTaskLineage(ctx, task)
	if err != nil {
		return task, err
	}
	if parked, ok, err := r.parkIfWait(ctx, task); ok {
		return parked, err
	}
	// A SPONSORED worker that meets a T3 or T4 step is refused without
	// touching the task: the step stays available to an on-behalf-of run.
	if mode == ModeSponsored && task.CurrentStep < len(task.Plan.Steps) && task.Plan.Steps[task.CurrentStep].Tier >= agentrun.TierSubmitGoverned {
		return task, fmt.Errorf("%w: SPONSORED runs cannot reach T3 or T4", ErrDenied)
	}
	return r.Runtime.ExecuteNext(ctx, taskID, task.Version, &executor{runner: r, mode: mode}, r.p.cfg.Owner, r.p.cfg.Clock())
}

// pinSkills resolves every distinct skill of the plan to its current record
// and the capability scopes the grant needs for it.
func (r *Runner) pinSkills(steps []agentrun.PlanStep) ([]agentskills.SkillRecord, map[string][]string, error) {
	seen := map[string]agentskills.SkillRecord{}
	scopes := map[string][]string{}
	for _, step := range steps {
		record, ok := r.p.cfg.Skills.Lookup(agentskills.SkillKey{ID: step.SkillID, Version: step.SkillVersion})
		if !ok || record.Status == agentskills.StatusRetired {
			return nil, nil, fmt.Errorf("%w: skill %s/v%d is not published", ErrDenied, step.SkillID, step.SkillVersion)
		}
		if uint8(step.Tier) < uint8(record.Definition.SideEffectTier) {
			return nil, nil, fmt.Errorf("%w: step %s declares tier T%d below skill tier %s", ErrDenied, step.ID, step.Tier, record.Definition.SideEffectTier)
		}
		if prior, dup := seen[step.SkillID]; dup && prior.Digest != record.Digest {
			return nil, nil, fmt.Errorf("%w: skill %s is pinned at two versions", ErrDenied, step.SkillID)
		}
		seen[step.SkillID] = record
		scopes[step.SkillID] = skillScopes(record)
	}
	records := make([]agentskills.SkillRecord, 0, len(seen))
	for _, record := range seen {
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Definition.ID < records[j].Definition.ID })
	return records, scopes, nil
}

// skillScopes are the capability names a skill may invoke; connection
// operations are scoped to their connection.
func skillScopes(record agentskills.SkillRecord) []string {
	set := map[string]struct{}{}
	for _, op := range record.Definition.Operations {
		if op.Kind == agentskills.OperationConnection {
			set["connection:"+op.ConnectionID+"/"+op.Operation] = struct{}{}
			continue
		}
		set[op.Capability.ID] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for name := range set {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func skillIDs(records []agentskills.SkillRecord) []string {
	ids := make([]string, len(records))
	for i, record := range records {
		ids[i] = record.Definition.ID
	}
	return ids
}

// skillSetDigest binds the grant to the exact skill versions and digests the
// plan was confirmed against.
func skillSetDigest(records []agentskills.SkillRecord) string {
	h := sha256.New()
	for _, record := range records {
		fmt.Fprintf(h, "%s\x00%d\x00%s\x00", record.Definition.ID, record.Definition.Version, record.Digest)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

func digestOf(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		fmt.Fprintf(h, "%s\x00", part)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// Wake delivers a durable wake event to a parked task. Before the task can
// resume, the delegation grant is re-exchanged for the step that will run
// next, which re-resolves the user's current authority: a deactivated user,
// a revoked grant or a changed skill keeps the task parked.
func (r *Runner) Wake(ctx context.Context, taskID string, event agentrun.WakeEvent) (agentrun.WakeResult, error) {
	result, err := r.Runtime.WakeWithRecheck(ctx, taskID, event, wakeRechecker{runner: r})
	if result.Task.ID != "" {
		result.Task, err = r.pauseAfterAuthorityDenial(ctx, result.Task, err)
	}
	return result, err
}

func (r *Runner) pauseAfterAuthorityDenial(ctx context.Context, task agentrun.AgentTask, err error) (agentrun.AgentTask, error) {
	reason := authorityPauseReason(err)
	if reason != "" && task.ID != "" {
		paused, pauseErr := r.Runtime.PauseAuthority(ctx, task.ID, task.Version, reason, r.p.cfg.Clock())
		if pauseErr != nil && !errors.Is(pauseErr, agentrun.ErrConflict) && !errors.Is(pauseErr, agentrun.ErrTerminal) {
			return task, errors.Join(err, pauseErr)
		}
		if paused.ID != "" {
			task = paused
		}
	}
	return task, err
}

func authorityPauseReason(err error) agentrun.AuthorityPauseReason {
	var reason agentrun.AuthorityPauseReason
	switch {
	case errors.Is(err, ErrTenantDisabled):
		reason = agentrun.PauseTenantDisabled
	case errors.Is(err, agentdelegation.ErrUserInactive):
		reason = agentrun.PauseUserInactive
	case errors.Is(err, agentdelegation.ErrGrantRevoked), errors.Is(err, agentdelegation.ErrTokenRevoked):
		reason = agentrun.PauseGrantRevoked
	case errors.Is(err, agentdelegation.ErrGrantExpired):
		reason = agentrun.PauseGrantExpired
	}
	return reason
}

type wakeRechecker struct{ runner *Runner }

func (w wakeRechecker) RecheckWake(ctx context.Context, task agentrun.AgentTask, _ agentrun.WakeEvent) error {
	if gate := w.runner.p.cfg.WakeGate; gate != nil && !gate(ctx, w.runner.tenant.String()) {
		return errors.Join(ErrDenied, ErrTenantDisabled)
	}
	if _, err := w.runner.recheckTaskLineage(ctx, task); err != nil {
		return err
	}
	if task.CurrentStep >= len(task.Plan.Steps) {
		return fmt.Errorf("%w: task has no step to resume", ErrDenied)
	}
	step := task.Plan.Steps[task.CurrentStep]
	e := &executor{runner: w.runner, mode: ModeOnBehalfOf}
	record, grant, err := e.admit(ctx, task, step)
	if err != nil {
		return err
	}
	_, _, err = e.credential(task, step, record, grant)
	return err
}

// UserTasks lists one user's tasks in this tenant, oldest first. It is the
// read side of the task view: a user never sees another user's task, whatever
// the store returns.
func (r *Runner) UserTasks(ctx context.Context, userID string) ([]agentrun.AgentTask, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, fmt.Errorf("%w: user is required", ErrInvalid)
	}
	tasks, err := r.tasks.List(ctx)
	if err != nil {
		return nil, err
	}
	own := tasks[:0:0]
	for _, task := range tasks {
		if task.UserID == userID {
			own = append(own, task)
		}
	}
	return own, nil
}

// BudgetUsage reports the settled usage and ceiling of one task.
func (r *Runner) BudgetUsage(taskID string) (used, limit agentbudget.Limits, ok bool) {
	for _, task := range r.p.cfg.Budget.Snapshot().Tasks {
		if task.ID == taskID {
			return task.Used, task.Limit, true
		}
	}
	return agentbudget.Limits{}, agentbudget.Limits{}, false
}
