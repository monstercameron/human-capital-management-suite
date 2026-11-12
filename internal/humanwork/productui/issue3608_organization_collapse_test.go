package productui

import (
	"regexp"
	"testing"
)

// TestIssue3608_CollapsedBranchIsNotDisplayed proves the stylesheet keeps a
// collapsed reporting branch out of layout: the grid rule on the tree list
// would otherwise override the hidden attribute, leaving descendants drawn and
// exposed to the accessible tree.
func TestIssue3608_CollapsedBranchIsNotDisplayed(t *testing.T) {
	css := Stylesheet()
	hiddenRule := regexp.MustCompile(`\.ownership-tree ul\[hidden\]\s*\{[^}]*display:\s*none`)
	if !hiddenRule.MatchString(css) {
		t.Fatal("stylesheet has no display:none rule for a hidden ownership-tree branch")
	}
	// The rule must be more specific than the grid rule it overrides.
	gridRule := regexp.MustCompile(`\.ownership-tree ul\s*\{[^}]*display:\s*grid`)
	if !gridRule.MatchString(css) {
		t.Fatal("grid rule for ownership-tree ul not found; the regression premise changed")
	}
}
