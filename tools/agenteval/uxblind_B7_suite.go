// Package agenteval evaluates long-horizon agent tasks against a versioned,
// synthetic-tenant suite. It records the task evidence that a single-turn
// safety evaluation cannot see: completion, deterministic VERIFY checks,
// replanning, approvals, work, wall-clock and spend.
package agenteval

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
)

var (
	ErrInvalid       = errors.New("agenteval: invalid evaluation request")
	ErrThreshold     = errors.New("agenteval: thresholds not met")
	ErrUnsealed      = errors.New("agenteval: evaluation evidence seal is broken")
	ErrNotPublished  = errors.New("agenteval: no passing evaluation is published for release")
	ErrAlreadyStored = errors.New("agenteval: evaluation is already published")
)

// TaskKind is a stable name in the task-suite evidence. Adding a task is a
// suite-version change; existing task names are never silently repurposed.
type TaskKind string

const (
	TaskPromotionPreparation TaskKind = "promotion_preparation"
	TaskOnboardingChecklist  TaskKind = "onboarding_checklist"
	TaskPolicyQuestion       TaskKind = "policy_question_with_citations"
	TaskCrossSystemLookup    TaskKind = "cross_system_lookup"
)

// TaskCase is a synthetic-tenant task. FixtureConnection is descriptive
// metadata for the cross-system task and is not a credential or endpoint.
type TaskCase struct {
	ID                string   `json:"id"`
	Kind              TaskKind `json:"kind"`
	TenantID          string   `json:"tenant_id"`
	Goal              string   `json:"goal"`
	FixtureConnection string   `json:"fixture_connection,omitempty"`
	ApprovalEffect    string   `json:"approval_effect,omitempty"`
}

// Thresholds are release thresholds for one suite version. Zero is a valid
// maximum, so a suite can explicitly require that a task request no approval.
type Thresholds struct {
	MinCompletionRate     float64        `json:"min_completion_rate"`
	MinVerifyPassRate     float64        `json:"min_verify_pass_rate"`
	MaxPlanRevisions      int            `json:"max_plan_revisions"`
	MaxApprovalsPerEffect map[string]int `json:"max_approvals_per_effect"`
	MaxSteps              int            `json:"max_steps"`
	MaxWallClock          time.Duration  `json:"max_wall_clock_ns"`
	MaxCostMicros         int64          `json:"max_cost_micros"`
}

// Suite is immutable after validation. Version is part of the evidence seal.
type Suite struct {
	Version    string     `json:"version"`
	Tasks      []TaskCase `json:"tasks"`
	Thresholds Thresholds `json:"thresholds"`
}

// DefaultSuite returns the reference AGENT2-025 suite. Each task uses its own
// synthetic tenant so a driver cannot accidentally satisfy the suite by
// leaking state between scenarios.
func DefaultSuite() Suite {
	return Suite{
		Version: "agent2-025/v1",
		Tasks: []TaskCase{
			{ID: "promotion-preparation", Kind: TaskPromotionPreparation, TenantID: "synthetic-promotion", Goal: "Prepare a governed promotion batch and verify the proposed subjects.", ApprovalEffect: "T3"},
			{ID: "onboarding-checklist", Kind: TaskOnboardingChecklist, TenantID: "synthetic-onboarding", Goal: "Prepare and verify an onboarding checklist for a new worker."},
			{ID: "policy-question", Kind: TaskPolicyQuestion, TenantID: "synthetic-policy", Goal: "Answer a policy question with citations and verify every citation."},
			{ID: "cross-system-lookup", Kind: TaskCrossSystemLookup, TenantID: "synthetic-connection", Goal: "Look up a worker in the reviewed fixture connection and verify the result.", FixtureConnection: "fixture-hris-v1"},
		},
		Thresholds: Thresholds{
			MinCompletionRate:     1,
			MinVerifyPassRate:     1,
			MaxPlanRevisions:      4,
			MaxApprovalsPerEffect: map[string]int{"T3": 4},
			MaxSteps:              32,
			MaxWallClock:          10 * time.Minute,
			MaxCostMicros:         250_000,
		},
	}
}

// TaskOutcome is the typed observation returned by the long-horizon runtime.
// VERIFY counts are separate so a task with no VERIFY step cannot pass by
// reporting completion alone.
type TaskOutcome struct {
	Completed          bool           `json:"completed"`
	VerifyPassed       int            `json:"verify_passed"`
	VerifyTotal        int            `json:"verify_total"`
	PlanRevisions      int            `json:"plan_revisions"`
	ApprovalsRequested map[string]int `json:"approvals_requested,omitempty"`
	Steps              int            `json:"steps"`
	WallClock          time.Duration  `json:"wall_clock_ns"`
	CostMicros         int64          `json:"cost_micros"`
}

// TaskExecutor is the seam to the real task runner. Implementations should
// execute the pinned plan and report owner-observed VERIFY results, not model
// claims. A failed execution is retained as failed task evidence.
type TaskExecutor interface {
	Execute(context.Context, TaskCase) (TaskOutcome, error)
}

// TaskExecutorFunc adapts a function to TaskExecutor.
type TaskExecutorFunc func(context.Context, TaskCase) (TaskOutcome, error)

func (f TaskExecutorFunc) Execute(ctx context.Context, task TaskCase) (TaskOutcome, error) {
	return f(ctx, task)
}

// TaskResult is the per-task sealed evidence. ErrorCode is deliberately a
// stable class, never a raw prompt, provider response, or credential.
type TaskResult struct {
	Task      TaskCase    `json:"task"`
	Outcome   TaskOutcome `json:"outcome"`
	ErrorCode string      `json:"error_code,omitempty"`
}

// Metrics are the aggregate measurements used by the release gate.
type Metrics struct {
	TotalTasks         int            `json:"total_tasks"`
	CompletedTasks     int            `json:"completed_tasks"`
	CompletionRate     float64        `json:"completion_rate"`
	VerifyPassed       int            `json:"verify_passed"`
	VerifyTotal        int            `json:"verify_total"`
	VerifyPassRate     float64        `json:"verify_pass_rate"`
	PlanRevisions      int            `json:"plan_revisions"`
	ApprovalsRequested map[string]int `json:"approvals_requested,omitempty"`
	Steps              int            `json:"steps"`
	WallClock          time.Duration  `json:"wall_clock_ns"`
	CostMicros         int64          `json:"cost_micros"`
}

// ThresholdResult records each gate decision so a failed release is
// explainable without replaying a provider call.
type ThresholdResult struct {
	CompletionRate bool            `json:"completion_rate"`
	VerifyPassRate bool            `json:"verify_pass_rate"`
	PlanRevisions  bool            `json:"plan_revisions"`
	Approvals      map[string]bool `json:"approvals"`
	Steps          bool            `json:"steps"`
	WallClock      bool            `json:"wall_clock"`
	Cost           bool            `json:"cost"`
	Passed         bool            `json:"passed"`
}

// Run is immutable evaluation evidence. Security is the AGENT-004 sealed
// safety/grounding/tool-selection run; task metrics extend it without changing
// the security package's single-turn contract.
type Run struct {
	SuiteVersion string                `json:"suite_version"`
	Security     agentsecurity.EvalRun `json:"security"`
	Tasks        []TaskResult          `json:"tasks"`
	Metrics      Metrics               `json:"metrics"`
	Thresholds   Thresholds            `json:"thresholds"`
	Threshold    ThresholdResult       `json:"threshold"`
	Passed       bool                  `json:"passed"`
	Digest       string                `json:"digest"`
}

// Request supplies one exact release, the safety fixtures, and a task
// executor. Release identity includes the prompt and model digests through
// agentsecurity.Release, so routing cannot reuse evidence after a change.
type Request struct {
	Release  agentsecurity.Release
	Suite    Suite
	Fixtures []agentsecurity.EvalFixture
	Executor TaskExecutor
}

// Evaluator composes the existing AGENT-004 evaluator with task evidence.
type Evaluator struct {
	security *agentsecurity.Evaluator
}

// NewEvaluator creates a monotonic evaluator with no mutable package state.
func NewEvaluator() *Evaluator {
	return &Evaluator{security: agentsecurity.NewEvaluator()}
}

// Evaluate runs every task and seals both safety and long-horizon evidence.
// Task failures are recorded and make the run fail; they do not discard the
// evidence needed to diagnose a release candidate.
func (e *Evaluator) Evaluate(ctx context.Context, req Request) (Run, error) {
	if e == nil || e.security == nil {
		return Run{}, fmt.Errorf("%w: nil evaluator", ErrInvalid)
	}
	if err := validateRequest(req); err != nil {
		return Run{}, err
	}
	if err := contextErr(ctx); err != nil {
		return Run{}, err
	}
	securityRun, err := e.security.Evaluate(req.Release, req.Fixtures)
	if err != nil {
		return Run{}, err
	}
	results := make([]TaskResult, 0, len(req.Suite.Tasks))
	for _, task := range req.Suite.Tasks {
		if err := contextErr(ctx); err != nil {
			return Run{}, err
		}
		outcome, execErr := req.Executor.Execute(ctx, task)
		result := TaskResult{Task: cloneTask(task), Outcome: cloneOutcome(outcome)}
		if execErr != nil {
			result.ErrorCode = errorCode(execErr)
			result.Outcome = TaskOutcome{}
		} else if err := validateOutcome(outcome); err != nil {
			result.ErrorCode = "invalid_outcome"
			result.Outcome = TaskOutcome{}
		}
		results = append(results, result)
	}

	metrics := aggregate(results)
	threshold := checkThresholds(req.Suite.Thresholds, metrics)
	passed := securityRun.Passed && threshold.Passed
	run := Run{
		SuiteVersion: req.Suite.Version,
		Security:     cloneSecurityRun(securityRun),
		Tasks:        cloneResults(results),
		Metrics:      cloneMetrics(metrics),
		Thresholds:   cloneThresholds(req.Suite.Thresholds),
		Threshold:    cloneThresholdResult(threshold),
		Passed:       passed,
	}
	run.Digest = digest(run)
	return run, nil
}

// Verify recomputes the evidence seal and the embedded AGENT-004 seal.
func (r Run) Verify() error {
	if err := r.Security.Verify(); err != nil {
		return err
	}
	if r.Digest == "" || digest(r) != r.Digest {
		return ErrUnsealed
	}
	return nil
}

// MarshalEvidence emits deterministic evidence bytes, including the seal.
func (r Run) MarshalEvidence() ([]byte, error) { return json.Marshal(r) }

// Publisher gates both publication and routing on a passing, sealed run.
// The embedded AGENT-004 publisher preserves the existing single-turn gate.
type Publisher struct {
	mu       sync.Mutex
	security *agentsecurity.Publisher
	runs     map[string]Run
	routes   map[string]string
}

// NewPublisher returns an empty release gate.
func NewPublisher() *Publisher {
	return &Publisher{security: agentsecurity.NewPublisher(), runs: make(map[string]Run), routes: make(map[string]string)}
}

// Publish stores one passing run and makes its exact release routable.
func (p *Publisher) Publish(run Run) error {
	if p == nil || p.security == nil {
		return fmt.Errorf("%w: nil publisher", ErrInvalid)
	}
	if err := run.Verify(); err != nil {
		return err
	}
	if !run.Passed {
		return ErrThreshold
	}
	if err := p.security.Publish(run.Security); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, exists := p.runs[run.Digest]; exists {
		return ErrAlreadyStored
	}
	key := routeKey(run.SuiteVersion, run.Security.Release)
	p.runs[run.Digest] = cloneRun(run)
	p.routes[key] = run.Digest
	return nil
}

// Route returns the passing evidence for an exact suite and release. A model,
// prompt, tool, or agent version change therefore requires a new run.
func (p *Publisher) Route(suiteVersion string, release agentsecurity.Release) (Run, error) {
	if p == nil {
		return Run{}, ErrNotPublished
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	digest, ok := p.routes[routeKey(suiteVersion, release)]
	if !ok {
		return Run{}, ErrNotPublished
	}
	run := p.runs[digest]
	return cloneRun(run), nil
}

// Published reports whether this exact evidence digest was stored.
func (p *Publisher) Published(digest string) bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	_, ok := p.runs[digest]
	return ok
}

func validateRequest(req Request) error {
	if req.Executor == nil {
		return fmt.Errorf("%w: executor is required", ErrInvalid)
	}
	if strings.TrimSpace(req.Suite.Version) == "" || len(req.Suite.Tasks) == 0 {
		return fmt.Errorf("%w: suite version and tasks are required", ErrInvalid)
	}
	if err := validateThresholds(req.Suite.Thresholds); err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(req.Suite.Tasks))
	for _, task := range req.Suite.Tasks {
		if strings.TrimSpace(task.ID) == "" || strings.TrimSpace(task.TenantID) == "" || task.Kind == "" || strings.TrimSpace(task.Goal) == "" {
			return fmt.Errorf("%w: task id, kind, tenant and goal are required", ErrInvalid)
		}
		if _, ok := seen[task.ID]; ok {
			return fmt.Errorf("%w: duplicate task %q", ErrInvalid, task.ID)
		}
		seen[task.ID] = struct{}{}
	}
	return nil
}

func validateThresholds(t Thresholds) error {
	if t.MinCompletionRate < 0 || t.MinCompletionRate > 1 || t.MinVerifyPassRate < 0 || t.MinVerifyPassRate > 1 || t.MaxPlanRevisions < 0 || t.MaxSteps <= 0 || t.MaxWallClock <= 0 || t.MaxCostMicros < 0 {
		return fmt.Errorf("%w: threshold bounds are invalid", ErrInvalid)
	}
	for effect, limit := range t.MaxApprovalsPerEffect {
		if strings.TrimSpace(effect) == "" || limit < 0 {
			return fmt.Errorf("%w: approval threshold is invalid", ErrInvalid)
		}
	}
	return nil
}

func validateOutcome(o TaskOutcome) error {
	if o.VerifyTotal <= 0 || o.VerifyPassed < 0 || o.VerifyPassed > o.VerifyTotal || o.PlanRevisions < 0 || o.Steps <= 0 || o.WallClock < 0 || o.CostMicros < 0 {
		return ErrInvalid
	}
	for effect, count := range o.ApprovalsRequested {
		if strings.TrimSpace(effect) == "" || count < 0 {
			return ErrInvalid
		}
	}
	return nil
}

func aggregate(results []TaskResult) Metrics {
	m := Metrics{TotalTasks: len(results), ApprovalsRequested: make(map[string]int)}
	for _, result := range results {
		if result.ErrorCode == "" && result.Outcome.Completed {
			m.CompletedTasks++
		}
		m.VerifyPassed += result.Outcome.VerifyPassed
		m.VerifyTotal += result.Outcome.VerifyTotal
		m.PlanRevisions += result.Outcome.PlanRevisions
		m.Steps += result.Outcome.Steps
		m.WallClock += result.Outcome.WallClock
		m.CostMicros += result.Outcome.CostMicros
		for effect, count := range result.Outcome.ApprovalsRequested {
			m.ApprovalsRequested[effect] += count
		}
	}
	if m.TotalTasks > 0 {
		m.CompletionRate = float64(m.CompletedTasks) / float64(m.TotalTasks)
	}
	if m.VerifyTotal > 0 {
		m.VerifyPassRate = float64(m.VerifyPassed) / float64(m.VerifyTotal)
	}
	return m
}

func checkThresholds(t Thresholds, m Metrics) ThresholdResult {
	r := ThresholdResult{
		CompletionRate: m.CompletionRate >= t.MinCompletionRate,
		VerifyPassRate: m.VerifyPassRate >= t.MinVerifyPassRate,
		PlanRevisions:  m.PlanRevisions <= t.MaxPlanRevisions,
		Approvals:      make(map[string]bool),
		Steps:          m.Steps <= t.MaxSteps,
		WallClock:      m.WallClock <= t.MaxWallClock,
		Cost:           m.CostMicros <= t.MaxCostMicros,
	}
	for effect, limit := range t.MaxApprovalsPerEffect {
		r.Approvals[effect] = m.ApprovalsRequested[effect] <= limit
	}
	for effect := range m.ApprovalsRequested {
		if _, known := t.MaxApprovalsPerEffect[effect]; !known {
			r.Approvals[effect] = false
		}
	}
	r.Passed = r.CompletionRate && r.VerifyPassRate && r.PlanRevisions && r.Steps && r.WallClock && r.Cost
	for _, ok := range r.Approvals {
		r.Passed = r.Passed && ok
	}
	return r
}

type canonicalApproval struct {
	Effect string `json:"effect"`
	Count  int    `json:"count"`
}

type canonicalOutcome struct {
	Completed          bool                `json:"completed"`
	VerifyPassed       int                 `json:"verify_passed"`
	VerifyTotal        int                 `json:"verify_total"`
	PlanRevisions      int                 `json:"plan_revisions"`
	ApprovalsRequested []canonicalApproval `json:"approvals_requested,omitempty"`
	Steps              int                 `json:"steps"`
	WallClock          time.Duration       `json:"wall_clock_ns"`
	CostMicros         int64               `json:"cost_micros"`
}

type canonicalMetricValues struct {
	TotalTasks         int                 `json:"total_tasks"`
	CompletedTasks     int                 `json:"completed_tasks"`
	CompletionRate     float64             `json:"completion_rate"`
	VerifyPassed       int                 `json:"verify_passed"`
	VerifyTotal        int                 `json:"verify_total"`
	VerifyPassRate     float64             `json:"verify_pass_rate"`
	PlanRevisions      int                 `json:"plan_revisions"`
	ApprovalsRequested []canonicalApproval `json:"approvals_requested,omitempty"`
	Steps              int                 `json:"steps"`
	WallClock          time.Duration       `json:"wall_clock_ns"`
	CostMicros         int64               `json:"cost_micros"`
}

type canonicalThresholdValues struct {
	MinCompletionRate     float64             `json:"min_completion_rate"`
	MinVerifyPassRate     float64             `json:"min_verify_pass_rate"`
	MaxPlanRevisions      int                 `json:"max_plan_revisions"`
	MaxApprovalsPerEffect []canonicalApproval `json:"max_approvals_per_effect,omitempty"`
	MaxSteps              int                 `json:"max_steps"`
	MaxWallClock          time.Duration       `json:"max_wall_clock_ns"`
	MaxCostMicros         int64               `json:"max_cost_micros"`
}

type canonicalBool struct {
	Key   string `json:"key"`
	Value bool   `json:"value"`
}

type canonicalThresholdResult struct {
	CompletionRate bool            `json:"completion_rate"`
	VerifyPassRate bool            `json:"verify_pass_rate"`
	PlanRevisions  bool            `json:"plan_revisions"`
	Approvals      []canonicalBool `json:"approvals"`
	Steps          bool            `json:"steps"`
	WallClock      bool            `json:"wall_clock"`
	Cost           bool            `json:"cost"`
	Passed         bool            `json:"passed"`
}

type canonicalResult struct {
	Task      TaskCase         `json:"task"`
	Outcome   canonicalOutcome `json:"outcome"`
	ErrorCode string           `json:"error_code,omitempty"`
}

type canonicalRun struct {
	SuiteVersion string                   `json:"suite_version"`
	Security     agentsecurity.EvalRun    `json:"security"`
	Tasks        []canonicalResult        `json:"tasks"`
	Metrics      canonicalMetricValues    `json:"metrics"`
	Thresholds   canonicalThresholdValues `json:"thresholds"`
	Threshold    canonicalThresholdResult `json:"threshold"`
	Passed       bool                     `json:"passed"`
}

func digest(run Run) string {
	canonical := canonicalRun{SuiteVersion: run.SuiteVersion, Security: run.Security, Tasks: canonicalResults(run.Tasks), Metrics: canonicalMetricsValue(run.Metrics), Thresholds: canonicalThresholdsValue(run.Thresholds), Threshold: canonicalThresholdValue(run.Threshold), Passed: run.Passed}
	b, _ := json.Marshal(canonical)
	sum := sha256.Sum256(append([]byte("hcm-next-agent-task-eval/v1\x00"), b...))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func routeKey(suite string, release agentsecurity.Release) string {
	b, _ := json.Marshal(struct {
		Suite          string `json:"suite"`
		Agent          string `json:"agent"`
		Build          string `json:"build"`
		AgentVersion   string `json:"agent_version"`
		PersonaID      string `json:"persona_id"`
		PersonaVersion string `json:"persona_version"`
		InstallationID string `json:"installation_id"`
		Model          string `json:"model"`
		ModelDigest    string `json:"model_digest"`
		Tool           string `json:"tool"`
		ToolVersion    uint32 `json:"tool_version"`
		PromptHash     string `json:"prompt_hash"`
	}{suite, release.Agent, release.AgentBuild, release.AgentVersion, release.PersonaID, release.PersonaVersion, release.InstallationID, release.Model, release.ModelDigest, release.Tool, release.ToolVersion, release.PromptHash})
	return string(b)
}

func errorCode(err error) string {
	if err == nil {
		return ""
	}
	return "executor_error"
}

func contextErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func cloneTask(t TaskCase) TaskCase { return t }

func cloneOutcome(o TaskOutcome) TaskOutcome {
	o.ApprovalsRequested = cloneIntMap(o.ApprovalsRequested)
	return o
}

func cloneResults(in []TaskResult) []TaskResult {
	out := make([]TaskResult, len(in))
	for i, result := range in {
		out[i] = result
		out[i].Outcome = cloneOutcome(result.Outcome)
	}
	return out
}

func cloneMetrics(m Metrics) Metrics {
	m.ApprovalsRequested = cloneIntMap(m.ApprovalsRequested)
	return m
}

func cloneThresholds(t Thresholds) Thresholds {
	t.MaxApprovalsPerEffect = cloneIntMap(t.MaxApprovalsPerEffect)
	return t
}

func cloneThresholdResult(t ThresholdResult) ThresholdResult {
	t.Approvals = cloneBoolMap(t.Approvals)
	return t
}

func cloneSecurityRun(r agentsecurity.EvalRun) agentsecurity.EvalRun {
	r.Fixtures = append([]agentsecurity.EvalFixture(nil), r.Fixtures...)
	return r
}

func cloneRun(r Run) Run {
	r.Security = cloneSecurityRun(r.Security)
	r.Tasks = cloneResults(r.Tasks)
	r.Metrics = cloneMetrics(r.Metrics)
	r.Thresholds = cloneThresholds(r.Thresholds)
	r.Threshold = cloneThresholdResult(r.Threshold)
	return r
}

func cloneIntMap(in map[string]int) map[string]int {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]int, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneBoolMap(in map[string]bool) map[string]bool {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]bool, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func canonicalApprovals(in map[string]int) []canonicalApproval {
	keys := make([]string, 0, len(in))
	for key := range in {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]canonicalApproval, 0, len(keys))
	for _, key := range keys {
		out = append(out, canonicalApproval{Effect: key, Count: in[key]})
	}
	return out
}

func canonicalOutcomeOf(o TaskOutcome) canonicalOutcome {
	return canonicalOutcome{Completed: o.Completed, VerifyPassed: o.VerifyPassed, VerifyTotal: o.VerifyTotal, PlanRevisions: o.PlanRevisions, ApprovalsRequested: canonicalApprovals(o.ApprovalsRequested), Steps: o.Steps, WallClock: o.WallClock, CostMicros: o.CostMicros}
}

func canonicalResults(in []TaskResult) []canonicalResult {
	out := make([]canonicalResult, len(in))
	for i, result := range in {
		out[i] = canonicalResult{Task: result.Task, Outcome: canonicalOutcomeOf(result.Outcome), ErrorCode: result.ErrorCode}
	}
	return out
}

func canonicalMetricsValue(m Metrics) canonicalMetricValues {
	return canonicalMetricValues{
		TotalTasks: m.TotalTasks, CompletedTasks: m.CompletedTasks, CompletionRate: m.CompletionRate,
		VerifyPassed: m.VerifyPassed, VerifyTotal: m.VerifyTotal, VerifyPassRate: m.VerifyPassRate,
		PlanRevisions: m.PlanRevisions, ApprovalsRequested: canonicalApprovals(m.ApprovalsRequested),
		Steps: m.Steps, WallClock: m.WallClock, CostMicros: m.CostMicros,
	}
}

func canonicalThresholdsValue(t Thresholds) canonicalThresholdValues {
	return canonicalThresholdValues{
		MinCompletionRate: t.MinCompletionRate, MinVerifyPassRate: t.MinVerifyPassRate,
		MaxPlanRevisions: t.MaxPlanRevisions, MaxApprovalsPerEffect: canonicalApprovals(t.MaxApprovalsPerEffect),
		MaxSteps: t.MaxSteps, MaxWallClock: t.MaxWallClock, MaxCostMicros: t.MaxCostMicros,
	}
}

func canonicalThresholdValue(t ThresholdResult) canonicalThresholdResult {
	keys := make([]string, 0, len(t.Approvals))
	for key := range t.Approvals {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	approvals := make([]canonicalBool, 0, len(keys))
	for _, key := range keys {
		approvals = append(approvals, canonicalBool{Key: key, Value: t.Approvals[key]})
	}
	return canonicalThresholdResult{CompletionRate: t.CompletionRate, VerifyPassRate: t.VerifyPassRate, PlanRevisions: t.PlanRevisions, Approvals: approvals, Steps: t.Steps, WallClock: t.WallClock, Cost: t.Cost, Passed: t.Passed}
}
