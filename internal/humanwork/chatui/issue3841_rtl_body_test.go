package chatui

import (
	"regexp"
	"strings"
	"testing"
)

var issue3841Rule = regexp.MustCompile(`([^{}]+)\{([^{}]*)\}`)

// In an Arabic page a message whose words are English stays under its author on
// the start (right) side: the body block takes the page's direction through the
// stylesheet, which beats the dir attribute the language detection sets (#3841).
func TestIssue3841_RTLBodyBlockStaysOnTheStartSide(t *testing.T) {
	anchored := false
	for _, m := range issue3841Rule.FindAllStringSubmatch(Stylesheet, -1) {
		selectors, decls := m[1], strings.ReplaceAll(m[2], " ", "")
		if !strings.Contains(selectors, `.chat-workspace[dir="rtl"] .message-body`) {
			continue
		}
		for _, sel := range strings.Split(selectors, ",") {
			if strings.HasSuffix(strings.TrimSpace(sel), `.chat-workspace[dir="rtl"] .message-body`) &&
				strings.Contains(decls, "direction:rtl") && strings.Contains(decls, "text-align:start") {
				anchored = true
			}
		}
	}
	if !anchored {
		t.Fatal(`no .chat-workspace[dir="rtl"] .message-body rule sets direction:rtl and text-align:start`)
	}
}

// Nothing may push the body block to the physical left: no rule aimed at the
// body block itself may set a left margin, left alignment or a left float.
func TestIssue3841_NoRuleAnchorsBodyBlockToThePhysicalLeft(t *testing.T) {
	for _, m := range issue3841Rule.FindAllStringSubmatch(Stylesheet, -1) {
		selectors, decls := m[1], strings.ReplaceAll(m[2], " ", "")
		for _, sel := range strings.Split(selectors, ",") {
			sel = strings.TrimSpace(sel)
			if !regexp.MustCompile(`\.message-body(\[[^\]]*\])?$`).MatchString(sel) {
				continue
			}
			for _, bad := range []string{"text-align:left", "margin-right:auto", "margin-left:0;", "float:left", "justify-self:end", "direction:ltr"} {
				if strings.Contains(decls, bad) {
					t.Errorf("rule %q sets %q on the message body block", sel, bad)
				}
			}
		}
	}
}
