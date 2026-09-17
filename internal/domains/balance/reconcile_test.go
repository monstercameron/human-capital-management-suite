package balance

import (
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// BAL-007: an external observation reconciles against the canonical
// derivation only when it names the same account, period and as-of instant,
// cites the canonical digest it read, is observed no earlier than the
// canonical known-at, covers every counted entry, and agrees on the amount.
// Anything less is a MISMATCH that still identifies dimensions, period,
// expected/observed/delta and the repair owner.

func reconcileFixture(t *testing.T) (AuthorizedBalance, BalanceEntry, BalanceEntry) {
	t.Helper()
	at := correctionInstant(t, "2026-06-01T00:00:00Z")
	recorded := correctionInstant(t, "2026-06-02T00:00:00Z")
	grant := validEntry()
	grant.Kind = Credit
	grant.EntryType = "GRANT"
	grant.Amount = values.MustDecimal("10.00", 2, values.RoundingExactRequired)
	grant.SourceTransactionID, grant.IdempotencyKey = "grant-1", "grant-1"
	grant.EffectiveAt, grant.RecordedAt, grant.AuthorizedAt = at, recorded, recorded
	usage := validEntry()
	usage.EffectiveAt, usage.RecordedAt, usage.AuthorizedAt = at, recorded, recorded
	bal, err := CalculateAuthorizedBalance(AuthorizedBalanceRequest{AccountID: grant.AccountID, EffectiveAsOf: at, KnownAt: recorded, Opening: values.MustDecimal("0.00", 2, values.RoundingExactRequired), Scale: 2, Rounding: values.RoundingExactRequired}, []BalanceEntry{grant, usage})
	if err != nil {
		t.Fatal(err)
	}
	if bal.Ending.String() != "7.50" {
		t.Fatalf("ending=%s", bal.Ending.String())
	}
	return bal, grant, usage
}

func reconcileObservation(bal AuthorizedBalance, grant, usage BalanceEntry) ExternalBalance {
	return ExternalBalance{
		AccountID:      bal.AccountID,
		Dimensions:     map[string]string{"worker_id": "worker-1", "program": "pto"},
		Period:         string(PeriodCalendarYear),
		EffectiveAsOf:  bal.EffectiveAsOf,
		BasisDigest:    bal.Digest,
		CoveredDigests: []string{grant.Digest(), usage.Digest()},
		Observed:       bal.Ending,
		ObservedAt:     bal.KnownAt,
		SourceID:       "payroll-adapter",
	}
}

func TestTodo_BAL_007(t *testing.T) {
	bal, grant, usage := reconcileFixture(t)
	got, err := ReconcileExternalBalance(ReconciliationRequest{Canonical: bal, Observed: reconcileObservation(bal, grant, usage)})
	if err != nil {
		t.Fatal(err)
	}
	if got.Verdict != ReconciliationMatch || got.Reason != ReasonValueMatch || !got.Delta.IsZero() || got.Digest == "" {
		t.Fatalf("match=%+v", got)
	}
	obs := reconcileObservation(bal, grant, usage)
	obs.Observed = values.MustDecimal("7.00", 2, values.RoundingExactRequired)
	mismatch, err := ReconcileExternalBalance(ReconciliationRequest{Canonical: bal, Observed: obs})
	if err != nil {
		t.Fatal(err)
	}
	if mismatch.Verdict != ReconciliationMismatch || mismatch.Reason != ReasonValueMismatch {
		t.Fatalf("mismatch=%+v", mismatch)
	}
	if mismatch.Expected.String() != "7.50" || mismatch.Observed.String() != "7.00" || mismatch.Delta.String() != "-0.50" {
		t.Fatalf("amounts=%+v", mismatch)
	}
	if mismatch.Owner != "payroll-adapter" || mismatch.Period != string(PeriodCalendarYear) || mismatch.Dimensions["worker_id"] != "worker-1" || mismatch.Digest == "" {
		t.Fatalf("identity=%+v", mismatch)
	}
}

func TestTodo_BAL_007_Property(t *testing.T) {
	bal, grant, usage := reconcileFixture(t)
	first, err := ReconcileExternalBalance(ReconciliationRequest{Canonical: bal, Observed: reconcileObservation(bal, grant, usage)})
	if err != nil {
		t.Fatal(err)
	}
	swapped := reconcileObservation(bal, grant, usage)
	swapped.Dimensions = map[string]string{"program": "pto", "worker_id": "worker-1"}
	swapped.CoveredDigests = []string{usage.Digest(), grant.Digest()}
	second, err := ReconcileExternalBalance(ReconciliationRequest{Canonical: bal, Observed: swapped})
	if err != nil {
		t.Fatal(err)
	}
	if second.Verdict != ReconciliationMatch || second.Digest != first.Digest {
		t.Fatalf("dimension/coverage order leaked: %+v vs %+v", first, second)
	}
	short := reconcileObservation(bal, grant, usage)
	short.Observed = values.MustDecimal("9.00", 2, values.RoundingExactRequired)
	over, err := ReconcileExternalBalance(ReconciliationRequest{Canonical: bal, Observed: short})
	if err != nil {
		t.Fatal(err)
	}
	roundtrip, err := over.Expected.Add(over.Delta)
	if err != nil || !roundtrip.Equal(over.Observed) {
		t.Fatalf("expected+delta != observed: %+v", over)
	}
}

func TestTodo_BAL_007_Golden(t *testing.T) {
	bal, grant, usage := reconcileFixture(t)
	got, err := ReconcileExternalBalance(ReconciliationRequest{Canonical: bal, Observed: reconcileObservation(bal, grant, usage)})
	if err != nil {
		t.Fatal(err)
	}
	if got.Expected.String() != "7.50" || got.Observed.String() != "7.50" || got.Delta.String() != "0.00" || got.Owner != "payroll-adapter" {
		t.Fatalf("golden amounts=%+v", got)
	}
	const wantDigest = "sha256:2195022ad332177e56a9707756750a820585a3f91661a0bdea1c48e45e340507"
	if got.Digest != wantDigest {
		t.Fatalf("golden digest=%s", got.Digest)
	}
}

func TestTodo_BAL_007_Race(t *testing.T) {
	bal, grant, usage := reconcileFixture(t)
	req := ReconciliationRequest{Canonical: bal, Observed: reconcileObservation(bal, grant, usage)}
	base, err := ReconcileExternalBalance(req)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := ReconcileExternalBalance(req)
			if err != nil || got.Digest != base.Digest {
				t.Errorf("got=%+v err=%v", got, err)
			}
		}()
	}
	wg.Wait()
}

func TestTodo_BAL_007_Mutation(t *testing.T) {
	bal, grant, usage := reconcileFixture(t)
	cases := map[string]struct {
		mutate func(*ExternalBalance)
		reason ReconciliationReason
	}{
		"stale observation cannot match": {func(o *ExternalBalance) {
			o.ObservedAt = correctionInstant(t, "2026-06-01T00:00:00Z")
		}, ReasonStaleObservation},
		"partial coverage cannot match": {func(o *ExternalBalance) {
			o.CoveredDigests = o.CoveredDigests[:1]
		}, ReasonPartialCoverage},
		"extra coverage cannot match": {func(o *ExternalBalance) {
			o.CoveredDigests = append(o.CoveredDigests, "sha256:unknown")
		}, ReasonPartialCoverage},
		"wrong basis cannot match": {func(o *ExternalBalance) {
			o.BasisDigest = "sha256:other-derivation"
		}, ReasonBasisMismatch},
		"wrong account cannot match": {func(o *ExternalBalance) {
			o.AccountID = "worker-2"
		}, ReasonAccountMismatch},
		"wrong period cannot match": {func(o *ExternalBalance) {
			o.Period = string(PeriodPayPeriod)
		}, ReasonPeriodMismatch},
		"wrong as-of cannot match": {func(o *ExternalBalance) {
			o.EffectiveAsOf = correctionInstant(t, "2026-07-01T00:00:00Z")
		}, ReasonAsOfMismatch},
	}
	for name, tc := range cases {
		obs := reconcileObservation(bal, grant, usage)
		tc.mutate(&obs)
		got, err := ReconcileExternalBalance(ReconciliationRequest{Canonical: bal, Observed: obs})
		if err != nil {
			t.Fatalf("%s err=%v", name, err)
		}
		if got.Verdict != ReconciliationMismatch || got.Reason != tc.reason {
			t.Fatalf("%s got=%+v", name, got)
		}
		if got.Owner != "payroll-adapter" || got.Digest == "" {
			t.Fatalf("%s lost repair identity: %+v", name, got)
		}
	}
	invalid := reconcileObservation(bal, grant, usage)
	invalid.SourceID = ""
	if _, err := ReconcileExternalBalance(ReconciliationRequest{Canonical: bal, Observed: invalid}); err == nil {
		t.Fatal("ownerless observation accepted")
	}
	bare := reconcileObservation(bal, grant, usage)
	bare.Observed = values.Decimal{}
	if _, err := ReconcileExternalBalance(ReconciliationRequest{Canonical: bal, Observed: bare}); err == nil {
		t.Fatal("undecided observation accepted")
	}
	fresh := reconcileObservation(bal, grant, usage)
	fresh.ObservedAt = correctionInstant(t, "2026-06-03T00:00:00Z")
	fresh.Observed = values.MustDecimal("7.5", 1, values.RoundingExactRequired)
	if got, err := ReconcileExternalBalance(ReconciliationRequest{Canonical: bal, Observed: fresh}); err != nil || got.Verdict != ReconciliationMatch {
		t.Fatalf("scale-tolerant match got=%+v err=%v", got, err)
	}
}
