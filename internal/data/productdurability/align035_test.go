package productdurability

import (
	"errors"
	"testing"
)

// TestTodo_ALIGN_035 proves monetary storage is exact: integer minor units
// in a pinned-scale currency, parsed without floats, refusing anything
// that would need rounding.
func TestTodo_ALIGN_035(t *testing.T) {
	usd, err := ParseMoneyExact("10.50", "USD")
	if err != nil {
		t.Fatalf("ParseMoneyExact: %v", err)
	}
	if usd.AmountMinor != 1050 || usd.Currency != "USD" {
		t.Fatalf("parsed = %+v, want 1050 minor USD", usd)
	}
	if got := usd.String(); got != "10.50 USD" {
		t.Fatalf("String = %q, want 10.50 USD", got)
	}
	jpy, err := ParseMoneyExact("10", "JPY")
	if err != nil {
		t.Fatalf("ParseMoneyExact JPY: %v", err)
	}
	if jpy.AmountMinor != 10 || jpy.String() != "10 JPY" {
		t.Fatalf("parsed JPY = %+v %q", jpy, jpy.String())
	}
}

func TestTodo_ALIGN_035_Property(t *testing.T) {
	// Decimal arithmetic stays exact: a tenth plus two tenths is three
	// tenths, with no binary float in between.
	ten, err := ParseMoneyExact("0.10", "USD")
	if err != nil {
		t.Fatal(err)
	}
	twenty, err := ParseMoneyExact("0.20", "USD")
	if err != nil {
		t.Fatal(err)
	}
	sum, err := ten.Add(twenty)
	if err != nil {
		t.Fatal(err)
	}
	want, err := ParseMoneyExact("0.30", "USD")
	if err != nil {
		t.Fatal(err)
	}
	if sum != want {
		t.Fatalf("0.10 + 0.20 = %v, want %v", sum, want)
	}
	// Short fractions pad exactly to scale.
	padded, err := ParseMoneyExact("10.5", "USD")
	if err != nil {
		t.Fatal(err)
	}
	full, err := ParseMoneyExact("10.50", "USD")
	if err != nil {
		t.Fatal(err)
	}
	if padded != full {
		t.Fatalf("10.5 parsed as %+v, want %+v", padded, full)
	}
}

func TestTodo_ALIGN_035_Golden(t *testing.T) {
	vectors := []struct{ amount, currency, want string }{
		{"10.50", "USD", "10.50 USD"},
		{"0.07", "EUR", "0.07 EUR"},
		{"10", "JPY", "10 JPY"},
		{"-3.25", "GBP", "-3.25 GBP"},
		{"100.00", "CAD", "100.00 CAD"},
		{"7.77", "AUD", "7.77 AUD"},
	}
	for _, v := range vectors {
		got, err := ParseMoneyExact(v.amount, v.currency)
		if err != nil {
			t.Fatalf("ParseMoneyExact(%q, %q): %v", v.amount, v.currency, err)
		}
		if got.String() != v.want {
			t.Fatalf("ParseMoneyExact(%q, %q) = %q, want %q", v.amount, v.currency, got.String(), v.want)
		}
	}
}

func TestTodo_ALIGN_035_Security(t *testing.T) {
	// Unknown currencies are refused, never stored with a guessed scale.
	if _, err := ParseMoneyExact("10.00", "XXX"); !errors.Is(err, ErrMoneyCurrency) {
		t.Fatalf("ParseMoneyExact(XXX) = %v, want ErrMoneyCurrency", err)
	}
	// Sub-scale precision is refused rather than rounded.
	if _, err := ParseMoneyExact("10.501", "USD"); !errors.Is(err, ErrMoneyInexact) {
		t.Fatalf("ParseMoneyExact(10.501 USD) = %v, want ErrMoneyInexact", err)
	}
	if _, err := ParseMoneyExact("10.5", "JPY"); !errors.Is(err, ErrMoneyInexact) {
		t.Fatalf("ParseMoneyExact(10.5 JPY) = %v, want ErrMoneyInexact", err)
	}
	// Mixed currencies never combine.
	usd, _ := ParseMoneyExact("1.00", "USD")
	eur, _ := ParseMoneyExact("1.00", "EUR")
	if _, err := usd.Add(eur); !errors.Is(err, ErrMoneyMismatch) {
		t.Fatalf("Add(USD, EUR) = %v, want ErrMoneyMismatch", err)
	}
	// Overflow is refused, never wrapped.
	huge := Money{AmountMinor: 9223372036854775807, Currency: "USD"}
	one, _ := ParseMoneyExact("0.01", "USD")
	if _, err := huge.Add(one); !errors.Is(err, ErrMoneyOverflow) {
		t.Fatalf("Add(overflow) = %v, want ErrMoneyOverflow", err)
	}
	// Malformed amounts are refused.
	for _, bad := range []string{"", "ten", "10.5.2", "--3", "  "} {
		if _, err := ParseMoneyExact(bad, "USD"); !errors.Is(err, ErrMoneyInvalid) {
			t.Fatalf("ParseMoneyExact(%q) = %v, want ErrMoneyInvalid", bad, err)
		}
	}
}

func TestTodo_ALIGN_035_Conformance(t *testing.T) {
	// Canonical forms round-trip: rendering then parsing is the identity.
	for _, canonical := range []string{"10.50", "0.07", "-3.25", "100.00"} {
		parsed, err := ParseMoneyExact(canonical, "USD")
		if err != nil {
			t.Fatalf("ParseMoneyExact(%q): %v", canonical, err)
		}
		if got := parsed.String(); got != canonical+" USD" {
			t.Fatalf("round trip %q -> %q", canonical, got)
		}
		reparsed, err := ParseMoneyExact(parsed.String()[:len(parsed.String())-4], "USD")
		if err != nil || reparsed != parsed {
			t.Fatalf("reparse of %q diverged", parsed.String())
		}
	}
	// Zero-scale currencies never render a decimal point.
	jpy, err := ParseMoneyExact("10", "JPY")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range jpy.String() {
		if c == '.' {
			t.Fatalf("JPY rendered with a decimal point: %q", jpy.String())
		}
	}
}

func FuzzTodo_ALIGN_035_Fuzz(f *testing.F) {
	f.Add("10.50", "USD")
	f.Fuzz(func(t *testing.T, amount, currency string) {
		got, err := ParseMoneyExact(amount, currency)
		if err != nil {
			return
		}
		if got.Currency != currency {
			t.Fatalf("currency changed in parse: %q vs %q", got.Currency, currency)
		}
		reparsed, err := ParseMoneyExact(got.String()[:len(got.String())-len(currency)-1], currency)
		if err != nil || reparsed != got {
			t.Fatalf("rendered %q does not reparse to %+v", got.String(), got)
		}
	})
}
