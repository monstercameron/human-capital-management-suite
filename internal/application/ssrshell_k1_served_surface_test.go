package application_test

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/application"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/pagedef"
)

func servedShellPage() pagedef.PageDefinition {
	return pagedef.PageDefinition{
		PageID:       "test.page",
		Version:      1,
		FloorplanRef: "floorplan.test.v1",
		Regions: []pagedef.Region{
			{ID: "shell", Kind: pagedef.RegionShell},
			{ID: "identity", Kind: pagedef.RegionPageIdentity, Heading: &pagedef.Heading{Level: 1, Text: "Test page"}},
			{ID: "authority", Kind: pagedef.RegionAuthorityContext, Heading: &pagedef.Heading{Level: 2, Text: "Authority"}},
			{ID: "nav", Kind: pagedef.RegionLocalNavigation, Heading: &pagedef.Heading{Level: 2, Text: "Navigation"}},
			{
				ID:      "primary",
				Kind:    pagedef.RegionPrimary,
				Heading: &pagedef.Heading{Level: 2, Text: "Primary job"},
				Widgets: []pagedef.WidgetSlot{{ID: "w1", WidgetRef: "widget.table.v1"}},
			},
			{ID: "supporting", Kind: pagedef.RegionSupporting, Heading: &pagedef.Heading{Level: 3, Text: "Supporting"}},
			{ID: "utility", Kind: pagedef.RegionUtility, Heading: &pagedef.Heading{Level: 3, Text: "Utility"}},
			{ID: "completion", Kind: pagedef.RegionCompletion, Heading: &pagedef.Heading{Level: 2, Text: "Completion"}},
		},
		Accessibility: pagedef.Accessibility{
			Landmarks:  []string{"banner", "navigation", "main", "complementary", "contentinfo"},
			LiveRegion: pagedef.LiveRegionPolite,
		},
		BrandTokens: []string{"brand.color.primary"},
	}
}

// TestTodo_WEB_025_Served proves WEB-025's semantic shell renderer is
// reachable through the served application boundary: a valid page renders to a
// deterministic landmark shell with a digest, an invalid page is refused, and
// the shell stylesheet and CSP are served from the same surface.
func TestTodo_WEB_025_Served(t *testing.T) {
	surface := application.NewServedSSRShellSurface()

	first, err := surface.Render(servedShellPage())
	if err != nil {
		t.Fatalf("served Render: %v", err)
	}
	if first.Digest == "" || !strings.Contains(first.HTML, "<main") {
		t.Fatalf("served shell is missing its digest or main landmark: %+v", first)
	}
	second, err := surface.Render(servedShellPage())
	if err != nil || second.HTML != first.HTML || second.Digest != first.Digest {
		t.Fatal("served shell render is not deterministic")
	}

	invalid := servedShellPage()
	invalid.PageID = ""
	if _, err := surface.Render(invalid); err == nil {
		t.Fatal("served Render accepted an invalid PageDefinition")
	}

	if css := surface.ShellCSS(); css == "" {
		t.Fatal("served ShellCSS is empty")
	}
	if csp := surface.ContentSecurityPolicy(); !strings.Contains(csp, "default-src") {
		t.Fatalf("served CSP = %q, want a default-src directive", csp)
	}
	if tag, _, ok := surface.LandmarkForRegionKind(pagedef.RegionPrimary); !ok || tag == "" {
		t.Fatal("served landmark lookup does not resolve the primary region")
	}
}

func TestServedSSRShellAppSurface(t *testing.T) {
	var app application.App
	if got := app.SSRShell(); got.Render == nil || got.ContentSecurityPolicy == nil {
		t.Fatal("composed application omitted SSRShell surface")
	}
	var nilApp *application.App
	if got := nilApp.SSRShell(); got.Render != nil {
		t.Fatal("nil application exposed SSRShell capabilities")
	}
}
