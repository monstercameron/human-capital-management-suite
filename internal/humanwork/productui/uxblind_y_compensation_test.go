package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UXBLIND_013(t *testing.T) {
	props := CompensationGuardrailPropsFrom(ResolveProductLocale("en-US"), compensationGuardrailAvailable(t, "550000.00"))
	if len(props.Facts) != 6 {
		t.Fatalf("facts = %d, want six guardrail facts", len(props.Facts))
	}
	if props.Facts[2].Label != "Allowed minimum for this promotion" || props.Facts[3].Label != "Allowed maximum for this promotion" {
		t.Fatalf("guardrail labels = %q, %q, want promotion-rule labels", props.Facts[2].Label, props.Facts[3].Label)
	}
	if props.Facts[4].Label != "Current pay versus the role band" {
		t.Fatalf("role-band label = %q, want an explicit role-band distinction", props.Facts[4].Label)
	}
}

func TestTodo_UXBLIND_013_Browser(t *testing.T) {
	props := CompensationGuardrailPropsFrom(ResolveProductLocale("en-US"), compensationGuardrailAvailable(t, "550000.00"))
	markup, err := ui.RenderToString(CompensationGuardrailCard(props))
	if err != nil {
		t.Fatalf("render guardrail: %v", err)
	}
	for _, want := range []string{"Allowed minimum for this promotion", "Allowed maximum for this promotion", "Current pay versus the role band"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("guardrail markup missing %q: %s", want, markup)
		}
	}
}
