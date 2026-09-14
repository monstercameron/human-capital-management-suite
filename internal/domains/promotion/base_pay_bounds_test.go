package promotion

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestTodo_PROMOUX_007_ExactBasePayBounds(t *testing.T) {
	current, err := values.NewMoney("100.03", "USD", 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	minimum, err := values.NewPercentage("0.0500", 4, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	maximum, err := values.NewPercentage("0.1800", 4, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	low, high, err := BasePayBounds(current, minimum, maximum)
	if err != nil {
		t.Fatal(err)
	}
	if low.Amount().String() != "105.04" || high.Amount().String() != "118.03" {
		t.Fatalf("inclusive cent bounds = %s..%s", low, high)
	}
	for _, amount := range []string{"105.03", "118.04"} {
		candidate, err := values.NewMoney(amount, "USD", 2, values.RoundingExactRequired)
		if err != nil {
			t.Fatal(err)
		}
		if cmp, _ := candidate.Cmp(low); cmp >= 0 {
			if cmp, _ := candidate.Cmp(high); cmp <= 0 {
				t.Fatalf("out-of-range amount %s admitted by bounds", amount)
			}
		}
	}
	if _, _, err := BasePayBounds(current, maximum, minimum); !errors.Is(err, ErrBasePayBoundsInvalid) {
		t.Fatalf("reversed ladder edge = %v", err)
	}
}
