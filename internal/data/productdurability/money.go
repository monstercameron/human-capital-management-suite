package productdurability

import (
	"errors"
	"fmt"
	"math"
	"strings"
)

// ALIGN-035: money is stored exactly. Amounts are integer minor units in a
// named currency with a pinned scale; floats never appear, and parsing
// refuses any decimal that would need rounding. Arithmetic requires a
// shared currency and refuses overflow, so a stored amount is always the
// amount that was meant.

// Money errors.
var (
	ErrMoneyInvalid  = errors.New("productdurability: money is invalid")
	ErrMoneyCurrency = errors.New("productdurability: currency is not admitted")
	ErrMoneyInexact  = errors.New("productdurability: amount needs rounding to fit its currency scale")
	ErrMoneyMismatch = errors.New("productdurability: money currencies differ")
	ErrMoneyOverflow = errors.New("productdurability: money arithmetic overflows")
)

// currencyScales pins the minor-unit scale of every admitted currency.
var currencyScales = map[string]int{
	"USD": 2, "EUR": 2, "GBP": 2, "CAD": 2, "AUD": 2,
	"JPY": 0,
}

// Money is an exact amount: integer minor units plus currency.
type Money struct {
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
}

func currencyScale(currency string) (int, error) {
	scale, ok := currencyScales[currency]
	if !ok {
		return 0, fmt.Errorf("%w: %q", ErrMoneyCurrency, currency)
	}
	return scale, nil
}

func pow10(n int) int64 {
	p := int64(1)
	for i := 0; i < n; i++ {
		p *= 10
	}
	return p
}

// ParseMoneyExact parses a decimal amount into exact minor units. Up to
// scale fractional digits are accepted and padded; more digits are refused
// rather than rounded. No float is consulted at any point.
func ParseMoneyExact(amount, currency string) (Money, error) {
	scale, err := currencyScale(currency)
	if err != nil {
		return Money{}, err
	}
	text := strings.TrimSpace(amount)
	if text == "" {
		return Money{}, fmt.Errorf("%w: amount is required", ErrMoneyInvalid)
	}
	negative := false
	if strings.HasPrefix(text, "-") {
		negative = true
		text = text[1:]
	} else if strings.HasPrefix(text, "+") {
		text = text[1:]
	}
	parts := strings.Split(text, ".")
	if len(parts) > 2 {
		return Money{}, fmt.Errorf("%w: %q is not a decimal amount", ErrMoneyInvalid, amount)
	}
	intPart, fracPart := parts[0], ""
	if len(parts) == 2 {
		fracPart = parts[1]
	}
	if intPart == "" || strings.Trim(intPart, "0123456789") != "" {
		return Money{}, fmt.Errorf("%w: %q is not a decimal amount", ErrMoneyInvalid, amount)
	}
	if strings.Trim(fracPart, "0123456789") != "" {
		return Money{}, fmt.Errorf("%w: %q is not a decimal amount", ErrMoneyInvalid, amount)
	}
	if len(fracPart) > scale {
		return Money{}, fmt.Errorf("%w: %q exceeds the %s scale of %d", ErrMoneyInexact, amount, currency, scale)
	}
	var whole, frac int64
	for _, digit := range intPart {
		whole = whole*10 + int64(digit-'0')
		if whole > (math.MaxInt64-pow10(scale))/pow10(scale) {
			return Money{}, fmt.Errorf("%w: %q", ErrMoneyOverflow, amount)
		}
	}
	for _, digit := range fracPart {
		frac = frac*10 + int64(digit-'0')
	}
	for i := len(fracPart); i < scale; i++ {
		frac *= 10
	}
	minor := whole*pow10(scale) + frac
	if negative {
		minor = -minor
	}
	return Money{AmountMinor: minor, Currency: currency}, nil
}

// Add sums two amounts in the same currency, refusing overflow.
func (m Money) Add(other Money) (Money, error) {
	if m.Currency != other.Currency {
		return Money{}, fmt.Errorf("%w: %s vs %s", ErrMoneyMismatch, m.Currency, other.Currency)
	}
	if _, err := currencyScale(m.Currency); err != nil {
		return Money{}, err
	}
	if (other.AmountMinor > 0 && m.AmountMinor > math.MaxInt64-other.AmountMinor) ||
		(other.AmountMinor < 0 && m.AmountMinor < math.MinInt64-other.AmountMinor) {
		return Money{}, fmt.Errorf("%w: %s + %s", ErrMoneyOverflow, m.String(), other.String())
	}
	return Money{AmountMinor: m.AmountMinor + other.AmountMinor, Currency: m.Currency}, nil
}

// Negate returns the exact negation, refusing the overflow of the most
// negative minor value.
func (m Money) Negate() (Money, error) {
	if m.AmountMinor == math.MinInt64 {
		return Money{}, fmt.Errorf("%w: %s", ErrMoneyOverflow, m.String())
	}
	return Money{AmountMinor: -m.AmountMinor, Currency: m.Currency}, nil
}

// String renders the canonical decimal form: exactly scale digits.
func (m Money) String() string {
	scale, err := currencyScale(m.Currency)
	if err != nil {
		return fmt.Sprintf("%d<%s>", m.AmountMinor, m.Currency)
	}
	negative := m.AmountMinor < 0
	minor := m.AmountMinor
	if negative {
		minor = -minor
	}
	whole, frac := minor/pow10(scale), minor%pow10(scale)
	sign := ""
	if negative {
		sign = "-"
	}
	if scale == 0 {
		return fmt.Sprintf("%s%d %s", sign, whole, m.Currency)
	}
	return fmt.Sprintf("%s%d.%0*d %s", sign, whole, scale, frac, m.Currency)
}
