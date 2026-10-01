package projectboard

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

var (
	ErrWIPPolicyInvalid        = errors.New("projectboard: invalid WIP policy")
	ErrWIPPolicyConflict       = errors.New("projectboard: WIP policy version conflict")
	ErrWIPTaskConflict         = errors.New("projectboard: WIP task version conflict")
	ErrWIPLimitExceeded        = errors.New("projectboard: WIP limit exceeded")
	ErrWIPOverrideUnauthorized = errors.New("projectboard: WIP override unauthorized")
	ErrWIPOverrideReason       = errors.New("projectboard: WIP override reason required")
)

type WIPMode string

const (
	WIPAdvisory WIPMode = "ADVISORY"
	WIPHard     WIPMode = "HARD"
)

type WIPPolicy struct {
	Version           uint64   `json:"version"`
	Mode              WIPMode  `json:"mode"`
	Limit             int      `json:"limit"`
	EligibleStatusIDs []string `json:"eligible_status_ids"`
}

type WIPTask struct {
	ID       string `json:"id"`
	StatusID string `json:"status_id"`
	Revision uint64 `json:"revision"`
}

type WIPOverride struct {
	ActorID       string `json:"actor_id"`
	Reason        string `json:"reason"`
	PolicyVersion uint64 `json:"policy_version"`
	TaskID        string `json:"task_id"`
}

type WIPState struct {
	Policy      WIPPolicy     `json:"policy"`
	ActiveCount int           `json:"active_count"`
	Remaining   int           `json:"remaining"`
	Overrides   []WIPOverride `json:"overrides,omitempty"`
}

type WIPOverrideAuthorizer interface {
	CanOverrideWIP(string) bool
}

type WIPOverrideAuthorizerFunc func(string) bool

func (f WIPOverrideAuthorizerFunc) CanOverrideWIP(actorID string) bool { return f(actorID) }

type WIPMoveRequest struct {
	TaskID                string
	FromStatusID          string
	ToStatusID            string
	ExpectedTaskRevision  uint64
	ExpectedPolicyVersion uint64
	ActorID               string
	OverrideReason        string
}

type WIPMoveResult struct {
	Task       WIPTask
	State      WIPState
	Advisory   bool
	Overridden bool
}

type WIPController struct {
	mu         sync.Mutex
	policy     WIPPolicy
	tasks      map[string]WIPTask
	overrides  []WIPOverride
	authorizer WIPOverrideAuthorizer
}

func NewWIPController(policy WIPPolicy, tasks []WIPTask, authorizer WIPOverrideAuthorizer) (*WIPController, error) {
	if err := validateWIPPolicy(policy); err != nil {
		return nil, err
	}
	controller := &WIPController{policy: cloneValue(policy), tasks: make(map[string]WIPTask), authorizer: authorizer}
	for _, task := range tasks {
		if task.ID == "" || task.Revision == 0 || task.StatusID == "" {
			return nil, fmt.Errorf("%w: tasks require an ID, status, and revision", ErrWIPPolicyInvalid)
		}
		if _, exists := controller.tasks[task.ID]; exists {
			return nil, fmt.Errorf("%w: duplicate task %q", ErrWIPPolicyInvalid, task.ID)
		}
		controller.tasks[task.ID] = task
	}
	return controller, nil
}

func (c *WIPController) State() WIPState {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stateLocked()
}

func (c *WIPController) SetPolicy(expectedVersion uint64, next WIPPolicy) error {
	if c == nil {
		return ErrWIPPolicyInvalid
	}
	if err := validateWIPPolicy(next); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.policy.Version != expectedVersion || next.Version != expectedVersion+1 {
		return ErrWIPPolicyConflict
	}
	c.policy = cloneValue(next)
	return nil
}

func (c *WIPController) Move(request WIPMoveRequest) (WIPMoveResult, error) {
	if c == nil || request.TaskID == "" || request.FromStatusID == "" || request.ToStatusID == "" || request.ActorID == "" {
		return WIPMoveResult{}, ErrWIPPolicyInvalid
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if request.ExpectedPolicyVersion != c.policy.Version {
		return WIPMoveResult{}, ErrWIPPolicyConflict
	}
	task, ok := c.tasks[request.TaskID]
	if !ok || task.StatusID != request.FromStatusID || task.Revision != request.ExpectedTaskRevision {
		return WIPMoveResult{}, ErrWIPTaskConflict
	}
	entering := eligible(c.policy, request.ToStatusID) && !eligible(c.policy, task.StatusID)
	active := c.activeCountLocked()
	if entering && active >= c.policy.Limit {
		if c.policy.Mode == WIPHard {
			if request.OverrideReason == "" {
				return WIPMoveResult{}, ErrWIPLimitExceeded
			}
			if c.authorizer == nil || !c.authorizer.CanOverrideWIP(request.ActorID) {
				return WIPMoveResult{}, ErrWIPOverrideUnauthorized
			}
			if strings.TrimSpace(request.OverrideReason) == "" {
				return WIPMoveResult{}, ErrWIPOverrideReason
			}
			c.overrides = append(c.overrides, WIPOverride{ActorID: request.ActorID, Reason: strings.TrimSpace(request.OverrideReason), PolicyVersion: c.policy.Version, TaskID: request.TaskID})
		}
	}
	task.StatusID, task.Revision = request.ToStatusID, task.Revision+1
	c.tasks[task.ID] = task
	return WIPMoveResult{Task: task, State: c.stateLocked(), Advisory: entering && c.policy.Mode == WIPAdvisory, Overridden: entering && c.policy.Mode == WIPHard && active >= c.policy.Limit}, nil
}

func validateWIPPolicy(policy WIPPolicy) error {
	if policy.Version == 0 || policy.Limit < 1 || (policy.Mode != WIPAdvisory && policy.Mode != WIPHard) || len(policy.EligibleStatusIDs) == 0 {
		return fmt.Errorf("%w: version, positive limit, mode, and eligible statuses are required", ErrWIPPolicyInvalid)
	}
	seen := make(map[string]bool, len(policy.EligibleStatusIDs))
	for _, id := range policy.EligibleStatusIDs {
		if strings.TrimSpace(id) == "" || seen[id] {
			return fmt.Errorf("%w: eligible status IDs must be unique and nonempty", ErrWIPPolicyInvalid)
		}
		seen[id] = true
	}
	return nil
}

func eligible(policy WIPPolicy, statusID string) bool {
	for _, id := range policy.EligibleStatusIDs {
		if id == statusID {
			return true
		}
	}
	return false
}

func (c *WIPController) activeCountLocked() int {
	count := 0
	for _, task := range c.tasks {
		if eligible(c.policy, task.StatusID) {
			count++
		}
	}
	return count
}

func (c *WIPController) stateLocked() WIPState {
	active := c.activeCountLocked()
	overrides := append([]WIPOverride(nil), c.overrides...)
	sort.SliceStable(overrides, func(i, j int) bool { return overrides[i].TaskID < overrides[j].TaskID })
	return WIPState{Policy: cloneValue(c.policy), ActiveCount: active, Remaining: c.policy.Limit - active, Overrides: overrides}
}
