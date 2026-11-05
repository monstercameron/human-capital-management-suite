// Package roundtrip binds vendor round trips to workflows (WF-EXT-022): a
// connector-binding registry entry declares dispatch, callback correlation,
// bounded polling fallback, SLA and quarantine; the vendor round-trip
// fragment expands to dispatch CAPABILITY, correlated SIGNAL and bounded
// OBSERVE; and the trip driver correlates callbacks through
// internal/workflow/steps/signal, falls back to bounded polling, and
// quarantines on exhaustion — never fabricating a result.
package roundtrip

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/designerpalette"
	stepsignal "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/signal"
)

var (
	ErrInvalidBinding     = errors.New("roundtrip: invalid connector binding")
	ErrBindingExists      = errors.New("roundtrip: connector binding already registered")
	ErrBindingUnknown     = errors.New("roundtrip: connector binding is not registered")
	ErrBindingMismatch    = errors.New("roundtrip: capability does not resolve through this binding")
	ErrInvalidTrip        = errors.New("roundtrip: invalid vendor trip")
	ErrCallbackWindowOpen = errors.New("roundtrip: callback is still due; polling starts after the callback window")
	ErrTripTerminal       = errors.New("roundtrip: trip is already terminal")
)

// DispatchSpec declares the dispatch leg: the exact capability version that
// sends work to the vendor and the input field carrying the stable effect
// identity.
type DispatchSpec struct {
	CapabilityID        string
	CapabilityVersion   uint32
	Operation           string
	IdempotencyKeyField string
	AuthorityScopes     []string
}

// CallbackSpec declares the callback leg: the event type, correlation key,
// payload schema and the allowlisted vendor sources.
type CallbackSpec struct {
	EventType       string
	CorrelationKey  string
	SchemaRef       string
	AcceptedSources []string
}

// PollSpec declares the bounded polling fallback: at most MaxPolls reads
// through the read-back capability, spaced by the scheduler's interval.
type PollSpec struct {
	MaxPolls              int
	IntervalSeconds       uint64
	ReadCapabilityID      string
	ReadCapabilityVersion uint32
	ReadOperation         string
}

// SLASpec declares the vendor timing contract: the callback is due within
// CallbackDueSeconds of dispatch, and the trip quarantines at the hard
// deadline no matter what.
type SLASpec struct {
	CallbackDueSeconds  uint64
	HardDeadlineSeconds uint64
}

// QuarantineSpec declares where an exhausted trip goes: the author's
// quarantine route and the reason code the evidence carries.
type QuarantineSpec struct {
	Route      string
	ReasonCode string
}

// Binding is one connector-binding registry entry: the whole vendor
// round-trip contract in one versioned record.
type Binding struct {
	ID          string
	Version     uint32
	Vendor      string
	DisplayName string
	Dispatch    DispatchSpec
	Callback    CallbackSpec
	Poll        PollSpec
	SLA         SLASpec
	Quarantine  QuarantineSpec
}

// Validate refuses a binding that does not fully declare its round trip. A
// binding with no correlation, no poll bound, no SLA or no quarantine route
// is not a binding.
func (b Binding) Validate() error {
	if strings.TrimSpace(b.ID) == "" || b.Version == 0 || strings.TrimSpace(b.Vendor) == "" || strings.TrimSpace(b.DisplayName) == "" {
		return fmt.Errorf("%w: id, version, vendor and display name are required", ErrInvalidBinding)
	}
	if strings.TrimSpace(b.Dispatch.CapabilityID) == "" || b.Dispatch.CapabilityVersion == 0 || strings.TrimSpace(b.Dispatch.Operation) == "" || strings.TrimSpace(b.Dispatch.IdempotencyKeyField) == "" || len(b.Dispatch.AuthorityScopes) == 0 {
		return fmt.Errorf("%w: dispatch needs capability, operation, idempotency field and scopes", ErrInvalidBinding)
	}
	if strings.TrimSpace(b.Callback.EventType) == "" || strings.TrimSpace(b.Callback.CorrelationKey) == "" || strings.TrimSpace(b.Callback.SchemaRef) == "" || len(b.Callback.AcceptedSources) == 0 {
		return fmt.Errorf("%w: callback needs event type, correlation key, schema and accepted sources", ErrInvalidBinding)
	}
	if b.Poll.MaxPolls < 1 || b.Poll.IntervalSeconds == 0 || strings.TrimSpace(b.Poll.ReadCapabilityID) == "" || b.Poll.ReadCapabilityVersion == 0 || strings.TrimSpace(b.Poll.ReadOperation) == "" {
		return fmt.Errorf("%w: polling needs a positive bound, interval and read-back capability", ErrInvalidBinding)
	}
	if b.SLA.CallbackDueSeconds == 0 || b.SLA.HardDeadlineSeconds <= b.SLA.CallbackDueSeconds {
		return fmt.Errorf("%w: SLA needs a positive callback window strictly inside the hard deadline", ErrInvalidBinding)
	}
	if strings.TrimSpace(b.Quarantine.Route) == "" || strings.TrimSpace(b.Quarantine.ReasonCode) == "" {
		return fmt.Errorf("%w: quarantine needs a route and reason code", ErrInvalidBinding)
	}
	return nil
}

// Digest pins the binding bytes.
func (b Binding) Digest() string {
	sources := append([]string(nil), b.Callback.AcceptedSources...)
	sort.Strings(sources)
	scopes := append([]string(nil), b.Dispatch.AuthorityScopes...)
	sort.Strings(scopes)
	sum := sha256.Sum256([]byte(strings.Join([]string{"vendor-roundtrip-binding",
		b.ID, fmt.Sprintf("%d", b.Version), b.Vendor, b.DisplayName,
		b.Dispatch.CapabilityID, fmt.Sprintf("%d", b.Dispatch.CapabilityVersion), b.Dispatch.Operation, b.Dispatch.IdempotencyKeyField, strings.Join(scopes, ","),
		b.Callback.EventType, b.Callback.CorrelationKey, b.Callback.SchemaRef, strings.Join(sources, ","),
		fmt.Sprintf("%d", b.Poll.MaxPolls), fmt.Sprintf("%d", b.Poll.IntervalSeconds), b.Poll.ReadCapabilityID, fmt.Sprintf("%d", b.Poll.ReadCapabilityVersion), b.Poll.ReadOperation,
		fmt.Sprintf("%d", b.SLA.CallbackDueSeconds), fmt.Sprintf("%d", b.SLA.HardDeadlineSeconds),
		b.Quarantine.Route, b.Quarantine.ReasonCode,
	}, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Registry publishes connector bindings by exact (ID, Version). Publication
// is immutable: re-registering an identity is refused.
type Registry struct {
	bindings map[string]Binding
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{bindings: map[string]Binding{}} }

func bindingKey(id string, version uint32) string { return id + "\x00" + fmt.Sprintf("%d", version) }

// Register publishes one binding.
func (r *Registry) Register(b Binding) error {
	if r == nil {
		return fmt.Errorf("%w: registry is required", ErrInvalidBinding)
	}
	if err := b.Validate(); err != nil {
		return err
	}
	key := bindingKey(b.ID, b.Version)
	if _, exists := r.bindings[key]; exists {
		return fmt.Errorf("%w: %s/v%d", ErrBindingExists, b.ID, b.Version)
	}
	r.bindings[key] = b
	return nil
}

// Lookup resolves one exact binding version.
func (r *Registry) Lookup(id string, version uint32) (Binding, bool) {
	if r == nil {
		return Binding{}, false
	}
	b, ok := r.bindings[bindingKey(id, version)]
	return b, ok
}

// ResolveDelegated is the REFACTOR seam: a DELEGATED capability dispatches
// through the binding only when it names the binding's exact dispatch
// capability. Anything else is refused rather than misrouted.
func ResolveDelegated(b Binding, capabilityID string, version uint32) error {
	if err := b.Validate(); err != nil {
		return err
	}
	if capabilityID != b.Dispatch.CapabilityID || version != b.Dispatch.CapabilityVersion {
		return fmt.Errorf("%w: %s/v%d is not this binding's dispatch capability", ErrBindingMismatch, capabilityID, version)
	}
	return nil
}

var fragmentString = workflow.ValueType{Kind: workflow.KindString}

func fragmentSchema(id string) workflow.SchemaRef {
	return workflow.SchemaRef{SchemaID: id, Version: 1}
}

// FragmentEntry expands one binding into the vendor round-trip palette
// fragment: dispatch CAPABILITY, correlated SIGNAL and bounded OBSERVE, with
// the dispatch-to-signal and timed-out-to-poll internal edges. The SIGNAL's
// SUCCEEDED route and the OBSERVE's PASS/FAIL routes are fragment exits the
// author routes; exhaustion names the binding's quarantine route.
func FragmentEntry(b Binding) (designerpalette.Entry, error) {
	if err := b.Validate(); err != nil {
		return designerpalette.Entry{}, err
	}
	prefix := "roundtrip_" + strings.ReplaceAll(b.ID, ".", "_") + "_"
	dispatchID, awaitID, pollID := prefix+"dispatch", prefix+"await_callback", prefix+"poll"
	nodes := []workflow.Node{
		{
			ID: dispatchID, Type: workflow.StepCapability,
			InputSchema: fragmentSchema("roundtrip.dispatch.request/v1"), OutputSchema: fragmentSchema("roundtrip.dispatch.result/v1"),
			Inputs:  []workflow.Field{{Path: "candidate_ref", Type: fragmentString}, {Path: b.Dispatch.IdempotencyKeyField, Type: fragmentString}},
			Outputs: []workflow.Field{{Path: "correlation_value", Type: fragmentString}},
			Capability: &workflow.CapabilityRef{ID: b.Dispatch.CapabilityID, Version: b.Dispatch.CapabilityVersion,
				OperationMode: workflow.ModeExecute, AuthorityScopes: append([]string(nil), b.Dispatch.AuthorityScopes...),
				IdempotencyKeyMapping: b.Dispatch.IdempotencyKeyField, EffectBinding: "vendor." + b.ID},
			DeclaredEffect: capability.EffectExternalMutation, EffectRole: workflow.RoleDownstreamEffect,
			FailureRoute: b.Quarantine.Route,
		},
		{
			ID: awaitID, Type: workflow.StepSignal,
			InputSchema: fragmentSchema("roundtrip.callback.request/v1"), OutputSchema: fragmentSchema("roundtrip.callback.result/v1"),
			Inputs:  []workflow.Field{{Path: "correlation_value", Type: fragmentString}},
			Outputs: []workflow.Field{{Path: "callback_digest", Type: fragmentString}},
			Signal: &workflow.SignalSpec{EventType: b.Callback.EventType, CorrelationKeyExpression: b.Callback.CorrelationKey,
				ExpectedSchemaRef: fragmentSchema(b.Callback.SchemaRef), AcceptedSources: append([]string(nil), b.Callback.AcceptedSources...),
				Ordering: workflow.SignalOrderingNone, CloseAfterSeconds: b.SLA.CallbackDueSeconds},
			FailureRoute: b.Quarantine.Route,
		},
		{
			ID: pollID, Type: workflow.StepObserve,
			InputSchema: fragmentSchema("roundtrip.poll.request/v1"), OutputSchema: fragmentSchema("roundtrip.poll.result/v1"),
			Inputs:  []workflow.Field{{Path: "correlation_value", Type: fragmentString}},
			Outputs: []workflow.Field{{Path: "readback_digest", Type: fragmentString}, {Path: "poll_watermark", Type: fragmentString}},
			Capability: &workflow.CapabilityRef{ID: b.Poll.ReadCapabilityID, Version: b.Poll.ReadCapabilityVersion,
				OperationMode: workflow.ModeExecute, AuthorityScopes: append([]string(nil), b.Dispatch.AuthorityScopes...)},
			Observe: &workflow.ObserveSpec{EvidenceKind: workflow.EvidenceAuthoritativeRead, SourceAuthority: b.Vendor,
				ExpectedStateFields: []string{"correlation_value"}, MaxAgeSeconds: b.Poll.IntervalSeconds,
				ComparisonProfile: "roundtrip.poll/v1", RetryExhaustionRoute: b.Quarantine.Route},
			DeclaredEffect: capability.EffectReadOnly,
		},
	}
	edges := []workflow.Edge{
		{From: dispatchID, To: awaitID, RouteKey: string(workflow.OutcomeSucceeded)},
		{From: awaitID, To: pollID, RouteKey: "TIMED_OUT"},
	}
	return designerpalette.Entry{
		ID: "fragment.vendor_roundtrip." + b.ID, Version: b.Version, Name: "Vendor round trip: " + b.DisplayName,
		Kind: designerpalette.KindFragment, Domain: "Integrations",
		Description: "Dispatch to " + b.DisplayName + ", await its correlated callback, and fall back to bounded polling.",
		EffectClass: capability.EffectExternalMutation, Reversal: "COMPENSATE_OR_CORRECT", Status: "ACTIVE", StepType: workflow.StepCapability,
		RequiredCapabilities: []capability.Key{
			{ID: b.Dispatch.CapabilityID, Version: b.Dispatch.CapabilityVersion},
			{ID: b.Poll.ReadCapabilityID, Version: b.Poll.ReadCapabilityVersion},
		},
		Expansion: designerpalette.Expansion{Nodes: nodes, Edges: edges},
	}, nil
}

// TripState is the closed round-trip lifecycle.
type TripState string

const (
	TripDispatched       TripState = "DISPATCHED"
	TripCallbackAccepted TripState = "CALLBACK_ACCEPTED"
	TripPolling          TripState = "POLLING"
	TripReadbackFound    TripState = "READBACK_FOUND"
	TripQuarantined      TripState = "QUARANTINED"
)

// Trip is one vendor round trip: its correlation, dispatch instant, poll
// count, state and evidence. Evidence carries digests, watermarks and route
// notes only — a trip never fabricates vendor results.
type Trip struct {
	BindingID        string
	BindingVersion   uint32
	InstanceID       string
	NodeID           string
	CorrelationValue string
	DispatchedAt     time.Time
	PollsUsed        int
	State            TripState
	Evidence         []string
}

// Terminal reports whether the trip reached an end state.
func (t Trip) Terminal() bool {
	return t.State == TripCallbackAccepted || t.State == TripReadbackFound || t.State == TripQuarantined
}

// VendorCall is one dispatch request's workflow identity and inputs.
type VendorCall struct {
	InstanceID string
	NodeID     string
	Inputs     map[string]string
}

// DispatchReceipt is the vendor's acknowledgement: the correlation value all
// later legs join on.
type DispatchReceipt struct {
	CorrelationValue string
	DispatchedAt     time.Time
}

// Dispatcher sends work to the vendor. It returns the correlation value the
// vendor will echo on every leg of this trip.
type Dispatcher interface {
	Dispatch(ctx context.Context, binding Binding, call VendorCall, idempotencyKey string) (DispatchReceipt, error)
}

// PollReading is one bounded read-back: Found with a payload digest, or an
// explicit miss with the watermark observed.
type PollReading struct {
	Found         bool
	PayloadDigest string
	Watermark     string
	Detail        string
}

// Poller reads vendor state back through the binding's read capability.
type Poller interface {
	Poll(ctx context.Context, binding Binding, trip Trip) (PollReading, error)
}

// DispatchID derives the stable dispatch idempotency key: one binding,
// instance and node dispatch once.
func DispatchID(binding Binding, instanceID, nodeID string) string {
	return "vendor:" + binding.ID + ":" + instanceID + ":" + nodeID
}

// StartTrip validates the call, dispatches through the binding, and opens a
// DISPATCHED trip keyed by the vendor's correlation value.
func StartTrip(ctx context.Context, dispatcher Dispatcher, b Binding, call VendorCall, now time.Time) (Trip, error) {
	if err := b.Validate(); err != nil {
		return Trip{}, err
	}
	if dispatcher == nil {
		return Trip{}, fmt.Errorf("%w: dispatcher is required", ErrInvalidTrip)
	}
	if strings.TrimSpace(call.InstanceID) == "" || strings.TrimSpace(call.NodeID) == "" || now.IsZero() {
		return Trip{}, fmt.Errorf("%w: instance, node and dispatch instant are required", ErrInvalidTrip)
	}
	receipt, err := dispatcher.Dispatch(ctx, b, call, DispatchID(b, call.InstanceID, call.NodeID))
	if err != nil {
		return Trip{}, err
	}
	if strings.TrimSpace(receipt.CorrelationValue) == "" || receipt.DispatchedAt.IsZero() {
		return Trip{}, fmt.Errorf("%w: dispatch returned no correlation", ErrInvalidTrip)
	}
	at := receipt.DispatchedAt.UTC()
	return Trip{BindingID: b.ID, BindingVersion: b.Version, InstanceID: call.InstanceID, NodeID: call.NodeID,
		CorrelationValue: receipt.CorrelationValue, DispatchedAt: at, State: TripDispatched,
		Evidence: []string{"dispatched:" + receipt.CorrelationValue + "@" + at.Format(time.RFC3339)}}, nil
}

// SubscriptionFor builds the waiting SIGNAL subscription from the binding's
// callback contract and the trip's correlation value.
func SubscriptionFor(b Binding, tenant values.TenantId, trip Trip, closesAt values.Instant) (stepsignal.SignalSubscription, error) {
	if err := b.Validate(); err != nil {
		return stepsignal.SignalSubscription{}, err
	}
	if strings.TrimSpace(trip.InstanceID) == "" || strings.TrimSpace(trip.NodeID) == "" || strings.TrimSpace(trip.CorrelationValue) == "" {
		return stepsignal.SignalSubscription{}, fmt.Errorf("%w: trip carries no correlation", ErrInvalidTrip)
	}
	sub := stepsignal.SignalSubscription{Tenant: tenant, WorkflowInstanceID: trip.InstanceID, NodeID: trip.NodeID,
		EventType: b.Callback.EventType, CorrelationKey: b.Callback.CorrelationKey, CorrelationValue: trip.CorrelationValue,
		ExpectedSchemaRef: b.Callback.SchemaRef, AcceptedSources: append([]string(nil), b.Callback.AcceptedSources...),
		Ordering: stepsignal.OrderingNone, ClosesAt: closesAt}
	if err := sub.Validate(); err != nil {
		return stepsignal.SignalSubscription{}, err
	}
	return sub, nil
}

// AcceptCallback correlates one vendor callback to the waiting trip through
// steps/signal Accept. An accepted callback completes the trip; any refusal
// leaves the trip untouched, so a misdirected or forged callback can never
// complete someone else's trip.
func AcceptCallback(trip Trip, sub stepsignal.SignalSubscription, sig stepsignal.Signal, prior []stepsignal.LogEntry, verify stepsignal.Verifier, now values.Instant) (Trip, stepsignal.Result, error) {
	if trip.Terminal() {
		return trip, stepsignal.Result{}, fmt.Errorf("%w: %s", ErrTripTerminal, trip.State)
	}
	if trip.State != TripDispatched && trip.State != TripPolling {
		return trip, stepsignal.Result{}, fmt.Errorf("%w: state %s accepts no callback", ErrInvalidTrip, trip.State)
	}
	result, err := stepsignal.Accept(sub, sig, prior, verify, now)
	if err != nil {
		return trip, stepsignal.Result{}, err
	}
	if !result.Status.Accepted() {
		return trip, result, nil
	}
	sum := sha256.Sum256(sig.Payload)
	trip.State = TripCallbackAccepted
	trip.Evidence = append(trip.Evidence, "callback:accepted:sha256:"+hex.EncodeToString(sum[:]))
	return trip, result, nil
}

// PollOnce runs one bounded poll. Before the callback window passes it
// refuses (the vendor still owns the answer); a found read-back completes
// the trip; exhausted polls or the hard deadline quarantine it with observed
// evidence only.
func PollOnce(ctx context.Context, poller Poller, b Binding, trip Trip, now time.Time) (Trip, error) {
	if err := b.Validate(); err != nil {
		return trip, err
	}
	if poller == nil {
		return trip, fmt.Errorf("%w: poller is required", ErrInvalidTrip)
	}
	if trip.Terminal() {
		return trip, fmt.Errorf("%w: %s", ErrTripTerminal, trip.State)
	}
	if now.IsZero() {
		return trip, fmt.Errorf("%w: poll instant is required", ErrInvalidTrip)
	}
	deadline := trip.DispatchedAt.Add(time.Duration(b.SLA.HardDeadlineSeconds) * time.Second)
	if !now.Before(deadline) {
		return quarantine(trip, b, "hard deadline passed with no correlated result"), nil
	}
	if now.Before(trip.DispatchedAt.Add(time.Duration(b.SLA.CallbackDueSeconds) * time.Second)) {
		return trip, fmt.Errorf("%w: callback due %s", ErrCallbackWindowOpen, trip.DispatchedAt.Add(time.Duration(b.SLA.CallbackDueSeconds)*time.Second).UTC().Format(time.RFC3339))
	}
	if trip.PollsUsed >= b.Poll.MaxPolls {
		return quarantine(trip, b, "poll bound exhausted"), nil
	}
	reading, err := poller.Poll(ctx, b, trip)
	if err != nil {
		return trip, err
	}
	trip.PollsUsed++
	if reading.Found {
		if strings.TrimSpace(reading.PayloadDigest) == "" {
			return trip, fmt.Errorf("%w: read-back found without a payload digest", ErrInvalidTrip)
		}
		trip.State = TripReadbackFound
		trip.Evidence = append(trip.Evidence, "readback:"+reading.PayloadDigest+"@"+reading.Watermark)
		return trip, nil
	}
	trip.State = TripPolling
	trip.Evidence = append(trip.Evidence, fmt.Sprintf("poll:%d:%s", trip.PollsUsed, reading.Watermark))
	return trip, nil
}

func quarantine(trip Trip, b Binding, cause string) Trip {
	trip.State = TripQuarantined
	trip.Evidence = append(trip.Evidence, "callback:absent", "quarantined:"+b.Quarantine.ReasonCode+":"+cause+"->"+b.Quarantine.Route)
	return trip
}
