package promotion_test

// TestTodo_PROMOUX_011_Performance proves the delivery cost SubscriberSequencer
// imposes is bounded by counted work, not by a wall-clock budget: this
// machine's timing-based tests are unreliable under load, so this test
// counts exactly how many messages are delivered and exactly how much
// per-subscriber state accumulates, rather than measuring elapsed time.
import (
	"fmt"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/productquery"
)

func TestTodo_PROMOUX_011_Performance(t *testing.T) {
	tenant := values.TenantId("acme")
	otherTenant := values.TenantId("other-tenant")
	viewer := promoux011Principal(t, tenant)
	sequencer := promotion.NewSubscriberSequencer()
	const subscriberKey = "session:promoux011-performance"

	const transitionCount = 500
	regions := promotion.Regions()

	delivered := 0
	authorizedTransitions := 0
	for i := 0; i < transitionCount; i++ {
		authorized := i%2 == 0
		var target productquery.InvalidationTarget
		if authorized {
			authorizedTransitions++
			subject := promoux011Subject(tenant, uuidFromCounter(i))
			target = productquery.InvalidationTarget{Subject: subject, Revision: uint64(i + 1), Decision: promoux011Decision(t, viewer, subject)}
		} else {
			// A denied transition never reaches the decision comparison
			// (EmitInvalidation drops a cross-tenant target on the tenant
			// check alone), so no Decision needs to be computed for it.
			subject := promoux011Subject(otherTenant, uuidFromCounter(i))
			target = productquery.InvalidationTarget{Subject: subject, Revision: uint64(i + 1)}
		}
		req := promoux011Request(viewer, uint64(i+1), target)
		for _, region := range regions {
			_, ok, err := sequencer.EmitForSubscriber(subscriberKey, region, req)
			if err != nil {
				t.Fatalf("transition %d region %q: %v", i, region, err)
			}
			if ok {
				delivered++
			}
		}
	}

	wantDelivered := authorizedTransitions * len(regions)
	if delivered != wantDelivered {
		t.Fatalf("delivered = %d, want exactly %d (one message per authorized transition per region, no more)", delivered, wantDelivered)
	}

	// The bounded-work claim: per-subscriber state is one counter per
	// (subscriber, region/projection) pair, however many thousands of
	// transitions were processed -- it must never grow with transitionCount.
	snapshot := sequencer.Snapshot()
	if len(snapshot) != len(regions) {
		t.Fatalf("subscriber sequencer state size = %d entries, want exactly %d (one per region, independent of transition volume)", len(snapshot), len(regions))
	}
	for _, region := range regions {
		projection, err := region.Projection()
		if err != nil {
			t.Fatal(err)
		}
		got := snapshot[subscriberKey+"\x00"+projection]
		if got != uint64(authorizedTransitions) {
			t.Fatalf("region %q final local sequence = %d, want %d (exactly one per authorized transition, contiguous)", region, got, authorizedTransitions)
		}
	}
}

// uuidFromCounter produces a deterministic, distinct, well-formed
// (canonical lowercase, version 4 variant 8) UUID string for each iteration
// without pulling in a random source: values.EntityId requires an opaque id
// shaped like a UUID or ULID, and this test needs transitionCount of them to
// be pairwise distinct.
func uuidFromCounter(i int) string {
	return fmt.Sprintf("00000000-0000-4000-8000-%012x", i)
}
