package benefits

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func ben005Input() ElectionInput {
	return ElectionInput{
		Tenant: "acme", WorkerRef: "worker-1", PlanRef: "plan-hd-2026",
		PlanRevision: "rev-3", RateVersion: "rates-2026.01", WindowID: "oe-2026",
		Tier: TierFamily, Dependents: []string{"dep-spouse", "dep-child-1"},
		EffectiveDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		EmployeeCost:  values.MustDecimal("240.00", 2, values.RoundingHalfUp),
		EmployerCost:  values.MustDecimal("760.00", 2, values.RoundingHalfUp),
	}
}

// TestTodo_BEN_005 is the PRIMARY contract: an election binds plan, tier,
// dependents, effective date, cost and waiver evidence under a digest and
// never overwrites its predecessor; stale or changed replays fail.
func TestTodo_BEN_005(t *testing.T) {
	genesis, err := RecordElection(nil, ben005Input())
	if err != nil {
		t.Fatalf("RecordElection genesis: %v", err)
	}
	if genesis.Revision != 1 || genesis.CanonicalDigest == "" {
		t.Fatalf("genesis must seal revision 1: %+v", genesis)
	}
	if err := genesis.Verify(); err != nil {
		t.Fatalf("genesis Verify: %v", err)
	}

	next := ben005Input()
	next.PriorDigest = genesis.CanonicalDigest
	next.Tier = TierEmployeeChildren
	next.Dependents = []string{"dep-child-1"}
	successor, err := RecordElection(&genesis, next)
	if err != nil {
		t.Fatalf("RecordElection successor: %v", err)
	}
	if successor.Revision != 2 || successor.PriorDigest != genesis.CanonicalDigest {
		t.Fatalf("successor must chain the head: %+v", successor)
	}
	if successor.CanonicalDigest == genesis.CanonicalDigest {
		t.Fatalf("changed election must seal a new digest")
	}
	// The predecessor is immutable: recording a successor changes nothing.
	if genesis.Revision != 1 || genesis.Tier != TierFamily {
		t.Fatalf("predecessor mutated: %+v", genesis)
	}

	t.Run("stale window/plan/rate/dependent replay fails", func(t *testing.T) {
		stale := ben005Input()
		stale.PriorDigest = "sha256:stale-head"
		if _, err := RecordElection(&genesis, stale); !errors.Is(err, ErrElectionRejected) {
			t.Fatalf("stale prior digest must be BEN_005_REJECTED, got %v", err)
		}
		var rej *ElectionRejection
		if _, err := RecordElection(&genesis, stale); !errors.As(err, &rej) || rej.Field == "" || rej.State == "" || rej.Version == "" {
			t.Fatalf("rejection must name field/state/version: %v", err)
		}
	})

	t.Run("changed replay against the same head fails", func(t *testing.T) {
		changed := ben005Input()
		changed.PriorDigest = genesis.CanonicalDigest
		changed.RateVersion = "rates-2026.02"
		if _, err := RecordElection(&successor, changed); !errors.Is(err, ErrElectionRejected) {
			t.Fatalf("changed replay must be BEN_005_REJECTED, got %v", err)
		}
	})

	t.Run("waiver without evidence fails", func(t *testing.T) {
		waiver := ben005Input()
		waiver.Waiver = true
		if _, err := RecordElection(nil, waiver); !errors.Is(err, ErrElectionRejected) {
			t.Fatalf("waiver without evidence must be BEN_005_REJECTED, got %v", err)
		}
		waiver.WaiverEvidence = "signed-waiver:2026-001"
		if _, err := RecordElection(nil, waiver); err != nil {
			t.Fatalf("evidenced waiver must record: %v", err)
		}
	})
}

func TestTodo_BEN_005_Property(t *testing.T) {
	mk := func() ElectionInput { return ben005Input() }
	a, err := RecordElection(nil, mk())
	if err != nil {
		t.Fatal(err)
	}
	b, err := RecordElection(nil, mk())
	if err != nil {
		t.Fatal(err)
	}
	if a.CanonicalDigest != b.CanonicalDigest {
		t.Fatalf("identical elections must digest identically")
	}
	// Dependent order is not semantic.
	shuffled := mk()
	shuffled.Dependents = []string{"dep-child-1", "dep-spouse"}
	c, err := RecordElection(nil, shuffled)
	if err != nil {
		t.Fatal(err)
	}
	if c.CanonicalDigest != a.CanonicalDigest {
		t.Fatalf("dependent order must not change the digest")
	}
	// Every bound field moves the digest.
	changed := mk()
	changed.Tier = TierEmployeeOnly
	changed.Dependents = nil
	d, err := RecordElection(nil, changed)
	if err != nil {
		t.Fatal(err)
	}
	if d.CanonicalDigest == a.CanonicalDigest {
		t.Fatalf("tier change must move the digest")
	}
	// Revision strictly increments across a chain of five.
	head := a
	for want := uint64(2); want <= 5; want++ {
		in := mk()
		in.PriorDigest = head.CanonicalDigest
		head, err = RecordElection(&head, in)
		if err != nil {
			t.Fatal(err)
		}
		if head.Revision != want {
			t.Fatalf("revision = %d, want %d", head.Revision, want)
		}
	}
}

func TestTodo_BEN_005_Recovery(t *testing.T) {
	var history []ElectionRevision
	var head *ElectionRevision
	for i := 0; i < 3; i++ {
		in := ben005Input()
		if head != nil {
			in.PriorDigest = head.CanonicalDigest
		}
		next, err := RecordElection(head, in)
		if err != nil {
			t.Fatal(err)
		}
		history = append(history, next)
		head = &next
	}
	rebuilt, err := ReplayElections(history)
	if err != nil {
		t.Fatalf("ReplayElections: %v", err)
	}
	if rebuilt.CanonicalDigest != head.CanonicalDigest || rebuilt.Revision != 3 {
		t.Fatalf("replay must rebuild the head: %+v", rebuilt)
	}
	broken := append([]ElectionRevision(nil), history...)
	broken[2].Tier = TierEmployeeOnly
	if _, err := ReplayElections(broken); !errors.Is(err, ErrElectionRejected) {
		t.Fatalf("tampered history must be BEN_005_REJECTED, got %v", err)
	}
	if _, err := ReplayElections(nil); !errors.Is(err, ErrElectionRejected) {
		t.Fatalf("empty history must be BEN_005_REJECTED, got %v", err)
	}
}

func TestTodo_BEN_005_Mutation(t *testing.T) {
	genesis, err := RecordElection(nil, ben005Input())
	if err != nil {
		t.Fatal(err)
	}
	mutated := genesis
	mutated.Tier = TierEmployeeOnly
	if err := mutated.Verify(); !errors.Is(err, ErrElectionRejected) {
		t.Fatalf("tier mutation must break the seal, got %v", err)
	}
	mutated = genesis
	mutated.EmployeeCost = values.MustDecimal("0.01", 2, values.RoundingHalfUp)
	if err := mutated.Verify(); !errors.Is(err, ErrElectionRejected) {
		t.Fatalf("cost mutation must break the seal, got %v", err)
	}
	for field, mutate := range map[string]func(*ElectionInput){
		"worker": func(in *ElectionInput) { in.WorkerRef = "" },
		"plan":   func(in *ElectionInput) { in.PlanRevision = "" },
		"rate":   func(in *ElectionInput) { in.RateVersion = "" },
		"window": func(in *ElectionInput) { in.WindowID = "" },
		"tier":   func(in *ElectionInput) { in.Tier = "GOLD_PLUS" },
	} {
		in := ben005Input()
		mutate(&in)
		if _, err := RecordElection(nil, in); !errors.Is(err, ErrElectionRejected) {
			t.Fatalf("missing %s must be BEN_005_REJECTED", field)
		}
	}
	neg := ben005Input()
	neg.EmployeeCost = values.MustDecimal("-1.00", 2, values.RoundingHalfUp)
	if _, err := RecordElection(nil, neg); !errors.Is(err, ErrElectionRejected) {
		t.Fatalf("negative cost must be BEN_005_REJECTED, got %v", err)
	}
	_ = time.Now
}
