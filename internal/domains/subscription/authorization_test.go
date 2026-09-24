package subscription

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

func subscriptionScopeGrant(s EventSubscription) ScopeGrant {
	return ScopeGrant{
		PartnerRef:  s.Subscriber.PartnerRef,
		TenantScope: s.TenantScope,
		Purpose:     "payroll_processing",
		EventKinds:  append([]EventKind(nil), s.EventKinds...),
		Resources:   RequiredResources(s),
		Fields:      requiredFields(s),
	}
}

func TestTodo_SUB_002(t *testing.T) {
	draft, err := NewDraft(subscriptionRequest("sub-scope", "requester-1"))
	if err != nil {
		t.Fatal(err)
	}
	grant := subscriptionScopeGrant(draft)
	active, event, err := draft.ActivateWithAuthorization(grant, "requester-2", "approver-1")
	if err != nil {
		t.Fatal(err)
	}
	if active.State != StateActive || event.Digest == "" || !event.Allowed || event.Rule != scopeRule {
		t.Fatalf("active=%+v event=%+v", active, event)
	}
	if decision, err := Authorize(active, grant); err != nil || !decision.Allowed || decision.Explain() == "" {
		t.Fatalf("decision=%+v err=%v", decision, err)
	}
	eventDigest := event.Digest
	grant.Fields[EventWorkerChanged] = []string{"worker.id"}
	if _, _, err := MatchAuthorized(EventDigest{TenantScope: "tenant-a", Kind: EventWorkerChanged, Digest: "event-1", FieldDigests: map[string]string{"worker.status": DigestValue("ACTIVE")}}, []EventSubscription{active}, map[string]ScopeGrant{"sub-scope": grant}); !errors.Is(err, ErrScopeDenied) {
		t.Fatalf("changed entitlement error=%v", err)
	}
	if eventDigest == "" {
		t.Fatal("authorization event was not digested")
	}
}

func TestTodo_SUB_002_Golden(t *testing.T) {
	draft, err := NewDraft(subscriptionRequest("sub-golden-scope", "requester-1"))
	if err != nil {
		t.Fatal(err)
	}
	grant := subscriptionScopeGrant(draft)
	first, err := Authorize(draft, grant)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Authorize(draft, grant)
	if err != nil || first.Digest != second.Digest || first.Event(grant).Digest != second.Event(grant).Digest {
		t.Fatalf("first=%+v second=%+v err=%v", first, second, err)
	}
}

func TestTodo_SUB_002_Race(t *testing.T) {
	draft, err := NewDraft(subscriptionRequest("sub-race-scope", "requester-1"))
	if err != nil {
		t.Fatal(err)
	}
	active, err := draft.Activate("requester-2", "approver-1")
	if err != nil {
		t.Fatal(err)
	}
	grant := subscriptionScopeGrant(active)
	event := EventDigest{TenantScope: "tenant-a", Kind: EventWorkerChanged, Digest: "event-1", FieldDigests: map[string]string{"worker.status": DigestValue("ACTIVE")}}
	const workers = 12
	type result struct {
		matches []SubscriptionMatch
		events  []AuthorizationEvent
		err     error
	}
	results := make(chan result, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			matches, events, err := MatchAuthorized(event, []EventSubscription{active}, map[string]ScopeGrant{active.SubscriptionID: grant})
			results <- result{matches: matches, events: events, err: err}
		}()
	}
	wg.Wait()
	close(results)
	for got := range results {
		if got.err != nil || len(got.matches) != 1 || len(got.events) != 1 || got.events[0].Digest == "" {
			t.Fatalf("concurrent authorization=%+v", got)
		}
		if got.matches[0].SubscriptionID != active.SubscriptionID {
			t.Fatalf("authorized subscription=%q, want %q", got.matches[0].SubscriptionID, active.SubscriptionID)
		}
	}
}

func TestTodo_SUB_002_Integration(t *testing.T) {
	draft, err := NewDraft(subscriptionRequest("sub-integration-scope", "requester-1"))
	if err != nil {
		t.Fatal(err)
	}
	grant := subscriptionScopeGrant(draft)
	grant.Resources = []string{"tenant-a"}
	if _, _, err := draft.ActivateWithAuthorization(grant, "requester-2", "approver-1"); !errors.Is(err, ErrScopeDenied) {
		t.Fatalf("resource scope refusal=%v", err)
	}
}

func TestTodo_SUB_002_Fault(t *testing.T) {
	draft, err := NewDraft(subscriptionRequest("sub-fault-scope", "requester-1"))
	if err != nil {
		t.Fatal(err)
	}
	grant := subscriptionScopeGrant(draft)
	grant.EventKinds = []EventKind{"*"}
	if _, err := Authorize(draft, grant); !errors.Is(err, ErrInvalidScope) {
		t.Fatalf("wildcard scope error=%v", err)
	}
	grant = subscriptionScopeGrant(draft)
	grant.Fields[EventWorkerChanged] = []string{"worker.id"}
	if _, err := Authorize(draft, grant); !errors.Is(err, ErrScopeDenied) || !strings.Contains(err.Error(), "worker.status") {
		t.Fatalf("field scope refusal=%v", err)
	}
}
