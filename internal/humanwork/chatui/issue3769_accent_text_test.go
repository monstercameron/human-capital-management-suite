package chatui

import (
	"regexp"
	"strings"
	"testing"
)

// #3769: accent-coloured text in Chat must read the text-safe token, never the
// brand fill, even through a var() fallback chain (the brand fill is about
// 2.6:1 on the dark message surface).
func TestIssue3769_NoTextUsesTheBrandFillThroughAFallback(t *testing.T) {
	brandText := regexp.MustCompile(`(?:^|[;{\s])color:\s*var\(--hcm-color-brand-primary`)
	for _, hit := range brandText.FindAllStringIndex(Stylesheet, -1) {
		start := strings.LastIndex(Stylesheet[:hit[0]], "}") + 1
		end := hit[1] + strings.Index(Stylesheet[hit[1]:], "}")
		t.Errorf("accent text still reads the brand fill: %s", Stylesheet[start:end+1])
	}
	if !strings.Contains(Stylesheet, ".chatcmd002-link{padding:0;border:0;background:none;color:var(--chat-accent-text,var(--accent))") {
		t.Error("the poll and card link does not read --chat-accent-text")
	}
}
