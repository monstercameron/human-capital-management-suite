// Package agentrevoke fences delegated agent work when the authority behind a
// grant changes. Revocation is deliberately a control-plane boundary: it
// does not decide why a user lost access, but it makes every affected grant,
// waiting task, and unsubmitted approval stop before the next step.
package agentrevoke

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

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
)

const (
	// MaxTokenLifetime is the maximum lifetime a delegated step credential may
	// have. A revocation therefore bounds an already-issued credential to five
	// minutes even when its holder never asks for a new one.
	MaxTokenLifetime = 5 * time.Minute

	ReasonUserDeactivated       = "AUTHORITY_REVOKED_USER_DEACTIVATED"
	ReasonRoleChanged           = "AUTHORITY_REVOKED_ROLE_CHANGED"
	ReasonRelationshipChanged   = "AUTHORITY_REVOKED_RELATIONSHIP_CHANGED"
	ReasonSessionFamilyRevoked  = "AUTHORITY_REVOKED_SESSION_FAMILY"
	ReasonConnectionUnlinked    = "AUTHORITY_REVOKED_CONNECTION_UNLINKED"
	ReasonConnectionAdminRevoke = "AUTHORITY_REVOKED_CONNECTION_ADMIN"
	ReasonAgentQuarantined      = "AUTHORITY_REVOKED_AGENT_QUARANTINED"
	ReasonTenantKillSwitch      = "AUTHORITY_REVOKED_TENANT_KILL_SWITCH"
)

var (
	ErrInvalid       = errors.New("agentrevoke: invalid request")
	ErrRevoked       = errors.New("agentrevoke: authority revoked")
	ErrEpochStale    = errors.New("agentrevoke: revocation epoch is stale")
	ErrNotFound      = errors.New("agentrevoke: grant not found")
	ErrTaskConflict  = errors.New("agentrevoke: task changed while revoking")
	ErrSourceInvalid = errors.New("agentrevoke: invalid event source")
)

// Cause is the authoritative reason for a revocation event. These values are
// stable policy codes, not UI strings.
type Cause string

const (
	CauseUserDeactivated       Cause = "USER_DEACTIVATED"
	CauseRoleChanged           Cause = "ROLE_CHANGED"
	CauseRelationshipChanged   Cause = "RELATIONSHIP_CHANGED"
	CauseSessionFamilyRevoked  Cause = "SESSION_FAMILY_REVOKED"
	CauseConnectionUnlinked    Cause = "CONNECTION_UNLINKED"
	CauseConnectionAdminRevoke Cause = "CONNECTION_ADMIN_REVOKED"
	CauseAgentQuarantined      Cause = "AGENT_QUARANTINED"
	CauseTenantKillSwitch      Cause = "TENANT_KILL_SWITCH"
)

func (c Cause) valid() bool {
	switch c {
	case CauseUserDeactivated, CauseRoleChanged, CauseRelationshipChanged,
		CauseSessionFamilyRevoked, CauseConnectionUnlinked,
		CauseConnectionAdminRevoke, CauseAgentQuarantined, CauseTenantKillSwitch:
		return true
	default:
		return false
	}
}

func reasonFor(c Cause) string {
	switch c {
	case CauseUserDeactivated:
		return ReasonUserDeactivated
	case CauseRoleChanged:
		return ReasonRoleChanged
	case CauseRelationshipChanged:
		return ReasonRelationshipChanged
	case CauseSessionFamilyRevoked:
		return ReasonSessionFamilyRevoked
	case CauseConnectionUnlinked:
		return ReasonConnectionUnlinked
	case CauseConnectionAdminRevoke:
		return ReasonConnectionAdminRevoke
	case CauseAgentQuarantined:
		return ReasonAgentQuarantined
	case CauseTenantKillSwitch:
		return ReasonTenantKillSwitch
	default:
		return ""
	}
}

// Event is emitted by identity, role, session, connection, agent-release, or
// tenant-control code. The producer must resolve the affected user or scope;
// this package never infers a person's authority from an agent request.
type Event struct {
	TenantID        string
	UserID          string
	SessionFamilyID string
	ConnectionID    string
	AgentVersion    string
	InstallationID  string
	Cause           Cause
	OccurredAt      time.Time
}

// ScopeKind identifies an independent revocation epoch.
type ScopeKind string

const (
	ScopeTenant       ScopeKind = "TENANT"
	ScopeUser         ScopeKind = "USER"
	ScopeSession      ScopeKind = "SESSION_FAMILY"
	ScopeConnection   ScopeKind = "CONNECTION"
	ScopeAgent        ScopeKind = "AGENT_VERSION"
	ScopeInstallation ScopeKind = "INSTALLATION"
)

// Scope is the durable key for one monotonic epoch.
type Scope struct {
	Kind     ScopeKind
	TenantID string
	Subject  string
}

func (s Scope) key() string { return string(s.Kind) + "\x00" + s.TenantID + "\x00" + s.Subject }

func (s Scope) valid() bool {
	return strings.TrimSpace(s.TenantID) != "" && strings.TrimSpace(s.Subject) != "" && strings.TrimSpace(string(s.Kind)) != ""
}

// Grant is the minimum server-side index needed to find affected delegated
// authority. Epochs records the values captured when the grant was created.
// It contains no bearer token or secret.
type Grant struct {
	ID              string
	TaskID          string
	TenantID        string
	UserID          string
	SessionFamilyID string
	ConnectionID    string
	AgentVersion    string
	InstallationID  string
	Epochs          map[string]uint64
	Revoked         bool
}

// EpochStore is implemented by the agent delegation persistence layer. Bump
// must be transactional with the identity/session/connection event that
// caused it, and must never decrease an epoch.
type EpochStore interface {
	Current(context.Context, Scope) (uint64, error)
	Bump(context.Context, Scope, string) (uint64, error)
}

// GrantStore is the narrow grant-index seam. Revoke must be monotonic and
// must never reinstate a grant.
type GrantStore interface {
	Get(context.Context, string) (Grant, error)
	List(context.Context) ([]Grant, error)
	Revoke(context.Context, string, string) error
}

// TaskStore is the narrow task-runtime seam. Save must use optimistic version
// checking and preserve the task tenant boundary.
type TaskStore interface {
	List(context.Context) ([]agentrun.AgentTask, error)
	Save(context.Context, agentrun.AgentTask, uint64) error
}

// Source is the subscription seam for existing identity and role-change
// event buses. The returned function detaches the subscription.
type Source interface {
	Subscribe(func(Event)) (func(), error)
}

// Config wires the revocation coordinator. All stores are required because a
// successful event must fence credentials, grants, and durable task state as
// one control operation.
type Config struct {
	Epochs EpochStore
	Grants GrantStore
	Tasks  TaskStore
	Clock  func() time.Time
}

// Coordinator applies revocations and admits steps against current epochs.
// Its mutex serializes those operations only inside this process; a production
// store must provide the cross-process transaction used by the worker gateway.
type Coordinator struct {
	epochs EpochStore
	grants GrantStore
	tasks  TaskStore
	clock  func() time.Time
	mu     sync.Mutex
}

// New constructs a coordinator with no package-level mutable state.
func New(cfg Config) (*Coordinator, error) {
	if cfg.Epochs == nil || cfg.Grants == nil || cfg.Tasks == nil {
		return nil, fmt.Errorf("%w: epochs, grants and tasks are required", ErrInvalid)
	}
	if cfg.Clock == nil {
		cfg.Clock = func() time.Time { return time.Now().UTC() }
	}
	return &Coordinator{epochs: cfg.Epochs, grants: cfg.Grants, tasks: cfg.Tasks, clock: cfg.Clock}, nil
}

// EpochAdvance records one committed scope bump.
type EpochAdvance struct {
	Scope  Scope
	Before uint64
	After  uint64
}

// Result is an immutable summary of one revocation application.
type Result struct {
	Event           Event
	Reason          string
	Epochs          []EpochAdvance
	RevokedGrants   int
	PausedTasks     int
	VoidedApprovals int
	Digest          string
}

// Apply bumps all affected epochs, revokes matching grants, pauses open tasks,
// clears worker/model leases, and voids all unsubmitted approval material.
func (c *Coordinator) Apply(ctx context.Context, event Event) (Result, error) {
	if c == nil {
		return Result{}, fmt.Errorf("%w: nil coordinator", ErrInvalid)
	}
	if err := validateEvent(event); err != nil {
		return Result{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.applyLocked(ctx, event)
}

func (c *Coordinator) applyLocked(ctx context.Context, event Event) (Result, error) {
	scopes := eventScopes(event)
	advances := make([]EpochAdvance, 0, len(scopes))
	for _, scope := range scopes {
		before, err := c.epochs.Current(ctx, scope)
		if err != nil {
			return Result{}, err
		}
		after, err := c.epochs.Bump(ctx, scope, reasonFor(event.Cause))
		if err != nil {
			return Result{}, err
		}
		if after <= before {
			return Result{}, fmt.Errorf("%w: %s did not advance", ErrEpochStale, scope.key())
		}
		advances = append(advances, EpochAdvance{Scope: scope, Before: before, After: after})
	}
	grants, err := c.grants.List(ctx)
	if err != nil {
		return Result{}, err
	}
	grantCount := 0
	affectedTasks := make(map[string]struct{})
	for _, grant := range grants {
		if grant.TenantID != event.TenantID || grant.Revoked || !grantMatchesEvent(grant, event) {
			continue
		}
		if present(grant.TaskID) {
			affectedTasks[grant.TaskID] = struct{}{}
		}
		if err := c.grants.Revoke(ctx, grant.ID, reasonFor(event.Cause)); err != nil {
			return Result{}, err
		}
		grantCount++
	}
	paused, voided, err := c.pauseTasksLocked(ctx, event, affectedTasks)
	if err != nil {
		return Result{}, err
	}
	result := Result{Event: event, Reason: reasonFor(event.Cause), Epochs: advances,
		RevokedGrants: grantCount, PausedTasks: paused, VoidedApprovals: voided}
	result.Digest = resultDigest(result)
	return result, nil
}

// Subscribe connects an existing identity/role event stream to this
// coordinator. Events are handled synchronously by the source callback; a
// source that needs its own retry policy can call Apply directly instead.
func (c *Coordinator) Subscribe(source Source) (func(), error) {
	if c == nil || source == nil {
		return nil, ErrSourceInvalid
	}
	return source.Subscribe(func(event Event) { _, _ = c.Apply(context.Background(), event) })
}

// StepRequest identifies the exact grant and context a worker is about to
// use. All fields are server-resolved; an agent cannot widen them.
type StepRequest struct {
	GrantID string
	Now     time.Time
}

// StepPermit is the successful linearized admission of one step. Its expiry
// is capped at five minutes and its epoch snapshot is diagnostic evidence.
type StepPermit struct {
	GrantID   string
	Epochs    map[string]uint64
	ExpiresAt time.Time
}

// StartStep checks grant state and every applicable current epoch while
// holding the same process-local mutex used by Apply. A return of ErrRevoked
// refuses execution after this coordinator applies an identity event. A
// multi-process gateway must make this preflight atomic with its durable epoch
// store before relying on it for cross-instance fencing.
func (c *Coordinator) StartStep(ctx context.Context, req StepRequest) (StepPermit, error) {
	if c == nil || strings.TrimSpace(req.GrantID) == "" {
		return StepPermit{}, fmt.Errorf("%w: grant id is required", ErrInvalid)
	}
	now := req.Now
	if now.IsZero() {
		now = c.clock().UTC()
	} else {
		now = now.UTC()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	grant, err := c.grants.Get(ctx, req.GrantID)
	if err != nil {
		return StepPermit{}, err
	}
	if grant.Revoked {
		return StepPermit{}, ErrRevoked
	}
	for _, scope := range grantScopes(grant) {
		current, err := c.epochs.Current(ctx, scope)
		if err != nil {
			return StepPermit{}, err
		}
		captured := grant.Epochs[scope.key()]
		if captured == 0 || current != captured {
			return StepPermit{}, fmt.Errorf("%w: %s", ErrRevoked, scope.key())
		}
	}
	return StepPermit{GrantID: grant.ID, Epochs: cloneEpochs(grant.Epochs), ExpiresAt: now.Add(MaxTokenLifetime)}, nil
}

// Valid reports whether a permit is still inside its bounded token window.
func (p StepPermit) Valid(at time.Time) bool {
	return !p.ExpiresAt.IsZero() && at.Before(p.ExpiresAt)
}

func validateEvent(event Event) error {
	if strings.TrimSpace(event.TenantID) == "" || !event.Cause.valid() {
		return fmt.Errorf("%w: tenant and supported cause are required", ErrInvalid)
	}
	switch event.Cause {
	case CauseUserDeactivated, CauseRoleChanged, CauseRelationshipChanged:
		if !present(event.UserID) {
			return fmt.Errorf("%w: user is required for %s", ErrInvalid, event.Cause)
		}
	case CauseSessionFamilyRevoked:
		if !present(event.UserID) || !present(event.SessionFamilyID) {
			return fmt.Errorf("%w: user and session family are required", ErrInvalid)
		}
	case CauseConnectionUnlinked:
		if !present(event.UserID) || !present(event.ConnectionID) {
			return fmt.Errorf("%w: user and connection are required", ErrInvalid)
		}
	case CauseConnectionAdminRevoke:
		if !present(event.ConnectionID) {
			return fmt.Errorf("%w: connection is required", ErrInvalid)
		}
	case CauseAgentQuarantined:
		if !present(event.AgentVersion) && !present(event.InstallationID) {
			return fmt.Errorf("%w: agent version or installation is required", ErrInvalid)
		}
	}
	return nil
}

func eventScopes(event Event) []Scope {
	seen := map[string]Scope{}
	add := func(kind ScopeKind, subject string) {
		if subject == "" {
			return
		}
		scope := Scope{Kind: kind, TenantID: event.TenantID, Subject: subject}
		seen[scope.key()] = scope
	}
	switch event.Cause {
	case CauseTenantKillSwitch:
		add(ScopeTenant, event.TenantID)
	case CauseConnectionAdminRevoke:
		add(ScopeConnection, event.ConnectionID)
	case CauseConnectionUnlinked:
		add(ScopeUser, event.UserID)
		add(ScopeConnection, event.ConnectionID)
	case CauseSessionFamilyRevoked:
		add(ScopeSession, event.SessionFamilyID)
	case CauseAgentQuarantined:
		add(ScopeAgent, event.AgentVersion)
		add(ScopeInstallation, event.InstallationID)
	default:
		add(ScopeUser, event.UserID)
	}
	scopes := make([]Scope, 0, len(seen))
	for _, scope := range seen {
		scopes = append(scopes, scope)
	}
	sort.Slice(scopes, func(i, j int) bool { return scopes[i].key() < scopes[j].key() })
	return scopes
}

func present(value string) bool { return strings.TrimSpace(value) != "" }

func grantScopes(grant Grant) []Scope {
	values := []Scope{
		{Kind: ScopeTenant, TenantID: grant.TenantID, Subject: grant.TenantID},
		{Kind: ScopeUser, TenantID: grant.TenantID, Subject: grant.UserID},
		{Kind: ScopeSession, TenantID: grant.TenantID, Subject: grant.SessionFamilyID},
		{Kind: ScopeConnection, TenantID: grant.TenantID, Subject: grant.ConnectionID},
		{Kind: ScopeAgent, TenantID: grant.TenantID, Subject: grant.AgentVersion},
		{Kind: ScopeInstallation, TenantID: grant.TenantID, Subject: grant.InstallationID},
	}
	result := make([]Scope, 0, len(values))
	for _, value := range values {
		if value.valid() {
			result = append(result, value)
		}
	}
	return result
}

func grantMatchesEvent(grant Grant, event Event) bool {
	switch event.Cause {
	case CauseTenantKillSwitch:
		return true
	case CauseUserDeactivated, CauseRoleChanged, CauseRelationshipChanged:
		return grant.UserID == event.UserID
	case CauseSessionFamilyRevoked:
		return grant.UserID == event.UserID && grant.SessionFamilyID == event.SessionFamilyID
	case CauseConnectionUnlinked:
		return grant.UserID == event.UserID && grant.ConnectionID == event.ConnectionID
	case CauseConnectionAdminRevoke:
		return grant.ConnectionID == event.ConnectionID
	case CauseAgentQuarantined:
		return (event.AgentVersion != "" && grant.AgentVersion == event.AgentVersion) ||
			(event.InstallationID != "" && grant.InstallationID == event.InstallationID)
	default:
		return false
	}
}

func (c *Coordinator) pauseTasksLocked(ctx context.Context, event Event, affectedTasks map[string]struct{}) (int, int, error) {
	tasks, err := c.tasks.List(ctx)
	if err != nil {
		return 0, 0, err
	}
	paused, voided := 0, 0
	now := event.OccurredAt
	if now.IsZero() {
		now = c.clock().UTC()
	}
	for _, task := range tasks {
		if task.TenantID != event.TenantID || !taskMatchesEvent(task, event, affectedTasks) || terminal(task.State) {
			continue
		}
		for i := range task.Plan.Steps {
			step := &task.Plan.Steps[i]
			if step.Approved || step.ApprovalDigest != "" {
				step.Approved = false
				step.ApprovalDigest = ""
				if step.State == agentrun.StepAwaitingApproval {
					step.State = agentrun.StepPending
				}
				voided++
			}
		}
		if task.State != agentrun.StatePaused {
			task.PausedState = task.State
			task.PausedWake = cloneWake(task.Wake)
			task.State = agentrun.StatePaused
			task.Wake = nil
			task.WorkerLease, task.ModelSession = "", ""
			paused++
		}
		task.FailureCode = reasonFor(event.Cause)
		task.FailureDetail = "delegated authority revoked before the next step"
		task.Version++
		task.UpdatedAt = now.UTC()
		if err := c.tasks.Save(ctx, task, task.Version-1); err != nil {
			if errors.Is(err, agentrun.ErrConflict) {
				return paused, voided, fmt.Errorf("%w: %s", ErrTaskConflict, task.ID)
			}
			return paused, voided, err
		}
	}
	return paused, voided, nil
}

func taskMatchesEvent(task agentrun.AgentTask, event Event, affectedTasks map[string]struct{}) bool {
	if event.Cause == CauseTenantKillSwitch {
		return true
	}
	if event.Cause == CauseSessionFamilyRevoked || event.Cause == CauseAgentQuarantined {
		_, affected := affectedTasks[task.ID]
		return affected
	}
	if event.UserID != "" && task.UserID != event.UserID {
		return false
	}
	if event.ConnectionID == "" {
		return present(event.UserID)
	}
	for _, step := range task.Plan.Steps {
		if step.ConnectionID == event.ConnectionID {
			return true
		}
	}
	return false
}

func terminal(state agentrun.TaskState) bool {
	switch state {
	case agentrun.StateCompleted, agentrun.StateFailed, agentrun.StateCancelled, agentrun.StateExpired:
		return true
	default:
		return false
	}
}

func cloneWake(in *agentrun.WakeCondition) *agentrun.WakeCondition {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func cloneEpochs(in map[string]uint64) map[string]uint64 {
	out := make(map[string]uint64, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func resultDigest(result Result) string {
	type epoch struct {
		Kind, Tenant, Subject string
		Before, After         uint64
	}
	epochs := make([]epoch, len(result.Epochs))
	for i, item := range result.Epochs {
		epochs[i] = epoch{string(item.Scope.Kind), item.Scope.TenantID, item.Scope.Subject, item.Before, item.After}
	}
	payload := struct {
		Event                   Event
		Reason                  string
		Epochs                  []epoch
		Revoked, Paused, Voided int
	}{result.Event, result.Reason, epochs, result.RevokedGrants, result.PausedTasks, result.VoidedApprovals}
	encoded, _ := json.Marshal(payload)
	sum := sha256.Sum256(append([]byte("hcm-next-agent-revoke/v1\x00"), encoded...))
	return "sha256:" + hex.EncodeToString(sum[:])
}
