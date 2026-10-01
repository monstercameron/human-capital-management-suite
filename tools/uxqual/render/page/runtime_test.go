package page

import "testing"

// TestServedRuntimeContract proves the renderer bridge is a real deterministic
// composition rather than a package-presence import: both governed Promotion
// pages resolve widgets, floorplans, semantic SSR, and GWC output, and the
// resulting evidence digest is stable across calls.
func TestServedRuntimeContract(t *testing.T) {
	first, err := BuildPromotionRuntimeContract()
	if err != nil {
		t.Fatalf("BuildPromotionRuntimeContract: %v", err)
	}
	second, err := BuildPromotionRuntimeContract()
	if err != nil {
		t.Fatalf("BuildPromotionRuntimeContract second call: %v", err)
	}
	if first.Digest == "" || first.WidgetRegistryDigest == "" || first.StylesheetDigest == "" {
		t.Fatalf("runtime contract omitted retained evidence: %+v", first)
	}
	if len(first.Pages) != 2 {
		t.Fatalf("runtime contract pages = %d, want 2", len(first.Pages))
	}
	for _, page := range first.Pages {
		if page.PageID == "" || page.PageDefinitionDigest == "" || page.SemanticShellDigest == "" || page.GWCPageDigest == "" {
			t.Fatalf("runtime page omitted retained evidence: %+v", page)
		}
	}
	if first.Digest != second.Digest {
		t.Fatalf("runtime contract digest changed: first=%q second=%q", first.Digest, second.Digest)
	}
}
