// Package agentrun owns the durable, inspectable state machine for long-
// horizon agent tasks. It deliberately contains no model client and no
// effect port: model output is a proposal, while task state, approvals,
// waits, checkpoints, and the context ledger are server-owned facts.
package agentrun

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
)

const (
	MaxTaskLifetime = 7 * 24 * time.Hour
	MaxPlanSteps    = 200
	MaxAnswerBytes  = 16 << 10
)

var (
	ErrInvalid                  = errors.New("invalid agent task")
	ErrNotFound                 = errors.New("agent task not found")
	ErrConflict                 = errors.New("agent task revision conflict")
	ErrPlanConfirmationRequired = errors.New("agent plan confirmation required")
	ErrApprovalRequired         = errors.New("agent step approval required")
	ErrInvalidWake              = errors.New("invalid agent wake")
	ErrTerminal                 = errors.New("agent task is terminal")
	ErrStepNotReady             = errors.New("agent step is not ready")
	ErrVerificationFailed       = errors.New("agent step verification failed")
	ErrReconciliationRequired   = errors.New("agent step requires owner reconciliation")
)

// TaskState is intentionally finite. A waiting task owns no worker lease or
// model session; those fields are cleared as part of Park.
type TaskState string

const (
	StateDrafting                 TaskState = "DRAFTING"
	StateAwaitingPlanConfirmation TaskState = "AWAITING_PLAN_CONFIRMATION"
	StateRunning                  TaskState = "RUNNING"
	StateWaiting                  TaskState = "WAITING"
	StateAwaitingApproval         TaskState = "AWAITING_APPROVAL"
	StatePaused                   TaskState = "PAUSED"
	StateCompleted                TaskState = "COMPLETED"
	StateFailed                   TaskState = "FAILED"
	StateCancelled                TaskState = "CANCELLED"
	StateExpired                  TaskState = "EXPIRED"
)

func (s TaskState) terminal() bool {
	return s == StateCompleted || s == StateFailed || s == StateCancelled || s == StateExpired
}

func (s TaskState) valid() bool {
	switch s {
	case StateDrafting, StateAwaitingPlanConfirmation, StateRunning, StateWaiting,
		StateAwaitingApproval, StatePaused, StateCompleted, StateFailed,
		StateCancelled, StateExpired:
		return true
	default:
		return false
	}
}

// StepType is the only vocabulary a plan may use. VERIFY is separate from a
// model claim and is completed by an owner read through OwnerVerifier.
type StepType string

const (
	StepRead        StepType = "READ"
	StepAnalyze     StepType = "ANALYZE"
	StepDraft       StepType = "DRAFT"
	StepCommunicate StepType = "COMMUNICATE"
	StepSubmit      StepType = "SUBMIT"
	StepVerify      StepType = "VERIFY"
	StepAskUser     StepType = "ASK_USER"
	StepWait        StepType = "WAIT"
)

func (s StepType) valid() bool {
	switch s {
	case StepRead, StepAnalyze, StepDraft, StepCommunicate, StepSubmit,
		StepVerify, StepAskUser, StepWait:
		return true
	default:
		return false
	}
}

// Tier follows AGENT2-001. T0/T1 can run inside a confirmed plan; T2 needs a
// confirmed destination and T3/T4 require an exact approval digest.
type Tier uint8

const (
	TierRead           Tier = 0
	TierPrivateDraft   Tier = 1
	TierCommunicate    Tier = 2
	TierSubmitGoverned Tier = 3
	TierExternalWrite  Tier = 4
)

func (t Tier) valid() bool { return t <= TierExternalWrite }

type InputRef struct {
	Name     string   `json:"name"`
	Ref      string   `json:"ref"`
	SourceID string   `json:"source_id,omitempty"`
	Taint    []string `json:"taint,omitempty"`
}

type PlanStepState string

const (
	StepPending          PlanStepState = "PENDING"
	StepRunning          PlanStepState = "RUNNING"
	StepWaiting          PlanStepState = "WAITING"
	StepAwaitingApproval PlanStepState = "AWAITING_APPROVAL"
	StepCompleted        PlanStepState = "COMPLETED"
	StepFailed           PlanStepState = "FAILED"
)

func (s PlanStepState) valid() bool {
	switch s {
	case StepPending, StepRunning, StepWaiting, StepAwaitingApproval, StepCompleted, StepFailed:
		return true
	default:
		return false
	}
}

// PlanStep is a typed, version-pinned checkpoint. ResultRef is a durable
// reference, never an instruction-bearing transcript fragment.
type PlanStep struct {
	ID                   string        `json:"id"`
	Type                 StepType      `json:"type"`
	SkillID              string        `json:"skill_id"`
	SkillVersion         uint32        `json:"skill_version"`
	ConnectionID         string        `json:"connection_id,omitempty"`
	Inputs               []InputRef    `json:"inputs,omitempty"`
	ExpectedOutput       string        `json:"expected_output"`
	Tier                 Tier          `json:"tier"`
	Destination          string        `json:"destination,omitempty"`
	DestinationConfirmed bool          `json:"destination_confirmed,omitempty"`
	Approved             bool          `json:"approved,omitempty"`
	ApprovalDigest       string        `json:"approval_digest,omitempty"`
	ApprovalRevision     uint64        `json:"approval_revision,omitempty"`
	State                PlanStepState `json:"state"`
	Attempt              uint32        `json:"attempt"`
	ResultRef            string        `json:"result_ref,omitempty"`
	VerificationRef      string        `json:"verification_ref,omitempty"`
	StartedAt            time.Time     `json:"started_at,omitempty"`
	FinishedAt           time.Time     `json:"finished_at,omitempty"`
	// Wait is the typed wake condition of a WAIT or ASK_USER step. It is part of
	// the step identity, so it is bound by the plan digest and by an approval.
	Wait *WakeCondition `json:"wait,omitempty"`
}

type AgentPlan struct {
	Revision           uint64                  `json:"revision"`
	Steps              []PlanStep              `json:"steps"`
	DocumentReferences []agentdocref.Reference `json:"document_references,omitempty"`
	DocumentOmissions  []agentdocref.Omission  `json:"document_omissions,omitempty"`
	AnsweringAgent     *TaskAgentIdentity      `json:"answering_agent,omitempty"`
	Confirmed          bool                    `json:"confirmed"`
	ConfirmedBy        string                  `json:"confirmed_by,omitempty"`
	ConfirmedAt        time.Time               `json:"confirmed_at,omitempty"`
	Digest             string                  `json:"digest"`
}

// NewPlan validates and canonically digests a plan. Plan digest excludes the
// mutable confirmation fields so an approval is always bound to the steps.
func NewPlan(steps []PlanStep) (AgentPlan, error) {
	if len(steps) == 0 || len(steps) > MaxPlanSteps {
		return AgentPlan{}, fmt.Errorf("%w: plan must contain 1..%d steps", ErrInvalid, MaxPlanSteps)
	}
	steps = cloneSteps(steps)
	seen := map[string]bool{}
	for i := range steps {
		if err := validateStep(steps[i]); err != nil {
			return AgentPlan{}, fmt.Errorf("%w: step %d: %v", ErrInvalid, i, err)
		}
		if seen[steps[i].ID] {
			return AgentPlan{}, fmt.Errorf("%w: duplicate step id %q", ErrInvalid, steps[i].ID)
		}
		seen[steps[i].ID] = true
		steps[i].State = StepPending
		steps[i].Attempt = 0
		steps[i].ResultRef = ""
		steps[i].VerificationRef = ""
		steps[i].Approved = false
		steps[i].ApprovalDigest = ""
		steps[i].ApprovalRevision = 0
	}
	plan := AgentPlan{Revision: 1, Steps: cloneSteps(steps)}
	plan.Digest = digestPlan(plan)
	return plan, nil
}

func validateStep(step PlanStep) error {
	if strings.TrimSpace(step.ID) == "" || !step.Type.valid() || strings.TrimSpace(step.SkillID) == "" || step.SkillVersion == 0 || strings.TrimSpace(step.ExpectedOutput) == "" || !step.Tier.valid() {
		return errors.New("id, type, skill id/version, expected output and valid tier are required")
	}
	if step.Type == StepVerify && strings.TrimSpace(step.VerificationRef) == "" && len(step.Inputs) == 0 {
		return errors.New("VERIFY needs an owner reference")
	}
	if step.Tier == TierCommunicate && strings.TrimSpace(step.Destination) == "" {
		return errors.New("T2 COMMUNICATE needs a destination")
	}
	for _, in := range step.Inputs {
		if strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.Ref) == "" {
			return errors.New("input references need names and refs")
		}
	}
	if err := validateWaitSpec(step); err != nil {
		return err
	}
	return nil
}

func digestPlan(plan AgentPlan) string {
	steps := make([]PlanStepIdentity, len(plan.Steps))
	for i, step := range plan.Steps {
		steps[i] = identityOf(step)
	}
	return digestPlanIdentityWithTaskInputs(plan.Revision, steps, plan.DocumentReferences, plan.AnsweringAgent)
}

func digestPlanIdentity(revision uint64, steps []PlanStepIdentity) string {
	return digestPlanIdentityWithDocuments(revision, steps, nil)
}

func digestPlanIdentityWithDocuments(revision uint64, steps []PlanStepIdentity, refs []agentdocref.Reference) string {
	return digestPlanIdentityWithTaskInputs(revision, steps, refs, nil)
}

func digestPlanIdentityWithTaskInputs(revision uint64, steps []PlanStepIdentity, refs []agentdocref.Reference, agent *TaskAgentIdentity) string {
	type binding struct {
		Revision           uint64                  `json:"revision"`
		Steps              []PlanStepIdentity      `json:"steps"`
		DocumentReferences []agentdocref.Reference `json:"document_references,omitempty"`
		AnsweringAgent     *TaskAgentIdentity      `json:"answering_agent,omitempty"`
	}
	encoded, _ := json.Marshal(binding{Revision: revision, Steps: steps, DocumentReferences: refs, AnsweringAgent: cloneTaskAgentIdentity(agent)})
	sum := sha256.Sum256(append([]byte("hcm-next-agent-plan/v1\x00"), encoded...))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// WakeKind identifies the durable event that can resume a parked task.
type WakeKind string

const (
	WakeApproval  WakeKind = "APPROVAL"
	WakeSignal    WakeKind = "SIGNAL"
	WakeTimer     WakeKind = "TIMER"
	WakeUserReply WakeKind = "USER_REPLY"
	WakePolling   WakeKind = "POLLING"
)

type WakeCondition struct {
	Kind        WakeKind      `json:"kind"`
	Key         string        `json:"key"`
	Correlation string        `json:"correlation,omitempty"`
	DueAt       time.Time     `json:"due_at,omitempty"`
	PollAfter   time.Duration `json:"poll_after,omitempty"`
	StaleAfter  time.Time     `json:"stale_after,omitempty"`
}

type WakeEvent struct {
	ID          string    `json:"id"`
	Kind        WakeKind  `json:"kind"`
	Key         string    `json:"key"`
	Correlation string    `json:"correlation,omitempty"`
	PayloadRef  string    `json:"payload_ref,omitempty"`
	OccurredAt  time.Time `json:"occurred_at"`
}

type WakeResult struct {
	Accepted  bool
	Duplicate bool
	Ignored   bool
	Task      AgentTask
}

// WakeRechecker is the seam for AGENT2-003's short-lived delegated grant.
// The runtime owns state transitions, while the delegation package owns the
// current-user authority check immediately before a wake is accepted.
type WakeRechecker interface {
	RecheckWake(context.Context, AgentTask, WakeEvent) error
}

// LedgerEntry is an append-only context fact. Raw model notes are explicitly
// tainted and never become plan instructions merely by being in the ledger.
type LedgerEntry struct {
	Sequence  uint64   `json:"sequence"`
	Kind      string   `json:"kind"`
	Ref       string   `json:"ref"`
	SourceID  string   `json:"source_id,omitempty"`
	SourceIDs []string `json:"source_ids,omitempty"`
	Taint     []string `json:"taint,omitempty"`
	Revoked   bool     `json:"revoked,omitempty"`
	Digest    string   `json:"digest,omitempty"`
}

type TaskLedger struct {
	Entries    []LedgerEntry `json:"entries"`
	AnswerText string        `json:"answer_text,omitempty"`
}

type AgentTask struct {
	ID              string         `json:"id"`
	TenantID        string         `json:"tenant_id"`
	UserID          string         `json:"user_id"`
	ParentTaskID    string         `json:"parent_task_id,omitempty"`
	RootTaskID      string         `json:"root_task_id,omitempty"`
	BudgetTaskID    string         `json:"budget_task_id,omitempty"`
	DelegationDepth uint8          `json:"delegation_depth,omitempty"`
	Goal            string         `json:"goal"`
	Constraints     []string       `json:"constraints"`
	Plan            AgentPlan      `json:"plan"`
	State           TaskState      `json:"state"`
	Version         uint64         `json:"version"`
	CurrentStep     int            `json:"current_step"`
	Wake            *WakeCondition `json:"wake,omitempty"`
	LastWake        *WakeEvent     `json:"last_wake,omitempty"`
	PausedState     TaskState      `json:"paused_state,omitempty"`
	PausedWake      *WakeCondition `json:"paused_wake,omitempty"`
	WorkerLease     string         `json:"worker_lease,omitempty"`
	ModelSession    string         `json:"model_session,omitempty"`
	FailureCode     string         `json:"failure_code,omitempty"`
	FailureDetail   string         `json:"failure_detail,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	ExpiresAt       time.Time      `json:"expires_at"`
	Ledger          TaskLedger     `json:"ledger"`
}

type CreateRequest struct {
	ID              string
	TenantID        string
	UserID          string
	ParentTaskID    string
	RootTaskID      string
	BudgetTaskID    string
	DelegationDepth uint8
	Goal            string
	Constraints     []string
	Plan            AgentPlan
	Now             time.Time
	ExpiresAt       time.Time
}

type TaskStore interface {
	TaskEventStore
	Create(context.Context, AgentTask) error
	Get(context.Context, string) (AgentTask, error)
	Save(context.Context, AgentTask, uint64) error
	ClaimWake(context.Context, string, WakeEvent) (WakeResult, error)
	List(context.Context) ([]AgentTask, error)
}

// MemoryStore provides transactional, version-checked semantics for tests
// and local composition. Production wiring can implement TaskStore over the
// agent-owned store without changing the state machine.
type MemoryStore struct {
	mu     sync.Mutex
	tasks  map[string]AgentTask
	wakes  map[string]WakeEvent
	events map[string][]TaskEvent
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{tasks: make(map[string]AgentTask), wakes: make(map[string]WakeEvent), events: make(map[string][]TaskEvent)}
}

func NewStore() *MemoryStore { return NewMemoryStore() }

func (s *MemoryStore) Create(_ context.Context, task AgentTask) error {
	if s == nil {
		return fmt.Errorf("%w: nil store", ErrInvalid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tasks[task.ID]; ok {
		return fmt.Errorf("%w: task %q already exists", ErrConflict, task.ID)
	}
	s.tasks[task.ID] = cloneTask(task)
	return nil
}

// CreateWithEvents atomically creates a task and its initial event records.
func (s *MemoryStore) CreateWithEvents(ctx context.Context, task AgentTask, events ...TaskEvent) error {
	if s == nil {
		return fmt.Errorf("%w: nil store", ErrInvalid)
	}
	for _, event := range events {
		if event.TaskID != task.ID {
			return fmt.Errorf("%w: event task does not match created task", ErrInvalid)
		}
		if err := event.Validate(); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tasks[task.ID]; ok {
		return fmt.Errorf("%w: task %q already exists", ErrConflict, task.ID)
	}
	copyEvents, err := sequenceTaskEvents(nil, events)
	if err != nil {
		return err
	}
	s.tasks[task.ID] = cloneTask(task)
	s.events[task.ID] = copyEvents
	return nil
}

// SaveWithEvents atomically saves a versioned task and appends its events.
func (s *MemoryStore) SaveWithEvents(_ context.Context, task AgentTask, expectedVersion uint64, events ...TaskEvent) error {
	if s == nil {
		return fmt.Errorf("%w: nil store", ErrInvalid)
	}
	for _, event := range events {
		if event.TaskID != task.ID {
			return fmt.Errorf("%w: event task does not match saved task", ErrInvalid)
		}
		if err := event.Validate(); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.tasks[task.ID]
	if !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, task.ID)
	}
	if current.Version != expectedVersion {
		return fmt.Errorf("%w: task %s is version %d, expected %d", ErrConflict, task.ID, current.Version, expectedVersion)
	}
	if task.Version != expectedVersion+1 {
		return fmt.Errorf("%w: next version must be %d", ErrInvalid, expectedVersion+1)
	}
	if !slices.Equal(current.Plan.DocumentReferences, task.Plan.DocumentReferences) {
		return ErrDocumentReferenceInvalid
	}
	if !sameTaskAgentIdentity(current.Plan.AnsweringAgent, task.Plan.AnsweringAgent) {
		return ErrTaskAgentInvalid
	}
	task.Plan.DocumentOmissions = cloneDocumentOmissions(current.Plan.DocumentOmissions)
	copyEvents, err := sequenceTaskEvents(s.events[task.ID], events)
	if err != nil {
		return err
	}
	s.tasks[task.ID] = cloneTask(task)
	s.events[task.ID] = copyEvents
	return nil
}

// ListEvents returns a defensive, sequence-ordered copy of one task's events.
func (s *MemoryStore) ListEvents(_ context.Context, id string) ([]TaskEvent, error) {
	if s == nil {
		return nil, fmt.Errorf("%w: nil store", ErrInvalid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tasks[id]; !ok {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	out := make([]TaskEvent, len(s.events[id]))
	for i, event := range s.events[id] {
		out[i] = cloneTaskEvent(event)
	}
	return out, nil
}

func sequenceTaskEvents(existing, incoming []TaskEvent) ([]TaskEvent, error) {
	out := make([]TaskEvent, len(existing), len(existing)+len(incoming))
	for i, event := range existing {
		out[i] = cloneTaskEvent(event)
	}
	for _, event := range incoming {
		if event.Sequence != 0 {
			return nil, fmt.Errorf("%w: event sequence is store assigned", ErrInvalid)
		}
		event.Sequence = uint64(len(out) + 1)
		out = append(out, cloneTaskEvent(event))
	}
	return out, nil
}

func (s *MemoryStore) Get(_ context.Context, id string) (AgentTask, error) {
	if s == nil {
		return AgentTask{}, fmt.Errorf("%w: nil store", ErrInvalid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[id]
	if !ok {
		return AgentTask{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return cloneTask(task), nil
}

func (s *MemoryStore) Save(_ context.Context, task AgentTask, expectedVersion uint64) error {
	if s == nil {
		return fmt.Errorf("%w: nil store", ErrInvalid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.tasks[task.ID]
	if !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, task.ID)
	}
	if current.Version != expectedVersion {
		return fmt.Errorf("%w: task %s is version %d, expected %d", ErrConflict, task.ID, current.Version, expectedVersion)
	}
	if task.Version != expectedVersion+1 {
		return fmt.Errorf("%w: next version must be %d", ErrInvalid, expectedVersion+1)
	}
	if !slices.Equal(current.Plan.DocumentReferences, task.Plan.DocumentReferences) {
		return ErrDocumentReferenceInvalid
	}
	if !sameTaskAgentIdentity(current.Plan.AnsweringAgent, task.Plan.AnsweringAgent) {
		return ErrTaskAgentInvalid
	}
	task.Plan.DocumentOmissions = cloneDocumentOmissions(current.Plan.DocumentOmissions)
	s.tasks[task.ID] = cloneTask(task)
	return nil
}

func (s *MemoryStore) ClaimWake(_ context.Context, id string, event WakeEvent) (WakeResult, error) {
	if s == nil {
		return WakeResult{}, fmt.Errorf("%w: nil store", ErrInvalid)
	}
	if strings.TrimSpace(event.ID) == "" || !validWakeKind(event.Kind) || strings.TrimSpace(event.Key) == "" || event.OccurredAt.IsZero() {
		return WakeResult{}, fmt.Errorf("%w: event id, kind and key are required", ErrInvalidWake)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[id]
	if !ok {
		return WakeResult{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	dedupe := id + "\x00" + event.ID
	if _, ok := s.wakes[dedupe]; ok {
		return WakeResult{Duplicate: true, Task: cloneTask(task)}, nil
	}
	s.wakes[dedupe] = event
	if !task.State.terminal() && !task.ExpiresAt.After(event.OccurredAt) {
		task.State, task.Wake, task.WorkerLease, task.ModelSession = StateExpired, nil, "", ""
		task.FailureCode, task.FailureDetail = "TASK_EXPIRED", "maximum task lifetime elapsed"
		task.Version++
		task.UpdatedAt = event.OccurredAt.UTC()
		s.tasks[id] = cloneTask(task)
		return WakeResult{Ignored: true, Task: cloneTask(task)}, nil
	}
	if task.State.terminal() || task.State == StatePaused || task.Wake == nil || task.Wake.Kind != event.Kind || task.Wake.Key != event.Key || (task.Wake.Correlation != "" && task.Wake.Correlation != event.Correlation) {
		return WakeResult{Ignored: true, Task: cloneTask(task)}, nil
	}
	if (event.Kind == WakeApproval && task.CurrentStep < len(task.Plan.Steps) && task.Plan.Steps[task.CurrentStep].State == StepAwaitingApproval) || (event.Kind == WakeTimer && event.OccurredAt.Before(task.Wake.DueAt)) {
		return WakeResult{Ignored: true, Task: cloneTask(task)}, nil
	}
	accepted := event
	task.LastWake = &accepted
	task.Wake = nil
	task.State = StateRunning
	task.Version++
	task.UpdatedAt = event.OccurredAt
	if task.UpdatedAt.IsZero() {
		task.UpdatedAt = task.CreatedAt
	}
	s.tasks[id] = cloneTask(task)
	return WakeResult{Accepted: true, Task: cloneTask(task)}, nil
}

func (s *MemoryStore) List(_ context.Context) ([]AgentTask, error) {
	if s == nil {
		return nil, fmt.Errorf("%w: nil store", ErrInvalid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, 0, len(s.tasks))
	for id := range s.tasks {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]AgentTask, 0, len(ids))
	for _, id := range ids {
		out = append(out, cloneTask(s.tasks[id]))
	}
	return out, nil
}

type Runtime struct{ store TaskStore }

func NewRuntime(store TaskStore) (*Runtime, error) {
	if store == nil {
		return nil, fmt.Errorf("%w: task store is required", ErrInvalid)
	}
	return &Runtime{store: store}, nil
}

func (r *Runtime) CreateTask(ctx context.Context, req CreateRequest) (AgentTask, error) {
	if r == nil || r.store == nil || strings.TrimSpace(req.ID) == "" || strings.TrimSpace(req.TenantID) == "" || strings.TrimSpace(req.UserID) == "" || strings.TrimSpace(req.Goal) == "" || req.Now.IsZero() || req.ExpiresAt.Before(req.Now) || req.ExpiresAt.Sub(req.Now) > MaxTaskLifetime {
		return AgentTask{}, fmt.Errorf("%w: id, tenant, user, goal and bounded lifetime are required", ErrInvalid)
	}
	if !req.Plan.validDigest() {
		return AgentTask{}, fmt.Errorf("%w: plan digest is invalid", ErrInvalid)
	}
	if refs := documentReferencesFromContext(ctx); len(refs) > 0 {
		req.Plan.DocumentReferences = cloneDocumentReferences(refs)
		req.Plan.Digest = digestPlan(req.Plan)
	}
	if agent := taskAgentIdentityFromContext(ctx); agent != nil {
		req.Plan.AnsweringAgent = agent
		req.Plan.Digest = digestPlan(req.Plan)
	}
	if err := validateTaskLineage(req); err != nil {
		return AgentTask{}, err
	}
	if req.Plan.Confirmed {
		return AgentTask{}, fmt.Errorf("%w: new tasks must be confirmed through ConfirmPlan", ErrInvalid)
	}
	req.Plan = normalizeProposedPlan(req.Plan)
	task := AgentTask{ID: req.ID, TenantID: req.TenantID, UserID: req.UserID, Goal: req.Goal, Constraints: append([]string(nil), req.Constraints...), Plan: clonePlan(req.Plan), State: StateAwaitingPlanConfirmation, Version: 1, CurrentStep: 0, CreatedAt: req.Now.UTC(), UpdatedAt: req.Now.UTC(), ExpiresAt: req.ExpiresAt.UTC()}
	task.ParentTaskID, task.RootTaskID, task.BudgetTaskID, task.DelegationDepth = req.ParentTaskID, req.RootTaskID, req.BudgetTaskID, req.DelegationDepth
	task.Ledger.Entries = []LedgerEntry{{Sequence: 1, Kind: "USER_GOAL", Ref: "task:" + req.ID, Taint: []string{"USER_AUTHORED"}, Digest: digestText(req.Goal)}}
	for i, constraint := range req.Constraints {
		task.Ledger.Entries = append(task.Ledger.Entries, LedgerEntry{Sequence: uint64(i + 2), Kind: "USER_CONSTRAINT", Ref: fmt.Sprintf("task:%s:constraint:%d", req.ID, i), Taint: []string{"USER_AUTHORED"}, Digest: digestText(constraint)})
	}
	snapshot, err := SnapshotPlan(task.Plan)
	if err != nil {
		return AgentTask{}, err
	}
	if err := createTaskWithEvents(ctx, r.store, task, TaskEvent{TaskID: task.ID, Type: TaskEventPlanRevision, PlanRevision: task.Plan.Revision,
		PlanDigest: task.Plan.Digest, PlanSnapshot: &snapshot, Outcome: "AWAITING_CONFIRMATION", OccurredAt: req.Now.UTC()}); err != nil {
		return AgentTask{}, err
	}
	return task, nil
}

func (p AgentPlan) validDigest() bool {
	if len(p.Steps) == 0 || len(p.Steps) > MaxPlanSteps || p.Revision == 0 || p.Digest == "" || digestPlan(p) != p.Digest {
		return false
	}
	if agentdocref.Validate(p.DocumentReferences, agentdocref.MaxRequestReferences) != nil || ValidateTaskDocumentOmissions(p.DocumentReferences, p.DocumentOmissions) != nil {
		return false
	}
	if p.AnsweringAgent != nil && p.AnsweringAgent.Validate() != nil {
		return false
	}
	seen := make(map[string]struct{}, len(p.Steps))
	for _, step := range p.Steps {
		if err := validateStep(step); err != nil {
			return false
		}
		if !step.State.valid() {
			return false
		}
		if _, exists := seen[step.ID]; exists {
			return false
		}
		seen[step.ID] = struct{}{}
	}
	return true
}

func (r *Runtime) GetTask(ctx context.Context, id string) (AgentTask, error) {
	return r.store.Get(ctx, id)
}

func (r *Runtime) ConfirmPlan(ctx context.Context, id, user string, expectedVersion uint64, now time.Time) (AgentTask, error) {
	task, err := r.store.Get(ctx, id)
	if err != nil {
		return AgentTask{}, err
	}
	if task.Version != expectedVersion {
		return AgentTask{}, fmt.Errorf("%w: expected %d, got %d", ErrConflict, expectedVersion, task.Version)
	}
	if expired, err := r.expireBeforeWork(ctx, task, now); err != nil {
		return expired, err
	}
	if task.State != StateAwaitingPlanConfirmation && task.State != StateDrafting {
		return AgentTask{}, fmt.Errorf("%w: task is %s", ErrPlanConfirmationRequired, task.State)
	}
	if strings.TrimSpace(user) == "" || user != task.UserID || now.IsZero() || !task.Plan.validDigest() {
		return AgentTask{}, fmt.Errorf("%w: user, time and valid plan are required", ErrInvalid)
	}
	for i := range task.Plan.Steps {
		step := &task.Plan.Steps[i]
		if step.Tier == TierCommunicate && !step.DestinationConfirmed {
			return AgentTask{}, fmt.Errorf("%w: destination for step %s", ErrPlanConfirmationRequired, step.ID)
		}
		if step.State != StepCompleted {
			step.State = StepPending
			step.StartedAt, step.FinishedAt = time.Time{}, time.Time{}
			step.Approved = false
			step.ApprovalDigest = ""
		}
	}
	task.Plan.Confirmed = true
	task.Plan.ConfirmedBy = user
	task.Plan.ConfirmedAt = now.UTC()
	task.State = StateRunning
	task.Wake = nil
	task.WorkerLease, task.ModelSession = "", ""
	task.LastWake = nil
	task.CurrentStep = firstPendingStep(task.Plan.Steps)
	task.Version++
	task.UpdatedAt = now.UTC()
	snapshot, err := SnapshotPlan(task.Plan)
	if err != nil {
		return AgentTask{}, err
	}
	if err := saveTaskWithEvents(ctx, r.store, task, expectedVersion, TaskEvent{TaskID: task.ID, Type: TaskEventPlanConfirmation,
		PlanRevision: task.Plan.Revision, PlanDigest: task.Plan.Digest, PlanSnapshot: &snapshot, Outcome: "CONFIRMED", ActorID: user, OccurredAt: now.UTC()}); err != nil {
		return AgentTask{}, err
	}
	return task, nil
}

// Replan applies only a plan that the caller has already built and digested.
// New T0/T1 steps inside the confirmed skill/connection set remain confirmed;
// a new skill, connection, or T2-T4 step is parked for explicit confirmation.
func (r *Runtime) Replan(ctx context.Context, id string, expectedVersion uint64, plan AgentPlan, now time.Time) (AgentTask, error) {
	task, err := r.store.Get(ctx, id)
	if err != nil {
		return AgentTask{}, err
	}
	if task.Version != expectedVersion {
		return AgentTask{}, fmt.Errorf("%w: expected %d, got %d", ErrConflict, expectedVersion, task.Version)
	}
	if task.State.terminal() || now.IsZero() || !plan.validDigest() {
		return AgentTask{}, fmt.Errorf("%w: terminal task, time or plan is invalid", ErrInvalid)
	}
	if task.WorkerLease != "" || (task.CurrentStep < len(task.Plan.Steps) && task.Plan.Steps[task.CurrentStep].State == StepRunning) {
		return task, ErrReconciliationRequired
	}
	if expired, err := r.expireBeforeWork(ctx, task, now); err != nil {
		return expired, err
	}
	allowedSkills, allowedConnections := map[string]uint32{}, map[string]bool{}
	for _, old := range task.Plan.Steps {
		allowedSkills[old.SkillID] = old.SkillVersion
		if old.ConnectionID != "" {
			allowedConnections[old.ConnectionID] = true
		}
	}
	needsConfirmation := false
	for _, step := range plan.Steps {
		if allowedVersion, ok := allowedSkills[step.SkillID]; !ok || allowedVersion != step.SkillVersion || (step.ConnectionID != "" && !allowedConnections[step.ConnectionID]) || step.Tier >= TierCommunicate {
			needsConfirmation = true
		}
	}
	plan.DocumentReferences = cloneDocumentReferences(task.Plan.DocumentReferences)
	plan.DocumentOmissions = cloneDocumentOmissions(task.Plan.DocumentOmissions)
	plan.AnsweringAgent = cloneTaskAgentIdentity(task.Plan.AnsweringAgent)
	plan.Revision = task.Plan.Revision + 1
	plan = normalizeProposedPlan(plan)
	plan.Steps = preserveCompletedSteps(task.Plan.Steps, cloneSteps(plan.Steps))
	plan.Digest = digestPlan(plan)
	plan.Confirmed = !needsConfirmation
	if plan.Confirmed {
		plan.ConfirmedBy, plan.ConfirmedAt = task.Plan.ConfirmedBy, now.UTC()
	}
	task.Plan = plan
	task.State = StateRunning
	if needsConfirmation {
		task.State = StateAwaitingPlanConfirmation
	}
	task.Wake = nil
	task.WorkerLease, task.ModelSession = "", ""
	task.CurrentStep = firstPendingStep(task.Plan.Steps)
	task.Version++
	task.UpdatedAt = now.UTC()
	outcome := "AUTO_CONFIRMED"
	if needsConfirmation {
		outcome = "AWAITING_CONFIRMATION"
	}
	snapshot, err := SnapshotPlan(task.Plan)
	if err != nil {
		return AgentTask{}, err
	}
	if err := saveTaskWithEvents(ctx, r.store, task, expectedVersion, TaskEvent{TaskID: task.ID, Type: TaskEventPlanRevision,
		PlanRevision: task.Plan.Revision, PlanDigest: task.Plan.Digest, PlanSnapshot: &snapshot, Outcome: outcome, OccurredAt: now.UTC()}); err != nil {
		return AgentTask{}, err
	}
	return task, nil
}

type StepResult struct {
	Ref        string
	Digest     string
	Taint      []string
	SourceIDs  []string
	Note       string
	AnswerText string
}

type StepExecutor interface {
	Execute(context.Context, AgentTask, PlanStep) (StepResult, error)
}

type OwnerVerifier interface {
	Verify(context.Context, AgentTask, PlanStep, StepResult) error
}

// ExecuteNext claims one checkpoint and runs exactly one step. The durable
// claim is committed before the external executor is called; a crashed worker
// therefore leaves a recoverable RUNNING checkpoint instead of replaying an
// unknown effect automatically.
func (r *Runtime) ExecuteNext(ctx context.Context, id string, expectedVersion uint64, exec StepExecutor, verifier OwnerVerifier, now time.Time) (AgentTask, error) {
	if exec == nil || now.IsZero() {
		return AgentTask{}, fmt.Errorf("%w: executor and time are required", ErrInvalid)
	}
	task, err := r.store.Get(ctx, id)
	if err != nil {
		return AgentTask{}, err
	}
	if task.Version != expectedVersion {
		return AgentTask{}, fmt.Errorf("%w: expected %d, got %d", ErrConflict, expectedVersion, task.Version)
	}
	if expired, err := r.expireBeforeWork(ctx, task, now); err != nil {
		return expired, err
	}
	if task.State != StateRunning || !task.Plan.Confirmed || task.CurrentStep >= len(task.Plan.Steps) {
		return AgentTask{}, fmt.Errorf("%w: task state or cursor is not executable", ErrStepNotReady)
	}
	step := &task.Plan.Steps[task.CurrentStep]
	if step.State != StepPending {
		return AgentTask{}, fmt.Errorf("%w: step %s is %s", ErrStepNotReady, step.ID, step.State)
	}
	if step.Tier >= TierSubmitGoverned && !step.Approved {
		step.ApprovalRevision = task.Version + 1
		step.ApprovalDigest = digestApproval(task, *step)
		task.State, step.State = StateAwaitingApproval, StepAwaitingApproval
		task.Wake = &WakeCondition{Kind: WakeApproval, Key: step.ApprovalDigest, StaleAfter: now.UTC().Add(24 * time.Hour)}
		task.WorkerLease, task.ModelSession = "", ""
		task.Version++
		task.UpdatedAt = now.UTC()
		if err := saveTaskWithEvents(ctx, r.store, task, expectedVersion, TaskEvent{TaskID: task.ID, Type: TaskEventApprovalRequest,
			PlanRevision: task.Plan.Revision, PlanDigest: task.Plan.Digest, StepID: step.ID, StepType: step.Type, Tier: step.Tier,
			ApprovalDigest: step.ApprovalDigest, Outcome: "PENDING", OccurredAt: now.UTC()}); err != nil {
			return AgentTask{}, err
		}
		return task, ErrApprovalRequired
	}
	if step.Tier == TierCommunicate && !step.DestinationConfirmed {
		return AgentTask{}, fmt.Errorf("%w: destination for %s", ErrPlanConfirmationRequired, step.ID)
	}
	step.State = StepRunning
	step.Attempt++
	step.StartedAt = now.UTC()
	step.FinishedAt = time.Time{}
	task.WorkerLease = "step:" + step.ID
	task.Version++
	task.UpdatedAt = now.UTC()
	if err := r.store.Save(ctx, task, expectedVersion); err != nil {
		return AgentTask{}, err
	}

	result, execErr := exec.Execute(ctx, task, *step)
	if execErr != nil {
		return r.finishStep(ctx, id, task.Version, now, step.ID, StepResult{}, execErr, false, verifier)
	}
	if step.Type == StepVerify {
		if verifier == nil {
			return r.finishStep(ctx, id, task.Version, now, step.ID, result, ErrVerificationFailed, true, verifier)
		}
		if err := verifier.Verify(ctx, task, *step, result); err != nil {
			return r.finishStep(ctx, id, task.Version, now, step.ID, result, fmt.Errorf("%w: %v", ErrVerificationFailed, err), true, verifier)
		}
	}
	return r.finishStep(ctx, id, task.Version, now, step.ID, result, nil, true, verifier)
}

func (r *Runtime) finishStep(ctx context.Context, id string, expected uint64, now time.Time, stepID string, result StepResult, runErr error, executorSucceeded bool, _ OwnerVerifier) (AgentTask, error) {
	task, err := r.store.Get(ctx, id)
	if err != nil {
		return AgentTask{}, err
	}
	if task.Version != expected {
		return AgentTask{}, fmt.Errorf("%w: worker checkpoint version", ErrConflict)
	}
	idx := task.CurrentStep
	if idx >= len(task.Plan.Steps) || task.Plan.Steps[idx].ID != stepID {
		return AgentTask{}, fmt.Errorf("%w: checkpoint cursor moved", ErrConflict)
	}
	step := &task.Plan.Steps[idx]
	task.WorkerLease, task.ModelSession = "", ""
	task.LastWake = nil
	var pause *StepPauseError
	stepPaused := !executorSucceeded && errors.As(runErr, &pause) && pause.valid()
	if stepPaused {
		step.State = StepPending
		step.StartedAt, step.FinishedAt = time.Time{}, time.Time{}
		task.State, task.PausedState, task.PausedWake = StatePaused, StateRunning, nil
		task.FailureCode, task.FailureDetail = pause.Reason, "step resource admission paused before execution"
	} else if runErr != nil {
		step.State = StepFailed
		step.FinishedAt = now.UTC()
		task.State = StateFailed
		task.FailureCode = "STEP_FAILED"
		task.FailureDetail = runErr.Error()
	} else {
		step.State = StepCompleted
		step.FinishedAt = now.UTC()
		if task.FailureCode == "AMBIGUOUS_EFFECT" {
			task.FailureCode, task.FailureDetail = "", ""
		}
		step.ResultRef = result.Ref
		step.VerificationRef = result.Digest
		if step.Type == StepAnalyze && acceptedAnswer(result) {
			task.Ledger.AnswerText = result.AnswerText
		}
		appendLedger(&task, LedgerEntry{Kind: "STEP_RESULT", Ref: result.Ref, SourceID: first(result.SourceIDs), SourceIDs: append([]string(nil), result.SourceIDs...), Taint: append([]string(nil), result.Taint...), Digest: result.Digest})
		task.CurrentStep++
		if task.CurrentStep == len(task.Plan.Steps) {
			task.State = StateCompleted
		}
	}
	task.Version++
	task.UpdatedAt = now.UTC()
	outcome := "SUCCEEDED"
	if runErr != nil {
		outcome = "FAILED"
	}
	if stepPaused {
		outcome = "PAUSED"
	}
	events := []TaskEvent{{TaskID: task.ID, Type: TaskEventStepExecution, PlanRevision: task.Plan.Revision, PlanDigest: task.Plan.Digest,
		StepID: step.ID, StepType: step.Type, Tier: step.Tier, Outcome: outcome, EvidenceRef: result.Ref, EvidenceDigest: result.Digest, OccurredAt: now.UTC()}}
	if stepPaused && strings.HasPrefix(pause.Reason, "AUTHORITY_") {
		for i := range task.Plan.Steps {
			pending := &task.Plan.Steps[i]
			if pending.State == StepCompleted || pending.Tier < TierSubmitGoverned {
				continue
			}
			if pending.ApprovalDigest != "" {
				events = append(events, TaskEvent{TaskID: task.ID, Type: TaskEventApprovalOutcome, PlanRevision: task.Plan.Revision, PlanDigest: task.Plan.Digest, StepID: pending.ID, StepType: pending.Type, Tier: pending.Tier, ApprovalDigest: pending.ApprovalDigest, Outcome: "VOIDED_AUTHORITY", OccurredAt: now.UTC()})
			}
			pending.Approved, pending.ApprovalDigest = false, ""
			if pending.State == StepAwaitingApproval {
				pending.State = StepPending
			}
		}
	}
	if step.Type == StepVerify && !stepPaused {
		verification := "VERIFIED"
		if runErr != nil {
			verification = "FAILED"
		}
		events = append(events, TaskEvent{TaskID: task.ID, Type: TaskEventStepVerification, PlanRevision: task.Plan.Revision, PlanDigest: task.Plan.Digest,
			StepID: step.ID, StepType: step.Type, Tier: step.Tier, Outcome: verification, EvidenceRef: result.Ref, EvidenceDigest: result.Digest, OccurredAt: now.UTC()})
	}
	if step.Tier >= TierSubmitGoverned && step.Approved && !stepPaused {
		effect := "SUCCEEDED"
		if !executorSucceeded {
			effect = "FAILED"
		}
		events = append(events, TaskEvent{TaskID: task.ID, Type: TaskEventStepEffect, PlanRevision: task.Plan.Revision, PlanDigest: task.Plan.Digest,
			StepID: step.ID, StepType: step.Type, Tier: step.Tier, ApprovalDigest: step.ApprovalDigest,
			Outcome: effect, EvidenceRef: result.Ref, EvidenceDigest: result.Digest, OccurredAt: now.UTC()})
	}
	if err := saveTaskWithEvents(ctx, r.store, task, expected, events...); err != nil {
		return AgentTask{}, err
	}
	if runErr != nil {
		return task, runErr
	}
	return task, nil
}

func acceptedAnswer(result StepResult) bool {
	return strings.TrimSpace(result.AnswerText) != "" && len(result.AnswerText) <= MaxAnswerBytes &&
		utf8.ValidString(result.AnswerText) && hasTaint(result.Taint, "AGENT_DERIVED")
}

// PlanStepIdentity is the immutable, digest-bound part of one historical plan step.
type PlanStepIdentity struct {
	ID                   string         `json:"id"`
	Type                 StepType       `json:"type"`
	SkillID              string         `json:"skill_id"`
	SkillVersion         uint32         `json:"skill_version"`
	ConnectionID         string         `json:"connection_id,omitempty"`
	Inputs               []InputRef     `json:"inputs,omitempty"`
	ExpectedOutput       string         `json:"expected_output"`
	Tier                 Tier           `json:"tier"`
	Destination          string         `json:"destination,omitempty"`
	DestinationConfirmed bool           `json:"destination_confirmed,omitempty"`
	Wait                 *WakeCondition `json:"wait,omitempty"`
}

func identityOf(step PlanStep) PlanStepIdentity {
	return PlanStepIdentity{ID: step.ID, Type: step.Type, SkillID: step.SkillID, SkillVersion: step.SkillVersion, ConnectionID: step.ConnectionID, Inputs: step.Inputs, ExpectedOutput: step.ExpectedOutput, Tier: step.Tier, Destination: step.Destination, DestinationConfirmed: step.DestinationConfirmed, Wait: step.Wait}
}

func digestStep(step PlanStep) string {
	encoded, _ := json.Marshal(identityOf(step))
	sum := sha256.Sum256(append([]byte("hcm-next-agent-step/v1\x00"), encoded...))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func digestApproval(task AgentTask, step PlanStep) string {
	if step.ApprovalRevision == 0 {
		return digestStep(step)
	}
	encoded, _ := json.Marshal(struct {
		TaskID, PlanDigest, StepDigest string
		Revision                       uint64
	}{task.ID, task.Plan.Digest, digestStep(step), step.ApprovalRevision})
	sum := sha256.Sum256(append([]byte("hcm-next-agent-approval/v2\x00"), encoded...))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (r *Runtime) ApproveStep(ctx context.Context, id, stepID, digest string, expectedVersion uint64, now time.Time) (AgentTask, error) {
	if now.IsZero() {
		return AgentTask{}, fmt.Errorf("%w: time is required", ErrInvalid)
	}
	task, err := r.store.Get(ctx, id)
	if err != nil {
		return AgentTask{}, err
	}
	if task.Version != expectedVersion {
		return AgentTask{}, fmt.Errorf("%w: version", ErrConflict)
	}
	if expired, err := r.expireBeforeWork(ctx, task, now); err != nil {
		return expired, err
	}
	if task.State != StateAwaitingApproval || task.CurrentStep >= len(task.Plan.Steps) {
		return AgentTask{}, fmt.Errorf("%w: task is not awaiting approval", ErrApprovalRequired)
	}
	step := &task.Plan.Steps[task.CurrentStep]
	if step.ID != stepID || digest == "" || digest != step.ApprovalDigest || digestApproval(task, *step) != digest {
		return AgentTask{}, fmt.Errorf("%w: exact step digest mismatch", ErrApprovalRequired)
	}
	step.State = StepPending
	step.Approved = true
	step.ApprovalDigest = digest
	task.State = StateRunning
	task.Wake = nil
	task.Version++
	task.UpdatedAt = now.UTC()
	if err := saveTaskWithEvents(ctx, r.store, task, expectedVersion, TaskEvent{TaskID: task.ID, Type: TaskEventApprovalOutcome,
		PlanRevision: task.Plan.Revision, PlanDigest: task.Plan.Digest, StepID: step.ID, StepType: step.Type, Tier: step.Tier,
		ApprovalDigest: digest, Outcome: "APPROVED", OccurredAt: now.UTC()}); err != nil {
		return AgentTask{}, err
	}
	return task, nil
}

func (r *Runtime) Park(ctx context.Context, id string, expectedVersion uint64, condition WakeCondition, now time.Time) (AgentTask, error) {
	if !validWakeCondition(condition) || now.IsZero() {
		return AgentTask{}, fmt.Errorf("%w: wake condition and time are required", ErrInvalidWake)
	}
	task, err := r.store.Get(ctx, id)
	if err != nil {
		return AgentTask{}, err
	}
	if task.Version != expectedVersion {
		return AgentTask{}, fmt.Errorf("%w: version", ErrConflict)
	}
	if task.State != StateRunning && task.State != StateAwaitingApproval {
		return AgentTask{}, fmt.Errorf("%w: task is not parkable", ErrInvalidWake)
	}
	task.Wake = &condition
	task.State = StateWaiting
	task.WorkerLease, task.ModelSession = "", ""
	task.Version++
	task.UpdatedAt = now.UTC()
	if err := r.store.Save(ctx, task, expectedVersion); err != nil {
		return AgentTask{}, err
	}
	return task, nil
}

func validWakeKind(k WakeKind) bool {
	return k == WakeApproval || k == WakeSignal || k == WakeTimer || k == WakeUserReply || k == WakePolling
}
func validWakeCondition(c WakeCondition) bool {
	if !validWakeKind(c.Kind) || strings.TrimSpace(c.Key) == "" {
		return false
	}
	if c.Kind == WakeTimer && c.DueAt.IsZero() {
		return false
	}
	if c.Kind == WakePolling && c.PollAfter <= 0 {
		return false
	}
	return true
}

func (r *Runtime) Wake(ctx context.Context, id string, event WakeEvent) (WakeResult, error) {
	return r.store.ClaimWake(ctx, id, event)
}

// WakeWithRecheck applies the authority seam before an event can resume a
// parked task. A refused event is not entered into the dedupe inbox, so a
// later grant exchange may retry it without changing task state.
func (r *Runtime) WakeWithRecheck(ctx context.Context, id string, event WakeEvent, rechecker WakeRechecker) (WakeResult, error) {
	if rechecker == nil {
		return WakeResult{}, fmt.Errorf("%w: wake rechecker is required", ErrInvalidWake)
	}
	task, err := r.store.Get(ctx, id)
	if err != nil {
		return WakeResult{}, err
	}
	if task.Wake != nil && (task.State == StateWaiting || task.State == StateAwaitingApproval) && task.Wake.Kind == event.Kind && task.Wake.Key == event.Key && (task.Wake.Correlation == "" || task.Wake.Correlation == event.Correlation) {
		if !event.OccurredAt.IsZero() && !task.ExpiresAt.After(event.OccurredAt) {
			return r.store.ClaimWake(ctx, id, event)
		}
		if err := rechecker.RecheckWake(ctx, task, event); err != nil {
			return WakeResult{Ignored: true, Task: task}, err
		}
	}
	return r.store.ClaimWake(ctx, id, event)
}

func (r *Runtime) Pause(ctx context.Context, id string, expectedVersion uint64, now time.Time) (AgentTask, error) {
	return r.transition(ctx, id, expectedVersion, StatePaused, now)
}
func (r *Runtime) Resume(ctx context.Context, id string, expectedVersion uint64, now time.Time) (AgentTask, error) {
	if now.IsZero() {
		return AgentTask{}, fmt.Errorf("%w: time is required", ErrInvalid)
	}
	task, err := r.store.Get(ctx, id)
	if err != nil {
		return AgentTask{}, err
	}
	if task.Version != expectedVersion {
		return AgentTask{}, fmt.Errorf("%w: version", ErrConflict)
	}
	if task.State != StatePaused {
		return AgentTask{}, fmt.Errorf("%w: task is not paused", ErrInvalid)
	}
	if expired, err := r.expireBeforeWork(ctx, task, now); err != nil {
		return expired, err
	}
	if task.CurrentStep < len(task.Plan.Steps) && task.Plan.Steps[task.CurrentStep].State == StepRunning {
		if task.Plan.Steps[task.CurrentStep].Tier >= TierCommunicate {
			return task, ErrReconciliationRequired
		}
		task.Plan.Steps[task.CurrentStep].State = StepPending
		task.Plan.Steps[task.CurrentStep].StartedAt, task.Plan.Steps[task.CurrentStep].FinishedAt = time.Time{}, time.Time{}
	}
	if task.PausedState.valid() && task.PausedState != StatePaused {
		task.State = task.PausedState
		task.Wake = cloneWake(task.PausedWake)
	} else {
		task.State = StateRunning
	}
	task.PausedState, task.PausedWake = "", nil
	task.Version++
	task.UpdatedAt = now.UTC()
	if err := r.store.Save(ctx, task, expectedVersion); err != nil {
		return AgentTask{}, err
	}
	return task, nil
}
func (r *Runtime) Cancel(ctx context.Context, id string, expectedVersion uint64, now time.Time) (AgentTask, error) {
	return r.transition(ctx, id, expectedVersion, StateCancelled, now)
}
func (r *Runtime) transition(ctx context.Context, id string, expected uint64, state TaskState, now time.Time) (AgentTask, error) {
	if now.IsZero() {
		return AgentTask{}, fmt.Errorf("%w: time is required", ErrInvalid)
	}
	task, err := r.store.Get(ctx, id)
	if err != nil {
		return AgentTask{}, err
	}
	if task.Version != expected {
		return AgentTask{}, fmt.Errorf("%w: version", ErrConflict)
	}
	if task.State.terminal() {
		return AgentTask{}, ErrTerminal
	}
	if state == StatePaused && task.State == StatePaused {
		return task, nil
	}
	if state == StatePaused {
		task.PausedState = task.State
		task.PausedWake = cloneWake(task.Wake)
		if task.CurrentStep < len(task.Plan.Steps) && task.Plan.Steps[task.CurrentStep].State == StepRunning && task.Plan.Steps[task.CurrentStep].Tier >= TierCommunicate {
			task.FailureCode, task.FailureDetail = "AMBIGUOUS_EFFECT", "owner reconciliation required before this effect can advance"
		}
	} else {
		task.PausedState = ""
		task.PausedWake = nil
	}
	task.State = state
	task.Wake = nil
	task.WorkerLease, task.ModelSession = "", ""
	task.Version++
	task.UpdatedAt = now.UTC()
	if err := r.store.Save(ctx, task, expected); err != nil {
		return AgentTask{}, err
	}
	return task, nil
}

// RecoverStale releases a checkpoint left RUNNING by a dead worker. Read and
// private-draft work can be retried; externally visible effects stay paused
// until their owner reconciles the outcome.
func (r *Runtime) RecoverStale(ctx context.Context, id string, expectedVersion uint64, now time.Time) (AgentTask, error) {
	if now.IsZero() {
		return AgentTask{}, fmt.Errorf("%w: time is required", ErrInvalid)
	}
	task, err := r.store.Get(ctx, id)
	if err != nil {
		return AgentTask{}, err
	}
	if task.Version != expectedVersion {
		return AgentTask{}, fmt.Errorf("%w: version", ErrConflict)
	}
	if task.State != StateRunning || task.CurrentStep >= len(task.Plan.Steps) || task.Plan.Steps[task.CurrentStep].State != StepRunning || task.WorkerLease == "" {
		return AgentTask{}, fmt.Errorf("%w: no stale running step", ErrStepNotReady)
	}
	if expired, err := r.expireBeforeWork(ctx, task, now); err != nil {
		return expired, err
	}
	if task.Plan.Steps[task.CurrentStep].Tier >= TierCommunicate {
		task.PausedState = StateRunning
		task.State = StatePaused
		task.FailureCode, task.FailureDetail = "AMBIGUOUS_EFFECT", "owner reconciliation required before this effect can advance"
	} else {
		task.Plan.Steps[task.CurrentStep].State = StepPending
		task.Plan.Steps[task.CurrentStep].StartedAt, task.Plan.Steps[task.CurrentStep].FinishedAt = time.Time{}, time.Time{}
	}
	task.WorkerLease, task.ModelSession = "", ""
	task.Version++
	task.UpdatedAt = now.UTC()
	if err := r.store.Save(ctx, task, expectedVersion); err != nil {
		return AgentTask{}, err
	}
	return task, nil
}

// SweepStaleTasks is the caller-owned repair loop for lost wakes and expiry.
// It never executes a step. A timer/signal scheduler may call it repeatedly;
// version checks ensure a concurrent wake wins without a second transition.
func (r *Runtime) SweepStaleTasks(ctx context.Context, now time.Time) ([]AgentTask, error) {
	if r == nil || now.IsZero() {
		return nil, fmt.Errorf("%w: runtime and time are required", ErrInvalid)
	}
	tasks, err := r.store.List(ctx)
	if err != nil {
		return nil, err
	}
	settled := make([]AgentTask, 0)
	for _, task := range tasks {
		if task.State.terminal() {
			continue
		}
		code := ""
		detail := ""
		switch {
		case !task.ExpiresAt.After(now):
			task.State, task.Wake, task.WorkerLease, task.ModelSession = StateExpired, nil, "", ""
			code, detail = "TASK_EXPIRED", "maximum task lifetime elapsed"
		case (task.State == StateWaiting || task.State == StateAwaitingApproval) && task.Wake != nil && !task.Wake.StaleAfter.IsZero() && !task.Wake.StaleAfter.After(now):
			task.State, task.Wake, task.WorkerLease, task.ModelSession = StateFailed, nil, "", ""
			code, detail = "LOST_WAKE", "wake condition became stale without a delivered wake"
		default:
			continue
		}
		task.FailureCode, task.FailureDetail = code, detail
		task.Version++
		task.UpdatedAt = now.UTC()
		if saveErr := r.store.Save(ctx, task, task.Version-1); saveErr != nil {
			if errors.Is(saveErr, ErrConflict) {
				continue
			}
			return nil, saveErr
		}
		settled = append(settled, task)
	}
	return settled, nil
}

type OwnerRead struct {
	Ref, SourceID, ValueRef string
	Revoked                 bool
	Taint                   []string
}
type OwnerReader interface {
	Read(context.Context, string) (OwnerRead, error)
}
type TaskContext struct {
	Goal          string
	Constraints   []string
	Plan          AgentPlan
	Results       []LedgerEntry
	Notes         []LedgerEntry
	OpenQuestions []LedgerEntry
	Artifacts     []LedgerEntry
	FreshReads    []OwnerRead
}

func (r *Runtime) AddModelNote(ctx context.Context, id string, expectedVersion uint64, ref, digest string, now time.Time) (AgentTask, error) {
	task, err := r.store.Get(ctx, id)
	if err != nil {
		return AgentTask{}, err
	}
	var sources []string
	for _, entry := range task.Ledger.Entries {
		if entry.Revoked {
			continue
		}
		for _, source := range ledgerSources(entry) {
			if !hasTaint(sources, source) {
				sources = append(sources, source)
			}
		}
	}
	return r.appendEntry(ctx, id, expectedVersion, LedgerEntry{Kind: "MODEL_NOTE", Ref: ref, Digest: digest, SourceIDs: sources, Taint: []string{"AGENT_DERIVED"}}, now)
}
func (r *Runtime) AddArtifact(ctx context.Context, id string, expectedVersion uint64, ref, digest string, now time.Time) (AgentTask, error) {
	return r.appendEntry(ctx, id, expectedVersion, LedgerEntry{Kind: "ARTIFACT", Ref: ref, Digest: digest}, now)
}
func (r *Runtime) AddOpenQuestion(ctx context.Context, id string, expectedVersion uint64, ref, digest string, now time.Time) (AgentTask, error) {
	return r.appendEntry(ctx, id, expectedVersion, LedgerEntry{Kind: "OPEN_QUESTION", Ref: ref, Digest: digest}, now)
}

// RevokeSource marks every ledger reference from one owner source revoked.
// RebuildContext filters these entries and asks the owner for fresh reads, so
// a cached summary cannot preserve access after an owner-side revocation.
func (r *Runtime) RevokeSource(ctx context.Context, id, sourceID string, expectedVersion uint64, now time.Time) (AgentTask, error) {
	if strings.TrimSpace(sourceID) == "" {
		return AgentTask{}, fmt.Errorf("%w: source id is required", ErrInvalid)
	}
	task, err := r.store.Get(ctx, id)
	if err != nil {
		return AgentTask{}, err
	}
	if task.Version != expectedVersion {
		return AgentTask{}, fmt.Errorf("%w: version", ErrConflict)
	}
	for i := range task.Ledger.Entries {
		if hasTaint(ledgerSources(task.Ledger.Entries[i]), sourceID) {
			task.Ledger.Entries[i].Revoked = true
		}
	}
	task.Version++
	task.UpdatedAt = now.UTC()
	if err := r.store.Save(ctx, task, expectedVersion); err != nil {
		return AgentTask{}, err
	}
	return task, nil
}
func (r *Runtime) appendEntry(ctx context.Context, id string, expected uint64, entry LedgerEntry, now time.Time) (AgentTask, error) {
	if strings.TrimSpace(entry.Ref) == "" || now.IsZero() {
		return AgentTask{}, fmt.Errorf("%w: ledger reference and time required", ErrInvalid)
	}
	task, err := r.store.Get(ctx, id)
	if err != nil {
		return AgentTask{}, err
	}
	if task.Version != expected {
		return AgentTask{}, fmt.Errorf("%w: version", ErrConflict)
	}
	appendLedger(&task, entry)
	task.Version++
	task.UpdatedAt = now.UTC()
	if err := r.store.Save(ctx, task, expected); err != nil {
		return AgentTask{}, err
	}
	return task, nil
}

func (r *Runtime) RebuildContext(ctx context.Context, id string, reader OwnerReader) (TaskContext, error) {
	task, err := r.store.Get(ctx, id)
	if err != nil {
		return TaskContext{}, err
	}
	result := TaskContext{Goal: task.Goal, Constraints: append([]string(nil), task.Constraints...), Plan: clonePlan(task.Plan)}
	freshBySource := make(map[string]OwnerRead)
	freshRevoked := make(map[string]bool)
	for _, entry := range task.Ledger.Entries {
		if entry.Revoked {
			continue
		}
		entryRevoked := false
		for _, sourceID := range ledgerSources(entry) {
			if reader == nil {
				return TaskContext{}, fmt.Errorf("%w: source %s needs a current owner read", ErrInvalid, sourceID)
			}
			fresh, ok := freshBySource[sourceID]
			if !ok && !freshRevoked[sourceID] {
				var readErr error
				fresh, readErr = reader.Read(ctx, sourceID)
				if readErr != nil {
					return TaskContext{}, readErr
				}
				if fresh.SourceID != sourceID {
					return TaskContext{}, fmt.Errorf("%w: owner returned a different source", ErrInvalid)
				}
				if fresh.Revoked {
					freshRevoked[sourceID] = true
				} else {
					freshBySource[sourceID] = cloneOwnerRead(fresh)
				}
			}
			if freshRevoked[sourceID] {
				entryRevoked = true
			}
		}
		if entryRevoked {
			continue
		}
		switch entry.Kind {
		case "STEP_RESULT":
			result.Results = append(result.Results, cloneEntry(entry))
		case "MODEL_NOTE":
			if hasTaint(entry.Taint, "AGENT_DERIVED") {
				result.Notes = append(result.Notes, cloneEntry(entry))
			}
		case "OPEN_QUESTION":
			result.OpenQuestions = append(result.OpenQuestions, cloneEntry(entry))
		case "ARTIFACT":
			result.Artifacts = append(result.Artifacts, cloneEntry(entry))
		}
	}
	for _, fresh := range freshBySource {
		result.FreshReads = append(result.FreshReads, fresh)
	}
	sort.Slice(result.FreshReads, func(i, j int) bool { return result.FreshReads[i].SourceID < result.FreshReads[j].SourceID })
	return result, nil
}

func appendLedger(task *AgentTask, entry LedgerEntry) {
	entry.Sequence = uint64(len(task.Ledger.Entries) + 1)
	task.Ledger.Entries = append(task.Ledger.Entries, entry)
}
func first(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
func digestText(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}
func cloneEntry(in LedgerEntry) LedgerEntry {
	in.Taint = append([]string(nil), in.Taint...)
	in.SourceIDs = append([]string(nil), in.SourceIDs...)
	return in
}

func ledgerSources(entry LedgerEntry) []string {
	sources := append([]string(nil), entry.SourceIDs...)
	if entry.SourceID != "" && !hasTaint(sources, entry.SourceID) {
		sources = append(sources, entry.SourceID)
	}
	return sources
}
func cloneSteps(in []PlanStep) []PlanStep {
	out := make([]PlanStep, len(in))
	copy(out, in)
	for i := range out {
		out[i].Inputs = append([]InputRef(nil), out[i].Inputs...)
		out[i].Wait = cloneWake(out[i].Wait)
		for j := range out[i].Inputs {
			out[i].Inputs[j].Taint = append([]string(nil), out[i].Inputs[j].Taint...)
		}
	}
	return out
}
func clonePlan(in AgentPlan) AgentPlan {
	in.Steps = cloneSteps(in.Steps)
	in.DocumentReferences = cloneDocumentReferences(in.DocumentReferences)
	in.DocumentOmissions = cloneDocumentOmissions(in.DocumentOmissions)
	in.AnsweringAgent = cloneTaskAgentIdentity(in.AnsweringAgent)
	return in
}
func cloneTask(in AgentTask) AgentTask {
	if in.LastWake != nil {
		event := *in.LastWake
		in.LastWake = &event
	}
	in.Constraints = append([]string(nil), in.Constraints...)
	in.Plan = clonePlan(in.Plan)
	if in.Wake != nil {
		in.Wake = cloneWake(in.Wake)
	}
	if in.PausedWake != nil {
		in.PausedWake = cloneWake(in.PausedWake)
	}
	in.Ledger.Entries = append([]LedgerEntry(nil), in.Ledger.Entries...)
	for i := range in.Ledger.Entries {
		in.Ledger.Entries[i] = cloneEntry(in.Ledger.Entries[i])
	}
	return in
}

func cloneWake(in *WakeCondition) *WakeCondition {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func firstPendingStep(steps []PlanStep) int {
	for i, step := range steps {
		if step.State != StepCompleted {
			return i
		}
	}
	return len(steps)
}

func preserveCompletedSteps(previous, next []PlanStep) []PlanStep {
	completed := make(map[string]PlanStep, len(previous))
	for _, step := range previous {
		if step.State == StepCompleted {
			completed[step.ID] = step
		}
	}
	for i := range next {
		prior, ok := completed[next[i].ID]
		if !ok || digestStep(prior) != digestStep(next[i]) {
			continue
		}
		next[i].State = StepCompleted
		next[i].Attempt = prior.Attempt
		next[i].ResultRef = prior.ResultRef
		next[i].VerificationRef = prior.VerificationRef
		next[i].StartedAt = prior.StartedAt
		next[i].FinishedAt = prior.FinishedAt
	}
	return next
}

func normalizeProposedPlan(plan AgentPlan) AgentPlan {
	plan = clonePlan(plan)
	plan.Confirmed, plan.ConfirmedBy, plan.ConfirmedAt = false, "", time.Time{}
	for i := range plan.Steps {
		step := &plan.Steps[i]
		step.State, step.Attempt = StepPending, 0
		step.Approved, step.ApprovalDigest, step.ApprovalRevision = false, "", 0
		step.ResultRef, step.VerificationRef = "", ""
		step.StartedAt, step.FinishedAt = time.Time{}, time.Time{}
	}
	return plan
}

func cloneOwnerRead(in OwnerRead) OwnerRead {
	in.Taint = append([]string(nil), in.Taint...)
	return in
}

func hasTaint(labels []string, want string) bool {
	for _, label := range labels {
		if label == want {
			return true
		}
	}
	return false
}

// validateWaitSpec checks the optional wake condition a WAIT or ASK_USER step
// carries. A relative timer (DueAt zero, PollAfter set) is resolved to an
// absolute due time when the step parks.
func validateWaitSpec(step PlanStep) error {
	if step.Wait == nil {
		return nil
	}
	if step.Type != StepWait && step.Type != StepAskUser {
		return errors.New("only WAIT and ASK_USER steps carry a wait condition")
	}
	w := step.Wait
	switch w.Kind {
	case WakeTimer:
		if w.DueAt.IsZero() && w.PollAfter <= 0 {
			return errors.New("a TIMER wait needs a due time or a delay")
		}
	case WakePolling:
		if w.PollAfter <= 0 {
			return errors.New("a POLLING wait needs a positive interval")
		}
	case WakeSignal:
		if strings.TrimSpace(w.Key) == "" {
			return errors.New("a SIGNAL wait needs a signal key")
		}
	case WakeUserReply:
	default:
		return errors.New("a wait condition must be TIMER, POLLING, SIGNAL or USER_REPLY")
	}
	if step.Type == StepAskUser && w.Kind != WakeUserReply {
		return errors.New("ASK_USER waits for a USER_REPLY")
	}
	return nil
}

// WaitLike reports whether a step type parks the task instead of running an
// executor: the wake event itself completes the step.
func WaitLike(t StepType) bool { return t == StepWait || t == StepAskUser }

// ParkStep parks a RUNNING task on the WAIT or ASK_USER step at its cursor.
// The step becomes WAITING, the task WAITING with the typed wake condition and
// no worker lease or model session.
func (r *Runtime) ParkStep(ctx context.Context, id string, expectedVersion uint64, condition WakeCondition, now time.Time) (AgentTask, error) {
	if !validWakeCondition(condition) || now.IsZero() {
		return AgentTask{}, fmt.Errorf("%w: wake condition and time are required", ErrInvalidWake)
	}
	task, err := r.store.Get(ctx, id)
	if err != nil {
		return AgentTask{}, err
	}
	if task.Version != expectedVersion {
		return AgentTask{}, fmt.Errorf("%w: version", ErrConflict)
	}
	if expired, err := r.expireBeforeWork(ctx, task, now); err != nil {
		return expired, err
	}
	if task.State != StateRunning || !task.Plan.Confirmed || task.CurrentStep >= len(task.Plan.Steps) {
		return AgentTask{}, fmt.Errorf("%w: task state or cursor is not parkable", ErrStepNotReady)
	}
	step := &task.Plan.Steps[task.CurrentStep]
	if !WaitLike(step.Type) || step.State != StepPending {
		return AgentTask{}, fmt.Errorf("%w: step %s is %s %s", ErrStepNotReady, step.ID, step.Type, step.State)
	}
	step.State = StepWaiting
	step.Attempt++
	step.StartedAt = now.UTC()
	step.FinishedAt = time.Time{}
	task.Wake = &condition
	task.LastWake = nil
	task.State = StateWaiting
	task.WorkerLease, task.ModelSession = "", ""
	task.Version++
	task.UpdatedAt = now.UTC()
	if err := r.store.Save(ctx, task, expectedVersion); err != nil {
		return AgentTask{}, err
	}
	return task, nil
}

// CompleteWait completes the WAITING step of a task that a wake already
// resumed (state RUNNING): the wake event is the step result.
func (r *Runtime) CompleteWait(ctx context.Context, id string, expectedVersion uint64, result StepResult, now time.Time) (AgentTask, error) {
	if now.IsZero() {
		return AgentTask{}, fmt.Errorf("%w: time is required", ErrInvalid)
	}
	task, err := r.store.Get(ctx, id)
	if err != nil {
		return AgentTask{}, err
	}
	if task.Version != expectedVersion {
		return AgentTask{}, fmt.Errorf("%w: version", ErrConflict)
	}
	if task.State != StateRunning || task.CurrentStep >= len(task.Plan.Steps) {
		return AgentTask{}, fmt.Errorf("%w: task is not resumed", ErrStepNotReady)
	}
	step := task.Plan.Steps[task.CurrentStep]
	if !WaitLike(step.Type) || step.State != StepWaiting {
		return AgentTask{}, fmt.Errorf("%w: step %s is %s %s", ErrStepNotReady, step.ID, step.Type, step.State)
	}
	return r.finishStep(ctx, id, expectedVersion, now, step.ID, result, nil, true, nil)
}
