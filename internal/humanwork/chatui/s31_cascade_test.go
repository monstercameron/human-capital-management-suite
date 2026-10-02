package chatui

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
)

var s31Prop = regexp.MustCompile(`(?:^|;)\s*([a-z-]+)\s*:`)

// s31OverridesIn lists the rules in css after the last copy of block that give
// a property to the same element block's rules do: same selector (or a
// descendant form of it) under the same at-rule conditions, with a property in
// common. A block that is not last in the stylesheet is fine as long as the
// list is empty; "it is the last block" was only ever a stand-in for that.
func s31OverridesIn(css, block string) []string {
	at := strings.LastIndex(css, block)
	if at < 0 {
		return []string{"the block is not part of the stylesheet"}
	}
	props := func(body string) map[string]bool {
		out := map[string]bool{}
		for _, m := range s31Prop.FindAllStringSubmatch(body, -1) {
			out[m[1]] = true
		}
		return out
	}
	var found []string
	later := s11ParseRules(css[at+len(block):])
	for _, own := range s11ParseRules(block) {
		ownProps := props(own.body)
		for _, r := range later {
			if strings.Join(r.conds, "|") != strings.Join(own.conds, "|") {
				continue
			}
			if r.selector != own.selector && !strings.HasSuffix(r.selector, " "+own.selector) {
				continue
			}
			for p := range props(r.body) {
				if ownProps[p] {
					found = append(found, r.selector+" {"+p+"} redraws "+own.selector)
				}
			}
		}
	}
	return found
}

// s31LaterOverrides is s31OverridesIn for the real stylesheet.
func s31LaterOverrides(t *testing.T, block string) []string {
	t.Helper()
	return s31OverridesIn(Stylesheet, block)
}

func TestS31CascadeHelper(t *testing.T) {
	block := `.a{color:red;gap:2px}@media(max-width:600px){.b{width:1px}}`
	if got := s31OverridesIn(block+`.c{color:blue}`, block); len(got) != 0 {
		t.Errorf("another element: %v", got)
	}
	if got := s31OverridesIn(block+`.z .a{color:blue}`, block); !reflect.DeepEqual(got, []string{".z .a {color} redraws .a"}) {
		t.Errorf("a descendant form: %v", got)
	}
	if got := s31OverridesIn(block+`@media(max-width:600px){.b{width:2px}}`, block); len(got) != 1 {
		t.Errorf("same condition: %v", got)
	}
	if got := s31OverridesIn(block+`@media(min-width:900px){.b{width:2px}}`, block); len(got) != 0 {
		t.Errorf("other condition: %v", got)
	}
	if got := s31OverridesIn(block+`.a{padding:0}`, block); len(got) != 0 {
		t.Errorf("another property: %v", got)
	}
	// The real stylesheet: nothing after the Lane2 block redraws it.
	if got := s31LaterOverrides(t, ChatLane2Styles); len(got) != 0 {
		t.Errorf("later blocks redraw the Lane2 rules: %v", got)
	}
}
