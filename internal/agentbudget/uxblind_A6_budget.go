// Package agentbudget owns hierarchical resource admission for long-horizon
// agent tasks. It is deliberately independent of model, skill, and storage
// implementations: callers reserve an upper bound before work, then settle
// or release that reservation.
package agentbudget

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalid              = errors.New("agentbudget: invalid request")
	ErrTaskExists           = errors.New("agentbudget: task already exists")
	ErrTaskNotFound         = errors.New("agentbudget: task not found")
	ErrPaused               = errors.New("agentbudget: task is paused")
	ErrReservationClosed    = errors.New("agentbudget: reservation is closed")
	ErrReservationExceeded  = errors.New("agentbudget: actual usage exceeded reservation")
	ErrExtensionUnavailable = errors.New("agentbudget: extension is unavailable")
	ErrExtensionConflict    = errors.New("agentbudget: extension request conflicts with a replay")
	ErrStaleRevision        = errors.New("agentbudget: stale task revision")
	ErrExtensionReserved    = errors.New("agentbudget: active reservation blocks extension")
	ErrExtensionOverflow    = errors.New("agentbudget: extension overflows a limit")
)

// Scope identifies the level that stopped admission.
type Scope string

const (
	ScopeTask   Scope = "TASK"
	ScopeUser   Scope = "USER_DAILY"
	ScopeTenant Scope = "TENANT_MONTHLY"
)

// PauseReason is stable, typed state suitable for task projections and
// user-facing pause cards. It never contains provider or prompt content.
type PauseReason string

const (
	PauseTaskSteps       PauseReason = "TASK_STEP_CEILING"
	PauseTaskTokens      PauseReason = "TASK_TOKEN_CEILING"
	PauseTaskWallClock   PauseReason = "TASK_WALL_CLOCK_CEILING"
	PauseTaskSpend       PauseReason = "TASK_SPEND_CEILING"
	PauseUserSteps       PauseReason = "USER_DAILY_STEP_CEILING"
	PauseUserTokens      PauseReason = "USER_DAILY_TOKEN_CEILING"
	PauseUserWallClock   PauseReason = "USER_DAILY_WALL_CLOCK_CEILING"
	PauseUserSpend       PauseReason = "USER_DAILY_SPEND_CEILING"
	PauseTenantSteps     PauseReason = "TENANT_MONTHLY_STEP_CEILING"
	PauseTenantTokens    PauseReason = "TENANT_MONTHLY_TOKEN_CEILING"
	PauseTenantWallClock PauseReason = "TENANT_MONTHLY_WALL_CLOCK_CEILING"
	PauseTenantSpend     PauseReason = "TENANT_MONTHLY_SPEND_CEILING"
	PauseRetryLimit      PauseReason = "STEP_RETRY_CEILING"
	PauseLoopDetected    PauseReason = "IDENTICAL_FAILURE_LOOP"
)

// Limits is a budget in integer, deterministic units. SpendMicros is a
// currency minor-unit value scaled by 1,000,000 (for example, USD cents are
// represented by their decimal-million equivalent).
type Limits struct {
	Steps       int64         `json:"steps"`
	Tokens      int64         `json:"tokens"`
	WallClock   time.Duration `json:"wall_clock_ns"`
	SpendMicros int64         `json:"spend_micros"`
}

// Usage is the amount reserved or finally recorded for one call.
type Usage = Limits

func (l Limits) valid() bool {
	return l.Steps >= 0 && l.Tokens >= 0 && l.WallClock >= 0 && l.SpendMicros >= 0
}

func (l Limits) positive() bool {
	return l.Steps > 0 && l.Tokens > 0 && l.WallClock > 0 && l.SpendMicros > 0
}

func (l Limits) add(other Limits) Limits {
	return Limits{Steps: addInt64(l.Steps, other.Steps), Tokens: addInt64(l.Tokens, other.Tokens), WallClock: time.Duration(addInt64(int64(l.WallClock), int64(other.WallClock))), SpendMicros: addInt64(l.SpendMicros, other.SpendMicros)}
}

func addInt64(left, right int64) int64 {
	if right > 0 && left > int64(^uint64(0)>>1)-right {
		return int64(^uint64(0) >> 1)
	}
	return left + right
}

func (l Limits) sub(other Limits) Limits {
	return Limits{Steps: l.Steps - other.Steps, Tokens: l.Tokens - other.Tokens, WallClock: l.WallClock - other.WallClock, SpendMicros: l.SpendMicros - other.SpendMicros}
}

func (l Limits) exceeds(limit Limits) bool {
	return l.Steps > limit.Steps || l.Tokens > limit.Tokens || l.WallClock > limit.WallClock || l.SpendMicros > limit.SpendMicros
}

func (l Limits) any() bool {
	return l.Steps != 0 || l.Tokens != 0 || l.WallClock != 0 || l.SpendMicros != 0
}

// Policy supplies the three hierarchical ceilings. A zero or negative
// dimension is rejected so a tenant can never silently run without a
// per-user limit. MaxRetries means retries after the first attempt.
type Policy struct {
	TaskDefault     Limits
	UserDaily       Limits
	TenantMonthly   Limits
	ExtensionPolicy ExtensionPolicy
	MaxRetries      int
	LoopThreshold   int
	Backoff         func(retry int) time.Duration
}

// ExtensionPolicy bounds a user-accepted increase independently of the
// immutable tenant ceiling. A zero MaxAdditional disables extensions.
type ExtensionPolicy struct {
	MaxAdditional Limits
}

func (p Policy) validate() error {
	if !p.TaskDefault.positive() || !p.UserDaily.positive() || !p.TenantMonthly.positive() {
		return fmt.Errorf("%w: all task, user-daily and tenant-monthly limits must be positive", ErrInvalid)
	}
	if !p.ExtensionPolicy.MaxAdditional.valid() {
		return fmt.Errorf("%w: extension policy limits must be non-negative", ErrInvalid)
	}
	if p.MaxRetries < 0 || p.LoopThreshold < 1 {
		return fmt.Errorf("%w: retry and loop thresholds are invalid", ErrInvalid)
	}
	return nil
}

func (p Policy) normalized() Policy {
	if p.MaxRetries == 0 {
		p.MaxRetries = 3
	}
	if p.LoopThreshold == 0 {
		p.LoopThreshold = 3
	}
	if p.Backoff == nil {
		p.Backoff = func(retry int) time.Duration {
			if retry < 1 {
				return 0
			}
			return 50 * time.Millisecond * time.Duration(1<<(retry-1))
		}
	}
	return p
}

// TaskSpec registers one user's task and its task-local ceiling.
type TaskSpec struct {
	ID       string
	TenantID string
	UserID   string
	Limit    Limits
	// ParentTaskID and RootTaskID identify delegated work. They are empty for
	// root tasks; child tasks are created only through OpenChildTask.
	ParentTaskID string
	RootTaskID   string
	Depth        int
}

// Task is retained as a concise alias for callers creating a task.
type Task = TaskSpec

// Request describes one model or skill call. Fingerprint must be a stable
// digest or opaque key; raw prompts and arguments are intentionally absent.
type Request struct {
	TaskID      string
	StepID      string
	Fingerprint string
	Estimate    Usage
}

// ExtensionCard is safe to render: it contains only typed limits and IDs.
type ExtensionCard struct {
	ID            string      `json:"id"`
	TaskID        string      `json:"task_id"`
	Scope         Scope       `json:"scope"`
	Reason        PauseReason `json:"reason"`
	MaxAdditional Limits      `json:"max_additional"`
	CanAccept     bool        `json:"can_accept"`
	Message       string      `json:"message"`
}

// PauseError is returned when work must stop. Card is always populated so a
// task projection can offer an explicit user action without guessing why it
// stopped.
type PauseError struct {
	Reason PauseReason
	Scope  Scope
	TaskID string
	Card   ExtensionCard
}

func (e *PauseError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("agentbudget: task %s paused: %s", e.TaskID, e.Reason)
}

func (e *PauseError) Unwrap() error { return ErrPaused }

// Reservation is the only handle that can settle or release a pre-call
// reservation. It is safe to close once; a second close returns a typed error.
type Reservation struct {
	ledger       *Ledger
	ID           string
	TaskID       string
	StepID       string
	Fingerprint  string
	Estimate     Usage
	Attempt      int
	Backoff      time.Duration
	userPeriod   string
	tenantPeriod string
	rootTaskID   string
	ancestorIDs  []string
}

// Service is the seam consumed by model and run orchestration. Implementations
// may be durable, but callers always retain the reserve-before-work contract.
type Service interface {
	OpenTask(TaskSpec) error
	Reserve(context.Context, Request) (*Reservation, error)
	AcceptExtension(ExtensionRequest) error
}

// Settle records actual usage, which must never exceed the reserved upper
// bound. Unused capacity is released atomically with the recorded usage.
func (r *Reservation) Settle(actual Usage) error {
	if r == nil || r.ledger == nil {
		return ErrReservationClosed
	}
	return r.ledger.settle(r, actual)
}

// Release abandons the reservation without recording spend or tokens.
func (r *Reservation) Release() error {
	if r == nil || r.ledger == nil {
		return ErrReservationClosed
	}
	return r.ledger.release(r)
}

// Fail abandons the reservation and records a failed attempt. It returns a
// PauseError once the retry or identical-failure loop ceiling is reached.
func (r *Reservation) Fail() error {
	if r == nil || r.ledger == nil {
		return ErrReservationClosed
	}
	return r.ledger.fail(r)
}

type taskState struct {
	spec     TaskSpec
	Revision uint64
	used     Usage
	reserved Usage
	paused   *PauseError
	attempts map[string]int
	failures map[string]int
}

type extensionReplay struct {
	TaskID           string
	RequestID        string
	ExpectedRevision uint64
	Additional       Limits
	Result           ExtensionResult
}

type periodState struct {
	used     Usage
	reserved Usage
}

type reservationState struct {
	reservation *Reservation
	closed      bool
}

// Ledger is a race-safe in-memory budget ledger. A durable adapter can replay
// the same typed transitions without changing agentmodel or agentrun callers.
type Ledger struct {
	mu           sync.Mutex
	policy       Policy
	now          func() time.Time
	sequence     uint64
	tasks        map[string]*taskState
	users        map[string]*periodState
	tenants      map[string]*periodState
	reservations map[string]*reservationState
	replays      map[string]extensionReplay
	persister    Persister
}

var _ Service = (*Ledger)(nil)

// New creates a ledger using time.Now. Tests and durable compositions should
// use NewWithClock to make period boundaries deterministic.
func New(policy Policy) (*Ledger, error) { return NewWithClock(policy, time.Now) }

// NewWithClock creates a ledger with an injected clock.
func NewWithClock(policy Policy, clock func() time.Time) (*Ledger, error) {
	policy = policy.normalized()
	if err := policy.validate(); err != nil {
		return nil, err
	}
	if clock == nil {
		return nil, fmt.Errorf("%w: nil clock", ErrInvalid)
	}
	return &Ledger{policy: policy, now: clock, tasks: make(map[string]*taskState), users: make(map[string]*periodState), tenants: make(map[string]*periodState), reservations: make(map[string]*reservationState), replays: make(map[string]extensionReplay)}, nil
}

// OpenTask registers a task. The task limit is explicit; an empty limit uses
// the policy default. User and tenant identity are mandatory for every task.
func (l *Ledger) OpenTask(spec TaskSpec) error {
	if l == nil {
		return ErrInvalid
	}
	if spec.ID == "" || spec.TenantID == "" || spec.UserID == "" {
		return fmt.Errorf("%w: task, tenant and user IDs are required", ErrInvalid)
	}
	if !spec.Limit.any() {
		spec.Limit = l.policy.TaskDefault
	}
	if !spec.Limit.positive() {
		return fmt.Errorf("%w: task limit must be positive", ErrInvalid)
	}
	if spec.Limit.exceeds(l.policy.TenantMonthly) {
		return fmt.Errorf("%w: task limit exceeds the tenant ceiling", ErrInvalid)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.tasks[spec.ID]; ok {
		return ErrTaskExists
	}
	created := &taskState{spec: spec, Revision: 1, attempts: make(map[string]int), failures: make(map[string]int)}
	if err := l.persistLocked(Transition{Kind: TransitionOpenTask, Task: durableTask(created), ResultRevision: created.Revision}); err != nil {
		return err
	}
	l.tasks[spec.ID] = created
	return nil
}

// OpenChildTask registers bounded delegated work under parentTaskID. The
// child inherits the parent's tenant and user identity and shares the root
// task ceiling, while retaining its own task-local ceiling.
func (l *Ledger) OpenChildTask(spec TaskSpec, parentTaskID string) error {
	if l == nil || parentTaskID == "" || spec.ID == "" {
		return fmt.Errorf("%w: child and parent task IDs are required", ErrInvalid)
	}
	l.mu.Lock()
	parent, ok := l.tasks[parentTaskID]
	if !ok {
		l.mu.Unlock()
		return ErrTaskNotFound
	}
	if parentTaskID == spec.ID || parent.spec.Depth >= 16 {
		l.mu.Unlock()
		return fmt.Errorf("%w: delegation depth exceeded", ErrInvalid)
	}
	if spec.TenantID != "" && spec.TenantID != parent.spec.TenantID || spec.UserID != "" && spec.UserID != parent.spec.UserID {
		l.mu.Unlock()
		return fmt.Errorf("%w: child identity must match parent", ErrInvalid)
	}
	spec.TenantID, spec.UserID = parent.spec.TenantID, parent.spec.UserID
	spec.ParentTaskID = parentTaskID
	spec.RootTaskID = parent.spec.RootTaskID
	if spec.RootTaskID == "" {
		spec.RootTaskID = parentTaskID
	}
	spec.Depth = parent.spec.Depth + 1
	if !spec.Limit.any() {
		spec.Limit = parent.spec.Limit
	}
	if !spec.Limit.positive() || spec.Limit.exceeds(parent.spec.Limit) || spec.Limit.exceeds(l.policy.TenantMonthly) {
		l.mu.Unlock()
		return fmt.Errorf("%w: invalid child task limit", ErrInvalid)
	}
	if _, exists := l.tasks[spec.ID]; exists {
		l.mu.Unlock()
		return ErrTaskExists
	}
	created := &taskState{spec: spec, Revision: 1, attempts: make(map[string]int), failures: make(map[string]int)}
	if err := l.persistLocked(Transition{Kind: TransitionOpenTask, Task: durableTask(created), ResultRevision: created.Revision}); err != nil {
		l.mu.Unlock()
		return err
	}
	l.tasks[spec.ID] = created
	l.mu.Unlock()
	return nil
}

// Reserve admits a call only after reserving its complete estimate at task,
// user-day, and tenant-month levels. No provider work may begin before this
// method succeeds.
func (l *Ledger) Reserve(ctx context.Context, req Request) (*Reservation, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	if l == nil || req.TaskID == "" || req.StepID == "" || req.Fingerprint == "" || !req.Estimate.valid() || req.Estimate.Steps <= 0 {
		return nil, fmt.Errorf("%w: task, step, fingerprint and non-negative estimate with a positive step count are required", ErrInvalid)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	task, ok := l.tasks[req.TaskID]
	if !ok {
		return nil, ErrTaskNotFound
	}
	if task.paused != nil {
		return nil, clonePause(task.paused)
	}
	stepKey := req.StepID
	attempt := task.attempts[stepKey] + 1
	if attempt > l.policy.MaxRetries+1 {
		return nil, l.recordPauseLocked(task, l.pauseLocked(task, PauseRetryLimit, ScopeTask, Limits{}, task.spec.Limit))
	}
	failureKey := stepKey + "\x00" + req.Fingerprint
	if task.failures[failureKey] >= l.policy.LoopThreshold {
		return nil, l.recordPauseLocked(task, l.pauseLocked(task, PauseLoopDetected, ScopeTask, Limits{}, task.spec.Limit))
	}
	now := l.now().UTC()
	userPeriod := userPeriodKey(task.spec.UserID, now)
	tenantPeriod := tenantPeriodKey(task.spec.TenantID, now)
	if pause := l.checkCapacityLocked(task, req.Estimate, now); pause != nil {
		return nil, l.recordPauseLocked(task, pause)
	}
	ancestors, err := l.ancestorChainLocked(task)
	if err != nil {
		return nil, err
	}
	for _, ancestor := range ancestors {
		if reason := capacityReason(ancestor.used, ancestor.reserved, req.Estimate, ancestor.spec.Limit, ScopeTask); reason != "" {
			return nil, l.recordPauseLocked(task, l.pauseLocked(task, reason, ScopeTask, ancestor.used.add(ancestor.reserved).add(req.Estimate), ancestor.spec.Limit))
		}
	}
	// The attempt counter is durable before any work starts, so a crash loop
	// cannot dodge the retry ceiling. The reserved amount is not durable.
	counted := cloneTaskState(task)
	counted.attempts[stepKey] = attempt
	if err := advanceRevision(counted); err != nil {
		return nil, err
	}
	if err := l.persistLocked(Transition{Kind: TransitionReserve, At: now, Task: durableTask(counted), StepID: req.StepID, ExpectedRevision: task.Revision, ResultRevision: counted.Revision}); err != nil {
		return nil, err
	}
	l.sequence++
	id := fmt.Sprintf("reservation-%08d", l.sequence)
	ancestorIDs := make([]string, 0, len(ancestors))
	for _, ancestor := range ancestors {
		ancestorIDs = append(ancestorIDs, ancestor.spec.ID)
	}
	reservation := &Reservation{ledger: l, ID: id, TaskID: req.TaskID, StepID: req.StepID, Fingerprint: req.Fingerprint, Estimate: req.Estimate, Attempt: attempt, Backoff: l.policy.Backoff(attempt - 1), userPeriod: userPeriod, tenantPeriod: tenantPeriod, rootTaskID: task.spec.RootTaskID, ancestorIDs: ancestorIDs}
	l.reservations[id] = &reservationState{reservation: reservation}
	task.reserved = task.reserved.add(req.Estimate)
	for _, ancestor := range ancestors {
		ancestor.reserved = ancestor.reserved.add(req.Estimate)
	}
	user := l.periodLocked(l.users, userPeriod)
	user.reserved = user.reserved.add(req.Estimate)
	tenant := l.periodLocked(l.tenants, tenantPeriod)
	tenant.reserved = tenant.reserved.add(req.Estimate)
	task.attempts[stepKey] = attempt
	task.Revision = counted.Revision
	return reservation, nil
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

func (l *Ledger) checkCapacityLocked(task *taskState, estimate Usage, now time.Time) *PauseError {
	if reason := capacityReason(task.used, task.reserved, estimate, task.spec.Limit, ScopeTask); reason != "" {
		return l.pauseLocked(task, reason, ScopeTask, task.used.add(task.reserved).add(estimate), task.spec.Limit)
	}
	user := l.userPeriodLockedAt(task.spec.UserID, now)
	if reason := capacityReason(user.used, user.reserved, estimate, l.policy.UserDaily, ScopeUser); reason != "" {
		return l.pauseLocked(task, reason, ScopeUser, user.used.add(user.reserved).add(estimate), l.policy.UserDaily)
	}
	tenant := l.tenantPeriodLockedAt(task.spec.TenantID, now)
	if reason := capacityReason(tenant.used, tenant.reserved, estimate, l.policy.TenantMonthly, ScopeTenant); reason != "" {
		return l.pauseLocked(task, reason, ScopeTenant, tenant.used.add(tenant.reserved).add(estimate), l.policy.TenantMonthly)
	}
	return nil
}

func (l *Ledger) ancestorChainLocked(task *taskState) ([]*taskState, error) {
	var out []*taskState
	seen := map[string]bool{task.spec.ID: true}
	parentID := task.spec.ParentTaskID
	for parentID != "" {
		if seen[parentID] {
			return nil, fmt.Errorf("%w: delegation cycle", ErrInvalid)
		}
		parent := l.tasks[parentID]
		if parent == nil {
			return nil, fmt.Errorf("%w: parent task %q", ErrTaskNotFound, parentID)
		}
		if parent.spec.TenantID != task.spec.TenantID || parent.spec.UserID != task.spec.UserID {
			return nil, fmt.Errorf("%w: lineage identity mismatch", ErrInvalid)
		}
		seen[parentID] = true
		out = append(out, parent)
		parentID = parent.spec.ParentTaskID
	}
	return out, nil
}

// Capacity is compared by subtracting from the ceiling, so saturating usage
// projections cannot hide an integer overflow when a ceiling is MaxInt64.
func capacityReason(used, reserved, estimate, limit Limits, scope Scope) PauseReason {
	dimensions := []struct {
		used, reserved, estimate, limit int64
		reasons                         [3]PauseReason
	}{
		{used.Steps, reserved.Steps, estimate.Steps, limit.Steps, [3]PauseReason{PauseTaskSteps, PauseUserSteps, PauseTenantSteps}},
		{used.Tokens, reserved.Tokens, estimate.Tokens, limit.Tokens, [3]PauseReason{PauseTaskTokens, PauseUserTokens, PauseTenantTokens}},
		{int64(used.WallClock), int64(reserved.WallClock), int64(estimate.WallClock), int64(limit.WallClock), [3]PauseReason{PauseTaskWallClock, PauseUserWallClock, PauseTenantWallClock}},
		{used.SpendMicros, reserved.SpendMicros, estimate.SpendMicros, limit.SpendMicros, [3]PauseReason{PauseTaskSpend, PauseUserSpend, PauseTenantSpend}},
	}
	index := 0
	if scope == ScopeUser {
		index = 1
	} else if scope == ScopeTenant {
		index = 2
	}
	for _, dimension := range dimensions {
		if dimension.used > dimension.limit || dimension.reserved > dimension.limit-dimension.used ||
			dimension.estimate > dimension.limit-dimension.used-dimension.reserved {
			return dimension.reasons[index]
		}
	}
	return ""
}

func (l *Ledger) pauseLocked(task *taskState, reason PauseReason, scope Scope, current, limit Limits) *PauseError {
	card := ExtensionCard{ID: "extension-" + task.spec.ID, TaskID: task.spec.ID, Scope: scope, Reason: reason, Message: "Review the budget extension before resuming this task."}
	if scope == ScopeTask && isTaskCeilingReason(reason) {
		card.MaxAdditional = minLimits(l.policy.ExtensionPolicy.MaxAdditional, l.policy.TenantMonthly.sub(task.spec.Limit))
		card.CanAccept = card.MaxAdditional.any()
	}
	if reason == PauseLoopDetected || reason == PauseRetryLimit {
		card.Message = "Revise the failing step before retrying; increasing a budget cannot clear this pause."
	}
	if !card.CanAccept && scope != ScopeTask {
		card.Message = "The shared ceiling is exhausted; an administrator must change the tenant policy."
	}
	paused := &PauseError{Reason: reason, Scope: scope, TaskID: task.spec.ID, Card: card}
	task.paused = paused
	_ = current
	_ = limit
	return clonePause(paused)
}

func clonePause(p *PauseError) *PauseError {
	if p == nil {
		return nil
	}
	copy := *p
	return &copy
}

func isTaskCeilingReason(reason PauseReason) bool {
	switch reason {
	case PauseTaskSteps, PauseTaskTokens, PauseTaskWallClock, PauseTaskSpend:
		return true
	default:
		return false
	}
}

func (l *Ledger) userPeriodLockedAt(userID string, now time.Time) *periodState {
	return l.periodLocked(l.users, userPeriodKey(userID, now))
}

func userPeriodKey(userID string, now time.Time) string {
	return userID + "|" + now.UTC().Format("2006-01-02")
}

func (l *Ledger) tenantPeriodLockedAt(tenantID string, now time.Time) *periodState {
	return l.periodLocked(l.tenants, tenantPeriodKey(tenantID, now))
}

func tenantPeriodKey(tenantID string, now time.Time) string {
	return tenantID + "|" + now.UTC().Format("2006-01")
}

func (l *Ledger) periodLocked(periods map[string]*periodState, key string) *periodState {
	state := periods[key]
	if state == nil {
		state = &periodState{}
		periods[key] = state
	}
	return state
}

// openReservationLocked finds the task of a still-open reservation. The caller
// marks the reservation closed only after the transition is durable.
func (l *Ledger) openReservationLocked(r *Reservation) (*taskState, *reservationState, error) {
	state, ok := l.reservations[r.ID]
	if !ok || state.closed {
		return nil, nil, ErrReservationClosed
	}
	task, ok := l.tasks[r.TaskID]
	if !ok {
		return nil, nil, ErrTaskNotFound
	}
	return task, state, nil
}

func (l *Ledger) settle(r *Reservation, actual Usage) error {
	if !actual.valid() || actual.exceeds(r.Estimate) {
		return ErrReservationExceeded
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	task, state, err := l.openReservationLocked(r)
	if err != nil {
		return err
	}
	next := cloneTaskState(task)
	next.reserved = next.reserved.sub(r.Estimate)
	next.used = next.used.add(actual)
	ancestors, err := l.ancestorChainLocked(task)
	if err != nil {
		return err
	}
	nextAncestors := make([]*taskState, 0, len(ancestors))
	for _, ancestor := range ancestors {
		nextAncestor := cloneTaskState(ancestor)
		nextAncestor.reserved = nextAncestor.reserved.sub(r.Estimate)
		nextAncestor.used = nextAncestor.used.add(actual)
		if err := advanceRevision(nextAncestor); err != nil {
			return err
		}
		nextAncestors = append(nextAncestors, nextAncestor)
	}
	if err := advanceRevision(next); err != nil {
		return err
	}
	user, tenant := *l.periodLocked(l.users, r.userPeriod), *l.periodLocked(l.tenants, r.tenantPeriod)
	user.reserved, user.used = user.reserved.sub(r.Estimate), user.used.add(actual)
	tenant.reserved, tenant.used = tenant.reserved.sub(r.Estimate), tenant.used.add(actual)
	tr := Transition{Kind: TransitionSettle, Task: durableTask(next), ReservationID: r.ID, StepID: r.StepID, Actual: actual, ExpectedRevision: task.Revision, ResultRevision: next.Revision,
		Periods: []DurablePeriod{durablePeriod(ScopeUser, r.userPeriod, &user), durablePeriod(ScopeTenant, r.tenantPeriod, &tenant)}}
	for _, nextAncestor := range nextAncestors {
		tr.RelatedTasks = append(tr.RelatedTasks, durableTask(nextAncestor))
	}
	if err := l.persistLocked(tr); err != nil {
		return err
	}
	*task = *next
	for i, ancestor := range ancestors {
		*ancestor = *nextAncestors[i]
	}
	*l.users[r.userPeriod], *l.tenants[r.tenantPeriod] = user, tenant
	state.closed = true
	return nil
}

func (l *Ledger) release(r *Reservation) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	task, state, err := l.openReservationLocked(r)
	if err != nil {
		return err
	}
	next := cloneTaskState(task)
	next.reserved = next.reserved.sub(r.Estimate)
	ancestors, err := l.ancestorChainLocked(task)
	if err != nil {
		return err
	}
	nextAncestors := make([]*taskState, 0, len(ancestors))
	for _, ancestor := range ancestors {
		nextAncestor := cloneTaskState(ancestor)
		nextAncestor.reserved = nextAncestor.reserved.sub(r.Estimate)
		if err := advanceRevision(nextAncestor); err != nil {
			return err
		}
		nextAncestors = append(nextAncestors, nextAncestor)
	}
	if err := advanceRevision(next); err != nil {
		return err
	}
	tr := Transition{Kind: TransitionRelease, Task: durableTask(next), ReservationID: r.ID, StepID: r.StepID, ExpectedRevision: task.Revision, ResultRevision: next.Revision}
	for _, nextAncestor := range nextAncestors {
		tr.RelatedTasks = append(tr.RelatedTasks, durableTask(nextAncestor))
	}
	if err := l.persistLocked(tr); err != nil {
		return err
	}
	*task = *next
	for i, ancestor := range ancestors {
		*ancestor = *nextAncestors[i]
	}
	user, tenant := l.periodLocked(l.users, r.userPeriod), l.periodLocked(l.tenants, r.tenantPeriod)
	user.reserved, tenant.reserved = user.reserved.sub(r.Estimate), tenant.reserved.sub(r.Estimate)
	state.closed = true
	return nil
}

func (l *Ledger) fail(r *Reservation) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	task, state, err := l.openReservationLocked(r)
	if err != nil {
		return err
	}
	next := cloneTaskState(task)
	next.reserved = next.reserved.sub(r.Estimate)
	ancestors, err := l.ancestorChainLocked(task)
	if err != nil {
		return err
	}
	nextAncestors := make([]*taskState, 0, len(ancestors))
	for _, ancestor := range ancestors {
		nextAncestor := cloneTaskState(ancestor)
		nextAncestor.reserved = nextAncestor.reserved.sub(r.Estimate)
		if err := advanceRevision(nextAncestor); err != nil {
			return err
		}
		nextAncestors = append(nextAncestors, nextAncestor)
	}
	failureKey := r.StepID + "\x00" + r.Fingerprint
	next.failures[failureKey]++
	var pause *PauseError
	if next.failures[failureKey] >= l.policy.LoopThreshold {
		pause = l.pauseLocked(next, PauseLoopDetected, ScopeTask, Limits{}, next.spec.Limit)
	} else if next.attempts[r.StepID] >= l.policy.MaxRetries+1 {
		pause = l.pauseLocked(next, PauseRetryLimit, ScopeTask, Limits{}, next.spec.Limit)
	}
	if err := advanceRevision(next); err != nil {
		return err
	}
	tr := Transition{Kind: TransitionFail, Task: durableTask(next), ReservationID: r.ID, StepID: r.StepID, ExpectedRevision: task.Revision, ResultRevision: next.Revision}
	for _, nextAncestor := range nextAncestors {
		tr.RelatedTasks = append(tr.RelatedTasks, durableTask(nextAncestor))
	}
	if err := l.persistLocked(tr); err != nil {
		return err
	}
	*task = *next
	for i, ancestor := range ancestors {
		*ancestor = *nextAncestors[i]
	}
	user, tenant := l.periodLocked(l.users, r.userPeriod), l.periodLocked(l.tenants, r.tenantPeriod)
	user.reserved, tenant.reserved = user.reserved.sub(r.Estimate), tenant.reserved.sub(r.Estimate)
	state.closed = true
	if pause != nil {
		return pause
	}
	return nil
}

// ExtensionRequest is an explicit user-accepted increase to a task ceiling.
// RequestID and ExpectedRevision are mandatory replay and CAS fences.
type ExtensionRequest struct {
	TaskID           string
	RequestID        string
	ExpectedRevision uint64
	Additional       Limits
}

// ExtensionResult describes the durable result of an extension. Replayed
// requests return the same result with Replayed set, without another charge.
type ExtensionResult struct {
	TaskID           string
	RequestID        string
	ExpectedRevision uint64
	PreviousRevision uint64
	Revision         uint64
	PreviousLimit    Limits
	Limit            Limits
	NewLimit         Limits
	Additional       Limits
	Replayed         bool
}

// AcceptExtension applies a task-local extension and returns only its error
// for compatibility. Requests still require a request ID and revision; there
// is no unguarded mutation path.
func (l *Ledger) AcceptExtension(req ExtensionRequest) error {
	_, err := l.AcceptExtensionCAS(req)
	return err
}

// AcceptExtensionCAS applies an extension under a task revision CAS and an
// append-only request replay fence. A replay is checked before pause and CAS
// validation so a client can safely retry after an ambiguous response.
func (l *Ledger) AcceptExtensionCAS(req ExtensionRequest) (ExtensionResult, error) {
	if l == nil || req.TaskID == "" || !req.Additional.valid() || !req.Additional.any() {
		return ExtensionResult{}, fmt.Errorf("%w: task and a non-negative extension are required", ErrInvalid)
	}
	if !validRequestID(req.RequestID) || req.ExpectedRevision == 0 {
		return ExtensionResult{}, fmt.Errorf("%w: request ID and positive expected revision are required", ErrInvalid)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if replay, ok := l.replays[replayKey(req.TaskID, req.RequestID)]; ok {
		if replay.ExpectedRevision != req.ExpectedRevision || replay.Additional != req.Additional {
			return ExtensionResult{}, ErrExtensionConflict
		}
		result := replay.Result
		result.Replayed = true
		return result, nil
	}
	task, ok := l.tasks[req.TaskID]
	if !ok {
		return ExtensionResult{}, ErrTaskNotFound
	}
	if task.Revision != req.ExpectedRevision {
		return ExtensionResult{}, fmt.Errorf("%w: expected %d, current %d", ErrStaleRevision, req.ExpectedRevision, task.Revision)
	}
	if task.paused == nil || task.paused.Scope != ScopeTask || !isTaskCeilingReason(task.paused.Reason) {
		return ExtensionResult{}, ErrExtensionUnavailable
	}
	for _, state := range l.reservations {
		if !state.closed && state.reservation != nil && state.reservation.TaskID == req.TaskID && state.reservation.Estimate.any() {
			return ExtensionResult{}, ErrExtensionReserved
		}
	}
	if !fitsExtension(req.Additional, task.spec.Limit, l.policy.ExtensionPolicy.MaxAdditional) {
		return ExtensionResult{}, ErrExtensionUnavailable
	}
	newLimit, ok := addLimitsChecked(task.spec.Limit, req.Additional)
	if !ok || newLimit.exceeds(l.policy.TenantMonthly) {
		return ExtensionResult{}, ErrExtensionOverflow
	}
	period := l.tenantPeriodLockedAt(task.spec.TenantID, l.now().UTC())
	remaining, ok := addLimitsChecked(period.used, period.reserved)
	if !ok {
		return ExtensionResult{}, ErrExtensionOverflow
	}
	remaining, ok = addLimitsChecked(remaining, req.Additional)
	if !ok || remaining.exceeds(l.policy.TenantMonthly) {
		return ExtensionResult{}, ErrExtensionUnavailable
	}
	next := cloneTaskState(task)
	next.spec.Limit = newLimit
	next.paused = nil
	if err := advanceRevision(next); err != nil {
		return ExtensionResult{}, err
	}
	result := ExtensionResult{TaskID: req.TaskID, RequestID: req.RequestID, ExpectedRevision: req.ExpectedRevision, PreviousRevision: task.Revision, Revision: next.Revision, PreviousLimit: task.spec.Limit, Limit: next.spec.Limit, NewLimit: next.spec.Limit, Additional: req.Additional}
	if err := l.persistLocked(Transition{Kind: TransitionExtension, Task: durableTask(next), Extension: req.Additional, RequestID: req.RequestID, ExpectedRevision: req.ExpectedRevision, ResultRevision: next.Revision, Result: result}); err != nil {
		return ExtensionResult{}, err
	}
	*task = *next
	l.replays[replayKey(req.TaskID, req.RequestID)] = extensionReplay{TaskID: req.TaskID, RequestID: req.RequestID, ExpectedRevision: req.ExpectedRevision, Additional: req.Additional, Result: result}
	return result, nil
}

func fitsExtension(additional, current, ceiling Limits) bool {
	if !additional.valid() || !additional.any() || !ceiling.valid() {
		return false
	}
	return additional.Steps <= ceiling.Steps && additional.Tokens <= ceiling.Tokens && additional.WallClock <= ceiling.WallClock && additional.SpendMicros <= ceiling.SpendMicros &&
		current.Steps <= ceiling.Steps && current.Tokens <= ceiling.Tokens && current.WallClock <= ceiling.WallClock && current.SpendMicros <= ceiling.SpendMicros
}

func minLimits(left, right Limits) Limits {
	return Limits{
		Steps:       minInt64(left.Steps, right.Steps),
		Tokens:      minInt64(left.Tokens, right.Tokens),
		WallClock:   time.Duration(minInt64(int64(left.WallClock), int64(right.WallClock))),
		SpendMicros: minInt64(left.SpendMicros, right.SpendMicros),
	}
}

func minInt64(left, right int64) int64 {
	if left < right {
		return left
	}
	return right
}

func validRequestID(id string) bool {
	trimmed := strings.TrimSpace(id)
	return trimmed == id && len(id) > 0 && len(id) <= 200
}

func replayKey(taskID, requestID string) string { return taskID + "\x00" + requestID }

func addLimitsChecked(left, right Limits) (Limits, bool) {
	steps, ok := addChecked(left.Steps, right.Steps)
	if !ok {
		return Limits{}, false
	}
	tokens, ok := addChecked(left.Tokens, right.Tokens)
	if !ok {
		return Limits{}, false
	}
	wall, ok := addChecked(int64(left.WallClock), int64(right.WallClock))
	if !ok {
		return Limits{}, false
	}
	spend, ok := addChecked(left.SpendMicros, right.SpendMicros)
	if !ok {
		return Limits{}, false
	}
	return Limits{Steps: steps, Tokens: tokens, WallClock: time.Duration(wall), SpendMicros: spend}, true
}

func addChecked(left, right int64) (int64, bool) {
	if right > 0 && left > int64(^uint64(0)>>1)-right {
		return 0, false
	}
	return left + right, true
}

// TaskSnapshot is a redacted, deterministic task projection.
type TaskSnapshot struct {
	ID           string      `json:"id"`
	TenantID     string      `json:"tenant_id"`
	UserID       string      `json:"user_id"`
	Revision     uint64      `json:"revision"`
	Limit        Limits      `json:"limit"`
	Used         Usage       `json:"used"`
	Reserved     Usage       `json:"reserved"`
	Paused       PauseReason `json:"paused,omitempty"`
	Attempts     int         `json:"attempts"`
	ParentTaskID string      `json:"parent_task_id,omitempty"`
	RootTaskID   string      `json:"root_task_id,omitempty"`
	Depth        int         `json:"delegation_depth,omitempty"`
}

// Snapshot is suitable for audit/test golden bytes. Maps are flattened and
// sorted so concurrent insertion order cannot change its representation.
type Snapshot struct {
	Tasks              []TaskSnapshot `json:"tasks"`
	UserPeriodTotals   []Usage        `json:"user_period_totals"`
	TenantPeriodTotals []Usage        `json:"tenant_period_totals"`
}

// Snapshot returns current usage without exposing fingerprints or prompts.
func (l *Ledger) Snapshot() Snapshot {
	if l == nil {
		return Snapshot{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	tasks := make([]TaskSnapshot, 0, len(l.tasks))
	ids := make([]string, 0, len(l.tasks))
	for id := range l.tasks {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		t := l.tasks[id]
		paused := PauseReason("")
		if t.paused != nil {
			paused = t.paused.Reason
		}
		totalAttempts := 0
		for _, count := range t.attempts {
			totalAttempts += count
		}
		tasks = append(tasks, TaskSnapshot{ID: t.spec.ID, TenantID: t.spec.TenantID, UserID: t.spec.UserID, Revision: t.Revision, Limit: t.spec.Limit, Used: t.used, Reserved: t.reserved, Paused: paused, Attempts: totalAttempts, ParentTaskID: t.spec.ParentTaskID, RootTaskID: t.spec.RootTaskID, Depth: t.spec.Depth})
	}
	userTotals := make([]Usage, 0, len(l.users))
	for _, state := range l.users {
		userTotals = append(userTotals, state.used.add(state.reserved))
	}
	tenantTotals := make([]Usage, 0, len(l.tenants))
	for _, state := range l.tenants {
		tenantTotals = append(tenantTotals, state.used.add(state.reserved))
	}
	return Snapshot{Tasks: tasks, UserPeriodTotals: sortedUsages(userTotals), TenantPeriodTotals: sortedUsages(tenantTotals)}
}

// ExtensionEnabled reports whether the configured extension policy permits any
// user-accepted increase. It is a projection hint; AcceptExtensionCAS remains
// the authority for pause, owner, revision and aggregate-cap checks.
func (l *Ledger) ExtensionEnabled() bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.policy.ExtensionPolicy.MaxAdditional.any()
}

func sortedUsages(values []Usage) []Usage {
	sort.Slice(values, func(i, j int) bool {
		if values[i].Steps != values[j].Steps {
			return values[i].Steps < values[j].Steps
		}
		if values[i].Tokens != values[j].Tokens {
			return values[i].Tokens < values[j].Tokens
		}
		if values[i].WallClock != values[j].WallClock {
			return values[i].WallClock < values[j].WallClock
		}
		return values[i].SpendMicros < values[j].SpendMicros
	})
	return values
}

// MarshalSnapshot emits canonical JSON for golden evidence.
func (l *Ledger) MarshalSnapshot() ([]byte, error) { return json.Marshal(l.Snapshot()) }
