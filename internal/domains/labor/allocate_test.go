package labor

import (
	"errors"
	"math/rand"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func allocDecimal(t *testing.T, text string) values.Decimal {
	t.Helper()
	d, err := values.NewDecimal(text, 2, values.RoundingHalfUp)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func allocRequest(t *testing.T) WorkAllocationRequest {
	t.Helper()
	rule := validLaborRule(t)
	return WorkAllocationRequest{
		SourceID:         "time-week-26",
		SourceLabel:      "MINUTES",
		Rule:             rule,
		RuleVersion:      rule.Version,
		SourceAmount:     allocDecimal(t, "480.00"),
		DeclaredResidual: allocDecimal(t, "30.00"),
		Shares: []TimeShare{
			{Dimension: dimension(DimensionCostCenter, "cc-100"), Amount: allocDecimal(t, "300.00")},
			{Dimension: dimension(DimensionProject, "proj-7"), Amount: allocDecimal(t, "150.00")},
		},
	}
}

// TestTodo_LABOR_002 is the primary acceptance case: exact shares plus an
// explicit residual conserve the source, and every inexact, stale, invalid
// or overlapping allocation is rejected without an authoritative effect.
func TestTodo_LABOR_002(t *testing.T) {
	got, err := AllocateWorkedTime(allocRequest(t))
	if err != nil {
		t.Fatalf("AllocateWorkedTime: %v", err)
	}
	rejectEmptyDigest(t, got.Digest)
	sum, err := got.Shares[0].Amount.Add(got.Shares[1].Amount)
	if err != nil {
		t.Fatal(err)
	}
	total, err := sum.Add(got.Residual)
	if err != nil {
		t.Fatal(err)
	}
	if !total.Equal(allocDecimal(t, "480.00")) {
		t.Fatalf("shares plus residual = %s, want 480.00", total)
	}
	if !got.Residual.Equal(allocDecimal(t, "30.00")) {
		t.Fatalf("residual = %s, want explicit 30.00", got.Residual)
	}

	cases := map[string]func(*WorkAllocationRequest){
		"unbalanced": func(r *WorkAllocationRequest) {
			r.Shares[0].Amount = allocDecimal(t, "299.99")
		},
		"hidden residual": func(r *WorkAllocationRequest) {
			r.DeclaredResidual = allocDecimal(t, "0.00")
		},
		"over-allocated": func(r *WorkAllocationRequest) {
			r.Shares[0].Amount = allocDecimal(t, "400.00")
		},
		"duplicate dimension": func(r *WorkAllocationRequest) {
			r.Shares[1] = r.Shares[0]
			r.DeclaredResidual = allocDecimal(t, "180.00")
		},
		"invalid dimension kind": func(r *WorkAllocationRequest) {
			r.Shares[1].Dimension = Dimension{Kind: "WORKTAG", Value: "wt-1", Version: "v1"}
		},
		"dimension outside rule": func(r *WorkAllocationRequest) {
			narrow := validLaborRule(t)
			narrow.Dimensions = []DimensionKind{DimensionCostCenter}
			var err error
			narrow, err = NewLaborRule(narrow)
			if err != nil {
				t.Fatal(err)
			}
			r.Rule = narrow
			r.RuleVersion = narrow.Version
		},
		"stale rule version": func(r *WorkAllocationRequest) {
			r.RuleVersion = "v0"
		},
		"negative share": func(r *WorkAllocationRequest) {
			d, err := values.NewDecimal("-1.00", 2, values.RoundingHalfUp)
			if err != nil {
				t.Fatal(err)
			}
			r.Shares[0].Amount = d
			r.DeclaredResidual = allocDecimal(t, "181.00")
		},
		"missing source": func(r *WorkAllocationRequest) {
			r.SourceID = ""
		},
	}
	for name, mutate := range cases {
		req := allocRequest(t)
		mutate(&req)
		if _, err := AllocateWorkedTime(req); !errors.Is(err, ErrTimeAllocationRejected) {
			t.Fatalf("%s: err = %v, want LABOR_002_REJECTED", name, err)
		}
	}
}

// TestTodo_LABOR_002_Property proves conservation over generated splits and
// that the digest is stable under share permutation.
func TestTodo_LABOR_002_Property(t *testing.T) {
	rng := rand.New(rand.NewSource(20260917))
	for i := 0; i < 64; i++ {
		req := allocRequest(t)
		splits := 1 + rng.Intn(4)
		totalCents := 100 + rng.Intn(48000)
		cents := make([]int, splits)
		rest := totalCents
		for s := 0; s < splits-1; s++ {
			v := rng.Intn(rest + 1)
			cents[s] = v
			rest -= v
		}
		cents[splits-1] = rest
		kinds := []DimensionKind{DimensionCostCenter, DimensionProject, DimensionActivity, DimensionFundingSource, DimensionLocation}
		req.Shares = req.Shares[:0]
		for s, c := range cents {
			whole, frac := c/100, c%100
			text := itoa2(whole, frac)
			req.Shares = append(req.Shares, TimeShare{
				Dimension: Dimension{Kind: kinds[s%len(kinds)], Value: string(rune('a'+s)) + "-val", Version: "v1"},
				Amount:    allocDecimal(t, text),
			})
		}
		sourceText := itoa2(totalCents/100, totalCents%100)
		req.SourceAmount = allocDecimal(t, sourceText)
		req.DeclaredResidual = allocDecimal(t, "0.00")
		got, err := AllocateWorkedTime(req)
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		sum := allocDecimal(t, "0.00")
		for _, share := range got.Shares {
			var err error
			sum, err = sum.Add(share.Amount)
			if err != nil {
				t.Fatal(err)
			}
		}
		if !sum.Equal(req.SourceAmount) {
			t.Fatalf("case %d: conserved %s, want %s", i, sum, req.SourceAmount)
		}
		shuffled := allocRequest(t)
		shuffled.SourceAmount = req.SourceAmount
		shuffled.DeclaredResidual = req.DeclaredResidual
		shuffled.Shares = append([]TimeShare(nil), req.Shares...)
		for s := range shuffled.Shares {
			other := rng.Intn(len(shuffled.Shares))
			shuffled.Shares[s], shuffled.Shares[other] = shuffled.Shares[other], shuffled.Shares[s]
		}
		again, err := AllocateWorkedTime(shuffled)
		if err != nil {
			t.Fatalf("case %d shuffled: %v", i, err)
		}
		if again.Digest != got.Digest {
			t.Fatalf("case %d: digest depends on share order", i)
		}
	}
}

// rejectEmptyDigest fails when a digest is missing or is the well-known
// empty-payload hash, which signals a broken canonical body.
func rejectEmptyDigest(t *testing.T, digest string) {
	t.Helper()
	if digest == "" || digest == "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Fatalf("digest %q is empty or the empty-payload hash", digest)
	}
}

func itoa2(whole, frac int) string {
	return itoa(whole) + "." + itoaPad(frac)
}

func itoa(n int) string {
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

func itoaPad(n int) string {
	return string([]byte{byte('0' + n/10), byte('0' + n%10)})
}
