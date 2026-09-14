package journey

import (
	"regexp"
	"testing"
)

func TestTodo_UIPOLISH_003_JourneySurfacesDoNotUseRestingElevation(t *testing.T) {
	css := Stylesheet()
	for _, selector := range []string{`.jn-notice`, `.jn-card`, `.jn-panel`, `.jn-btn`, `.jn-btn[data-variant="secondary"]`} {
		pattern := regexp.MustCompile(regexp.QuoteMeta(selector) + `\{[^}]*box-shadow:none`)
		if !pattern.MatchString(css) {
			t.Errorf("%s does not keep ordinary surface elevation flat", selector)
		}
	}
	for _, selector := range []string{`.jn-journey:hover`, `.jn-btn:hover`} {
		pattern := regexp.MustCompile(regexp.QuoteMeta(selector) + `\{[^}]*box-shadow:`)
		if pattern.MatchString(css) {
			t.Errorf("%s elevates an ordinary surface on hover", selector)
		}
	}
}
