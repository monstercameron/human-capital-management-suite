package balance

import (
	"errors"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// BAL-006 completion: external dependents are recomputed in dependency order
// against version-pinned evidence. A dependent with a registered recalculator
// that returns evidence bound to the corrected balance digest and its pinned
// manifest version is RECALCULATED; dependents without an implementation stay
// RECONCILIATION_REQUIRED; claimed completion without evidence still fails.

type recalcCall struct {
	id       string
	upstream []CorrectionImpact
}

func mustOpening() values.Decimal {
	return values.MustDecimal("0.00", 2, values.RoundingExactRequired)
}

func recalcRequest(t *testing.T, d AccumulatorDefinition, original, corrected BalanceEntry, calls *[]recalcCall, mu *sync.Mutex, override map[string]func(RecalculationContext) (RecalculationEvidence, error)) RetroCorrectionRequest {
	t.Helper()
	recs := map[string]DependentRecalculator{}
	for _, dep := range correctionDeps() {
		dep := dep
		recs[dep.ID] = func(ctx RecalculationContext) (RecalculationEvidence, error) {
			if calls != nil {
				if mu != nil {
					mu.Lock()
					defer mu.Unlock()
				}
				*calls = append(*calls, recalcCall{id: ctx.Dependent.ID, upstream: append([]CorrectionImpact(nil), ctx.Upstream...)})
			}
			if fn, ok := override[dep.ID]; ok {
				return fn(ctx)
			}
			return RecalculationEvidence{InputDigest: ctx.Balance.Digest, Version: ctx.Dependent.Version, Digest: "recomputed:" + ctx.Dependent.ID}, nil
		}
	}
	return RetroCorrectionRequest{Definition: d, Original: original, Corrected: corrected, Ledger: []BalanceEntry{original}, Opening: mustOpening(), ExpectedDependencies: correctionDeps(), Dependencies: correctionDeps(), Recalculators: recs}
}

func TestTodo_BAL_006_RecalculatedDependents(t *testing.T) {
	d, original, corrected := correctionFixture(t)
	var calls []recalcCall
	result, err := ApplyRetroCorrection(recalcRequest(t, d, original, corrected, &calls, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Impacts) != 4 {
		t.Fatalf("impacts=%+v", result.Impacts)
	}
	for _, impact := range result.Impacts {
		if impact.State != RecalculationComplete {
			t.Fatalf("impact %s state=%s, want RECALCULATED", impact.ID, impact.State)
		}
		if impact.EvidenceDigest != "recomputed:"+impact.ID {
			t.Fatalf("impact %s evidence=%q", impact.ID, impact.EvidenceDigest)
		}
	}
	wantOrder := []string{"balance-result", "payroll-result", "tax-result", "benefits-result"}
	if len(calls) != 4 {
		t.Fatalf("recalculator calls=%+v", calls)
	}
	for i, id := range wantOrder {
		if calls[i].id != id {
			t.Fatalf("call order=%v, want dependency order %v", calls, wantOrder)
		}
		if len(calls[i].upstream) != i {
			t.Fatalf("call %s saw %d upstream impacts, want %d", id, len(calls[i].upstream), i)
		}
		for _, up := range calls[i].upstream {
			if up.State != RecalculationComplete {
				t.Fatalf("call %s saw upstream %+v not yet recalculated", id, up)
			}
		}
	}
	if result.Digest == "" {
		t.Fatal("recalculated result has no digest")
	}
}

func TestTodo_BAL_006_PartialRecalculationStaysReconciliationRequired(t *testing.T) {
	d, original, corrected := correctionFixture(t)
	req := recalcRequest(t, d, original, corrected, nil, nil, nil)
	req.Recalculators = map[string]DependentRecalculator{"balance-result": req.Recalculators["balance-result"]}
	partial, err := ApplyRetroCorrection(req)
	if err != nil {
		t.Fatal(err)
	}
	if partial.Impacts[0].State != RecalculationComplete || partial.Impacts[0].EvidenceDigest == "" {
		t.Fatalf("balance impact=%+v", partial.Impacts[0])
	}
	for _, impact := range partial.Impacts[1:] {
		if impact.State != ReconciliationRequired || impact.EvidenceDigest != "" {
			t.Fatalf("impact %s should stay reconciliation-required: %+v", impact.ID, impact)
		}
	}
	none, err := ApplyRetroCorrection(RetroCorrectionRequest{Definition: d, Original: original, Corrected: corrected, Ledger: []BalanceEntry{original}, Opening: mustOpening(), ExpectedDependencies: correctionDeps(), Dependencies: correctionDeps()})
	if err != nil {
		t.Fatal(err)
	}
	if partial.Digest == none.Digest {
		t.Fatal("recalculated digest does not bind recomputation evidence")
	}
}

func TestTodo_BAL_006_RecalculationOrder(t *testing.T) {
	d, original, corrected := correctionFixture(t)
	first, err := ApplyRetroCorrection(recalcRequest(t, d, original, corrected, nil, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		var calls []recalcCall
		var mu sync.Mutex
		next, err := ApplyRetroCorrection(recalcRequest(t, d, original, corrected, &calls, &mu, nil))
		if err != nil {
			t.Fatal(err)
		}
		if next.Digest != first.Digest {
			t.Fatalf("recomputation not deterministic: %s vs %s", next.Digest, first.Digest)
		}
		for j, id := range []string{"balance-result", "payroll-result", "tax-result", "benefits-result"} {
			if calls[j].id != id {
				t.Fatalf("run %d order=%v, map iteration leaked into recomputation order", i, calls)
			}
		}
	}
}

func TestTodo_BAL_006_RecalculationRace(t *testing.T) {
	d, original, corrected := correctionFixture(t)
	base, err := ApplyRetroCorrection(recalcRequest(t, d, original, corrected, nil, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := ApplyRetroCorrection(recalcRequest(t, d, original, corrected, nil, nil, nil))
			if err != nil || result.Digest != base.Digest {
				t.Errorf("result=%+v err=%v", result, err)
			}
		}()
	}
	wg.Wait()
}

func TestTodo_BAL_006_RecalculationMutation(t *testing.T) {
	d, original, corrected := correctionFixture(t)
	build := func(override map[string]func(RecalculationContext) (RecalculationEvidence, error), recs map[string]DependentRecalculator) RetroCorrectionRequest {
		req := recalcRequest(t, d, original, corrected, nil, nil, override)
		if recs != nil {
			req.Recalculators = recs
		}
		return req
	}
	cases := map[string]RetroCorrectionRequest{
		"recalculator error fails closed": build(map[string]func(RecalculationContext) (RecalculationEvidence, error){
			"payroll-result": func(RecalculationContext) (RecalculationEvidence, error) {
				return RecalculationEvidence{}, errors.New("payroll engine down")
			},
		}, nil),
		"version drift rejected": build(map[string]func(RecalculationContext) (RecalculationEvidence, error){
			"tax-result": func(ctx RecalculationContext) (RecalculationEvidence, error) {
				return RecalculationEvidence{InputDigest: ctx.Balance.Digest, Version: "tax/2", Digest: "recomputed:tax-result"}, nil
			},
		}, nil),
		"stale input digest rejected": build(map[string]func(RecalculationContext) (RecalculationEvidence, error){
			"benefits-result": func(ctx RecalculationContext) (RecalculationEvidence, error) {
				return RecalculationEvidence{InputDigest: "sha256:stale-balance", Version: ctx.Dependent.Version, Digest: "recomputed:benefits-result"}, nil
			},
		}, nil),
		"empty evidence digest rejected": build(map[string]func(RecalculationContext) (RecalculationEvidence, error){
			"balance-result": func(ctx RecalculationContext) (RecalculationEvidence, error) {
				return RecalculationEvidence{InputDigest: ctx.Balance.Digest, Version: ctx.Dependent.Version}, nil
			},
		}, nil),
		"unknown recalculator rejected": func() RetroCorrectionRequest {
			req := build(nil, nil)
			req.Recalculators["ghost-result"] = func(ctx RecalculationContext) (RecalculationEvidence, error) {
				return RecalculationEvidence{InputDigest: ctx.Balance.Digest, Version: "ghost/1", Digest: "recomputed:ghost"}, nil
			}
			return req
		}(),
	}
	for name, req := range cases {
		if _, err := ApplyRetroCorrection(req); !errors.Is(err, ErrCorrectionInvalid) {
			t.Fatalf("%s err=%v", name, err)
		}
	}
	nilRec := build(nil, nil)
	nilRec.Recalculators["payroll-result"] = nil
	res, err := ApplyRetroCorrection(nilRec)
	if err != nil {
		t.Fatal(err)
	}
	if res.Impacts[1].State != ReconciliationRequired {
		t.Fatalf("nil recalculator claimed completion: %+v", res.Impacts[1])
	}
}
