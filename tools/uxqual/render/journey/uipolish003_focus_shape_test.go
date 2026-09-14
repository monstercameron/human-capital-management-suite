package journey

import (
	"strings"
	"testing"
)

// TestTodo_UIPOLISH_003_FocusRingUsesCompactControlShape proves the rendered
// focus ring follows the same semantic radius as compact controls. A literal
// radius here would drift from the control shape when the token scale changes.
func TestTodo_UIPOLISH_003_FocusRingUsesCompactControlShape(t *testing.T) {
	css := Stylesheet()
	want := `:where(.jn-page,.jn-embedded) :focus-visible{border-radius:var(--jn-r1);outline:3px solid var(--jn-accent);outline-offset:2px;}`
	if !strings.Contains(css, want) {
		const marker = `:where(.jn-page,.jn-embedded) :focus-visible{`
		start := strings.Index(css, marker)
		if start >= 0 {
			t.Fatalf("focus-visible rule does not use compact control radius token; want %q, got %q", want, css[start:start+strings.Index(css[start:], "}")+1])
		}
		t.Fatalf("focus-visible rule is missing; want %q", want)
	}
	if strings.Contains(css, `:focus-visible{border-radius:.25rem;outline:3px solid var(--jn-accent);outline-offset:2px;}`) {
		t.Fatal("focus-visible rule still hard-codes the pre-token radius")
	}
}
