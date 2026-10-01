package punchpolicy

import (
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// TipDeclaration is the worker's own statement of tips received for a
// shift. It is a statement, not a payroll fact: later corrections are
// TipAdjustment entries appended to the same TipRecord, never edits to the
// declaration itself.
type TipDeclaration struct {
	WorkerID   string
	ShiftID    string
	DeclaredBy string
	DeclaredAt time.Time
	Amount     values.Decimal
}

func (d TipDeclaration) validate() error {
	if strings.TrimSpace(d.WorkerID) == "" || strings.TrimSpace(d.ShiftID) == "" {
		return fmt.Errorf("%w: tip declaration requires a worker and a shift", ErrInvalidTipDeclaration)
	}
	if strings.TrimSpace(d.DeclaredBy) == "" {
		return fmt.Errorf("%w: tip declaration requires who declared it", ErrInvalidTipDeclaration)
	}
	if d.DeclaredAt.IsZero() {
		return fmt.Errorf("%w: tip declaration requires when it was declared", ErrInvalidTipDeclaration)
	}
	if err := d.Amount.Validate(); err != nil {
		return fmt.Errorf("%w: declared amount: %v", ErrInvalidTipDeclaration, err)
	}
	if d.Amount.Sign() < 0 {
		return fmt.Errorf("%w: declared amount must not be negative", ErrInvalidTipDeclaration)
	}
	return nil
}

// TipAdjustment is one later correction to a declaration. It is never
// applied by rewriting the declaration: it is appended to the record's
// adjustment list, so every prior statement stays exactly as declared.
type TipAdjustment struct {
	Amount     values.Decimal
	Reason     string
	AdjustedBy string
	AdjustedAt time.Time
}

func (a TipAdjustment) validate() error {
	if err := a.Amount.Validate(); err != nil {
		return fmt.Errorf("%w: adjustment amount: %v", ErrInvalidTipDeclaration, err)
	}
	if strings.TrimSpace(a.Reason) == "" {
		return fmt.Errorf("%w: adjustment requires a reason", ErrInvalidTipDeclaration)
	}
	if strings.TrimSpace(a.AdjustedBy) == "" {
		return fmt.Errorf("%w: adjustment requires who made it", ErrInvalidTipDeclaration)
	}
	if a.AdjustedAt.IsZero() {
		return fmt.Errorf("%w: adjustment requires when it was made", ErrInvalidTipDeclaration)
	}
	return nil
}

// TipRecord is a worker's declaration and every adjustment later appended
// to it, in the order they were made.
type TipRecord struct {
	Declaration TipDeclaration
	Adjustments []TipAdjustment
}

// NewTipDeclaration validates and starts a TipRecord from the worker's own
// statement, with no adjustments yet.
func NewTipDeclaration(d TipDeclaration) (TipRecord, error) {
	if err := d.validate(); err != nil {
		return TipRecord{}, err
	}
	return TipRecord{Declaration: d}, nil
}

// Adjust appends adj to rec's adjustment list and returns the new record.
// rec is never mutated and no existing adjustment is ever removed or
// rewritten: Adjust only ever grows the list.
func Adjust(rec TipRecord, adj TipAdjustment) (TipRecord, error) {
	if err := adj.validate(); err != nil {
		return TipRecord{}, err
	}
	out := TipRecord{
		Declaration: rec.Declaration,
		Adjustments: make([]TipAdjustment, len(rec.Adjustments)+1),
	}
	copy(out.Adjustments, rec.Adjustments)
	out.Adjustments[len(rec.Adjustments)] = adj
	return out, nil
}

// Total sums the declaration and every appended adjustment, quantizing each
// to a common scale and mode before adding so Decimal.Add's equal-scale
// requirement always holds regardless of how each amount was declared.
func (r TipRecord) Total(scale int32, mode values.RoundingMode) (values.Decimal, error) {
	total, err := r.Declaration.Amount.Quantize(scale, mode)
	if err != nil {
		return values.Decimal{}, fmt.Errorf("declaration: %w", err)
	}
	for i, adj := range r.Adjustments {
		amt, err := adj.Amount.Quantize(scale, mode)
		if err != nil {
			return values.Decimal{}, fmt.Errorf("adjustment[%d]: %w", i, err)
		}
		total, err = total.Add(amt)
		if err != nil {
			return values.Decimal{}, fmt.Errorf("adjustment[%d]: %w", i, err)
		}
	}
	return total, nil
}
