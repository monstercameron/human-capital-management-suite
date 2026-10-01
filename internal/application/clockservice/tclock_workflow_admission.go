package clockservice

import (
	"context"
	"errors"
	"strings"
	"sync"
)

// ClockWorkflowPriority keeps live clocking ahead of lower-priority offline
// replay without creating a second execution path. Both classes still call
// the same WorkflowPunchExecutor and therefore have the same durable proof.
type ClockWorkflowPriority uint8

const (
	ClockWorkflowPriorityLive ClockWorkflowPriority = iota + 1
	ClockWorkflowPriorityOfflineReplay
)

// ClockWorkflowBudget is a per-tenant admission budget. A zero limit means
// unlimited for that dimension. ReplayReserve reserves capacity for live
// punches when replay traffic fills the ordinary tenant budget.
type ClockWorkflowBudget struct {
	MaxInFlightPerTenant int
	MaxReplayInFlight    int
	ReplayReserve        int
}

type clockWorkflowTenantLoad struct {
	total, replay int
}

// ClockWorkflowAdmission is a small in-process admission gate intended to be
// owned by one runtime cell. It never stores punches: a refused admission is
// ErrRetryLater, so the device retains the idempotent punch and retries it.
type ClockWorkflowAdmission struct {
	mu     sync.Mutex
	budget ClockWorkflowBudget
	load   map[string]clockWorkflowTenantLoad
}

func NewClockWorkflowAdmission(budget ClockWorkflowBudget) (*ClockWorkflowAdmission, error) {
	if budget.MaxInFlightPerTenant < 0 || budget.MaxReplayInFlight < 0 || budget.ReplayReserve < 0 {
		return nil, errors.New("clock workflow admission: limits cannot be negative")
	}
	if budget.MaxInFlightPerTenant > 0 && budget.ReplayReserve >= budget.MaxInFlightPerTenant {
		return nil, errors.New("clock workflow admission: replay reserve must leave live capacity")
	}
	return &ClockWorkflowAdmission{budget: budget, load: make(map[string]clockWorkflowTenantLoad)}, nil
}

// Admit reserves capacity and returns an idempotent release function. Context
// cancellation is checked before the reservation; the runtime itself remains
// responsible for its durable transaction and retries.
func (a *ClockWorkflowAdmission) Admit(ctx context.Context, tenant string, priority ClockWorkflowPriority) (func(), error) {
	if a == nil || strings.TrimSpace(tenant) == "" {
		return nil, ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if priority != ClockWorkflowPriorityLive && priority != ClockWorkflowPriorityOfflineReplay {
		return nil, errors.New("clock workflow admission: unknown priority")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	key := strings.TrimSpace(tenant)
	current := a.load[key]
	if a.budget.MaxInFlightPerTenant > 0 {
		limit := a.budget.MaxInFlightPerTenant
		if priority == ClockWorkflowPriorityOfflineReplay {
			limit -= a.budget.ReplayReserve
			if limit < 1 {
				limit = 1
			}
		}
		if current.total >= limit {
			return nil, ErrRetryLater
		}
	}
	if priority == ClockWorkflowPriorityOfflineReplay && a.budget.MaxReplayInFlight > 0 && current.replay >= a.budget.MaxReplayInFlight {
		return nil, ErrRetryLater
	}
	current.total++
	if priority == ClockWorkflowPriorityOfflineReplay {
		current.replay++
	}
	a.load[key] = current
	var once sync.Once
	return func() {
		once.Do(func() {
			a.mu.Lock()
			current := a.load[key]
			if current.total > 0 {
				current.total--
			}
			if priority == ClockWorkflowPriorityOfflineReplay && current.replay > 0 {
				current.replay--
			}
			if current.total == 0 {
				delete(a.load, key)
			} else {
				a.load[key] = current
			}
			a.mu.Unlock()
		})
	}, nil
}

func clockWorkflowPriority(work PunchWork) ClockWorkflowPriority {
	source := strings.ToUpper(strings.TrimSpace(work.Observation.Source))
	if strings.Contains(source, "IMPORT") || strings.Contains(source, "OFFLINE") || strings.Contains(source, "REPLAY") {
		return ClockWorkflowPriorityOfflineReplay
	}
	return ClockWorkflowPriorityLive
}
