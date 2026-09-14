package journey

import (
	"strings"
	"testing"
)

// TestTodo_UIPOLISH_010_BrandRegistryPreservesClass proves the registry does
// not discard styling hooks when resolving the substitutable brand semantic.
func TestTodo_UIPOLISH_010_BrandRegistryPreservesClass(t *testing.T) {
	const class = "customer-brand-mark"
	out := renderNode(t, RenderIcon(IconBrandMark, class, nil))
	if !strings.Contains(out, `class="`+class+`"`) {
		t.Fatalf("registry-rendered brand mark dropped caller class %q: %s", class, out)
	}
	if strings.Contains(out, `class="jn-mark"`) {
		t.Fatalf("registry-rendered brand mark retained hard-coded class instead of caller class: %s", out)
	}
}
