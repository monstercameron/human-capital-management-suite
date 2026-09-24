package workspace

import "testing"

func TestEmployeePhotosAreDevelopmentSeedOnlyAssets(t *testing.T) {
	for _, original := range []string{
		"person-priya.png", "person-jane.png", "person-omar.png", "person-lena.png", "person-noor.png",
		"person-priya-small.jpg", "person-jane-small.jpg", "person-omar-small.jpg", "person-lena-small.jpg", "person-noor-small.jpg",
	} {
		if _, ok := FrontendAssetContentType(original); ok {
			t.Errorf("legacy demo media %q is routable", original)
		}
		if _, ok := asset(original); ok {
			t.Errorf("legacy demo media %q is embedded in the clean source tree", original)
		}
	}
	for _, name := range []string{"person-hc-001-small.jpg", "person-hc-059-small.jpg"} {
		if got, ok := FrontendAssetContentType(name); !ok || got != "image/jpeg" {
			t.Errorf("seed proxy %q = (%q, %v), want image/jpeg and routable", name, got, ok)
		}
	}
	for _, name := range []string{"person-unknown.png", "person-hc-000-small.jpg", "person-hc-004-small.jpg", "person-hc-060-small.jpg", "person-hc-01-small.jpg"} {
		if _, ok := FrontendAssetContentType(name); ok {
			t.Errorf("invalid demo media %q is routable", name)
		}
	}
}
