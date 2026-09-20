package evolution

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
)

// TestTodo_REV_049_02 proves the MANAGED publish checker adapter: it reports
// the real CompatibilityCheck verdict with deterministic evidence, and
// surfaces caller mistakes as errors rather than verdicts.
func TestTodo_REV_049_02(t *testing.T) {
	check := ManagedChecker()
	if check == nil {
		t.Fatal("ManagedChecker returned nil; the registry MANAGED action needs the real check")
	}

	t.Run("compatible versions report compatible with evidence", func(t *testing.T) {
		ok, evidence, err := check(promotionV1(), promotionV2CompatibleOptionalAdded())
		if err != nil {
			t.Fatalf("checker error: %v", err)
		}
		if !ok {
			t.Fatal("an optional-input addition must report compatible")
		}
		if evidence == "" {
			t.Fatal("the checker recorded no evidence")
		}
	})

	t.Run("incompatible versions report incompatible with evidence", func(t *testing.T) {
		ok, evidence, err := check(promotionV1(), promotionV2RequiredInputAdded())
		if err != nil {
			t.Fatalf("checker error: %v", err)
		}
		if ok {
			t.Fatal("a required-input addition must report incompatible")
		}
		if evidence == "" {
			t.Fatal("the checker recorded no evidence")
		}
	})

	t.Run("caller mistakes are errors, not verdicts", func(t *testing.T) {
		other := promotionV1()
		other.Ref = intent.Ref{TypeID: "hcmnext.people.something_else", Version: 1}
		if _, _, err := check(promotionV1(), other); !errors.Is(err, ErrDifferentIntentType) {
			t.Fatalf("cross-type check err = %v, want ErrDifferentIntentType", err)
		}
		stale := promotionV1()
		if _, _, err := check(promotionV1(), stale); !errors.Is(err, ErrVersionNotAdvancing) {
			t.Fatalf("non-advancing check err = %v, want ErrVersionNotAdvancing", err)
		}
	})
}
