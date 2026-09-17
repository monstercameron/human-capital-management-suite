package labor

import (
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ErrTimeAllocationRejected is the LABOR-002 refusal boundary. Allocation is
// pure; rejecting here means that no authoritative labor cost exists.
var ErrTimeAllocationRejected = errors.New("LABOR_002_REJECTED")

// TimeShare assigns one exact amount of worked time or cost to one governed
// labor dimension value.
type TimeShare struct {
	Dimension Dimension
	Amount    values.Decimal
}

// WorkAllocationRequest binds a source of worked time to exact dimension
// shares. DeclaredResidual is the caller's explicit leftover: the
// constructor computes the residual independently and refuses to absorb an
// undeclared difference silently.
type WorkAllocationRequest struct {
	SourceID         string
	SourceLabel      string
	Rule             LaborRule
	RuleVersion      string
	SourceAmount     values.Decimal
	Shares           []TimeShare
	DeclaredResidual values.Decimal
}

// WorkedTimeAllocation is the immutable result of allocating worked time
// exactly across governed dimensions.
type WorkedTimeAllocation struct {
	SourceID     string
	SourceLabel  string
	RuleID       string
	RuleVersion  string
	SourceAmount values.Decimal
	Shares       []TimeShare
	Residual     values.Decimal
	Digest       string
}

// AllocateWorkedTime splits a source amount across governed dimensions so
// that shares plus the explicit residual conserve the source exactly.
func AllocateWorkedTime(req WorkAllocationRequest) (WorkedTimeAllocation, error) {
	fail := func(format string, args ...any) (WorkedTimeAllocation, error) {
		return WorkedTimeAllocation{}, errors.Join(ErrTimeAllocationRejected, fmt.Errorf(format, args...))
	}
	if strings.TrimSpace(req.SourceID) == "" {
		return fail("source id is required")
	}
	if strings.TrimSpace(req.SourceLabel) == "" {
		return fail("source label is required")
	}
	if err := req.Rule.Validate(); err != nil {
		return fail("rule: %v", err)
	}
	if req.RuleVersion != req.Rule.Version {
		return fail("allocation binds rule version %q, want %q", req.RuleVersion, req.Rule.Version)
	}
	if err := req.SourceAmount.Validate(); err != nil {
		return fail("source amount: %v", err)
	}
	if req.SourceAmount.Sign() < 0 {
		return fail("source amount cannot be negative")
	}
	if len(req.Shares) == 0 {
		return fail("at least one share is required")
	}
	allowed := make(map[DimensionKind]struct{}, len(req.Rule.Dimensions))
	for _, kind := range req.Rule.Dimensions {
		allowed[kind] = struct{}{}
	}
	scale, rounding := req.SourceAmount.Scale(), req.SourceAmount.Rounding()
	seen := make(map[string]struct{}, len(req.Shares))
	sum := req.SourceAmount
	zero := true
	for i, share := range req.Shares {
		if err := share.Dimension.Validate(); err != nil {
			return fail("share %d: %v", i, err)
		}
		if _, ok := allowed[share.Dimension.Kind]; !ok {
			return fail("share %d: dimension %q is outside rule %s", i, share.Dimension.Kind, req.Rule.ID)
		}
		if err := share.Amount.Validate(); err != nil {
			return fail("share %d amount: %v", i, err)
		}
		if share.Amount.Scale() != scale || share.Amount.Rounding() != rounding {
			return fail("share %d precision differs from source", i)
		}
		if share.Amount.Sign() < 0 {
			return fail("share %d amount cannot be negative", i)
		}
		key := share.Dimension.Kind.String() + "\x00" + share.Dimension.Value + "\x00" + share.Dimension.Version
		if _, ok := seen[key]; ok {
			return fail("share %d overlaps dimension %q", i, key)
		}
		seen[key] = struct{}{}
		var err error
		if zero {
			sum = req.SourceAmount
			zero = false
		}
		sum, err = sum.Sub(share.Amount)
		if err != nil {
			return fail("share %d: %v", i, err)
		}
	}
	if sum.Sign() < 0 {
		return fail("shares exceed source by %s", mustNegate(sum))
	}
	if err := req.DeclaredResidual.Validate(); err != nil {
		return fail("declared residual: %v", err)
	}
	if req.DeclaredResidual.Scale() != scale || req.DeclaredResidual.Rounding() != rounding {
		return fail("declared residual precision differs from source")
	}
	if req.DeclaredResidual.Sign() < 0 {
		return fail("declared residual cannot be negative")
	}
	if !sum.Equal(req.DeclaredResidual) {
		return fail("%v: residual is %s, declared %s", ErrAllocationUnbalanced, sum, req.DeclaredResidual)
	}
	out := WorkedTimeAllocation{
		SourceID:     req.SourceID,
		SourceLabel:  req.SourceLabel,
		RuleID:       req.Rule.ID,
		RuleVersion:  req.Rule.Version,
		SourceAmount: req.SourceAmount,
		Shares:       append([]TimeShare(nil), req.Shares...),
		Residual:     sum,
	}
	out.Digest = canonicalbytes.Digest(out.body())
	return out, nil
}

func mustNegate(d values.Decimal) values.Decimal {
	neg, err := d.Neg()
	if err != nil {
		return d
	}
	return neg
}

func (a WorkedTimeAllocation) body() []byte {
	shares := append([]TimeShare(nil), a.Shares...)
	sortShares(shares)
	w := canonicalbytes.New("hcmnext.domains.labor.WorkedTimeAllocation", 1).
		String("source_id", a.SourceID).String("source_label", a.SourceLabel).
		String("rule_id", a.RuleID).String("rule_version", a.RuleVersion).
		Value("source_amount", a.SourceAmount).Value("residual", a.Residual).
		Count("shares", len(shares))
	for _, share := range shares {
		w.Value("dimension", share.Dimension).Value("amount", share.Amount)
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Validate rechecks a decoded allocation: shape, conservation and digest.
// Rule-version binding is a constructor concern; the digest retains it.
func (a WorkedTimeAllocation) Validate() error {
	fail := func(format string, args ...any) error {
		return errors.Join(ErrTimeAllocationRejected, fmt.Errorf(format, args...))
	}
	if strings.TrimSpace(a.SourceID) == "" || strings.TrimSpace(a.SourceLabel) == "" {
		return fail("source is required")
	}
	if strings.TrimSpace(a.RuleID) == "" || strings.TrimSpace(a.RuleVersion) == "" {
		return fail("rule lineage is required")
	}
	if len(a.Shares) == 0 {
		return fail("at least one share is required")
	}
	if err := a.SourceAmount.Validate(); err != nil {
		return fail("source amount: %v", err)
	}
	if err := a.Residual.Validate(); err != nil {
		return fail("residual: %v", err)
	}
	scale, rounding := a.SourceAmount.Scale(), a.SourceAmount.Rounding()
	if a.Residual.Scale() != scale || a.Residual.Rounding() != rounding {
		return fail("residual precision differs from source")
	}
	sum := a.Residual
	seen := map[string]struct{}{}
	for i, share := range a.Shares {
		if err := share.Dimension.Validate(); err != nil {
			return fail("share %d: %v", i, err)
		}
		if share.Amount.Scale() != scale || share.Amount.Rounding() != rounding {
			return fail("share %d precision differs from source", i)
		}
		if share.Amount.Sign() < 0 {
			return fail("share %d amount cannot be negative", i)
		}
		key := shareKey(share)
		if _, ok := seen[key]; ok {
			return fail("share %d overlaps dimension", i)
		}
		seen[key] = struct{}{}
		next, err := sum.Add(share.Amount)
		if err != nil {
			return fail("share %d: %v", i, err)
		}
		sum = next
	}
	if !sum.Equal(a.SourceAmount) {
		return fail("shares plus residual do not conserve source")
	}
	if a.Digest != canonicalbytes.Digest(a.body()) {
		return fail("canonical digest mismatch")
	}
	return nil
}

func sortShares(shares []TimeShare) {
	for i := 1; i < len(shares); i++ {
		for j := i; j > 0 && shareKey(shares[j]) < shareKey(shares[j-1]); j-- {
			shares[j], shares[j-1] = shares[j-1], shares[j]
		}
	}
}

func shareKey(s TimeShare) string {
	return s.Dimension.Kind.String() + "\x00" + s.Dimension.Value + "\x00" + s.Dimension.Version
}
