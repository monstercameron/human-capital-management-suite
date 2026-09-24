package settlement

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var settle006At = time.Date(2026, 3, 5, 9, 0, 0, 0, time.UTC)

func settle006Intent() CorrectiveIntent {
	return CorrectiveIntent{
		Tenant: "acme", OriginalInstruction: "instruction-1",
		OriginalAmount: values.MustDecimal("1250.00", 2, values.RoundingHalfUp),
		OriginalState:  EvidenceSettled, Kind: CorrectiveReturn,
		Reason: "recipient bank returned funds", EvidenceRef: "rail-evidence:ret-991",
		AuthorityRef: "treasurer:ops-3", IdempotencyKey: "idem-ret-001", OccurredAt: settle006At,
	}
}

// TestTodo_SETTLE_006 is the PRIMARY contract: a governed corrective
// intent restores payable, balance, funding and reporting without deleting
// the original payment, and communicates without leaking bank data.
func TestTodo_SETTLE_006(t *testing.T) {
	got, err := ApplyCorrectiveIntent(settle006Intent())
	if err != nil {
		t.Fatalf("ApplyCorrectiveIntent: %v", err)
	}
	for _, d := range []values.Decimal{got.PayableDelta, got.BalanceDelta, got.FundingDelta, got.ReportingDelta} {
		if d.String() != "1250.00" {
			t.Fatalf("every delta must restore the original amount: %+v", got)
		}
	}
	if got.InstructionRef != "instruction-1" || got.Digest == "" {
		t.Fatalf("result must bind the original instruction and seal: %+v", got)
	}
	if containsBankPayload(got.Communication) {
		t.Fatalf("communication must not leak bank data: %q", got.Communication)
	}

	t.Run("only settled originals correct", func(t *testing.T) {
		for _, state := range []EvidenceState{EvidenceAccepted, EvidencePending, EvidenceRejected, EvidenceReturned, EvidenceUnknown} {
			in := settle006Intent()
			in.OriginalState = state
			if _, err := ApplyCorrectiveIntent(in); !errors.Is(err, ErrCorrectiveRejected) {
				t.Fatalf("state %s must be SETTLE_006_REJECTED", state)
			}
		}
	})

	t.Run("reversal restores the same deltas", func(t *testing.T) {
		in := settle006Intent()
		in.Kind = CorrectiveReversal
		in.IdempotencyKey = "idem-rev-001"
		got, err := ApplyCorrectiveIntent(in)
		if err != nil {
			t.Fatal(err)
		}
		if got.Kind != CorrectiveReversal || got.PayableDelta.String() != "1250.00" {
			t.Fatalf("reversal must restore the same deltas: %+v", got)
		}
	})

	t.Run("bank payload in input is refused", func(t *testing.T) {
		in := settle006Intent()
		in.EvidenceRef = "rail-evidence acct:123456"
		if _, err := ApplyCorrectiveIntent(in); !errors.Is(err, ErrCorrectiveRejected) {
			t.Fatalf("bank payload must be SETTLE_006_REJECTED, got %v", err)
		}
	})
}

func TestTodo_SETTLE_006_Property(t *testing.T) {
	a, err := ApplyCorrectiveIntent(settle006Intent())
	if err != nil {
		t.Fatal(err)
	}
	b, err := ApplyCorrectiveIntent(settle006Intent())
	if err != nil {
		t.Fatal(err)
	}
	if a.Digest != b.Digest {
		t.Fatalf("idempotent replays must digest identically")
	}
	// Kind is the only intentional digest mover among equal amounts.
	rev := settle006Intent()
	rev.Kind = CorrectiveReversal
	rev.IdempotencyKey = "idem-rev-002"
	c, err := ApplyCorrectiveIntent(rev)
	if err != nil {
		t.Fatal(err)
	}
	if c.Digest == a.Digest {
		t.Fatalf("return and reversal must seal distinct digests")
	}
}

func TestTodo_SETTLE_006_Golden(t *testing.T) {
	got, err := ApplyCorrectiveIntent(settle006Intent())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("testdata", "settle_006_golden.json")
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read required golden file %s: %v", path, err)
	}
	if string(want) != string(raw)+"\n" {
		t.Fatalf("golden mismatch:\n got %s\nwant %s", raw, want)
	}
}

func TestTodo_SETTLE_006_Race(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := ApplyCorrectiveIntent(settle006Intent())
			if err != nil {
				t.Error(err)
				return
			}
			if got.PayableDelta.String() != "1250.00" {
				t.Errorf("concurrent corrective diverged: %+v", got)
			}
		}()
	}
	wg.Wait()
}

func TestTodo_SETTLE_006_Integration(t *testing.T) {
	// The corrective result replays through the observation vocabulary:
	// a RETURN observation for the same rail evidence stays RETURN and
	// is never reclassified as settlement.
	got, err := ApplyCorrectiveIntent(settle006Intent())
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != CorrectiveReturn {
		t.Fatalf("integration must preserve the corrective kind: %+v", got)
	}
	if got.Communication == "" || got.IdempotencyKey != "idem-ret-001" {
		t.Fatalf("integration must preserve idempotency and communication: %+v", got)
	}
}

func TestTodo_SETTLE_006_Fault(t *testing.T) {
	for name, mutate := range map[string]func(*CorrectiveIntent){
		"instruction": func(in *CorrectiveIntent) { in.OriginalInstruction = "" },
		"kind":        func(in *CorrectiveIntent) { in.Kind = "REFUND" },
		"reason":      func(in *CorrectiveIntent) { in.Reason = "" },
		"evidence":    func(in *CorrectiveIntent) { in.EvidenceRef = "" },
		"authority":   func(in *CorrectiveIntent) { in.AuthorityRef = "" },
		"idempotency": func(in *CorrectiveIntent) { in.IdempotencyKey = "" },
		"instant":     func(in *CorrectiveIntent) { in.OccurredAt = time.Time{} },
	} {
		in := settle006Intent()
		mutate(&in)
		if _, err := ApplyCorrectiveIntent(in); !errors.Is(err, ErrCorrectiveRejected) {
			t.Fatalf("fault %s must be SETTLE_006_REJECTED", name)
		}
	}
	zero := settle006Intent()
	zero.OriginalAmount = values.MustDecimal("0.00", 2, values.RoundingHalfUp)
	if _, err := ApplyCorrectiveIntent(zero); !errors.Is(err, ErrCorrectiveRejected) {
		t.Fatalf("zero original must be SETTLE_006_REJECTED")
	}
}

func TestTodo_SETTLE_006_Security(t *testing.T) {
	got, err := ApplyCorrectiveIntent(settle006Intent())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{got.Communication, got.Digest, got.InstructionRef} {
		if containsBankPayload(s) {
			t.Fatalf("corrective output must not leak bank data: %q", s)
		}
	}
	anon := settle006Intent()
	anon.AuthorityRef = ""
	if _, err := ApplyCorrectiveIntent(anon); !errors.Is(err, ErrCorrectiveRejected) {
		t.Fatalf("authority-free corrective must be SETTLE_006_REJECTED")
	}
}
