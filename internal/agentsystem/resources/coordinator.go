package resources

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

var (
	ErrInvalid         = errors.New("agent resources: invalid configuration or request")
	ErrUnknownLane     = errors.New("agent resources: workload lane is not configured")
	ErrUnknownProvider = errors.New("agent resources: provider quota is not configured")
	ErrWrongCell       = errors.New("agent resources: request cell does not match this coordinator")
	ErrQueueFull       = errors.New("agent resources: bounded admission queue is full")
	ErrPressureShed    = errors.New("agent resources: background work is shed under pressure")
)

// Lane identifies an independently bounded class of agent work.
type Lane string

const (
	LaneInteractive Lane = "interactive"
	LaneAutonomous  Lane = "autonomous"
	LaneEvaluation  Lane = "evaluation"
	LaneMaintenance Lane = "maintenance"
	LaneRollout     Lane = "rollout"
)

// Policy is the trusted capacity envelope installed by the composition root.
// ProviderConcurrency is a per-provider in-flight quota; token and spend
// ceilings remain owned by agentbudget and are reserved before provider work.
type Policy struct {
	CellID                       string
	MaxConcurrent                int
	MaxConcurrentPerTenant       int
	MaxQueued                    int
	MaxQueuedPerTenant           int
	MaxConcurrentPerUser         int
	MaxConcurrentPerTask         int
	MaxQueuedPerUser             int
	MaxQueuedPerTask             int
	ReservedInteractive          int
	ReservedInteractivePerTenant int
	ReservedInteractivePerUser   int
	LaneConcurrency              map[Lane]int
	ProviderConcurrency          map[string]int
}

// Request carries only the route and resource class resolved by trusted
// server-side code. Tenant and cell values are never inferred from task text.
type Request struct {
	TenantID string
	UserID   string
	TaskID   string
	CellID   string
	Lane     Lane
	Provider string
}

// TenantSnapshot exposes resource counts for exactly one tenant.
type TenantSnapshot struct {
	Active     int
	Queued     int
	ByLane     map[Lane]int
	ByProvider map[string]int
}

type fairKey struct {
	tenant string
}

type laneState struct {
	active int
	queues map[fairKey][]*waiter
	order  []fairKey
	cursor int
}

type waiter struct {
	request Request
	key     fairKey
	ready   chan struct{}
	state   waiterState
}

type waiterState uint8

const (
	waiterQueued waiterState = iota
	waiterGranted
	waiterReleased
	waiterCanceled
)

// Coordinator applies global, lane, tenant, cell, and provider concurrency
// limits with round-robin admission among tenants within each workload lane.
type Coordinator struct {
	mu                   sync.Mutex
	policy               Policy
	lanes                map[Lane]*laneState
	laneOrder            []Lane
	laneCursor           int
	globalActive         int
	tenantActive         map[string]int
	providerActive       map[string]int
	queued               int
	queuedTenant         map[string]int
	activeTenant         map[string]int
	activeLaneTenant     map[tenantLane]int
	activeProviderTenant map[tenantProvider]int
	activeUsers          map[tenantIdentity]int
	interactiveUsers     map[tenantIdentity]int
	activeTasks          map[tenantIdentity]int
	queuedUsers          map[tenantIdentity]int
	queuedTasks          map[tenantIdentity]int
	pressure             Pressure
}

type tenantIdentity struct{ tenant, id string }

// Pressure is the trusted operations owner's resource pressure decision.
type Pressure string

const (
	PressureNormal Pressure = "normal"
	PressureSlow   Pressure = "slow"
	PressureShed   Pressure = "shed"
)

// ApplyPressure bounds subsequent admission while retaining existing leases.
// Shed refuses new background work; Slow halves total capacity, floored at one.
func (c *Coordinator) ApplyPressure(pressure Pressure) error {
	if c == nil || (pressure != PressureNormal && pressure != PressureSlow && pressure != PressureShed) {
		return ErrInvalid
	}
	c.mu.Lock()
	c.pressure = pressure
	c.dispatchLocked()
	c.mu.Unlock()
	return nil
}

type tenantLane struct {
	tenant string
	lane   Lane
}

type tenantProvider struct {
	tenant   string
	provider string
}

// New constructs a coordinator from an explicit trusted capacity policy.
func New(policy Policy) (*Coordinator, error) {
	if policy.CellID == "" || policy.MaxConcurrent < 1 || policy.MaxConcurrentPerTenant < 1 ||
		policy.MaxConcurrentPerTenant > policy.MaxConcurrent || policy.MaxQueued < 1 ||
		policy.MaxQueuedPerTenant < 1 || policy.MaxQueuedPerTenant > policy.MaxQueued ||
		len(policy.LaneConcurrency) == 0 {
		return nil, fmt.Errorf("%w: positive and ordered capacity limits are required", ErrInvalid)
	}
	if policy.MaxConcurrentPerUser < 0 || policy.MaxConcurrentPerTask < 0 || policy.MaxQueuedPerUser < 0 || policy.MaxQueuedPerTask < 0 ||
		policy.ReservedInteractive < 0 || policy.ReservedInteractive >= policy.MaxConcurrent || (policy.ReservedInteractive > 0 && policy.LaneConcurrency[LaneInteractive] < policy.ReservedInteractive) ||
		policy.ReservedInteractivePerTenant < 0 || policy.ReservedInteractivePerTenant >= policy.MaxConcurrentPerTenant || policy.ReservedInteractivePerUser < 0 ||
		(policy.ReservedInteractivePerUser > 0 && policy.ReservedInteractivePerUser >= policy.MaxConcurrentPerUser) {
		return nil, fmt.Errorf("%w: hierarchy and interactive reserve are invalid", ErrInvalid)
	}
	copyPolicy := policy
	copyPolicy.LaneConcurrency = make(map[Lane]int, len(policy.LaneConcurrency))
	for lane, limit := range policy.LaneConcurrency {
		if lane == "" || limit < 1 {
			return nil, fmt.Errorf("%w: lane limits must be positive", ErrInvalid)
		}
		copyPolicy.LaneConcurrency[lane] = limit
	}
	copyPolicy.ProviderConcurrency = make(map[string]int, len(policy.ProviderConcurrency))
	for provider, limit := range policy.ProviderConcurrency {
		if provider == "" || limit < 1 {
			return nil, fmt.Errorf("%w: provider limits must be positive", ErrInvalid)
		}
		copyPolicy.ProviderConcurrency[provider] = limit
	}
	lanes := make(map[Lane]*laneState, len(copyPolicy.LaneConcurrency))
	order := make([]Lane, 0, len(copyPolicy.LaneConcurrency))
	for lane := range copyPolicy.LaneConcurrency {
		lanes[lane] = &laneState{queues: make(map[fairKey][]*waiter)}
		order = append(order, lane)
	}
	// Deterministic order prevents map iteration from deciding which lane is
	// first after an otherwise identical restart or test run.
	sortLanes(order)
	return &Coordinator{
		policy: copyPolicy, lanes: lanes, laneOrder: order,
		tenantActive: make(map[string]int), providerActive: make(map[string]int),
		queuedTenant: make(map[string]int), activeTenant: make(map[string]int),
		activeLaneTenant: make(map[tenantLane]int), activeProviderTenant: make(map[tenantProvider]int),
		activeUsers: make(map[tenantIdentity]int), activeTasks: make(map[tenantIdentity]int),
		interactiveUsers: make(map[tenantIdentity]int),
		queuedUsers:      make(map[tenantIdentity]int), queuedTasks: make(map[tenantIdentity]int), pressure: PressureNormal,
	}, nil
}

// Acquire waits for fair bounded admission and returns a lease that releases
// all concurrency counters when closed. Cancellation removes queued work and
// returns a concurrently granted slot before reporting the context error.
func (c *Coordinator) Acquire(ctx context.Context, request Request) (*Lease, error) {
	if c == nil || ctx == nil || request.TenantID == "" || request.CellID == "" || request.Lane == "" {
		return nil, ErrInvalid
	}
	if ((c.policy.MaxConcurrentPerUser > 0 || c.policy.MaxQueuedPerUser > 0) && request.UserID == "") ||
		((c.policy.MaxConcurrentPerTask > 0 || c.policy.MaxQueuedPerTask > 0) && request.TaskID == "") {
		return nil, ErrInvalid
	}
	if request.CellID != c.policy.CellID {
		return nil, ErrWrongCell
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, ok := c.lanes[request.Lane]; !ok {
		return nil, ErrUnknownLane
	}
	if request.Provider != "" {
		if _, ok := c.policy.ProviderConcurrency[request.Provider]; !ok {
			return nil, ErrUnknownProvider
		}
	}
	w := &waiter{request: request, key: fairKey{tenant: request.TenantID}, ready: make(chan struct{}), state: waiterQueued}
	c.mu.Lock()
	if c.pressure == PressureShed && request.Lane != LaneInteractive {
		c.mu.Unlock()
		return nil, ErrPressureShed
	}
	if c.queued >= c.policy.MaxQueued || c.queuedTenant[request.TenantID] >= c.policy.MaxQueuedPerTenant ||
		atLimit(c.queuedUsers, tenantIdentity{request.TenantID, request.UserID}, c.policy.MaxQueuedPerUser) ||
		atLimit(c.queuedTasks, tenantIdentity{request.TenantID, request.TaskID}, c.policy.MaxQueuedPerTask) {
		c.mu.Unlock()
		return nil, ErrQueueFull
	}
	c.enqueueLocked(w)
	c.dispatchLocked()
	c.mu.Unlock()
	select {
	case <-w.ready:
		if err := ctx.Err(); err != nil {
			c.cancelLocked(w)
			return nil, err
		}
		return &Lease{coordinator: c, waiter: w}, nil
	case <-ctx.Done():
		c.cancelLocked(w)
		return nil, ctx.Err()
	}
}

// SnapshotForTenant reports active and queued work for one tenant only.
func (c *Coordinator) SnapshotForTenant(tenant string) TenantSnapshot {
	snapshot := TenantSnapshot{ByLane: make(map[Lane]int), ByProvider: make(map[string]int)}
	if c == nil || tenant == "" {
		return snapshot
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	snapshot.Active = c.activeTenant[tenant]
	snapshot.Queued = c.queuedTenant[tenant]
	for key, count := range c.activeLaneTenant {
		if key.tenant == tenant {
			snapshot.ByLane[key.lane] = count
		}
	}
	for key, count := range c.activeProviderTenant {
		if key.tenant == tenant {
			snapshot.ByProvider[key.provider] = count
		}
	}
	return snapshot
}

// Lease is a concurrency reservation held while one unit of work is active.
type Lease struct {
	coordinator *Coordinator
	waiter      *waiter
}

// Release returns the lease's global, lane, tenant and provider capacity.
// Repeated calls are safe and have no further effect.
func (l *Lease) Release() {
	if l == nil || l.coordinator == nil || l.waiter == nil {
		return
	}
	c := l.coordinator
	c.mu.Lock()
	if l.waiter.state == waiterGranted {
		c.releaseLocked(l.waiter)
		c.dispatchLocked()
	}
	c.mu.Unlock()
}

func (c *Coordinator) enqueueLocked(w *waiter) {
	state := c.lanes[w.request.Lane]
	if len(state.queues[w.key]) == 0 {
		state.order = append(state.order, w.key)
	}
	state.queues[w.key] = append(state.queues[w.key], w)
	c.queued++
	c.queuedTenant[w.request.TenantID]++
	if w.request.UserID != "" {
		c.queuedUsers[tenantIdentity{w.request.TenantID, w.request.UserID}]++
	}
	if w.request.TaskID != "" {
		c.queuedTasks[tenantIdentity{w.request.TenantID, w.request.TaskID}]++
	}
}

func (c *Coordinator) dispatchLocked() {
	capacity := c.policy.MaxConcurrent
	if c.pressure == PressureSlow {
		capacity = max(1, capacity/2)
	}
	for c.globalActive < capacity {
		granted := false
		for checked := 0; checked < len(c.laneOrder); checked++ {
			index := (c.laneCursor + checked) % len(c.laneOrder)
			lane := c.laneOrder[index]
			state := c.lanes[lane]
			if lane != LaneInteractive && c.globalActive-c.interactiveActiveLocked() >= max(0, capacity-c.policy.ReservedInteractive) {
				continue
			}
			if state.active >= c.policy.LaneConcurrency[lane] {
				continue
			}
			w, keyIndex := c.nextEligibleLocked(lane, state)
			if w == nil {
				continue
			}
			c.grantLocked(w)
			c.removeQueueHeadLocked(lane, state, w, keyIndex)
			c.laneCursor = (index + 1) % len(c.laneOrder)
			close(w.ready)
			granted = true
			break
		}
		if !granted {
			return
		}
	}
}

func (c *Coordinator) nextEligibleLocked(lane Lane, state *laneState) (*waiter, int) {
	for checked := 0; checked < len(state.order); checked++ {
		index := (state.cursor + checked) % len(state.order)
		key := state.order[index]
		queue := state.queues[key]
		if len(queue) == 0 {
			continue
		}
		for _, w := range queue {
			if lane != LaneInteractive {
				if c.activeTenant[w.request.TenantID]-c.activeLaneTenant[tenantLane{w.request.TenantID, LaneInteractive}] >= c.policy.MaxConcurrentPerTenant-c.policy.ReservedInteractivePerTenant {
					continue
				}
				userKey := tenantIdentity{w.request.TenantID, w.request.UserID}
				if c.policy.MaxConcurrentPerUser > 0 && c.activeUsers[userKey]-c.interactiveUsers[userKey] >= c.policy.MaxConcurrentPerUser-c.policy.ReservedInteractivePerUser {
					continue
				}
			}
			if c.activeTenant[w.request.TenantID] >= c.policy.MaxConcurrentPerTenant {
				continue
			}
			if atLimit(c.activeUsers, tenantIdentity{w.request.TenantID, w.request.UserID}, c.policy.MaxConcurrentPerUser) ||
				atLimit(c.activeTasks, tenantIdentity{w.request.TenantID, w.request.TaskID}, c.policy.MaxConcurrentPerTask) {
				continue
			}
			if w.request.Provider != "" && c.providerActive[w.request.Provider] >= c.policy.ProviderConcurrency[w.request.Provider] {
				continue
			}
			return w, index
		}
	}
	return nil, 0
}

func (c *Coordinator) grantLocked(w *waiter) {
	w.state = waiterGranted
	c.globalActive++
	c.lanes[w.request.Lane].active++
	c.activeTenant[w.request.TenantID]++
	c.activeLaneTenant[tenantLane{tenant: w.request.TenantID, lane: w.request.Lane}]++
	if w.request.UserID != "" {
		c.activeUsers[tenantIdentity{w.request.TenantID, w.request.UserID}]++
		if w.request.Lane == LaneInteractive {
			c.interactiveUsers[tenantIdentity{w.request.TenantID, w.request.UserID}]++
		}
	}
	if w.request.TaskID != "" {
		c.activeTasks[tenantIdentity{w.request.TenantID, w.request.TaskID}]++
	}
	if w.request.Provider != "" {
		c.providerActive[w.request.Provider]++
		c.activeProviderTenant[tenantProvider{tenant: w.request.TenantID, provider: w.request.Provider}]++
	}
}

func (c *Coordinator) releaseLocked(w *waiter) {
	w.state = waiterReleased
	c.globalActive--
	c.lanes[w.request.Lane].active--
	decrement(c.activeTenant, w.request.TenantID)
	decrement(c.activeLaneTenant, tenantLane{tenant: w.request.TenantID, lane: w.request.Lane})
	decrement(c.activeUsers, tenantIdentity{w.request.TenantID, w.request.UserID})
	if w.request.Lane == LaneInteractive {
		decrement(c.interactiveUsers, tenantIdentity{w.request.TenantID, w.request.UserID})
	}
	decrement(c.activeTasks, tenantIdentity{w.request.TenantID, w.request.TaskID})
	if w.request.Provider != "" {
		decrement(c.providerActive, w.request.Provider)
		decrement(c.activeProviderTenant, tenantProvider{tenant: w.request.TenantID, provider: w.request.Provider})
	}
}

func (c *Coordinator) cancelLocked(w *waiter) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch w.state {
	case waiterQueued:
		c.removeQueuedLocked(w)
		w.state = waiterCanceled
	case waiterGranted:
		c.releaseLocked(w)
	}
	c.dispatchLocked()
}

func (c *Coordinator) removeQueuedLocked(w *waiter) {
	state := c.lanes[w.request.Lane]
	queue := state.queues[w.key]
	for i, candidate := range queue {
		if candidate == w {
			queue = append(queue[:i], queue[i+1:]...)
			break
		}
	}
	if len(queue) == 0 {
		delete(state.queues, w.key)
		state.order = removeKey(state.order, w.key)
		if len(state.order) == 0 {
			state.cursor = 0
		} else {
			state.cursor %= len(state.order)
		}
	} else {
		state.queues[w.key] = queue
	}
	c.queued--
	decrement(c.queuedTenant, w.request.TenantID)
	decrement(c.queuedUsers, tenantIdentity{w.request.TenantID, w.request.UserID})
	decrement(c.queuedTasks, tenantIdentity{w.request.TenantID, w.request.TaskID})
}

func (c *Coordinator) removeQueueHeadLocked(lane Lane, state *laneState, w *waiter, index int) {
	queue := state.queues[w.key]
	for i, candidate := range queue {
		if candidate == w {
			queue = append(queue[:i], queue[i+1:]...)
			break
		}
	}
	if len(queue) == 0 {
		delete(state.queues, w.key)
		state.order = removeKey(state.order, w.key)
		if len(state.order) == 0 {
			state.cursor = 0
		} else {
			state.cursor = index % len(state.order)
		}
	} else {
		state.queues[w.key] = queue
		state.cursor = (index + 1) % len(state.order)
	}
	c.queued--
	decrement(c.queuedTenant, w.request.TenantID)
	decrement(c.queuedUsers, tenantIdentity{w.request.TenantID, w.request.UserID})
	decrement(c.queuedTasks, tenantIdentity{w.request.TenantID, w.request.TaskID})
}

func atLimit(counts map[tenantIdentity]int, key tenantIdentity, limit int) bool {
	return key.id != "" && limit > 0 && counts[key] >= limit
}

func (c *Coordinator) interactiveActiveLocked() int {
	if lane := c.lanes[LaneInteractive]; lane != nil {
		return lane.active
	}
	return 0
}

func decrement[K comparable](counts map[K]int, key K) {
	if counts[key] <= 1 {
		delete(counts, key)
		return
	}
	counts[key]--
}

func removeKey(keys []fairKey, target fairKey) []fairKey {
	for i, key := range keys {
		if key == target {
			return append(keys[:i], keys[i+1:]...)
		}
	}
	return keys
}

func sortLanes(lanes []Lane) {
	for i := 1; i < len(lanes); i++ {
		for j := i; j > 0 && lanes[j] < lanes[j-1]; j-- {
			lanes[j], lanes[j-1] = lanes[j-1], lanes[j]
		}
	}
}
