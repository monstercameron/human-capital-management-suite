package productui

import (
	"regexp"
	"strings"
	"testing"
)

// TestNoIsMixesElementsWithClasses: :is() takes the specificity of its most
// specific argument, so :is(label,.label) scores a bare <label> at (0,1,0) and
// lets a shared default beat a component's own single-class rule by source
// order. It did this four times -- headings, labels, tables, code -- and the
// journey form's labels, the journey section headings and more rendered at
// the wrong size. A bare element belongs in :where(), its classes in :is().
func TestNoIsMixesElementsWithClasses(t *testing.T) {
	element := regexp.MustCompile(`^[a-z][a-z0-9]*$`)
	for _, m := range regexp.MustCompile(`:is\(([^()]*)\)`).FindAllStringSubmatch(Stylesheet(), -1) {
		hasElement, hasClass := false, false
		for _, arg := range strings.Split(m[1], ",") {
			arg = strings.TrimSpace(arg)
			switch {
			case arg == "":
			case strings.ContainsAny(arg[:1], ".[:"):
				hasClass = true
			case element.MatchString(arg):
				hasElement = true
			}
		}
		if hasElement && hasClass {
			t.Errorf("%s lifts its bare elements to class specificity", m[0])
		}
	}
}
