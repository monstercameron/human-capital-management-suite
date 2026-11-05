// Package asset maintenance scheduling: SMB-ASSET-004 plans, due evaluation,
// service evidence and verified release on top of the canonical custody
// owner. Serviceability is a parallel projection: it never edits custody
// revisions, and standalone equipment needs no project identity.
//
// The generic schedule engine owns trigger publishing; this package evaluates
// due obligations and mints the per-cycle service work the engine targets.
// Engine publication (TriggerDefinition allowlists and dispatch) stays with
// the scheduler composition root and is not reimplemented here.
package asset

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	ErrServiceValidation = errors.New("asset: service request is invalid")
	ErrServiceState      = errors.New("asset: no maintenance state for asset")
	ErrServiceBlocked    = errors.New("asset: equipment is blocked for use")
	ErrServiceRelease    = errors.New("asset: release criteria are not met")
	ErrMeterRegression   = errors.New("asset: meter reading regressed within its epoch")
	ErrBookingConflict   = errors.New("asset: booking conflicts with service state")
)

// Service origins. Ticks from the schedule engine arrive as app origin;
// offline device completions stay unverified until the server confirms them.
const (
	ServiceOriginHuman  = "human"
	ServiceOriginApp    = "app"
	ServiceOriginDevice = "device"
)

// ServiceActor authenticates one service write.
type ServiceActor struct {
	Actor  values.EntityRef
	Origin string
}

func (a ServiceActor) validate(asset values.EntityRef) error {
	if err := a.Actor.Validate(); err != nil {
		return fmt.Errorf("%w: actor: %v", ErrServiceValidation, err)
	}
	switch a.Origin {
	case ServiceOriginHuman, ServiceOriginApp, ServiceOriginDevice:
	default:
		return fmt.Errorf("%w: origin %q is not declared", ErrServiceValidation, a.Origin)
	}
	if a.Actor.Tenant != asset.Tenant {
		return ErrTenantBoundary
	}
	return nil
}

// MaintenanceBlockMode distinguishes advisory warnings from hard blocks.
type MaintenanceBlockMode string

const (
	BlockWarning MaintenanceBlockMode = "WARNING"
	BlockHard    MaintenanceBlockMode = "HARD"
)

// StockLinkState tracks optional inventory linkage. Linking never blocks
// unrelated lawful asset operations.
type StockLinkState string

const (
	StockLinkNone    StockLinkState = "NONE"
	StockLinkPending StockLinkState = "PENDING"
	StockLinkLinked  StockLinkState = "LINKED"
)

// MaintenancePlanInput creates or revises a plan. At least one trigger
// input must be enabled; the two inputs are OR, never AND.
type MaintenancePlanInput struct {
	Asset            values.EntityRef
	CalendarEnabled  bool
	IntervalDays     int
	MeterEnabled     bool
	MeterThreshold   int64
	Baseline         time.Time
	TemplateRevision string
	BlockMode        MaintenanceBlockMode
	BlockWhenOverdue bool
	ProjectRef       string
	CatalogItemRef   string
	StockLocationRef string
}

// MaintenancePlan is one immutable plan version. Revisions reschedule the
// future; past service evidence is untouched.
type MaintenancePlan struct {
	Asset            values.EntityRef
	PlanVersion      int
	CalendarEnabled  bool
	IntervalDays     int
	MeterEnabled     bool
	MeterThreshold   int64
	Baseline         time.Time
	LastService      time.Time
	NextDue          time.Time
	TemplateRevision string
	BlockMode        MaintenanceBlockMode
	BlockWhenOverdue bool
	ProjectRef       string
	CatalogItemRef   string
	StockLocationRef string
	StockLinkState   StockLinkState
	UpdatedAt        time.Time
}

// ServiceWorkTrigger names what opened the work. Calendar and meter due in
// the same cycle share one task.
type ServiceWorkTrigger string

const (
	DueCalendar ServiceWorkTrigger = "DUE_CALENDAR"
	DueMeter    ServiceWorkTrigger = "DUE_METER"
	Manual      ServiceWorkTrigger = "MANUAL"
)

// ServiceWorkStatus is the work lifecycle. RELEASE_FAILED is retryable.
type ServiceWorkStatus string

const (
	WorkOpen          ServiceWorkStatus = "OPEN"
	WorkCompleted     ServiceWorkStatus = "COMPLETED"
	WorkReleased      ServiceWorkStatus = "RELEASED"
	WorkReleaseFailed ServiceWorkStatus = "RELEASE_FAILED"
)

// ServiceWorkItem is one service task for an asset plan version and cycle.
// ProjectRef stays empty for standalone equipment: no dummy project ID.
type ServiceWorkItem struct {
	WorkID            string
	Asset             values.EntityRef
	PlanVersion       int
	Cycle             int
	Trigger           ServiceWorkTrigger
	ProjectRef        string
	Status            ServiceWorkStatus
	ResultExplanation string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// CompletionStatus distinguishes verified, offline-pending and incomplete
// service receipts. Only VERIFIED completions can release equipment.
type CompletionStatus string

const (
	CompletionVerified   CompletionStatus = "VERIFIED"
	CompletionUnverified CompletionStatus = "UNVERIFIED"
	CompletionIncomplete CompletionStatus = "INCOMPLETE"
)

// PartsLine is one requested service part; PartsReceipt is the exact
// inventory receipt. STOCK-004 owns issuing; this package records it.
type PartsLine struct {
	ItemSKU string
	Qty     string
	Unit    string
}

// PartsReceipt is the exact issue evidence.
type PartsReceipt struct {
	ReceiptID string
	Lines     []PartsLine
}

// PartsIssuer is the STOCK-004 seam for service parts.
type PartsIssuer interface {
	IssueServiceParts(ctx context.Context, asset values.EntityRef, workID string, lines []PartsLine) (PartsReceipt, error)
}

// ServiceCompletionInput records one service attempt.
type ServiceCompletionInput struct {
	WorkPerformed string
	Parts         []PartsLine
	Evidence      string
	Offline       bool
}

// ServiceCompletion is the immutable service receipt.
type ServiceCompletion struct {
	ReceiptID     string
	WorkID        string
	Asset         values.EntityRef
	Status        CompletionStatus
	WorkPerformed string
	Evidence      string
	PartsReceipt  *PartsReceipt
	PartsAction   string
	Offline       bool
	At            time.Time
}

// Service event kinds. Events are append-only facts.
const (
	ServiceEventPlanSet          = "PLAN_SET"
	ServiceEventInspection       = "INSPECTED"
	ServiceEventMeter            = "METER_RECORDED"
	ServiceEventMeterSwap        = "METER_REPLACED"
	ServiceEventWorkOpened       = "WORK_OPENED"
	ServiceEventCompletion       = "COMPLETION_RECORDED"
	ServiceEventConfirmed        = "COMPLETION_CONFIRMED"
	ServiceEventReleased         = "RELEASED"
	ServiceEventReleaseFailed    = "RELEASE_FAILED"
	ServiceEventBookingHeld      = "BOOKING_HELD"
	ServiceEventBookingConfirmed = "BOOKING_CONFIRMED"
	ServiceEventBookingFlagged   = "BOOKING_FLAGGED"
	ServiceEventLink             = "STOCK_LINK"
)

// ServiceEvent is one immutable maintenance fact.
type ServiceEvent struct {
	Seq     int
	At      time.Time
	Kind    string
	ActorID string
	Detail  string
}

// ServiceState is the equipment eligibility projection.
type ServiceState string

const (
	ServiceAvailable    ServiceState = "AVAILABLE"
	ServiceDueWarning   ServiceState = "DUE_WARNING"
	ServiceOutOfService ServiceState = "OUT_OF_SERVICE"
	ServiceInService    ServiceState = "IN_SERVICE"
)

// Serviceability carries eligibility plus the blocked revision bookings
// fence on. The revision moves on every block and unblock transition.
type Serviceability struct {
	State      ServiceState
	BlockedRev uint64
	Cycle      int
	NextDue    time.Time
}

// BookingStatus is the booking lifecycle. Flagged bookings keep their owner
// and history; nothing silently cancels or reallocates them.
type BookingStatus string

const (
	BookingHeld            BookingStatus = "HELD"
	BookingConfirmed       BookingStatus = "CONFIRMED"
	BookingNeedsResolution BookingStatus = "NEEDS_RESOLUTION"
	BookingCancelled       BookingStatus = "CANCELLED"
)

// ServiceBooking is one equipment hold with the eligibility revision it was
// confirmed under.
type ServiceBooking struct {
	BookingID      string
	Asset          values.EntityRef
	Owner          values.EntityRef
	Start          time.Time
	End            time.Time
	Status         BookingStatus
	EligibilityRev uint64
}

// MeterState tracks the current meter epoch. Readings are monotonic within
// an epoch; replacement opens a new epoch without erasing due obligations.
type MeterState struct {
	Epoch       int
	LastReading int64
}

// ServiceWorkSink receives created service tasks. JOB-001 owns the sink;
// a nil sink keeps tasks in the coordinator only.
type ServiceWorkSink interface {
	CreateServiceTask(ctx context.Context, item ServiceWorkItem) error
}

// assetServiceState is the per-asset maintenance record.
type assetServiceState struct {
	Plan       MaintenancePlan
	PlanPast   []MaintenancePlan
	Cycle      int
	MeterDue   bool
	OpenWorkID string
	State      ServiceState
	BlockedRev uint64
	Meter      MeterState
}

// MaintenanceCoordinator owns plans, due evaluation, service evidence,
// verified release and booking eligibility. CustodyStore and PartsIssuer are
// optional seams; nil keeps the coordinator standalone.
type MaintenanceCoordinator struct {
	mu          sync.Mutex
	custody     *CustodyStore
	parts       PartsIssuer
	sink        ServiceWorkSink
	assets      map[string]*assetServiceState
	works       map[string]*ServiceWorkItem
	completions map[string]*ServiceCompletion
	byReceipt   map[string]*ServiceCompletion
	bookings    map[string]*ServiceBooking
	events      map[string][]ServiceEvent
	partsKeys   map[string]PartsReceipt
	idem        map[string]serviceIdem
}

type serviceIdem struct {
	Fingerprint string
	WorkID      string
	ReceiptID   string
	PlanVersion int
}

// NewMaintenanceCoordinator builds an empty coordinator.
func NewMaintenanceCoordinator(custody *CustodyStore, parts PartsIssuer, sink ServiceWorkSink) *MaintenanceCoordinator {
	return &MaintenanceCoordinator{
		custody: custody, parts: parts, sink: sink,
		assets: make(map[string]*assetServiceState), works: make(map[string]*ServiceWorkItem),
		completions: make(map[string]*ServiceCompletion), byReceipt: make(map[string]*ServiceCompletion),
		bookings: make(map[string]*ServiceBooking), events: make(map[string][]ServiceEvent),
		partsKeys: make(map[string]PartsReceipt), idem: make(map[string]serviceIdem),
	}
}

func serviceKey(op string, tenant values.TenantId, key string) string {
	return op + "\x00" + string(tenant) + "\x00" + strings.TrimSpace(key)
}

func (c *MaintenanceCoordinator) checkIdem(op string, tenant values.TenantId, key, fingerprint string) (serviceIdem, bool, error) {
	if strings.TrimSpace(key) == "" {
		return serviceIdem{}, false, nil
	}
	prior, ok := c.idem[serviceKey(op, tenant, key)]
	if !ok {
		return serviceIdem{}, false, nil
	}
	if prior.Fingerprint != fingerprint {
		return serviceIdem{}, false, fmt.Errorf("%w: key %q", ErrOperationConflict, key)
	}
	return prior, true, nil
}

func (c *MaintenanceCoordinator) storeIdem(op string, tenant values.TenantId, key string, record serviceIdem) {
	if strings.TrimSpace(key) == "" {
		return
	}
	c.idem[serviceKey(op, tenant, key)] = record
}

func (c *MaintenanceCoordinator) appendEvent(asset values.EntityRef, kind, actorID, detail string) {
	key := asset.String()
	event := ServiceEvent{Seq: len(c.events[key]) + 1, At: time.Now().UTC(), Kind: kind, ActorID: actorID, Detail: detail}
	c.events[key] = append(c.events[key], event)
}

// SetMaintenancePlan creates or revises the asset plan. Revisions reschedule
// the future from the last verified service; past evidence is untouched.
func (c *MaintenanceCoordinator) SetMaintenancePlan(actor ServiceActor, input MaintenancePlanInput, key string) (MaintenancePlan, error) {
	if err := input.Asset.Validate(); err != nil {
		return MaintenancePlan{}, fmt.Errorf("%w: asset: %v", ErrServiceValidation, err)
	}
	if err := actor.validate(input.Asset); err != nil {
		return MaintenancePlan{}, err
	}
	if !input.CalendarEnabled && !input.MeterEnabled {
		return MaintenancePlan{}, fmt.Errorf("%w: enable a calendar or meter trigger", ErrServiceValidation)
	}
	if input.CalendarEnabled && input.IntervalDays <= 0 {
		return MaintenancePlan{}, fmt.Errorf("%w: calendar interval must be positive days", ErrServiceValidation)
	}
	if input.MeterEnabled && input.MeterThreshold <= 0 {
		return MaintenancePlan{}, fmt.Errorf("%w: meter threshold must be positive", ErrServiceValidation)
	}
	if input.Baseline.IsZero() || strings.TrimSpace(input.TemplateRevision) == "" {
		return MaintenancePlan{}, fmt.Errorf("%w: baseline and template revision are required", ErrServiceValidation)
	}
	if input.BlockMode != BlockWarning && input.BlockMode != BlockHard {
		return MaintenancePlan{}, fmt.Errorf("%w: block mode must be WARNING or HARD", ErrServiceValidation)
	}
	fp := fmt.Sprintf("plan\x00%#v", input)
	c.mu.Lock()
	defer c.mu.Unlock()
	if prior, ok, err := c.checkIdem("plan", input.Asset.Tenant, key, fp); err != nil || ok {
		if err != nil {
			return MaintenancePlan{}, err
		}
		state := c.assets[input.Asset.String()]
		for _, version := range append([]MaintenancePlan{state.Plan}, state.PlanPast...) {
			if version.PlanVersion == prior.PlanVersion {
				return version, nil
			}
		}
		return state.Plan, nil
	}
	now := time.Now().UTC()
	state, exists := c.assets[input.Asset.String()]
	if !exists {
		state = &assetServiceState{State: ServiceAvailable}
		c.assets[input.Asset.String()] = state
	} else {
		state.PlanPast = append(state.PlanPast, state.Plan)
	}
	anchor := input.Baseline
	if !state.Plan.LastService.IsZero() {
		anchor = state.Plan.LastService
	}
	plan := MaintenancePlan{
		Asset: input.Asset, PlanVersion: state.Plan.PlanVersion + 1,
		CalendarEnabled: input.CalendarEnabled, IntervalDays: input.IntervalDays,
		MeterEnabled: input.MeterEnabled, MeterThreshold: input.MeterThreshold,
		Baseline: input.Baseline, LastService: state.Plan.LastService,
		TemplateRevision: strings.TrimSpace(input.TemplateRevision),
		BlockMode:        input.BlockMode, BlockWhenOverdue: input.BlockWhenOverdue,
		ProjectRef:     strings.TrimSpace(input.ProjectRef),
		CatalogItemRef: strings.TrimSpace(input.CatalogItemRef), StockLocationRef: strings.TrimSpace(input.StockLocationRef),
		StockLinkState: state.Plan.StockLinkState, UpdatedAt: now,
	}
	if plan.StockLinkState == "" {
		plan.StockLinkState = StockLinkNone
	}
	if plan.CalendarEnabled {
		plan.NextDue = anchor.AddDate(0, 0, plan.IntervalDays)
	}
	if plan.PlanVersion == 0 {
		plan.PlanVersion = 1
	}
	state.Plan = plan
	c.appendEvent(input.Asset, ServiceEventPlanSet, actor.Actor.String(), fmt.Sprintf("version %d next due %v", plan.PlanVersion, plan.NextDue.UTC().Format(time.RFC3339)))
	c.storeIdem("plan", input.Asset.Tenant, key, serviceIdem{Fingerprint: fp, PlanVersion: plan.PlanVersion})
	return plan, nil
}

// Plan returns the current plan version.
func (c *MaintenanceCoordinator) Plan(asset values.EntityRef) (MaintenancePlan, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	state, ok := c.assets[asset.String()]
	if !ok {
		return MaintenancePlan{}, fmt.Errorf("%w: %s", ErrServiceState, asset.String())
	}
	return state.Plan, nil
}

// RecordInspection logs condition evidence. Inspections are observations;
// they never release equipment or reset due obligations.
func (c *MaintenanceCoordinator) RecordInspection(actor ServiceActor, asset values.EntityRef, notes, evidence string, at time.Time, key string) (ServiceEvent, error) {
	if err := actor.validate(asset); err != nil {
		return ServiceEvent{}, err
	}
	if strings.TrimSpace(notes) == "" || strings.TrimSpace(evidence) == "" || at.IsZero() {
		return ServiceEvent{}, fmt.Errorf("%w: inspection needs notes, evidence and time", ErrServiceValidation)
	}
	fp := fmt.Sprintf("inspect\x00%s\x00%s\x00%s\x00%d", asset.String(), notes, evidence, at.UnixNano())
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok, err := c.checkIdem("inspect", asset.Tenant, key, fp); err != nil || ok {
		if err != nil {
			return ServiceEvent{}, err
		}
		events := c.events[asset.String()]
		return events[len(events)-1], nil
	}
	if _, ok := c.assets[asset.String()]; !ok {
		return ServiceEvent{}, fmt.Errorf("%w: %s", ErrServiceState, asset.String())
	}
	c.appendEvent(asset, ServiceEventInspection, actor.Actor.String(), strings.TrimSpace(notes))
	c.storeIdem("inspect", asset.Tenant, key, serviceIdem{Fingerprint: fp})
	events := c.events[asset.String()]
	return events[len(events)-1], nil
}

// RecordMeterReading records a monotonic reading. Reaching the approved
// threshold marks the cycle due; the reading itself never opens duplicate
// work or releases anything.
func (c *MaintenanceCoordinator) RecordMeterReading(actor ServiceActor, asset values.EntityRef, reading int64, at time.Time, key string) (MeterState, error) {
	if err := actor.validate(asset); err != nil {
		return MeterState{}, err
	}
	if reading < 0 || at.IsZero() {
		return MeterState{}, fmt.Errorf("%w: meter reading must be non-negative with a time", ErrServiceValidation)
	}
	fp := fmt.Sprintf("meter\x00%s\x00%d\x00%d", asset.String(), reading, at.UnixNano())
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok, err := c.checkIdem("meter", asset.Tenant, key, fp); err != nil || ok {
		if err != nil {
			return MeterState{}, err
		}
		return c.assets[asset.String()].Meter, nil
	}
	state, ok := c.assets[asset.String()]
	if !ok {
		return MeterState{}, fmt.Errorf("%w: %s", ErrServiceState, asset.String())
	}
	if reading < state.Meter.LastReading {
		return MeterState{}, fmt.Errorf("%w: %d below %d in epoch %d", ErrMeterRegression, reading, state.Meter.LastReading, state.Meter.Epoch)
	}
	state.Meter.LastReading = reading
	detail := fmt.Sprintf("epoch %d reading %d", state.Meter.Epoch, reading)
	if state.Plan.MeterEnabled && reading >= state.Plan.MeterThreshold {
		state.MeterDue = true
		detail += " threshold reached"
	}
	c.appendEvent(asset, ServiceEventMeter, actor.Actor.String(), detail)
	c.storeIdem("meter", asset.Tenant, key, serviceIdem{Fingerprint: fp})
	return state.Meter, nil
}

// RecordMeterReplacement opens a new meter epoch after an authorized swap.
// The outstanding due obligation survives: resets never erase due work.
func (c *MaintenanceCoordinator) RecordMeterReplacement(actor ServiceActor, asset values.EntityRef, oldBaseline, newBaseline int64, evidence, reviewer string, at time.Time, key string) (MeterState, error) {
	if err := actor.validate(asset); err != nil {
		return MeterState{}, err
	}
	if strings.TrimSpace(evidence) == "" || strings.TrimSpace(reviewer) == "" || at.IsZero() {
		return MeterState{}, fmt.Errorf("%w: meter replacement needs evidence, reviewer and time", ErrServiceValidation)
	}
	if newBaseline < 0 {
		return MeterState{}, fmt.Errorf("%w: new baseline must be non-negative", ErrServiceValidation)
	}
	fp := fmt.Sprintf("meter-swap\x00%s\x00%d\x00%d\x00%s\x00%s", asset.String(), oldBaseline, newBaseline, evidence, reviewer)
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok, err := c.checkIdem("meter-swap", asset.Tenant, key, fp); err != nil || ok {
		if err != nil {
			return MeterState{}, err
		}
		return c.assets[asset.String()].Meter, nil
	}
	state, ok := c.assets[asset.String()]
	if !ok {
		return MeterState{}, fmt.Errorf("%w: %s", ErrServiceState, asset.String())
	}
	if oldBaseline != state.Meter.LastReading {
		return MeterState{}, fmt.Errorf("%w: old baseline %d does not match recorded %d", ErrServiceValidation, oldBaseline, state.Meter.LastReading)
	}
	state.Meter.Epoch++
	state.Meter.LastReading = newBaseline
	c.appendEvent(asset, ServiceEventMeterSwap, actor.Actor.String(), fmt.Sprintf("epoch %d baselines %d to %d", state.Meter.Epoch, oldBaseline, newBaseline))
	c.storeIdem("meter-swap", asset.Tenant, key, serviceIdem{Fingerprint: fp})
	return state.Meter, nil
}

// TickDue evaluates due obligations at now and ensures one task for the
// current cycle. Repeated ticks return the same task; calendar and meter
// due share it through the asset, plan version and service-cycle key.
func (c *MaintenanceCoordinator) TickDue(asset values.EntityRef, now time.Time) (ServiceWorkItem, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	state, ok := c.assets[asset.String()]
	if !ok {
		return ServiceWorkItem{}, false, fmt.Errorf("%w: %s", ErrServiceState, asset.String())
	}
	calendarDue := state.Plan.CalendarEnabled && !state.Plan.NextDue.IsZero() && !now.Before(state.Plan.NextDue)
	due := calendarDue || (state.Plan.MeterEnabled && state.MeterDue)
	if !due {
		return ServiceWorkItem{}, false, nil
	}
	if state.OpenWorkID != "" {
		if work, ok := c.works[state.OpenWorkID]; ok {
			return *work, false, nil
		}
	}
	trigger := DueCalendar
	if !calendarDue {
		trigger = DueMeter
	}
	work := &ServiceWorkItem{
		WorkID: uuid.NewString(), Asset: asset, PlanVersion: state.Plan.PlanVersion, Cycle: state.Cycle,
		Trigger: trigger, ProjectRef: state.Plan.ProjectRef, Status: WorkOpen,
		CreatedAt: now.UTC(), UpdatedAt: now.UTC(),
	}
	if c.sink != nil {
		if err := c.sink.CreateServiceTask(context.Background(), *work); err != nil {
			return ServiceWorkItem{}, false, fmt.Errorf("asset: service task sink: %w", err)
		}
	}
	c.works[work.WorkID] = work
	state.OpenWorkID = work.WorkID
	c.appendEvent(asset, ServiceEventWorkOpened, "system:maintenance-engine", fmt.Sprintf("%s cycle %d %s", work.WorkID, work.Cycle, work.Trigger))
	c.applyDueBlock(asset, state, string(work.Trigger))
	return *work, true, nil
}

// applyDueBlock moves overdue equipment to its policy state and fences
// bookings. Hard blocks bump the revision; warnings stay usable.
func (c *MaintenanceCoordinator) applyDueBlock(asset values.EntityRef, state *assetServiceState, trigger string) {
	_ = trigger
	if state.Plan.BlockWhenOverdue && state.Plan.BlockMode == BlockHard {
		if state.State != ServiceOutOfService {
			state.State = ServiceOutOfService
			state.BlockedRev++
			c.flagStaleBookings(asset, state)
		}
		return
	}
	if state.State == ServiceAvailable {
		state.State = ServiceDueWarning
	}
}

// OpenServiceWork ensures the current cycle task exists for hands-on work
// and holds future bookings. Manual opens share the cycle task; they never
// mint a second one.
func (c *MaintenanceCoordinator) OpenServiceWork(ctx context.Context, actor ServiceActor, asset values.EntityRef, reason, projectRef, key string) (ServiceWorkItem, error) {
	if err := actor.validate(asset); err != nil {
		return ServiceWorkItem{}, err
	}
	if strings.TrimSpace(reason) == "" {
		return ServiceWorkItem{}, fmt.Errorf("%w: opening service needs a reason", ErrServiceValidation)
	}
	fp := fmt.Sprintf("open\x00%s\x00%s\x00%s", asset.String(), reason, projectRef)
	c.mu.Lock()
	defer c.mu.Unlock()
	if prior, ok, err := c.checkIdem("open", asset.Tenant, key, fp); err != nil || ok {
		if err != nil {
			return ServiceWorkItem{}, err
		}
		if work, ok := c.works[prior.WorkID]; ok {
			return *work, nil
		}
	}
	state, ok := c.assets[asset.String()]
	if !ok {
		return ServiceWorkItem{}, fmt.Errorf("%w: %s", ErrServiceState, asset.String())
	}
	if state.OpenWorkID != "" {
		if work, ok := c.works[state.OpenWorkID]; ok {
			c.blockForService(asset, state)
			c.storeIdem("open", asset.Tenant, key, serviceIdem{Fingerprint: fp, WorkID: work.WorkID})
			return *work, nil
		}
	}
	ref := strings.TrimSpace(projectRef)
	if ref == "" {
		ref = state.Plan.ProjectRef
	}
	work := &ServiceWorkItem{
		WorkID: uuid.NewString(), Asset: asset, PlanVersion: state.Plan.PlanVersion, Cycle: state.Cycle,
		Trigger: Manual, ProjectRef: ref, Status: WorkOpen,
		ResultExplanation: strings.TrimSpace(reason), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if c.sink != nil {
		if err := c.sink.CreateServiceTask(ctx, *work); err != nil {
			return ServiceWorkItem{}, fmt.Errorf("asset: service task sink: %w", err)
		}
	}
	c.works[work.WorkID] = work
	state.OpenWorkID = work.WorkID
	c.appendEvent(asset, ServiceEventWorkOpened, actor.Actor.String(), fmt.Sprintf("%s cycle %d MANUAL: %s", work.WorkID, work.Cycle, strings.TrimSpace(reason)))
	c.blockForService(asset, state)
	c.storeIdem("open", asset.Tenant, key, serviceIdem{Fingerprint: fp, WorkID: work.WorkID})
	return *work, nil
}

// blockForService holds future bookings once hands-on work opens.
func (c *MaintenanceCoordinator) blockForService(asset values.EntityRef, state *assetServiceState) {
	if state.State == ServiceOutOfService {
		c.flagStaleBookings(asset, state)
		return
	}
	state.State = ServiceInService
	state.BlockedRev++
	c.flagStaleBookings(asset, state)
}

// flagStaleBookings moves confirmed bookings fenced on an older revision to
// needs-resolution with their owners intact.
func (c *MaintenanceCoordinator) flagStaleBookings(asset values.EntityRef, state *assetServiceState) {
	for _, booking := range c.bookings {
		if booking.Asset.String() != asset.String() || booking.Status != BookingConfirmed || booking.EligibilityRev == state.BlockedRev {
			continue
		}
		booking.Status = BookingNeedsResolution
		c.appendEvent(asset, ServiceEventBookingFlagged, "system:maintenance-engine", fmt.Sprintf("%s owner %s revision %d", booking.BookingID, booking.Owner.String(), booking.EligibilityRev))
	}
}

// RecordServiceCompletion records one service attempt. Parts issue once per
// task through the STOCK-004 seam; offline attempts wait for server
// confirmation and failed parts keep the receipt incomplete.
func (c *MaintenanceCoordinator) RecordServiceCompletion(ctx context.Context, actor ServiceActor, asset values.EntityRef, workID string, input ServiceCompletionInput, key string) (ServiceCompletion, error) {
	if err := actor.validate(asset); err != nil {
		return ServiceCompletion{}, err
	}
	if strings.TrimSpace(workID) == "" || strings.TrimSpace(input.WorkPerformed) == "" || strings.TrimSpace(input.Evidence) == "" {
		return ServiceCompletion{}, fmt.Errorf("%w: completion needs work, performed notes and evidence", ErrServiceValidation)
	}
	fp := fmt.Sprintf("complete\x00%s\x00%s\x00%#v\x00%v", asset.String(), workID, input.Parts, input.Offline)
	c.mu.Lock()
	defer c.mu.Unlock()
	if prior, ok, err := c.checkIdem("complete", asset.Tenant, key, fp); err != nil || ok {
		if err != nil {
			return ServiceCompletion{}, err
		}
		if completion, ok := c.byReceipt[prior.ReceiptID]; ok {
			return *completion, nil
		}
	}
	if _, ok := c.assets[asset.String()]; !ok {
		return ServiceCompletion{}, fmt.Errorf("%w: %s", ErrServiceState, asset.String())
	}
	work, ok := c.works[strings.TrimSpace(workID)]
	if !ok || work.Asset.String() != asset.String() {
		return ServiceCompletion{}, fmt.Errorf("%w: work %s", ErrServiceState, workID)
	}
	if work.Status == WorkCompleted || work.Status == WorkReleased {
		return ServiceCompletion{}, fmt.Errorf("%w: work %s is already complete", ErrServiceState, workID)
	}
	completion := &ServiceCompletion{
		ReceiptID: uuid.NewString(), WorkID: work.WorkID, Asset: asset,
		WorkPerformed: strings.TrimSpace(input.WorkPerformed), Evidence: strings.TrimSpace(input.Evidence),
		Offline: input.Offline, At: time.Now().UTC(),
	}
	if len(input.Parts) > 0 {
		if receipt, ok := c.partsKeys[work.WorkID]; ok {
			completion.PartsReceipt = &receipt
			completion.PartsAction = "ISSUED:" + receipt.ReceiptID
		} else if c.parts == nil {
			completion.Status = CompletionIncomplete
			completion.PartsAction = "FAILED: no parts issuer is wired"
		} else if receipt, err := c.parts.IssueServiceParts(ctx, asset, work.WorkID, input.Parts); err != nil {
			completion.Status = CompletionIncomplete
			completion.PartsAction = "FAILED: " + err.Error()
		} else {
			c.partsKeys[work.WorkID] = receipt
			stored := receipt
			completion.PartsReceipt = &stored
			completion.PartsAction = "ISSUED:" + receipt.ReceiptID
		}
	}
	switch {
	case completion.Status == CompletionIncomplete:
		work.ResultExplanation = "incomplete: " + completion.PartsAction
	case input.Offline:
		completion.Status = CompletionUnverified
		work.Status = WorkCompleted
		work.ResultExplanation = "offline completion awaiting server validation"
	default:
		completion.Status = CompletionVerified
		work.Status = WorkCompleted
		work.ResultExplanation = "verified service completion"
	}
	work.UpdatedAt = time.Now().UTC()
	c.completions[work.WorkID] = completion
	c.byReceipt[completion.ReceiptID] = completion
	c.appendEvent(asset, ServiceEventCompletion, actor.Actor.String(), fmt.Sprintf("%s %s %s", completion.ReceiptID, work.WorkID, completion.Status))
	c.storeIdem("complete", asset.Tenant, key, serviceIdem{Fingerprint: fp, WorkID: work.WorkID, ReceiptID: completion.ReceiptID})
	return *completion, nil
}

// ConfirmServiceCompletion server-validates one offline receipt.
func (c *MaintenanceCoordinator) ConfirmServiceCompletion(actor ServiceActor, asset values.EntityRef, receiptID, reviewer, evidence, key string) (ServiceCompletion, error) {
	if err := actor.validate(asset); err != nil {
		return ServiceCompletion{}, err
	}
	if strings.TrimSpace(reviewer) == "" || strings.TrimSpace(evidence) == "" {
		return ServiceCompletion{}, fmt.Errorf("%w: confirmation needs a reviewer and evidence", ErrServiceValidation)
	}
	fp := fmt.Sprintf("confirm\x00%s\x00%s\x00%s\x00%s", asset.String(), receiptID, reviewer, evidence)
	c.mu.Lock()
	defer c.mu.Unlock()
	if prior, ok, err := c.checkIdem("confirm", asset.Tenant, key, fp); err != nil || ok {
		if err != nil {
			return ServiceCompletion{}, err
		}
		if completion, ok := c.byReceipt[prior.ReceiptID]; ok {
			return *completion, nil
		}
	}
	completion, ok := c.byReceipt[strings.TrimSpace(receiptID)]
	if !ok || completion.Asset.String() != asset.String() || completion.Status != CompletionUnverified {
		return ServiceCompletion{}, fmt.Errorf("%w: receipt %s is not awaiting validation", ErrServiceState, receiptID)
	}
	completion.Status = CompletionVerified
	completion.Offline = false
	if work, ok := c.works[completion.WorkID]; ok {
		work.ResultExplanation = "server-validated service completion"
		work.UpdatedAt = time.Now().UTC()
	}
	c.appendEvent(asset, ServiceEventConfirmed, actor.Actor.String(), fmt.Sprintf("%s reviewed by %s", completion.ReceiptID, strings.TrimSpace(reviewer)))
	c.storeIdem("confirm", asset.Tenant, key, serviceIdem{Fingerprint: fp, WorkID: completion.WorkID, ReceiptID: completion.ReceiptID})
	return *completion, nil
}

// ReleaseToService returns equipment to available after verified service.
// Anything short of verified completion keeps the asset blocked with an
// explained, retryable release failure.
func (c *MaintenanceCoordinator) ReleaseToService(actor ServiceActor, asset values.EntityRef, reviewer, evidence string, at time.Time, key string) (Serviceability, error) {
	if err := actor.validate(asset); err != nil {
		return Serviceability{}, err
	}
	if strings.TrimSpace(reviewer) == "" || strings.TrimSpace(evidence) == "" || at.IsZero() {
		return Serviceability{}, fmt.Errorf("%w: release needs a reviewer, evidence and time", ErrServiceValidation)
	}
	fp := fmt.Sprintf("release\x00%s\x00%s\x00%s\x00%d", asset.String(), reviewer, evidence, at.UnixNano())
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok, err := c.checkIdem("release", asset.Tenant, key, fp); err != nil || ok {
		if err != nil {
			return Serviceability{}, err
		}
		state := c.assets[asset.String()]
		return Serviceability{State: state.State, BlockedRev: state.BlockedRev, Cycle: state.Cycle, NextDue: state.Plan.NextDue}, nil
	}
	state, ok := c.assets[asset.String()]
	if !ok || state.OpenWorkID == "" {
		return Serviceability{}, fmt.Errorf("%w: %s has no open service work", ErrServiceState, asset.String())
	}
	work := c.works[state.OpenWorkID]
	refuse := func(reason string) (Serviceability, error) {
		work.Status = WorkReleaseFailed
		work.ResultExplanation = reason
		work.UpdatedAt = time.Now().UTC()
		c.appendEvent(asset, ServiceEventReleaseFailed, actor.Actor.String(), reason)
		return Serviceability{}, fmt.Errorf("%w: %s", ErrServiceRelease, reason)
	}
	completion, ok := c.completions[work.WorkID]
	if !ok {
		return refuse("no service completion is recorded")
	}
	switch completion.Status {
	case CompletionVerified:
	case CompletionUnverified:
		return refuse("completion " + completion.ReceiptID + " is offline and not server-validated")
	default:
		return refuse("completion " + completion.ReceiptID + " is incomplete: " + completion.PartsAction)
	}
	if c.custody != nil {
		if current, ok := c.custody.Current(asset); ok && current.Status == Lost {
			return refuse("asset is lost in custody; recover it before release")
		}
	}
	state.Plan.LastService = at.UTC()
	if state.Plan.CalendarEnabled {
		state.Plan.NextDue = at.UTC().AddDate(0, 0, state.Plan.IntervalDays)
	}
	state.MeterDue = false
	state.Cycle++
	state.OpenWorkID = ""
	state.State = ServiceAvailable
	state.BlockedRev++
	work.Status = WorkReleased
	work.ResultExplanation = "released by " + strings.TrimSpace(reviewer)
	work.UpdatedAt = time.Now().UTC()
	c.appendEvent(asset, ServiceEventReleased, actor.Actor.String(), fmt.Sprintf("cycle %d next due %v", state.Cycle, state.Plan.NextDue.UTC().Format(time.RFC3339)))
	c.storeIdem("release", asset.Tenant, key, serviceIdem{Fingerprint: fp, WorkID: work.WorkID})
	return Serviceability{State: state.State, BlockedRev: state.BlockedRev, Cycle: state.Cycle, NextDue: state.Plan.NextDue}, nil
}

// ServiceabilityOf reports current eligibility.
func (c *MaintenanceCoordinator) ServiceabilityOf(asset values.EntityRef) (Serviceability, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	state, ok := c.assets[asset.String()]
	if !ok {
		return Serviceability{}, fmt.Errorf("%w: %s", ErrServiceState, asset.String())
	}
	return Serviceability{State: state.State, BlockedRev: state.BlockedRev, Cycle: state.Cycle, NextDue: state.Plan.NextDue}, nil
}

// RequestBooking holds equipment for a window. Blocked equipment refuses;
// warnings stay usable.
func (c *MaintenanceCoordinator) RequestBooking(actor ServiceActor, asset, owner values.EntityRef, bookingID string, start, end time.Time) (ServiceBooking, error) {
	if err := actor.validate(asset); err != nil {
		return ServiceBooking{}, err
	}
	if err := owner.Validate(); err != nil || owner.Tenant != asset.Tenant {
		return ServiceBooking{}, ErrTenantBoundary
	}
	if strings.TrimSpace(bookingID) == "" || !end.After(start) {
		return ServiceBooking{}, fmt.Errorf("%w: booking needs an id and a positive window", ErrServiceValidation)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	state, ok := c.assets[asset.String()]
	if !ok {
		return ServiceBooking{}, fmt.Errorf("%w: %s", ErrServiceState, asset.String())
	}
	if _, exists := c.bookings[strings.TrimSpace(bookingID)]; exists {
		return ServiceBooking{}, fmt.Errorf("%w: booking %s already exists", ErrServiceValidation, bookingID)
	}
	if state.State == ServiceOutOfService || state.State == ServiceInService {
		return ServiceBooking{}, fmt.Errorf("%w: equipment is %s", ErrServiceBlocked, state.State)
	}
	booking := &ServiceBooking{
		BookingID: strings.TrimSpace(bookingID), Asset: asset, Owner: owner,
		Start: start.UTC(), End: end.UTC(), Status: BookingHeld, EligibilityRev: state.BlockedRev,
	}
	c.bookings[booking.BookingID] = booking
	c.appendEvent(asset, ServiceEventBookingHeld, actor.Actor.String(), booking.BookingID)
	return *booking, nil
}

// ConfirmBooking confirms a held booking fenced on the current blocked
// revision. Confirms racing a block transition refuse instead of creating
// usable bookings past the revision.
func (c *MaintenanceCoordinator) ConfirmBooking(actor ServiceActor, bookingID string) (ServiceBooking, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	booking, ok := c.bookings[strings.TrimSpace(bookingID)]
	if !ok {
		return ServiceBooking{}, fmt.Errorf("%w: booking %s", ErrServiceState, bookingID)
	}
	if err := actor.validate(booking.Asset); err != nil {
		return ServiceBooking{}, err
	}
	switch booking.Status {
	case BookingConfirmed:
		return *booking, nil
	case BookingHeld:
	default:
		return ServiceBooking{}, fmt.Errorf("%w: booking %s is %s", ErrBookingConflict, bookingID, booking.Status)
	}
	state := c.assets[booking.Asset.String()]
	if booking.EligibilityRev != state.BlockedRev {
		booking.Status = BookingNeedsResolution
		c.appendEvent(booking.Asset, ServiceEventBookingFlagged, actor.Actor.String(), fmt.Sprintf("%s owner %s revision %d", booking.BookingID, booking.Owner.String(), booking.EligibilityRev))
		return ServiceBooking{}, fmt.Errorf("%w: booking fenced on revision %d, current is %d", ErrBookingConflict, booking.EligibilityRev, state.BlockedRev)
	}
	if state.State == ServiceOutOfService || state.State == ServiceInService {
		return ServiceBooking{}, fmt.Errorf("%w: equipment is %s", ErrServiceBlocked, state.State)
	}
	booking.Status = BookingConfirmed
	c.appendEvent(booking.Asset, ServiceEventBookingConfirmed, actor.Actor.String(), booking.BookingID)
	return *booking, nil
}

// Bookings lists every booking for the asset in booking-ID order.
func (c *MaintenanceCoordinator) Bookings(asset values.EntityRef) []ServiceBooking {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []ServiceBooking
	for _, booking := range c.bookings {
		if booking.Asset.String() == asset.String() {
			out = append(out, *booking)
		}
	}
	return out
}

// SetStockLink moves optional inventory linkage through its explicit
// states. Linking requires refs and receipt evidence; it never blocks
// unrelated lawful asset operations.
func (c *MaintenanceCoordinator) SetStockLink(actor ServiceActor, asset values.EntityRef, state StockLinkState, catalogRef, locationRef, evidence, key string) (MaintenancePlan, error) {
	if err := actor.validate(asset); err != nil {
		return MaintenancePlan{}, err
	}
	switch state {
	case StockLinkNone, StockLinkPending, StockLinkLinked:
	default:
		return MaintenancePlan{}, fmt.Errorf("%w: link state %q is not declared", ErrServiceValidation, state)
	}
	fp := fmt.Sprintf("link\x00%s\x00%s\x00%s\x00%s\x00%s", asset.String(), state, catalogRef, locationRef, evidence)
	c.mu.Lock()
	defer c.mu.Unlock()
	if prior, ok, err := c.checkIdem("link", asset.Tenant, key, fp); err != nil || ok {
		if err != nil {
			return MaintenancePlan{}, err
		}
		_ = prior
		return c.assets[asset.String()].Plan, nil
	}
	current, ok := c.assets[asset.String()]
	if !ok {
		return MaintenancePlan{}, fmt.Errorf("%w: %s", ErrServiceState, asset.String())
	}
	from := current.Plan.StockLinkState
	refs := strings.TrimSpace(catalogRef) != "" && strings.TrimSpace(locationRef) != ""
	switch {
	case from == state:
	case from == StockLinkNone && state == StockLinkPending:
	case from == StockLinkPending && state == StockLinkLinked && refs && strings.TrimSpace(evidence) != "":
	case from == StockLinkLinked && state == StockLinkNone && strings.TrimSpace(evidence) != "":
	case from == StockLinkPending && state == StockLinkNone:
	default:
		return MaintenancePlan{}, fmt.Errorf("%w: link transition %s to %s needs its refs and evidence", ErrServiceValidation, from, state)
	}
	current.Plan.StockLinkState = state
	if refs {
		current.Plan.CatalogItemRef = strings.TrimSpace(catalogRef)
		current.Plan.StockLocationRef = strings.TrimSpace(locationRef)
	}
	current.Plan.UpdatedAt = time.Now().UTC()
	c.appendEvent(asset, ServiceEventLink, actor.Actor.String(), fmt.Sprintf("%s to %s", from, state))
	c.storeIdem("link", asset.Tenant, key, serviceIdem{Fingerprint: fp, PlanVersion: current.Plan.PlanVersion})
	return current.Plan, nil
}

// Trace returns the asset event history oldest-first.
func (c *MaintenanceCoordinator) Trace(actor ServiceActor, asset values.EntityRef) ([]ServiceEvent, error) {
	if err := actor.validate(asset); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]ServiceEvent(nil), c.events[asset.String()]...), nil
}

// serviceSnapshot is the durable coordinator state.
type serviceSnapshot struct {
	Assets      map[string]assetServiceState
	Works       map[string]ServiceWorkItem
	Completions map[string]ServiceCompletion
	Bookings    map[string]ServiceBooking
	Events      map[string][]ServiceEvent
	PartsKeys   map[string]PartsReceipt
	Idem        map[string]serviceIdem
}

// Snapshot exports plans, cycles, work, bookings, meters and events.
func (c *MaintenanceCoordinator) Snapshot() ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	export := serviceSnapshot{
		Assets:      make(map[string]assetServiceState, len(c.assets)),
		Works:       make(map[string]ServiceWorkItem, len(c.works)),
		Completions: make(map[string]ServiceCompletion, len(c.completions)),
		Bookings:    make(map[string]ServiceBooking, len(c.bookings)),
		Events:      c.events, PartsKeys: c.partsKeys, Idem: c.idem,
	}
	for key, state := range c.assets {
		export.Assets[key] = *state
	}
	for id, work := range c.works {
		export.Works[id] = *work
	}
	for id, completion := range c.completions {
		export.Completions[id] = *completion
	}
	for id, booking := range c.bookings {
		export.Bookings[id] = *booking
	}
	return json.Marshal(export)
}

// Restore loads a Snapshot into an empty coordinator.
func (c *MaintenanceCoordinator) Restore(data []byte) error {
	var export serviceSnapshot
	if err := json.Unmarshal(data, &export); err != nil {
		return fmt.Errorf("asset: snapshot cannot be restored: %w", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.assets) > 0 || len(c.works) > 0 {
		return fmt.Errorf("asset: snapshot cannot be restored into a populated coordinator")
	}
	for key, state := range export.Assets {
		if state.Plan.Asset.String() == "" {
			return fmt.Errorf("asset: snapshot asset %s is corrupt", key)
		}
	}
	c.assets = make(map[string]*assetServiceState, len(export.Assets))
	for key, state := range export.Assets {
		stored := state
		c.assets[key] = &stored
	}
	c.works = make(map[string]*ServiceWorkItem, len(export.Works))
	for id, work := range export.Works {
		stored := work
		c.works[id] = &stored
	}
	c.completions = make(map[string]*ServiceCompletion, len(export.Completions))
	c.byReceipt = make(map[string]*ServiceCompletion, len(export.Completions))
	for id, completion := range export.Completions {
		stored := completion
		c.completions[id] = &stored
		c.byReceipt[stored.ReceiptID] = &stored
	}
	c.bookings = make(map[string]*ServiceBooking, len(export.Bookings))
	for id, booking := range export.Bookings {
		stored := booking
		c.bookings[id] = &stored
	}
	c.events = export.Events
	if c.events == nil {
		c.events = make(map[string][]ServiceEvent)
	}
	c.partsKeys = export.PartsKeys
	if c.partsKeys == nil {
		c.partsKeys = make(map[string]PartsReceipt)
	}
	c.idem = export.Idem
	if c.idem == nil {
		c.idem = make(map[string]serviceIdem)
	}
	return nil
}
