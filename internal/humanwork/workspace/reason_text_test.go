package workspace

import (
	"errors"
	"math/rand"
	"strings"
	"testing"
)

// TestTodo_REV_095_02_Property pins the one token-shape rule both the write
// path and the display formatter use, over generated values: a value is a
// token exactly when, once trimmed, it is non-empty, has no whitespace and
// carries an underscore or hyphen; ValidateBusinessReason refuses exactly
// those values, with a typed error naming the field it was given.
func TestTodo_REV_095_02_Property(t *testing.T) {
	alphabet := []rune("abcXYZ019_- \t\néü")
	rng := rand.New(rand.NewSource(95021))
	oracle := func(value string) bool {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return false
		}
		for _, r := range trimmed {
			if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
				return false
			}
		}
		return strings.ContainsRune(trimmed, '_') || strings.ContainsRune(trimmed, '-')
	}
	for i := 0; i < 5000; i++ {
		runes := make([]rune, rng.Intn(12))
		for j := range runes {
			runes[j] = alphabet[rng.Intn(len(alphabet))]
		}
		value := string(runes)
		want := oracle(value)
		if got := TokenShaped(value); got != want {
			t.Fatalf("TokenShaped(%q) = %v, want %v", value, got, want)
		}
		err := ValidateBusinessReason("business_reason", value)
		if (err != nil) != want {
			t.Fatalf("ValidateBusinessReason(%q) = %v, want refusal=%v", value, err, want)
		}
		if err != nil {
			var typed *JourneyInputError
			if !errors.As(err, &typed) || typed.FieldPath != "business_reason" || typed.ReasonRef != JourneyReasonNotProse || !errors.Is(err, ErrJourneyInput) {
				t.Fatalf("ValidateBusinessReason(%q) refusal is not the typed business_reason error: %v", value, err)
			}
		}
	}
	for _, prose := range []string{"Reorganisation", "Promotion into the senior HRBP role", "Covers the team-lead gap", ""} {
		if err := ValidateBusinessReason("reason", prose); err != nil {
			t.Fatalf("prose reason %q refused: %v", prose, err)
		}
	}
}
