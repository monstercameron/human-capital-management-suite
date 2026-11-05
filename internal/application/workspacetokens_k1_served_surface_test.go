package application_test

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/application"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/tokens"
)

// TestTodo_WEB_024_Served proves WEB-024's high-contrast and print mode
// contracts are reachable through the served application boundary: the served
// workspace stylesheet carries all three media contracts, the CSS variables
// are served from the same surface, and the contrast checker enforces the
// accessibility floor on served text pairs.
func TestTodo_WEB_024_Served(t *testing.T) {
	surface := application.NewServedWorkspaceTokensSurface()

	css := surface.WorkspaceCSS()
	for _, media := range []string{"@media (prefers-contrast:more)", "@media (forced-colors:active)", "@media print"} {
		if !strings.Contains(css, media) {
			t.Fatalf("served workspace CSS is missing the %s contract", media)
		}
	}
	if vars := surface.CSSVariables(); vars == "" {
		t.Fatal("served CSS variables are empty")
	}
	for _, pair := range surface.TextPairs() {
		ratio, err := surface.ContrastRatio(pair.Foreground.Hex, pair.Background.Hex)
		if err != nil {
			t.Fatalf("served contrast check: %v", err)
		}
		if ratio < tokens.MinRatioNormalText {
			t.Fatalf("served text pair %+v contrast ratio = %.2f, want >= %.1f", pair, ratio, tokens.MinRatioNormalText)
		}
	}
	if _, err := surface.ContrastRatio("not-a-color", "#ffffff"); err == nil {
		t.Fatal("served contrast check accepted a malformed color")
	}
}

func TestServedWorkspaceTokensAppSurface(t *testing.T) {
	var app application.App
	if got := app.WorkspaceTokens(); got.WorkspaceCSS == nil || got.ContrastRatio == nil {
		t.Fatal("composed application omitted WorkspaceTokens surface")
	}
	var nilApp *application.App
	if got := nilApp.WorkspaceTokens(); got.WorkspaceCSS != nil {
		t.Fatal("nil application exposed WorkspaceTokens capabilities")
	}
}
