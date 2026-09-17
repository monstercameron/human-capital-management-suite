package garnishment

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var garn004At = time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)

func garn004State(t *testing.T) BalanceState {
	t.Helper()
	state, err := OpenBalance("acme", "worker-1", "order:creditor-1", values.MustDecimal("1000.00", 2, values.RoundingHalfUp))
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func garn004Entry(seq uint64, kind LedgerEntryKind, amount string) LedgerEntry {
	return LedgerEntry{
		Tenant: "acme", WorkerRef: "worker-1", OrderRef: "order:creditor-1",
		Seq: seq, Kind: kind, Amount: values.MustDecimal(amount, 2, values.RoundingHalfUp),
		EffectiveAt: garn004At, AuthorityRef: "clerk:payroll-2",
	}
}

// TestTodo_GARN_004 is the PRIMARY contract: payments, changes, releases
// and retro corrections post immutable entries, withholding never exceeds
// the order cap, and the balance explanation is complete.
func TestTodo_GARN_004(t *testing.T) {
	state := garn004State(t)
	state, err := ApplyLedgerEntry(state, garn004Entry(1, LedgerWithholding, "386.75"))
	if err != nil {
		t.Fatalf("ApplyLedgerEntry withholding: %v", err)
	}
	if state.Withheld.String() != "386.75" || state.Arrears.String() != "613.25" {
		t.Fatalf("balance must explain withholding and arrears: %+v", state)
	}
	state, err = ApplyLedgerEntry(state, garn004Entry(2, LedgerRemittance, "386.75"))
	if err != nil {
		t.Fatalf("ApplyLedgerEntry remittance: %v", err)
	}
	if state.Remitted.String() != "386.75" || state.Digest == "" {
		t.Fatalf("remittance must track and seal: %+v", state)
	}

	t.Run("retro correction posts a linked entry, history untouched", func(t *testing.T) {
		state := garn004State(t)
		state, err := ApplyLedgerEntry(state, garn004Entry(1, LedgerWithholding, "400.00"))
		if err != nil {
			t.Fatal(err)
		}
		first := state.Entries[0]
		correction := garn004Entry(2, LedgerRetroCredit, "13.25")
		correction.CorrectsSeq = 1
		state, err = ApplyLedgerEntry(state, correction)
		if err != nil {
			t.Fatal(err)
		}
		if state.Withheld.String() != "386.75" || state.Entries[0] != first {
			t.Fatalf("retro must adjust forward without rewriting history: %+v", state)
		}
		if len(state.Explanation) != 2 {
			t.Fatalf("explanation must cover every entry: %v", state.Explanation)
		}
	})

	t.Run("over-withhold beyond the cap is refused", func(t *testing.T) {
		state := garn004State(t)
		state, err := ApplyLedgerEntry(state, garn004Entry(1, LedgerWithholding, "900.00"))
		if err != nil {
			t.Fatal(err)
		}
		_, err = ApplyLedgerEntry(state, garn004Entry(2, LedgerWithholding, "200.00"))
		var rej *BalanceRejection
		if !errors.As(err, &rej) || !errors.Is(err, ErrBalanceRejected) {
			t.Fatalf("over-cap posting must be GARN_004_REJECTED, got %v", err)
		}
		if rej.Field == "" || rej.State == "" || rej.Version == "" {
			t.Fatalf("rejection must name field/state/version: %+v", rej)
		}
		if len(state.Entries) != 1 {
			t.Fatalf("refusal must leave the ledger untouched")
		}
	})

	t.Run("release and amendment carry no amount and need sequence", func(t *testing.T) {
		state := garn004State(t)
		if _, err := ApplyLedgerEntry(state, garn004Entry(2, LedgerWithholding, "10.00")); !errors.Is(err, ErrBalanceRejected) {
			t.Fatalf("skipped seq must be GARN_004_REJECTED, got %v", err)
		}
		state, err = ApplyLedgerEntry(state, garn004Entry(1, LedgerRelease, "0.00"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ApplyLedgerEntry(state, garn004Entry(2, LedgerRelease, "5.00")); !errors.Is(err, ErrBalanceRejected) {
			t.Fatalf("nonzero lifecycle amount must be GARN_004_REJECTED")
		}
	})
}

func TestTodo_GARN_004_Property(t *testing.T) {
	state := garn004State(t)
	amounts := []string{"100.00", "200.00", "50.00"}
	var seq uint64
	for _, a := range amounts {
		seq++
		var err error
		state, err = ApplyLedgerEntry(state, garn004Entry(seq, LedgerWithholding, a))
		if err != nil {
			t.Fatal(err)
		}
	}
	if state.Withheld.String() != "350.00" || state.Arrears.String() != "650.00" {
		t.Fatalf("withheld must equal the entry sum: %+v", state)
	}
	if len(state.Explanation) != len(state.Entries) {
		t.Fatalf("every entry needs its explanation line")
	}
	// Digests are order-sensitive: the same entries in another order
	// would break the sequence, so no two histories collide silently.
	a, _ := ApplyLedgerEntry(garn004State(t), garn004Entry(1, LedgerWithholding, "100.00"))
	b, _ := ApplyLedgerEntry(garn004State(t), garn004Entry(1, LedgerWithholding, "200.00"))
	if a.Digest == b.Digest {
		t.Fatalf("distinct histories must digest distinctly")
	}
}

func TestTodo_GARN_004_Race(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			state := garn004State(t)
			next, err := ApplyLedgerEntry(state, garn004Entry(1, LedgerWithholding, "100.00"))
			if err != nil {
				t.Error(err)
				return
			}
			if next.Withheld.String() != "100.00" || len(state.Entries) != 0 {
				t.Errorf("concurrent apply diverged or mutated the base: %+v / %+v", next, state)
			}
		}()
	}
	wg.Wait()
}

func TestTodo_GARN_004_Mutation(t *testing.T) {
	state := garn004State(t)
	for name, mutate := range map[string]func(*LedgerEntry){
		"kind":      func(e *LedgerEntry) { e.Kind = "BONUS" },
		"scope":     func(e *LedgerEntry) { e.OrderRef = "order:other" },
		"amount":    func(e *LedgerEntry) { e.Amount = values.MustDecimal("-5.00", 2, values.RoundingHalfUp) },
		"instant":   func(e *LedgerEntry) { e.EffectiveAt = time.Time{} },
		"authority": func(e *LedgerEntry) { e.AuthorityRef = "" },
	} {
		entry := garn004Entry(1, LedgerWithholding, "10.00")
		mutate(&entry)
		if _, err := ApplyLedgerEntry(state, entry); !errors.Is(err, ErrBalanceRejected) {
			t.Fatalf("mutated %s must be GARN_004_REJECTED", name)
		}
	}
	dangling := garn004Entry(1, LedgerRetroCredit, "10.00")
	dangling.CorrectsSeq = 9
	if _, err := ApplyLedgerEntry(state, dangling); !errors.Is(err, ErrBalanceRejected) {
		t.Fatalf("dangling retro link must be GARN_004_REJECTED")
	}
	over := garn004Entry(1, LedgerRemittance, "10.00")
	if _, err := ApplyLedgerEntry(state, over); !errors.Is(err, ErrBalanceRejected) {
		t.Fatalf("remittance beyond withholding must be GARN_004_REJECTED")
	}
}
