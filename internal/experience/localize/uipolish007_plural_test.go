package localize

import (
	"math/big"
	"testing"
)

func TestTodo_UIPOLISH_007_ArabicCardinalPluralRules(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"0", "zero"}, {"1", "one"}, {"2", "two"}, {"3", "few"}, {"10", "few"},
		{"11", "many"}, {"99", "many"}, {"100", "other"}, {"103", "few"},
		{"111", "many"}, {"1.5", "other"}, {"1000000000000000000000000002", "other"},
	} {
		value, ok := new(big.Rat).SetString(tc.input)
		if !ok {
			t.Fatalf("invalid test count %q", tc.input)
		}
		if got := pluralCase("ar", value); got != tc.want {
			t.Errorf("ar %s = %s, want %s", tc.input, got, tc.want)
		}
		if got := pluralCase("en-US", value); got == "two" || got == "few" || got == "many" || got == "zero" {
			t.Errorf("en-US %s unexpectedly used Arabic category %s", tc.input, got)
		}
	}
}

func TestTodo_UIPOLISH_007_ArabicNumbersKeepExactDecimalAndNativeDigits(t *testing.T) {
	if got, err := FormatNumber("ar", "1234.5", 2); err != nil || got != "١٬٢٣٤٫٥٠" {
		t.Fatalf("Arabic number = %q, %v", got, err)
	}
	if got, err := FormatMoney("ar", "1234.5", "USD", 2); err != nil || got != "USD\u00a0١٬٢٣٤٫٥٠" {
		t.Fatalf("Arabic money = %q, %v", got, err)
	}
}
