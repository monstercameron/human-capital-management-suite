package timecalc

import (
	"math"
	"math/bits"
	"strconv"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Seconds is an exact duration. Worked time is measured between instants,
// so a 25-hour DST day contributes 25 hours.
type Seconds int64

// Hour is one hour of Seconds.
const Hour Seconds = 3600

// Money is an exact amount in ten-thousandths of the currency unit.
type Money int64

// Factor is a multiplier in ten-thousandths: One is 1.0, 15000 is 1.5.
type Factor int64

// One is the unit Factor.
const One Factor = 10000

const moneyScale = 4

func pow10(n int) int64 {
	p := int64(1)
	for ; n > 0; n-- {
		p *= 10
	}
	return p
}

// ParseMoney parses a non-negative fixed-point amount with at most four
// fractional digits ("13.64").
func ParseMoney(s string) (Money, error) {
	whole, frac, _ := strings.Cut(s, ".")
	if whole == "" || len(frac) > moneyScale || strings.HasPrefix(s, "-") || strings.HasPrefix(s, "+") {
		return 0, reject(ErrInvalidRequest, "money", "%q is not a non-negative amount with at most 4 decimals", s)
	}
	w, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, reject(ErrInvalidRequest, "money", "%q: %v", s, err)
	}
	var f int64
	if frac != "" {
		if f, err = strconv.ParseInt(frac, 10, 64); err != nil || f < 0 {
			return 0, reject(ErrInvalidRequest, "money", "%q has a bad fraction", s)
		}
		f *= pow10(moneyScale - len(frac))
	}
	if w > (math.MaxInt64-f)/pow10(moneyScale) {
		return 0, reject(ErrArithmetic, "money", "%q overflows", s)
	}
	return Money(w*pow10(moneyScale) + f), nil
}

// String renders the amount with at least two and at most four decimals.
func (m Money) String() string { return string(m.appendTo(make([]byte, 0, 24))) }

func (m Money) appendTo(b []byte) []byte {
	u := int64(m)
	if u < 0 {
		b = append(b, '-')
		u = -u
	}
	b = strconv.AppendInt(b, u/pow10(moneyScale), 10)
	frac := u % pow10(moneyScale)
	b = append(b, '.', byte('0'+frac/1000), byte('0'+frac/100%10))
	if frac%100 != 0 {
		b = append(b, byte('0'+frac/10%10))
		if frac%10 != 0 {
			b = append(b, byte('0'+frac%10))
		}
	}
	return b
}

// String renders the factor as a decimal multiplier ("1.5").
func (f Factor) String() string {
	s := Money(f).String()
	return strings.TrimSuffix(strings.TrimRight(s, "0"), ".")
}

// String renders the duration as H:MM:SS.
func (s Seconds) String() string { return string(s.appendTo(make([]byte, 0, 16))) }

func (s Seconds) appendTo(b []byte) []byte {
	v := int64(s)
	if v < 0 {
		b = append(b, '-')
		v = -v
	}
	b = strconv.AppendInt(b, v/3600, 10)
	mm, ss := v%3600/60, v%60
	return append(b, ':', byte('0'+mm/10), byte('0'+mm%10), ':', byte('0'+ss/10), byte('0'+ss%10))
}

func two(v int64) string { return string([]byte{byte('0' + v/10), byte('0' + v%10)}) }

// mulDiv returns round(a*b/d) for a, b >= 0 and d > 0 using a 128-bit
// intermediate, so no product overflows and exactly one rounding happens.
func mulDiv(a, b, d int64, mode values.RoundingMode) (int64, error) {
	if a < 0 || b < 0 || d <= 0 {
		return 0, reject(ErrArithmetic, "operand", "mulDiv(%d, %d, %d) needs non-negative operands", a, b, d)
	}
	hi, lo := bits.Mul64(uint64(a), uint64(b))
	if hi >= uint64(d) {
		return 0, reject(ErrArithmetic, "operand", "product overflows")
	}
	q, r := bits.Div64(hi, lo, uint64(d))
	if r != 0 {
		twice, half := 2*r, uint64(d)
		switch mode {
		case values.RoundingHalfUp, values.RoundingHalfAwayFromZero:
			if twice >= half {
				q++
			}
		case values.RoundingHalfEven:
			if twice > half || (twice == half && q%2 == 1) {
				q++
			}
		case values.RoundingTowardZero, values.RoundingFloor:
		case values.RoundingAwayFromZero, values.RoundingCeiling:
			q++
		case values.RoundingExactRequired:
			return 0, reject(ErrArithmetic, "rounding", "inexact result under EXACT_REQUIRED")
		default:
			return 0, reject(ErrArithmetic, "rounding", "rounding mode %s is not declared", mode)
		}
	}
	if q > math.MaxInt64 {
		return 0, reject(ErrArithmetic, "operand", "quotient overflows")
	}
	return int64(q), nil
}

func mulChecked(a, b int64) (int64, error) {
	if a < 0 || b < 0 {
		return 0, reject(ErrArithmetic, "operand", "negative product operand")
	}
	hi, lo := bits.Mul64(uint64(a), uint64(b))
	if hi != 0 || lo > math.MaxInt64 {
		return 0, reject(ErrArithmetic, "operand", "product overflows")
	}
	return int64(lo), nil
}

func addChecked(a, b int64) (int64, error) {
	if (b > 0 && a > math.MaxInt64-b) || (b < 0 && a < math.MinInt64-b) {
		return 0, reject(ErrArithmetic, "sum", "overflow")
	}
	return a + b, nil
}

// rateFor rounds a rate-seconds numerator over a duration to a rate at the
// declared number of decimals.
func rateFor(numerator int64, secs Seconds, decimals int, mode values.RoundingMode) (Money, error) {
	if secs <= 0 {
		return 0, nil
	}
	unit := pow10(moneyScale - decimals)
	q, err := mulDiv(numerator, 1, int64(secs)*unit, mode)
	return Money(q * unit), err
}

// amountFor returns rate x hours rounded once to the declared decimals.
func amountFor(rate Money, secs Seconds, decimals int, mode values.RoundingMode) (Money, error) {
	unit := pow10(moneyScale - decimals)
	q, err := mulDiv(int64(rate), int64(secs), int64(Hour)*unit, mode)
	return Money(q * unit), err
}

// scaleRate returns rate x factor rounded once to the declared decimals.
func scaleRate(rate Money, f Factor, decimals int, mode values.RoundingMode) (Money, error) {
	unit := pow10(moneyScale - decimals)
	q, err := mulDiv(int64(rate), int64(f), int64(One)*unit, mode)
	return Money(q * unit), err
}

// ToDecimal converts an amount to a kernel Decimal at the given scale.
func ToDecimal(m Money, scale int32, mode values.RoundingMode) (values.Decimal, error) {
	d, err := values.NewDecimal(m.String(), moneyScale, mode)
	if err != nil {
		return values.Decimal{}, err
	}
	return d.Quantize(scale, mode)
}

// HoursDecimal converts a duration to decimal hours at the given scale.
func HoursDecimal(s Seconds, scale int32, mode values.RoundingMode) (values.Decimal, error) {
	num, err := values.NewDecimal(strconv.FormatInt(int64(s), 10), 0, mode)
	if err != nil {
		return values.Decimal{}, err
	}
	return num.Div(values.MustDecimal("3600", 0, mode), scale, mode)
}
