package compensate

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"strings"
	"sync"
	"testing"
	"time"
)

var testNow = time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

func validRequest() Request {
	tenant := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	return Request{TenantID: tenant.String(), ActorID: "a", TargetExecutionRef: "x", TargetEffectRef: "e", CompensationCapabilityRef: "cap/v1", VerificationObservationRef: "obs/v1", Reason: "repair", ApprovalPolicy: "p", ApprovalRef: "ap", AuthorityPolicyFingerprint: "pf", CapabilityManifestDigest: "md", PayloadDigest: "sha256:p", IdempotencyKey: "i", OriginalHistoryRef: "h", Strategy: StrategyCorrection, RepairRef: "repair-1", ObservationMaxAge: time.Hour,
		PlanDigest: "source-plan-digest", OriginalScope: idempotency.Scope{Tenant: tenant, Capability: "source.capability", EffectScope: "source-effect", Key: "source-key"}, OriginalEffectKind: OriginalEffectIdempotencyScope}
}

type fakeAuthorizer struct{ other bool }

func (a fakeAuthorizer) Authorize(_ context.Context, r Request) (AuthorizationDecision, error) {
	d := AuthorizationDecision{true, r.TenantID, r.ActorID, r.TargetEffectRef, r.CompensationCapabilityRef, r.PayloadDigest, r.AuthorityPolicyFingerprint, r.ApprovalRef, "auth"}
	if a.other {
		d.TenantID = "other"
	}
	return d, nil
}

type fakeCapability struct {
	mu      sync.Mutex
	calls   int
	receipt CapabilityReceipt
	err     error
}

func (c *fakeCapability) Manifest(context.Context) (CapabilityManifest, error) {
	return CapabilityManifest{"cap/v1", "md", true}, nil
}
func (c *fakeCapability) Compensate(context.Context, CapabilityRequest) (CapabilityReceipt, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	return c.receipt, c.err
}

type fakeObserver struct {
	o   Observation
	err error
}

func (o fakeObserver) ObserveCompensation(context.Context, ObservationRequest) (Observation, error) {
	return o.o, o.err
}

type fakeOwner struct {
	mu       sync.Mutex
	m        map[OperationKey]OperationRecord
	closures int
}

func (o *fakeOwner) Reserve(_ context.Context, k OperationKey, d string) (OperationRecord, bool, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if x, ok := o.m[k]; ok {
		return x, false, nil
	}
	x := OperationRecord{Key: k, RequestDigest: d, State: OperationReserved}
	o.m[k] = x
	return x, true, nil
}
func (o *fakeOwner) RecordEffect(_ context.Context, k OperationKey, d string, c CapabilityReceipt) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	x := o.m[k]
	x.State = OperationEffectRecorded
	x.Receipt = c
	o.m[k] = x
	return nil
}
func (o *fakeOwner) Complete(_ context.Context, k OperationKey, d string, r Result) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	x := o.m[k]
	x.State = OperationCompleted
	x.Result = r
	o.m[k] = x
	return nil
}
func (o *fakeOwner) CloseCompensated(_ context.Context, _ idempotency.Scope, eventRef string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if eventRef == "" {
		return errors.New("empty event ref")
	}
	o.closures++
	return nil
}

type fakeLedger struct {
	mu     sync.Mutex
	events map[string]Event
	fail   bool
}

func (l *fakeLedger) AppendCompensation(_ context.Context, e Event) (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.fail {
		l.fail = false
		return "", errors.New("ledger")
	}
	l.events[e.Digest] = e
	return e.EventRef, nil
}
func fixture() (*Executor, *fakeCapability, *fakeOwner, *fakeLedger) {
	r := validRequest()
	c := &fakeCapability{receipt: CapabilityReceipt{Accepted: true, Applied: true, EvidenceRef: "ce"}}
	o := &fakeOwner{m: map[OperationKey]OperationRecord{}}
	l := &fakeLedger{events: map[string]Event{}}
	obs := Observation{Match: true, ObservedAt: testNow, TenantID: r.TenantID, TargetEffectRef: r.TargetEffectRef, CorrectionEvidenceRef: "ce", EvidenceRef: "oe"}
	return &Executor{c, fakeAuthorizer{}, fakeObserver{o: obs}, o, l, func() time.Time { return testNow }}, c, o, l
}
func TestTodo_WF_STEP_016(t *testing.T) {
	e, _, _, l := fixture()
	got, err := e.Execute(context.Background(), validRequest())
	if err != nil || got.Status != StatusCompensated || got.Event.AuthorizationEvidenceRef == "" ||
		!strings.HasPrefix(got.Event.EventRef, "compensation:v1:") || len(l.events) != 1 {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}

// TestTodo_WF_REV_010 proves the compensating ledger identity is stable for
// an identical correction request and changes when the semantic request
// changes. The ledger append receipt is the identity returned to discharge.
func TestTodo_WF_REV_010(t *testing.T) {
	e1, _, _, l1 := fixture()
	first, err := e1.Execute(context.Background(), validRequest())
	if err != nil || first.Event.EventRef == "" || len(l1.events) != 1 || l1.events[first.Event.Digest].EventRef != first.Event.EventRef {
		t.Fatalf("first compensation event = %+v, err=%v", first.Event, err)
	}
	e2, _, _, _ := fixture()
	replayedIdentity, err := e2.Execute(context.Background(), validRequest())
	if err != nil || replayedIdentity.Event.EventRef != first.Event.EventRef {
		t.Fatalf("identical compensation identity = %q (%v), want %q", replayedIdentity.Event.EventRef, err, first.Event.EventRef)
	}
	e3, _, _, _ := fixture()
	changed := validRequest()
	changed.IdempotencyKey = "different-effect"
	different, err := e3.Execute(context.Background(), changed)
	if err != nil || different.Event.EventRef == first.Event.EventRef {
		t.Fatalf("distinct compensation identity = %q (%v), want a different event ref", different.Event.EventRef, err)
	}
}

func TestTodo_WF_REV_010_Property(t *testing.T) {
	for i := 0; i < 3; i++ {
		e, _, _, _ := fixture()
		got, err := e.Execute(context.Background(), validRequest())
		if err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
		if i == 0 {
			continue
		}
		first, _, _, _ := fixture()
		want, err := first.Execute(context.Background(), validRequest())
		if err != nil || got.Event.EventRef != want.Event.EventRef {
			t.Fatalf("run %d event ref %q differs from stable ref %q: %v", i, got.Event.EventRef, want.Event.EventRef, err)
		}
	}
}

func TestTodo_WF_STEP_016_Golden(t *testing.T) {
	e, _, _, _ := fixture()
	got, _ := e.Execute(context.Background(), validRequest())
	if got.Event.Digest != "354929e6aed9c7b0f3a672c99bc5c1db602fc8358fcffb50570aca4bb4c2b3f6" {
		t.Fatalf("digest=%s", got.Event.Digest)
	}
}

func TestCompensationRequestRequiresOriginalClosureBinding(t *testing.T) {
	r := validRequest()
	r.OriginalScope = idempotency.Scope{}
	r.PlanDigest = ""
	if err := r.Validate(); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("request without original binding err=%v, want ErrInvalidRequest", err)
	}
}
func TestTodo_WF_STEP_016_Security(t *testing.T) {
	e, c, _, _ := fixture()
	e.Authorizer = fakeAuthorizer{true}
	if _, err := e.Execute(context.Background(), validRequest()); !errors.Is(err, ErrAuthorizationRequired) {
		t.Fatalf("err=%v", err)
	}
	if c.calls != 0 {
		t.Fatal("effect before authority")
	}
}
func TestTodo_WF_STEP_016_Conformance(t *testing.T) {
	e, c, _, l := fixture()
	r := validRequest()
	r.Strategy = StrategyIrreversible
	got, err := e.Execute(context.Background(), r)
	if err != nil || got.Status != StatusRepairRequired || c.calls != 0 || len(l.events) != 1 {
		t.Fatalf("got=%+v err=%v calls=%d", got, err, c.calls)
	}
}
func TestTodo_WF_STEP_016_Recovery(t *testing.T) {
	e, c, _, l := fixture()
	l.fail = true
	if _, err := e.Execute(context.Background(), validRequest()); err == nil {
		t.Fatal("want ledger failure")
	}
	got, err := e.Execute(context.Background(), validRequest())
	if err != nil || got.Status != StatusCompensated || c.calls != 1 {
		t.Fatalf("got=%+v err=%v calls=%d", got, err, c.calls)
	}
}
func TestTodo_WF_STEP_016_Race(t *testing.T) {
	e, c, _, _ := fixture()
	var w sync.WaitGroup
	for range 20 {
		w.Add(1)
		go func() {
			defer w.Done()
			if _, err := e.Execute(context.Background(), validRequest()); err != nil && !errors.Is(err, ErrIdempotencyRequired) {
				t.Errorf("err=%v", err)
			}
		}()
	}
	w.Wait()
	if c.calls != 1 {
		t.Fatalf("effects=%d", c.calls)
	}
}
func TestTodo_WF_STEP_016_Mutation(t *testing.T) {
	for name, mutate := range map[string]func(*Result){
		"event without matching digest": func(r *Result) { r.Event.Status = StatusFailed },
		"outer status differs":          func(r *Result) { r.Status = StatusFailed },
		"alternate authority": func(r *Result) {
			r.Event.AuthorizationEvidenceRef = "other-auth"
			r.Event.Digest = Digest(r.Event)
		},
		"alternate capability evidence": func(r *Result) {
			r.Event.CapabilityEvidenceRef = "other-capability-receipt"
			r.Event.Digest = Digest(r.Event)
		},
		"alternate request with valid digest": func(r *Result) {
			r.Event.Request.Reason = "different correction"
			r.Event.Digest = Digest(r.Event)
		},
		"persisted replay marker": func(r *Result) { r.Replayed = true },
	} {
		t.Run(name, func(t *testing.T) {
			e, _, o, _ := fixture()
			got, err := e.Execute(context.Background(), validRequest())
			if err != nil {
				t.Fatal(err)
			}
			k := OperationKey{validRequest().TenantID, "cap/v1", "e", "i"}
			o.mu.Lock()
			x := o.m[k]
			mutate(&x.Result)
			o.m[k] = x
			o.mu.Unlock()
			if _, err := e.Execute(context.Background(), validRequest()); !errors.Is(err, ErrIdempotencyRequired) {
				t.Fatalf("err=%v", err)
			}
			if got.Event.Status != StatusCompensated {
				t.Fatal("returned result aliased stored mutation")
			}
		})
	}
}

func TestProviderErrorsAreNotPersisted(t *testing.T) {
	for name, fault := range map[string]func(*Executor, *fakeCapability){
		"capability": func(_ *Executor, c *fakeCapability) { c.err = errors.New("provider-token=secret") },
		"observation": func(e *Executor, _ *fakeCapability) {
			o := e.Observer.(fakeObserver)
			o.err = errors.New("provider-token=secret")
			e.Observer = o
		},
	} {
		t.Run(name, func(t *testing.T) {
			e, c, _, l := fixture()
			fault(e, c)
			if _, err := e.Execute(context.Background(), validRequest()); err != nil {
				t.Fatal(err)
			}
			for _, event := range l.events {
				if strings.Contains(event.Detail, "secret") {
					t.Fatalf("persisted provider error: %q", event.Detail)
				}
			}
		})
	}
}
func TestTodo_WF_STEP_016_Fault(t *testing.T) {
	for name, change := range map[string]func(*Executor, *fakeCapability){"capability": func(_ *Executor, c *fakeCapability) { c.err = errors.New("down") }, "stale": func(e *Executor, _ *fakeCapability) {
		o := e.Observer.(fakeObserver)
		o.o.ObservedAt = testNow.Add(-2 * time.Hour)
		e.Observer = o
	}, "contradictory": func(e *Executor, _ *fakeCapability) {
		o := e.Observer.(fakeObserver)
		o.o.Partial = true
		e.Observer = o
	}} {
		t.Run(name, func(t *testing.T) {
			e, c, _, l := fixture()
			change(e, c)
			got, err := e.Execute(context.Background(), validRequest())
			if err != nil || got.Status != StatusRepairRequired || len(l.events) != 1 {
				t.Fatalf("got=%+v err=%v", got, err)
			}
		})
	}
}
