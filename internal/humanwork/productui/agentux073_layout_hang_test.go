package productui

import (
	"strings"
	"testing"
)

// agentUX073Rule is one style rule with the at-rules it sits inside.
type agentUX073Rule struct {
	at, selector, body string
}

// agentUX073Rules walks a stylesheet rule by rule, keeping at-rule context.
func agentUX073Rules(sheet string) []agentUX073Rule {
	rules := []agentUX073Rule{}
	at := []string{}
	start := 0
	for i := 0; i < len(sheet); i++ {
		switch sheet[i] {
		case '{':
			head := strings.TrimSpace(sheet[start:i])
			if strings.HasPrefix(head, "@") {
				at = append(at, head)
				start = i + 1
				continue
			}
			end := strings.IndexByte(sheet[i:], '}')
			if end < 0 {
				return rules
			}
			rules = append(rules, agentUX073Rule{at: strings.Join(at, " "), selector: head, body: sheet[i+1 : i+end]})
			i += end
			start = i + 1
		case '}':
			if len(at) > 0 {
				at = at[:len(at)-1]
			}
			start = i + 1
		}
	}
	return rules
}

// agentUX073SelectorParts splits a selector list on the commas that are not
// inside parentheses.
func agentUX073SelectorParts(selector string) []string {
	parts, depth, start := []string{}, 0, 0
	for i, r := range selector {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, strings.TrimSpace(selector[start:i]))
				start = i + 1
			}
		}
	}
	return append(parts, strings.TrimSpace(selector[start:]))
}

// agentUX073FinalCompound is the last compound selector of one selector: what
// the rule actually styles.
func agentUX073FinalCompound(part string) string {
	depth, last := 0, 0
	for i, r := range part {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case ' ', '>', '+', '~':
			if depth == 0 {
				last = i + 1
			}
		}
	}
	return part[last:]
}

// agentUX073SizedLeaf reports whether a compound selector names only elements
// that have a size of their own and no layout children: the ones a percentage
// max-width is meant for.
func agentUX073SizedLeaf(compound string) bool {
	leaves := map[string]bool{"img": true, "svg": true, "table": true, "pre": true, "input": true, "select": true, "textarea": true, "button": true, "video": true, "canvas": true, ".button": true}
	names := []string{compound}
	if strings.HasPrefix(compound, ":is(") || strings.HasPrefix(compound, ":where(") {
		inner := compound[strings.IndexByte(compound, '(')+1:]
		if end := strings.LastIndexByte(inner, ')'); end >= 0 {
			inner = inner[:end]
		}
		names = agentUX073SelectorParts(inner)
	}
	for _, name := range names {
		name = agentUX073FinalCompound(strings.TrimSpace(name))
		// Keep the element name or the first class; drop pseudo-classes and
		// attribute tests, which do not change what kind of box it is.
		cut := len(name)
		for i, r := range name {
			if i > 0 && (r == ':' || r == '[' || r == '.') {
				cut = i
				break
			}
		}
		if !leaves[name[:cut]] {
			return false
		}
	}
	return true
}

func agentUX073PercentCap(body string) bool {
	for _, declaration := range strings.Split(body, ";") {
		property, value, ok := strings.Cut(declaration, ":")
		if !ok {
			continue
		}
		property = strings.TrimSpace(property)
		if (property == "max-width" || property == "max-inline-size") && strings.Contains(value, "%") {
			return true
		}
	}
	return false
}

// Agent setup froze the browser tab as soon as its cards were drawn. The agent
// pages' rules gave "max-width:100%" to every descendant of the page frame;
// with cards that nest grids and flex rows many levels deep, the browser's
// layout did not finish, so no script could run and no screenshot completed.
// (The rule had been written earlier but only took effect once the agent sheets
// joined the stylesheet the content security policy admits.) A percentage cap
// belongs on leaf elements that have a size of their own; containers are kept
// in their track by min-width:0.
func TestTodo_AGENTUX_073_LayoutCannotHang(t *testing.T) {
	// In the agent pages' own sheets a percentage cap is allowed only on leaves.
	checked := 0
	for _, rule := range agentUX073Rules(agentUX073Stylesheet()) {
		if !agentUX073PercentCap(rule.body) {
			continue
		}
		for _, part := range agentUX073SelectorParts(rule.selector) {
			checked++
			if compound := agentUX073FinalCompound(part); !agentUX073SizedLeaf(compound) {
				t.Errorf("%q caps a container at a percentage of its parent (%s); nested, this can stop the page's layout from finishing", part, strings.TrimSpace(rule.body))
			}
		}
	}
	if checked == 0 {
		t.Fatal("no percentage cap was examined; the scan is not reading the agent sheets")
	}

	// Nowhere in the served stylesheet does a rule for every descendant of an
	// agent page set a percentage size.
	scopes := []string{".agent-page-frame", ".persona-admin-page", ".agent-operations-page", ".agents-page", ".agent-announcements"}
	universal := 0
	for _, rule := range agentUX073Rules(Stylesheet()) {
		for _, part := range agentUX073SelectorParts(rule.selector) {
			scoped := false
			for _, scope := range scopes {
				scoped = scoped || strings.Contains(part, scope)
			}
			if compound := agentUX073FinalCompound(part); !scoped || (compound != "*" && !strings.HasPrefix(compound, "*:")) {
				continue
			}
			universal++
			for _, declaration := range strings.Split(rule.body, ";") {
				property, value, _ := strings.Cut(declaration, ":")
				switch strings.TrimSpace(property) {
				case "max-width", "max-inline-size", "width", "inline-size", "min-width", "min-inline-size", "height", "block-size", "max-height", "max-block-size":
					if strings.Contains(value, "%") {
						t.Errorf("%q sets %s on every descendant of an agent page", part, strings.TrimSpace(declaration))
					}
				}
			}
		}
	}
	if universal == 0 {
		t.Fatal("the universal agent-page rule was not found; the scan is not reading the served stylesheet")
	}

	// The fix itself: descendants are free to shrink, and only sized leaves are capped.
	sheet := Stylesheet()
	for _, want := range []string{
		`.agent-page-frame,.agent-page-frame *,.agent-operations-region-content,.agent-run-history{min-width:0;box-sizing:border-box}`,
		`.agent-page-frame :is(img,svg,table,pre,input,select,textarea,button){max-width:100%}`,
	} {
		if !strings.Contains(sheet, want) {
			t.Fatalf("the served stylesheet is missing %q", want)
		}
	}

	// The scan tells a container from a leaf.
	for selector, leaf := range map[string]bool{
		"*": false, ".card": false, "div": false, ".agent-version-table-wrap": false, ":is(.card,.agent-running-card)": false, ":is(input,.card)": false,
		"img": true, "textarea": true, ".button": true, "input:not([type=radio])": true, ":is(img,svg,table,pre,input,select,textarea,button)": true, "select": true,
	} {
		if agentUX073SizedLeaf(selector) != leaf {
			t.Errorf("selector %q judged leaf=%t, want %t", selector, !leaf, leaf)
		}
	}
	if !agentUX073PercentCap("min-width:0;max-width:100%") || agentUX073PercentCap("max-width:calc(100vw - 56px)") || agentUX073PercentCap("width:100%") || agentUX073PercentCap("max-inline-size:72ch") {
		t.Fatal("the scan does not tell a percentage cap from other sizes")
	}
}
