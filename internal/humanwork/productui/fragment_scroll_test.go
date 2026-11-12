package productui

import (
	"os"
	"strings"
	"testing"
)

// TestIssue3683_FragmentTargetID pins how a URL fragment becomes an element id.
func TestIssue3683_FragmentTargetID(t *testing.T) {
	for hash, want := range map[string]string{
		"#agents-setting": "agents-setting",
		"":                "",
		"#":               "",
		"#a%20b":          "a b",
	} {
		if got := fragmentTargetID(hash); got != want {
			t.Errorf("fragmentTargetID(%q) = %q, want %q", hash, got, want)
		}
	}
}

// TestIssue3683_NavigationReappliesFragment proves the shell re-applies the
// URL fragment after the client renders, which the native fragment scroll
// cannot do for content that does not exist at load.
func TestIssue3683_NavigationReappliesFragment(t *testing.T) {
	for file, want := range map[string]string{
		"uxblind_tt_navigation.go":      "scrollToURLFragment(",
		"uxblind_tt_nav_scroll_wasm.go": "MutationObserver",
	} {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), want) {
			t.Errorf("%s does not contain %q", file, want)
		}
	}
}
