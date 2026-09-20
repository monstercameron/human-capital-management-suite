package journeyinvalidation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// TestTodo_REV_091_03_Security proves a viewer without authority over a
// journey never receives its invalidation -- not the item, not a gap in the
// numbering, not a message at all -- while an authorized viewer on the same
// hub receives both transitions.
func TestTodo_REV_091_03_Security(t *testing.T) {
	tenant := values.TenantId("acme")
	hub := newHub()

	// The manager may see only journey B (another worker's journey A is
	// outside their population); the admin may see both.
	manager, err := hub.Subscribe(SubscribeRequest{
		Principal: hubPrincipal(t, tenant, "manager-1", "org:acme:people-ops", "manager"),
		Region:    promotion.RegionPerson,
		Inspector: &fakeInspector{tenant: tenant, visible: map[string]string{journeyB: workerB}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	admin, err := hub.Subscribe(SubscribeRequest{
		Principal: hubPrincipal(t, tenant, "admin", "org:acme:people-ops", "comp_admin"),
		Region:    promotion.RegionPerson,
		Inspector: &fakeInspector{tenant: tenant, visible: map[string]string{journeyA: workerA, journeyB: workerB}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	// A self-service viewer whose engine read would (wrongly) admit journey A
	// is still refused by the authorization check: no role of theirs admits
	// relationship-scoped access to another worker.
	self, err := hub.Subscribe(SubscribeRequest{
		Principal: hubPrincipal(t, tenant, workerB, "", "worker_self"),
		Region:    promotion.RegionPerson,
		Inspector: &fakeInspector{tenant: tenant, visible: map[string]string{journeyA: workerA}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer self.Close()
	// A viewer in another tenant never sees this tenant's records at all.
	otherTenant := values.TenantId("globex")
	otherInspector := &fakeInspector{tenant: otherTenant, visible: map[string]string{journeyA: workerA}}
	other, err := hub.Subscribe(SubscribeRequest{
		Principal: hubPrincipal(t, otherTenant, "admin", "", "comp_admin"),
		Region:    promotion.RegionPerson, Inspector: otherInspector,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()

	hub.Publish(Committed{Tenant: tenant, IntentID: journeyA, Revision: 1})
	hub.Publish(Committed{Tenant: tenant, IntentID: journeyB, Revision: 2})

	raw, err := nextWithin(t, manager)
	if err != nil {
		t.Fatalf("manager Next: %v", err)
	}
	got := decodeMessage(t, raw)
	if got.SourceSequence != 1 || got.Watermark != 0 {
		t.Fatalf("manager sequence = %d/%d; a gap would disclose the filtered transition", got.SourceSequence, got.Watermark)
	}
	if len(got.Items) != 1 || got.Items[0].Subject.Id != workerB {
		t.Fatalf("manager items = %+v, want only worker B", got.Items)
	}
	if _, err := nextWithin(t, manager); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("manager received a second message (%v); worker A's invalidation leaked", err)
	}

	for _, want := range []struct {
		seq    uint64
		worker string
	}{{1, workerA}, {2, workerB}} {
		raw, err := nextWithin(t, admin)
		if err != nil {
			t.Fatalf("admin Next: %v", err)
		}
		if message := decodeMessage(t, raw); message.SourceSequence != want.seq || message.Items[0].Subject.Id != want.worker {
			t.Fatalf("admin message = %d/%s, want %d/%s", message.SourceSequence, message.Items[0].Subject.Id, want.seq, want.worker)
		}
	}

	if raw, err := nextWithin(t, self); !errors.Is(err, context.DeadlineExceeded) || raw != nil {
		t.Fatalf("self-service viewer received %q (%v)", raw, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if raw, err := other.Next(ctx); !errors.Is(err, context.DeadlineExceeded) || raw != nil {
		t.Fatalf("other tenant received %q (%v)", raw, err)
	}
	if otherInspector.calls != 0 {
		t.Fatalf("other tenant's subscription was even asked about this tenant's journey (%d calls)", otherInspector.calls)
	}
}
