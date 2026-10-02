package chatui

import (
	"regexp"
	"strings"
	"testing"
)

// agentux058Rules splits a stylesheet into (selector, declarations) pairs,
// looking inside @media and @supports blocks as well.
func agentux058Rules(css string) [][2]string {
	var rules [][2]string
	rule := regexp.MustCompile(`([^{}]+)\{([^{}]*)\}`)
	for _, match := range rule.FindAllStringSubmatch(css, -1) {
		selector := strings.TrimSpace(match[1])
		if strings.HasPrefix(selector, "@") {
			continue
		}
		rules = append(rules, [2]string{selector, match[2]})
	}
	return rules
}

// agentux058RowSelector reports whether one selector of a rule styles a message
// row itself (not something inside it) in a hover or focus state.
func agentux058RowSelector(selector string) bool {
	selector = strings.TrimSpace(selector)
	// The subject of the selector is its last compound.
	fields := strings.FieldsFunc(selector, func(r rune) bool { return r == ' ' || r == '>' || r == '+' || r == '~' })
	if len(fields) == 0 {
		return false
	}
	subject := fields[len(fields)-1]
	row := strings.HasPrefix(subject, ".message:") || strings.HasPrefix(subject, ".message.") && strings.Contains(subject, ":") || strings.HasPrefix(subject, ".thread-message:") || strings.HasPrefix(subject, ".virtual-row:")
	// The first message of an open thread and an agent's answer card are rows of
	// the list too: a hover that resized either would move everything under it.
	for _, other := range []string{".thread-root", ".chat-ephemeral", ".agent-reply-row", ".persona-progress-failure", ".persona-progress-status"} {
		row = row || strings.HasPrefix(subject, other+":") || strings.HasPrefix(subject, other+".") && strings.Contains(subject, ":")
	}
	if !row {
		return false
	}
	return strings.Contains(subject, ":hover") || strings.Contains(subject, ":focus") || strings.Contains(subject, ":has(")
}

// No rule changes a message row's box when it is hovered, focused or shows its
// actions: the list must not move under the pointer. The action bar is taken
// out of the row's flow and straddles its top edge at the inline end.
func TestTodo_AGENTUX_058(t *testing.T) {
	box := regexp.MustCompile(`(?:^|;)\s*(padding[a-z-]*|margin[a-z-]*|border(?:-[a-z]+)*-width|border|height|min-height|max-height|block-size|min-block-size|line-height|font-size|display|position)\s*:`)
	checked := 0
	for _, rule := range agentux058Rules(Stylesheet) {
		for _, selector := range strings.Split(rule[0], ",") {
			if !agentux058RowSelector(selector) {
				continue
			}
			checked++
			if found := box.FindStringSubmatch(rule[1]); found != nil {
				t.Errorf("%s changes the row's %s on hover or focus: {%s}", strings.TrimSpace(selector), found[1], rule[1])
			}
		}
	}
	if checked == 0 {
		t.Fatal("no hover or focus rule for message rows was found; the test is looking at the wrong selectors")
	}
	if strings.Contains(Stylesheet, ".message:hover{padding-block-start:44px}") {
		t.Fatal("the rule that grew a hovered row by 36px is back")
	}
	// The action bar: out of flow, on the row's top edge, at the inline end.
	bar := map[string]string{}
	for _, rule := range agentux058Rules(Stylesheet) {
		for _, selector := range strings.Split(rule[0], ",") {
			if strings.TrimSpace(selector) != ".message-actions" {
				continue
			}
			for _, declaration := range strings.Split(rule[1], ";") {
				if name, value, ok := strings.Cut(declaration, ":"); ok {
					bar[strings.TrimSpace(name)] = strings.TrimSpace(value)
				}
			}
		}
	}
	if bar["position"] != "absolute" {
		t.Fatalf("the action bar is in the row's flow: position=%q", bar["position"])
	}
	if bar["top"] != "0" || !strings.Contains(bar["transform"], "translateY(-50%)") {
		t.Fatalf("the action bar does not straddle the row's top edge: top=%q transform=%q", bar["top"], bar["transform"])
	}
	if bar["inset-inline-end"] == "" && bar["right"] == "" {
		t.Fatalf("the action bar is not at the inline end: %+v", bar)
	}
	// On the first row there is nothing above to straddle: the bar sits below
	// the row's top edge instead of being cut off by the list.
	if !strings.Contains(Stylesheet, ".message-list .message:first-of-type .message-actions{top:4px;transform:none}") {
		t.Fatal("the action bar of the first row is not moved below the row's top edge")
	}
}
