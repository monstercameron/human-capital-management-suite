package balance

import (
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// BAL-010: the shared entry/time/correction/explanation model must pass one
// conformance vector per domain -- Payroll, Leave, Time, Benefits, Tax --
// without erasing statutory/domain-specific composition. A vector with a
// missing definition dimension or an unevidenced correction is unexplained:
// ProveCrossDomainConformance returns BAL_010_REJECTED naming the offending
// field/state/version. The prover is pure: it persists nothing.

func bal010Definition(extraDims ...string) AccumulatorDefinition {
	d := validDefinition()
	d.ID = "bal010"
	d.Version = "1.0.0"
	for _, name := range extraDims {
		d.Dimensions = append(d.Dimensions, BalanceDimension{Name: name, ValueType: "reference", Required: true})
	}
	return d
}

func bal010Entry(def AccumulatorDefinition, account string, kind EntryKind, entryType, amount, key string, comp map[string]string) BalanceEntry {
	dims := map[string]string{"worker_id": account, "program": "core"}
	for k, v := range comp {
		dims[k] = v
	}
	at := instant(2026, 1, 10, 0, 0, 0)
	return BalanceEntry{
		AccountID:           account,
		DefinitionID:        def.ID,
		DefinitionVersion:   def.Version,
		Unit:                def.Unit,
		Currency:            def.Currency,
		Subject:             def.Subject,
		Period:              string(def.Period),
		Dimensions:          dims,
		Kind:                kind,
		Amount:              decimal2(amount),
		EntryType:           entryType,
		SourceTransactionID: "tx-" + key,
		IdempotencyKey:      key,
		EffectiveAt:         at,
		RecordedAt:          at,
		AuthorizedAt:        at,
	}
}

// bal010Vectors builds the five domain vectors: grant 40.00, usage 7.50,
// ending 32.50. The Tax vector additionally carries a correction pair
// (original usage 5.00 superseded by a 5.00 correction credit) so the shared
// correction model is exercised without changing the ending.
func bal010Vectors(t *testing.T) CrossDomainRequest {
	t.Helper()
	comps := []struct {
		domain string
		comp   map[string]string
	}{
		{DomainPayroll, map[string]string{"pay_group": "pg-weekly", "withholding_class": "supplemental"}},
		{DomainLeave, map[string]string{"leave_program": "fmla", "statutory_basis": "29-cfr-825"}},
		{DomainTime, map[string]string{"schedule": "sched-rotating", "overtime_rule": "flsa-weekly"}},
		{DomainBenefits, map[string]string{"plan": "ppo-2026", "tier": "employee-plus-one"}},
		{DomainTax, map[string]string{"jurisdiction": "US-CA", "form": "de-9"}},
	}
	req := CrossDomainRequest{}
	for _, spec := range comps {
		keys := make([]string, 0, len(spec.comp))
		for k := range spec.comp {
			keys = append(keys, k)
		}
		def := bal010Definition(keys...)
		account := "acct-bal010-" + spec.domain
		ledger := []BalanceEntry{
			bal010Entry(def, account, Credit, "GRANT", "40.00", spec.domain+"-grant", spec.comp),
			bal010Entry(def, account, Debit, "USAGE", "7.50", spec.domain+"-use", spec.comp),
		}
		if spec.domain == DomainTax {
			orig := bal010Entry(def, account, Debit, "USAGE", "5.00", "TAX-orig", spec.comp)
			corr := bal010Entry(def, account, Credit, "CORRECTION", "5.00", "TAX-corr", spec.comp)
			corr.SupersedesDigest = orig.Digest()
			ledger = append(ledger, orig, corr)
		}
		req.Vectors = append(req.Vectors, DomainVector{
			Domain:         spec.domain,
			Definition:     def,
			Opening:        decimal2("0.00"),
			Ledger:         ledger,
			EffectiveAsOf:  instant(2026, 2, 1, 0, 0, 0),
			KnownAt:        instant(2026, 2, 5, 0, 0, 0),
			Composition:    spec.comp,
			ExpectedEnding: decimal2("32.50"),
		})
	}
	return req
}

func bal010Rejected(t *testing.T, err error, field, state, version string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected BAL_010_REJECTED (field=%s state=%s), got nil", field, state)
	}
	if !errors.Is(err, ErrCrossDomainRejected) {
		t.Fatalf("error %v does not wrap ErrCrossDomainRejected", err)
	}
	var detail *CrossDomainRejectedError
	if !errors.As(err, &detail) {
		t.Fatalf("error %v is not a *CrossDomainRejectedError", err)
	}
	if detail.Field != field || detail.State != state || detail.Version != version {
		t.Fatalf("rejection = field=%s state=%s version=%s, want field=%s state=%s version=%s",
			detail.Field, detail.State, detail.Version, field, state, version)
	}
}

// TestTodo_BAL_010 is the PRIMARY test: all five domain vectors pass the
// shared model with exact endings and preserved composition, while seeded
// defects (missing dimension, unevidenced correction, erased composition)
// return BAL_010_REJECTED with field/state/version.
func TestTodo_BAL_010(t *testing.T) {
	req := bal010Vectors(t)
	before := make([]string, 0)
	for _, v := range req.Vectors {
		for _, e := range v.Ledger {
			before = append(before, e.Digest())
		}
	}
	got, err := ProveCrossDomainConformance(req)
	if err != nil {
		t.Fatalf("ProveCrossDomainConformance() error = %v", err)
	}
	if len(got.Outcomes) != 5 {
		t.Fatalf("outcomes = %d, want 5", len(got.Outcomes))
	}
	seen := map[string]bool{}
	for i, o := range got.Outcomes {
		if o.Domain != req.Vectors[i].Domain {
			t.Fatalf("outcome[%d].Domain = %s, want %s (request order)", i, o.Domain, req.Vectors[i].Domain)
		}
		if o.Ending.String() != "32.50" {
			t.Errorf("%s: ending = %s, want 32.50", o.Domain, o.Ending.String())
		}
		if o.Counted != len(req.Vectors[i].Ledger) {
			t.Errorf("%s: counted = %d, want %d", o.Domain, o.Counted, len(req.Vectors[i].Ledger))
		}
		if !reflect.DeepEqual(o.Composition, req.Vectors[i].Composition) {
			t.Errorf("%s: composition not preserved: %#v", o.Domain, o.Composition)
		}
		if o.Digest == "" {
			t.Errorf("%s: empty outcome digest", o.Domain)
		}
		seen[o.Digest] = true
	}
	if len(seen) != 5 {
		t.Errorf("outcome digests collide across domains: %d distinct for 5 vectors", len(seen))
	}
	if got.Digest == "" {
		t.Error("empty result digest")
	}
	// Zero-effect: the prover must not mutate the caller's ledger.
	after := make([]string, 0)
	for _, v := range req.Vectors {
		for _, e := range v.Ledger {
			after = append(after, e.Digest())
		}
	}
	if !reflect.DeepEqual(before, after) {
		t.Error("prover mutated the caller's ledger")
	}

	t.Run("missing dimension", func(t *testing.T) {
		bad := bal010Vectors(t)
		bad.Vectors[0].Ledger[1].Dimensions["pay_group"] = ""
		bal010Rejected(t, mustProve(bad), "vectors[0].ledger[1].dimensions.pay_group", "MISSING_DIMENSION", "1.0.0")
	})
	t.Run("unevidenced correction", func(t *testing.T) {
		bad := bal010Vectors(t)
		bad.Vectors[4].Ledger[3].SupersedesDigest = ""
		bal010Rejected(t, mustProve(bad), "vectors[4].ledger[3].supersedes", "MISSING_CORRECTION", "1.0.0")
	})
	t.Run("erased composition", func(t *testing.T) {
		bad := bal010Vectors(t)
		bad.Vectors[4].Ledger[0].Dimensions["jurisdiction"] = "US-NV"
		bal010Rejected(t, mustProve(bad), "vectors[4].composition.jurisdiction", "COMPOSITION_ERASED", "1.0.0")
	})
	t.Run("duplicate domain", func(t *testing.T) {
		bad := bal010Vectors(t)
		bad.Vectors[1] = bad.Vectors[0]
		bal010Rejected(t, mustProve(bad), "vectors[1].domain", "DUPLICATE_DOMAIN", "1.0.0")
	})
	t.Run("wrong expectation", func(t *testing.T) {
		bad := bal010Vectors(t)
		bad.Vectors[2].ExpectedEnding = decimal2("32.51")
		bal010Rejected(t, mustProve(bad), "vectors[2].expected_ending", "TOTAL_MISMATCH", "1.0.0")
	})
	t.Run("unknown domain", func(t *testing.T) {
		bad := bal010Vectors(t)
		bad.Vectors[3].Domain = "EQUITY"
		bal010Rejected(t, mustProve(bad), "vectors[3].domain", "UNKNOWN_DOMAIN", "1.0.0")
	})
}

func mustProve(req CrossDomainRequest) error {
	_, err := ProveCrossDomainConformance(req)
	return err
}

// TestTodo_BAL_010_Property: the shared model is order-independent and
// decimal-exact. Shuffled ledgers reach the same ending with composition
// intact; sub-cent precision is refused rather than rounded.
func TestTodo_BAL_010_Property(t *testing.T) {
	t.Run("order independent", func(t *testing.T) {
		base := bal010Vectors(t).Vectors[4]
		orders := [][]int{{1, 0, 2, 3}, {3, 2, 1, 0}, {2, 0, 3, 1}, {0, 2, 3, 1}, {2, 3, 0, 1}}
		for oi, order := range orders {
			v := base
			v.Ledger = make([]BalanceEntry, len(base.Ledger))
			for j, src := range order {
				v.Ledger[j] = base.Ledger[src]
			}
			got, err := ProveCrossDomainConformance(CrossDomainRequest{Vectors: []DomainVector{v}})
			if err != nil {
				t.Fatalf("order %d: error = %v", oi, err)
			}
			if got.Outcomes[0].Ending.String() != "32.50" {
				t.Errorf("order %d: ending = %s, want 32.50", oi, got.Outcomes[0].Ending.String())
			}
			if !reflect.DeepEqual(got.Outcomes[0].Composition, base.Composition) {
				t.Errorf("order %d: composition not preserved", oi)
			}
		}
	})
	t.Run("decimal exact", func(t *testing.T) {
		def := bal010Definition("jurisdiction", "form")
		account := "acct-bal010-exact"
		comp := map[string]string{"jurisdiction": "US-CA", "form": "de-9"}
		v := DomainVector{
			Domain:        DomainTax,
			Definition:    def,
			Opening:       decimal2("0.00"),
			EffectiveAsOf: instant(2026, 2, 1, 0, 0, 0),
			KnownAt:       instant(2026, 2, 5, 0, 0, 0),
			Composition:   comp,
			Ledger: []BalanceEntry{
				bal010Entry(def, account, Credit, "GRANT", "0.30", "exact-grant", comp),
				bal010Entry(def, account, Debit, "USAGE", "0.10", "exact-use1", comp),
				bal010Entry(def, account, Debit, "USAGE", "0.20", "exact-use2", comp),
			},
			ExpectedEnding: decimal2("0.00"),
		}
		got, err := ProveCrossDomainConformance(CrossDomainRequest{Vectors: []DomainVector{v}})
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if got.Outcomes[0].Ending.String() != "0.00" {
			t.Fatalf("ending = %s, want 0.00 (0.30-0.10-0.20 must be exact)", got.Outcomes[0].Ending.String())
		}
	})
	t.Run("sub-cent refused", func(t *testing.T) {
		def := bal010Definition("jurisdiction", "form")
		account := "acct-bal010-scale"
		comp := map[string]string{"jurisdiction": "US-CA", "form": "de-9"}
		coarse := bal010Entry(def, account, Credit, "GRANT", "1.00", "scale-grant", comp)
		fine := bal010Entry(def, account, Debit, "USAGE", "0.00", "scale-use", comp)
		// A scale-3 amount at a scale-2 exact balance kind: refused, never rounded.
		fine.Amount = values.MustDecimal("0.005", 3, values.RoundingExactRequired)
		v := DomainVector{
			Domain:         DomainTax,
			Definition:     def,
			Opening:        decimal2("0.00"),
			EffectiveAsOf:  instant(2026, 2, 1, 0, 0, 0),
			KnownAt:        instant(2026, 2, 5, 0, 0, 0),
			Composition:    comp,
			Ledger:         []BalanceEntry{coarse, fine},
			ExpectedEnding: decimal2("1.00"),
		}
		if err := mustProve(CrossDomainRequest{Vectors: []DomainVector{v}}); err == nil {
			t.Fatal("scale-3 amount accepted at scale-2 exact rounding; want rejection")
		} else if !errors.Is(err, ErrCrossDomainRejected) {
			t.Fatalf("error %v does not wrap ErrCrossDomainRejected", err)
		}
	})
}

// TestTodo_BAL_010_Race: concurrent prover runs over a shared request reach
// identical digests, and concurrent postings with distinct keys converge to
// the serial sum. Run with -race.
func TestTodo_BAL_010_Race(t *testing.T) {
	req := bal010Vectors(t)
	const workers = 8
	digests := make([]string, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			got, err := ProveCrossDomainConformance(req)
			if err != nil {
				errs[w] = err
				return
			}
			digests[w] = got.Digest
		}(w)
	}
	wg.Wait()
	for w := 0; w < workers; w++ {
		if errs[w] != nil {
			t.Fatalf("worker %d: %v", w, errs[w])
		}
		if digests[w] != digests[0] {
			t.Fatalf("worker %d digest %s != worker 0 digest %s", w, digests[w], digests[0])
		}
	}

	t.Run("concurrent postings converge", func(t *testing.T) {
		def := bal010Definition("jurisdiction", "form")
		account := "acct-bal010-race"
		comp := map[string]string{"jurisdiction": "US-CA", "form": "de-9"}
		store := NewEntryStore()
		const posters = 8
		var pwg sync.WaitGroup
		perrs := make([]error, posters)
		for p := 0; p < posters; p++ {
			pwg.Add(1)
			go func(p int) {
				defer pwg.Done()
				// Distinct keys per poster; retry on stale head.
				key := string(rune('a'+p)) + "-race-key"
				e := bal010Entry(def, account, Credit, "GRANT", "1.00", key, comp)
				for {
					head := store.Head(account)
					if _, err := store.Post(PostRequest{Entry: e, ExpectedHead: head}, def); err != nil {
						if errors.Is(err, ErrStaleHead) {
							continue
						}
						perrs[p] = err
					}
					return
				}
			}(p)
		}
		pwg.Wait()
		for p := 0; p < posters; p++ {
			if perrs[p] != nil {
				t.Fatalf("poster %d: %v", p, perrs[p])
			}
		}
		v := DomainVector{
			Domain:         DomainTax,
			Definition:     def,
			Opening:        decimal2("0.00"),
			EffectiveAsOf:  instant(2031, 1, 1, 0, 0, 0),
			KnownAt:        instant(2031, 2, 1, 0, 0, 0),
			Composition:    comp,
			Ledger:         store.Entries(account),
			ExpectedEnding: decimal2("8.00"),
		}
		got, err := ProveCrossDomainConformance(CrossDomainRequest{Vectors: []DomainVector{v}})
		if err != nil {
			t.Fatalf("prove over raced ledger: %v", err)
		}
		if got.Outcomes[0].Ending.String() != "8.00" {
			t.Fatalf("ending = %s, want 8.00 (8 x 1.00, no lost update)", got.Outcomes[0].Ending.String())
		}
		if got.Outcomes[0].Counted != posters {
			t.Fatalf("counted = %d, want %d", got.Outcomes[0].Counted, posters)
		}
	})
}

// TestTodo_BAL_010_Conformance: every domain vector passes through the same
// shared-model oracle -- definition, entry, bitemporal calculation -- and the
// prover is a pure function of its inputs.
func TestTodo_BAL_010_Conformance(t *testing.T) {
	req := bal010Vectors(t)
	first, err := ProveCrossDomainConformance(req)
	if err != nil {
		t.Fatalf("first prove: %v", err)
	}
	second, err := ProveCrossDomainConformance(req)
	if err != nil {
		t.Fatalf("second prove: %v", err)
	}
	if first.Digest != second.Digest {
		t.Fatal("prover is not a pure function of its inputs")
	}
	for i, v := range req.Vectors {
		bal, err := CalculateAuthorizedBalance(AuthorizedBalanceRequest{
			AccountID:     v.Ledger[0].AccountID,
			EffectiveAsOf: v.EffectiveAsOf,
			KnownAt:       v.KnownAt,
			Opening:       v.Opening,
			Scale:         v.Opening.Scale(),
			Rounding:      v.Opening.Rounding(),
		}, v.Ledger)
		if err != nil {
			t.Fatalf("%s: shared calculation: %v", v.Domain, err)
		}
		if !bal.Ending.Equal(first.Outcomes[i].Ending) {
			t.Errorf("%s: prover ending %s != shared-model ending %s",
				v.Domain, first.Outcomes[i].Ending.String(), bal.Ending.String())
		}
		declared := map[string]bool{}
		for _, d := range v.Definition.Dimensions {
			declared[d.Name] = true
		}
		for key := range v.Composition {
			if !declared[key] {
				t.Errorf("%s: composition key %s is not a declared dimension (erased at the contract)", v.Domain, key)
			}
		}
	}
}

// TestTodo_BAL_010_Mutation: seeded semantic mutants -- weakened inputs a
// defective prover would accept -- must all be killed (rejected) while the
// unmutated control passes.
func TestTodo_BAL_010_Mutation(t *testing.T) {
	if _, err := ProveCrossDomainConformance(bal010Vectors(t)); err != nil {
		t.Fatalf("control vector set rejected: %v", err)
	}
	mutants := []struct {
		name   string
		mutate func(*CrossDomainRequest)
		field  string
		state  string
	}{
		{"empty required dimension", func(r *CrossDomainRequest) {
			r.Vectors[1].Ledger[0].Dimensions["leave_program"] = ""
		}, "vectors[1].ledger[0].dimensions.leave_program", "MISSING_DIMENSION"},
		{"correction without lineage", func(r *CrossDomainRequest) {
			r.Vectors[4].Ledger[3].SupersedesDigest = ""
		}, "vectors[4].ledger[3].supersedes", "MISSING_CORRECTION"},
		{"swapped statutory value", func(r *CrossDomainRequest) {
			r.Vectors[4].Ledger[2].Dimensions["form"] = "w-2"
		}, "vectors[4].composition.form", "COMPOSITION_ERASED"},
		{"tampered expectation", func(r *CrossDomainRequest) {
			r.Vectors[0].ExpectedEnding = decimal2("40.00")
		}, "vectors[0].expected_ending", "TOTAL_MISMATCH"},
	}
	for _, m := range mutants {
		t.Run(m.name, func(t *testing.T) {
			bad := bal010Vectors(t)
			m.mutate(&bad)
			bal010Rejected(t, mustProve(bad), m.field, m.state, "1.0.0")
		})
	}
}

// TestTodo_BAL_010_Security: rejections carry the BAL_010_REJECTED marker
// with field/state/version and never echo ledger contents; hostile domain
// strings are refused as unknown.
func TestTodo_BAL_010_Security(t *testing.T) {
	t.Run("rejections never echo ledger contents", func(t *testing.T) {
		bad := bal010Vectors(t)
		bad.Vectors[0].Ledger[1].Dimensions["pay_group"] = ""
		err := mustProve(bad)
		if err == nil {
			t.Fatal("missing dimension accepted")
		}
		text := err.Error()
		if !strings.Contains(text, "BAL_010_REJECTED") {
			t.Fatalf("rejection without BAL_010_REJECTED marker: %q", text)
		}
		for _, secret := range []string{"acct-bal010-payroll", "40.00", "7.50", "tx-payroll-use"} {
			if strings.Contains(text, secret) {
				t.Fatalf("rejection echoes ledger content %q: %q", secret, text)
			}
		}
	})

	t.Run("hostile domains are refused as unknown", func(t *testing.T) {
		for _, hostile := range []string{"PAYROLL ", "../../etc/passwd", "EQUITY\x00PAYROLL", strings.Repeat("X", 4096)} {
			req := bal010Vectors(t)
			req.Vectors[3].Domain = hostile
			err := mustProve(req)
			bal010Rejected(t, err, "vectors[3].domain", "UNKNOWN_DOMAIN", "1.0.0")
			if strings.Contains(err.Error(), hostile) {
				t.Fatalf("rejection echoes hostile domain %q: %q", hostile, err)
			}
		}
	})
}

// FuzzTodo_BAL_010: hostile domain and dimension strings never panic the
// prover and never escape the typed BAL_010_REJECTED contract.
func FuzzTodo_BAL_010(f *testing.F) {
	f.Add("PAYROLL", "pg-weekly")
	f.Add("EQUITY", "pg-weekly")
	f.Add("", "")
	f.Add("PAYROLL\x00DROP", "pg-weekly\nwithholding_class=supplemental")
	f.Fuzz(func(t *testing.T, domain, dimValue string) {
		req := bal010Vectors(t)
		req.Vectors[0].Domain = domain
		req.Vectors[0].Ledger[1].Dimensions["pay_group"] = dimValue
		got, err := ProveCrossDomainConformance(req)
		if err != nil {
			if !errors.Is(err, ErrCrossDomainRejected) {
				t.Fatalf("untyped prove error for domain %q: %v", domain, err)
			}
			var detail *CrossDomainRejectedError
			if !errors.As(err, &detail) || detail.Field == "" || detail.State == "" {
				t.Fatalf("rejection without field/state: %v", err)
			}
			return
		}
		if len(got.Outcomes) != 5 {
			t.Fatalf("outcomes = %d, want 5", len(got.Outcomes))
		}
	})
}
