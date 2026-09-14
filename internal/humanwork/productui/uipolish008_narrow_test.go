package productui

import (
	"strings"
	"testing"
)

func TestTodo_UIPOLISH_008_SelfServiceBoundaryNarrowDensity(t *testing.T) {
	css := Stylesheet()
	for _, want := range []string{
		`@media (max-width:420px){.self-service-boundary{`,
		`@media (max-width:420px){.self-service-boundary-icon{display:none;}}`,
		`@media (max-width:420px){.self-service-badge{grid-column:1;}}`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("narrow self-service notice lost its full-width copy: %q", want)
		}
	}
}
