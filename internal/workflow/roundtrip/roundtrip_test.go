package roundtrip

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/designerpalette"
	stepsignal "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/signal"
)

func testBinding() Binding {
	return Binding{
		ID: "screening.acme", Version: 2, Vendor: "vendor:screening/acme-checks", DisplayName: "Acme background screening",
		Dispatch:   DispatchSpec{CapabilityID: "hcmnext.screening.dispatch_check", CapabilityVersion: 3, Operation: "dispatch", IdempotencyKeyField: "idempotency_key", AuthorityScopes: []string{"scope:screening.dispatch"}},
		Callback:   CallbackSpec{EventType: "screening.check.completed", CorrelationKey: "screening_check_id", SchemaRef: "screening.callback/v2", AcceptedSources: []string{"vendor:screening/acme-checks"}},
		Poll:       PollSpec{MaxPolls: 4, IntervalSeconds: 3600, ReadCapabilityID: "hcmnext.screening.read_check", ReadCapabilityVersion: 1, ReadOperation: "read"},
		SLA:        SLASpec{CallbackDueSeconds: 86400, HardDeadlineSeconds: 86400 * 3},
		Quarantine: QuarantineSpec{Route: "quarantine_review", ReasonCode: "VENDOR_SLA_BREACH"},
	}
}

// TestTodo_WF_EXT_022 proves the PRIMARY matrix of WF-EXT-022: a
// connector-binding registry entry declares dispatch, callback correlation,
// bounded polling fallback, SLA and quarantine; the vendor round-trip
// fragment expands to dispatch CAPABILITY, correlated SIGNAL and bounded
// OBSERVE; DELEGATED capabilities resolve through the binding.
func TestTodo_WF_EXT_022(t *testing.T) {
	binding := testBinding()
	if err := binding.Validate(); err != nil {
		t.Fatalf("binding: %v", err)
	}
	if binding.Digest() == "" || binding.Digest() != binding.Digest() {
		t.Fatal("binding has no stable digest")
	}
	changed := binding
	changed.Poll.MaxPolls = 9
	if changed.Digest() == binding.Digest() {
		t.Fatal("binding digest ignores the polling bound")
	}
	bad := binding
	bad.Callback.AcceptedSources = nil
	if err := bad.Validate(); !errors.Is(err, ErrInvalidBinding) {
		t.Fatalf("sourceless callback = %v, want ErrInvalidBinding", err)
	}
	bad = binding
	bad.Poll.MaxPolls = 0
	if err := bad.Validate(); !errors.Is(err, ErrInvalidBinding) {
		t.Fatalf("unbounded poll = %v, want ErrInvalidBinding", err)
	}
	bad = binding
	bad.SLA.HardDeadlineSeconds = bad.SLA.CallbackDueSeconds
	if err := bad.Validate(); !errors.Is(err, ErrInvalidBinding) {
		t.Fatalf("deadline within callback due = %v, want ErrInvalidBinding", err)
	}

	registry := NewRegistry()
	if err := registry.Register(binding); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := registry.Register(binding); !errors.Is(err, ErrBindingExists) {
		t.Fatalf("duplicate register = %v, want ErrBindingExists", err)
	}
	found, ok := registry.Lookup(binding.ID, binding.Version)
	if !ok || found.Digest() != binding.Digest() {
		t.Fatal("registered binding did not resolve")
	}
	if _, ok := registry.Lookup(binding.ID, 99); ok {
		t.Fatal("unknown binding version resolved")
	}

	entry, err := FragmentEntry(binding)
	if err != nil {
		t.Fatalf("FragmentEntry: %v", err)
	}
	if entry.Kind != designerpalette.KindFragment || entry.ID != "fragment.vendor_roundtrip.screening.acme" {
		t.Fatalf("fragment entry = %+v", entry)
	}
	if len(entry.Expansion.Nodes) != 3 || len(entry.Expansion.Edges) != 2 {
		t.Fatalf("fragment expands to %d nodes %d edges, want 3 nodes (dispatch CAPABILITY, SIGNAL, OBSERVE) and 2 edges", len(entry.Expansion.Nodes), len(entry.Expansion.Edges))
	}
	byType := map[workflow.StepType]workflow.Node{}
	for _, node := range entry.Expansion.Nodes {
		byType[node.Type] = node
	}
	dispatch, ok := byType[workflow.StepCapability]
	if !ok || dispatch.Capability == nil || dispatch.Capability.ID != binding.Dispatch.CapabilityID || dispatch.Capability.Version != binding.Dispatch.CapabilityVersion {
		t.Fatalf("dispatch node = %+v", byType[workflow.StepCapability])
	}
	if dispatch.Capability.IdempotencyKeyMapping != binding.Dispatch.IdempotencyKeyField {
		t.Fatalf("dispatch idempotency mapping = %q", dispatch.Capability.IdempotencyKeyMapping)
	}
	await, ok := byType[workflow.StepSignal]
	if !ok || await.Signal == nil || await.Signal.EventType != binding.Callback.EventType || await.Signal.CloseAfterSeconds != binding.SLA.CallbackDueSeconds {
		t.Fatalf("signal node = %+v", byType[workflow.StepSignal])
	}
	if await.Signal.CorrelationKeyExpression != binding.Callback.CorrelationKey || len(await.Signal.AcceptedSources) != 1 {
		t.Fatalf("signal correlation = %+v", await.Signal)
	}
	poll, ok := byType[workflow.StepObserve]
	if !ok || poll.Observe == nil || poll.Observe.RetryExhaustionRoute != binding.Quarantine.Route {
		t.Fatalf("observe node = %+v", byType[workflow.StepObserve])
	}
	wantEdges := map[string]string{dispatch.ID + ">SUCCEEDED": await.ID, await.ID + ">TIMED_OUT": poll.ID}
	for _, edge := range entry.Expansion.Edges {
		key := edge.From + ">" + edge.RouteKey
		if wantEdges[key] != edge.To {
			t.Fatalf("edge %+v is not the round-trip path", edge)
		}
		delete(wantEdges, key)
	}
	if len(wantEdges) != 0 {
		t.Fatalf("missing fragment edges: %v", wantEdges)
	}

	if err := ResolveDelegated(binding, binding.Dispatch.CapabilityID, binding.Dispatch.CapabilityVersion); err != nil {
		t.Fatalf("ResolveDelegated: %v", err)
	}
	if err := ResolveDelegated(binding, "hcmnext.other.dispatch", 1); !errors.Is(err, ErrBindingMismatch) {
		t.Fatalf("foreign capability resolved: %v", err)
	}

	trip := Trip{BindingID: binding.ID, BindingVersion: binding.Version, InstanceID: "instance-1", NodeID: "await_screening", CorrelationValue: "check-7", State: TripDispatched}
	sub, err := SubscriptionFor(binding, values.TenantId("acme"), trip, values.Instant{})
	if err != nil {
		t.Fatalf("SubscriptionFor: %v", err)
	}
	if err := sub.Validate(); err != nil {
		t.Fatalf("subscription: %v", err)
	}
	if sub.CorrelationValue != "check-7" || sub.EventType != binding.Callback.EventType || sub.NodeID != "await_screening" {
		t.Fatalf("subscription = %+v", sub)
	}
}

// simulatorVendor is the INTEGRATION/FAULT vendor: dispatch records the call,
// callbacks are emitted on demand, and polls read scripted findings.
type simulatorVendor struct {
	dispatched []VendorCall
	keys       []string
	findings   []PollReading
	polls      int
}

func (v *simulatorVendor) Dispatch(_ context.Context, _ Binding, call VendorCall, idempotencyKey string) (DispatchReceipt, error) {
	v.dispatched = append(v.dispatched, call)
	v.keys = append(v.keys, idempotencyKey)
	return DispatchReceipt{CorrelationValue: "check-" + call.NodeID, DispatchedAt: time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)}, nil
}

func (v *simulatorVendor) Poll(_ context.Context, _ Binding, _ Trip) (PollReading, error) {
	if v.polls >= len(v.findings) {
		return PollReading{Found: false, Watermark: "sim:empty", Detail: "no finding yet"}, nil
	}
	reading := v.findings[v.polls]
	v.polls++
	return reading, nil
}

type hmacVerifier struct{ key []byte }

func (v hmacVerifier) Verify(sig stepsignal.Signal) error {
	mac := hmac.New(sha256.New, v.key)
	mac.Write(sig.Payload)
	var want []byte
	want = mac.Sum(want)
	if !hmac.Equal(want, sig.Signature) {
		return errors.New("bad signature")
	}
	return nil
}

func signPayload(key, payload []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(payload)
	return mac.Sum(nil)
}

// TestTodo_WF_EXT_022_Integration completes one vendor round trip against the
// simulator: dispatch, correlated callback, verified acceptance.
func TestTodo_WF_EXT_022_Integration(t *testing.T) {
	ctx := context.Background()
	binding := testBinding()
	vendor := &simulatorVendor{}
	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	trip, err := StartTrip(ctx, vendor, binding, VendorCall{InstanceID: "instance-1", NodeID: "await_screening", Inputs: map[string]string{"candidate": "worker:w1"}}, now)
	if err != nil {
		t.Fatalf("StartTrip: %v", err)
	}
	if trip.State != TripDispatched || trip.CorrelationValue == "" {
		t.Fatalf("trip = %+v", trip)
	}
	if len(vendor.keys) != 1 || !strings.HasPrefix(vendor.keys[0], "vendor:screening.acme:instance-1:await_screening") {
		t.Fatalf("dispatch key = %v", vendor.keys)
	}
	sub, err := SubscriptionFor(binding, values.TenantId("acme"), trip, values.NewInstant(now.Add(48*time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	key := []byte("sim-key")
	payload := []byte(`{"verdict":"clear"}`)
	sig := stepsignal.Signal{Tenant: values.TenantId("acme"), Source: binding.Callback.AcceptedSources[0], EventType: binding.Callback.EventType,
		SchemaRef: binding.Callback.SchemaRef, CorrelationKey: binding.Callback.CorrelationKey, CorrelationValue: trip.CorrelationValue,
		IdempotencyKey: "cb-1", Payload: payload, Signature: signPayload(key, payload), ReceivedAt: values.NewInstant(now.Add(time.Hour))}
	trip, result, err := AcceptCallback(trip, sub, sig, nil, hmacVerifier{key: key}, values.NewInstant(now.Add(time.Hour)))
	if err != nil {
		t.Fatalf("AcceptCallback: %v", err)
	}
	if result.Status != stepsignal.StatusAccepted || trip.State != TripCallbackAccepted {
		t.Fatalf("result = %+v trip = %+v", result, trip)
	}
	if !trip.Terminal() {
		t.Fatal("accepted trip is not terminal")
	}
	joined := strings.Join(trip.Evidence, "\n")
	for _, want := range []string{"dispatched:", "callback:accepted:", trip.CorrelationValue} {
		if !strings.Contains(joined, want) {
			t.Fatalf("trip evidence misses %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, `"verdict":"clear"`) {
		t.Fatalf("trip evidence carries vendor payload bytes:\n%s", joined)
	}
}

// TestTodo_WF_EXT_022_Fault proves a lost callback falls back to bounded
// polling and then quarantine, never a fabricated result; and that a
// mid-poll finding still completes through read-back.
func TestTodo_WF_EXT_022_Fault(t *testing.T) {
	ctx := context.Background()
	binding := testBinding()
	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)

	t.Run("lost_callback_quarantines_without_fabrication", func(t *testing.T) {
		vendor := &simulatorVendor{}
		trip, err := StartTrip(ctx, vendor, binding, VendorCall{InstanceID: "instance-9", NodeID: "await_screening"}, now)
		if err != nil {
			t.Fatal(err)
		}
		// Polling before the callback is due is refused: the vendor still
		// owns the answer.
		if _, err := PollOnce(ctx, vendor, binding, trip, now.Add(time.Hour)); !errors.Is(err, ErrCallbackWindowOpen) {
			t.Fatalf("early poll = %v, want ErrCallbackWindowOpen", err)
		}
		late := now.Add(time.Duration(binding.SLA.CallbackDueSeconds+1) * time.Second)
		for i := 0; i < binding.Poll.MaxPolls; i++ {
			trip, err = PollOnce(ctx, vendor, binding, trip, late.Add(time.Duration(i)*time.Hour))
			if err != nil {
				t.Fatal(err)
			}
			if trip.State == TripQuarantined {
				t.Fatalf("poll %d quarantined early: %+v", i, trip)
			}
		}
		if trip.State != TripPolling || trip.PollsUsed != binding.Poll.MaxPolls {
			t.Fatalf("polled trip = %+v", trip)
		}
		trip, err = PollOnce(ctx, vendor, binding, trip, late.Add(5*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		if trip.State != TripQuarantined || !trip.Terminal() {
			t.Fatalf("exhausted trip = %+v, want QUARANTINED", trip)
		}
		joined := strings.Join(trip.Evidence, "\n")
		for _, want := range []string{"quarantined:VENDOR_SLA_BREACH", "poll:4:", "callback:absent"} {
			if !strings.Contains(joined, want) {
				t.Fatalf("quarantine evidence misses %q:\n%s", want, joined)
			}
		}
	})

	t.Run("poll_finding_completes_through_readback", func(t *testing.T) {
		vendor := &simulatorVendor{findings: []PollReading{
			{Found: false, Watermark: "sim:1", Detail: "pending"},
			{Found: true, PayloadDigest: "sha256:abc", Watermark: "sim:2", Detail: "check complete"},
		}}
		trip, err := StartTrip(ctx, vendor, binding, VendorCall{InstanceID: "instance-3", NodeID: "await_screening"}, now)
		if err != nil {
			t.Fatal(err)
		}
		late := now.Add(time.Duration(binding.SLA.CallbackDueSeconds+1) * time.Second)
		trip, err = PollOnce(ctx, vendor, binding, trip, late)
		if err != nil || trip.State != TripPolling {
			t.Fatalf("first poll = %+v %v", trip, err)
		}
		trip, err = PollOnce(ctx, vendor, binding, trip, late.Add(time.Hour))
		if err != nil || trip.State != TripReadbackFound {
			t.Fatalf("second poll = %+v %v, want READBACK_FOUND", trip, err)
		}
		if !strings.Contains(strings.Join(trip.Evidence, "\n"), "readback:sha256:abc") {
			t.Fatalf("readback evidence = %v", trip.Evidence)
		}
	})

	t.Run("wrong_correlation_is_never_accepted", func(t *testing.T) {
		vendor := &simulatorVendor{}
		trip, err := StartTrip(ctx, vendor, binding, VendorCall{InstanceID: "instance-5", NodeID: "await_screening"}, now)
		if err != nil {
			t.Fatal(err)
		}
		sub, err := SubscriptionFor(binding, values.TenantId("acme"), trip, values.NewInstant(now.Add(48*time.Hour)))
		if err != nil {
			t.Fatal(err)
		}
		key := []byte("sim-key")
		payload := []byte(`{"verdict":"clear"}`)
		sig := stepsignal.Signal{Tenant: values.TenantId("acme"), Source: binding.Callback.AcceptedSources[0], EventType: binding.Callback.EventType,
			SchemaRef: binding.Callback.SchemaRef, CorrelationKey: binding.Callback.CorrelationKey, CorrelationValue: "check-someone-else",
			IdempotencyKey: "cb-x", Payload: payload, Signature: signPayload(key, payload), ReceivedAt: values.NewInstant(now.Add(time.Hour))}
		after, result, err := AcceptCallback(trip, sub, sig, nil, hmacVerifier{key: key}, values.NewInstant(now.Add(time.Hour)))
		if err != nil {
			t.Fatalf("AcceptCallback: %v", err)
		}
		if result.Status != stepsignal.StatusRefusedWrongCorrelation {
			t.Fatalf("result = %+v, want REFUSED_WRONG_CORRELATION", result)
		}
		if after.State != TripDispatched || after.Terminal() {
			t.Fatalf("trip = %+v, want still DISPATCHED", after)
		}
	})
}
