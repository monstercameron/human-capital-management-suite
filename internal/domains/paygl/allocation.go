package paygl

import (
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/labor"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ErrDimensionAllocationRejected is the PAYGL-003 refusal boundary.
// Percentages and amounts must sum exactly under fixed-decimal policy and
// every split must bind a governed dimension version.
var ErrDimensionAllocationRejected = errors.New("PAYGL_003_REJECTED")

// DimensionAllocationRequest asks for the exact labor-dimension split of one
// payroll component. Allocation carries the percentage policy; Expected
// optionally carries caller-computed amounts that are checked exactly and
// never trusted.
type DimensionAllocationRequest struct {
	ComponentID   string
	ComponentKind ComponentKind
	ComponentCode string
	Amount        values.Decimal
	Currency      string
	RuleID        string
	RuleVersion   string
	RuleDigest    string
	Allocation    labor.Allocation
	Expected      []DimensionSplit
}

// AllocatedSplit is one exact dimension share of a component.
type AllocatedSplit struct {
	SplitOrdinal int
	Dimension    labor.Dimension
	Percent      values.Decimal
	Amount       values.Decimal
}

// LaborDimensionAllocation is the immutable, content-addressed split of one
// component across governed labor dimensions.
type LaborDimensionAllocation struct {
	ComponentID     string
	ComponentKind   ComponentKind
	ComponentCode   string
	ComponentAmount values.Decimal
	Currency        string
	RuleID          string
	RuleVersion     string
	RuleDigest      string
	Splits          []AllocatedSplit
	Residual        values.Decimal
	Digest          string
}

type remainderCandidate struct {
	entry     labor.AllocationEntry
	quotient  *big.Int
	remainder *big.Int
	ordinal   int
}

// AllocateLaborDimensions splits one component amount across governed labor
// dimensions with deterministic largest-remainder units. The residual is
// always explicit; any imbalance is rejected, never absorbed.
func AllocateLaborDimensions(req DimensionAllocationRequest) (LaborDimensionAllocation, error) {
	fail := func(format string, args ...any) (LaborDimensionAllocation, error) {
		return LaborDimensionAllocation{}, errors.Join(ErrDimensionAllocationRejected, fmt.Errorf(format, args...))
	}
	if strings.TrimSpace(req.ComponentID) == "" {
		return fail("component id is required")
	}
	if !req.ComponentKind.Valid() || strings.TrimSpace(req.ComponentCode) == "" {
		return fail("component kind and code are required")
	}
	if err := req.Amount.Validate(); err != nil {
		return fail("component amount: %v", err)
	}
	if req.Amount.Sign() < 0 {
		return fail("component amount cannot be negative")
	}
	if strings.TrimSpace(req.Currency) == "" {
		return fail("component currency is required")
	}
	if strings.TrimSpace(req.RuleID) == "" || strings.TrimSpace(req.RuleVersion) == "" || strings.TrimSpace(req.RuleDigest) == "" {
		return fail("rule lineage is required")
	}
	if err := req.Allocation.Validate(); err != nil {
		return fail("allocation: %v", err)
	}
	if req.Allocation.RuleID != req.RuleID || req.Allocation.RuleVersion != req.RuleVersion {
		return fail("allocation binds rule %s/%s, want %s/%s", req.Allocation.RuleID, req.Allocation.RuleVersion, req.RuleID, req.RuleVersion)
	}
	for i, entry := range req.Allocation.Entries {
		if strings.TrimSpace(entry.Dimension.Version) == "" {
			return fail("entry %d: dimension version is required", i)
		}
	}
	entries := append([]labor.AllocationEntry(nil), req.Allocation.Entries...)
	sort.Slice(entries, func(i, j int) bool {
		return dimensionKey(entries[i].Dimension) < dimensionKey(entries[j].Dimension)
	})
	denominatorBase := new(big.Int).Mul(big.NewInt(100), pow10(entries[0].Percent.Scale()))
	candidates := make([]remainderCandidate, 0, len(entries))
	for i, entry := range entries {
		numerator := new(big.Int).Mul(req.Amount.Unscaled(), entry.Percent.Unscaled())
		quotient, remainder := new(big.Int).QuoRem(numerator, denominatorBase, new(big.Int))
		candidates = append(candidates, remainderCandidate{entry: entry, quotient: quotient, remainder: remainder, ordinal: i + 1})
	}
	baseTotal := big.NewInt(0)
	for _, candidate := range candidates {
		baseTotal.Add(baseTotal, candidate.quotient)
	}
	residual := new(big.Int).Sub(req.Amount.Unscaled(), baseTotal)
	if residual.Sign() < 0 || !residual.IsInt64() || residual.Int64() > int64(len(candidates)) {
		return fail("residual minor units are invalid")
	}
	ordered := append([]remainderCandidate(nil), candidates...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if comparison := ordered[i].remainder.Cmp(ordered[j].remainder); comparison != 0 {
			return comparison > 0
		}
		if ordered[i].ordinal != ordered[j].ordinal {
			return ordered[i].ordinal < ordered[j].ordinal
		}
		return dimensionKey(ordered[i].entry.Dimension) < dimensionKey(ordered[j].entry.Dimension)
	})
	for i := int64(0); i < residual.Int64(); i++ {
		ordered[i].quotient.Add(ordered[i].quotient, big.NewInt(1))
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return dimensionKey(candidates[i].entry.Dimension) < dimensionKey(candidates[j].entry.Dimension)
	})
	byDimension := map[string]values.Decimal{}
	for _, candidate := range ordered {
		byDimension[dimensionKey(candidate.entry.Dimension)] = mustSplitDecimal(candidate.quotient, req.Amount)
	}
	splits := make([]AllocatedSplit, 0, len(candidates))
	var splitTotal values.Decimal
	for i, candidate := range candidates {
		amount := byDimension[dimensionKey(candidate.entry.Dimension)]
		if i == 0 {
			splitTotal = amount
		} else {
			var err error
			splitTotal, err = splitTotal.Add(amount)
			if err != nil {
				return fail("split total: %v", err)
			}
		}
		splits = append(splits, AllocatedSplit{
			SplitOrdinal: i + 1, Dimension: candidate.entry.Dimension,
			Percent: candidate.entry.Percent, Amount: amount,
		})
	}
	if !splitTotal.Equal(req.Amount) {
		return fail("splits total %s, want %s", splitTotal, req.Amount)
	}
	for _, expected := range req.Expected {
		got, ok := byDimension[dimensionKey(expected.Dimension)]
		if !ok {
			return fail("expected dimension %q is not allocated", dimensionKey(expected.Dimension))
		}
		if expected.Amount.Validate() == nil && !expected.Amount.Equal(got) {
			return fail("dimension %q amount is %s, computed %s", expected.Dimension.Value, expected.Amount, got)
		}
	}
	leftover, err := req.Amount.Sub(splitTotal)
	if err != nil {
		return fail("residual: %v", err)
	}
	if !leftover.IsZero() {
		return fail("residual %s is not explicit zero", leftover)
	}
	out := LaborDimensionAllocation{
		ComponentID: req.ComponentID, ComponentKind: req.ComponentKind, ComponentCode: req.ComponentCode,
		ComponentAmount: req.Amount, Currency: req.Currency,
		RuleID: req.RuleID, RuleVersion: req.RuleVersion, RuleDigest: req.RuleDigest,
		Splits: splits, Residual: leftover,
	}
	out.Digest = canonicalbytes.Digest(out.body())
	return out, nil
}

func mustSplitDecimal(units *big.Int, amount values.Decimal) values.Decimal {
	d, err := values.NewDecimal(minorUnitText(units, amount.Scale()), amount.Scale(), amount.Rounding())
	if err != nil {
		panic(err)
	}
	return d
}

func (a LaborDimensionAllocation) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.paygl.LaborDimensionAllocation", 1).
		String("component_id", a.ComponentID).String("component_kind", string(a.ComponentKind)).
		String("component_code", a.ComponentCode).Value("component_amount", a.ComponentAmount).
		String("currency", a.Currency).String("rule_id", a.RuleID).
		String("rule_version", a.RuleVersion).String("rule_digest", a.RuleDigest).
		Value("residual", a.Residual).Count("splits", len(a.Splits))
	for _, split := range a.Splits {
		w.Int("split_ordinal", int64(split.SplitOrdinal)).Value("dimension", split.Dimension).
			Value("percent", split.Percent).Value("amount", split.Amount)
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Validate rechecks split conservation, lineage and digest binding.
func (a LaborDimensionAllocation) Validate() error {
	fail := func(format string, args ...any) error {
		return errors.Join(ErrDimensionAllocationRejected, fmt.Errorf(format, args...))
	}
	if strings.TrimSpace(a.ComponentID) == "" || !a.ComponentKind.Valid() || strings.TrimSpace(a.ComponentCode) == "" {
		return fail("component identity is required")
	}
	if strings.TrimSpace(a.RuleID) == "" || strings.TrimSpace(a.RuleVersion) == "" || strings.TrimSpace(a.RuleDigest) == "" {
		return fail("rule lineage is required")
	}
	if len(a.Splits) == 0 {
		return fail("at least one split is required")
	}
	seen := map[string]struct{}{}
	var total values.Decimal
	for i, split := range a.Splits {
		if err := split.Dimension.Validate(); err != nil {
			return fail("split %d: %v", i, err)
		}
		key := dimensionKey(split.Dimension)
		if _, ok := seen[key]; ok {
			return fail("split %d duplicates dimension", i)
		}
		seen[key] = struct{}{}
		if i == 0 {
			total = split.Amount
		} else {
			var err error
			total, err = total.Add(split.Amount)
			if err != nil {
				return fail("split %d: %v", i, err)
			}
		}
	}
	if !total.Equal(a.ComponentAmount) {
		return fail("splits total %s, want %s", total, a.ComponentAmount)
	}
	if !a.Residual.IsZero() {
		return fail("residual %s is not explicit zero", a.Residual)
	}
	if a.Digest != canonicalbytes.Digest(a.body()) {
		return fail("canonical digest mismatch")
	}
	return nil
}
