package balance

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

// BAL-008: one explanation must derive the whole balance story -- opening,
// every counted entry, cap/floor treatment, expiry, rollover, corrections and
// the authority source -- from the BAL-003..BAL-007 derivations. A derivation
// with a missing definition dimension or an unevidenced correction is
// unexplained: ExplainBalanceDerivation returns BAL_008_REJECTED naming the
// offending field/state/version and persists nothing. Contributors the caller
// may not see are redacted by identity, never by amount, so redacted totals
// still reconcile.

// explainFixture builds one coherent derivation across the BAL-003 (as-of),
// BAL-004 (cap/floor), BAL-005 (expiry/rollover) and BAL-006 (correction)
// engines: grant 10.00, usage 2.50, superseded usage 1.00, correction 1.25,
// plus one future-effective entry that stays excluded. Ending is 7.75.
func explainFixture(t *testing.T) (AccumulatorDefinition, AuthorizedBalance, RuleApplicationResult, LifecycleResult, []CorrectionImpact) {
	t.Helper()
	def := validDefinition()
	at := instant(2026, 1, 15, 0, 0, 0)
	asOf := instant(2026, 2, 1, 0, 0, 0)
	known := instant(2026, 2, 5, 0, 0, 0)
	grant := bal003Entry(Credit, "GRANT", "10.00", "grant-8", at, at)
	grant.RecordedAt = at
	usage := bal003Entry(Debit, "USAGE", "2.50", "usage-8", at, at)
	usage.RecordedAt = at
	original := bal003Entry(Debit, "USAGE", "1.00", "usage-orig", at, at)
	original.RecordedAt = at
	correction := bal003Entry(Credit, "CORRECTION", "1.25", "correction-8", at, at)
	correction.RecordedAt = at
	correction.SupersedesDigest = original.Digest()
	pending := bal003Entry(Debit, "USAGE", "9.00", "pending-8", instant(2026, 3, 1, 0, 0, 0), at)
	pending.RecordedAt = at
	counted := []BalanceEntry{grant, usage, original, correction}
	bal, err := CalculateAuthorizedBalance(AuthorizedBalanceRequest{
		AccountID: "acct-1", EffectiveAsOf: asOf, KnownAt: known,
		Opening: decimal2("0.00"), Scale: 2, Rounding: decimal2("0.00").Rounding(),
	}, append(append([]BalanceEntry(nil), counted...), pending))
	if err != nil {
		t.Fatal(err)
	}
	if bal.Ending.String() != "7.75" {
		t.Fatalf("fixture ending=%s, want 7.75", bal.Ending.String())
	}
	floor := BalanceRule{ID: "minimum", Version: "v2", Kind: BalanceRuleFloor, Limit: decimal2("0.00")}
	cap := BalanceRule{ID: "period-cap", Version: "v7", Kind: BalanceRuleCap, Limit: decimal2("100.00")}
	rules, err := ApplyRules(ruleTestRequest(floor, cap), counted)
	if err != nil {
		t.Fatal(err)
	}
	life, err := ApplyLifecycle(LifecycleRequest{
		AccountID: "acct-1",
		Period:    PeriodDefinition{ID: "p-2026-01", Version: "period-v4", Kind: PeriodCalendarYear, Start: instant(2026, 1, 1, 0, 0, 0), End: instant(2026, 2, 1, 0, 0, 0)},
		Next:      PeriodDefinition{ID: "p-2026-02", Version: "period-v4", Kind: PeriodCalendarYear, Start: instant(2026, 2, 1, 0, 0, 0), End: instant(2026, 3, 1, 0, 0, 0)},
		Template:  ruleTestEntry(Credit, "1.00", "template-8"),
		Opening:   decimal2("0.00"), Scale: 2, Rounding: decimal2("0.00").Rounding(),
		Policy: rolloverPolicy(), Ledger: counted,
	})
	if err != nil {
		t.Fatal(err)
	}
	return def, bal, rules, life, explainImpacts()
}

func explainImpacts() []CorrectionImpact {
	return []CorrectionImpact{
		{ID: "balance-result", Owner: "balance", Version: "balance/1", State: ReconciliationRequired},
		{ID: "payroll-result", Owner: "payroll", Version: "payroll/1", DependsOn: []string{"balance-result"}, State: ReconciliationRequired},
		{ID: "tax-result", Owner: "tax", Version: "tax/1", DependsOn: []string{"payroll-result"}, State: ReconciliationRequired},
		{ID: "benefits-result", Owner: "benefits", Version: "benefits/1", DependsOn: []string{"tax-result"}, State: ReconciliationRequired},
	}
}

func explainRequest(t *testing.T) ExplanationRequest {
	t.Helper()
	def, bal, rules, life, impacts := explainFixture(t)
	return ExplanationRequest{Definition: def, Balance: bal, Rules: rules, Lifecycle: life, Impacts: impacts}
}

func assertExplanationRejected(t *testing.T, err error, field, state, version string) {
	t.Helper()
	if !errors.Is(err, ErrExplanationRejected) {
		t.Fatalf("err=%v, want explanation rejection", err)
	}
	var detail *ExplanationRejectedError
	if !errors.As(err, &detail) {
		t.Fatalf("err=%v, want typed rejected error", err)
	}
	if detail.Field != field || detail.State != state || detail.Version != version {
		t.Fatalf("detail=%+v, want field=%q state=%q version=%q", detail, field, state, version)
	}
	if want := "BAL_008_REJECTED definition=" + version; !strings.Contains(err.Error(), want) {
		t.Fatalf("err=%q, want marker %q", err.Error(), want)
	}
}

func TestTodo_BAL_008(t *testing.T) {
	def, bal, rules, life, impacts := explainFixture(t)
	// Authoritative rows exist before the explanation runs; the explanation
	// receives no write handle, so every path below must leave them alone.
	store := NewEntryStore()
	for i, e := range append(append([]BalanceEntry(nil), bal.Entries...), bal.Adjustments...) {
		if _, err := store.Post(PostRequest{Entry: e, ExpectedHead: int64(i)}, def); err != nil {
			t.Fatal(err)
		}
	}
	headBefore := store.Head("acct-1")

	got, err := ExplainBalanceDerivation(ExplanationRequest{Definition: def, Balance: bal, Rules: rules, Lifecycle: life, Impacts: impacts})
	if err != nil {
		t.Fatal(err)
	}
	if got.AccountID != "acct-1" || got.DefinitionID != "pto" || got.DefinitionVersion != "1.0.0" {
		t.Fatalf("identity=%+v", got)
	}
	if got.Authority.SourceID != "rewards.balance" || got.Authority.Version != "2026.1" {
		t.Fatalf("authority=%+v", got.Authority)
	}
	if got.Opening.String() != "0.00" || got.Ending.String() != "7.75" {
		t.Fatalf("opening=%s ending=%s", got.Opening.String(), got.Ending.String())
	}
	if len(got.Contributors) != 4 || got.Contributors[3].IdempotencyKey != "correction-8" || !got.Contributors[3].Adjustment {
		t.Fatalf("contributors=%+v", got.Contributors)
	}
	if len(got.Excluded) != 1 || got.Excluded[0].Reason != ExcludedFutureEffective {
		t.Fatalf("excluded=%+v", got.Excluded)
	}
	if got.Floor.Kind != RuleNone || got.Cap.Kind != RuleConfigured || len(got.RuleDecisions) != 8 || got.RulesDigest != rules.Digest {
		t.Fatalf("cap/floor decisions=%d digest match=%v", len(got.RuleDecisions), got.RulesDigest == rules.Digest)
	}
	if got.Expired.String() != "4.75" || got.Carried.String() != "3.00" || len(got.LifecycleEntries) != 3 || got.LifecycleDigest != life.Digest {
		t.Fatalf("expiry=%s carried=%s entries=%d", got.Expired.String(), got.Carried.String(), len(got.LifecycleEntries))
	}
	if got.Correction.Kind != RuleConfigured || len(got.CorrectionDigests) != 1 || len(got.Impacts) != 4 {
		t.Fatalf("corrections=%+v impacts=%+v", got.CorrectionDigests, got.Impacts)
	}
	if got.Redacted != 0 || got.Digest == "" || !got.Verify() {
		t.Fatalf("redacted=%d digest=%q verify=%v", got.Redacted, got.Digest, got.Verify())
	}

	// Inaccessible contributors lose identity but keep amounts: totals still reconcile.
	denied := map[string]bool{"usage-8": true}
	redacted, err := ExplainBalanceDerivation(ExplanationRequest{Definition: def, Balance: bal, Rules: rules, Lifecycle: life, Impacts: impacts, CanView: func(e BalanceEntry) bool {
		return !denied[e.IdempotencyKey]
	}})
	if err != nil {
		t.Fatal(err)
	}
	if redacted.Redacted == 0 || !redacted.Verify() || redacted.Ending.String() != "7.75" {
		t.Fatalf("redacted=%d verify=%v ending=%s", redacted.Redacted, redacted.Verify(), redacted.Ending.String())
	}
	for _, c := range redacted.Contributors {
		if c.Redacted && (c.IdempotencyKey != "REDACTED" || c.Amount.String() != "2.50" || string(c.Kind) != "DEBIT") {
			t.Fatalf("redacted contributor=%+v", c)
		}
		if !c.Redacted && c.IdempotencyKey == "REDACTED" {
			t.Fatalf("accessible contributor masked: %+v", c)
		}
	}
	for _, d := range redacted.RuleDecisions {
		if denied[d.SourceEntry] && d.SourceEntry != "REDACTED" {
			t.Fatalf("decision leaks denied source: %+v", d)
		}
	}

	// Seeded defects: a missing definition dimension or an unevidenced
	// correction yields an unexplained balance with zero persisted effects.
	narrow := def
	narrow.Dimensions = narrow.Dimensions[:1]
	_, err = ExplainBalanceDerivation(ExplanationRequest{Definition: narrow, Balance: bal, Rules: rules, Lifecycle: life, Impacts: impacts})
	assertExplanationRejected(t, err, "contributors[0].dimensions", "MISSING_DIMENSION", "1.0.0")
	_, err = ExplainBalanceDerivation(ExplanationRequest{Definition: def, Balance: bal, Rules: rules, Lifecycle: life})
	assertExplanationRejected(t, err, "corrections", "MISSING_CORRECTION", "1.0.0")

	if headAfter := store.Head("acct-1"); headAfter != headBefore {
		t.Fatalf("head=%d, want %d: explanation persisted rows", headAfter, headBefore)
	}
	if len(store.Entries("acct-1")) != int(headBefore) {
		t.Fatal("explanation mutated authoritative entries")
	}
}

func TestTodo_BAL_008_Property(t *testing.T) {
	req := explainRequest(t)
	first, err := ExplainBalanceDerivation(req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ExplainBalanceDerivation(req)
	if err != nil || second.Digest != first.Digest {
		t.Fatalf("explanation not deterministic: %q vs %q err=%v", second.Digest, first.Digest, err)
	}
	// Redaction never moves the total: every visibility slice reconciles.
	for name, view := range map[string]func(BalanceEntry) bool{
		"full":   nil,
		"allow":  func(BalanceEntry) bool { return true },
		"deny":   func(BalanceEntry) bool { return false },
		"half":   func(e BalanceEntry) bool { return e.Kind == Credit },
		"single": func(e BalanceEntry) bool { return e.IdempotencyKey != "grant-8" },
	} {
		req.CanView = view
		got, err := ExplainBalanceDerivation(req)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got.Ending.String() != "7.75" || !got.Verify() {
			t.Fatalf("%s: ending=%s verify=%v", name, got.Ending.String(), got.Verify())
		}
	}
	req.CanView = func(BalanceEntry) bool { return false }
	denied, err := ExplainBalanceDerivation(req)
	if err != nil {
		t.Fatal(err)
	}
	if denied.Redacted != len(denied.Contributors)+len(denied.Excluded) {
		t.Fatalf("redacted=%d contributors=%d excluded=%d", denied.Redacted, len(denied.Contributors), len(denied.Excluded))
	}
	// Dimension map order never leaks into the derivation digest.
	swap := explainRequest(t)
	for i := range swap.Balance.Entries {
		rebuilt := map[string]string{}
		keys := []string{}
		for k := range swap.Balance.Entries[i].Dimensions {
			keys = append(keys, k)
		}
		for j := len(keys) - 1; j >= 0; j-- {
			rebuilt[keys[j]] = swap.Balance.Entries[i].Dimensions[keys[j]]
		}
		swap.Balance.Entries[i].Dimensions = rebuilt
	}
	// Rebuilt maps digest identically, so the stored balance digest still binds.
	reswapped, err := ExplainBalanceDerivation(swap)
	if err != nil {
		t.Fatal(err)
	}
	if reswapped.Digest != first.Digest {
		t.Fatal("dimension order leaked into explanation digest")
	}
}

func TestTodo_BAL_008_Race(t *testing.T) {
	req := explainRequest(t)
	base, err := ExplainBalanceDerivation(req)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := ExplainBalanceDerivation(req)
			if err != nil || got.Digest != base.Digest || !got.Verify() {
				t.Errorf("got=%+v err=%v", got, err)
			}
		}()
	}
	wg.Wait()
}

func TestTodo_BAL_008_Mutation(t *testing.T) {
	def, bal, rules, life, impacts := explainFixture(t)
	mkreq := func() ExplanationRequest {
		return ExplanationRequest{Definition: def, Balance: bal, Rules: rules, Lifecycle: life, Impacts: append([]CorrectionImpact(nil), impacts...)}
	}
	t.Run("rules omitted", func(t *testing.T) {
		req := mkreq()
		req.Rules = RuleApplicationResult{}
		_, err := ExplainBalanceDerivation(req)
		assertExplanationRejected(t, err, "rules", "MISSING_RULES", "1.0.0")
	})
	t.Run("lifecycle omitted", func(t *testing.T) {
		req := mkreq()
		req.Lifecycle = LifecycleResult{}
		_, err := ExplainBalanceDerivation(req)
		assertExplanationRejected(t, err, "lifecycle", "MISSING_LIFECYCLE", "1.0.0")
	})
	t.Run("ending tampered", func(t *testing.T) {
		req := mkreq()
		req.Balance.Ending = decimal2("8.75")
		_, err := ExplainBalanceDerivation(req)
		assertExplanationRejected(t, err, "balance.digest", "DIGEST_MISMATCH", "1.0.0")
	})
	t.Run("total recomputed but wrong", func(t *testing.T) {
		req := mkreq()
		req.Balance.Ending = decimal2("8.75")
		digest, err := req.Balance.canonicalDigest()
		if err != nil {
			t.Fatal(err)
		}
		req.Balance.Digest = digest
		_, err = ExplainBalanceDerivation(req)
		assertExplanationRejected(t, err, "ending", "TOTAL_MISMATCH", "1.0.0")
	})
	t.Run("completion without evidence", func(t *testing.T) {
		req := mkreq()
		req.Impacts[0].State = RecalculationComplete
		_, err := ExplainBalanceDerivation(req)
		assertExplanationRejected(t, err, "corrections[0].evidence", "UNEVIDENCED_COMPLETION", "1.0.0")
	})
	t.Run("foreign rules account", func(t *testing.T) {
		req := mkreq()
		req.Rules.AccountID = "worker-9"
		_, err := ExplainBalanceDerivation(req)
		assertExplanationRejected(t, err, "rules.account_id", "ACCOUNT_MISMATCH", "1.0.0")
	})
	t.Run("definition revision drift", func(t *testing.T) {
		req := mkreq()
		req.Definition.Version = "2.0.0"
		_, err := ExplainBalanceDerivation(req)
		assertExplanationRejected(t, err, "contributors[0].definition", "REVISION_MISMATCH", "2.0.0")
	})
	t.Run("accountless balance", func(t *testing.T) {
		req := mkreq()
		req.Balance.AccountID = ""
		digest, err := req.Balance.canonicalDigest()
		if err != nil {
			t.Fatal(err)
		}
		req.Balance.Digest = digest
		_, err = ExplainBalanceDerivation(req)
		assertExplanationRejected(t, err, "balance.account_id", "ACCOUNT_MISMATCH", "1.0.0")
	})
	t.Run("excluded dimension drift", func(t *testing.T) {
		req := mkreq()
		req.Balance.Excluded = append([]ExcludedEntry(nil), req.Balance.Excluded...)
		drifted := req.Balance.Excluded[0].Entry
		drifted.Dimensions = mapsCopy(drifted.Dimensions)
		drifted.Dimensions["ghost"] = "x"
		req.Balance.Excluded[0].Entry = drifted
		digest, err := req.Balance.canonicalDigest()
		if err != nil {
			t.Fatal(err)
		}
		req.Balance.Digest = digest
		_, err = ExplainBalanceDerivation(req)
		assertExplanationRejected(t, err, "excluded[0].dimensions", "MISSING_DIMENSION", "1.0.0")
	})
	t.Run("unknown impact state", func(t *testing.T) {
		req := mkreq()
		req.Impacts[0].State = "DONE"
		_, err := ExplainBalanceDerivation(req)
		assertExplanationRejected(t, err, "corrections[0].state", "INVALID_IMPACT", "1.0.0")
	})
	t.Run("impact without owner", func(t *testing.T) {
		req := mkreq()
		req.Impacts[1].Owner = ""
		_, err := ExplainBalanceDerivation(req)
		assertExplanationRejected(t, err, "corrections[1].owner", "INVALID_IMPACT", "1.0.0")
	})
	t.Run("authority stripped", func(t *testing.T) {
		req := mkreq()
		req.Definition.Authority = Authority{}
		_, err := ExplainBalanceDerivation(req)
		assertExplanationRejected(t, err, "authority.source_id", "DEFINITION_INVALID", "1.0.0")
	})
}
