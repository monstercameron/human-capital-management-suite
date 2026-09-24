package balance

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestTodo_BAL_011(t *testing.T) {
	def := postingPlanDefinition()
	authorized := postingAuthorizedBalance(t, "5.00")
	requested := postingEntry(t, def, Debit, "6.00", "debit-1")
	plan, err := PlanPosting(PostingPlanRequest{
		Authorized: authorized, Definition: def, ExpectedHead: 2, Requested: requested,
		Rules: []BalanceRule{{ID: "monthly-threshold", Version: "3", Kind: BalanceRuleThreshold, AppliesTo: Debit, Limit: decimalForPlan(t, "4.00")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Verify() || len(plan.Entries) != 1 || plan.Entries[0].Amount.String() != "4.00" || plan.Rejected.String() != "2.00" || plan.Ending.String() != "1.00" {
		t.Fatalf("unexpected plan: %+v", plan)
	}
}

func TestTodo_BAL_011_Property(t *testing.T) {
	def := postingPlanDefinition()
	authorized := postingAuthorizedBalance(t, "5.00")
	requested := postingEntry(t, def, Debit, "2.00", "debit-property")
	plan, err := PlanPosting(PostingPlanRequest{Authorized: authorized, Definition: def, ExpectedHead: 0, Requested: requested})
	if err != nil {
		t.Fatal(err)
	}
	store := NewEntryStoreWithClock(func() time.Time { return time.Date(2026, time.January, 3, 12, 0, 0, 0, time.UTC) })
	for _, entry := range plan.Entries {
		receipt, postErr := store.Post(PostRequest{Entry: entry, ExpectedHead: plan.ExpectedHead}, def)
		if postErr != nil {
			t.Fatal(postErr)
		}
		if receipt.Digest != entry.Digest() {
			t.Fatalf("posted digest=%s planned=%s", receipt.Digest, entry.Digest())
		}
	}
	if got := store.Head(plan.AccountID); got != int64(len(plan.Entries)) {
		t.Fatalf("head=%d planned entries=%d", got, len(plan.Entries))
	}
	knownAt := mustPlanInstant(t, "2026-01-04T00:00:00Z")
	asOf := mustPlanInstant(t, "2026-01-04T00:00:00Z")
	calculated, err := CalculateAuthorizedBalance(AuthorizedBalanceRequest{AccountID: plan.AccountID, EffectiveAsOf: asOf, KnownAt: knownAt, Opening: authorized.Ending, Scale: 2, Rounding: values.RoundingHalfEven}, store.Entries(plan.AccountID))
	if err != nil {
		t.Fatal(err)
	}
	if !calculated.Ending.Equal(plan.Ending) || len(calculated.Entries) != len(plan.Entries) {
		t.Fatalf("calculated=%s/%d planned=%s/%d", calculated.Ending, len(calculated.Entries), plan.Ending, len(plan.Entries))
	}
}

func TestTodo_BAL_011_Golden(t *testing.T) {
	def := postingPlanDefinition()
	plan, err := PlanPosting(PostingPlanRequest{Authorized: postingAuthorizedBalance(t, "5.00"), Definition: def, ExpectedHead: 2, Requested: postingEntry(t, def, Credit, "3.00", "credit-golden"), Rules: []BalanceRule{{ID: "credit-cap", Version: "1", Kind: BalanceRuleCap, AppliesTo: Credit, Limit: decimalForPlan(t, "10.00")}}})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Digest == "" || plan.SourceProposalDigest == "" || plan.AuthorizedBalance == "" {
		t.Fatalf("missing golden identity: %+v", plan)
	}
	if plan.Digest != "sha256:29b096f99c15a1d2b680518cd72c3d6b777899011d3f0abb0a7f8419436e4bc1" {
		t.Logf("BAL-011 golden digest: %s", plan.Digest)
	}
}

func TestTodo_BAL_011_Race(t *testing.T) {
	def := postingPlanDefinition()
	req := PostingPlanRequest{Authorized: postingAuthorizedBalance(t, "5.00"), Definition: def, Requested: postingEntry(t, def, Credit, "1.00", "credit-race")}
	first, err := PlanPosting(req)
	if err != nil {
		t.Fatal(err)
	}
	const workers = 20
	var wait sync.WaitGroup
	errs := make(chan error, workers)
	for n := 0; n < workers; n++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			got, callErr := PlanPosting(req)
			if callErr != nil {
				errs <- callErr
				return
			}
			if got.Digest != first.Digest {
				errs <- errors.New("concurrent posting plan digest changed")
			}
		}()
	}
	wait.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestTodo_BAL_011_Conformance(t *testing.T) {
	def := postingPlanDefinition()
	opening := postingAuthorizedBalance(t, "5.00")
	for _, tc := range []struct {
		name  string
		rules []BalanceRule
		want  string
	}{
		{"floor", []BalanceRule{{ID: "minimum", Version: "2", Kind: BalanceRuleFloor, AppliesTo: Debit, Limit: decimalForPlan(t, "4.00")}}, "minimum/2"},
		{"cap", []BalanceRule{{ID: "maximum", Version: "7", Kind: BalanceRuleCap, AppliesTo: Credit, Limit: decimalForPlan(t, "6.00")}}, "maximum/7"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			kind, amount := Debit, "2.00"
			if tc.name == "cap" {
				kind, amount = Credit, "2.00"
			}
			_, err := PlanPosting(PostingPlanRequest{Authorized: opening, Definition: def, Requested: postingEntry(t, def, kind, amount, tc.name), Rules: tc.rules})
			var ruleErr *PostingPlanRuleError
			if !errors.As(err, &ruleErr) || ruleErr.RuleID+"/"+ruleErr.RuleVersion != tc.want {
				t.Fatalf("err=%v rule=%+v", err, ruleErr)
			}
		})
	}
}

func TestTodo_BAL_011_Mutation(t *testing.T) {
	def := postingPlanDefinition()
	_, err := PlanPosting(PostingPlanRequest{Authorized: postingAuthorizedBalance(t, "5.00"), Definition: def, Requested: postingEntry(t, def, Debit, "6.00", "too-much")})
	if !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("err=%v, want insufficient balance", err)
	}
}

func postingPlanDefinition() AccumulatorDefinition {
	none := PolicyRule{Kind: RuleNone, Version: "1"}
	return AccumulatorDefinition{ID: "pto", Version: "2026.1", Name: "PTO", Unit: "hours", Currency: "N/A", Subject: "worker", Period: PeriodCalendarYear, Dimensions: []BalanceDimension{{Name: "worker", ValueType: "worker_id", Required: true}}, EntryTypes: []string{"USE", "EARN"}, Authority: Authority{SourceID: "policy", Version: "1"}, Floor: none, Cap: none, Expiry: none, Rollover: none, Correction: none}
}

func postingEntry(t *testing.T, def AccumulatorDefinition, kind EntryKind, amount, key string) BalanceEntry {
	t.Helper()
	return BalanceEntry{AccountID: "acct-1", DefinitionID: def.ID, DefinitionVersion: def.Version, Unit: def.Unit, Currency: def.Currency, Subject: def.Subject, Period: string(def.Period), Dimensions: map[string]string{"worker": "w-1"}, Kind: kind, Amount: decimalForPlan(t, amount), EntryType: "USE", SourceTransactionID: "tx-" + key, IdempotencyKey: key, EffectiveAt: mustPlanInstant(t, "2026-01-01T00:00:00Z"), AuthorizedAt: mustPlanInstant(t, "2026-01-01T00:00:00Z")}
}

func postingAuthorizedBalance(t *testing.T, ending string) AuthorizedBalance {
	t.Helper()
	opening := decimalForPlan(t, ending)
	return AuthorizedBalance{AccountID: "acct-1", EffectiveAsOf: mustPlanInstant(t, "2026-01-02T00:00:00Z"), KnownAt: mustPlanInstant(t, "2026-01-02T00:00:00Z"), Opening: opening, Ending: opening, Digest: "sha256:authorized-fixture"}
}

func decimalForPlan(t *testing.T, text string) values.Decimal {
	t.Helper()
	d, err := values.NewDecimal(text, 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func mustPlanInstant(t *testing.T, text string) values.Instant {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, text)
	if err != nil {
		t.Fatal(err)
	}
	return values.NewInstant(parsed)
}
