package main

import (
	"os"
	"strings"
	"testing"
)

// TestIssue_3692_FilterSkipsFoldedContext: the type-to-filter on the Moderation
// page matches only what a row shows. The conversation folded under an item is
// marked by the page and left out of the text the filter reads; the marker is
// the one the page renders.
func TestIssue_3692_FilterSkipsFoldedContext(t *testing.T) {
	source, err := os.ReadFile("chatmod005_overlay_wasm.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	if !strings.Contains(text, "hide := needle != \"\" && !strings.Contains(strings.ToLower(chatmod005ShownText(item)), needle)") {
		t.Error("the page filter still reads every character of a row, including the folded conversation")
	}
	if strings.Contains(text, `item.Get("textContent")`) {
		t.Error("the page filter reads textContent, which holds the folded conversation")
	}
	if !strings.Contains(text, `hasAttribute", "data-chatremove-context"`) {
		t.Error("the shown-text walk does not skip the folded conversation marked by the page")
	}
}
