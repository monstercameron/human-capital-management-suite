package promotion

import (
	"errors"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ErrBasePayBoundsInvalid means a proposed ladder edge cannot yield a usable
// monetary range. Callers must not offer a guessed correction in that state.
var ErrBasePayBoundsInvalid = errors.New("promotion: base-pay bounds are invalid")

// BasePayBounds is the inclusive range of monetary amounts the ladder edge
// admits at the current pay's declared scale. Ceiling the lower threshold and
// flooring the upper threshold preserve the exact server predicate at cents.
func BasePayBounds(current values.Money, minimum, maximum values.Percentage) (values.Money, values.Money, error) {
	if current.Validate() != nil || minimum.Validate() != nil || maximum.Validate() != nil ||
		current.Amount().Sign() <= 0 || minimum.Fraction().Sign() < 0 ||
		maximum.Fraction().Cmp(minimum.Fraction()) < 0 {
		return values.Money{}, values.Money{}, ErrBasePayBoundsInvalid
	}
	min, err := ladderBound(current, minimum, values.RoundingCeiling)
	if err != nil {
		return values.Money{}, values.Money{}, err
	}
	max, err := ladderBound(current, maximum, values.RoundingFloor)
	if err != nil {
		return values.Money{}, values.Money{}, err
	}
	if min.Amount().Cmp(max.Amount()) > 0 {
		return values.Money{}, values.Money{}, ErrBasePayBoundsInvalid
	}
	return min, max, nil
}

func ladderBound(current values.Money, rate values.Percentage, rounding values.RoundingMode) (values.Money, error) {
	scale := current.Amount().Scale()
	delta, err := current.Amount().Mul(rate.Fraction(), scale+rate.Fraction().Scale(), values.RoundingExactRequired)
	if err != nil {
		return values.Money{}, err
	}
	delta, err = delta.Quantize(scale, rounding)
	if err != nil {
		return values.Money{}, err
	}
	amount, err := current.Amount().Add(delta)
	if err != nil {
		return values.Money{}, err
	}
	return values.NewMoneyFromDecimal(amount, current.Currency())
}
