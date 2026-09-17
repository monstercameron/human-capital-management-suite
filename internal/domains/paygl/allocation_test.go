package paygl

import (
	"errors"
	"math/rand"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/labor"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func allocationRequest(t *testing.T) DimensionAllocationRequest {
	t.Helper()
	rule := mappingRule(t, "salary", "SALARY", "6001", "2101", mappingDimension("cc-1"))
	digest, err := rule.Digest()
	if err != nil {
		t.Fatal(err)
	}
	return DimensionAllocationRequest{
		ComponentID:   "component-1",
		ComponentKind: ComponentEarning,
		ComponentCode: "SALARY",
		Amount:        glDecimal(t, "100.01"),
		Currency:      "USD",
		RuleID:        rule.ID,
		RuleVersion:   rule.Version,
		RuleDigest:    digest,
		Allocation: labor.Allocation{
			RuleID: rule.ID, RuleVersion: rule.Version,
			Entries: []labor.AllocationEntry{
				{Dimension: mappingDimension("cc-1"), Percent: mappingPercent(t, "33.33")},
				{Dimension: mappingDimension("cc-2"), Percent: mappingPercent(t, "33.33")},
				{Dimension: mappingDimension("cc-3"), Percent: mappingPercent(t, "33.34")},
			},
		},
	}
}

// TestTodo_PAYGL_003 is the primary acceptance case: percentages and amounts
// sum exactly under fixed-decimal policy, dimensions bind governed versions,
// and the residual is explicit.
func TestTodo_PAYGL_003(t *testing.T) {
	got, err := AllocateLaborDimensions(allocationRequest(t))
	if err != nil {
		t.Fatalf("AllocateLaborDimensions: %v", err)
	}
	rejectEmptyPayGLDigest(t, got.Digest)
	if len(got.Splits) != 3 {
		t.Fatalf("splits = %d, want 3", len(got.Splits))
	}
	// 100.01 split 33.33/33.33/33.34 by largest remainder:
	// floors 33.33/33.33/33.34 with one cent of remainder to cc-3.
	want := map[string]string{"cc-1": "33.33", "cc-2": "33.33", "cc-3": "33.35"}
	sum := glDecimal(t, "0.00")
	for _, split := range got.Splits {
		if want[split.Dimension.Value] != split.Amount.String() {
			t.Fatalf("%s = %s, want %s", split.Dimension.Value, split.Amount, want[split.Dimension.Value])
		}
		var err error
		sum, err = sum.Add(split.Amount)
		if err != nil {
			t.Fatal(err)
		}
	}
	if !sum.Equal(glDecimal(t, "100.01")) {
		t.Fatalf("splits sum %s, want 100.01", sum)
	}
	if !got.Residual.IsZero() {
		t.Fatalf("residual = %s, want explicit zero", got.Residual)
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	cases := map[string]func(*DimensionAllocationRequest){
		"percentages short": func(r *DimensionAllocationRequest) {
			r.Allocation.Entries[2].Percent = mappingPercent(t, "33.33")
		},
		"stale rule version": func(r *DimensionAllocationRequest) {
			r.Allocation.RuleVersion = "v0"
		},
		"rule digest missing": func(r *DimensionAllocationRequest) {
			r.RuleDigest = ""
		},
		"dimension version missing": func(r *DimensionAllocationRequest) {
			dim := mappingDimension("cc-1")
			dim.Version = ""
			r.Allocation.Entries[0].Dimension = dim
		},
		"duplicate dimension": func(r *DimensionAllocationRequest) {
			r.Allocation.Entries[1].Dimension = mappingDimension("cc-1")
			r.Allocation.Entries[1].Percent = mappingPercent(t, "33.33")
		},
		"expected amount wrong": func(r *DimensionAllocationRequest) {
			r.Expected = []DimensionSplit{{
				Dimension: mappingDimension("cc-1"),
				Percent:   mappingPercent(t, "33.33"),
				Amount:    glDecimal(t, "33.34"),
			}}
		},
		"negative amount": func(r *DimensionAllocationRequest) {
			d, err := values.NewDecimal("-1.00", 2, values.RoundingHalfUp)
			if err != nil {
				t.Fatal(err)
			}
			r.Amount = d
		},
	}
	for name, mutate := range cases {
		req := allocationRequest(t)
		mutate(&req)
		if _, err := AllocateLaborDimensions(req); !errors.Is(err, ErrDimensionAllocationRejected) {
			t.Fatalf("%s: err = %v, want PAYGL_003_REJECTED", name, err)
		}
	}
	// A correct caller-supplied expectation is accepted.
	withExpected := allocationRequest(t)
	withExpected.Expected = []DimensionSplit{{
		Dimension: mappingDimension("cc-3"),
		Percent:   mappingPercent(t, "33.34"),
		Amount:    glDecimal(t, "33.35"),
	}}
	if _, err := AllocateLaborDimensions(withExpected); err != nil {
		t.Fatalf("correct expectation: %v", err)
	}
}

// TestTodo_PAYGL_003_Property proves every allocation conserves the
// component amount exactly and is deterministic across entry orders.
func TestTodo_PAYGL_003_Property(t *testing.T) {
	rng := rand.New(rand.NewSource(20260917))
	for i := 0; i < 48; i++ {
		req := allocationRequest(t)
		splits := 2 + rng.Intn(3)
		cents := 1 + rng.Intn(99999)
		req.Amount = glDecimal(t, itoaPayGL(cents))
		// Random exact percentages at scale 2 that sum to 100.00.
		bps := make([]int, splits)
		rest := 10000
		for s := 0; s < splits-1; s++ {
			v := rng.Intn(rest + 1)
			bps[s] = v
			rest -= v
		}
		bps[splits-1] = rest
		req.Allocation.Entries = req.Allocation.Entries[:0]
		for s, bp := range bps {
			pct, err := values.NewDecimal(itoaPayGLWhole(bp/100)+"."+itoaPadPayGL(bp%100)+"00", 4, values.RoundingExactRequired)
			if err != nil {
				t.Fatal(err)
			}
			req.Allocation.Entries = append(req.Allocation.Entries, labor.AllocationEntry{
				Dimension: mappingDimension("cc-" + itoaPayGLWhole(s)),
				Percent:   pct,
			})
		}
		got, err := AllocateLaborDimensions(req)
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		sum := glDecimal(t, "0.00")
		for _, split := range got.Splits {
			var err error
			sum, err = sum.Add(split.Amount)
			if err != nil {
				t.Fatal(err)
			}
		}
		if !sum.Equal(req.Amount) {
			t.Fatalf("case %d: splits sum %s, want %s", i, sum, req.Amount)
		}
		shuffled := allocationRequest(t)
		shuffled.Amount = req.Amount
		shuffled.Allocation = req.Allocation
		entries := append([]labor.AllocationEntry(nil), req.Allocation.Entries...)
		for s := range entries {
			other := rng.Intn(len(entries))
			entries[s], entries[other] = entries[other], entries[s]
		}
		shuffled.Allocation.Entries = entries
		again, err := AllocateLaborDimensions(shuffled)
		if err != nil {
			t.Fatalf("case %d shuffled: %v", i, err)
		}
		if again.Digest != got.Digest {
			t.Fatalf("case %d: digest depends on entry order", i)
		}
	}
}

// TestTodo_PAYGL_003_Mutation proves the digest binds component, rule and
// every split: any change yields a new digest and tampering fails.
func TestTodo_PAYGL_003_Mutation(t *testing.T) {
	base, err := AllocateLaborDimensions(allocationRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	mutations := []func(*DimensionAllocationRequest){
		func(r *DimensionAllocationRequest) { r.Amount = glDecimal(t, "100.02") },
		func(r *DimensionAllocationRequest) { r.RuleVersion = "v2"; r.Allocation.RuleVersion = "v2" },
		func(r *DimensionAllocationRequest) {
			r.Allocation.Entries[0].Percent = mappingPercent(t, "33.32")
			r.Allocation.Entries[2].Percent = mappingPercent(t, "33.35")
		},
		func(r *DimensionAllocationRequest) {
			r.Allocation.Entries[0].Dimension = mappingDimension("cc-9")
		},
		func(r *DimensionAllocationRequest) { r.Currency = "EUR" },
	}
	for i, mutate := range mutations {
		req := allocationRequest(t)
		mutate(&req)
		mutated, err := AllocateLaborDimensions(req)
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		if mutated.Digest == base.Digest {
			t.Fatalf("case %d did not change the digest", i)
		}
	}
	tampered := base
	tampered.Splits[0].Amount = glDecimal(t, "0.01")
	if err := tampered.Validate(); err == nil {
		t.Fatal("tampered split passed validation")
	}
	forged := base
	forged.Digest = "sha256:forged"
	if err := forged.Validate(); err == nil {
		t.Fatal("forged digest passed validation")
	}
}

func itoaPayGL(cents int) string {
	return itoaPayGLWhole(cents/100) + "." + itoaPadPayGL(cents%100)
}

func itoaPayGLWhole(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [32]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[pos:])
}

func itoaPadPayGL(n int) string {
	return string([]byte{byte('0' + n/10), byte('0' + n%10)})
}

func rejectEmptyPayGLDigest(t *testing.T, digest string) {
	t.Helper()
	if digest == "" || digest == "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Fatalf("digest %q is empty or the empty-payload hash", digest)
	}
}
