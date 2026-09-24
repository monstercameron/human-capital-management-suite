package delivery

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/idempotency"
)

type fakeProvider struct {
	mu    sync.Mutex
	calls []Delivery
	err   error
}

func (p *fakeProvider) Send(_ context.Context, d Delivery) (ProviderResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, d)
	if p.err != nil {
		return ProviderResult{}, p.err
	}
	return ProviderResult{Reference: "provider-1", Accepted: true}, nil
}

func testIntent() Intent {
	return Intent{TenantID: "tenant-1", IntentID: "intent-1", RecipientRef: "principal-1", Purpose: "APPROVAL_REQUIRED", Subject: "Approval needed", Body: "Open the secure task", Classification: "INTERNAL", IdempotencyKey: "msg-1", CanonicalRequest: []byte(`{"purpose":"APPROVAL_REQUIRED","recipient":"principal-1"}`), Committed: true}
}

func TestTodo_MSG_006(t *testing.T) {
	p := &fakeProvider{}
	d, err := NewDispatcher(p, Policy{ProviderMaximumClassification: "INTERNAL"})
	if err != nil {
		t.Fatal(err)
	}
	r, err := d.Dispatch(context.Background(), testIntent())
	if err != nil {
		t.Fatal(err)
	}
	if r.Attempt.State != AcceptedByProvider || len(p.calls) != 1 {
		t.Fatalf("dispatch failed: %#v calls=%d", r, len(p.calls))
	}
}

type sharedLifecycleStore struct{ registry *idempotency.Registry }

func (s sharedLifecycleStore) Reserve(req idempotency.Request) (idempotency.Resolution, error) {
	return s.registry.Reserve(req)
}
func (s sharedLifecycleStore) Complete(identity idempotency.Identity, digest, result, effect string, now time.Time) (idempotency.Record, error) {
	return s.registry.Complete(identity, digest, result, effect, now)
}
func (s sharedLifecycleStore) Lookup(identity idempotency.Identity) (idempotency.Record, error) {
	return s.registry.Lookup(identity)
}

func TestTodo_REV_060_01(t *testing.T) {
	if _, err := NewDispatcherWithRegistry(&fakeProvider{}, Policy{}, nil); err == nil {
		t.Fatal("durable dispatcher accepted a nil idempotency registry")
	}

	store := sharedLifecycleStore{registry: idempotency.NewRegistry()}
	firstProvider := &fakeProvider{}
	first, err := NewDispatcherWithRegistry(firstProvider, Policy{}, idempotency.NewRegistryWithStore(store))
	if err != nil {
		t.Fatal(err)
	}
	in := testIntent()
	in.CreatedAt = time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	initial, err := first.Dispatch(context.Background(), in)
	if err != nil || initial.Attempt.State != AcceptedByProvider {
		t.Fatalf("initial dispatch = %+v, %v", initial, err)
	}

	// A new dispatcher has no in-process attempt map. A second registry object
	// must recover the completed lifecycle record without calling its provider.
	secondProvider := &fakeProvider{}
	second, err := NewDispatcherWithRegistry(secondProvider, Policy{}, idempotency.NewRegistryWithStore(store))
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := second.Dispatch(context.Background(), in)
	if err != nil || recovered.Attempt.State != AcceptedByProvider || recovered.Attempt.ID != initial.Attempt.ID || recovered.Attempt.ProviderRef != initial.Attempt.ProviderRef {
		t.Fatalf("recovered dispatch = %+v, %v; initial=%+v", recovered, err, initial)
	}
	if len(firstProvider.calls) != 1 || len(secondProvider.calls) != 0 {
		t.Fatalf("provider calls after restart = (%d,%d), want (1,0)", len(firstProvider.calls), len(secondProvider.calls))
	}
}

type blockingProvider struct {
	calls   atomic.Int32
	entered chan struct{}
	resume  chan struct{}
}

func (p *blockingProvider) Send(context.Context, Delivery) (ProviderResult, error) {
	p.calls.Add(1)
	close(p.entered)
	<-p.resume
	return ProviderResult{Reference: "provider-blocked-1", Accepted: true}, nil
}

func TestTodo_REV_060_01_Fault(t *testing.T) {
	store := sharedLifecycleStore{registry: idempotency.NewRegistry()}
	provider := &blockingProvider{entered: make(chan struct{}), resume: make(chan struct{})}
	first, err := NewDispatcherWithRegistry(provider, Policy{}, idempotency.NewRegistryWithStore(store))
	if err != nil {
		t.Fatal(err)
	}
	in := testIntent()
	in.CreatedAt = time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	firstDone := make(chan error, 1)
	go func() {
		_, dispatchErr := first.Dispatch(context.Background(), in)
		firstDone <- dispatchErr
	}()
	<-provider.entered

	secondProvider := &fakeProvider{}
	second, err := NewDispatcherWithRegistry(secondProvider, Policy{}, idempotency.NewRegistryWithStore(store))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.Dispatch(context.Background(), in); !errors.Is(err, ErrIdempotencyInProgress) {
		t.Fatalf("concurrent restarted dispatcher error = %v, want ErrIdempotencyInProgress", err)
	}
	if len(secondProvider.calls) != 0 || provider.calls.Load() != 1 {
		t.Fatalf("provider calls = first:%d second:%d, want 1 and 0", provider.calls.Load(), len(secondProvider.calls))
	}
	close(provider.resume)
	if err := <-firstDone; err != nil {
		t.Fatalf("initial dispatch completion = %v", err)
	}
}

func TestTodo_MSG_006_Race(t *testing.T) {
	p := &fakeProvider{}
	d, _ := NewDispatcher(p, Policy{ProviderMaximumClassification: "INTERNAL"})
	in := testIntent()
	var wg sync.WaitGroup
	results := make(chan DispatchResult, 10)
	errs := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); r, err := d.Dispatch(context.Background(), in); results <- r; errs <- err }()
	}
	wg.Wait()
	close(results)
	close(errs)
	var id string
	for r := range results {
		if id == "" {
			id = r.Attempt.ID
		}
		if r.Attempt.ID != id {
			t.Fatal("logical attempt changed")
		}
	}
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(p.calls) != 1 {
		t.Fatalf("provider calls=%d", len(p.calls))
	}
}

func TestTodo_MSG_006_Integration(t *testing.T) {
	p := &fakeProvider{}
	d, err := NewDispatcher(p, Policy{ProviderMaximumClassification: "INTERNAL"})
	if err != nil {
		t.Fatal(err)
	}
	in := testIntent()
	r, err := d.Dispatch(context.Background(), in)
	if err != nil {
		t.Fatalf("dispatch committed intent: %v", err)
	}
	if r.Attempt.State != AcceptedByProvider || r.Attempt.ProviderRef != "provider-1" || len(p.calls) != 1 {
		t.Fatalf("integration delivery receipt=%+v provider calls=%d", r, len(p.calls))
	}
	if p.calls[0].TenantID != in.TenantID || p.calls[0].IdempotencyKey != in.IdempotencyKey {
		t.Fatalf("provider request lost tenant/idempotency binding: %+v", p.calls[0])
	}
}

func TestTodo_MSG_006_Fault(t *testing.T) {
	p := &fakeProvider{err: errors.New("provider offline")}
	d, _ := NewDispatcher(p, Policy{ProviderMaximumClassification: "INTERNAL"})
	in := testIntent()
	r, err := d.Dispatch(context.Background(), in)
	if !errors.Is(err, ErrProviderFailed) || r.Attempt.State != Failed {
		t.Fatalf("fault not recorded: %#v err=%v", r, err)
	}
	_, err = d.Dispatch(context.Background(), in)
	if !errors.Is(err, ErrProviderFailed) || len(p.calls) != 1 {
		t.Fatalf("failed retry duplicated provider call: %v calls=%d", err, len(p.calls))
	}
	in.Committed = false
	p2 := &fakeProvider{}
	d2, _ := NewDispatcher(p2, Policy{})
	if _, err := d2.Dispatch(context.Background(), in); !errors.Is(err, ErrNotCommitted) || len(p2.calls) != 0 {
		t.Fatalf("uncommitted intent reached provider: %v", err)
	}
}

func TestTodo_MSG_006_Mutation(t *testing.T) {
	p := &fakeProvider{}
	d, _ := NewDispatcher(p, Policy{})
	in := testIntent()
	if _, err := d.Dispatch(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	in.CanonicalRequest = []byte(`{"purpose":"different"}`)
	if _, err := d.Dispatch(context.Background(), in); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("got %v", err)
	}
}

func TestTodo_MSG_006_Security(t *testing.T) {
	p := &fakeProvider{}
	d, _ := NewDispatcher(p, Policy{ProviderMaximumClassification: "INTERNAL", AttentionOnlyClassifications: map[string]bool{"RESTRICTED": true}})
	in := testIntent()
	in.Classification = "RESTRICTED"
	in.Body = "salary=secret"
	r, err := d.Dispatch(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Attempt.AttentionOnly || p.calls[0].Body != "" || !p.calls[0].AttentionOnly {
		t.Fatalf("protected body bypassed DLP: %#v", p.calls[0])
	}
}
