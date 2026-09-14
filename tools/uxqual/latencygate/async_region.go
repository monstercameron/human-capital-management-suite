package latencygate

import "fmt"

// RegionKind identifies the user task an asynchronous region supports.
type RegionKind string

const (
	CollectionRegion RegionKind = "collection"
	DetailRegion     RegionKind = "detail"
	FormRegion       RegionKind = "form"
	MutationRegion   RegionKind = "mutation"
)

// RegionState is the only state a shared async-region renderer needs to know.
type RegionState string

const (
	StateLoading    RegionState = "loading"
	StateRefreshing RegionState = "refreshing"
	StateReady      RegionState = "ready"
	StateEmpty      RegionState = "empty"
	StateError      RegionState = "error"
	StateSubmitting RegionState = "submitting"
	StateSuccess    RegionState = "success"
)

// RegionEvent advances a region without allowing an in-flight operation to be
// accidentally started twice. HasData describes the resolved projection.
type RegionEvent struct {
	Type      RegionEventType
	HasData   bool
	Retryable bool
	// OperationID correlates a completion with the start that created it.
	// Empty IDs preserve the small, synchronous test/use-case API; production
	// callers should always provide one for asynchronous work.
	OperationID string
}

type RegionEventType string

const (
	EventLoadStarted   RegionEventType = "load-started"
	EventLoadSucceeded RegionEventType = "load-succeeded"
	EventLoadFailed    RegionEventType = "load-failed"
	EventRetry         RegionEventType = "retry"
	EventSubmitStarted RegionEventType = "submit-started"
	EventSubmitSuccess RegionEventType = "submit-success"
	// EventSubmitSucceeded is the descriptive spelling retained alongside the
	// shorter EventSubmitSuccess name.
	EventSubmitSucceeded RegionEventType = EventSubmitSuccess
	EventSubmitFailed    RegionEventType = "submit-failed"
)

// Region is a deterministic lifecycle model for collection, detail, form and
// mutation surfaces. ReservedHeight is kept across all visual variants by the
// renderer, preventing state messages from pushing surrounding content.
type Region struct {
	Kind           RegionKind
	State          RegionState
	ReservedHeight int
	HasData        bool
	EnteredValues  bool
	NextAction     string
	Retryable      bool
	// ActiveOperationID is the operation whose result may currently advance
	// this region. It prevents a late response from an older request from
	// replacing newer data.
	ActiveOperationID string
}

func NewRegion(kind RegionKind, reservedHeight int) (Region, error) {
	if !validRegionKind(kind) {
		return Region{}, fmt.Errorf("latencygate: unsupported async region kind %q", kind)
	}
	if reservedHeight <= 0 {
		return Region{}, fmt.Errorf("latencygate: %s reserved height must be positive", kind)
	}
	return Region{Kind: kind, State: StateLoading, ReservedHeight: reservedHeight}, nil
}

// WithEnteredValues records that a form has input which must survive an
// asynchronous result. It is intentionally value-returning for easy UI use.
func (region Region) WithEnteredValues(entered bool) Region {
	region.EnteredValues = entered
	return region
}

// WithNextAction supplies the task-specific action shown by empty and error
// variants (for example, "Invite a worker" or "Try again").
func (region Region) WithNextAction(action string) Region {
	region.NextAction = action
	return region
}

func (region Region) Transition(event RegionEvent) (Region, error) {
	next := region
	switch event.Type {
	case EventLoadStarted:
		switch region.State {
		case StateLoading:
			if region.ActiveOperationID != "" && event.OperationID != "" && region.ActiveOperationID != event.OperationID {
				return region, invalidTransition(region, event)
			}
		case StateReady, StateEmpty:
			next.State, next.HasData = StateRefreshing, region.State == StateReady
		default:
			return region, invalidTransition(region, event)
		}
		if event.OperationID != "" {
			next.ActiveOperationID = event.OperationID
		}
	case EventLoadSucceeded:
		if region.State != StateLoading && region.State != StateRefreshing {
			return region, invalidTransition(region, event)
		}
		if err := region.checkOperation(event); err != nil {
			return region, err
		}
		next.HasData = event.HasData
		next.ActiveOperationID = ""
		if event.HasData {
			next.State = StateReady
		} else {
			next.State = StateEmpty
		}
	case EventLoadFailed:
		if region.State != StateLoading && region.State != StateRefreshing {
			return region, invalidTransition(region, event)
		}
		if err := region.checkOperation(event); err != nil {
			return region, err
		}
		next.ActiveOperationID = ""
		next.State, next.Retryable = StateError, event.Retryable
	case EventRetry:
		if region.State != StateError || !region.Retryable {
			return region, invalidTransition(region, event)
		}
		next.Retryable = false
		// A retry is itself a new operation. Carry its identity immediately so
		// a delayed result from the failed attempt can never settle this region.
		next.ActiveOperationID = event.OperationID
		if region.HasData {
			next.State = StateRefreshing
		} else {
			next.State = StateLoading
		}
	case EventSubmitStarted:
		if (region.Kind != FormRegion && region.Kind != MutationRegion) || region.State == StateSubmitting {
			return region, invalidTransition(region, event)
		}
		if region.State != StateReady && region.State != StateEmpty && (region.State != StateError || !region.Retryable) {
			return region, invalidTransition(region, event)
		}
		next.State, next.Retryable = StateSubmitting, false
		next.ActiveOperationID = event.OperationID
	case EventSubmitSuccess:
		if region.State != StateSubmitting {
			return region, invalidTransition(region, event)
		}
		if err := region.checkOperation(event); err != nil {
			return region, err
		}
		next.State, next.HasData = StateSuccess, event.HasData
		next.ActiveOperationID = ""
	case EventSubmitFailed:
		if region.State != StateSubmitting {
			return region, invalidTransition(region, event)
		}
		if err := region.checkOperation(event); err != nil {
			return region, err
		}
		next.State, next.Retryable = StateError, event.Retryable
		next.ActiveOperationID = ""
	default:
		return region, fmt.Errorf("latencygate: unknown async region event %q", event.Type)
	}
	return next, nil
}

// Presentation is the renderer-facing quality contract for a region state.
type Presentation struct {
	Variant            RegionState
	ReservedHeight     int
	RetainProjection   bool
	Politeness         string
	NextAction         string
	PreserveInput      bool
	SubmissionsBlocked bool
}

func (region Region) Presentation() Presentation {
	p := Presentation{Variant: region.State, ReservedHeight: region.ReservedHeight, PreserveInput: region.EnteredValues}
	if region.State == StateRefreshing || region.State == StateError {
		p.RetainProjection = region.HasData
	}
	if region.State == StateLoading || region.State == StateRefreshing || region.State == StateError || region.State == StateSuccess {
		p.Politeness = "polite"
	}
	if region.State == StateError {
		p.NextAction = region.NextAction
	}
	if region.State == StateEmpty {
		p.NextAction = region.NextAction
	}
	if region.State == StateSubmitting {
		p.SubmissionsBlocked = true
	}
	return p
}

func validRegionKind(kind RegionKind) bool {
	return kind == CollectionRegion || kind == DetailRegion || kind == FormRegion || kind == MutationRegion
}

func invalidTransition(region Region, event RegionEvent) error {
	return fmt.Errorf("latencygate: cannot apply %s to %s region in %s state", event.Type, region.Kind, region.State)
}

func (region Region) checkOperation(event RegionEvent) error {
	if region.ActiveOperationID != "" && region.ActiveOperationID != event.OperationID {
		return fmt.Errorf("latencygate: stale %s completion for operation %q (active %q)", event.Type, event.OperationID, region.ActiveOperationID)
	}
	return nil
}
