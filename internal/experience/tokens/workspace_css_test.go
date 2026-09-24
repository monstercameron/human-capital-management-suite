package tokens

import (
	"strings"
	"testing"
)

func TestWorkspaceCSSContainsProductFoundation(t *testing.T) {
	first := WorkspaceCSS()
	if first == "" {
		t.Fatal("WorkspaceCSS returned an empty stylesheet")
	}
	for _, token := range []string{"--color-background", "--color-text", "--border-color-focus", "--radius-control"} {
		if !strings.Contains(first, token) {
			t.Errorf("WorkspaceCSS is missing %q", token)
		}
	}
	if second := WorkspaceCSS(); first != second {
		t.Fatal("WorkspaceCSS changed between calls")
	}
}

func TestContrastRatioRejectsMalformedColors(t *testing.T) {
	if _, err := ContrastRatio("white", "#000000"); err == nil {
		t.Fatal("ContrastRatio accepted a color without the #rrggbb form")
	}
	if _, err := ContrastRatio("#ffffff", "#000000"); err != nil {
		t.Fatalf("ContrastRatio rejected valid colors: %v", err)
	}
}
