package garnishment

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func garn002Order(t *testing.T, id string, orderType OrderType, priority int, jurisdiction string) AttachmentOrder {
	t.Helper()
	input := orderInput()
	input.OrderID = id
	input.OrderType = orderType
	input.Priority = priority
	input.Jurisdiction = jurisdiction
	input.EffectiveFrom = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	input.ReceivedAt = time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC)
	input.ClaimedBalance = values.MustDecimal("500.00", 2, values.RoundingHalfUp)
	order, err := IntakeOrder(input)
	if err != nil {
		t.Fatal(err)
	}
	order, err = order.Verify(orderEvidence())
	if err != nil {
		t.Fatal(err)
	}
	order, err = order.Activate("clerk:payroll-2")
	if err != nil {
		t.Fatal(err)
	}
	return order
}

func garn002Pack(t *testing.T) PriorityRulePack {
	t.Helper()
	pack, err := NewPriorityRulePack("rulepack:garnish-priority", "v2026.01", []OrderType{
		OrderChildSupport, OrderTaxLevy, OrderStudentLoan, OrderCreditor,
	})
	if err != nil {
		t.Fatal(err)
	}
	return pack
}

// TestTodo_GARN_002 is the PRIMARY contract: exact legal/rule pack and
// competing orders determine sequence and concurrency, while ambiguous
// jurisdiction, order type or priority returns review.
func TestTodo_GARN_002(t *testing.T) {
	pack := garn002Pack(t)
	orders := []AttachmentOrder{
		garn002Order(t, "order:creditor-1", OrderCreditor, 4, "US-CA"),
		garn002Order(t, "order:child-support-1", OrderChildSupport, 1, "US-CA"),
		garn002Order(t, "order:tax-levy-1", OrderTaxLevy, 2, "US-CA"),
	}
	composition, err := ComposePriority("acme", "worker-1", pack, orders)
	if err != nil {
		t.Fatalf("ComposePriority: %v", err)
	}
	if len(composition.Sequence) != 3 {
		t.Fatalf("every competing order must be sequenced: %+v", composition)
	}
	// Child support outranks the tax levy, which outranks the creditor:
	// bands run in sequence, so ranks and run groups ascend together.
	want := []string{"order:child-support-1", "order:tax-levy-1", "order:creditor-1"}
	for i, id := range want {
		placed := composition.Sequence[i]
		if placed.OrderID != id || placed.Rank != i+1 || placed.RunGroup != i+1 {
			t.Fatalf("sequence[%d] = %+v, want %s rank %d group %d", i, placed, id, i+1, i+1)
		}
	}
	if composition.RulePackDigest != pack.CanonicalDigest {
		t.Fatalf("composition must bind the exact rule pack: %+v", composition)
	}
	if err := composition.Verify(); err != nil {
		t.Fatalf("sealed composition Verify: %v", err)
	}

	t.Run("same-type orders share a concurrent group", func(t *testing.T) {
		second := garn002Order(t, "order:child-support-2", OrderChildSupport, 5, "US-CA")
		composition, err := ComposePriority("acme", "worker-1", pack, []AttachmentOrder{orders[1], second})
		if err != nil {
			t.Fatalf("ComposePriority: %v", err)
		}
		if len(composition.Sequence) != 2 ||
			composition.Sequence[0].RunGroup != composition.Sequence[1].RunGroup {
			t.Fatalf("same-type orders must withhold concurrently: %+v", composition)
		}
		if composition.Sequence[0].Rank == composition.Sequence[1].Rank {
			t.Fatalf("ranks must still be distinct: %+v", composition)
		}
	})

	t.Run("ambiguous priority returns review", func(t *testing.T) {
		dupe := garn002Order(t, "order:tax-levy-2", OrderTaxLevy, 2, "US-CA")
		_, err := ComposePriority("acme", "worker-1", pack, []AttachmentOrder{orders[2], dupe})
		var review *CompositionReview
		if !errors.As(err, &review) {
			t.Fatalf("expected *CompositionReview, got %v", err)
		}
		if !errors.Is(err, ErrCompositionReview) {
			t.Fatalf("expected GARN_002_REVIEW_REQUIRED, got %v", err)
		}
		if review.Field == "" || review.State == "" || review.Version == "" {
			t.Fatalf("review must name field/state/version: %+v", review)
		}
	})

	t.Run("ambiguous jurisdiction returns review", func(t *testing.T) {
		order := garn002Order(t, "order:tax-levy-3", OrderTaxLevy, 7, "US-CA")
		order.Jurisdiction = ""
		if _, err := ComposePriority("acme", "worker-1", pack, []AttachmentOrder{order}); !errors.Is(err, ErrCompositionReview) {
			t.Fatalf("blank jurisdiction must return review, got %v", err)
		}
	})

	t.Run("undeclared order type returns review", func(t *testing.T) {
		order := garn002Order(t, "order:mystery-1", OrderTaxLevy, 8, "US-CA")
		order.OrderType = "WAGE_ASSIGNMENT_UNDECLARED"
		if _, err := ComposePriority("acme", "worker-1", pack, []AttachmentOrder{order}); !errors.Is(err, ErrCompositionReview) {
			t.Fatalf("undeclared type must return review, got %v", err)
		}
	})
}

func TestTodo_GARN_002_Race(t *testing.T) {
	pack := garn002Pack(t)
	orders := []AttachmentOrder{
		garn002Order(t, "order:child-support-1", OrderChildSupport, 1, "US-CA"),
		garn002Order(t, "order:tax-levy-1", OrderTaxLevy, 2, "US-CA"),
	}
	const racers = 16
	digests := make([]string, racers)
	errs := make([]error, racers)
	var wg sync.WaitGroup
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			composition, err := ComposePriority("acme", "worker-1", pack, orders)
			if err != nil {
				errs[i] = err
				return
			}
			digests[i] = composition.CanonicalDigest
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("racer %d: %v", i, err)
		}
		if digests[i] != digests[0] || digests[0] == "" {
			t.Fatalf("concurrent compositions diverged: %v", digests)
		}
	}
}

func TestTodo_GARN_002_Fault(t *testing.T) {
	t.Run("unsealed rule pack is refused", func(t *testing.T) {
		pack := garn002Pack(t)
		pack.CanonicalDigest = "sha256:tampered"
		orders := []AttachmentOrder{garn002Order(t, "order:tax-levy-1", OrderTaxLevy, 2, "US-CA")}
		if _, err := ComposePriority("acme", "worker-1", pack, orders); !errors.Is(err, ErrCompositionReview) {
			t.Fatalf("unsealed rule pack must return review, got %v", err)
		}
	})

	t.Run("inactive order is refused", func(t *testing.T) {
		pack := garn002Pack(t)
		order, err := IntakeOrder(orderInput())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ComposePriority("acme", "worker-1", pack, []AttachmentOrder{order}); !errors.Is(err, ErrCompositionReview) {
			t.Fatalf("pending order must return review, got %v", err)
		}
	})

	t.Run("stale superseded version is refused", func(t *testing.T) {
		pack := garn002Pack(t)
		order := garn002Order(t, "order:tax-levy-9", OrderTaxLevy, 2, "US-CA")
		order.Status = OrderSuperseded
		if _, err := ComposePriority("acme", "worker-1", pack, []AttachmentOrder{order}); !errors.Is(err, ErrCompositionReview) {
			t.Fatalf("superseded order must return review, got %v", err)
		}
	})
}

func TestTodo_GARN_002_Security(t *testing.T) {
	pack := garn002Pack(t)
	t.Run("cross-tenant order is denied without effect", func(t *testing.T) {
		foreign := garn002Order(t, "order:tax-levy-1", OrderTaxLevy, 2, "US-CA")
		foreign.TenantID = "other-tenant"
		composition, err := ComposePriority("acme", "worker-1", pack, []AttachmentOrder{foreign})
		if !errors.Is(err, ErrCompositionReview) {
			t.Fatalf("cross-tenant order must be denied, got %v", err)
		}
		if composition.CanonicalDigest != "" || len(composition.Sequence) != 0 {
			t.Fatalf("denied composition must carry zero state: %+v", composition)
		}
	})

	t.Run("other worker's order is denied without effect", func(t *testing.T) {
		other := garn002Order(t, "order:tax-levy-1", OrderTaxLevy, 2, "US-CA")
		other.PersonRef = "worker-9"
		if _, err := ComposePriority("acme", "worker-1", pack, []AttachmentOrder{other}); !errors.Is(err, ErrCompositionReview) {
			t.Fatalf("other worker's order must be denied, got %v", err)
		}
	})
}

func TestTodo_GARN_002_Mutation(t *testing.T) {
	pack := garn002Pack(t)
	orders := []AttachmentOrder{
		garn002Order(t, "order:child-support-1", OrderChildSupport, 1, "US-CA"),
		garn002Order(t, "order:tax-levy-1", OrderTaxLevy, 2, "US-CA"),
	}
	composition, err := ComposePriority("acme", "worker-1", pack, orders)
	if err != nil {
		t.Fatal(err)
	}
	swapped := composition
	swapped.Sequence = append([]PlacedOrder(nil), composition.Sequence...)
	swapped.Sequence[0], swapped.Sequence[1] = swapped.Sequence[1], swapped.Sequence[0]
	if err := swapped.Verify(); !errors.Is(err, ErrCompositionReview) {
		t.Fatalf("reordered mutant must fail Verify, got %v", err)
	}
	regrouped := composition
	regrouped.Sequence = append([]PlacedOrder(nil), composition.Sequence...)
	regrouped.Sequence[1].RunGroup = regrouped.Sequence[0].RunGroup
	if err := regrouped.Verify(); !errors.Is(err, ErrCompositionReview) {
		t.Fatalf("regrouped mutant must fail Verify, got %v", err)
	}
}
